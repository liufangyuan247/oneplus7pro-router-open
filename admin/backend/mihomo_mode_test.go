package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMihomoModeUsesCanonicalRootCommand(t *testing.T) {
	var command string
	server := &Server{
		runSu: func(value string) ([]byte, error) {
			command = value
			return []byte("MIHOMO_MODE_RULE\n"), nil
		},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/mihomo/mode", strings.NewReader(`{"mode":"rule"}`))

	server.mihomoMode(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	want := routerNetworkManager + " proxy-mode rule"
	if command != want {
		t.Fatalf("root command = %q, want %q", command, want)
	}
}

func TestMihomoAPIProxyPersistsDashboardModeChanges(t *testing.T) {
	var command string
	server := &Server{
		runSu: func(value string) ([]byte, error) {
			command = value
			return []byte("MIHOMO_MODE_GLOBAL\n"), nil
		},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/mihomo-api/configs", strings.NewReader(`{"mode":"global"}`))

	server.mihomoAPIProxy(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	want := routerNetworkManager + " proxy-mode global"
	if command != want {
		t.Fatalf("root command = %q, want %q", command, want)
	}
}

func TestMihomoAPIProxyRejectsInvalidMode(t *testing.T) {
	called := false
	server := &Server{runSu: func(string) ([]byte, error) {
		called = true
		return nil, nil
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/mihomo-api/configs", strings.NewReader(`{"mode":"unexpected"}`))

	server.mihomoAPIProxy(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatal("invalid mode reached root command")
	}
}
