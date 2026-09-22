package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSubscriptionYAML = `proxies:
  - name: JP Block
    type: vless
    server: jp.example.com
    port: 443
    uuid: should-never-leak
  - {name: "HK Inline", type: hysteria2, server: hk.example.com, port: "8443", password: secret}
`

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func testSubscriptionRecord(id, name, rawURL string, active bool) SubscriptionRecord {
	now := time.Now().Add(-time.Hour)
	item := SubscriptionRecord{ID: id, Name: name, URL: rawURL, CreatedAt: now, UpdatedAt: now}
	if active {
		item.CachedURL = rawURL
		item.NodeCount = 2
	}
	return item
}

func decodeSubscriptionResponse(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	ActiveID string               `json:"activeId"`
	Items    []PublicSubscription `json:"items"`
} {
	t.Helper()
	var response struct {
		ActiveID string               `json:"activeId"`
		Items    []PublicSubscription `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	return response
}

func TestParseSubscriptionNodesReturnsOnlySafeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscription.yaml")
	writeTestFile(t, path, testSubscriptionYAML)

	nodes, err := parseSubscriptionNodes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Port != "443" || nodes[1].Port != "8443" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	encoded, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "should-never-leak") || strings.Contains(text, "secret") || strings.Contains(text, "uuid") || strings.Contains(text, "password") {
		t.Fatalf("node response leaked credentials: %s", text)
	}
}

func TestMigrateActiveSubscriptionCacheAndRejectStaleURL(t *testing.T) {
	dataDir := t.TempDir()
	server := &Server{data: dataDir}
	oldURL := "https://example.com/old?token=secret"
	store := SubscriptionStore{
		Version:  1,
		ActiveID: "active",
		Items:    []SubscriptionRecord{testSubscriptionRecord("active", "Main", oldURL, false)},
	}
	writeTestFile(t, filepath.Join(dataDir, "mihomo", "subscription.url"), oldURL+"\n")
	writeTestFile(t, filepath.Join(dataDir, "mihomo", "config.yaml"), testSubscriptionYAML)
	if err := server.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}

	loaded, err := server.loadSubscriptionStore()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Items[0].CachedURL != oldURL || loaded.Items[0].NodeCount != 2 || !fileExistsNonEmpty(server.subscriptionCachePath("active")) {
		t.Fatalf("active cache was not migrated: %#v", loaded.Items[0])
	}

	loaded.Items[0].URL = "https://example.com/new?token=changed"
	if err := server.saveSubscriptionStore(loaded); err != nil {
		t.Fatal(err)
	}
	reloaded, err := server.loadSubscriptionStore()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Items[0].CachedURL == reloaded.Items[0].URL {
		t.Fatal("old running config was incorrectly treated as cache for edited URL")
	}
}

func TestMigrateImportsNewerBackgroundUpdaterCache(t *testing.T) {
	dataDir := t.TempDir()
	server := &Server{data: dataDir}
	rawURL := "https://example.com/subscription"
	item := testSubscriptionRecord("active", "Main", rawURL, true)
	store := SubscriptionStore{Version: 1, ActiveID: item.ID, Items: []SubscriptionRecord{item}}
	writeTestFile(t, filepath.Join(dataDir, "mihomo", "subscription.url"), rawURL+"\n")
	cachePath := server.subscriptionCachePath(item.ID)
	writeTestFile(t, cachePath, "proxies:\n  - {name: Old, type: vless, server: old.example.com, port: 443}\n")
	backgroundPath := filepath.Join(dataDir, "mihomo", "active-subscription.raw.yaml")
	writeTestFile(t, backgroundPath, testSubscriptionYAML)
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(cachePath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(backgroundPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if err := server.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}

	loaded, err := server.loadSubscriptionStore()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Items[0].NodeCount != 2 {
		t.Fatalf("background cache was not imported: %#v", loaded.Items[0])
	}
	nodes, err := parseSubscriptionNodes(cachePath)
	if err != nil || nodes[0].Name != "JP Block" {
		t.Fatalf("unexpected imported cache: nodes=%#v err=%v", nodes, err)
	}
}

func TestSubscriptionEditNameKeepsCacheAndURLEditInvalidatesIt(t *testing.T) {
	dataDir := t.TempDir()
	server := &Server{data: dataDir}
	rawURL := "https://example.com/sub?token=secret"
	item := testSubscriptionRecord("active", "Old name", rawURL, true)
	store := SubscriptionStore{Version: 1, ActiveID: item.ID, Items: []SubscriptionRecord{item}}
	writeTestFile(t, filepath.Join(dataDir, "mihomo", "subscription.url"), rawURL+"\n")
	writeTestFile(t, server.subscriptionCachePath(item.ID), testSubscriptionYAML)
	if err := server.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}

	nameRequest := httptest.NewRequest(http.MethodPatch, "/api/mihomo/subscriptions", strings.NewReader(`{"id":"active","action":"edit","name":"New name"}`))
	nameRecorder := httptest.NewRecorder()
	server.mihomoSubscriptionsV2(nameRecorder, nameRequest)
	if nameRecorder.Code != http.StatusOK {
		t.Fatalf("name edit failed: %d %s", nameRecorder.Code, nameRecorder.Body.String())
	}
	nameResponse := decodeSubscriptionResponse(t, nameRecorder)
	if nameResponse.Items[0].Name != "New name" || !nameResponse.Items[0].Cached {
		t.Fatalf("name edit changed cache state: %#v", nameResponse.Items[0])
	}

	urlRequest := httptest.NewRequest(http.MethodPatch, "/api/mihomo/subscriptions", strings.NewReader(`{"id":"active","action":"edit","name":"New name","url":"https://new.example.com/sub?token=new-secret"}`))
	urlRecorder := httptest.NewRecorder()
	server.mihomoSubscriptionsV2(urlRecorder, urlRequest)
	if urlRecorder.Code != http.StatusOK {
		t.Fatalf("URL edit failed: %d %s", urlRecorder.Code, urlRecorder.Body.String())
	}
	urlResponse := decodeSubscriptionResponse(t, urlRecorder)
	if !urlResponse.Items[0].NeedsUpdate || urlResponse.Items[0].Cached || urlResponse.Items[0].NodeCount != 0 {
		t.Fatalf("URL edit did not invalidate cache: %#v", urlResponse.Items[0])
	}
	if strings.Contains(urlRecorder.Body.String(), "new-secret") {
		t.Fatal("subscription URL credential leaked in public response")
	}

	getRecorder := httptest.NewRecorder()
	server.mihomoSubscriptionsV2(getRecorder, httptest.NewRequest(http.MethodGet, "/api/mihomo/subscriptions", nil))
	if getRecorder.Code != http.StatusOK || !decodeSubscriptionResponse(t, getRecorder).Items[0].NeedsUpdate {
		t.Fatalf("stale config was remigrated after URL edit: %d %s", getRecorder.Code, getRecorder.Body.String())
	}
}

func TestSubscriptionNodesRequiresCacheAndDoesNotExposeCredentials(t *testing.T) {
	dataDir := t.TempDir()
	server := &Server{data: dataDir}
	rawURL := "https://example.com/sub"
	item := testSubscriptionRecord("sub", "Test", rawURL, false)
	if err := server.saveSubscriptionStore(SubscriptionStore{Version: 1, Items: []SubscriptionRecord{item}}); err != nil {
		t.Fatal(err)
	}

	missingRecorder := httptest.NewRecorder()
	server.mihomoSubscriptionNodes(missingRecorder, httptest.NewRequest(http.MethodGet, "/api/mihomo/subscriptions/nodes?id=sub", nil))
	if missingRecorder.Code != http.StatusConflict {
		t.Fatalf("missing cache status = %d, want 409", missingRecorder.Code)
	}

	item.CachedURL = rawURL
	item.NodeCount = 2
	if err := server.saveSubscriptionStore(SubscriptionStore{Version: 1, Items: []SubscriptionRecord{item}}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, server.subscriptionCachePath(item.ID), testSubscriptionYAML)
	readyRecorder := httptest.NewRecorder()
	server.mihomoSubscriptionNodes(readyRecorder, httptest.NewRequest(http.MethodGet, "/api/mihomo/subscriptions/nodes?id=sub", nil))
	if readyRecorder.Code != http.StatusOK {
		t.Fatalf("nodes request failed: %d %s", readyRecorder.Code, readyRecorder.Body.String())
	}
	if strings.Contains(readyRecorder.Body.String(), "should-never-leak") || strings.Contains(readyRecorder.Body.String(), "secret") {
		t.Fatalf("node endpoint leaked credentials: %s", readyRecorder.Body.String())
	}
}

func TestUpdateDoesNotSwitchAndActivateUsesCache(t *testing.T) {
	dataDir := t.TempDir()
	server := &Server{data: dataDir}
	firstURL := "https://first.example.com/sub"
	secondURL := "https://second.example.com/sub"
	first := testSubscriptionRecord("first", "First", firstURL, true)
	second := testSubscriptionRecord("second", "Second", secondURL, false)
	store := SubscriptionStore{Version: 1, ActiveID: first.ID, Items: []SubscriptionRecord{first, second}}
	writeTestFile(t, filepath.Join(dataDir, "mihomo", "subscription.url"), firstURL+"\n")
	writeTestFile(t, server.subscriptionCachePath(first.ID), testSubscriptionYAML)
	if err := server.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}

	type updaterCall struct {
		id                 string
		activate, useCache bool
	}
	calls := make([]updaterCall, 0, 2)
	server.subscriptionUpdater = func(item SubscriptionRecord, activate, useCache bool) ([]byte, error) {
		calls = append(calls, updaterCall{item.ID, activate, useCache})
		if !useCache {
			writeTestFile(t, server.subscriptionCachePath(item.ID), testSubscriptionYAML)
		}
		return []byte("ok"), nil
	}

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPatch, "/api/mihomo/subscriptions", strings.NewReader(`{"id":"second","action":"update"}`))
	server.mihomoSubscriptionsV2(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	updateResponse := decodeSubscriptionResponse(t, updateRecorder)
	if updateResponse.ActiveID != first.ID || strings.TrimSpace(readText(filepath.Join(dataDir, "mihomo", "subscription.url"))) != firstURL {
		t.Fatal("updating an inactive subscription changed the active subscription")
	}
	if len(calls) != 1 || calls[0] != (updaterCall{second.ID, false, false}) {
		t.Fatalf("unexpected update call: %#v", calls)
	}

	activateRecorder := httptest.NewRecorder()
	activateRequest := httptest.NewRequest(http.MethodPatch, "/api/mihomo/subscriptions", strings.NewReader(`{"id":"second","action":"activate"}`))
	server.mihomoSubscriptionsV2(activateRecorder, activateRequest)
	if activateRecorder.Code != http.StatusOK {
		t.Fatalf("activate failed: %d %s", activateRecorder.Code, activateRecorder.Body.String())
	}
	activateResponse := decodeSubscriptionResponse(t, activateRecorder)
	if activateResponse.ActiveID != second.ID || strings.TrimSpace(readText(filepath.Join(dataDir, "mihomo", "subscription.url"))) != secondURL {
		t.Fatal("activation did not switch to the cached subscription")
	}
	if len(calls) != 2 || calls[1] != (updaterCall{second.ID, true, true}) {
		t.Fatalf("activation downloaded instead of using cache: %#v", calls)
	}
}

func TestDeletingActiveSubscriptionSwitchesOnlyToValidCache(t *testing.T) {
	dataDir := t.TempDir()
	server := &Server{data: dataDir}
	firstURL := "https://first.example.com/sub"
	secondURL := "https://second.example.com/sub"
	first := testSubscriptionRecord("first", "First", firstURL, true)
	second := testSubscriptionRecord("second", "Second", secondURL, true)
	store := SubscriptionStore{Version: 1, ActiveID: first.ID, Items: []SubscriptionRecord{first, second}}
	writeTestFile(t, filepath.Join(dataDir, "mihomo", "subscription.url"), firstURL+"\n")
	writeTestFile(t, server.subscriptionCachePath(first.ID), testSubscriptionYAML)
	writeTestFile(t, server.subscriptionCachePath(second.ID), testSubscriptionYAML)
	if err := server.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}
	usedCache := false
	server.subscriptionUpdater = func(item SubscriptionRecord, activate, useCache bool) ([]byte, error) {
		usedCache = item.ID == second.ID && activate && useCache
		return []byte("ok"), nil
	}

	recorder := httptest.NewRecorder()
	server.mihomoSubscriptionsV2(recorder, httptest.NewRequest(http.MethodDelete, "/api/mihomo/subscriptions?id=first", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", recorder.Code, recorder.Body.String())
	}
	response := decodeSubscriptionResponse(t, recorder)
	if response.ActiveID != second.ID || len(response.Items) != 1 || !usedCache {
		t.Fatalf("active delete did not switch via cache: %#v usedCache=%v", response, usedCache)
	}
	if fileExistsNonEmpty(server.subscriptionCachePath(first.ID)) {
		t.Fatal("deleted subscription cache still exists")
	}
}
