package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var studioOSReleaseID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{8}$`)

func studioOSReleaseTime(id string) (time.Time, bool) {
	if !studioOSReleaseID.MatchString(id) {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T150405Z", id[:16])
	return t, err == nil
}

// Read the existing updater's public metadata only. Never invoke the updater:
// its --once mode can install a release, so it is not a read-only check.
func (s *Server) readStudioOSUpdateInfo() ServiceUpdateInfo {
	info := ServiceUpdateInfo{
		Name: "studioos", Label: allowedServices["studioos"].Label,
		VersionScheme: "deployment", UpdateState: "UNKNOWN",
		UpdateMessage: "更新状态未知",
		UpgradeHint:   "由 StudioOS 自动更新器校验并安装，忙碌时等待空闲后更新",
	}
	app := filepath.Join(s.home, "router-apps", "studioos")
	var current struct {
		ReleaseID string `json:"release_id"`
		Version   string `json:"version"`
	}
	raw, err := os.ReadFile(filepath.Join(app, "current", "release.json"))
	if err != nil || json.Unmarshal(raw, &current) != nil {
		info.UpdateMessage = "无法读取部署版本"
		return info
	}
	currentTime, valid := studioOSReleaseTime(current.ReleaseID)
	if !valid {
		info.UpdateMessage = "部署版本记录无效"
		return info
	}
	info.CurrentBuild = current.ReleaseID
	info.CurrentVersion = studioOSManifestVersion(app, raw)
	if info.CurrentVersion == "" {
		s.queueStudioOSVersionSync(app)
	}
	var status struct {
		State     string `json:"state"`
		CheckedAt string `json:"checked_at"`
		ReleaseID string `json:"release_id"`
	}
	raw, err = os.ReadFile(filepath.Join(app, "updates", "status.json"))
	if err != nil || json.Unmarshal(raw, &status) != nil {
		info.UpdateMessage = "等待更新器检查"
		return info
	}
	checked, err := time.Parse(time.RFC3339Nano, status.CheckedAt)
	if err != nil || checked.After(time.Now().Add(5*time.Minute)) {
		info.UpdateMessage = "更新检查记录无效"
		return info
	}
	info.LastChecked = status.CheckedAt
	if time.Since(checked) > 15*time.Minute {
		info.UpdateState, info.UpdateMessage = "STALE", "更新状态已过期"
		return info
	}
	labels := map[string]string{
		"UP_TO_DATE":               "已是最新版本",
		"UPDATED":                  "已更新",
		"RECOVERED_UPDATED":        "已恢复更新",
		"DEFERRED_BUSY":            "发现新版 · 等待空闲",
		"DEFERRED_MAINTENANCE":     "发现新版 · 等待维护完成",
		"UPDATING":                 "正在更新",
		"RUNTIME_UPDATE_REQUIRED":  "发现新版 · 需更新运行环境",
		"FAILED_RELEASE_BLOCKED":   "新版安装失败 · 等待处理",
		"ROLLED_BACK":              "更新失败 · 已回退",
		"RECOVERED_ROLLBACK":       "已恢复至旧版",
		"ROLLBACK_NEEDS_ATTENTION": "更新回退需处理",
		"NO_RELEASE_AVAILABLE":     "暂无正式发布",
		"NO_MATCHING_RELEASE":      "暂无匹配的正式发布",
		"RELEASE_INCOMPLETE":       "新版发布文件不完整",
		"CHANNEL_AUTH_REQUIRED":    "更新源未授权",
		"CHECK_FAILED":             "更新检查失败",
		"DISABLED":                 "自动更新已关闭",
	}
	message, known := labels[status.State]
	if !known {
		return info
	}
	info.UpdateState, info.UpdateMessage = status.State, message
	switch status.State {
	case "UP_TO_DATE":
		info.LatestVersion = info.CurrentVersion
		info.LatestBuild = info.CurrentBuild
	case "DEFERRED_BUSY", "DEFERRED_MAINTENANCE", "UPDATING", "RUNTIME_UPDATE_REQUIRED",
		"FAILED_RELEASE_BLOCKED", "ROLLED_BACK", "RECOVERED_ROLLBACK", "ROLLBACK_NEEDS_ATTENTION":
		candidateTime, valid := studioOSReleaseTime(status.ReleaseID)
		if valid && candidateTime.After(currentTime) {
			info.LatestBuild = status.ReleaseID
			for _, path := range []string{
				filepath.Join(app, "updates", "downloads", status.ReleaseID, "release.json"),
				filepath.Join(app, "updates", "incoming", "release.json"),
			} {
				candidate, err := os.ReadFile(path)
				var identity struct {
					ReleaseID string `json:"release_id"`
				}
				if err == nil && json.Unmarshal(candidate, &identity) == nil && identity.ReleaseID == status.ReleaseID {
					info.LatestVersion = studioOSManifestVersion(app, candidate)
					break
				}
			}
			if info.LatestVersion == "" {
				s.queueStudioOSVersionSync(app)
			}
			info.Upgradable = true
		} else if status.State == "DEFERRED_BUSY" || status.State == "DEFERRED_MAINTENANCE" || status.State == "RUNTIME_UPDATE_REQUIRED" {
			// A status left by a previous release is not evidence of a new update.
			info.UpdateState, info.UpdateMessage = "UNKNOWN", "等待更新器重新检查"
		}
	}
	info.Upgrading = status.State == "UPDATING"
	return info
}
