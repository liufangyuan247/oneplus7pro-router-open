package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ServiceUpdateInfo contains version and upgrade metadata for a managed service.
type ServiceUpdateInfo struct {
	Name           string `json:"name"`
	Label          string `json:"label"`
	CurrentVersion string `json:"currentVersion"`
	BaseVersion    string `json:"baseVersion,omitempty"`
	CustomBuild    bool   `json:"customBuild,omitempty"`
	LatestVersion  string `json:"latestVersion"`
	Upgradable     bool   `json:"upgradable"`
	HasBackup      bool   `json:"hasBackup"`
	BackupVersion  string `json:"backupVersion,omitempty"`
	BackupTime     string `json:"backupTime,omitempty"`
	CanUpgrade     bool   `json:"canUpgrade"`
	UpgradeHint    string `json:"upgradeHint,omitempty"`
	Upgrading      bool   `json:"upgrading"`
	LastChecked    string `json:"lastChecked"`
	Error          string `json:"error,omitempty"`
}

type serviceBackupMeta struct {
	Service   string `json:"service"`
	Version   string `json:"version"`
	Binary    string `json:"binary"`
	Timestamp string `json:"timestamp"`
}

type serviceUpdaterSpec struct {
	Name          string
	Binary        string
	GitHubRepo    string
	IsAptPackage  bool
	AptPackage    string
	Port          int
	ExtractBinary string
	PreflightTest func(s *Server, candidatePath string) error
	HealthProbe   func(s *Server) bool
	DetectionOnly bool
}

type serviceBuildInfo struct {
	SchemaVersion      int      `json:"schemaVersion"`
	Service            string   `json:"service,omitempty"`
	Product            string   `json:"product"`
	CustomVersion      string   `json:"customVersion"`
	UpstreamVersion    string   `json:"upstreamVersion"`
	UpstreamCommit     string   `json:"upstreamCommit"`
	UpstreamRepository string   `json:"upstreamRepository,omitempty"`
	Variant            string   `json:"variant"`
	BuildDate          string   `json:"buildDate"`
	SHA256             string   `json:"sha256"`
	Patches            []string `json:"patches,omitempty"`
}

var (
	serviceUpdatesMu    sync.RWMutex
	serviceUpdatesCache = make(map[string]*ServiceUpdateInfo)
	serviceUpgrading    = make(map[string]bool)
	serviceUpdatesAt    time.Time

	// Services with upstream release tracking. DetectionOnly entries deliberately
	// do not use the generic binary updater because they require a custom build.
	supportedServiceUpdaters = map[string]serviceUpdaterSpec{
		"cpa": {
			Name:          "cpa",
			Binary:        "cli-proxy-api",
			GitHubRepo:    "router-for-me/CLIProxyAPI",
			DetectionOnly: true,
		},
		"cpa-usage-keeper": {
			Name:          "cpa-usage-keeper",
			Binary:        "cpa-usage-keeper",
			GitHubRepo:    "Willxup/cpa-usage-keeper",
			DetectionOnly: true,
		},
		"openlist": {
			Name:          "openlist",
			Binary:        "openlist",
			GitHubRepo:    "OpenListTeam/OpenList",
			ExtractBinary: "openlist",
			Port:          5244,
			PreflightTest: func(s *Server, candidatePath string) error {
				out, err := exec.Command(candidatePath, "version").CombinedOutput()
				if err != nil {
					return fmt.Errorf("openlist version test failed: %w (%s)", err, strings.TrimSpace(string(out)))
				}
				if !strings.Contains(string(out), "Version:") {
					return fmt.Errorf("unexpected openlist version output: %s", strings.TrimSpace(string(out)))
				}
				return nil
			},
			HealthProbe: func(s *Server) bool {
				client := http.Client{Timeout: 2 * time.Second}
				resp, err := client.Get("http://127.0.0.1:5244/")
				if err == nil {
					_ = resp.Body.Close()
					return true
				}
				return false
			},
		},
		"mihomo": {
			Name:       "mihomo",
			Binary:     "mihomo",
			GitHubRepo: "MetaCubeX/mihomo",
			Port:       17890,
			PreflightTest: func(s *Server, candidatePath string) error {
				out, err := exec.Command(candidatePath, "-v").CombinedOutput()
				if err != nil {
					return fmt.Errorf("mihomo core test failed: %w (%s)", err, strings.TrimSpace(string(out)))
				}
				cfgPath := filepath.Join(s.data, "mihomo", "config.yaml")
				if _, statErr := os.Stat(cfgPath); statErr == nil {
					testCmd := exec.Command(candidatePath, "-t", "-d", filepath.Join(s.data, "mihomo"), "-f", cfgPath)
					testOut, testErr := testCmd.CombinedOutput()
					if testErr != nil {
						return fmt.Errorf("mihomo 配置语法预检失败: %s", strings.TrimSpace(string(testOut)))
					}
				}
				return nil
			},
			HealthProbe: func(s *Server) bool {
				var versionResp struct {
					Version string `json:"version"`
				}
				err := s.mihomoRequest("GET", "/version", nil, &versionResp)
				return err == nil && versionResp.Version != ""
			},
		},
		"qbittorrent": {
			Name:         "qbittorrent",
			Binary:       "qbittorrent-nox",
			IsAptPackage: true,
			AptPackage:   "qbittorrent-nox",
			Port:         8080,
			HealthProbe: func(s *Server) bool {
				client := http.Client{Timeout: 2 * time.Second}
				resp, err := client.Get("http://127.0.0.1:8080/")
				if err == nil {
					_ = resp.Body.Close()
					return true
				}
				return false
			},
		},
		"rclone": {
			Name:         "rclone",
			Binary:       "rclone",
			IsAptPackage: true,
			AptPackage:   "rclone",
			Port:         5572,
			HealthProbe: func(s *Server) bool {
				client := http.Client{Timeout: 2 * time.Second}
				resp, err := client.Get("http://127.0.0.1:5572/")
				if err == nil {
					_ = resp.Body.Close()
					return true
				}
				return false
			},
		},
	}
)

func parseVersionParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if idx := strings.Index(v, "+"); idx != -1 {
		v = v[:idx]
	}
	if idx := strings.Index(v, "-"); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	res := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		res = append(res, n)
	}
	return res
}

func compareVersions(v1, v2 string) int {
	p1 := parseVersionParts(v1)
	p2 := parseVersionParts(v2)
	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}
	for i := 0; i < maxLen; i++ {
		val1, val2 := 0, 0
		if i < len(p1) {
			val1 = p1[i]
		}
		if i < len(p2) {
			val2 = p2[i]
		}
		if val1 < val2 {
			return -1
		}
		if val1 > val2 {
			return 1
		}
	}
	return 0
}

func isNewerVersion(current, latest string) bool {
	if current == "" || latest == "" {
		return false
	}
	return compareVersions(current, latest) < 0
}

func versionBase(version string) string {
	re := regexp.MustCompile(`(?i)^v?[0-9]+(?:\.[0-9]+){1,3}`)
	return re.FindString(strings.TrimSpace(version))
}

func (s *Server) readServiceBuildInfo(name string) *serviceBuildInfo {
	manifestPath := filepath.Join(s.data, name, "build-info.json")
	if raw, err := os.ReadFile(manifestPath); err == nil {
		var info serviceBuildInfo
		if json.Unmarshal(raw, &info) == nil && strings.TrimSpace(info.Product) != "" && strings.TrimSpace(info.CustomVersion) != "" {
			manifestService := strings.TrimSpace(info.Service)
			if manifestService == "" || strings.EqualFold(manifestService, name) {
				return &info
			}
		}
	}
	return nil
}

func (s *Server) detectVersionInfo(name string) (current, base string, custom bool, buildInfo *serviceBuildInfo) {
	current = s.detectCurrentVersion(name)
	if info := s.readServiceBuildInfo(name); info != nil {
		current = strings.TrimSpace(info.CustomVersion)
		base = strings.TrimSpace(info.UpstreamVersion)
		if base == "" {
			base = versionBase(current)
		}
		return current, base, true, info
	}

	base = versionBase(current)
	custom = base != "" && !strings.EqualFold(strings.TrimPrefix(current, "v"), strings.TrimPrefix(base, "v"))
	if base == "" {
		base = current
	}
	return current, base, custom, nil
}

func resolveUpdateTracking(spec serviceUpdaterSpec, supported, custom bool, buildInfo *serviceBuildInfo) (upstreamRepo string, tracked, detectionOnly bool) {
	upstreamRepo = spec.GitHubRepo
	if buildInfo != nil && strings.TrimSpace(buildInfo.UpstreamRepository) != "" {
		upstreamRepo = strings.TrimSpace(buildInfo.UpstreamRepository)
	}
	tracked = supported || upstreamRepo != ""
	detectionOnly = spec.DetectionOnly || custom
	return upstreamRepo, tracked, detectionOnly
}

func (s *Server) detectCurrentVersion(name string) string {
	switch name {
	case "openlist":
		out := s.runCmd(filepath.Join(s.prefix, "bin", "openlist"), "version")
		re := regexp.MustCompile(`(?i)(?:Version|WebVersion):\s*(v[0-9.]+)`)
		if m := re.FindStringSubmatch(out); len(m) >= 2 {
			return m[1]
		}
	case "mihomo":
		out := s.runCmd(filepath.Join(s.prefix, "bin", "mihomo"), "-v")
		re := regexp.MustCompile(`(?i)Meta\s+(v[0-9.]+)`)
		if m := re.FindStringSubmatch(out); len(m) >= 2 {
			return m[1]
		}
	case "qbittorrent":
		out := s.runCmd(filepath.Join(s.prefix, "bin", "qbittorrent-nox"), "-v")
		re := regexp.MustCompile(`(?i)qBittorrent\s+(v?[0-9.]+)`)
		if m := re.FindStringSubmatch(out); len(m) >= 2 {
			return m[1]
		}
	case "rclone":
		out := s.runCmd(filepath.Join(s.prefix, "bin", "rclone"), "version")
		re := regexp.MustCompile(`(?i)rclone\s+(v[0-9.]+)`)
		if m := re.FindStringSubmatch(out); len(m) >= 2 {
			return m[1]
		}
	case "cpa":
		// CPA prints build information before flag validation; --version makes it
		// exit immediately instead of accidentally starting a second server.
		out := s.runCmd(filepath.Join(s.prefix, "bin", "cli-proxy-api"), "--version")
		re := regexp.MustCompile(`CLIProxyAPI Version:\s*([^\s,]+)`)
		if m := re.FindStringSubmatch(out); len(m) >= 2 {
			return m[1]
		}
	case "cpa-usage-keeper":
		out := s.runCmd(filepath.Join(s.prefix, "bin", "cpa-usage-keeper"), "--version")
		return strings.TrimSpace(out)
	case "aria2":
		out := commandText(filepath.Join(s.prefix, "bin", "aria2c"), "-v")
		re := regexp.MustCompile(`aria2 version\s+([0-9.]+)`)
		if m := re.FindStringSubmatch(out); len(m) >= 2 {
			return m[1]
		}
	case "sshd":
		out := commandText(filepath.Join(s.prefix, "bin", "sshd"), "-V")
		re := regexp.MustCompile(`OpenSSH_([^\s,]+)`)
		if m := re.FindStringSubmatch(out); len(m) >= 2 {
			return m[1]
		}
	case "caddy":
		out := commandText(filepath.Join(s.prefix, "bin", "dpkg-query"), "-W", "-f=${Version}", "caddy")
		return strings.TrimSpace(out)
	}
	return ""
}

func getUpdateHTTPClient(timeout time.Duration) *http.Client {
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	for _, certPath := range []string{
		"/data/data/com.termux/files/usr/etc/tls/cert.pem",
		"/data/data/com.termux/files/usr/etc/ssl/cert.pem",
		"/system/etc/security/cacerts",
	} {
		if certBytes, err := os.ReadFile(certPath); err == nil {
			rootCAs.AppendCertsFromPEM(certBytes)
		}
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: rootCAs,
		},
		Proxy: func(req *http.Request) (*url.URL, error) {
			if p, err := http.ProxyFromEnvironment(req); err == nil && p != nil {
				return p, nil
			}
			if strings.Contains(req.URL.Host, "github") {
				for _, port := range []int{17890, 7890} {
					conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
					if err == nil {
						_ = conn.Close()
						return url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
					}
				}
			}
			return nil, nil
		},
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func fetchGitHubLatestRelease(repo string) (string, error) {
	client := getUpdateHTTPClient(10 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "OnePlus-Router-Admin")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return strings.TrimSpace(data.TagName), nil
}

func (s *Server) fetchAptCandidate(pkg string) (string, error) {
	out := commandText(filepath.Join(s.prefix, "bin", "apt-cache"), "policy", pkg)
	re := regexp.MustCompile(`(?m)^\s*Candidate:\s*([^\s]+)`)
	m := re.FindStringSubmatch(out)
	if len(m) >= 2 && m[1] != "(none)" && m[1] != "" {
		return m[1], nil
	}
	return "", fmt.Errorf("no apt candidate for %s", pkg)
}

func (s *Server) getBackupInfo(name string) (bool, string, string) {
	backupDir := filepath.Join(s.data, "backups", "services", name)
	lastBackupPath := filepath.Join(backupDir, "last-backup")
	targetDir := strings.TrimSpace(readText(lastBackupPath))
	if targetDir == "" {
		return false, "", ""
	}
	metaBytes, err := os.ReadFile(filepath.Join(targetDir, "meta.json"))
	if err != nil {
		return false, "", ""
	}
	var meta serviceBackupMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return false, "", ""
	}
	if _, err := os.Stat(meta.Binary); err != nil {
		return false, "", ""
	}
	return true, meta.Version, meta.Timestamp
}

func (s *Server) refreshServiceUpdates(forceRemote bool) {
	serviceUpdatesMu.Lock()
	defer serviceUpdatesMu.Unlock()

	now := time.Now()
	needRemote := forceRemote || serviceUpdatesAt.IsZero() || now.Sub(serviceUpdatesAt) > 30*time.Minute

	for name, meta := range allowedServices {
		cur, base, custom, buildInfo := s.detectVersionInfo(name)
		hasBak, bakVer, bakTime := s.getBackupInfo(name)

		existing, ok := serviceUpdatesCache[name]
		if !ok {
			existing = &ServiceUpdateInfo{
				Name:  name,
				Label: meta.Label,
			}
			serviceUpdatesCache[name] = existing
		}
		existing.CurrentVersion = cur
		existing.BaseVersion = base
		existing.CustomBuild = custom
		existing.HasBackup = hasBak
		existing.BackupVersion = bakVer
		existing.BackupTime = bakTime

		spec, supported := supportedServiceUpdaters[name]
		upstreamRepo, tracked, detectionOnly := resolveUpdateTracking(spec, supported, custom, buildInfo)
		existing.CanUpgrade = tracked && !detectionOnly
		if tracked && detectionOnly {
			existing.UpgradeHint = "检测到上游新版本后，需要重新构建并部署此设备的定制版本"
		} else {
			existing.UpgradeHint = ""
		}

		if tracked && needRemote {
			var latest string
			var err error
			if supported && spec.IsAptPackage {
				latest, err = s.fetchAptCandidate(spec.AptPackage)
			} else if upstreamRepo != "" {
				latest, err = fetchGitHubLatestRelease(upstreamRepo)
			}
			if err == nil && latest != "" {
				existing.LatestVersion = latest
				existing.Upgradable = isNewerVersion(base, latest)
				existing.LastChecked = now.Format(time.RFC3339)
				existing.Error = ""
			} else if err != nil {
				existing.Error = err.Error()
			}
		} else if tracked && existing.LatestVersion != "" {
			existing.Upgradable = isNewerVersion(base, existing.LatestVersion)
		}
	}
	if needRemote {
		serviceUpdatesAt = now
	}
}

func (s *Server) initServiceUpdates() {
	go func() {
		// Run initial update detection in background after a brief delay
		time.Sleep(3 * time.Second)
		s.refreshServiceUpdates(true)

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			s.refreshServiceUpdates(true)
		}
	}()
}

func (s *Server) enrichServiceUpdates(services []ServiceInfo) {
	serviceUpdatesMu.RLock()
	defer serviceUpdatesMu.RUnlock()

	for i := range services {
		name := services[i].Name
		if info, ok := serviceUpdatesCache[name]; ok {
			services[i].CurrentVersion = info.CurrentVersion
			services[i].BaseVersion = info.BaseVersion
			services[i].CustomBuild = info.CustomBuild
			services[i].LatestVersion = info.LatestVersion
			services[i].Upgradable = info.Upgradable
			services[i].HasBackup = info.HasBackup
			services[i].CanUpgrade = info.CanUpgrade
			services[i].UpgradeHint = info.UpgradeHint
		}
	}
}

func (s *Server) servicesUpdatesAPI(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	force := r.URL.Query().Get("refresh") == "1"
	s.refreshServiceUpdates(force)

	serviceUpdatesMu.RLock()
	defer serviceUpdatesMu.RUnlock()

	resp := make([]ServiceUpdateInfo, 0, len(serviceUpdatesCache))
	for _, info := range serviceUpdatesCache {
		resp = append(resp, *info)
	}
	jsonOut(w, http.StatusOK, resp)
}

func downloadToFile(rawURL, destPath string) error {
	urlsToTry := []string{rawURL}
	if strings.Contains(rawURL, "github.com") {
		urlsToTry = append(urlsToTry, "https://ghproxy.net/"+rawURL)
	}

	var lastErr error
	for _, tryURL := range urlsToTry {
		err := doDownload(tryURL, destPath)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func doDownload(urlStr, destPath string) error {
	client := getUpdateHTTPClient(180 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "OnePlus-Router-Admin")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP status %d", resp.StatusCode)
	}

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func untarFile(tarGzPath, targetBinaryName, destPath string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg && (header.Name == targetBinaryName || filepath.Base(header.Name) == targetBinaryName) {
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			out.Close()
			return copyErr
		}
	}
	return fmt.Errorf("binary %s not found in archive", targetBinaryName)
}

func ungunzipFile(gzPath, destPath string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, copyErr := io.Copy(out, gzr)
	return copyErr
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func (s *Server) saveServiceBackup(name, version, liveBinaryPath string) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(s.data, "backups", "services", name, stamp)
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return "", err
	}
	backupBinary := filepath.Join(backupDir, filepath.Base(liveBinaryPath))
	if err := copyFile(liveBinaryPath, backupBinary, 0755); err != nil {
		return "", fmt.Errorf("copy live binary to backup failed: %w", err)
	}
	meta := serviceBackupMeta{
		Service:   name,
		Version:   version,
		Binary:    backupBinary,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(backupDir, "meta.json"), metaBytes, 0600); err != nil {
		return "", err
	}
	lastBackupPointer := filepath.Join(s.data, "backups", "services", name, "last-backup")
	if err := os.WriteFile(lastBackupPointer, []byte(backupDir), 0600); err != nil {
		return "", err
	}
	return backupDir, nil
}

func (s *Server) performRollback(name string) (string, error) {
	spec, ok := supportedServiceUpdaters[name]
	if !ok {
		return "", fmt.Errorf("服务 %s 不支持自动回滚", name)
	}

	backupPointer := filepath.Join(s.data, "backups", "services", name, "last-backup")
	targetBackupDir := strings.TrimSpace(readText(backupPointer))
	if targetBackupDir == "" {
		return "", fmt.Errorf("未找到服务 %s 的备份快照", name)
	}

	metaBytes, err := os.ReadFile(filepath.Join(targetBackupDir, "meta.json"))
	if err != nil {
		return "", fmt.Errorf("读取备份元数据失败: %w", err)
	}
	var meta serviceBackupMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return "", fmt.Errorf("解析备份元数据失败: %w", err)
	}

	liveBinaryPath := filepath.Join(s.prefix, "bin", spec.Binary)
	if _, err := os.Stat(meta.Binary); err != nil {
		return "", fmt.Errorf("备份二进制文件不存在: %s", meta.Binary)
	}

	// 1. Stop service
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "down", filepath.Join(s.stack, "services", name)).Run()
	time.Sleep(1 * time.Second)

	// 2. Restore binary atomically
	restoredTemp := liveBinaryPath + ".restoring"
	if err := copyFile(meta.Binary, restoredTemp, 0755); err != nil {
		return "", fmt.Errorf("还原备份二进制失败: %w", err)
	}
	if err := os.Rename(restoredTemp, liveBinaryPath); err != nil {
		return "", fmt.Errorf("替换恢复二进制失败: %w", err)
	}

	// 3. Start service
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "up", filepath.Join(s.stack, "services", name)).Run()

	// 4. Verify health
	healthy := false
	for attempt := 0; attempt < 8; attempt++ {
		time.Sleep(1 * time.Second)
		if spec.HealthProbe != nil {
			if spec.HealthProbe(s) {
				healthy = true
				break
			}
		} else {
			statusText := commandText(filepath.Join(s.prefix, "bin", "sv"), "status", filepath.Join(s.stack, "services", name))
			if strings.HasPrefix(statusText, "run:") {
				healthy = true
				break
			}
		}
	}

	if !healthy {
		return meta.Version, fmt.Errorf("回滚后服务启动异常，请检查服务日志")
	}

	s.refreshServiceUpdates(false)
	return meta.Version, nil
}

func (s *Server) servicesUpgradeAPI(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var payload struct {
		Service string `json:"service"`
	}
	if err := decodeJSON(r, &payload); err != nil || payload.Service == "" {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "无效请求参数"})
		return
	}

	serviceName := payload.Service
	spec, ok := supportedServiceUpdaters[serviceName]
	if !ok {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("服务 %s 不支持一键升级", serviceName)})
		return
	}
	if spec.DetectionOnly || s.readServiceBuildInfo(serviceName) != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "此服务使用设备定制构建，请在项目中重新构建并部署，不能直接安装上游二进制"})
		return
	}

	serviceUpdatesMu.Lock()
	if serviceUpgrading[serviceName] {
		serviceUpdatesMu.Unlock()
		jsonOut(w, http.StatusConflict, map[string]string{"error": "该服务正在升级中，请稍候"})
		return
	}
	serviceUpgrading[serviceName] = true
	serviceUpdatesMu.Unlock()

	defer func() {
		serviceUpdatesMu.Lock()
		delete(serviceUpgrading, serviceName)
		serviceUpdatesMu.Unlock()
	}()

	currentVer := s.detectCurrentVersion(serviceName)
	liveBinary := filepath.Join(s.prefix, "bin", spec.Binary)
	stamp := time.Now().Format("20060102-150405")
	workDir := filepath.Join(s.data, ".updates", fmt.Sprintf("%s-%s", serviceName, stamp))
	if err := os.MkdirAll(workDir, 0700); err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "创建临时工作目录失败: " + err.Error()})
		return
	}
	defer os.RemoveAll(workDir)

	var targetVer string
	candidatePath := filepath.Join(workDir, spec.Binary)

	// Step 1: Download or fetch candidate
	if spec.IsAptPackage {
		candidateVer, err := s.fetchAptCandidate(spec.AptPackage)
		if err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "获取可升级 APT 版本失败: " + err.Error()})
			return
		}
		targetVer = candidateVer

		// Backup current binary before APT upgrade
		backupDir, err := s.saveServiceBackup(serviceName, currentVer, liveBinary)
		if err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "创建回滚备份失败: " + err.Error()})
			return
		}

		// Run apt upgrade
		aptCmd := exec.Command("apt", "--only-upgrade", "install", "-y", spec.AptPackage)
		aptCmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "PREFIX="+s.prefix, "PATH="+s.prefix+"/bin:/system/bin")
		aptOut, aptErr := aptCmd.CombinedOutput()
		if aptErr != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("APT 升级失败: %v (%s)", aptErr, strings.TrimSpace(string(aptOut)))})
			return
		}

		// Restart and health check
		_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "restart", filepath.Join(s.stack, "services", serviceName)).Run()

		healthy := false
		for attempt := 0; attempt < 10; attempt++ {
			time.Sleep(1 * time.Second)
			if spec.HealthProbe != nil && spec.HealthProbe(s) {
				healthy = true
				break
			}
		}

		if !healthy {
			log.Printf("[Upgrade] %s health check failed after apt upgrade! Triggering automatic rollback...", serviceName)
			_, rbErr := s.performRollback(serviceName)
			msg := fmt.Sprintf("新版本启动健康探测超时，已自动回滚至原版本 %s", currentVer)
			if rbErr != nil {
				msg += fmt.Sprintf(" (回滚时遇到异常: %v)", rbErr)
			}
			jsonOut(w, http.StatusInternalServerError, map[string]any{"ok": false, "rolledBack": true, "error": msg})
			return
		}

		s.refreshServiceUpdates(false)
		jsonOut(w, http.StatusOK, map[string]any{
			"ok":        true,
			"service":   serviceName,
			"version":   targetVer,
			"backupDir": backupDir,
			"message":   fmt.Sprintf("%s 成功升级至 %s", allowedServices[serviceName].Label, targetVer),
		})
		return
	}

	// GitHub Binary Release Workflow (openlist, mihomo)
	latestTag, err := fetchGitHubLatestRelease(spec.GitHubRepo)
	if err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "获取 GitHub 最新版本失败: " + err.Error()})
		return
	}
	targetVer = latestTag

	var downloadURL string
	switch serviceName {
	case "openlist":
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/openlist-linux-musl-arm64.tar.gz", spec.GitHubRepo, targetVer)
		tarPath := filepath.Join(workDir, "openlist.tar.gz")
		if err := downloadToFile(downloadURL, tarPath); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "下载 OpenList 升级包失败: " + err.Error()})
			return
		}
		if err := untarFile(tarPath, spec.ExtractBinary, candidatePath); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "解压 OpenList 升级包失败: " + err.Error()})
			return
		}
	case "mihomo":
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/mihomo-android-arm64-v8-%s.gz", spec.GitHubRepo, targetVer, targetVer)
		gzPath := filepath.Join(workDir, "mihomo.gz")
		if err := downloadToFile(downloadURL, gzPath); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "下载 Mihomo 升级包失败: " + err.Error()})
			return
		}
		if err := ungunzipFile(gzPath, candidatePath); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "解压 Mihomo 升级包失败: " + err.Error()})
			return
		}
	default:
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "未知升级通道"})
		return
	}

	if err := os.Chmod(candidatePath, 0755); err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "设置新二进制权限失败: " + err.Error()})
		return
	}

	// Step 2: Preflight check before touching current service
	if spec.PreflightTest != nil {
		if err := spec.PreflightTest(s, candidatePath); err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "新版本预检拦截: " + err.Error()})
			return
		}
	}

	// Step 3: Atomic Backup of current working binary
	backupDir, err := s.saveServiceBackup(serviceName, currentVer, liveBinary)
	if err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "创建回滚备份失败: " + err.Error()})
		return
	}

	// Step 4: Gracefully stop and atomically swap binary
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "down", filepath.Join(s.stack, "services", serviceName)).Run()
	time.Sleep(1 * time.Second)

	stagedNew := liveBinary + ".new"
	if err := copyFile(candidatePath, stagedNew, 0755); err != nil {
		_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "up", filepath.Join(s.stack, "services", serviceName)).Run()
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "部署新二进制失败: " + err.Error()})
		return
	}
	if err := os.Rename(stagedNew, liveBinary); err != nil {
		_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "up", filepath.Join(s.stack, "services", serviceName)).Run()
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "原子替换二进制失败: " + err.Error()})
		return
	}

	// Step 5: Start service
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "up", filepath.Join(s.stack, "services", serviceName)).Run()

	// Step 6: Post-Swap Continuous Health Check
	healthy := false
	for attempt := 0; attempt < 12; attempt++ {
		time.Sleep(1 * time.Second)
		if spec.HealthProbe != nil {
			if spec.HealthProbe(s) {
				healthy = true
				break
			}
		} else {
			statusText := commandText(filepath.Join(s.prefix, "bin", "sv"), "status", filepath.Join(s.stack, "services", serviceName))
			if strings.HasPrefix(statusText, "run:") {
				healthy = true
				break
			}
		}
	}

	// Step 7: Automatic rollback if health check fails!
	if !healthy {
		log.Printf("[Upgrade] %s health check failed! Executing automatic rollback to %s...", serviceName, currentVer)
		_, rbErr := s.performRollback(serviceName)
		msg := fmt.Sprintf("新版本启动健康探测超时，系统已自动回滚至原版本 %s", currentVer)
		if rbErr != nil {
			msg += fmt.Sprintf(" (自动回滚异常: %v)", rbErr)
		}
		jsonOut(w, http.StatusInternalServerError, map[string]any{
			"ok":         false,
			"rolledBack": true,
			"error":      msg,
		})
		return
	}

	s.refreshServiceUpdates(false)
	jsonOut(w, http.StatusOK, map[string]any{
		"ok":        true,
		"service":   serviceName,
		"version":   targetVer,
		"backupDir": backupDir,
		"message":   fmt.Sprintf("%s 成功升级至 %s 并通过健康校验", allowedServices[serviceName].Label, targetVer),
	})
}

func (s *Server) servicesRollbackAPI(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var payload struct {
		Service string `json:"service"`
	}
	if err := decodeJSON(r, &payload); err != nil || payload.Service == "" {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "无效请求参数"})
		return
	}

	serviceName := payload.Service
	restoredVer, err := s.performRollback(serviceName)
	if err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "回滚失败: " + err.Error()})
		return
	}

	jsonOut(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": serviceName,
		"version": restoredVer,
		"message": fmt.Sprintf("%s 已成功回滚至原版本 %s", allowedServices[serviceName].Label, restoredVer),
	})
}
