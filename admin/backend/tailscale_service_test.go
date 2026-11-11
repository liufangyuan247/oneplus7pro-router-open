package main

import "testing"

func TestTailscaleServiceStates(t *testing.T) {
	for _, tc := range []struct {
		name, status, state string
		pid, uptime         int
	}{
		{"running", "installed=yes\nenabled=yes\ndaemon=running\npid=123\nuptime=456\n", "运行中", 123, 456},
		{"disabled", "installed=yes\nenabled=no\ndaemon=stopped\npid=999\nuptime=999\n", "已禁用", 0, 0},
		{"waiting for supervisor", "installed=yes\nenabled=yes\ndaemon=stopped\n", "停止", 0, 0},
		{"not installed", "installed=no\n", "未安装", 0, 0},
		{"invalid counters", "installed=yes\ndaemon=running\npid=-1\nuptime=invalid\n", "运行中", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := parseTailscaleService(tc.status)
			if info.Name != "tailscale" || info.State != tc.state || info.PID != tc.pid || info.UptimeSec != tc.uptime || info.WebURL != "" || info.Port != 0 {
				t.Fatalf("unexpected Tailscale card: %+v", info)
			}
		})
	}
}

func TestTailscaleVersionOutput(t *testing.T) {
	for _, tc := range []struct{ output, want string }{
		{"1.102.3\n  tailscale commit: abcdef\n  go version: go1.26.6\n", "v1.102.3"},
		{"v1.104.6\n", "v1.104.6"},
		{"1.105.0-dev20261111\n", "v1.105.0-dev20261111"},
		{"cannot execute tailscale\n", ""},
		{"go version: go1.26.6", ""},
	} {
		if got := parseTailscaleVersion(tc.output); got != tc.want {
			t.Errorf("parseTailscaleVersion(%q) = %q; want %q", tc.output, got, tc.want)
		}
	}
}

func TestRootTailscaleTracksReleasesWithoutSingleBinaryUpgrade(t *testing.T) {
	spec, ok := supportedServiceUpdaters["tailscale"]
	repo, tracked, detectionOnly := resolveUpdateTracking(spec, ok, false, nil)
	if !tracked || !detectionOnly || repo != "tailscale/tailscale" || spec.DetectionHint == "" {
		t.Fatalf("unsafe Root service update tracking: %+v", spec)
	}
	if !isNewerVersion("v1.102.3", "v1.104.6") {
		t.Fatal("stable upstream release must be identified as newer")
	}
}
