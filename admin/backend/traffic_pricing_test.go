package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanelJSONPricingAdapterParsesCommonShapes(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"name":"🇯🇵 日本04","rate":1.5},{"remarks":"香港05","traffic_rate":"2x"}]}`))
	}))
	defer server.Close()

	adapter := &panelJSONPricingAdapter{client: server.Client()}
	values, err := adapter.Fetch(context.Background(), SubscriptionPricingSource{
		Endpoint: server.URL, Authorization: "panel-auth-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "panel-auth-token" {
		t.Fatalf("Authorization header = %q", authorization)
	}
	if values["日本04"] != 1500 || values["香港05"] != 2000 {
		t.Fatalf("unexpected parsed pricing: %+v", values)
	}
}

func TestPanelJSONPricingAdapterRejectsConflictsAndInvalidRates(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"conflicting canonical names", `{"nodes":[{"name":"🇯🇵 日本04","rate":1.5},{"name":"日本04","rate":2}]}`},
		{"out of range", `{"items":[{"name":"日本04","multiplier":0}]}`},
		{"empty", `{"data":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			adapter := &panelJSONPricingAdapter{client: server.Client()}
			if _, err := adapter.Fetch(context.Background(), SubscriptionPricingSource{Endpoint: server.URL}); err == nil {
				t.Fatal("expected invalid pricing payload to fail")
			}
		})
	}
}

func TestAutomaticPricingIsAuthoritativeAndBackfillsPending(t *testing.T) {
	var connections []mihomoRawConnection
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: t.TempDir(),
		FetchConnections: func(context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: connections}, nil
		},
		GetActiveSubscription: func() (string, string) { return "sub-1", "Primary" },
	})
	connections = []mihomoRawConnection{
		newMockRawConnection("c", 0, 0, []string{"🇯🇵 日本04", "Proxy"}, "example.com", "1.1.1.1", "192.168.2.10"),
	}
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	connections[0].Upload = 400
	connections[0].Download = 600
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := mgr.GetSnapshot().Proxy.UnpricedRawBytes; got != 1000 {
		t.Fatalf("pending raw bytes = %d, want 1000", got)
	}
	if _, err := mgr.UpdateAutomaticMultipliers("sub-1", map[string]uint32{"日本04": 1500}, "2026-09-22T10:00:00+08:00"); err != nil {
		t.Fatal(err)
	}
	after := mgr.GetSnapshot()
	if after.Proxy.UnpricedRawBytes != 0 || after.Proxy.WeightedBytes != 1500 {
		t.Fatalf("automatic backfill mismatch: %+v", after.Proxy)
	}
	settings := mgr.GetSettings()
	if settings.Subscriptions["sub-1"].AutomaticNodeMultipliersMilli["日本04"] != 1500 {
		t.Fatalf("automatic pricing was not persisted: %+v", settings.Subscriptions["sub-1"])
	}

	manual := uint32(2000)
	if _, err := mgr.UpdateSettings(UpdateTrafficSettingsRequest{Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
		"sub-1": {DefaultMultiplierMilli: &manual, NodeMultipliersMilli: map[string]uint32{"日本04": 2500}},
	}}); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	multiplier, status := mgr.resolveMultiplierLocked("sub-1", "🇯🇵 日本04")
	missingMultiplier, missingStatus := mgr.resolveMultiplierLocked("sub-1", "Missing Node")
	mgr.mu.Unlock()
	if multiplier == nil || *multiplier != 1500 || status != "automatic" {
		t.Fatalf("automatic authority mismatch: multiplier=%v status=%s", multiplier, status)
	}
	if missingMultiplier != nil || missingStatus != "unpriced" {
		t.Fatalf("missing provider node used a manual/default fallback: multiplier=%v status=%s", missingMultiplier, missingStatus)
	}

	if _, err := mgr.UpdateAutomaticMultipliers("sub-1", map[string]uint32{"日本04": 3000}, "2026-09-22T11:00:00+08:00"); err != nil {
		t.Fatal(err)
	}
	if got := mgr.GetSnapshot().Proxy.WeightedBytes; got != 1500 {
		t.Fatalf("already-priced history changed after automatic update: got %d", got)
	}
	settings = mgr.GetSettings()
	if len(settings.Subscriptions["sub-1"].NodeMultipliersMilli) != 0 || settings.Subscriptions["sub-1"].DefaultMultiplierMilli != nil {
		t.Fatalf("successful provider sync retained manual pricing: %+v", settings.Subscriptions["sub-1"])
	}
	if err := mgr.DisableAutomaticPricing("sub-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.UpdateSettings(UpdateTrafficSettingsRequest{Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
		"sub-1": {NodeMultipliersMilli: map[string]uint32{"日本04": 2500}},
	}}); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	multiplier, status = mgr.resolveMultiplierLocked("sub-1", "日本04")
	mgr.mu.Unlock()
	if multiplier == nil || *multiplier != 2500 || status != "node_override" {
		t.Fatalf("manual pricing did not resume after disabling automatic source: multiplier=%v status=%s", multiplier, status)
	}
}

func TestPublicSubscriptionDoesNotExposePricingCredential(t *testing.T) {
	server := &Server{data: t.TempDir()}
	now := server.publicSubscriptions(SubscriptionStore{Items: []SubscriptionRecord{{
		ID: "sub-1", Name: "Primary", URL: "https://subscription.example/secret",
		PricingSource: &SubscriptionPricingSource{
			Kind: pricingSourcePanelJSON, Endpoint: "https://panel.example/api/rates?token=secret",
			Authorization: "highly-secret", Enabled: true,
		},
	}}})
	encoded, err := json.Marshal(now)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "highly-secret") || strings.Contains(text, "token=secret") {
		t.Fatalf("public response leaked pricing credentials: %s", text)
	}
}

func TestPricingAPIKeepsExistingCredentialWhenFieldIsBlank(t *testing.T) {
	server := &Server{data: t.TempDir()}
	store := SubscriptionStore{Items: []SubscriptionRecord{{
		ID: "sub-1", Name: "Primary", URL: "https://subscription.example/value",
		PricingSource: &SubscriptionPricingSource{
			Kind: pricingSourcePanelJSON, Endpoint: "https://old.example/rates",
			Authorization: "existing-secret", Enabled: false,
		},
	}}}
	if err := server.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"id":"sub-1","kind":"panel-json","endpoint":"https://new.example/rates","authorization":"","enabled":false}`)
	req := httptest.NewRequest(http.MethodPut, "/api/mihomo/subscriptions/pricing", body)
	recorder := httptest.NewRecorder()
	server.mihomoSubscriptionPricing(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	loaded, err := server.loadSubscriptionStore()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Items[0].PricingSource.Authorization != "existing-secret" {
		t.Fatal("blank credential unexpectedly cleared the existing secret")
	}
	if strings.Contains(recorder.Body.String(), "existing-secret") {
		t.Fatal("API response exposed the stored credential")
	}
}

func TestPricingSyncKeepsLastGoodAndSeparatesSubscriptions(t *testing.T) {
	failFirst := false
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failFirst {
			http.Error(w, "temporary outage", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"name":"Node A","rate":1.5}]}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[{"tag":"Node B","ratio":"2.5"}]}`))
	}))
	defer second.Close()

	dataDir := t.TempDir()
	server := &Server{data: dataDir}
	server.trafficAccounting = NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: dataDir + "/accounting",
	})
	store := SubscriptionStore{Items: []SubscriptionRecord{
		{ID: "sub-a", Name: "A", PricingSource: &SubscriptionPricingSource{Kind: pricingSourcePanelJSON, Endpoint: first.URL, Enabled: true}},
		{ID: "sub-b", Name: "B", PricingSource: &SubscriptionPricingSource{Kind: pricingSourcePanelJSON, Endpoint: second.URL, Enabled: true}},
	}}
	if err := server.saveSubscriptionStore(store); err != nil {
		t.Fatal(err)
	}
	if err := server.syncSubscriptionPricing("sub-a"); err != nil {
		t.Fatal(err)
	}
	if err := server.syncSubscriptionPricing("sub-b"); err != nil {
		t.Fatal(err)
	}
	settings := server.trafficAccounting.GetSettings()
	if settings.Subscriptions["sub-a"].AutomaticNodeMultipliersMilli["Node A"] != 1500 {
		t.Fatalf("subscription A pricing mismatch: %+v", settings.Subscriptions["sub-a"])
	}
	if settings.Subscriptions["sub-b"].AutomaticNodeMultipliersMilli["Node B"] != 2500 {
		t.Fatalf("subscription B pricing mismatch: %+v", settings.Subscriptions["sub-b"])
	}

	failFirst = true
	if err := server.syncSubscriptionPricing("sub-a"); err == nil {
		t.Fatal("expected failed refresh")
	}
	settings = server.trafficAccounting.GetSettings()
	if settings.Subscriptions["sub-a"].AutomaticNodeMultipliersMilli["Node A"] != 1500 {
		t.Fatal("failed refresh discarded the last-good automatic pricing")
	}
	loaded, err := server.loadSubscriptionStore()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Items[0].PricingSource.LastSyncedAt == "" || loaded.Items[0].PricingSource.LastError == "" {
		t.Fatalf("sync status was not retained: %+v", loaded.Items[0].PricingSource)
	}
}
