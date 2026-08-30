package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
