package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionComparison(t *testing.T) {
	cases := []struct {
		v1       string
		v2       string
		expected int
		newer    bool
	}{
		{"v4.2.5", "v4.2.6", -1, true},
		{"v4.2.6", "v4.2.5", 1, false},
		{"v4.2.5", "v4.2.5", 0, false},
		{"v1.19.30", "v1.19.31", -1, true},
		{"1.19.31", "1.19.30", 1, false},
		{"5.2.3-2", "5.2.4", -1, true},
		{"5.2.4", "5.2.3", 1, false},
		{"v1.102.3", "v1.102.5", -1, true},
		{"v7.2.140-op7", "v8.0.4", -1, true},
		{"", "v1.0.0", -1, false},
		{"v1.0.0", "", 1, false},
	}

	for _, c := range cases {
		cmp := compareVersions(c.v1, c.v2)
		if (cmp < 0 && c.expected >= 0) || (cmp > 0 && c.expected <= 0) || (cmp == 0 && c.expected != 0) {
			t.Errorf("compareVersions(%q, %q) = %d, expected sign %d", c.v1, c.v2, cmp, c.expected)
		}
		newer := isNewerVersion(c.v1, c.v2)
		if newer != c.newer {
			t.Errorf("isNewerVersion(%q, %q) = %v, expected %v", c.v1, c.v2, newer, c.newer)
		}
	}
}

func TestServiceUpdatesAPI(t *testing.T) {
	tempDir := t.TempDir()
	s := &Server{
		prefix:   filepath.Join(tempDir, "usr"),
		home:     filepath.Join(tempDir, "home"),
		stack:    filepath.Join(tempDir, "home", "router-stack"),
		data:     filepath.Join(tempDir, "home", "router-data"),
		password: "test-password",
		sessions: map[string]authSession{
			"valid-token": {Expires: time.Now().Add(1 * time.Hour)},
		},
		commandRunner: func(name string, args ...string) string {
			if strings.Contains(name, "openlist") {
				return "OpenList Version: v4.2.5\n"
			}
			return ""
		},
	}
	_ = os.MkdirAll(filepath.Join(s.prefix, "bin"), 0755)
	_ = os.MkdirAll(filepath.Join(s.data, "backups", "services", "openlist"), 0755)

	// Inject cache
	serviceUpdatesMu.Lock()
	serviceUpdatesCache["openlist"] = &ServiceUpdateInfo{
		Name:           "openlist",
		Label:          "OpenList",
		CurrentVersion: "v4.2.5",
		LatestVersion:  "v4.2.6",
		Upgradable:     true,
		CanUpgrade:     true,
	}
	serviceUpdatesMu.Unlock()

	req := httptest.NewRequest("GET", "/api/services/updates", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()

	s.auth(s.servicesUpdatesAPI)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var updates []ServiceUpdateInfo
	if err := json.Unmarshal(w.Body.Bytes(), &updates); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	found := false
	for _, u := range updates {
		if u.Name == "openlist" {
			found = true
			if u.CurrentVersion != "v4.2.5" || u.LatestVersion != "v4.2.6" || !u.Upgradable {
				t.Errorf("unexpected openlist update info: %+v", u)
			}
		}
	}
	if !found {
		t.Error("openlist not found in updates response")
	}
}

func TestSaveAndPerformRollback(t *testing.T) {
	tempDir := t.TempDir()
	prefixBin := filepath.Join(tempDir, "usr", "bin")
	serviceDir := filepath.Join(tempDir, "home", "router-stack", "services", "openlist")
	dataDir := filepath.Join(tempDir, "home", "router-data")

	_ = os.MkdirAll(prefixBin, 0755)
	_ = os.MkdirAll(serviceDir, 0755)
	_ = os.MkdirAll(dataDir, 0755)

	s := &Server{
		prefix:   filepath.Join(tempDir, "usr"),
		home:     filepath.Join(tempDir, "home"),
		stack:    filepath.Join(tempDir, "home", "router-stack"),
		data:     dataDir,
		password: "test-password",
		sessions: map[string]authSession{
			"valid-token": {Expires: time.Now().Add(1 * time.Hour)},
		},
	}

	liveBinary := filepath.Join(prefixBin, "openlist")
	if err := os.WriteFile(liveBinary, []byte("#!/bin/sh\necho v4.2.5 original\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Save backup of v4.2.5
	backupDir, err := s.saveServiceBackup("openlist", "v4.2.5", liveBinary)
	if err != nil {
		t.Fatalf("saveServiceBackup failed: %v", err)
	}

	hasBak, bakVer, _ := s.getBackupInfo("openlist")
	if !hasBak || bakVer != "v4.2.5" {
		t.Fatalf("getBackupInfo failed: hasBak=%v, bakVer=%s", hasBak, bakVer)
	}

	// 2. Simulate bad update overwriting liveBinary
	if err := os.WriteFile(liveBinary, []byte("#!/bin/sh\necho broken bad binary\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// 3. Override health probe and sv commands for testing
	origSpec := supportedServiceUpdaters["openlist"]
	defer func() { supportedServiceUpdaters["openlist"] = origSpec }()

	customSpec := origSpec
	customSpec.HealthProbe = func(srv *Server) bool {
		// Mock health check passing after rollback
		b, _ := os.ReadFile(liveBinary)
		return strings.Contains(string(b), "v4.2.5 original")
	}
	supportedServiceUpdaters["openlist"] = customSpec

	// Mock sv command
	svBin := filepath.Join(prefixBin, "sv")
	_ = os.WriteFile(svBin, []byte("#!/bin/sh\nexit 0\n"), 0755)

	// 4. Test Rollback
	restoredVer, err := s.performRollback("openlist")
	if err != nil {
		t.Fatalf("performRollback failed: %v", err)
	}
	if restoredVer != "v4.2.5" {
		t.Errorf("expected restored version v4.2.5, got %s", restoredVer)
	}

	// Verify restored content
	restoredContent, _ := os.ReadFile(liveBinary)
	if !strings.Contains(string(restoredContent), "v4.2.5 original") {
		t.Fatalf("live binary does not match original backup after rollback: %s", string(restoredContent))
	}

	_ = backupDir
}
