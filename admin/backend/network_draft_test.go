package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDraft(t *testing.T) {
	valid := NetworkDraft{
		WANInterface: "eth0",
		LANInterface: "eth1",
		WANMode:      "pppoe",
		LANAddress:   "192.168.2.1/24",
		DHCPStart:    "192.168.2.100",
		DHCPEnd:      "192.168.2.249",
		DNS:          "192.168.2.1",
		MTU:          1492,
		PPPoEUser:    "broadband_user_123",
		PPPoEPass:    "broadband_pass_456",
		PPPoEBackend: "auto",
		WANMAC:       "12:34:56:78:9a:bc",
	}

	if err := validateDraft(valid); err != nil {
		t.Fatalf("expected valid draft to pass, got: %v", err)
	}

	// Test missing PPPoE user when WANMode is pppoe
	missingUser := valid
	missingUser.PPPoEUser = "   "
	if err := validateDraft(missingUser); err == nil || !strings.Contains(err.Error(), "账号不能为空") {
		t.Fatalf("expected error for empty PPPoE user, got: %v", err)
	}

	// Test missing PPPoE pass when WANMode is pppoe
	missingPass := valid
	missingPass.PPPoEPass = ""
	if err := validateDraft(missingPass); err == nil || !strings.Contains(err.Error(), "密码不能为空") {
		t.Fatalf("expected error for empty PPPoE pass, got: %v", err)
	}

	// Test invalid backend
	invalidBackend := valid
	invalidBackend.PPPoEBackend = "invalid_backend"
	if err := validateDraft(invalidBackend); err == nil || !strings.Contains(err.Error(), "后端模式无效") {
		t.Fatalf("expected error for invalid backend, got: %v", err)
	}

	// Test valid backend options
	for _, b := range []string{"auto", "kernel", "userspace", ""} {
		validWithB := valid
		validWithB.PPPoEBackend = b
		if err := validateDraft(validWithB); err != nil {
			t.Fatalf("expected backend %q to be valid, got: %v", b, err)
		}
	}

	// Test invalid WAN MAC
	invalidMAC := valid
	invalidMAC.WANMAC = "invalid:mac:address"
	if err := validateDraft(invalidMAC); err == nil || !strings.Contains(err.Error(), "MAC 地址格式无效") {
		t.Fatalf("expected error for invalid MAC, got: %v", err)
	}

	// Test valid alternative MAC formats
	validMAC := valid
	validMAC.WANMAC = "2C-16-DB-A8-E7-20"
	if err := validateDraft(validMAC); err != nil {
		t.Fatalf("expected hyphen-separated MAC to pass, got: %v", err)
	}

	// Test non-pppoe mode doesn't require credentials
	dhcpDraft := valid
	dhcpDraft.WANMode = "dhcp"
	dhcpDraft.PPPoEUser = ""
	dhcpDraft.PPPoEPass = ""
	if err := validateDraft(dhcpDraft); err != nil {
		t.Fatalf("expected DHCP draft to pass without credentials, got: %v", err)
	}
}

func TestReadAndUpdateRouterNetworkConfig(t *testing.T) {
	initialConfig := `WAN_VIDPID='bda/8153'
LAN_VIDPID=''
WAN_MAC='12:34:56:78:9a:bc'
LAN_MAC=''
PPPOE_USER='broadband_user_123'
PPPOE_PASS='broadband_pass_456'
PROXY_ENABLED='1'
DISABLE_WIFI_ON_ACTIVE=1
PPPOE_BACKEND='auto'
PPPOE_KERNEL_TRIAL_SECONDS=90
`

	var lastExecutedCmd string
	s := &Server{
		data: t.TempDir(),
		runSu: func(command string) ([]byte, error) {
			lastExecutedCmd = command
			if command == "cat /data/adb/router-network/config" {
				return []byte(initialConfig), nil
			}
			return []byte("ok"), nil
		},
	}

	// Test reading
	cfg, err := s.readRouterNetworkConfig()
	if err != nil {
		t.Fatalf("readRouterNetworkConfig failed: %v", err)
	}
	if cfg["PPPOE_USER"] != "broadband_user_123" || cfg["PPPOE_PASS"] != "broadband_pass_456" || cfg["PPPOE_BACKEND"] != "auto" {
		t.Fatalf("unexpected read values: %#v", cfg)
	}

	// Test updating
	updateDraft := NetworkDraft{
		WANInterface: "eth0",
		LANInterface: "eth1",
		WANMode:      "pppoe",
		LANAddress:   "192.168.2.1/24",
		DHCPStart:    "192.168.2.100",
		DHCPEnd:      "192.168.2.249",
		DNS:          "192.168.2.1",
		MTU:          1492,
		PPPoEUser:    "new_user_123",
		PPPoEPass:    "new_pass_456",
		PPPoEBackend: "kernel",
		WANMAC:       "00:11:22:33:44:55",
	}

	if err := s.updateRouterNetworkConfig(updateDraft); err != nil {
		t.Fatalf("updateRouterNetworkConfig failed: %v", err)
	}

	if !strings.Contains(lastExecutedCmd, "new_user_123") {
		t.Fatalf("command does not contain updated user: %s", lastExecutedCmd)
	}
	if !strings.Contains(lastExecutedCmd, "new_pass_456") {
		t.Fatalf("command does not contain updated pass: %s", lastExecutedCmd)
	}
	if !strings.Contains(lastExecutedCmd, "PPPOE_BACKEND") || !strings.Contains(lastExecutedCmd, "kernel") {
		t.Fatalf("command does not contain updated backend: %s", lastExecutedCmd)
	}
	if !strings.Contains(lastExecutedCmd, "00:11:22:33:44:55") {
		t.Fatalf("command does not contain updated MAC: %s", lastExecutedCmd)
	}
	// Verify preservation of other fields
	if !strings.Contains(lastExecutedCmd, "bda/8153") {
		t.Fatalf("command did not preserve other fields: %s", lastExecutedCmd)
	}
}

func TestNetworkReconnectAPI(t *testing.T) {
	tempDir := t.TempDir()
	draftPath := filepath.Join(tempDir, "admin")
	_ = os.MkdirAll(draftPath, 0700)

	draft := NetworkDraft{
		WANInterface: "eth0",
		LANInterface: "eth1",
		WANMode:      "pppoe",
		LANAddress:   "192.168.2.1/24",
		DHCPStart:    "192.168.2.100",
		DHCPEnd:      "192.168.2.249",
		DNS:          "192.168.2.1",
		MTU:          1492,
		PPPoEUser:    "user",
		PPPoEPass:    "pass",
		PPPoEBackend: "kernel",
	}
	b, _ := json.Marshal(draft)
	_ = os.WriteFile(filepath.Join(draftPath, "network-draft.json"), b, 0600)

	var lastCommand string
	s := &Server{
		data: tempDir,
		runSu: func(command string) ([]byte, error) {
			lastCommand = command
			return []byte("PPPOE_KERNEL_RETRY_ARMED"), nil
		},
	}

	req := httptest.NewRequest("POST", "/api/network/reconnect", bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	s.networkReconnect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(lastCommand, "pppoe-retry-kernel") {
		t.Fatalf("expected pppoe-retry-kernel command, got: %s", lastCommand)
	}

	// Test userspace reconnect
	reqUserspace := httptest.NewRequest("POST", "/api/network/reconnect", bytes.NewBufferString(`{"backend": "userspace"}`))
	wUserspace := httptest.NewRecorder()
	s.networkReconnect(wUserspace, reqUserspace)

	if wUserspace.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", wUserspace.Code, wUserspace.Body.String())
	}
	if !strings.Contains(lastCommand, "pppoe-retry-userspace") {
		t.Fatalf("expected pppoe-retry-userspace command, got: %s", lastCommand)
	}
}
