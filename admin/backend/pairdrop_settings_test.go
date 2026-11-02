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

const testAdminKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func setupTestPairdropEnv(t *testing.T, keyContent string) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	keyDir := filepath.Join(dir, "router-data", "pairdrop", "mailbox")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatalf("failed to create key dir: %v", err)
	}
	if keyContent != "" {
		keyPath := filepath.Join(keyDir, "admin.key")
		if err := os.WriteFile(keyPath, []byte(keyContent), 0600); err != nil {
			t.Fatalf("failed to write admin.key: %v", err)
		}
	}
	return dir, func() { os.RemoveAll(dir) }
}

func TestPairdropSettingsGETSuccessDropsSecrets(t *testing.T) {
	homeDir, cleanup := setupTestPairdropEnv(t, testAdminKeyHex+"\n")
	defer cleanup()

	var receivedAdminHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mailbox/api/admin/settings" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected upstream method: %s", r.Method)
		}
		receivedAdminHeader = r.Header.Get("x-router-mailbox-admin")
		w.Header().Set("Content-Type", "application/json")
		// Simulate upstream returning secret directRoomSecret
		_, _ = w.Write([]byte(`{
			"directRoomSecret": "super_secret_token_12345",
			"retentionSeconds": 7200,
			"maxFileBytes": 536870912,
			"maxTotalBytes": 2147483648,
			"maxFiles": 1000,
			"storedBytes": 4096
		}`))
	}))
	defer upstream.Close()

	s := &Server{
		home:             homeDir,
		pairdropAdminURL: upstream.URL,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pairdrop/settings", nil)
	w := httptest.NewRecorder()
	s.pairdropSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d, body: %s", w.Code, w.Body.String())
	}
	if receivedAdminHeader != testAdminKeyHex {
		t.Fatalf("upstream did not receive correct admin key header, got: %s", receivedAdminHeader)
	}

	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, "super_secret_token_12345") || strings.Contains(bodyStr, "directRoomSecret") {
		t.Fatalf("response leaked upstream secrets: %s", bodyStr)
	}

	var res PairdropSettings
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if res.RetentionSeconds != 7200 || res.MaxFileBytes != 536870912 || res.MaxTotalBytes != 2147483648 || res.MaxFiles != 1000 || res.StoredBytes != 4096 {
		t.Fatalf("unexpected settings values: %+v", res)
	}
}

func TestPairdropSettingsAdminKeyValidation(t *testing.T) {
	// Case 1: Key file missing
	homeDirNoKey, cleanupNoKey := setupTestPairdropEnv(t, "")
	defer cleanupNoKey()

	sNoKey := &Server{
		home:             homeDirNoKey,
		pairdropAdminURL: "http://127.0.0.1:3000",
	}
	req := httptest.NewRequest(http.MethodGet, "/api/pairdrop/settings", nil)
	w := httptest.NewRecorder()
	sNoKey.pairdropSettings(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for missing key, got %d", w.Code)
	}

	// Case 2: Invalid hex characters
	homeDirBadHex, cleanupBadHex := setupTestPairdropEnv(t, "not_a_valid_hex_key_string!!!")
	defer cleanupBadHex()
	sBadHex := &Server{
		home:             homeDirBadHex,
		pairdropAdminURL: "http://127.0.0.1:3000",
	}
	w = httptest.NewRecorder()
	sBadHex.pairdropSettings(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for bad hex, got %d", w.Code)
	}

	// Case 3: Key too short (< 16 chars)
	homeDirShort, cleanupShort := setupTestPairdropEnv(t, "1234abcd")
	defer cleanupShort()
	sShort := &Server{
		home:             homeDirShort,
		pairdropAdminURL: "http://127.0.0.1:3000",
	}
	w = httptest.NewRecorder()
	sShort.pairdropSettings(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for short key, got %d", w.Code)
	}
}

func TestPairdropSettingsUpstreamFailures(t *testing.T) {
	homeDir, cleanup := setupTestPairdropEnv(t, testAdminKeyHex)
	defer cleanup()

	// 1. Upstream 401 Unauthorized
	upstream401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"secret_error":"bad_token"}`, http.StatusUnauthorized)
	}))
	defer upstream401.Close()

	s := &Server{home: homeDir, pairdropAdminURL: upstream401.URL}
	w := httptest.NewRecorder()
	s.pairdropSettings(w, httptest.NewRequest(http.MethodGet, "/api/pairdrop/settings", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway for upstream 401, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret_error") {
		t.Fatalf("leaked upstream body in error: %s", w.Body.String())
	}

	// 2. Upstream 500 Internal Server Error
	upstream500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"internal_debug_stack":"trace_dump"}`, http.StatusInternalServerError)
	}))
	defer upstream500.Close()

	s = &Server{home: homeDir, pairdropAdminURL: upstream500.URL}
	w = httptest.NewRecorder()
	s.pairdropSettings(w, httptest.NewRequest(http.MethodGet, "/api/pairdrop/settings", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway for upstream 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "internal_debug_stack") {
		t.Fatalf("leaked upstream body in error: %s", w.Body.String())
	}

	// 3. Upstream connection refused (server stopped)
	upstreamClosed := httptest.NewServer(http.NotFoundHandler())
	upstreamClosedURL := upstreamClosed.URL
	upstreamClosed.Close()

	s = &Server{home: homeDir, pairdropAdminURL: upstreamClosedURL}
	w = httptest.NewRecorder()
	s.pairdropSettings(w, httptest.NewRequest(http.MethodGet, "/api/pairdrop/settings", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway for unreachable upstream, got %d", w.Code)
	}
}

func TestPairdropSettingsPUTSuccess(t *testing.T) {
	homeDir, cleanup := setupTestPairdropEnv(t, testAdminKeyHex)
	defer cleanup()

	var receivedMethod string
	var receivedBody map[string]int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("expected application/json content type, got: %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("x-router-mailbox-admin") != testAdminKeyHex {
			t.Fatalf("incorrect admin header")
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"directRoomSecret": "hidden_secret",
			"retentionSeconds": 3600,
			"maxFileBytes": 1048576,
			"maxTotalBytes": 20971520,
			"maxFiles": 1000,
			"storedBytes": 0
		}`))
	}))
	defer upstream.Close()

	s := &Server{home: homeDir, pairdropAdminURL: upstream.URL}
	putPayload := `{"retentionSeconds":3600,"maxFileBytes":1048576,"maxTotalBytes":20971520}`
	req := httptest.NewRequest(http.MethodPut, "/api/pairdrop/settings", strings.NewReader(putPayload))
	w := httptest.NewRecorder()
	s.pairdropSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}
	if receivedMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %s", receivedMethod)
	}
	if receivedBody["retentionSeconds"] != 3600 || receivedBody["maxFileBytes"] != 1048576 || receivedBody["maxTotalBytes"] != 20971520 {
		t.Fatalf("unexpected forwarded body: %+v", receivedBody)
	}
	if strings.Contains(w.Body.String(), "hidden_secret") {
		t.Fatalf("leaked secret in PUT response: %s", w.Body.String())
	}
}

func TestPairdropSettingsPUTStrictValidation(t *testing.T) {
	homeDir, cleanup := setupTestPairdropEnv(t, testAdminKeyHex)
	defer cleanup()

	s := &Server{home: homeDir, pairdropAdminURL: "http://127.0.0.1:3000"}

	testCases := []struct {
		name    string
		payload string
	}{
		{"unknown field", `{"retentionSeconds":3600,"maxFileBytes":1000,"maxTotalBytes":2000,"extra":true}`},
		{"missing retentionSeconds", `{"maxFileBytes":1000,"maxTotalBytes":2000}`},
		{"missing maxFileBytes", `{"retentionSeconds":3600,"maxTotalBytes":2000}`},
		{"missing maxTotalBytes", `{"retentionSeconds":3600,"maxFileBytes":1000}`},
		{"float in retentionSeconds", `{"retentionSeconds":3600.5,"maxFileBytes":1000,"maxTotalBytes":2000}`},
		{"quoted number", `{"retentionSeconds":"3600","maxFileBytes":1000,"maxTotalBytes":2000}`},
		{"float zero in retentionSeconds", `{"retentionSeconds":3600.0,"maxFileBytes":1000,"maxTotalBytes":2000}`},
		{"exponent in maxFileBytes", `{"retentionSeconds":3600,"maxFileBytes":1e6,"maxTotalBytes":2000000}`},
		{"retention below min 60", `{"retentionSeconds":59,"maxFileBytes":1000,"maxTotalBytes":2000}`},
		{"retention above max 604800", `{"retentionSeconds":604801,"maxFileBytes":1000,"maxTotalBytes":2000}`},
		{"maxFileBytes below 1", `{"retentionSeconds":3600,"maxFileBytes":0,"maxTotalBytes":2000}`},
		{"maxFileBytes above 10GiB", `{"retentionSeconds":3600,"maxFileBytes":10737418241,"maxTotalBytes":20000000000}`},
		{"maxTotalBytes above 100GiB", `{"retentionSeconds":3600,"maxFileBytes":1000,"maxTotalBytes":107374182401}`},
		{"file greater than total", `{"retentionSeconds":3600,"maxFileBytes":5000,"maxTotalBytes":1000}`},
		{"trailing json junk", `{"retentionSeconds":3600,"maxFileBytes":1000,"maxTotalBytes":2000} trailing`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/pairdrop/settings", strings.NewReader(tc.payload))
			w := httptest.NewRecorder()
			s.pairdropSettings(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for %s, got %d, body: %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestPairdropSettingsDoesNotFollowRedirect(t *testing.T) {
	homeDir, cleanup := setupTestPairdropEnv(t, testAdminKeyHex)
	defer cleanup()
	var redirectReached bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectReached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer upstream.Close()
	s := &Server{home: homeDir, pairdropAdminURL: upstream.URL}
	w := httptest.NewRecorder()
	s.pairdropSettings(w, httptest.NewRequest(http.MethodGet, "/api/pairdrop/settings", nil))
	if redirectReached || w.Code != http.StatusBadGateway {
		t.Fatal("admin request followed an unexpected redirect")
	}
}

func TestPairdropSettingsMethodNotAllowed(t *testing.T) {
	s := &Server{}
	for _, m := range []string{http.MethodPost, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(m, "/api/pairdrop/settings", nil)
		w := httptest.NewRecorder()
		s.pairdropSettings(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for %s, got %d", m, w.Code)
		}
	}
}

func TestPairdropSettingsAuthAndCSRFProtection(t *testing.T) {
	homeDir, cleanup := setupTestPairdropEnv(t, testAdminKeyHex)
	defer cleanup()

	s := &Server{
		home:     homeDir,
		password: "secretpassword",
		sessions: make(map[string]authSession),
	}

	handler := s.auth(s.pairdropSettings)

	// Unauthenticated request without session or loopback bypass
	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.1/api/pairdrop/settings", nil)
	req.RemoteAddr = "192.168.1.50:54321"
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", w.Code)
	}

	// Cross-site PUT (origin rejected)
	putReq := httptest.NewRequest(http.MethodPut, "http://192.168.1.1/api/pairdrop/settings", bytes.NewReader([]byte(`{}`)))
	putReq.RemoteAddr = "192.168.1.50:54321"
	putReq.Header.Set("Origin", "http://evil-site.com")
	w = httptest.NewRecorder()
	handler(w, putReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for CSRF origin rejection, got %d", w.Code)
	}
}
