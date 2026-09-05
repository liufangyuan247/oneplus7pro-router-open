package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAria2RPCParams(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		args   []any
		want   []any
	}{
		{name: "authentication disabled", args: []any{"gid"}, want: []any{"gid"}},
		{name: "authentication enabled", secret: "rpc-secret", args: []any{"gid"}, want: []any{"token:rpc-secret", "gid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aria2RPCParams(tt.secret, tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("aria2RPCParams() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestLoginRememberChoice(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		persistent bool
	}{
		{name: "browser session", body: `{"password":"test-password","remember":false}`},
		{name: "remembered", body: `{"password":"test-password","remember":true}`, persistent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			server := &Server{
				data:     dataDir,
				password: "test-password",
				sessions: make(map[string]authSession),
			}
			req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(tt.body))
			res := httptest.NewRecorder()

			server.login(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("login status = %d, body = %s", res.Code, res.Body.String())
			}
			cookies := res.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != "router_session" {
				t.Fatalf("login cookies = %#v", cookies)
			}
			if got := cookies[0].MaxAge > 0; got != tt.persistent {
				t.Fatalf("persistent cookie = %v, want %v", got, tt.persistent)
			}

			_, statErr := os.Stat(server.rememberedSessionsPath())
			if !tt.persistent {
				if !os.IsNotExist(statErr) {
					t.Fatalf("non-remembered login wrote session file: %v", statErr)
				}
				return
			}
			if statErr != nil {
				t.Fatalf("remembered session file: %v", statErr)
			}
			contents, err := os.ReadFile(server.rememberedSessionsPath())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(contents), "test-password") || strings.Contains(string(contents), cookies[0].Value) {
				t.Fatal("remembered session file contains a plaintext credential")
			}
		})
	}
}

func TestRememberedLoginSurvivesServerRestart(t *testing.T) {
	dataDir := t.TempDir()
	first := &Server{data: dataDir, password: "test-password", sessions: make(map[string]authSession)}
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"password":"test-password","remember":true}`))
	loginRes := httptest.NewRecorder()
	first.login(loginRes, loginReq)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginRes.Code)
	}
	cookie := loginRes.Result().Cookies()[0]

	restarted := &Server{data: dataDir, sessions: make(map[string]authSession)}
	if err := restarted.loadRememberedSessions(); err != nil {
		t.Fatal(err)
	}
	protected := restarted.auth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	req.RemoteAddr = "192.168.2.100:54321"
	req.AddCookie(cookie)
	res := httptest.NewRecorder()

	protected(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("remembered request status = %d, body = %s", res.Code, res.Body.String())
	}
	refreshed := res.Result().Cookies()
	if len(refreshed) != 1 || refreshed[0].MaxAge <= 0 {
		t.Fatalf("remembered cookie was not renewed: %#v", refreshed)
	}
}
