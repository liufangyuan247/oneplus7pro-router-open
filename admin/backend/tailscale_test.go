package main

import "testing"

func TestParseTailscaleState(t *testing.T) {
	state := parseTailscaleState(`installed=yes
enabled=yes
supervisor=running
daemon=running
login_pending=no
auth_url=
backend=Running
online=yes
login_required=no
hostname=oneplus7-router
ip=100.64.1.2
advertise_routes=192.168.2.0/24
primary_routes=192.168.2.0/24
route_status=active
`)
	if !state.Installed || !state.Enabled || !state.Supervisor || !state.Daemon || !state.Online {
		t.Fatalf("unexpected service flags: %+v", state)
	}
	if state.LoginRequired || state.LoginPending {
		t.Fatalf("unexpected login state: %+v", state)
	}
	if state.Hostname != "oneplus7-router" || state.IP != "100.64.1.2" {
		t.Fatalf("unexpected identity: %+v", state)
	}
	if state.RouteStatus != "active" || state.PrimaryRoutes != "192.168.2.0/24" {
		t.Fatalf("unexpected route state: %+v", state)
	}
}

func TestParseTailscaleNeedsLogin(t *testing.T) {
	state := parseTailscaleState(`installed=yes
enabled=yes
supervisor=running
daemon=running
login_pending=yes
auth_url=https://login.tailscale.com/a/example
backend=NeedsLogin
online=no
login_required=yes
hostname=oneplus7-router
route_status=configured
`)
	if !state.LoginRequired || !state.LoginPending || state.Online {
		t.Fatalf("unexpected login flags: %+v", state)
	}
	if state.AuthURL != "https://login.tailscale.com/a/example" {
		t.Fatalf("authorization URL not parsed: %+v", state)
	}
}
