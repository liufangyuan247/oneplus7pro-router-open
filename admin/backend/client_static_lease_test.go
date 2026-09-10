package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeClientMAC(t *testing.T) {
	got, err := normalizeClientMAC("02-AA-bb-00-10-20")
	if err != nil || got != "02:aa:bb:00:10:20" {
		t.Fatalf("normalizeClientMAC() = %q, %v", got, err)
	}
	for _, value := range []string{"", "not-a-mac", "00:00:00:00:00:00", "ff:ff:ff:ff:ff:ff", "01:00:5e:00:00:01"} {
		if _, err := normalizeClientMAC(value); err == nil {
			t.Errorf("normalizeClientMAC(%q) accepted an invalid address", value)
		}
	}
}

func TestValidateStaticIPv4(t *testing.T) {
	if got, err := validateStaticIPv4("192.168.2.200", "192.168.2.1/24"); err != nil || got != "192.168.2.200" {
		t.Fatalf("validateStaticIPv4() = %q, %v", got, err)
	}
	for _, value := range []string{"192.168.2.0", "192.168.2.1", "192.168.2.255", "192.168.3.20", "2001:db8::10", "invalid"} {
		if _, err := validateStaticIPv4(value, "192.168.2.1/24"); err == nil {
			t.Errorf("validateStaticIPv4(%q) accepted an invalid address", value)
		}
	}
}

func TestParseStaticLeases(t *testing.T) {
	leases := parseStaticLeases("02:aa:bb:00:10:20\t192.168.2.200\tdemo-client\ninvalid\t192.168.2.201\n")
	lease, ok := leases["02:aa:bb:00:10:20"]
	if !ok || lease.IP != "192.168.2.200" || lease.Hostname != "demo-client" {
		t.Fatalf("unexpected parsed lease: %#v", lease)
	}
	if len(leases) != 1 {
		t.Fatalf("parseStaticLeases returned %d entries, want 1", len(leases))
	}
}

func TestStaticLeaseAPISetAndClear(t *testing.T) {
	var commands []string
	server := &Server{
		data: t.TempDir(),
		runSu: func(command string) ([]byte, error) {
			commands = append(commands, command)
			if strings.Contains(command, "'list-static'") {
				return nil, nil
			}
			return []byte("ok\n"), nil
		},
	}

	setRequest := httptest.NewRequest(http.MethodPut, "/api/clients/static-lease", strings.NewReader(`{"mac":"02:AA:BB:00:10:20","ip":"192.168.2.200"}`))
	setResponse := httptest.NewRecorder()
	server.staticLeaseAPI(setResponse, setRequest)
	if setResponse.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", setResponse.Code, setResponse.Body.String())
	}
	if len(commands) < 2 || !strings.Contains(commands[len(commands)-1], "'set-static' '02:aa:bb:00:10:20' '192.168.2.200' ''") {
		t.Fatalf("unexpected set command: %#v", commands)
	}

	clearRequest := httptest.NewRequest(http.MethodDelete, "/api/clients/static-lease", strings.NewReader(`{"mac":"02:aa:bb:00:10:20"}`))
	clearResponse := httptest.NewRecorder()
	server.staticLeaseAPI(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d body=%s", clearResponse.Code, clearResponse.Body.String())
	}
	if !strings.Contains(commands[len(commands)-1], "'clear-static' '02:aa:bb:00:10:20'") {
		t.Fatalf("unexpected clear command: %q", commands[len(commands)-1])
	}
}

func TestStaticLeaseAPIRejectsInvalidOrConflictingInput(t *testing.T) {
	rootCalls := 0
	server := &Server{data: t.TempDir(), runSu: func(command string) ([]byte, error) {
		rootCalls++
		return nil, nil
	}}
	request := httptest.NewRequest(http.MethodPut, "/api/clients/static-lease", strings.NewReader(`{"mac":"02:aa:bb:00:10:20","ip":"192.168.2.1"}`))
	response := httptest.NewRecorder()
	server.staticLeaseAPI(response, request)
	if response.Code != http.StatusBadRequest || rootCalls != 0 {
		t.Fatalf("invalid request status=%d rootCalls=%d", response.Code, rootCalls)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("missing JSON error: %s", response.Body.String())
	}
}

func TestParseConntrackClientTraffic(t *testing.T) {
	sample := `ipv4     2 tcp      6 431999 ESTABLISHED src=192.168.2.151 dst=1.1.1.1 sport=54321 dport=443 packets=100 bytes=150000 src=1.1.1.1 dst=192.168.2.151 sport=443 dport=54321 packets=200 bytes=980000 [ASSURED] mark=0 use=2
ipv4     2 udp      17 29 src=192.168.2.100 dst=8.8.8.8 sport=12345 dport=53 packets=2 bytes=200 src=8.8.8.8 dst=192.168.2.100 sport=53 dport=12345 packets=2 bytes=400 [ASSURED] mark=0 use=1
`
	stats := make(map[string]netCounter)
	parseConntrackClientTraffic(sample, stats)

	if c1, ok := stats["192.168.2.151"]; !ok || c1.rx != 150000 || c1.tx != 980000 {
		t.Fatalf("unexpected stats for 192.168.2.151: %#v", c1)
	}
	if c2, ok := stats["192.168.2.100"]; !ok || c2.rx != 200 || c2.tx != 400 {
		t.Fatalf("unexpected stats for 192.168.2.100: %#v", c2)
	}
}

func TestClientTrafficRateCalculation(t *testing.T) {
	s := &Server{}
	start := time.Unix(1_700_000_000, 0)

	s.updateClientTraffic(start, map[string]netCounter{
		"192.168.2.151": {rx: 1_000_000, tx: 2_000_000},
	})
	s.updateClientTraffic(start.Add(2*time.Second), map[string]netCounter{
		"192.168.2.151": {rx: 3_000_000, tx: 8_000_000},
	})

	stats, rates := s.getClientTrafficSnapshot()
	if stats["192.168.2.151"].rx != 3_000_000 || stats["192.168.2.151"].tx != 8_000_000 {
		t.Fatalf("stats = %#v, want rx 3000000, tx 8000000", stats["192.168.2.151"])
	}

	if diff := math.Abs(rates["192.168.2.151"].rx - 1_000_000); diff > 0.1 {
		t.Fatalf("rx rate = %f, want 1000000", rates["192.168.2.151"].rx)
	}
	if diff := math.Abs(rates["192.168.2.151"].tx - 3_000_000); diff > 0.1 {
		t.Fatalf("tx rate = %f, want 3000000", rates["192.168.2.151"].tx)
	}
}

func TestClientInfoJSONSerialization(t *testing.T) {
	client := ClientInfo{
		IP:          "192.168.2.151",
		MAC:         "02:aa:bb:cc:dd:ee",
		Hostname:    "Test-Phone",
		Interface:   "eth1",
		State:       "在线",
		Source:      "DHCP",
		Expires:     3600,
		StaticLease: false,
		StaticIP:    "",
		RxBytes:     1024000,
		TxBytes:     5120000,
		RxBps:       150000.5,
		TxBps:       450000.8,
	}

	data, err := json.Marshal(client)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["RxBytes"].(float64) != 1024000 || parsed["TxBytes"].(float64) != 5120000 {
		t.Fatalf("unexpected RxBytes/TxBytes in JSON: %v", parsed)
	}
	if parsed["RxBps"].(float64) != 150000.5 || parsed["TxBps"].(float64) != 450000.8 {
		t.Fatalf("unexpected RxBps/TxBps in JSON: %v", parsed)
	}
}
