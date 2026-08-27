package main

import (
	"os"
	"path/filepath"
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
	path := filepath.Join(t.TempDir(), "battery-guard.status")
	tests := []struct {
		name                   string
		content                string
		wantEnabled, wantPause bool
	}{
		{name: "fresh and paused", content: "enabled=1\npaused=1\nhealthy=1\nupdated=1799999970\n", wantEnabled: true, wantPause: true},
		{name: "fresh and charging", content: "enabled=1\npaused=0\nhealthy=1\nupdated=1800000000\n", wantEnabled: true, wantPause: false},
		{name: "explicitly disabled", content: "enabled=0\npaused=0\nhealthy=1\nupdated=1800000000\n", wantEnabled: false, wantPause: false},
		{name: "controller unhealthy", content: "enabled=1\npaused=1\nhealthy=0\nupdated=1800000000\n", wantEnabled: false, wantPause: false},
		{name: "stale heartbeat", content: "enabled=1\npaused=1\nhealthy=1\nupdated=1799999900\n", wantEnabled: false, wantPause: false},
		{name: "future heartbeat", content: "enabled=1\npaused=1\nhealthy=1\nupdated=1800000040\n", wantEnabled: false, wantPause: false},
		{name: "invalid heartbeat", content: "enabled=1\npaused=1\nhealthy=1\nupdated=nope\n", wantEnabled: false, wantPause: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
				t.Fatal(err)
			}
			enabled, paused := readBatteryGuardStatus(path, now)
			if enabled != test.wantEnabled || paused != test.wantPause {
				t.Fatalf("readBatteryGuardStatus() = (%v, %v), want (%v, %v)", enabled, paused, test.wantEnabled, test.wantPause)
			}
		})
	}
}
