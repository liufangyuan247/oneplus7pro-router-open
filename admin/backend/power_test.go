package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPowerSupplyVoltage(t *testing.T) {
	tests := []struct {
		name string
		raw  int
		want float64
	}{
		{name: "millivolts from OnePlus USB node", raw: 4475, want: 4.475},
		{name: "microvolts from battery node", raw: 4018000, want: 4.018},
		{name: "microvolts from PD contract", raw: 5000000, want: 5},
		{name: "missing", raw: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := powerSupplyVoltage(tt.raw); got != tt.want {
				t.Fatalf("powerSupplyVoltage(%d) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestReadBatteryGuardStatus(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	path := filepath.Join(dir, "battery-guard.status")

	tests := []struct {
		name        string
		content     string
		wantHealthy bool
		wantEnabled bool
		wantPause   bool
		wantLow     int
		wantHigh    int
	}{
		{
			name:        "fresh, healthy and paused with custom thresholds",
			content:     "enabled=1\npaused=1\nhealthy=1\nlow=35\nhigh=65\nupdated=1799999970\n",
			wantHealthy: true,
			wantEnabled: true,
			wantPause:   true,
			wantLow:     35,
			wantHigh:    65,
		},
		{
			name:        "fresh, healthy and charging",
			content:     "enabled=1\npaused=0\nhealthy=1\nlow=40\nhigh=60\nupdated=1800000000\n",
			wantHealthy: true,
			wantEnabled: true,
			wantPause:   false,
			wantLow:     40,
			wantHigh:    60,
		},
		{
			name:        "explicitly disabled but controller healthy",
			content:     "enabled=0\npaused=0\nhealthy=1\nlow=40\nhigh=60\nupdated=1800000000\n",
			wantHealthy: true,
			wantEnabled: false,
			wantPause:   false,
			wantLow:     40,
			wantHigh:    60,
		},
		{
			name:        "controller unhealthy - does not pretend defaults applied",
			content:     "enabled=1\npaused=1\nhealthy=0\nlow=40\nhigh=60\nupdated=1800000000\n",
			wantHealthy: false,
			wantEnabled: false,
			wantPause:   false,
			wantLow:     0,
			wantHigh:    0,
		},
		{
			name:        "stale heartbeat - does not pretend defaults applied",
			content:     "enabled=1\npaused=1\nhealthy=1\nlow=40\nhigh=60\nupdated=1799999900\n",
			wantHealthy: false,
			wantEnabled: false,
			wantPause:   false,
			wantLow:     0,
			wantHigh:    0,
		},
		{
			name:        "future heartbeat - does not pretend defaults applied",
			content:     "enabled=1\npaused=1\nhealthy=1\nlow=40\nhigh=60\nupdated=1800000040\n",
			wantHealthy: false,
			wantEnabled: false,
			wantPause:   false,
			wantLow:     0,
			wantHigh:    0,
		},
		{
			name:        "invalid heartbeat timestamp",
			content:     "enabled=1\npaused=1\nhealthy=1\nlow=40\nhigh=60\nupdated=invalid\n",
			wantHealthy: false,
			wantEnabled: false,
			wantPause:   false,
			wantLow:     0,
			wantHigh:    0,
		},
		{
			name:        "corrupted thresholds low >= high",
			content:     "enabled=1\npaused=0\nhealthy=1\nlow=60\nhigh=40\nupdated=1800000000\n",
			wantHealthy: false,
			wantEnabled: false,
			wantPause:   false,
			wantLow:     0,
			wantHigh:    0,
		},
		{
			name:        "invalid threshold out of range",
			content:     "enabled=1\npaused=0\nhealthy=1\nlow=10\nhigh=100\nupdated=1800000000\n",
			wantHealthy: false,
			wantEnabled: false,
			wantPause:   false,
			wantLow:     0,
			wantHigh:    0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
				t.Fatal(err)
			}
			st := readBatteryGuardStatus(path, now)
			if st.Healthy != test.wantHealthy || st.Enabled != test.wantEnabled || st.Paused != test.wantPause || st.Low != test.wantLow || st.High != test.wantHigh {
				t.Fatalf("readBatteryGuardStatus() = %+v, want (healthy=%v, enabled=%v, paused=%v, low=%d, high=%d)",
					st, test.wantHealthy, test.wantEnabled, test.wantPause, test.wantLow, test.wantHigh)
			}
		})
	}

	t.Run("missing status file", func(t *testing.T) {
		st := readBatteryGuardStatus(filepath.Join(dir, "non-existent.status"), now)
		if st.Healthy || st.Enabled || st.Paused || st.Low != 0 || st.High != 0 || st.Updated != 0 {
			t.Fatalf("expected zero/unhealthy status for missing file, got %+v", st)
		}
	})
}

func TestBatteryGuardConfigHandler(t *testing.T) {
	dataDir := t.TempDir()
	adminDir := filepath.Join(dataDir, "admin")
	if err := os.MkdirAll(adminDir, 0700); err != nil {
		t.Fatal(err)
	}

	t.Run("GET status healthy", func(t *testing.T) {
		statusContent := "enabled=1\npaused=1\nhealthy=1\nlow=45\nhigh=65\nupdated=" + strconv.FormatInt(time.Now().Unix(), 10) + "\n"
		if err := os.WriteFile(filepath.Join(adminDir, "battery-guard.status"), []byte(statusContent), 0600); err != nil {
			t.Fatal(err)
		}

		server := &Server{data: dataDir}
		req := httptest.NewRequest(http.MethodGet, "/api/power/battery-guard", nil)
		rec := httptest.NewRecorder()
		server.batteryGuardConfig(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		var res BatteryGuardStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !res.Healthy || !res.Enabled || !res.Paused || res.Low != 45 || res.High != 65 || res.Updated <= 0 {
			t.Fatalf("unexpected GET response: %+v", res)
		}
	})

	t.Run("GET status missing or unhealthy does not pretend defaults", func(t *testing.T) {
		emptyDir := t.TempDir()
		server := &Server{data: emptyDir}
		req := httptest.NewRequest(http.MethodGet, "/api/power/battery-guard", nil)
		rec := httptest.NewRecorder()
		server.batteryGuardConfig(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		var res BatteryGuardStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if res.Healthy || res.Enabled || res.Paused || res.Low != 0 || res.High != 0 {
			t.Fatalf("expected unapplied/zero status, got %+v", res)
		}
	})

	t.Run("Method not allowed", func(t *testing.T) {
		server := &Server{data: dataDir}
		req := httptest.NewRequest(http.MethodDelete, "/api/power/battery-guard", nil)
		rec := httptest.NewRecorder()
		server.batteryGuardConfig(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})

	invalidPayloads := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: "invalid json"},
		{name: "empty payload", body: "{}"},
		{name: "low below minimum 20", body: `{"low": 19, "high": 60}`},
		{name: "high above maximum 95", body: `{"low": 40, "high": 96}`},
		{name: "low equal to high", body: `{"low": 50, "high": 50}`},
		{name: "low greater than high", body: `{"low": 70, "high": 50}`},
		{name: "negative low", body: `{"low": -10, "high": 50}`},
		{name: "float low", body: `{"low": "abc", "high": 50}`},
	}

	for _, tt := range invalidPayloads {
		t.Run("POST invalid: "+tt.name, func(t *testing.T) {
			server := &Server{data: dataDir}
			req := httptest.NewRequest(http.MethodPost, "/api/power/battery-guard", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			server.batteryGuardConfig(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400 for payload %q, got %d (body: %s)", tt.body, rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("POST success with exact bytes, atomic write, and no pkill", func(t *testing.T) {
		var capturedCommand string
		server := &Server{
			data: dataDir,
			runSu: func(command string) ([]byte, error) {
				capturedCommand = command
				return []byte("ok"), nil
			},
		}

		req := httptest.NewRequest(http.MethodPost, "/api/power/battery-guard", strings.NewReader(`{"low": 45, "high": 70}`))
		rec := httptest.NewRecorder()
		server.batteryGuardConfig(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		var res struct {
			Ok   bool `json:"ok"`
			Low  int  `json:"low"`
			High int  `json:"high"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !res.Ok || res.Low != 45 || res.High != 70 {
			t.Fatalf("unexpected POST response: %+v", res)
		}

		// Verify command semantics
		if strings.Contains(capturedCommand, "pkill") {
			t.Fatalf("command MUST NOT contain pkill, got: %s", capturedCommand)
		}
		if !strings.Contains(capturedCommand, "LOW=45\nHIGH=70\n") {
			t.Fatalf("command must contain exact true-newline bytes LOW=45\\nHIGH=70\\n, got: %s", capturedCommand)
		}
		if !strings.Contains(capturedCommand, "chmod 600") {
			t.Fatalf("command must chmod 600 temp file, got: %s", capturedCommand)
		}
		if !strings.Contains(capturedCommand, `[ "$(sed -n '1p' "$tmp")" = 'LOW=45' ]`) {
			t.Fatalf("command must verify exact ordered line 1 is LOW=45 before move, got: %s", capturedCommand)
		}
		if !strings.Contains(capturedCommand, `[ "$(sed -n '2p' "$tmp")" = 'HIGH=70' ]`) {
			t.Fatalf("command must verify exact ordered line 2 is HIGH=70 before move, got: %s", capturedCommand)
		}
		if !strings.Contains(capturedCommand, `[ "$(wc -l < "$tmp")" -eq 2 ]`) {
			t.Fatalf("command must verify exactly 2 lines before move, got: %s", capturedCommand)
		}
		if !strings.Contains(capturedCommand, "mv -f \"$tmp\" '/data/adb/router-stack/battery-guard.conf'") && !strings.Contains(capturedCommand, "mv -f \"$tmp\" /data/adb/router-stack/battery-guard.conf") {
			t.Fatalf("command must atomically move temp file to target, got: %s", capturedCommand)
		}
		if !strings.Contains(capturedCommand, "trap 'rm -f \"$tmp\"' EXIT") {
			t.Fatalf("command must trap cleanup on failure, got: %s", capturedCommand)
		}
	})

	t.Run("POST runner failure returns 500", func(t *testing.T) {
		server := &Server{
			data: dataDir,
			runSu: func(command string) ([]byte, error) {
				return []byte("su: permission denied"), errors.New("exit status 1")
			},
		}

		req := httptest.NewRequest(http.MethodPost, "/api/power/battery-guard", strings.NewReader(`{"low": 40, "high": 60}`))
		rec := httptest.NewRecorder()
		server.batteryGuardConfig(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500 on runner failure, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var res map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		if res["error"] != "保存电池保护配置失败" {
			t.Fatalf("unexpected error message: %q", res["error"])
		}
	})
}
