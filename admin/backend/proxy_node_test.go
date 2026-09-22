package main

import "testing"

func TestSubscriptionMetadataNodeDetection(t *testing.T) {
	tests := []struct {
		name     string
		metadata bool
	}{
		{"剩余流量：171.49 GB", true},
		{"套餐：旗舰版", true},
		{"到期时间 2026-12-31", true},
		{"Traffic Remaining: 10 GB", true},
		{"Reset Day: 1", true},
		{"官网：https://example.com", true},
		{"香港 | 05", false},
		{"美国17 | 直连 | 三网优化", false},
		{"不限流量 | 香港", false},
	}
	for _, test := range tests {
		if got := isSubscriptionMetadataNode(test.name); got != test.metadata {
			t.Errorf("isSubscriptionMetadataNode(%q) = %v, want %v", test.name, got, test.metadata)
		}
	}
}

func TestMetadataNodeUsesZeroCostWithoutPendingPricing(t *testing.T) {
	manager := &TrafficAccountingManager{
		settings: TrafficAccountingSettings{Subscriptions: map[string]*SubscriptionAccountingConfig{}},
	}
	multiplier, status := manager.resolveMultiplierLocked("sub-1", "剩余流量：10 GB")
	if multiplier == nil || *multiplier != 0 || status != "metadata" {
		t.Fatalf("metadata pricing = (%v, %q), want (0, metadata)", multiplier, status)
	}
}

func TestDiscardMetadataPendingKeepsRawHistory(t *testing.T) {
	pending := newPendingNodePricing("sub-1", "剩余流量：10 GB")
	pending.Proxy = DirectionalBytes{UploadBytes: 100, DownloadBytes: 200}
	pending.Devices["device"] = pending.Proxy
	nodeKey := nodeCompositeKey("sub-1", "剩余流量：10 GB")
	pending.Nodes[nodeKey] = pending.Proxy

	manager := &TrafficAccountingManager{
		ledger: TrafficLedger{
			Proxy: TrafficStats{UploadBytes: 100, DownloadBytes: 200, RawBytes: 300, UnpricedRawBytes: 300},
			Devices: map[string]*DeviceAccounting{
				"device": {IP: "device", TrafficStats: TrafficStats{UploadBytes: 100, DownloadBytes: 200, RawBytes: 300, UnpricedRawBytes: 300}},
			},
			Nodes: map[string]*NodeAccounting{
				nodeKey: {Node: "剩余流量：10 GB", SubscriptionID: "sub-1", PricingStatus: "unpriced", TrafficStats: TrafficStats{UploadBytes: 100, DownloadBytes: 200, RawBytes: 300, UnpricedRawBytes: 300}},
			},
			PendingPricing: map[string]*PendingNodePricing{nodeKey: pending},
		},
	}

	if !manager.discardMetadataPendingLocked() {
		t.Fatal("metadata pending entry was not discarded")
	}
	if len(manager.ledger.PendingPricing) != 0 {
		t.Fatalf("pending entries remain: %#v", manager.ledger.PendingPricing)
	}
	if manager.ledger.Proxy.RawBytes != 300 || manager.ledger.Proxy.UnpricedRawBytes != 0 {
		t.Fatalf("proxy history was rewritten incorrectly: %#v", manager.ledger.Proxy)
	}
	device := manager.ledger.Devices["device"]
	if device.RawBytes != 300 || device.UnpricedRawBytes != 0 {
		t.Fatalf("device history was rewritten incorrectly: %#v", device)
	}
	node := manager.ledger.Nodes[nodeKey]
	if node.RawBytes != 300 || node.UnpricedRawBytes != 0 || node.PricingStatus != "metadata" {
		t.Fatalf("node metadata cleanup incorrect: %#v", node)
	}
}

func TestNormalizeAccountingConfigRemovesMetadataNodes(t *testing.T) {
	cfg := &SubscriptionAccountingConfig{
		KnownNodes: []string{"香港 | 05", "剩余流量：10 GB"},
		NodeMultipliersMilli: map[string]uint32{
			"香港 | 05":    2000,
			"剩余流量：10 GB": 1000,
		},
		AutomaticNodeMultipliersMilli: map[string]uint32{
			"香港 | 05":    2000,
			"剩余流量：10 GB": 1000,
		},
	}
	if !normalizeSubscriptionAccountingConfig(cfg) {
		t.Fatal("metadata configuration was not normalized")
	}
	if len(cfg.KnownNodes) != 1 || cfg.KnownNodes[0] != "香港 | 05" {
		t.Fatalf("unexpected known nodes: %#v", cfg.KnownNodes)
	}
	if len(cfg.NodeMultipliersMilli) != 1 || len(cfg.AutomaticNodeMultipliersMilli) != 1 {
		t.Fatalf("metadata multipliers remain: manual=%#v automatic=%#v", cfg.NodeMultipliersMilli, cfg.AutomaticNodeMultipliersMilli)
	}
}
