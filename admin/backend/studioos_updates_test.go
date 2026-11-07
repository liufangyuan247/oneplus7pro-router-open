package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func studioOSFixture(t *testing.T, state, target string, checked time.Time) *Server {
	t.Helper()
	s := &Server{home: t.TempDir()}
	app := filepath.Join(s.home, "router-apps", "studioos")
	for name, value := range map[string]any{
		"current/release.json": map[string]string{"release_id": "20261107T031636Z-703f36d5", "version": "v0.3.0"},
		"updates/status.json": map[string]string{
			"state": state, "release_id": target, "checked_at": checked.Format(time.RFC3339Nano),
		},
	} {
		path := filepath.Join(app, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestStudioOSPendingUpdatesUseReleaseIdentity(t *testing.T) {
	for _, state := range []string{"DEFERRED_BUSY", "DEFERRED_MAINTENANCE", "UPDATING", "RUNTIME_UPDATE_REQUIRED", "FAILED_RELEASE_BLOCKED", "ROLLED_BACK"} {
		t.Run(state, func(t *testing.T) {
			s := studioOSFixture(t, state, "20261108T031636Z-aabbccdd", time.Now())
			info := s.readStudioOSUpdateInfo()
			if !info.Upgradable || info.CanUpgrade || info.LatestBuild != "20261108T031636Z-aabbccdd" || info.VersionScheme != "deployment" {
				t.Fatalf("pending update not represented correctly: %#v", info)
			}
			if info.Upgrading != (state == "UPDATING") || info.UpdateState != state {
				t.Fatalf("native updater state lost: %#v", info)
			}
		})
	}
	for _, target := range []string{"20261107T031636Z-703f36d5", "20261106T031636Z-aabbccdd", "20261308T031636Z-aabbccdd", "not-a-release"} {
		s := studioOSFixture(t, "DEFERRED_BUSY", target, time.Now())
		info := s.readStudioOSUpdateInfo()
		if info.Upgradable || info.LatestVersion != "" || info.UpdateState != "UNKNOWN" {
			t.Fatalf("invalid/installed/older release shown as an update: %#v", info)
		}
	}
}

func TestStudioOSUncertainChecksDoNotClaimLatest(t *testing.T) {
	for _, state := range []string{"CHANNEL_AUTH_REQUIRED", "CHECK_FAILED", "NO_RELEASE_AVAILABLE", "RELEASE_INCOMPLETE", "DISABLED", "FUTURE_STATE"} {
		s := studioOSFixture(t, state, "", time.Now())
		info := s.readStudioOSUpdateInfo()
		if info.Upgradable || info.LatestVersion != "" || info.UpdateState == "UP_TO_DATE" || info.CurrentVersion == "" {
			t.Fatalf("uncertain check reported success or lost installed version: %#v", info)
		}
	}
	for _, checked := range []time.Time{time.Now().Add(-time.Hour), time.Now().Add(time.Hour)} {
		s := studioOSFixture(t, "UP_TO_DATE", "", checked)
		info := s.readStudioOSUpdateInfo()
		if info.UpdateState == "UP_TO_DATE" || info.LatestVersion != "" {
			t.Fatalf("stale/future check claims latest: %#v", info)
		}
	}
}

func TestStudioOSSnapshotTracksIndependentReleaseSwitch(t *testing.T) {
	s := studioOSFixture(t, "UP_TO_DATE", "", time.Now())
	first := []ServiceInfo{{Name: "studioos"}}
	s.enrichServiceUpdates(first)
	if first[0].CurrentVersion != "v0.3.0" || first[0].CurrentBuild != "20261107T031636Z-703f36d5" || first[0].UpdateState != "UP_TO_DATE" {
		t.Fatalf("initial snapshot missing version/state: %#v", first)
	}
	path := filepath.Join(s.home, "router-apps", "studioos", "current", "release.json")
	if err := os.WriteFile(path, []byte(`{"release_id":"20261108T031636Z-aabbccdd","version":"v0.4.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	next := []ServiceInfo{{Name: "studioos"}}
	s.enrichServiceUpdates(next)
	if next[0].CurrentVersion != "v0.4.0" || next[0].CurrentBuild != "20261108T031636Z-aabbccdd" {
		t.Fatalf("snapshot retained the generic hourly cache: %#v", next)
	}
	if err := os.WriteFile(path, []byte(`broken`), 0600); err != nil {
		t.Fatal(err)
	}
	info := s.readStudioOSUpdateInfo()
	if info.CurrentVersion != "" || info.Upgradable || info.UpdateState != "UNKNOWN" {
		t.Fatalf("invalid manifest retained an authoritative version: %#v", info)
	}
}
