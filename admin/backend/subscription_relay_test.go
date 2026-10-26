package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"
)

func relayManagement(t *testing.T, s *Server, id, action string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/mihomo/subscriptions/relay?id="+id, nil)
	if action != "" {
		req = httptest.NewRequest(http.MethodPost, "/api/mihomo/subscriptions/relay", strings.NewReader(`{"id":"`+id+`","action":"`+action+`"}`))
	}
	w := httptest.NewRecorder()
	s.mihomoSubscriptionRelay(w, req)
	return w
}

func responseRelayPath(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("management status=%d body=%s", response.Code, response.Body.String())
	}
	var data struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data.Path
}

func fetchRelay(s *Server, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.subscriptionRelay(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestRelayRepeatedReadsReturnOriginalDocumentWithoutContactingUpstream(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "expired one-hour subscription", http.StatusGone)
	}))
	defer upstream.Close()
	s := &Server{data: t.TempDir(), subscriptionUpdater: func(SubscriptionRecord, bool, bool) ([]byte, error) {
		t.Error("relay must not invoke updater")
		return nil, nil
	}}
	item := testSubscriptionRecord("cached", "示例订阅", upstream.URL+"/private?token=origin-secret", true)
	item.AutoUpdateEnabled = new(bool)
	store := SubscriptionStore{ActiveID: item.ID, Items: []SubscriptionRecord{item}}
	if err := s.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}
	raw := "# Original provider comments and formatting must survive.\r\n" + testSubscriptionYAML + `secret: original-controller-secret
external-controller: 0.0.0.0:9091
dns:
  enable: true
  nameserver: [1.1.1.1, 8.8.8.8]
proxy-groups:
  - {name: Original Select, type: select, proxies: [Original Auto, JP Block, HK Inline]}
  - {name: Original Auto, type: url-test, proxies: [JP Block, HK Inline], url: 'https://example.com/test', interval: 300}
rules:
  - DOMAIN-SUFFIX,example.com,Original Select
  - RULE-SET,private,DIRECT
  - MATCH,Original Select
proxy-providers:
  private: {type: http, url: "` + item.URL + `"}
rule-providers:
  private: {type: http, url: "https://rules.invalid/private"}
`
	cachePath := s.subscriptionCachePath(item.ID)
	writeTestFile(t, cachePath, raw)
	path := responseRelayPath(t, relayManagement(t, s, item.ID, "enable"))
	if len(strings.TrimSuffix(strings.Split(path, "/")[3], ".yaml")) != 43 {
		t.Fatal("expected a 256-bit capability")
	}
	for i := 0; i < 8; i++ {
		response := fetchRelay(s, path)
		if response.Code != http.StatusOK {
			t.Fatalf("read %d: status=%d", i, response.Code)
		}
		var config struct {
			Proxies []map[string]any `yaml:"proxies"`
			Groups  []map[string]any `yaml:"proxy-groups"`
			Rules   []string         `yaml:"rules"`
		}
		if err := yaml.Unmarshal(response.Body.Bytes(), &config); err != nil {
			t.Fatal(err)
		}
		if len(config.Proxies) != 2 || len(config.Groups) != 2 || len(config.Rules) != 3 {
			t.Fatal("original nodes, groups or rules were lost")
		}
		if scalarText(config.Proxies[0]["uuid"]) != "should-never-leak" {
			t.Fatal("node credentials must remain usable")
		}
		if !bytes.Equal(response.Body.Bytes(), []byte(raw)) {
			t.Fatal("relay did not return the original cache byte for byte")
		}
		_, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
		if err != nil || parameters["filename"] != "示例订阅（中转）.yaml" {
			t.Fatal("download name does not match the router subscription")
		}
		title, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(response.Header().Get("Profile-Title"), "base64:"))
		if err != nil || string(title) != "示例订阅（中转）" {
			t.Fatal("incorrect client profile title")
		}
	}
	if requests.Load() != 0 {
		t.Fatal("relay contacted upstream")
	}
	if after, _ := os.ReadFile(cachePath); string(after) != raw {
		t.Fatal("relay changed cache")
	}
	loaded, err := s.loadSubscriptionStore()
	if err != nil {
		t.Fatal(err)
	}
	after := loaded.Items[0]
	if loaded.ActiveID != store.ActiveID || after.URL != item.URL || !after.UpdatedAt.Equal(item.UpdatedAt) || after.IsAutoUpdateEnabled() {
		t.Fatal("relay changed subscription or update settings")
	}
	list, _ := json.Marshal(s.publicSubscriptions(loaded))
	if bytes.Contains(list, []byte(after.RelayToken)) || bytes.Contains(list, []byte("origin-secret")) {
		t.Fatal("list leaked capability or provider URL")
	}
	// The capability survives an admin process restart, and sees locally replaced nodes.
	restarted := &Server{data: s.data}
	writeTestFile(t, cachePath, strings.ReplaceAll(testSubscriptionYAML, "JP Block", "New JP"))
	if response := fetchRelay(restarted, path); response.Code != 200 || !strings.Contains(response.Body.String(), "New JP") {
		t.Fatal("persistent link did not follow the local cache")
	}
}

func TestRelayRevocationIsolationMissingCacheAndManagementAuth(t *testing.T) {
	s := &Server{data: t.TempDir()}
	a := testSubscriptionRecord("a", "First", "https://a.invalid/expired", true)
	b := testSubscriptionRecord("b", "Second", "https://b.invalid/expired", true)
	if err := s.saveSubscriptionStore(SubscriptionStore{Items: []SubscriptionRecord{a, b}}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, s.subscriptionCachePath(a.ID), testSubscriptionYAML)
	writeTestFile(t, s.subscriptionCachePath(b.ID), testSubscriptionYAML)
	first := responseRelayPath(t, relayManagement(t, s, a.ID, "enable"))
	second := responseRelayPath(t, relayManagement(t, s, b.ID, "enable"))
	if same := responseRelayPath(t, relayManagement(t, s, a.ID, "enable")); same != first {
		t.Fatal("enable must preserve existing URLs")
	}
	rotated := responseRelayPath(t, relayManagement(t, s, a.ID, "rotate"))
	if rotated == first || fetchRelay(s, first).Code != 404 || fetchRelay(s, second).Code != 200 {
		t.Fatal("rotation did not isolate subscriptions")
	}
	if response := fetchRelay(s, strings.Replace(rotated, "/a/", "/b/", 1)); response.Code != 404 {
		t.Fatal("capability worked for another subscription")
	}
	if responseRelayPath(t, relayManagement(t, s, a.ID, "disable")) != "" || fetchRelay(s, rotated).Code != 404 {
		t.Fatal("disabled URL remains usable")
	}
	if err := os.Remove(s.subscriptionCachePath(b.ID)); err != nil {
		t.Fatal(err)
	}
	if fetchRelay(s, second).Code != 503 || relayManagement(t, s, b.ID, "enable").Code != 409 {
		t.Fatal("missing cache should fail without fetching")
	}
	request := httptest.NewRequest(http.MethodGet, "http://router.local/api/mihomo/subscriptions/relay?id=a", nil)
	request.RemoteAddr = "192.168.2.30:5555"
	w := httptest.NewRecorder()
	s.auth(s.mihomoSubscriptionRelay)(w, request)
	if w.Code != 401 {
		t.Fatal("management endpoint must require admin authentication")
	}
}

func TestRelayQRAndMetadataNodes(t *testing.T) {
	s := &Server{data: t.TempDir()}
	a := testSubscriptionRecord("qr", "QR", "https://origin.invalid/secret", true)
	if err := s.saveSubscriptionStore(SubscriptionStore{Items: []SubscriptionRecord{a}}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, s.subscriptionCachePath(a.ID), testSubscriptionYAML+"  - {name: 剩余流量, type: ss, server: info.invalid, port: 443}\n")
	path := responseRelayPath(t, relayManagement(t, s, a.ID, "enable"))
	original, err := os.ReadFile(s.subscriptionCachePath(a.ID))
	if err != nil || !bytes.Equal(fetchRelay(s, path).Body.Bytes(), original) {
		t.Fatal("original metadata nodes were modified")
	}
	legacy := subscriptionRelayPrefix + a.ID + "/" + strings.Split(path, "/")[3] + ".yaml"
	if response := fetchRelay(s, legacy); response.Code != 200 || !bytes.Equal(response.Body.Bytes(), original) {
		t.Fatal("previously imported relay URL stopped working")
	}
	w := httptest.NewRecorder()
	s.mihomoSubscriptionRelay(w, httptest.NewRequest(http.MethodGet, "http://192.0.2.1:9090/api/mihomo/subscriptions/relay?id=qr&qr=1", nil))
	image, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || image.Bounds().Dx() != 320 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("invalid local QR: %v", err)
	}
	for _, method := range []string{http.MethodHead, http.MethodPost} {
		w := httptest.NewRecorder()
		s.subscriptionRelay(w, httptest.NewRequest(method, path, nil))
		if method == http.MethodHead && (w.Code != 200 || w.Body.Len() != 0) {
			t.Fatal("HEAD must have no body")
		}
		if method == http.MethodPost && w.Code != 405 {
			t.Fatal("public relay must be read-only")
		}
	}
	loaded, _ := s.loadSubscriptionStore()
	loaded.Items[0].URL = "https://new.invalid/changed"
	if err := s.saveSubscriptionStore(loaded); err != nil {
		t.Fatal(err)
	}
	if fetchRelay(s, path).Code != 503 {
		t.Fatal("stale cache for edited URL must not be served")
	}
}
