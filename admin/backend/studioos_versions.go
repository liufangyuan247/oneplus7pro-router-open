package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var studioOSSemanticVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var studioOSRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var studioOSVersionSync struct {
	sync.Mutex
	next time.Time
}

type studioOSPublishedVersion struct {
	Version        string `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

func studioOSVersion(value string) string {
	value = strings.TrimPrefix(value, "studioos-")
	if !studioOSSemanticVersion.MatchString(value) {
		return ""
	}
	return "v" + strings.TrimPrefix(value, "v")
}

func studioOSManifestVersion(app string, raw []byte) string {
	var manifest struct {
		ReleaseID string `json:"release_id"`
		Version   string `json:"version"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return ""
	}
	if version := studioOSVersion(manifest.Version); version != "" {
		return version
	}
	var versions map[string]studioOSPublishedVersion
	cache, err := os.ReadFile(filepath.Join(app, "updates", "admin-release-versions.json"))
	if err != nil || json.Unmarshal(cache, &versions) != nil {
		return ""
	}
	record := versions[manifest.ReleaseID]
	sum := sha256.Sum256(raw)
	if record.ManifestSHA256 != hex.EncodeToString(sum[:]) {
		return ""
	}
	return studioOSVersion(record.Version)
}

// Resolve a release tag through its source manifest hash, never through latest
// tag alone: a newer published release need not be the installed application.
func studioOSPublishedVersionMetadata(repository string, fetch func(string, string) ([]byte, error)) (string, studioOSPublishedVersion, error) {
	var record studioOSPublishedVersion
	base := "https://api.github.com/repos/" + repository
	raw, err := fetch(base+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return "", record, err
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if json.Unmarshal(raw, &release) != nil || release.Draft || release.Prerelease || !strings.HasPrefix(release.Tag, "studioos-v") {
		return "", record, fmt.Errorf("not a stable StudioOS release")
	}
	record.Version = studioOSVersion(release.Tag)
	if record.Version == "" {
		return "", record, fmt.Errorf("invalid version")
	}
	for _, asset := range release.Assets {
		if asset.Name != "update.json" {
			continue
		}
		if !regexp.MustCompile(`^` + regexp.QuoteMeta(base) + `/releases/assets/[0-9]+$`).MatchString(asset.URL) {
			return "", record, fmt.Errorf("invalid asset URL")
		}
		raw, err = fetch(asset.URL, "application/octet-stream")
		if err != nil {
			return "", record, err
		}
		var descriptor struct {
			Spec           string `json:"spec"`
			ReleaseID      string `json:"release_id"`
			ManifestSHA256 string `json:"manifest_sha256"`
		}
		if json.Unmarshal(raw, &descriptor) != nil || descriptor.Spec != "studioos.release/v1" || !studioOSReleaseID.MatchString(descriptor.ReleaseID) {
			return "", record, fmt.Errorf("invalid descriptor")
		}
		digest, err := hex.DecodeString(descriptor.ManifestSHA256)
		if err != nil || len(digest) != sha256.Size {
			return "", record, fmt.Errorf("invalid manifest digest")
		}
		record.ManifestSHA256 = strings.ToLower(descriptor.ManifestSHA256)
		return descriptor.ReleaseID, record, nil
	}
	return "", record, fmt.Errorf("missing update descriptor")
}

func (s *Server) queueStudioOSVersionSync(app string) {
	if _, err := os.Stat(filepath.Join(app, "updates", "config.json")); err != nil {
		return
	}
	studioOSVersionSync.Lock()
	if time.Now().Before(studioOSVersionSync.next) {
		studioOSVersionSync.Unlock()
		return
	}
	studioOSVersionSync.next = time.Now().Add(5 * time.Minute)
	studioOSVersionSync.Unlock()
	go func() { _ = s.syncStudioOSVersions(app) }()
}

// The existing private read-only Contents credential stays on the router. Only
// release metadata is fetched; no updater, downloads or application actions run.
func (s *Server) syncStudioOSVersions(app string) error {
	var config struct {
		Repository string `json:"github_repository"`
	}
	raw, err := os.ReadFile(filepath.Join(app, "updates", "config.json"))
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, &config) != nil || !studioOSRepository.MatchString(config.Repository) {
		return fmt.Errorf("invalid repository configuration")
	}
	token := ""
	tokenPath := filepath.Join(app, "config", "github-release-token")
	if stat, err := os.Stat(tokenPath); err == nil {
		if stat.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("release credential must be private")
		}
		raw, err := os.ReadFile(tokenPath)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(raw))
	} else if !os.IsNotExist(err) {
		return err
	}
	client := getUpdateHTTPClient(10 * time.Second)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" {
			return fmt.Errorf("invalid release redirect")
		}
		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}
	fetch := func(url, accept string) ([]byte, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", "OnePlus-Router-Admin")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("release metadata HTTP %d", resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	}
	id, record, err := studioOSPublishedVersionMetadata(config.Repository, fetch)
	if err != nil {
		return err
	}
	cachePath := filepath.Join(app, "updates", "admin-release-versions.json")
	versions := map[string]studioOSPublishedVersion{}
	if raw, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(raw, &versions)
	}
	if versions == nil {
		versions = map[string]studioOSPublishedVersion{}
	}
	versions[id] = record
	raw, err = json.Marshal(versions)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(cachePath), ".admin-versions-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), cachePath)
}
