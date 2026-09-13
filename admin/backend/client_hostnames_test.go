package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadClientHostnamesAndResolveRules(t *testing.T) {
	tempDir := t.TempDir()
	dnsmasqDir := filepath.Join(tempDir, "dnsmasq")
	if err := os.MkdirAll(dnsmasqDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mock dnsmasq.leases with future exp, past exp, OnePlus-7Pro, and valid entries
	leasesContent := strings.Join([]string{
		"1893456000 00:11:22:33:44:55 192.168.2.101 TestDevice *",
		"1893456000 00:11:22:33:44:56 192.168.2.102 OnePlus-7Pro *",
		"1000000000 00:11:22:33:44:57 192.168.2.103 ExpiredDevice *",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dnsmasqDir, "dnsmasq.leases"), []byte(leasesContent), 0644); err != nil {
		t.Fatal(err)
	}

	server := &Server{
		data: tempDir,
		runSu: func(command string) ([]byte, error) {
			if strings.Contains(command, "'list-static'") {
				return []byte("00:11:22:33:44:88\t192.168.2.200\tStaticServer\n"), nil
			}
			return nil, nil
		},
	}

	hostnames := server.readClientHostnames()

	// Special hostnames
	if hostnames["127.0.0.1"] != "路由器本机 / CPA" {
		t.Errorf("unexpected 127.0.0.1: %s", hostnames["127.0.0.1"])
	}
	if hostnames["198.18.0.1"] != "Mihomo TUN 本机" {
		t.Errorf("unexpected 198.18.0.1: %s", hostnames["198.18.0.1"])
	}
	if hostnames[""] != "路由器本机 / 来源未知" {
		t.Errorf("unexpected empty IP: %s", hostnames[""])
	}

	// DHCP lease
	if hostnames["192.168.2.101"] != "TestDevice" {
		t.Errorf("expected 192.168.2.101 to be TestDevice, got %s", hostnames["192.168.2.101"])
	}
	// Filtered hostnames
	if _, ok := hostnames["192.168.2.102"]; ok {
		t.Errorf("expected OnePlus-7Pro to be filtered out")
	}
	if _, ok := hostnames["192.168.2.103"]; ok {
		t.Errorf("expected expired lease to be filtered out")
	}

	// Static lease
	if hostnames["192.168.2.200"] != "StaticServer" {
		t.Errorf("expected static lease 192.168.2.200 to be StaticServer, got %s", hostnames["192.168.2.200"])
	}

	// Test resolveClientHostname
	if got := server.resolveClientHostname("127.0.0.1"); got != "路由器本机 / CPA" {
		t.Errorf("resolveClientHostname(127.0.0.1) = %s", got)
	}
	if got := server.resolveClientHostname("198.18.0.1"); got != "Mihomo TUN 本机" {
		t.Errorf("resolveClientHostname(198.18.0.1) = %s", got)
	}
	if got := server.resolveClientHostname(""); got != "路由器本机 / 来源未知" {
		t.Errorf("resolveClientHostname('') = %s", got)
	}
	if got := server.resolveClientHostname("192.168.2.101"); got != "TestDevice" {
		t.Errorf("resolveClientHostname(192.168.2.101) = %s", got)
	}
	if got := server.resolveClientHostname("192.168.2.254"); got != "192.168.2.254" {
		t.Errorf("resolveClientHostname unknown expected 192.168.2.254, got %s", got)
	}
}
