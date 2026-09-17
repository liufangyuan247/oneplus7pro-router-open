package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newMockRawConnection(id string, up, down uint64, chains []string, host, dstIP, srcIP string) mihomoRawConnection {
	var c mihomoRawConnection
	c.ID = id
	c.Upload = up
	c.Download = down
	c.Chains = chains
	c.Metadata.Host = host
	c.Metadata.DestinationIP = dstIP
	c.Metadata.SourceIP = srcIP
	return c
}

// 1. First snapshot baseline test: first poll only establishes baseline, 0 bytes counted.
func TestBaselineOnlyOnFirstPoll(t *testing.T) {
	tempDir := t.TempDir()
	conns := []mihomoRawConnection{
		newMockRawConnection("c1", 1000, 2000, []string{"HK-01", "Proxy"}, "example.com", "1.2.3.4", "192.168.2.50"),
	}

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: conns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub1", "SubOne"
		},
	})

	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatalf("first poll failed: %v", err)
	}

	snap := mgr.GetSnapshot()
	if snap.TotalObserved.RawBytes != 0 {
		t.Fatalf("expected 0 bytes on first poll baseline, got %d", snap.TotalObserved.RawBytes)
	}
	if snap.TotalObserved.ConnectionCount != 0 {
		t.Fatalf("expected 0 connection count on first poll baseline, got %d", snap.TotalObserved.ConnectionCount)
	}
}

// 2. Continuous incremental delta test: delta counted exactly once, no duplicate accounting.
func TestIncrementalCountingExactOnce(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub1", "SubOne"
		},
	})

	// Poll 1: Baseline established (1000 up, 2000 down)
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 1000, 2000, []string{"HK-01", "Proxy"}, "example.com", "1.2.3.4", "192.168.2.50"),
	}
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Poll 2: Delta 500 up, 1000 down -> 1500 raw delta
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 1500, 3000, []string{"HK-01", "Proxy"}, "example.com", "1.2.3.4", "192.168.2.50"),
	}
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}

	snap := mgr.GetSnapshot()
	if snap.TotalObserved.UploadBytes != 500 || snap.TotalObserved.DownloadBytes != 1000 || snap.TotalObserved.RawBytes != 1500 {
		t.Fatalf("expected 1500 raw bytes, got up=%d, down=%d, raw=%d",
			snap.TotalObserved.UploadBytes, snap.TotalObserved.DownloadBytes, snap.TotalObserved.RawBytes)
	}

	// Poll 3: No change in counters
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap = mgr.GetSnapshot()
	if snap.TotalObserved.RawBytes != 1500 {
		t.Fatalf("expected unchanged 1500 raw bytes on no-op poll, got %d", snap.TotalObserved.RawBytes)
	}
}

// 3. New connection initial bytes counted when appearing in running state.
func TestNewConnectionInitialBytesCounted(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub1", "SubOne"
		},
	})

	// Poll 1: Baseline with c1
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 100, 100, []string{"HK-01", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.50"),
	}
	_ = mgr.Poll(context.Background())

	// Poll 2: c2 appears with initial 300 up, 700 down
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 100, 100, []string{"HK-01", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.50"),
		newMockRawConnection("c2", 300, 700, []string{"HK-01", "Proxy"}, "b.com", "2.2.2.2", "192.168.2.51"),
	}
	_ = mgr.Poll(context.Background())

	snap := mgr.GetSnapshot()
	if snap.TotalObserved.RawBytes != 1000 {
		t.Fatalf("expected 1000 raw bytes from initial c2, got %d", snap.TotalObserved.RawBytes)
	}
	if snap.TotalObserved.ConnectionCount != 1 {
		t.Fatalf("expected 1 counted connection, got %d", snap.TotalObserved.ConnectionCount)
	}
}

// 4. Closed connections purged, counter rollback handled, Mihomo offline & recovery.
func TestRollbackAndOfflineRecovery(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection
	offline := false

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			if offline {
				return nil, errors.New("connection refused")
			}
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
	})

	// Establish baseline
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 500, 500, []string{"DIRECT"}, "", "1.1.1.1", "192.168.2.50"),
	}
	_ = mgr.Poll(context.Background())

	// Normal increment
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 600, 600, []string{"DIRECT"}, "", "1.1.1.1", "192.168.2.50"),
	}
	_ = mgr.Poll(context.Background())
	if mgr.GetSnapshot().TotalObserved.RawBytes != 200 {
		t.Fatalf("expected 200 bytes, got %d", mgr.GetSnapshot().TotalObserved.RawBytes)
	}

	// Counter rollback (e.g. ID reused or reset)
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 100, 100, []string{"DIRECT"}, "", "1.1.1.1", "192.168.2.50"),
	}
	_ = mgr.Poll(context.Background())
	// Should not decrease or add giant overflow
	if mgr.GetSnapshot().TotalObserved.RawBytes != 200 {
		t.Fatalf("expected unchanged 200 bytes on counter rollback, got %d", mgr.GetSnapshot().TotalObserved.RawBytes)
	}

	// Offline simulation
	offline = true
	err := mgr.Poll(context.Background())
	if err == nil {
		t.Fatal("expected poll failure during offline")
	}
	snap := mgr.GetSnapshot()
	if snap.Online != false || snap.PollFailCount != 1 || snap.ConsecutiveFailures != 1 {
		t.Fatalf("expected offline status, got online=%v, fails=%d", snap.Online, snap.PollFailCount)
	}

	// Recovery simulation: first poll after offline must establish baseline
	offline = false
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 99999, 99999, []string{"DIRECT"}, "", "1.1.1.1", "192.168.2.50"),
	}
	_ = mgr.Poll(context.Background())
	// Must not have added (99999-100) delta!
	if mgr.GetSnapshot().TotalObserved.RawBytes != 200 {
		t.Fatalf("recovery poll should not count jump delta, got %d bytes", mgr.GetSnapshot().TotalObserved.RawBytes)
	}
}

// 5. Route classification test: DIRECT, REJECT, and Proxy chains.
func TestRouteClassification(t *testing.T) {
	tests := []struct {
		chains        []string
		wantKind      string
		wantLeaf      string
		wantGroup     string
		wantSpeedtest bool
	}{
		{chains: nil, wantKind: "direct", wantLeaf: "", wantGroup: "", wantSpeedtest: false},
		{chains: []string{"DIRECT"}, wantKind: "direct", wantLeaf: "", wantGroup: "", wantSpeedtest: false},
		{chains: []string{"REJECT"}, wantKind: "reject", wantLeaf: "", wantGroup: "", wantSpeedtest: false},
		{chains: []string{"NodeA", "ProxyGroup1", "GLOBAL"}, wantKind: "proxy", wantLeaf: "NodeA", wantGroup: "ProxyGroup1", wantSpeedtest: false},
		{chains: []string{"NodeB", "家庭测速"}, wantKind: "proxy", wantLeaf: "NodeB", wantGroup: "家庭测速", wantSpeedtest: true},
		{chains: []string{"NodeC"}, wantKind: "proxy", wantLeaf: "NodeC", wantGroup: "", wantSpeedtest: false},
	}

	for _, tt := range tests {
		kind, leaf, group, speedtest := classifyRoute(tt.chains)
		if kind != tt.wantKind || leaf != tt.wantLeaf || group != tt.wantGroup || speedtest != tt.wantSpeedtest {
			t.Errorf("classifyRoute(%v) = (%s, %s, %s, %v), want (%s, %s, %s, %v)",
				tt.chains, kind, leaf, group, speedtest, tt.wantKind, tt.wantLeaf, tt.wantGroup, tt.wantSpeedtest)
		}
	}
}

// 6. Subscription switch isolation test: connection attribution remains isolated.
func TestSubscriptionIsolationOnSwitch(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection
	activeSubID := "sub-A"
	activeSubName := "Subscription Alpha"

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return activeSubID, activeSubName
		},
	})

	// Establish baseline
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 100, 100, []string{"HK-01", "Proxy"}, "example.com", "1.1.1.1", "192.168.2.10"),
	}
	_ = mgr.Poll(context.Background())

	// c1 increments under sub-A
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 200, 200, []string{"HK-01", "Proxy"}, "example.com", "1.1.1.1", "192.168.2.10"),
	}
	_ = mgr.Poll(context.Background())

	// Switch active subscription to sub-B
	activeSubID = "sub-B"
	activeSubName = "Subscription Beta"

	// c1 continues to transfer data; c2 is a new connection created under sub-B
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 300, 300, []string{"HK-01", "Proxy"}, "example.com", "1.1.1.1", "192.168.2.10"),
		newMockRawConnection("c2", 50, 50, []string{"US-01", "Proxy"}, "test.com", "2.2.2.2", "192.168.2.11"),
	}
	_ = mgr.Poll(context.Background())

	snap := mgr.GetSnapshot()
	var subA, subB *SubscriptionAccounting
	for _, s := range snap.Subscriptions {
		if s.ID == "sub-A" {
			sc := s
			subA = &sc
		} else if s.ID == "sub-B" {
			sc := s
			subB = &sc
		}
	}

	if subA == nil || subB == nil {
		t.Fatalf("expected both sub-A and sub-B in stats, got %+v", snap.Subscriptions)
	}
	// c1: 200 raw from poll 2, 200 raw from poll 3 -> 400 total for sub-A
	if subA.RawBytes != 400 {
		t.Errorf("sub-A rawBytes = %d, want 400", subA.RawBytes)
	}
	// c2: initial 100 raw for sub-B
	if subB.RawBytes != 100 {
		t.Errorf("sub-B rawBytes = %d, want 100", subB.RawBytes)
	}
}

// 7. Multi-dimension aggregation test: device, node, group, target, hourly, daily.
func TestDimensionAggregations(t *testing.T) {
	tempDir := t.TempDir()
	fixedTime := time.Date(2026, 9, 14, 12, 30, 0, 0, time.UTC)
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub-1", "TestSub"
		},
		LookupClientHost: func(ip string) string {
			if ip == "192.168.2.100" {
				return "MyPhone"
			}
			return ip
		},
		NowFunc: func() time.Time {
			return fixedTime
		},
	})

	// Baseline
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 0, 0, []string{"Node1", "GroupA"}, "Api.GitHub.Com:443", "140.82.114.4", "192.168.2.100"),
	}
	_ = mgr.Poll(context.Background())

	// Poll: transfer 500 up, 1500 down
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 500, 1500, []string{"Node1", "GroupA"}, "Api.GitHub.Com:443", "140.82.114.4", "192.168.2.100"),
	}
	_ = mgr.Poll(context.Background())

	snap := mgr.GetSnapshot()

	// Device check
	if len(snap.Devices) != 1 || snap.Devices[0].Hostname != "MyPhone" || snap.Devices[0].RawBytes != 2000 {
		t.Errorf("unexpected device accounting: %+v", snap.Devices)
	}

	// Node check
	if len(snap.Nodes) != 1 || snap.Nodes[0].Node != "Node1" || snap.Nodes[0].RawBytes != 2000 {
		t.Errorf("unexpected node accounting: %+v", snap.Nodes)
	}

	// Group check
	if len(snap.Groups) != 1 || snap.Groups[0].Group != "GroupA" || snap.Groups[0].RawBytes != 2000 {
		t.Errorf("unexpected group accounting: %+v", snap.Groups)
	}

	// Target check (must be lowercased, port stripped)
	if len(snap.Targets) != 1 || snap.Targets[0].Target != "api.github.com" || snap.Targets[0].RawBytes != 2000 {
		t.Errorf("unexpected target accounting: %+v", snap.Targets)
	}

	// Time check
	if len(snap.Hourly) != 1 || snap.Hourly[0].RawBytes != 2000 {
		t.Errorf("unexpected hourly accounting: %+v", snap.Hourly)
	}
	if len(snap.Daily) != 1 || snap.Daily[0].Time != "2026-09-14" || snap.Daily[0].RawBytes != 2000 {
		t.Errorf("unexpected daily accounting: %+v", snap.Daily)
	}
}

// 8. Speedtest separate line test: speedtest total separated, while still in global/node/sub totals.
func TestSpeedtestSeparateLine(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub-1", "Sub"
		},
	})

	// Baseline
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 0, 0, []string{"HK-01", "家庭测速"}, "speed.cloudflare.com", "1.1.1.1", "192.168.2.10"),
	}
	_ = mgr.Poll(context.Background())

	// Transfer 100 up, 900 down
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 100, 900, []string{"HK-01", "家庭测速"}, "speed.cloudflare.com", "1.1.1.1", "192.168.2.10"),
	}
	_ = mgr.Poll(context.Background())

	snap := mgr.GetSnapshot()
	if snap.Speedtest.RawBytes != 1000 {
		t.Errorf("speedtest rawBytes = %d, want 1000", snap.Speedtest.RawBytes)
	}
	if snap.TotalObserved.RawBytes != 1000 {
		t.Errorf("totalObserved rawBytes = %d, want 1000", snap.TotalObserved.RawBytes)
	}
	if snap.Proxy.RawBytes != 1000 {
		t.Errorf("proxy rawBytes = %d, want 1000", snap.Proxy.RawBytes)
	}
}

// 9. Multipliers and pricing test: node override > sub default > unpriced, and integer scaling.
func TestMultipliersAndPricing(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub-test", "SubTest"
		},
	})

	// Setup custom multipliers: default 1.5x (1500), node "NodeOverride" 2.0x (2000)
	defaultMult := uint32(1500)
	_, err := mgr.UpdateSettings(UpdateTrafficSettingsRequest{
		Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
			"sub-test": {
				DefaultMultiplierMilli: &defaultMult,
				NodeMultipliersMilli: map[string]uint32{
					"NodeOverride": 2000,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to set multipliers: %v", err)
	}

	// Baseline
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c-node", 0, 0, []string{"NodeOverride", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
		newMockRawConnection("c-default", 0, 0, []string{"OtherNode", "Proxy"}, "b.com", "2.2.2.2", "192.168.2.1"),
	}
	_ = mgr.Poll(context.Background())

	// Transfer:
	// c-node: 1000 raw bytes -> weighted at 2.0x = 2000
	// c-default: 1000 raw bytes -> weighted at 1.5x = 1500
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c-node", 500, 500, []string{"NodeOverride", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
		newMockRawConnection("c-default", 500, 500, []string{"OtherNode", "Proxy"}, "b.com", "2.2.2.2", "192.168.2.1"),
	}
	_ = mgr.Poll(context.Background())

	snap := mgr.GetSnapshot()
	if snap.TotalObserved.RawBytes != 2000 {
		t.Fatalf("expected 2000 raw bytes, got %d", snap.TotalObserved.RawBytes)
	}
	if snap.Proxy.WeightedBytes != 3500 {
		t.Fatalf("expected 3500 weighted bytes (2000 + 1500), got %d", snap.Proxy.WeightedBytes)
	}

	// Flag normalization test
	flagNode := "🇺🇸 美国17 | 直连 | 三网优化"
	norm := normalizeNodeName(flagNode)
	if norm != "美国17 | 直连 | 三网优化" {
		t.Errorf("flag normalization failed: got %q", norm)
	}
}

// 10. Settings update future-only test: updating multipliers does not alter historical stats.
func TestSettingsUpdateFutureOnly(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub-1", "Sub"
		},
	})

	// Initial default 1.0x (1000)
	mult1 := uint32(1000)
	_, _ = mgr.UpdateSettings(UpdateTrafficSettingsRequest{
		Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
			"sub-1": {DefaultMultiplierMilli: &mult1},
		},
	})

	// Baseline
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 0, 0, []string{"NodeX", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
	}
	_ = mgr.Poll(context.Background())

	// Step 1: 1000 raw bytes at 1.0x -> 1000 weighted
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 500, 500, []string{"NodeX", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
	}
	_ = mgr.Poll(context.Background())

	if mgr.GetSnapshot().Proxy.WeightedBytes != 1000 {
		t.Fatalf("expected 1000 weighted, got %d", mgr.GetSnapshot().Proxy.WeightedBytes)
	}

	// Update settings to 2.0x (2000)
	mult2 := uint32(2000)
	_, _ = mgr.UpdateSettings(UpdateTrafficSettingsRequest{
		Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
			"sub-1": {DefaultMultiplierMilli: &mult2},
		},
	})

	// Verify history is not modified
	if mgr.GetSnapshot().Proxy.WeightedBytes != 1000 {
		t.Fatalf("history should not be altered by settings update, got %d", mgr.GetSnapshot().Proxy.WeightedBytes)
	}

	// Step 2: Next 1000 raw bytes should be scaled by 2.0x -> +2000 weighted, total 3000
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 1000, 1000, []string{"NodeX", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
	}
	_ = mgr.Poll(context.Background())

	if mgr.GetSnapshot().Proxy.WeightedBytes != 3000 {
		t.Fatalf("expected 3000 weighted bytes (1000 + 2000), got %d", mgr.GetSnapshot().Proxy.WeightedBytes)
	}
}

// 11. Concurrent Reset and Poll test: race safety and correct baseline re-establishment.
func TestConcurrentResetAndPoll(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			conns := []mihomoRawConnection{
				newMockRawConnection("c1", 100, 100, []string{"HK-01", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
			}
			return &mihomoRawConnectionsResponse{Connections: conns}, nil
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = mgr.Poll(context.Background())
		}()
		go func() {
			defer wg.Done()
			_, _ = mgr.Reset()
		}()
	}
	wg.Wait()

	// Ensure manager remains in consistent state
	snap := mgr.GetSnapshot()
	if snap.StartedAt == "" {
		t.Fatal("startedAt should not be empty")
	}
}

// 12. Atomic persistence, 0600 file permission, corruption recovery, and restart continuity.
func TestAtomicPersistenceAndCorruptionRecovery(t *testing.T) {
	tempDir := t.TempDir()
	ledgerPath := filepath.Join(tempDir, "traffic-accounting.json")

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			conns := []mihomoRawConnection{
				newMockRawConnection("c1", 50, 50, []string{"DIRECT"}, "", "1.1.1.1", "192.168.2.1"),
			}
			return &mihomoRawConnectionsResponse{Connections: conns}, nil
		},
	})

	// Poll and trigger flush
	_ = mgr.Poll(context.Background())
	_ = mgr.Poll(context.Background())
	mgr.mu.Lock()
	_ = mgr.persistLedgerLocked()
	mgr.mu.Unlock()

	// Verify file exists
	fi, err := os.Stat(ledgerPath)
	if err != nil {
		t.Fatalf("ledger file does not exist: %v", err)
	}
	// Verify file permission is 0600 (or on Windows where modebits differ, file exists and readable)
	if fi.Size() == 0 {
		t.Fatal("ledger file is empty")
	}

	// Corrupt file test
	_ = os.WriteFile(ledgerPath, []byte("NOT_VALID_JSON{"), 0600)

	// Reinitialize manager
	restartedMgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
	})

	snap := restartedMgr.GetSnapshot()
	if snap.Warning == "" || !strings.Contains(snap.Warning, "损坏") {
		t.Fatalf("expected corruption warning on corrupted ledger, got: %s", snap.Warning)
	}

	// Verify corrupt backup was created
	matches, _ := filepath.Glob(filepath.Join(tempDir, "traffic-accounting.json.corrupt-*"))
	if len(matches) == 0 {
		t.Fatal("expected .corrupt-* backup file to be created")
	}
}

// 13. Dimension caps and overflow into _other test.
func TestCapsAndOverflowToOther(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
	})

	// Fill devices to limit
	mgr.mu.Lock()
	for i := 0; i < maxAccountingDevices; i++ {
		ip := fmt.Sprintf("192.168.100.%d", i)
		mgr.ledger.Devices[ip] = &DeviceAccounting{IP: ip, Hostname: "dev"}
	}
	mgr.hasBaseline = true
	mgr.mu.Unlock()

	// A new connection from device (maxAccountingDevices + 1)
	currentConns = []mihomoRawConnection{
		newMockRawConnection("overflow-c", 100, 200, []string{"DIRECT"}, "", "1.1.1.1", "192.168.200.99"),
	}
	_ = mgr.Poll(context.Background())

	snap := mgr.GetSnapshot()
	otherFound := false
	for _, dev := range snap.Devices {
		if dev.IP == otherKey {
			otherFound = true
			if dev.RawBytes != 300 {
				t.Errorf("expected 300 raw bytes in _other device, got %d", dev.RawBytes)
			}
		}
	}
	if !otherFound {
		t.Errorf("expected _other device to capture overflow, devices: %+v", snap.Devices)
	}
}

// 14. API credential and sensitive data secrecy test: no subscription URLs or raw tokens.
func TestNoCredentialLeakage(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
	})

	snap := mgr.GetSnapshot()
	data, _ := json.Marshal(snap)
	str := string(data)

	sensitiveKeywords := []string{"http://", "https://", "token", "secret", "password", "Bearer"}
	for _, kw := range sensitiveKeywords {
		if strings.Contains(strings.ToLower(str), strings.ToLower(kw)) {
			t.Errorf("snapshot leaks sensitive keyword %q: %s", kw, str)
		}
	}
}

// 15. Limits and overflow safety test: snapshot > 10,000 connections rejected, math safety.
func TestLimitsAndOverflowSafety(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			// Generate 10001 connections
			conns := make([]mihomoRawConnection, maxAccountingConnections+1)
			return &mihomoRawConnectionsResponse{Connections: conns}, nil
		},
	})

	err := mgr.Poll(context.Background())
	if err == nil {
		t.Fatal("expected poll rejection when connection count exceeds limit")
	}

	snap := mgr.GetSnapshot()
	if snap.RejectedSnapshots != 1 {
		t.Fatalf("expected rejectedSnapshots = 1, got %d", snap.RejectedSnapshots)
	}

	// Test scaleBytes safety on extreme values
	scaled := scaleBytes(math.MaxUint64, 2000)
	if scaled != math.MaxUint64 {
		t.Errorf("expected scaleBytes clamp on extreme value, got %d", scaled)
	}

	// Test safeAddUint64 on overflow
	added := safeAddUint64(math.MaxUint64-5, 10)
	if added != math.MaxUint64 {
		t.Errorf("expected safeAddUint64 clamp on overflow, got %d", added)
	}
}

// 16. Server HTTP endpoints integration test (GET snapshot, POST reset, GET settings, PUT settings).
func TestAccountingHTTPHandlers(t *testing.T) {
	tempDir := t.TempDir()
	server := &Server{
		data: tempDir,
	}
	// Seed valid subscription store with "sub1"
	_ = server.saveSubscriptionStore(SubscriptionStore{
		Version:  1,
		ActiveID: "sub1",
		Items: []SubscriptionRecord{
			{ID: "sub1", Name: "Sub One"},
		},
	})
	server.initTrafficAccounting()

	// 1. GET /api/mihomo/traffic-accounting
	reqGet := httptest.NewRequest(http.MethodGet, "/api/mihomo/traffic-accounting", nil)
	recGet := httptest.NewRecorder()
	server.mihomoTrafficAccounting(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET accounting status = %d, body = %s", recGet.Code, recGet.Body.String())
	}

	// 2. GET /api/mihomo/traffic-accounting/settings
	reqGetSet := httptest.NewRequest(http.MethodGet, "/api/mihomo/traffic-accounting/settings", nil)
	recGetSet := httptest.NewRecorder()
	server.mihomoTrafficAccountingSettings(recGetSet, reqGetSet)
	if recGetSet.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, body = %s", recGetSet.Code, recGetSet.Body.String())
	}

	// 3. PUT /api/mihomo/traffic-accounting/settings (valid update)
	validPut := `{"enabled": true, "subscriptions": {"sub1": {"defaultMultiplierMilli": 1500, "nodeMultipliersMilli": {"NodeA": 2000}}}}`
	reqPut := httptest.NewRequest(http.MethodPut, "/api/mihomo/traffic-accounting/settings", bytes.NewBufferString(validPut))
	recPut := httptest.NewRecorder()
	server.mihomoTrafficAccountingSettings(recPut, reqPut)
	if recPut.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d, body = %s", recPut.Code, recPut.Body.String())
	}

	// 4. PUT /api/mihomo/traffic-accounting/settings (spoofed subscription ID)
	spoofedPut := `{"subscriptions": {"spoofed-id": {"defaultMultiplierMilli": 1500}}}`
	reqSpoofed := httptest.NewRequest(http.MethodPut, "/api/mihomo/traffic-accounting/settings", bytes.NewBufferString(spoofedPut))
	recSpoofed := httptest.NewRecorder()
	server.mihomoTrafficAccountingSettings(recSpoofed, reqSpoofed)
	if recSpoofed.Code != http.StatusBadRequest {
		t.Fatalf("PUT spoofed subscription status = %d, want 400", recSpoofed.Code)
	}

	// 5. PUT with unknown fields -> 400
	unknownFieldPut := `{"enabled": true, "unknownField": 123}`
	reqUnk := httptest.NewRequest(http.MethodPut, "/api/mihomo/traffic-accounting/settings", bytes.NewBufferString(unknownFieldPut))
	recUnk := httptest.NewRecorder()
	server.mihomoTrafficAccountingSettings(recUnk, reqUnk)
	if recUnk.Code != http.StatusBadRequest {
		t.Fatalf("PUT unknown fields status = %d, want 400", recUnk.Code)
	}

	// 6. PUT with trailing second JSON -> 400
	trailingPut := `{"enabled": true}{"extra": true}`
	reqTrail := httptest.NewRequest(http.MethodPut, "/api/mihomo/traffic-accounting/settings", bytes.NewBufferString(trailingPut))
	recTrail := httptest.NewRecorder()
	server.mihomoTrafficAccountingSettings(recTrail, reqTrail)
	if recTrail.Code != http.StatusBadRequest {
		t.Fatalf("PUT trailing second JSON status = %d, want 400", recTrail.Code)
	}

	// 7. PUT invalid multiplier (< 100) -> 400
	invalidPut := `{"subscriptions": {"sub1": {"defaultMultiplierMilli": 50}}}`
	reqInvPut := httptest.NewRequest(http.MethodPut, "/api/mihomo/traffic-accounting/settings", bytes.NewBufferString(invalidPut))
	recInvPut := httptest.NewRecorder()
	server.mihomoTrafficAccountingSettings(recInvPut, reqInvPut)
	if recInvPut.Code != http.StatusBadRequest {
		t.Fatalf("PUT invalid multiplier status = %d, want 400", recInvPut.Code)
	}

	// 8. POST reset without body or confirm=false -> 400
	reqResetEmpty := httptest.NewRequest(http.MethodPost, "/api/mihomo/traffic-accounting/reset", bytes.NewBufferString(``))
	recResetEmpty := httptest.NewRecorder()
	server.mihomoTrafficAccountingReset(recResetEmpty, reqResetEmpty)
	if recResetEmpty.Code != http.StatusBadRequest {
		t.Fatalf("POST reset empty body status = %d, want 400", recResetEmpty.Code)
	}

	reqResetFalse := httptest.NewRequest(http.MethodPost, "/api/mihomo/traffic-accounting/reset", bytes.NewBufferString(`{"confirm": false}`))
	recResetFalse := httptest.NewRecorder()
	server.mihomoTrafficAccountingReset(recResetFalse, reqResetFalse)
	if recResetFalse.Code != http.StatusBadRequest {
		t.Fatalf("POST reset confirm=false status = %d, want 400", recResetFalse.Code)
	}

	// 9. POST reset with unknown fields -> 400
	reqResetUnk := httptest.NewRequest(http.MethodPost, "/api/mihomo/traffic-accounting/reset", bytes.NewBufferString(`{"confirm": true, "extra": 123}`))
	recResetUnk := httptest.NewRecorder()
	server.mihomoTrafficAccountingReset(recResetUnk, reqResetUnk)
	if recResetUnk.Code != http.StatusBadRequest {
		t.Fatalf("POST reset unknown field status = %d, want 400", recResetUnk.Code)
	}

	// 10. POST reset with trailing JSON -> 400
	reqResetTrail := httptest.NewRequest(http.MethodPost, "/api/mihomo/traffic-accounting/reset", bytes.NewBufferString(`{"confirm": true}{"extra": 123}`))
	recResetTrail := httptest.NewRecorder()
	server.mihomoTrafficAccountingReset(recResetTrail, reqResetTrail)
	if recResetTrail.Code != http.StatusBadRequest {
		t.Fatalf("POST reset trailing second JSON status = %d, want 400", recResetTrail.Code)
	}

	// 11. POST reset with confirm=true -> 200 OK
	reqReset := httptest.NewRequest(http.MethodPost, "/api/mihomo/traffic-accounting/reset", bytes.NewBufferString(`{"confirm": true}`))
	recReset := httptest.NewRecorder()
	server.mihomoTrafficAccountingReset(recReset, reqReset)
	if recReset.Code != http.StatusOK {
		t.Fatalf("POST reset status = %d, body = %s", recReset.Code, recReset.Body.String())
	}
	var resetRes map[string]any
	if err := json.Unmarshal(recReset.Body.Bytes(), &resetRes); err != nil || resetRes["ok"] != true || resetRes["startedAt"] == "" {
		t.Fatalf("unexpected reset response: %v", recReset.Body.String())
	}
}

// 17. Deadlock regression test: UpdateSettings must complete without self-deadlock.
func TestUpdateSettingsDeadlockRegression(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		mult := uint32(1500)
		_, err := mgr.UpdateSettings(UpdateTrafficSettingsRequest{
			Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
				"sub-test": {DefaultMultiplierMilli: &mult},
			},
		})
		if err != nil {
			t.Errorf("UpdateSettings failed: %v", err)
		}
	}()

	select {
	case <-done:
		// Success: no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("UpdateSettings deadlocked holding lock")
	}
}

// 18. Reset vs in-flight poll isolation: in-flight snapshot fetched across Reset must be discarded.
func TestResetInFlightPollDoesNotCountOldSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	fetchStarted := make(chan struct{})
	fetchProceed := make(chan struct{})

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			close(fetchStarted)
			<-fetchProceed
			return &mihomoRawConnectionsResponse{
				Connections: []mihomoRawConnection{
					newMockRawConnection("c1", 50000, 50000, []string{"Node1", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
				},
			}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub1", "Sub1"
		},
	})

	// Baseline poll
	pollDone := make(chan error, 1)
	go func() {
		pollDone <- mgr.Poll(context.Background())
	}()

	<-fetchStarted
	// Reset while fetch is in flight
	_, err := mgr.Reset()
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Allow in-flight fetch to return
	close(fetchProceed)
	if err := <-pollDone; err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}

	snap := mgr.GetSnapshot()
	if snap.TotalObserved.RawBytes != 0 {
		t.Fatalf("expected 0 bytes after reset, but in-flight poll counted %d bytes", snap.TotalObserved.RawBytes)
	}
	if snap.TotalObserved.ConnectionCount != 0 {
		t.Fatalf("expected 0 connection count, got %d", snap.TotalObserved.ConnectionCount)
	}
}

// 19. Connection reuse detection: same connection ID but changed metadata without counter rollback.
func TestConnectionReuseWithoutRollback(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub1", "Sub1"
		},
	})

	// Poll 1: Baseline established
	c1 := newMockRawConnection("conn-1", 1000, 1000, []string{"Node1", "Proxy"}, "siteA.com", "1.1.1.1", "192.168.2.10")
	c1.Start = "2026-09-14T00:00:01Z"
	currentConns = []mihomoRawConnection{c1}
	_ = mgr.Poll(context.Background())

	// Poll 2: Same connection ID, but metadata changed (different Start & Target), with upload >= previous
	c2 := newMockRawConnection("conn-1", 1500, 1500, []string{"Node2", "Proxy"}, "siteB.com", "2.2.2.2", "192.168.2.20")
	c2.Start = "2026-09-14T00:00:05Z"
	currentConns = []mihomoRawConnection{c2}
	_ = mgr.Poll(context.Background())

	// Because connection ID was reused by a new connection, it must re-establish baseline and NOT attribute the 500+500 difference to the old connection
	snap := mgr.GetSnapshot()
	if snap.TotalObserved.RawBytes != 0 {
		t.Fatalf("expected 0 raw bytes on reused connection re-baseline, got %d", snap.TotalObserved.RawBytes)
	}

	// Poll 3: Transfer on the reused connection
	c3 := newMockRawConnection("conn-1", 2000, 2000, []string{"Node2", "Proxy"}, "siteB.com", "2.2.2.2", "192.168.2.20")
	c3.Start = "2026-09-14T00:00:05Z"
	currentConns = []mihomoRawConnection{c3}
	_ = mgr.Poll(context.Background())

	snap3 := mgr.GetSnapshot()
	// Delta is (2000-1500) + (2000-1500) = 1000 bytes, attributed to siteB.com and Node2
	if snap3.TotalObserved.RawBytes != 1000 {
		t.Fatalf("expected 1000 raw bytes, got %d", snap3.TotalObserved.RawBytes)
	}
	foundNode2 := false
	for _, n := range snap3.Nodes {
		if n.Node == "Node2" {
			foundNode2 = true
			if n.RawBytes != 1000 {
				t.Errorf("Node2 rawBytes = %d, want 1000", n.RawBytes)
			}
		}
	}
	if !foundNode2 {
		t.Errorf("Node2 not found in nodes ranking")
	}
}

// 20. Compound node key test: same node name across different subscriptions.
func TestCompoundNodeKeyCrossSubscription(t *testing.T) {
	tempDir := t.TempDir()
	var currentConns []mihomoRawConnection
	var activeSubID, activeSubName string

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: currentConns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return activeSubID, activeSubName
		},
	})

	// Baseline
	activeSubID, activeSubName = "sub-hk", "Hong Kong Sub"
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 0, 0, []string{"香港 01", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
	}
	_ = mgr.Poll(context.Background())

	// Transfer under sub-hk
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c1", 1000, 1000, []string{"香港 01", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.1"),
	}
	_ = mgr.Poll(context.Background())

	// Switch subscription to sub-us with a node of the EXACT same name "香港 01"
	activeSubID, activeSubName = "sub-us", "US Sub"
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c2", 0, 0, []string{"香港 01", "Proxy"}, "b.com", "2.2.2.2", "192.168.2.2"),
	}
	_ = mgr.Poll(context.Background())

	// Transfer under sub-us
	currentConns = []mihomoRawConnection{
		newMockRawConnection("c2", 2000, 2000, []string{"香港 01", "Proxy"}, "b.com", "2.2.2.2", "192.168.2.2"),
	}
	_ = mgr.Poll(context.Background())

	snap := mgr.GetSnapshot()
	if len(snap.Nodes) < 2 {
		t.Fatalf("expected at least 2 separate node entries for distinct subscriptions, got %d", len(snap.Nodes))
	}

	var subHKNode, subUSNode *NodeAccounting
	for i := range snap.Nodes {
		n := &snap.Nodes[i]
		if n.Node == "香港 01" && n.SubscriptionID == "sub-hk" {
			subHKNode = n
		}
		if n.Node == "香港 01" && n.SubscriptionID == "sub-us" {
			subUSNode = n
		}
	}
	if subHKNode == nil || subHKNode.RawBytes != 2000 {
		t.Fatalf("sub-hk node expected 2000 bytes, got %v", subHKNode)
	}
	if subUSNode == nil || subUSNode.RawBytes != 4000 {
		t.Fatalf("sub-us node expected 4000 bytes, got %v", subUSNode)
	}
}

// 21. normalizeTarget IPv6 preservation test.
func TestNormalizeTargetBareIPv6(t *testing.T) {
	tests := []struct {
		host   string
		dstIP  string
		expect string
	}{
		{"", "2001:db8::1", "2001:db8::1"},
		{"2001:db8::1", "", "2001:db8::1"},
		{"[2001:db8::1]:443", "", "2001:db8::1"},
		{"", "[2001:db8::1]:8443", "2001:db8::1"},
		{"::1", "", "::1"},
		{"", "fe80::1", "fe80::1"},
		{"192.168.2.1:8080", "", "192.168.2.1"},
		{"", "192.168.2.1", "192.168.2.1"},
		{"Api.GitHub.Com:443", "140.82.112.4", "api.github.com"},
		{"Example.Com.", "", "example.com"},
		{"", "", "unknown"},
	}

	for _, tc := range tests {
		got := normalizeTarget(tc.host, tc.dstIP)
		if got != tc.expect {
			t.Errorf("normalizeTarget(%q, %q) = %q, want %q", tc.host, tc.dstIP, got, tc.expect)
		}
	}
}

// 22. Online flag: must NOT be true if no successful poll has ever completed.
func TestOnlineNeverTrueBeforeFirstSuccessfulPoll(t *testing.T) {
	tempDir := t.TempDir()
	failPoll := true

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			if failPoll {
				return nil, errors.New("simulated network failure")
			}
			return &mihomoRawConnectionsResponse{Connections: []mihomoRawConnection{}}, nil
		},
	})

	// Before any poll has run, Online must be false
	snap0 := mgr.GetSnapshot()
	if snap0.Online {
		t.Fatal("Online must be false before any poll runs")
	}

	// Failed poll: Online must be false
	_ = mgr.Poll(context.Background())
	snap1 := mgr.GetSnapshot()
	if snap1.Online {
		t.Fatal("Online must be false after failed poll")
	}

	// Successful poll: Online becomes true
	failPoll = false
	_ = mgr.Poll(context.Background())
	snap2 := mgr.GetSnapshot()
	if !snap2.Online {
		t.Fatal("Online must be true after successful poll")
	}

	// Another failed poll: Online returns to false
	failPoll = true
	_ = mgr.Poll(context.Background())
	snap3 := mgr.GetSnapshot()
	if snap3.Online {
		t.Fatal("Online must be false after subsequent failure")
	}
}

// 23. Strict bounding of hourly (<= 24) and daily (<= 90) time buckets.
func TestTimeBucketsStrictCaps(t *testing.T) {
	hourly := make(map[string]*TimeAccounting)
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("2026-09-14T%02d:00:00Z", i)
		hourly[key] = &TimeAccounting{Time: key}
	}
	boundedH := boundHourlyBuckets(hourly)
	if len(boundedH) != 24 {
		t.Fatalf("expected strictly 24 hourly buckets, got %d", len(boundedH))
	}

	daily := make(map[string]*TimeAccounting)
	for i := 0; i < 120; i++ {
		key := fmt.Sprintf("2026-01-%02d", i)
		daily[key] = &TimeAccounting{Time: key}
	}
	boundedD := boundDailyBuckets(daily)
	if len(boundedD) != 90 {
		t.Fatalf("expected strictly 90 daily buckets, got %d", len(boundedD))
	}

	// Nil safety
	if boundHourlyBuckets(nil) != nil {
		t.Error("expected nil on nil hourly map")
	}
	if boundDailyBuckets(nil) != nil {
		t.Error("expected nil on nil daily map")
	}
}

// 24. Known nodes low frequency caching and per-round host lookup deduplication.
func TestKnownNodesLowFrequencyAndHostLookupDedup(t *testing.T) {
	tempDir := t.TempDir()
	lookupCount := 0
	var mu sync.Mutex

	conns := []mihomoRawConnection{
		newMockRawConnection("c1", 100, 100, []string{"Node1", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.50"),
		newMockRawConnection("c2", 100, 100, []string{"Node1", "Proxy"}, "b.com", "1.1.1.2", "192.168.2.50"),
		newMockRawConnection("c3", 100, 100, []string{"Node1", "Proxy"}, "c.com", "1.1.1.3", "192.168.2.50"),
	}

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: conns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub1", "Sub1"
		},
		LookupClientHost: func(ip string) string {
			mu.Lock()
			lookupCount++
			mu.Unlock()
			return "host-50"
		},
	})

	_ = mgr.Poll(context.Background())

	mu.Lock()
	count := lookupCount
	mu.Unlock()

	// 3 connections from same IP 192.168.2.50 in one poll: host lookup must happen exactly ONCE
	if count != 1 {
		t.Fatalf("expected host lookup to be deduplicated to 1, got %d", count)
	}
}

// 24b. Low frequency fetch for client hostnames (30s) and resolution rules.
func TestClientHostnamesLowFrequencyAndNameRules(t *testing.T) {
	tempDir := t.TempDir()
	var callbackCalls int
	var legacyLookupCalls int
	var mu sync.Mutex

	currentTime := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	nowFunc := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currentTime
	}

	conns := []mihomoRawConnection{
		newMockRawConnection("c1", 100, 100, []string{"Node1", "Proxy"}, "a.com", "1.1.1.1", "192.168.2.50"),
		newMockRawConnection("c2", 100, 100, []string{"Node1", "Proxy"}, "b.com", "1.1.1.2", "192.168.2.51"),
		newMockRawConnection("c3", 100, 100, []string{"Node1", "Proxy"}, "c.com", "1.1.1.3", "127.0.0.1"),
		newMockRawConnection("c4", 100, 100, []string{"Node1", "Proxy"}, "d.com", "1.1.1.4", "198.18.0.1"),
		newMockRawConnection("c5", 100, 100, []string{"Node1", "Proxy"}, "e.com", "1.1.1.5", ""),
		newMockRawConnection("c6", 100, 100, []string{"Node1", "Proxy"}, "f.com", "1.1.1.6", "192.168.2.99"),
	}

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: conns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "sub1", "Sub1"
		},
		GetClientHostnames: func() map[string]string {
			mu.Lock()
			callbackCalls++
			mu.Unlock()
			return map[string]string{
				"192.168.2.50": "Laptop-Pro",
				"192.168.2.51": "Phone-Max",
			}
		},
		LookupClientHost: func(ip string) string {
			mu.Lock()
			legacyLookupCalls++
			mu.Unlock()
			return "legacy-" + ip
		},
		NowFunc: nowFunc,
	})

	// Poll 1: initial poll -> callback must be called once, establishes baseline
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatalf("poll 1 failed: %v", err)
	}

	mu.Lock()
	if callbackCalls != 1 {
		t.Fatalf("expected 1 callback call on poll 1, got %d", callbackCalls)
	}
	mu.Unlock()

	// Advance bytes for Poll 2 (+5s): delta recorded and device hostnames populated in ledger
	for i := range conns {
		conns[i].Upload += 100
		conns[i].Download += 100
	}
	mu.Lock()
	currentTime = currentTime.Add(5 * time.Second)
	mu.Unlock()
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatalf("poll 2 failed: %v", err)
	}

	// Verify device names in snapshot: bulk cache mapped, unknown IP fallback directly to IP, built-in rules intact
	snap := mgr.GetSnapshot()
	devByName := make(map[string]string)
	for _, dev := range snap.Devices {
		devByName[dev.IP] = dev.Hostname
	}

	if devByName["192.168.2.50"] != "Laptop-Pro" {
		t.Errorf("expected 192.168.2.50 hostname Laptop-Pro, got %s", devByName["192.168.2.50"])
	}
	if devByName["192.168.2.51"] != "Phone-Max" {
		t.Errorf("expected 192.168.2.51 hostname Phone-Max, got %s", devByName["192.168.2.51"])
	}
	if devByName["127.0.0.1"] != "路由器本机 / CPA" {
		t.Errorf("expected 127.0.0.1 hostname 路由器本机 / CPA, got %s", devByName["127.0.0.1"])
	}
	if devByName["198.18.0.1"] != "Mihomo TUN 本机" {
		t.Errorf("expected 198.18.0.1 hostname Mihomo TUN 本机, got %s", devByName["198.18.0.1"])
	}
	if devByName["unknown"] != "路由器本机 / 来源未知" {
		t.Errorf("expected devKey unknown hostname 路由器本机 / 来源未知, got %s", devByName["unknown"])
	}
	if devByName["192.168.2.99"] != "192.168.2.99" {
		t.Errorf("expected unknown IP 192.168.2.99 fallback to IP, got %s", devByName["192.168.2.99"])
	}

	// Legacy LookupClientHost must NEVER be called when GetClientHostnames is configured, even for missing IP 192.168.2.99
	mu.Lock()
	if legacyLookupCalls != 0 {
		t.Fatalf("expected legacy LookupClientHost never to be called when bulk cache is active, got %d calls", legacyLookupCalls)
	}
	mu.Unlock()

	// Poll 3 at +15s: still within 30s window -> callback must NOT be called
	mu.Lock()
	currentTime = currentTime.Add(10 * time.Second)
	mu.Unlock()
	_ = mgr.Poll(context.Background())

	// Poll 4 at +29s: still within 30s window -> callback must NOT be called
	mu.Lock()
	currentTime = currentTime.Add(14 * time.Second)
	mu.Unlock()
	_ = mgr.Poll(context.Background())

	mu.Lock()
	if callbackCalls != 1 {
		t.Fatalf("expected callback calls to remain 1 within 30s, got %d", callbackCalls)
	}
	if legacyLookupCalls != 0 {
		t.Fatalf("expected legacy LookupClientHost to remain 0, got %d", legacyLookupCalls)
	}
	mu.Unlock()

	// Poll 5 at +31s: exceeded 30s -> bulk callback MUST be called exactly a second time
	mu.Lock()
	currentTime = currentTime.Add(2 * time.Second)
	mu.Unlock()
	_ = mgr.Poll(context.Background())

	mu.Lock()
	if callbackCalls != 2 {
		t.Fatalf("expected callback calls to be 2 after 31s, got %d", callbackCalls)
	}
	if legacyLookupCalls != 0 {
		t.Fatalf("expected legacy LookupClientHost to remain 0 after second bulk fetch, got %d", legacyLookupCalls)
	}
	mu.Unlock()
}

// 25. Speedtest known nodes attributed only to active subscription and avoids redundant writes.
func TestSpeedtestKnownNodesAttributedOnlyToActiveSub(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: []mihomoRawConnection{}}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return "active-sub", "Active Sub"
		},
		GetKnownNodes: func() []string {
			return []string{"NodeA", "NodeB"}
		},
	})

	_ = mgr.Poll(context.Background())

	settings := mgr.GetSettings()
	cfg, ok := settings.Subscriptions["active-sub"]
	if !ok || len(cfg.KnownNodes) < 2 {
		t.Fatalf("expected active-sub to have discovered known nodes, got %v", cfg)
	}
	// Verify other subscriptions were not touched
	if _, hasOther := settings.Subscriptions[""]; hasOther {
		t.Errorf("empty subscription ID should never receive discovered nodes")
	}
}

// 26. Settings PUT rejects spoofed subscription IDs when validator configured.
func TestSettingsPUTRejectsSpoofedSubscriptionID(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		GetValidSubscriptionIDs: func() []string {
			return []string{"valid-sub-1", "valid-sub-2"}
		},
	})

	mult := uint32(1500)
	// Try spoofed sub ID
	_, err := mgr.UpdateSettings(UpdateTrafficSettingsRequest{
		Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
			"spoofed-id": {DefaultMultiplierMilli: &mult},
		},
	})
	if err == nil {
		t.Fatal("expected error updating settings with spoofed subscription ID, got nil")
	}

	// Try valid sub ID
	_, err = mgr.UpdateSettings(UpdateTrafficSettingsRequest{
		Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
			"valid-sub-1": {DefaultMultiplierMilli: &mult},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error updating settings with valid subscription ID: %v", err)
	}
}

// 27. TestDeviceRoutesAccounting: aggregation, DIRECT/REJECT, cross-subscription node isolation, persistence, and reset.
func TestDeviceRoutesAccounting(t *testing.T) {
	tempDir := t.TempDir()

	currentSubID := "sub1"
	currentSubName := "Sub One"

	conns := []mihomoRawConnection{
		// Direct connection
		newMockRawConnection("c_direct", 100, 200, []string{"DIRECT"}, "example.com", "1.1.1.1", "192.168.2.10"),
		// Reject connection
		newMockRawConnection("c_reject", 50, 50, []string{"REJECT"}, "blocked.com", "2.2.2.2", "192.168.2.10"),
		// Proxy connection on Sub 1
		newMockRawConnection("c_proxy1", 1000, 2000, []string{"HK-Node", "ProxyGroup"}, "proxy1.com", "3.3.3.3", "192.168.2.10"),
	}

	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		FetchConnections: func(ctx context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: conns}, nil
		},
		GetActiveSubscription: func() (string, string) {
			return currentSubID, currentSubName
		},
		LookupClientHost: func(ip string) string {
			if ip == "192.168.2.10" {
				return "phone-user1"
			}
			if ip == "192.168.2.20" {
				return "laptop-user2"
			}
			return ""
		},
	})

	// First poll establishes baseline
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatalf("first poll failed: %v", err)
	}

	// Advance bytes
	conns[0].Upload += 1000    // +1000 direct
	conns[0].Download += 2000  // +2000 direct
	conns[1].Upload += 100     // +100 reject
	conns[1].Download += 100   // +100 reject
	conns[2].Upload += 5000    // +5000 proxy sub1
	conns[2].Download += 10000 // +10000 proxy sub1

	// Switch active sub to sub2
	currentSubID = "sub2"
	currentSubName = "Sub Two"

	// Add c_proxy2 with 0 counts to establish connection under sub2
	conns = append(conns, newMockRawConnection("c_proxy2", 0, 0, []string{"HK-Node", "ProxyGroup"}, "proxy2.com", "4.4.4.4", "192.168.2.20"))

	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatalf("second poll failed: %v", err)
	}

	// Advance bytes for c_proxy2 in third poll
	conns[3].Upload += 3000   // +3000 proxy sub2
	conns[3].Download += 7000 // +7000 proxy sub2

	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatalf("third poll failed: %v", err)
	}

	// Persist ledger explicitly to disk
	mgr.mu.Lock()
	if err := mgr.persistLedgerLocked(); err != nil {
		mgr.mu.Unlock()
		t.Fatalf("persist ledger failed: %v", err)
	}
	mgr.mu.Unlock()

	// Verify ledger snapshot
	snap := mgr.GetSnapshot()
	if len(snap.DeviceRoutes) != 4 {
		t.Fatalf("expected 4 device routes, got %d", len(snap.DeviceRoutes))
	}

	// Map routes by IP + routeKind + subscriptionId
	routeMap := make(map[string]DeviceRouteAccounting)
	for _, dr := range snap.DeviceRoutes {
		key := fmt.Sprintf("%s|%s|%s", dr.IP, dr.RouteKind, dr.SubscriptionID)
		routeMap[key] = dr
	}

	// 1. Direct route for 192.168.2.10
	drDirect, ok := routeMap["192.168.2.10|direct|"]
	if !ok {
		t.Fatal("missing direct device route for 192.168.2.10")
	}
	if drDirect.RawBytes != 3000 || drDirect.UploadBytes != 1000 || drDirect.DownloadBytes != 2000 {
		t.Errorf("unexpected direct traffic stats: %+v", drDirect)
	}
	if drDirect.Hostname != "phone-user1" {
		t.Errorf("expected hostname phone-user1, got %s", drDirect.Hostname)
	}
	if drDirect.Node != "" || drDirect.Group != "" || drDirect.SubscriptionID != "" {
		t.Errorf("direct route must not have node/group/subID: %+v", drDirect)
	}

	// 2. Reject route for 192.168.2.10
	drReject, ok := routeMap["192.168.2.10|reject|"]
	if !ok {
		t.Fatal("missing reject device route for 192.168.2.10")
	}
	if drReject.RawBytes != 200 || drReject.UploadBytes != 100 || drReject.DownloadBytes != 100 {
		t.Errorf("unexpected reject traffic stats: %+v", drReject)
	}
	if drReject.Node != "" || drReject.Group != "" || drReject.SubscriptionID != "" {
		t.Errorf("reject route must not have node/group/subID: %+v", drReject)
	}

	// 3. Proxy route for 192.168.2.10 on sub1
	drProxy1, ok := routeMap["192.168.2.10|proxy|sub1"]
	if !ok {
		t.Fatal("missing proxy sub1 device route for 192.168.2.10")
	}
	if drProxy1.RawBytes != 15000 || drProxy1.UploadBytes != 5000 || drProxy1.DownloadBytes != 10000 {
		t.Errorf("unexpected proxy1 traffic stats: %+v", drProxy1)
	}
	if drProxy1.Node != "HK-Node" || drProxy1.Group != "ProxyGroup" || drProxy1.SubscriptionName != "Sub One" {
		t.Errorf("unexpected proxy1 metadata: %+v", drProxy1)
	}

	// 4. Proxy route for 192.168.2.20 on sub2 (same node name HK-Node, must not merge!)
	drProxy2, ok := routeMap["192.168.2.20|proxy|sub2"]
	if !ok {
		t.Fatal("missing proxy sub2 device route for 192.168.2.20")
	}
	if drProxy2.RawBytes != 10000 || drProxy2.UploadBytes != 3000 || drProxy2.DownloadBytes != 7000 {
		t.Errorf("unexpected proxy2 traffic stats: %+v", drProxy2)
	}
	if drProxy2.Node != "HK-Node" || drProxy2.Group != "ProxyGroup" || drProxy2.SubscriptionName != "Sub Two" {
		t.Errorf("unexpected proxy2 metadata: %+v", drProxy2)
	}

	// Test Persistence across manager restart
	mgrRestarted := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
	})
	snapRestarted := mgrRestarted.GetSnapshot()
	if len(snapRestarted.DeviceRoutes) != 4 {
		t.Fatalf("expected 4 device routes after restart, got %d", len(snapRestarted.DeviceRoutes))
	}
	restartedMap := make(map[string]DeviceRouteAccounting)
	for _, dr := range snapRestarted.DeviceRoutes {
		key := fmt.Sprintf("%s|%s|%s", dr.IP, dr.RouteKind, dr.SubscriptionID)
		restartedMap[key] = dr
	}
	if restartedMap["192.168.2.10|proxy|sub1"].RawBytes != 15000 {
		t.Errorf("expected 15000 bytes after restart for proxy1, got %d", restartedMap["192.168.2.10|proxy|sub1"].RawBytes)
	}

	// Test Reset clears device routes
	if _, err := mgrRestarted.Reset(); err != nil {
		t.Fatalf("reset failed: %v", err)
	}
	snapAfterReset := mgrRestarted.GetSnapshot()
	if len(snapAfterReset.DeviceRoutes) != 0 {
		t.Fatalf("expected 0 device routes after reset, got %d", len(snapAfterReset.DeviceRoutes))
	}
}

// 28. TestDeviceRoutesCapacityAndOtherBounding: 5000 limit and top 200 + _other bounding.
func TestDeviceRoutesCapacityAndOtherBounding(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
	})

	mgr.mu.Lock()
	// Fill ledger with 5000 distinct device routes
	for i := 0; i < 5000; i++ {
		key := fmt.Sprintf("10.0.%d.%d\x00proxy\x00sub1\x00Node%d\x00GroupA", i/250, i%250, i)
		dr := &DeviceRouteAccounting{
			IP:               fmt.Sprintf("10.0.%d.%d", i/250, i%250),
			RouteKind:        "proxy",
			SubscriptionID:   "sub1",
			SubscriptionName: "Sub One",
			Node:             fmt.Sprintf("Node%d", i),
			Group:            "GroupA",
		}
		dr.RawBytes = uint64(i + 10)
		mgr.ledger.DeviceRoutes[key] = dr
	}

	// Ingest 5001st route via accumulateDeltaLocked
	extraConn := &trackedConnection{
		ID:               "extra_conn",
		SourceIP:         "192.168.100.100",
		RouteKind:        "proxy",
		SubscriptionID:   "sub1",
		SubscriptionName: "Sub One",
		LeafNode:         "ExtraNode",
		ProxyGroup:       "GroupA",
	}
	mgr.accumulateDeltaLocked(extraConn, 500, 500, true, "2026-09-14T00:00:00Z")

	// Verify ledger size is capped at 5000 plus otherKey (or 5001 total entries including _other)
	otherDr := mgr.ledger.DeviceRoutes[otherKey]
	if otherDr == nil {
		t.Fatal("expected _other entry in ledger when exceeding 5000 device routes")
	}
	if otherDr.RawBytes != 1000 {
		t.Errorf("expected _other to have 1000 raw bytes, got %d", otherDr.RawBytes)
	}
	mgr.mu.Unlock()

	// Verify API Snapshot bounds rankings to top 200 + _other
	snap := mgr.GetSnapshot()
	if len(snap.DeviceRoutes) != 201 { // 200 top + 1 _other
		t.Fatalf("expected 201 device routes in snapshot, got %d", len(snap.DeviceRoutes))
	}
	lastItem := snap.DeviceRoutes[len(snap.DeviceRoutes)-1]
	if lastItem.IP != otherKey {
		t.Errorf("expected last item to be _other, got %s", lastItem.IP)
	}
}

// 29. TestRuntimePersistenceErrorWarningAndClear: records error in manager and response, cleared upon success.
func TestRuntimePersistenceErrorWarningAndClear(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
	})

	// Make ledgerPath unwritable by replacing it with a directory
	_ = os.Remove(mgr.ledgerPath)
	if err := os.Mkdir(mgr.ledgerPath, 0755); err != nil {
		t.Fatalf("failed creating blocker directory: %v", err)
	}

	mgr.mu.Lock()
	err := mgr.persistLedgerLocked()
	mgr.mu.Unlock()
	if err == nil {
		t.Fatal("expected error when ledgerPath is a directory, got nil")
	}

	// Verify error recorded in manager and GET snapshot
	snap := mgr.GetSnapshot()
	if snap.PersistenceError == "" {
		t.Fatal("expected non-empty PersistenceError in snapshot")
	}
	if !strings.Contains(snap.Warning, "账本持久化失败:") {
		t.Fatalf("expected Warning to contain '账本持久化失败:', got: %s", snap.Warning)
	}

	// Reset must also fail and return error
	if _, resetErr := mgr.Reset(); resetErr == nil {
		t.Fatal("expected Reset to fail when ledger persistence fails, got nil")
	}

	// Restore ledgerPath to writable
	_ = os.RemoveAll(mgr.ledgerPath)

	mgr.mu.Lock()
	err = mgr.persistLedgerLocked()
	mgr.mu.Unlock()
	if err != nil {
		t.Fatalf("persistLedgerLocked failed after restore: %v", err)
	}

	// Verify error cleared
	snap2 := mgr.GetSnapshot()
	if snap2.PersistenceError != "" {
		t.Fatalf("expected PersistenceError to be empty after successful persist, got %s", snap2.PersistenceError)
	}
	if snap2.Warning != "" {
		t.Fatalf("expected Warning to be empty after successful persist, got %s", snap2.Warning)
	}

	// Test settings persistence failure
	_ = os.Remove(mgr.settingsPath)
	if err := os.Mkdir(mgr.settingsPath, 0755); err != nil {
		t.Fatalf("failed creating blocker directory for settings: %v", err)
	}

	mgr.mu.Lock()
	err = mgr.persistSettingsLocked()
	mgr.mu.Unlock()
	if err == nil {
		t.Fatal("expected error when settingsPath is a directory, got nil")
	}

	snap3 := mgr.GetSnapshot()
	if !strings.Contains(snap3.PersistenceError, "设置持久化失败:") {
		t.Fatalf("expected PersistenceError to contain '设置持久化失败:', got: %s", snap3.PersistenceError)
	}

	// Restore settings path
	_ = os.RemoveAll(mgr.settingsPath)
	mgr.mu.Lock()
	err = mgr.persistSettingsLocked()
	mgr.mu.Unlock()
	if err != nil {
		t.Fatalf("persistSettingsLocked failed after restore: %v", err)
	}

	snap4 := mgr.GetSnapshot()
	if snap4.PersistenceError != "" {
		t.Fatalf("expected PersistenceError to be cleared, got %s", snap4.PersistenceError)
	}
}

// 30. TestSettingsPUTRollbackOnPersistenceFailure: rollback in-memory state on persistence failure.
func TestSettingsPUTRollbackOnPersistenceFailure(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: tempDir,
		GetValidSubscriptionIDs: func() []string {
			return []string{"sub1"}
		},
	})

	// Initial settings: enabled=true, sub1 default multiplier = 1000
	initMult := uint32(1000)
	_, err := mgr.UpdateSettings(UpdateTrafficSettingsRequest{
		Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
			"sub1": {DefaultMultiplierMilli: &initMult},
		},
	})
	if err != nil {
		t.Fatalf("initial settings update failed: %v", err)
	}

	// Make settingsPath unwritable to force persistence failure
	_ = os.Remove(mgr.settingsPath)
	if err := os.Mkdir(mgr.settingsPath, 0755); err != nil {
		t.Fatalf("failed creating blocker directory: %v", err)
	}

	// Attempt updating with enabled=false and multiplier=5000
	newMult := uint32(5000)
	disabled := false
	_, err = mgr.UpdateSettings(UpdateTrafficSettingsRequest{
		Enabled: &disabled,
		Subscriptions: map[string]*UpdateSubscriptionConfigRequest{
			"sub1": {DefaultMultiplierMilli: &newMult},
		},
	})
	if err == nil {
		t.Fatal("expected UpdateSettings to fail on write error, got nil")
	}

	// In-memory settings MUST be rolled back to pre-update state
	current := mgr.GetSettings()
	if !current.Enabled {
		t.Errorf("expected Enabled to remain true after rollback, got false")
	}
	if current.Subscriptions["sub1"] == nil || *current.Subscriptions["sub1"].DefaultMultiplierMilli != 1000 {
		t.Errorf("expected sub1 multiplier to remain 1000 after rollback, got %+v", current.Subscriptions["sub1"])
	}
}

// 31. TestHTTPBodyOverflow413: reset 4KiB and settings 64KiB overflow checks.
func TestHTTPBodyOverflow413(t *testing.T) {
	tempDir := t.TempDir()
	server := &Server{
		data: tempDir,
	}
	server.initTrafficAccounting()

	// 1. Reset endpoint: <= 4096 bytes succeeds, > 4096 bytes returns 413
	validReset := `{"confirm": true}`
	reqNormalReset := httptest.NewRequest(http.MethodPost, "/api/mihomo/traffic-accounting/reset", bytes.NewBufferString(validReset))
	recNormalReset := httptest.NewRecorder()
	server.mihomoTrafficAccountingReset(recNormalReset, reqNormalReset)
	if recNormalReset.Code != http.StatusOK {
		t.Fatalf("normal reset status = %d, expected 200", recNormalReset.Code)
	}

	// Reset payload with valid JSON prefix + trailing spaces exceeding 4096 bytes
	overflowReset := validReset + strings.Repeat(" ", 4096)
	reqOverflowReset := httptest.NewRequest(http.MethodPost, "/api/mihomo/traffic-accounting/reset", bytes.NewBufferString(overflowReset))
	recOverflowReset := httptest.NewRecorder()
	server.mihomoTrafficAccountingReset(recOverflowReset, reqOverflowReset)
	if recOverflowReset.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("overflow reset status = %d, expected 413", recOverflowReset.Code)
	}

	// 2. Settings PUT endpoint: <= 65536 bytes succeeds, > 65536 bytes returns 413
	validSettings := `{"enabled": true}`
	reqNormalSettings := httptest.NewRequest(http.MethodPut, "/api/mihomo/traffic-accounting/settings", bytes.NewBufferString(validSettings))
	recNormalSettings := httptest.NewRecorder()
	server.mihomoTrafficAccountingPutSettings(recNormalSettings, reqNormalSettings)
	if recNormalSettings.Code != http.StatusOK {
		t.Fatalf("normal settings status = %d, expected 200", recNormalSettings.Code)
	}

	// Settings payload with valid JSON prefix + trailing spaces exceeding 65536 bytes
	overflowSettings := validSettings + strings.Repeat(" ", 65536)
	reqOverflowSettings := httptest.NewRequest(http.MethodPut, "/api/mihomo/traffic-accounting/settings", bytes.NewBufferString(overflowSettings))
	recOverflowSettings := httptest.NewRecorder()
	server.mihomoTrafficAccountingPutSettings(recOverflowSettings, reqOverflowSettings)
	if recOverflowSettings.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("overflow settings status = %d, expected 413", recOverflowSettings.Code)
	}
}

func TestLedgerDoesNotRewriteWhileIdle(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir: t.TempDir(),
		NowFunc: func() time.Time { return now },
		FetchConnections: func(context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{}, nil
		},
	})
	initialSave := mgr.lastSaveTime
	now = now.Add(time.Minute)
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !mgr.lastSaveTime.Equal(initialSave) {
		t.Fatalf("idle poll rewrote ledger: last save moved from %v to %v", initialSave, mgr.lastSaveTime)
	}
}

func TestLedgerWriteAmplificationIsRateLimited(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	connection := newMockRawConnection("fast", 0, 0, []string{"DIRECT"}, "example.com", "1.1.1.1", "192.168.2.2")
	mgr := NewTrafficAccountingManager(TrafficAccountingConfig{
		DataDir:      t.TempDir(),
		SaveInterval: 10 * time.Second,
		NowFunc:      func() time.Time { return now },
		FetchConnections: func(context.Context) (*mihomoRawConnectionsResponse, error) {
			return &mihomoRawConnectionsResponse{Connections: []mihomoRawConnection{connection}}, nil
		},
	})
	initialSave := mgr.lastSaveTime
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Second)
	connection.Upload = 2 * 1024 * 1024
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !mgr.lastSaveTime.Equal(initialSave) {
		t.Fatalf("large one-second delta bypassed save interval: %v", mgr.lastSaveTime)
	}

	now = now.Add(9 * time.Second)
	if err := mgr.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !mgr.lastSaveTime.Equal(now) {
		t.Fatalf("dirty ledger was not flushed at save interval: got %v want %v", mgr.lastSaveTime, now)
	}
}

// 32. TestSanitizeLogSecrets: verify secrets and URLs are masked in logs.
func TestSanitizeLogSecrets(t *testing.T) {
	rawLog := "activation failed: error fetching https://user:pass@example.com/api/v1/client/subscribe?token=secret12345&auth=bearer_token999 from upstream"
	sanitized := sanitizeLogSecrets(rawLog)

	if strings.Contains(sanitized, "secret12345") {
		t.Errorf("sanitized log contains secret token: %s", sanitized)
	}
	if strings.Contains(sanitized, "bearer_token999") {
		t.Errorf("sanitized log contains secret auth: %s", sanitized)
	}
	if strings.Contains(sanitized, "/api/v1/client/subscribe") {
		t.Errorf("sanitized log contains private subscription path: %s", sanitized)
	}
	if !strings.Contains(sanitized, "https://example.com/…") {
		t.Errorf("sanitized log missing masked url: %s", sanitized)
	}

	rawParamLog := "update error: token=abcdef123456789 password=mySecretPassword"
	sanitizedParams := sanitizeLogSecrets(rawParamLog)
	if strings.Contains(sanitizedParams, "abcdef123456789") || strings.Contains(sanitizedParams, "mySecretPassword") {
		t.Errorf("sanitized log contains sensitive query/key parameters: %s", sanitizedParams)
	}
}
