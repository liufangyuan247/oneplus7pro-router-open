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
