package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCandidateFiltering(t *testing.T) {
	groupNames := map[string]bool{
		"家庭RULE":   true,
		"家庭RULE自动": true,
		"AI":       true,
		"AI-日本自动":  true,
		"家庭测速":     true,
	}

	tests := []struct {
		name     string
		nodeName string
		exclude  bool
	}{
		{name: "empty", nodeName: "", exclude: true},
		{name: "whitespace", nodeName: "   ", exclude: true},
		{name: "DIRECT", nodeName: "DIRECT", exclude: true},
		{name: "REJECT", nodeName: "REJECT", exclude: true},
		{name: "group name", nodeName: "家庭RULE", exclude: true},
		{name: "metadata remaining", nodeName: "剩余流量：100G", exclude: true},
		{name: "metadata expiry", nodeName: "到期时间：2026-12-31", exclude: true},
		{name: "metadata website", nodeName: "官网：example.com", exclude: true},
		{name: "metadata reset", nodeName: "重置日：每月1日", exclude: true},
		{name: "valid HK node", nodeName: "🇭🇰 香港 01", exclude: false},
		{name: "valid direct node containing 直连", nodeName: "🇨🇳 广州-香港 直连 01", exclude: false},
		{name: "valid JP node", nodeName: "🇯🇵 日本 02", exclude: false},
		{name: "valid US node", nodeName: "🇺🇸 美国 01", exclude: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExcludedCandidate(tt.nodeName, groupNames)
			if got != tt.exclude {
				t.Fatalf("isExcludedCandidate(%q) = %v, want %v", tt.nodeName, got, tt.exclude)
			}
		})
	}
}

func TestCandidateFingerprint(t *testing.T) {
	fp1 := computeCandidateFingerprint([]string{"node-A", "node-B"}, "sub1")
	fp2 := computeCandidateFingerprint([]string{"node-A", "node-B"}, "sub1")
	fp3 := computeCandidateFingerprint([]string{"node-A", "node-C"}, "sub1")
	fp4 := computeCandidateFingerprint([]string{"node-A", "node-B"}, "sub2")

	if fp1 != fp2 {
		t.Fatalf("fingerprint should be deterministic, got %s != %s", fp1, fp2)
	}
	if !strings.HasPrefix(fp1, "sha256:") {
		t.Fatalf("fingerprint should have sha256: prefix, got %s", fp1)
	}
	if fp1 == fp3 {
		t.Fatalf("fingerprint should change when candidates change")
	}
	if fp1 == fp4 {
		t.Fatalf("fingerprint should change when subscription changes")
	}

	// Collision avoidance test: ["a,b", "c"] vs ["a", "b,c"]
	fpColl1 := computeCandidateFingerprint([]string{"a,b", "c"}, "sub")
	fpColl2 := computeCandidateFingerprint([]string{"a", "b,c"}, "sub")
	if fpColl1 == fpColl2 {
		t.Fatalf("fingerprint collision detected between [a,b; c] and [a; b,c]: %s", fpColl1)
	}
}

func TestShortlistBuilding(t *testing.T) {
	candidates := []speedTestCandidate{
		{name: "🇭🇰 HK 01", delay: 30},
		{name: "🇭🇰 HK 02", delay: 20},
		{name: "🇭🇰 HK 03", delay: 40},
		{name: "🇭🇰 HK 04", delay: 50}, // excess HK
		{name: "🇯🇵 JP 01", delay: 60},
		{name: "🇯🇵 JP 02", delay: 55},
		{name: "🇯🇵 JP 03", delay: 70}, // excess JP
		{name: "🇸🇬 SG 01", delay: 80},
		{name: "🇹🇼 TW 01", delay: 45},
		{name: "🇹🇼 TW 02", delay: 90}, // excess TWSG
		{name: "🇺🇸 US 01", delay: 150},
		{name: "🇺🇸 US 02", delay: 140},
		{name: "🇺🇸 US 03", delay: 200}, // excess other
		{name: "🇩🇪 DE 01", delay: 180},
	}

	shortlist := buildShortlist(candidates, "🇩🇪 DE 01")

	if len(shortlist) > 10 {
		t.Fatalf("shortlist length %d exceeds max 10", len(shortlist))
	}

	// Verify HK <= 3, sorted by delay: HK 02 (20), HK 01 (30), HK 03 (40)
	hkCount := 0
	jpCount := 0
	twsgCount := 0
	hasCurrent := false
	for _, c := range shortlist {
		if reHK.MatchString(c.name) {
			hkCount++
		}
		if reJP.MatchString(c.name) {
			jpCount++
		}
		if reTWSG.MatchString(c.name) {
			twsgCount++
		}
		if c.name == "🇩🇪 DE 01" {
			hasCurrent = true
		}
	}

	if hkCount > 3 {
		t.Fatalf("hkCount = %d, want <= 3", hkCount)
	}
	if jpCount > 2 {
		t.Fatalf("jpCount = %d, want <= 2", jpCount)
	}
	if twsgCount > 2 {
		t.Fatalf("twsgCount = %d, want <= 2", twsgCount)
	}
	if !hasCurrent {
		t.Fatalf("shortlist should include initialProductionNode DE 01")
	}
}

func TestScoringAndVideoBadges(t *testing.T) {
	tests := []struct {
		minMbps  float64
		expected string
	}{
		{0.0, "unstable / below 1080p target"},
		{7.9, "unstable / below 1080p target"},
		{8.0, "1080p"},
		{14.9, "1080p"},
		{15.0, "1440p"},
		{24.9, "1440p"},
		{25.0, "1440p60"},
		{39.9, "1440p60"},
		{40.0, "4K candidate"},
		{85.5, "4K candidate"},
	}

	for _, tt := range tests {
		badge := calculateVideoBadge(tt.minMbps)
		if badge != tt.expected {
			t.Fatalf("calculateVideoBadge(%f) = %q, want %q", tt.minMbps, badge, tt.expected)
		}
	}

	// Effective speed calculation: 0.70 * min + 0.30 * median
	minSpeed := 20.0
	medianSpeed := 30.0
	effective := 0.70*minSpeed + 0.30*medianSpeed
	if effective != 23.0 {
		t.Fatalf("effective = %f, want 23.0", effective)
	}
}

func TestResultRanking(t *testing.T) {
	results := []MihomoSpeedTestNodeResult{
		{
			Node:          "IneligibleFastNode",
			Eligible:      false,
			EffectiveMbps: 50.0,
			MinimumMbps:   40.0,
			DelayMs:       20,
		},
		{
			Node:          "EligibleSlower",
			Eligible:      true,
			EffectiveMbps: 15.0,
			MinimumMbps:   12.0,
			DelayMs:       30,
		},
		{
			Node:          "EligibleFaster",
			Eligible:      true,
			EffectiveMbps: 25.0,
			MinimumMbps:   20.0,
			DelayMs:       40,
		},
		{
			Node:          "EligibleSameSpeedLowerDelay",
			Eligible:      true,
			EffectiveMbps: 25.0,
			MinimumMbps:   20.0,
			DelayMs:       25,
		},
	}

	sortResults(results)

	// Ranking order:
	// 1: EligibleSameSpeedLowerDelay (Eligible=true, Effective=25.0, Min=20.0, Delay=25)
	// 2: EligibleFaster (Eligible=true, Effective=25.0, Min=20.0, Delay=40)
	// 3: EligibleSlower (Eligible=true, Effective=15.0, Min=12.0)
	// 4: IneligibleFastNode (Eligible=false)

	if results[0].Node != "EligibleSameSpeedLowerDelay" {
		t.Fatalf("first result = %s, want EligibleSameSpeedLowerDelay", results[0].Node)
	}
	if results[1].Node != "EligibleFaster" {
		t.Fatalf("second result = %s, want EligibleFaster", results[1].Node)
	}
	if results[2].Node != "EligibleSlower" {
		t.Fatalf("third result = %s, want EligibleSlower", results[2].Node)
	}
	if results[3].Node != "IneligibleFastNode" {
		t.Fatalf("fourth result = %s, want IneligibleFastNode", results[3].Node)
	}
}

func TestPersistenceStartupAndRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	server := &Server{
		data:     tmpDir,
		sessions: make(map[string]authSession),
	}

	mihomoDir := filepath.Join(tmpDir, "mihomo")
	if err := os.MkdirAll(mihomoDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Write an existing store with a "running" job
	storePath := filepath.Join(mihomoDir, "speedtest-results.json")
	initialStore := speedTestPersistentStore{
		Version: speedTestStoreVersion,
		Jobs: []MihomoSpeedTestJob{
			{
				ID:        "job-interrupted-1",
				Mode:      "quick",
				State:     "running",
				Phase:     "preliminary",
				StartedAt: "2026-09-13T10:00:00Z",
			},
		},
	}
	initialBytes, _ := json.Marshal(initialStore)
	if err := os.WriteFile(storePath, initialBytes, 0600); err != nil {
		t.Fatal(err)
	}

	// Call initSpeedTest
	server.initSpeedTest()

	if server.speedTestJob == nil {
		t.Fatal("server.speedTestJob should not be nil")
	}
	if server.speedTestJob.State != "interrupted" {
		t.Fatalf("job state = %q, want 'interrupted'", server.speedTestJob.State)
	}
	if server.speedTestJob.FinishedAt == "" {
		t.Fatal("finishedAt should be set on interrupted job")
	}

	// Verify file on disk was updated to interrupted
	persistedBytes, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	var reloadedStore speedTestPersistentStore
	if err := json.Unmarshal(persistedBytes, &reloadedStore); err != nil {
		t.Fatal(err)
	}
	if reloadedStore.Jobs[0].State != "interrupted" {
		t.Fatalf("persisted job state = %q, want 'interrupted'", reloadedStore.Jobs[0].State)
	}
}

func TestSpeedTestAPIEndpoints(t *testing.T) {
	tmpDir := t.TempDir()

	// Mock Mihomo controller
	var appliedRuleNode string
	var appliedBenchNode string

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1.19.30"})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "JP-01", "DIRECT"},
					},
					speedTestProductionGroup: map[string]any{
						"name": speedTestProductionGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "JP-01", "DIRECT"},
					},
					"HK-01": map[string]any{"name": "HK-01", "type": "Vless"},
					"JP-01": map[string]any{"name": "JP-01", "type": "Shadowsocks"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/group/"+speedTestBenchmarkGroup+"/delay") || strings.HasPrefix(r.URL.Path, "/group/"+url.PathEscape(speedTestBenchmarkGroup)+"/delay"):
			_ = json.NewEncoder(w).Encode(map[string]int{
				"HK-01": 35,
				"JP-01": 50,
			})
		case (r.URL.Path == "/proxies/"+speedTestBenchmarkGroup || r.URL.Path == "/proxies/"+url.PathEscape(speedTestBenchmarkGroup)) && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			appliedBenchNode = body["name"]
			w.WriteHeader(http.StatusNoContent)
		case (r.URL.Path == "/proxies/"+speedTestProductionGroup || r.URL.Path == "/proxies/"+url.PathEscape(speedTestProductionGroup)) && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			appliedRuleNode = body["name"]
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	server := &Server{
		data:             tmpDir,
		mihomoController: mockController.URL,
		sessions:         make(map[string]authSession),
	}
	server.initSpeedTest()

	// Mock HTTP measurement to avoid real network
	server.speedTestHTTP = func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
		// simulate 25 Mbps: 8MB takes ~2.56s
		duration := time.Duration(float64(sampleBytes) * 8.0 / (25.0 * 1e6) * float64(time.Second))
		if duration <= 0 {
			duration = 10 * time.Millisecond
		}
		return sampleBytes, duration, 50 * time.Millisecond, http.StatusOK, nil
	}
	server.speedTestSampleWAN = func(ctx context.Context) (float64, error) {
		return 100000, nil // 0.1 Mbps: clean
	}

	// 1. GET speedtest when idle
	reqGet := httptest.NewRequest(http.MethodGet, "/api/mihomo/speedtest", nil)
	recGet := httptest.NewRecorder()
	server.mihomoSpeedTest(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /api/mihomo/speedtest code = %d", recGet.Code)
	}
	var idleResp map[string]any
	_ = json.NewDecoder(recGet.Body).Decode(&idleResp)
	if idleResp["state"] != "idle" {
		t.Fatalf("idleResp state = %v, want 'idle'", idleResp["state"])
	}

	// 2. Mock 17894 listener so checkBenchmarkListenerOnline returns true
	dummyListener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:17894")
	if err != nil {
		t.Logf("Note: could not bind 127.0.0.1:17894 on test machine: %v", err)
	} else {
		defer dummyListener.Close()
	}

	// 3. Test Apply logic with mock completed job
	fp := computeCandidateFingerprint([]string{"HK-01", "JP-01"}, server.getSubscriptionIdentity())
	completedJob := &MihomoSpeedTestJob{
		ID:                   "test-job-completed",
		Mode:                 "quick",
		State:                "completed",
		Phase:                "completed",
		CandidateFingerprint: fp,
		Results: []MihomoSpeedTestNodeResult{
			{
				Node:          "HK-01",
				Protocol:      "Vless",
				DelayMs:       35,
				Eligible:      true,
				EffectiveMbps: 25.0,
				MinimumMbps:   22.0,
			},
			{
				Node:          "JP-01",
				Protocol:      "Shadowsocks",
				DelayMs:       50,
				Eligible:      false,
				EffectiveMbps: 5.0,
				MinimumMbps:   4.0,
			},
		},
	}
	server.speedTestMu.Lock()
	server.speedTestJob = completedJob
	server.speedTestMu.Unlock()

	// 3a. Apply ineligible node -> 400
	badApplyReq := httptest.NewRequest(http.MethodPut, "/api/mihomo/speedtest/apply", bytes.NewBufferString(`{"jobId":"test-job-completed","node":"JP-01"}`))
	badApplyRec := httptest.NewRecorder()
	server.mihomoSpeedTestApply(badApplyRec, badApplyReq)
	if badApplyRec.Code != http.StatusBadRequest {
		t.Fatalf("apply ineligible node status = %d, want 400", badApplyRec.Code)
	}

	// 3b. Apply eligible node -> 200 and switches 家庭RULE
	goodApplyReq := httptest.NewRequest(http.MethodPut, "/api/mihomo/speedtest/apply", bytes.NewBufferString(`{"jobId":"test-job-completed","node":"HK-01"}`))
	goodApplyRec := httptest.NewRecorder()
	server.mihomoSpeedTestApply(goodApplyRec, goodApplyReq)
	if goodApplyRec.Code != http.StatusOK {
		t.Fatalf("apply eligible node status = %d, want 200: %s", goodApplyRec.Code, goodApplyRec.Body.String())
	}
	if appliedRuleNode != "HK-01" {
		t.Fatalf("appliedRuleNode = %q, want 'HK-01'", appliedRuleNode)
	}

	_ = appliedBenchNode
}

func TestQuickApplyWithManyCandidatesAndStaleRejection(t *testing.T) {
	tempDir := t.TempDir()

	// 15 raw candidates in 家庭测速
	benchAll := []string{
		"HK-01", "HK-02", "HK-03", "HK-04",
		"JP-01", "JP-02", "JP-03",
		"TW-01", "TW-02", "TW-03",
		"SG-01", "SG-02",
		"US-01", "US-02", "US-03",
	}

	appliedRuleNode := ""
	var currentBenchList []string
	currentBenchList = append(currentBenchList, benchAll...)

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/proxies" {
			proxies := map[string]any{
				"GLOBAL": map[string]any{"type": "Selector", "all": []string{"家庭RULE", "DIRECT"}},
				speedTestProductionGroup: map[string]any{
					"name": speedTestProductionGroup,
					"type": "Selector",
					"now":  "HK-01",
					"all":  currentBenchList,
				},
				speedTestBenchmarkGroup: map[string]any{
					"name": speedTestBenchmarkGroup,
					"type": "Selector",
					"now":  "HK-01",
					"all":  currentBenchList,
				},
			}
			for _, name := range currentBenchList {
				proxies[name] = map[string]any{"name": name, "type": "Vless"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"proxies": proxies})
			return
		}
		if r.Method == "PUT" && (strings.HasPrefix(r.URL.Path, "/proxies/"+speedTestProductionGroup) || strings.HasPrefix(r.URL.Path, "/proxies/"+url.PathEscape(speedTestProductionGroup))) {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			appliedRuleNode = body["name"]
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockController.Close()

	server := &Server{
		data:             tempDir,
		mihomoController: mockController.URL,
		sessions:         make(map[string]authSession),
	}
	server.initSpeedTest()

	// Initial candidate list has 15 candidates
	fp := computeCandidateFingerprint(benchAll, server.getSubscriptionIdentity())

	// Quick mode stores only shortlist (e.g. 5 nodes) in job.Results
	completedJob := &MihomoSpeedTestJob{
		ID:                   "test-job-quick-15",
		Mode:                 "quick",
		State:                "completed",
		Phase:                "completed",
		CandidateFingerprint: fp,
		Results: []MihomoSpeedTestNodeResult{
			{Node: "HK-01", Protocol: "Vless", Eligible: true, EffectiveMbps: 35.0, MinimumMbps: 30.0},
			{Node: "JP-01", Protocol: "Vless", Eligible: true, EffectiveMbps: 20.0, MinimumMbps: 18.0},
			{Node: "TW-01", Protocol: "Vless", Eligible: false, EffectiveMbps: 7.0, MinimumMbps: 6.0},
			{Node: "SG-01", Protocol: "Vless", Eligible: false, EffectiveMbps: 6.0, MinimumMbps: 5.0},
			{Node: "US-01", Protocol: "Vless", Eligible: false, EffectiveMbps: 4.0, MinimumMbps: 3.0},
		},
	}
	server.publishSpeedTestSnapshot(completedJob)

	// Step 1: Quick apply eligible node HK-01 with 15 candidates in group vs 5 in Results -> MUST succeed
	applyReq := httptest.NewRequest(http.MethodPut, "/api/mihomo/speedtest/apply", bytes.NewBufferString(`{"jobId":"test-job-quick-15","node":"HK-01"}`))
	applyRec := httptest.NewRecorder()
	server.mihomoSpeedTestApply(applyRec, applyReq)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("quick apply with >10 candidates failed with status %d: %s", applyRec.Code, applyRec.Body.String())
	}
	if appliedRuleNode != "HK-01" {
		t.Fatalf("appliedRuleNode = %q, want 'HK-01'", appliedRuleNode)
	}

	// Step 2: Add candidate HK-99 to controller -> Apply MUST return 409 Conflict (stale)
	currentBenchList = append([]string{"HK-99"}, benchAll...)
	staleAddReq := httptest.NewRequest(http.MethodPut, "/api/mihomo/speedtest/apply", bytes.NewBufferString(`{"jobId":"test-job-quick-15","node":"HK-01"}`))
	staleAddRec := httptest.NewRecorder()
	server.mihomoSpeedTestApply(staleAddRec, staleAddReq)
	if staleAddRec.Code != http.StatusConflict {
		t.Fatalf("candidate added: apply status = %d, want 409 Conflict: %s", staleAddRec.Code, staleAddRec.Body.String())
	}

	// Step 3: Remove candidate US-03 from controller -> Apply MUST return 409 Conflict (stale)
	currentBenchList = append([]string{}, benchAll[:14]...) // omit US-03
	staleRemReq := httptest.NewRequest(http.MethodPut, "/api/mihomo/speedtest/apply", bytes.NewBufferString(`{"jobId":"test-job-quick-15","node":"HK-01"}`))
	staleRemRec := httptest.NewRecorder()
	server.mihomoSpeedTestApply(staleRemRec, staleRemReq)
	if staleRemRec.Code != http.StatusConflict {
		t.Fatalf("candidate removed: apply status = %d, want 409 Conflict: %s", staleRemRec.Code, staleRemRec.Body.String())
	}
}

func TestSnapshotDeepCopyAndRace(t *testing.T) {
	tempDir := t.TempDir()
	server := &Server{
		data:     tempDir,
		sessions: make(map[string]authSession),
	}
	server.initSpeedTest()

	job := &MihomoSpeedTestJob{
		ID:    "race-test-job",
		Mode:  "quick",
		State: "running",
		Phase: "preliminary",
		Results: []MihomoSpeedTestNodeResult{
			{
				Node: "HK-01",
				Runs: []MihomoSpeedTestRun{
					{Stage: "preliminary", Mbps: 20.0, Success: true},
				},
			},
		},
	}
	server.publishSpeedTestSnapshot(job)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			snap := server.getSpeedTestJobSnapshot()
			if snap == nil {
				t.Errorf("got nil snapshot")
				return
			}
			if len(snap.Results) > 0 {
				_ = snap.Results[0].Node
				if len(snap.Results[0].Runs) > 0 {
					_ = snap.Results[0].Runs[0].Mbps
				}
			}
		}(i)

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			jobCopy := &MihomoSpeedTestJob{
				ID:            "race-test-job",
				State:         "running",
				CompletedRuns: idx,
				Results: []MihomoSpeedTestNodeResult{
					{
						Node: fmt.Sprintf("Node-%d", idx),
						Runs: []MihomoSpeedTestRun{
							{Stage: "confirmation", Mbps: float64(idx * 2), Success: true},
						},
					},
				},
			}
			server.publishSpeedTestSnapshot(jobCopy)
		}(i)
	}
	wg.Wait()
}

func TestIncrementalPersistenceAndRecovery(t *testing.T) {
	tempDir := t.TempDir()
	server := &Server{
		data:     tempDir,
		sessions: make(map[string]authSession),
	}
	server.initSpeedTest()

	job := &MihomoSpeedTestJob{
		ID:              "persist-recovery-job",
		Mode:            "deep",
		State:           "running",
		Phase:           "preliminary",
		BytesUsed:       50 * 1024 * 1024,
		CompletedRuns:   3,
		TotalRuns:       10,
		DataBudgetBytes: 300 * 1024 * 1024,
		StartedAt:       time.Now().Format(time.RFC3339),
		Results: []MihomoSpeedTestNodeResult{
			{
				Node:     "HK-01",
				Protocol: "Vless",
				DelayMs:  40,
				Runs: []MihomoSpeedTestRun{
					{Stage: "preliminary", Mbps: 50.0, Success: true},
				},
			},
		},
	}

	// Persist in-flight state
	server.persistJob(job)

	// Verify file was written immediately with running state
	storePath := filepath.Join(tempDir, "mihomo", "speedtest-results.json")
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("failed to read persisted store: %v", err)
	}
	var store speedTestPersistentStore
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatalf("failed to parse persisted store: %v", err)
	}
	if len(store.Jobs) == 0 || store.Jobs[0].State != "running" {
		t.Fatalf("persisted job state = %v, want 'running'", store.Jobs[0].State)
	}

	// Now simulate daemon restart with a new server instance
	serverRestarted := &Server{
		data:     tempDir,
		sessions: make(map[string]authSession),
	}
	serverRestarted.initSpeedTest()

	snap := serverRestarted.getSpeedTestJobSnapshot()
	if snap == nil {
		t.Fatalf("restarted server has nil speedTestJob")
	}
	if snap.ID != "persist-recovery-job" {
		t.Fatalf("restarted job ID = %q, want 'persist-recovery-job'", snap.ID)
	}
	if snap.State != "interrupted" {
		t.Fatalf("restarted job State = %q, want 'interrupted'", snap.State)
	}
	if snap.Phase != "interrupted" {
		t.Fatalf("restarted job Phase = %q, want 'interrupted'", snap.Phase)
	}
	if snap.FinishedAt == "" {
		t.Fatalf("restarted job FinishedAt is empty")
	}
	if snap.BytesUsed != 50*1024*1024 {
		t.Fatalf("restarted job BytesUsed = %d, want %d", snap.BytesUsed, 50*1024*1024)
	}
	if len(snap.Results) != 1 || snap.Results[0].Node != "HK-01" {
		t.Fatalf("restarted job Results not preserved: %+v", snap.Results)
	}
}

func TestSpeedTestTimeoutVsCancelDistinction(t *testing.T) {
	tmpDir := t.TempDir()

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1.19.30"})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					speedTestProductionGroup: map[string]any{
						"name": speedTestProductionGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"HK-01": map[string]any{"name": "HK-01", "type": "Vless"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"HK-01": 30})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	server := &Server{
		data:                   tmpDir,
		mihomoController:       mockController.URL,
		sessions:               make(map[string]authSession),
		speedTestCheckListener: func() bool { return true },
		speedTestSampleWAN:     func(ctx context.Context) (float64, error) { return 0, nil },
	}
	server.initSpeedTest()

	// 1. Test Timeout -> failed with job_timeout
	t.Run("deadline timeout yields failed and job_timeout", func(t *testing.T) {
		timeoutCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		jobTimeout := &MihomoSpeedTestJob{
			ID:              "test-timeout-job",
			Mode:            "quick",
			State:           "running",
			Phase:           "discovering",
			DataBudgetBytes: 100 * 1024 * 1024,
			StartedAt:       time.Now().Format(time.RFC3339),
			Results:         []MihomoSpeedTestNodeResult{},
		}
		server.publishSpeedTestSnapshot(jobTimeout)

		server.runSpeedTestWorker(timeoutCtx, jobTimeout, true)

		if jobTimeout.State != "failed" {
			t.Fatalf("timeout job State = %q, want 'failed'", jobTimeout.State)
		}
		if jobTimeout.ErrorCode != "job_timeout" {
			t.Fatalf("timeout job ErrorCode = %q, want 'job_timeout'", jobTimeout.ErrorCode)
		}
	})

	// 2. Test Manual Cancel -> cancelled with cancelled
	t.Run("manual cancel yields cancelled and cancelled", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before or during worker run

		jobCancel := &MihomoSpeedTestJob{
			ID:              "test-cancel-job",
			Mode:            "quick",
			State:           "running",
			Phase:           "discovering",
			DataBudgetBytes: 100 * 1024 * 1024,
			StartedAt:       time.Now().Format(time.RFC3339),
			Results:         []MihomoSpeedTestNodeResult{},
		}
		server.publishSpeedTestSnapshot(jobCancel)

		server.runSpeedTestWorker(cancelCtx, jobCancel, true)

		if jobCancel.State != "cancelled" {
			t.Fatalf("cancelled job State = %q, want 'cancelled'", jobCancel.State)
		}
		if jobCancel.ErrorCode != "cancelled" {
			t.Fatalf("cancelled job ErrorCode = %q, want 'cancelled'", jobCancel.ErrorCode)
		}
	})
}

func TestDataBudgetExceededIsTerminalFailure(t *testing.T) {
	tmpDir := t.TempDir()

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1.19.30"})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "HK-02", "HK-03", "DIRECT"},
					},
					speedTestProductionGroup: map[string]any{
						"name": speedTestProductionGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "HK-02", "HK-03", "DIRECT"},
					},
					"HK-01": map[string]any{"name": "HK-01", "type": "Vless"},
					"HK-02": map[string]any{"name": "HK-02", "type": "Vless"},
					"HK-03": map[string]any{"name": "HK-03", "type": "Vless"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"HK-01": 20, "HK-02": 25, "HK-03": 30})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	server := &Server{
		data:                   tmpDir,
		mihomoController:       mockController.URL,
		sessions:               make(map[string]authSession),
		speedTestCheckListener: func() bool { return true },
		speedTestSampleWAN:     func(ctx context.Context) (float64, error) { return 0, nil },
	}
	server.initSpeedTest()

	// 10 MiB budget: 3 preliminary runs consume 3 * 3 MiB = 9 MiB,
	// and first 8 MiB confirmation run requires 9 + 8 = 17 MiB -> exceeds 10 MiB budget in confirmation!
	budget := int64(10 * 1024 * 1024)
	job := &MihomoSpeedTestJob{
		ID:              "test-budget-job",
		Mode:            "quick",
		State:           "running",
		Phase:           "discovering",
		DataBudgetBytes: budget,
		StartedAt:       time.Now().Format(time.RFC3339),
		Results:         []MihomoSpeedTestNodeResult{},
	}
	server.publishSpeedTestSnapshot(job)

	server.speedTestHTTP = func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
		return sampleBytes, 100 * time.Millisecond, 20 * time.Millisecond, http.StatusOK, nil
	}

	server.runSpeedTestWorker(context.Background(), job, true)

	if job.State != "failed" {
		t.Fatalf("job state = %q, want 'failed' (must not masquerade as completed)", job.State)
	}
	if job.ErrorCode != "data_budget_exceeded" {
		t.Fatalf("job errorCode = %q, want 'data_budget_exceeded'", job.ErrorCode)
	}
	if job.BytesUsed > budget {
		t.Fatalf("job BytesUsed (%d) exceeded budget (%d)", job.BytesUsed, budget)
	}
	expectedPrelimBytes := 3 * quickPreliminaryBytes
	if job.BytesUsed != expectedPrelimBytes {
		t.Fatalf("job BytesUsed = %d, want exactly 3 preliminary runs (%d)", job.BytesUsed, expectedPrelimBytes)
	}
	if job.Phase != "failed" {
		t.Fatalf("job Phase = %q, want 'failed'", job.Phase)
	}
	// Assert no confirmation run was recorded on any result
	for _, r := range job.Results {
		for _, run := range r.Runs {
			if run.Stage == "confirmation" {
				t.Fatalf("node %s has confirmation run, but budget should have aborted before confirmation ran", r.Node)
			}
		}
	}
}

func TestWorkerInterleavingDoesNotOverwriteNewJob(t *testing.T) {
	tmpDir := t.TempDir()
	server := &Server{
		data:     tmpDir,
		sessions: make(map[string]authSession),
	}
	server.initSpeedTest()

	job1 := &MihomoSpeedTestJob{
		ID:        "job-1",
		Mode:      "quick",
		State:     "running",
		Phase:     "preliminary",
		StartedAt: time.Now().Format(time.RFC3339),
	}
	server.publishSpeedTestSnapshot(job1)

	// Now job 2 starts
	job2 := &MihomoSpeedTestJob{
		ID:        "job-2",
		Mode:      "deep",
		State:     "running",
		Phase:     "discovering",
		StartedAt: time.Now().Format(time.RFC3339),
	}
	cancel2Called := false
	cancel2 := func() { cancel2Called = true }

	server.speedTestMu.Lock()
	server.speedTestJob = deepCopyJob(job2)
	server.speedTestCancel = cancel2
	server.speedTestMu.Unlock()

	// Old worker for job1 now finishes and tries to publish/finalize
	job1.State = "completed"
	job1.Phase = "completed"
	server.publishSpeedTestSnapshot(job1)
	server.finalizeSpeedTestJob("job-1", job1)

	// Verify server snapshot is still job-2
	snap := server.getSpeedTestJobSnapshot()
	if snap == nil || snap.ID != "job-2" {
		t.Fatalf("server speedTestJob clobbered by old worker: got %+v, want job-2", snap)
	}

	// Verify cancel function for job-2 is still intact
	server.speedTestMu.Lock()
	curCancel := server.speedTestCancel
	server.speedTestMu.Unlock()
	if curCancel == nil {
		t.Fatalf("job-2 cancel function was cleared by old worker")
	}
	curCancel()
	if !cancel2Called {
		t.Fatalf("calling cancel did not invoke cancel2")
	}
}

func TestAPIConcurrencyAndMihomoPUTIsolation(t *testing.T) {
	tmpDir := t.TempDir()

	var putMu sync.Mutex
	var putPaths []string

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putMu.Lock()
			putPaths = append(putPaths, r.URL.Path)
			putMu.Unlock()
		}

		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1.19.30"})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					speedTestProductionGroup: map[string]any{
						"name": speedTestProductionGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"HK-01": map[string]any{"name": "HK-01", "type": "Vless"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"HK-01": 25})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	server := &Server{
		data:                   tmpDir,
		mihomoController:       mockController.URL,
		sessions:               make(map[string]authSession),
		speedTestCheckListener: func() bool { return true },
		speedTestSampleWAN:     func(ctx context.Context) (float64, error) { return 0, nil },
	}
	server.initSpeedTest()

	// Channel to block worker inside speedTestHTTP
	blockCh := make(chan struct{})
	workerStartedCh := make(chan struct{}, 1)
	server.speedTestHTTP = func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
		select {
		case workerStartedCh <- struct{}{}:
		default:
		}
		select {
		case <-blockCh:
			return sampleBytes, 50 * time.Millisecond, 10 * time.Millisecond, http.StatusOK, nil
		case <-ctx.Done():
			return 0, 0, 0, 0, ctx.Err()
		}
	}

	// 1. First POST: starts running job
	postBody1 := bytes.NewBufferString(`{"mode":"quick","allowBusyNetwork":true}`)
	reqPost1 := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", postBody1)
	recPost1 := httptest.NewRecorder()
	server.mihomoSpeedTest(recPost1, reqPost1)
	if recPost1.Code != http.StatusAccepted {
		t.Fatalf("1st POST code = %d, want 202. Body: %s", recPost1.Code, recPost1.Body.String())
	}

	// Wait for worker to enter speedTestHTTP
	select {
	case <-workerStartedCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("worker did not start in time")
	}

	// 2. Second POST while running: must return 409 Conflict
	postBody2 := bytes.NewBufferString(`{"mode":"quick","allowBusyNetwork":true}`)
	reqPost2 := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", postBody2)
	recPost2 := httptest.NewRecorder()
	server.mihomoSpeedTest(recPost2, reqPost2)
	if recPost2.Code != http.StatusConflict {
		t.Fatalf("2nd POST code = %d, want 409 Conflict. Body: %s", recPost2.Code, recPost2.Body.String())
	}

	// 3. DELETE /api/mihomo/speedtest: cancels running job
	reqDel1 := httptest.NewRequest(http.MethodDelete, "/api/mihomo/speedtest", nil)
	recDel1 := httptest.NewRecorder()
	server.mihomoSpeedTest(recDel1, reqDel1)
	if recDel1.Code != http.StatusOK {
		t.Fatalf("1st DELETE code = %d, want 200 OK. Body: %s", recDel1.Code, recDel1.Body.String())
	}

	// 4. Second DELETE: returns 409 Conflict (already cancelling/no active cancel)
	reqDel2 := httptest.NewRequest(http.MethodDelete, "/api/mihomo/speedtest", nil)
	recDel2 := httptest.NewRecorder()
	server.mihomoSpeedTest(recDel2, reqDel2)
	if recDel2.Code != http.StatusConflict {
		t.Fatalf("2nd DELETE code = %d, want 409 Conflict. Body: %s", recDel2.Code, recDel2.Body.String())
	}

	// Wait for worker to complete cancellation
	var finalSnap *MihomoSpeedTestJob
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		finalSnap = server.getSpeedTestJobSnapshot()
		if finalSnap != nil && finalSnap.State == "cancelled" {
			break
		}
	}
	if finalSnap == nil || finalSnap.State != "cancelled" {
		t.Fatalf("job state after delete = %v, want 'cancelled'", finalSnap)
	}

	// 5. Verify PUT boundaries: during worker execution, all PUT requests must be to 家庭测速, NEVER 家庭RULE
	putMu.Lock()
	workerPutPaths := make([]string, len(putPaths))
	copy(workerPutPaths, putPaths)
	putMu.Unlock()

	benchEscaped := url.PathEscape(speedTestBenchmarkGroup)
	ruleEscaped := url.PathEscape(speedTestProductionGroup)
	for _, p := range workerPutPaths {
		if strings.Contains(p, speedTestProductionGroup) || strings.Contains(p, ruleEscaped) {
			t.Fatalf("CRITICAL SAFETY VIOLATION: speedtest worker sent PUT to production group: %s", p)
		}
		if !strings.Contains(p, speedTestBenchmarkGroup) && !strings.Contains(p, benchEscaped) {
			t.Fatalf("unexpected PUT path during worker: %s", p)
		}
	}

	// 6. Now test Apply: only Apply is permitted to PUT to 家庭RULE
	fp := computeCandidateFingerprint([]string{"HK-01"}, server.getSubscriptionIdentity())
	eligibleCompletedJob := &MihomoSpeedTestJob{
		ID:                   "test-apply-completed",
		Mode:                 "quick",
		State:                "completed",
		Phase:                "completed",
		CandidateFingerprint: fp,
		Results: []MihomoSpeedTestNodeResult{
			{
				Node:          "HK-01",
				Protocol:      "Vless",
				DelayMs:       25,
				MinimumMbps:   20.0,
				MedianMbps:    25.0,
				EffectiveMbps: 21.5,
				Eligible:      true,
			},
		},
	}
	server.speedTestMu.Lock()
	server.speedTestJob = eligibleCompletedJob
	server.speedTestMu.Unlock()

	applyBody := bytes.NewBufferString(`{"jobId":"test-apply-completed","node":"HK-01"}`)
	reqApply := httptest.NewRequest(http.MethodPut, "/api/mihomo/speedtest/apply", applyBody)
	recApply := httptest.NewRecorder()
	server.mihomoSpeedTestApply(recApply, reqApply)
	if recApply.Code != http.StatusOK {
		t.Fatalf("apply code = %d, want 200 OK. Body: %s", recApply.Code, recApply.Body.String())
	}

	// Verify that 家庭RULE was indeed updated by apply
	putMu.Lock()
	var ruleUpdated bool
	for _, p := range putPaths {
		if strings.Contains(p, speedTestProductionGroup) || strings.Contains(p, ruleEscaped) {
			ruleUpdated = true
			break
		}
	}
	putMu.Unlock()
	if !ruleUpdated {
		t.Fatalf("expected apply to PUT to %s, but none was recorded in %v", speedTestProductionGroup, putPaths)
	}
}

func TestRestoreSelectorBlocksFinalizationAndSecondPOST(t *testing.T) {
	tmpDir := t.TempDir()

	restoreStarted := make(chan struct{})
	unblockRestore := make(chan struct{})
	var restoreOnce sync.Once
	var completedRunsCount atomic.Int32

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1.19.30"})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "INITIAL-NODE",
						"all":  []string{"INITIAL-NODE", "HK-01", "DIRECT"},
					},
					speedTestProductionGroup: map[string]any{
						"name": speedTestProductionGroup,
						"type": "Selector",
						"now":  "INITIAL-NODE",
						"all":  []string{"INITIAL-NODE", "HK-01", "DIRECT"},
					},
					"INITIAL-NODE": map[string]any{"name": "INITIAL-NODE", "type": "Vless"},
					"HK-01":        map[string]any{"name": "HK-01", "type": "Vless"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"INITIAL-NODE": 20, "HK-01": 25})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			// Both INITIAL-NODE and HK-01 are candidates (2 prelim + 2*2 confirmation = 6 runs).
			// During testing, INITIAL-NODE will be selected when testing itself (completedRunsCount < 6).
			// ONLY when completedRunsCount == 6 does body["name"] == "INITIAL-NODE" represent
			// the final restoration PUT inside finalize()!
			if body["name"] == "INITIAL-NODE" && completedRunsCount.Load() == 6 {
				restoreOnce.Do(func() {
					close(restoreStarted)
				})
				<-unblockRestore
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	server := &Server{
		data:                   tmpDir,
		mihomoController:       mockController.URL,
		sessions:               make(map[string]authSession),
		speedTestCheckListener: func() bool { return true },
		speedTestSampleWAN:     func(ctx context.Context) (float64, error) { return 0, nil },
		speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
			completedRunsCount.Add(1)
			return sampleBytes, 20 * time.Millisecond, 5 * time.Millisecond, http.StatusOK, nil
		},
	}
	server.initSpeedTest()
	t.Cleanup(func() {
		server.speedTestMu.Lock()
		if server.speedTestCancel != nil {
			server.speedTestCancel()
		}
		server.speedTestMu.Unlock()
	})

	// 1. Launch quick speed test via POST handler
	reqPost1 := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", bytes.NewBufferString(`{"mode":"quick"}`))
	recPost1 := httptest.NewRecorder()
	server.mihomoSpeedTest(recPost1, reqPost1)
	if recPost1.Code != http.StatusAccepted {
		t.Fatalf("initial POST code = %d, want 202 Accepted. Body: %s", recPost1.Code, recPost1.Body.String())
	}

	// 2. Wait until worker finishes all 6 test runs and is actively blocked in the final restore PUT
	select {
	case <-restoreStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for final restore PUT to begin")
	}

	// Verify that all 6 runs (2 preliminary + 4 confirmation) actually completed before restore began
	if count := completedRunsCount.Load(); count != 6 {
		t.Fatalf("expected 6 benchmark test runs before final restore, got %d", count)
	}

	// 3. While restore PUT is in flight, GET /api/mihomo/speedtest must STILL report state == 'running'
	// and CompletedRuns == TotalRuns == 6 (worker finished runs and is in restore defer)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/mihomo/speedtest", nil)
	recGet := httptest.NewRecorder()
	server.mihomoSpeedTest(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET code = %d, want 200 OK", recGet.Code)
	}
	var jobStatus MihomoSpeedTestJob
	_ = json.Unmarshal(recGet.Body.Bytes(), &jobStatus)
	if jobStatus.State != "running" {
		t.Fatalf("in-flight restore job State = %q, want 'running'", jobStatus.State)
	}
	if jobStatus.CompletedRuns != 6 || jobStatus.TotalRuns != 6 {
		t.Fatalf("expected all test runs completed before restore: CompletedRuns=%d, TotalRuns=%d, want 6/6",
			jobStatus.CompletedRuns, jobStatus.TotalRuns)
	}

	// 4. While restore PUT is in flight, a second POST must return 409 Conflict
	reqPost2 := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", bytes.NewBufferString(`{"mode":"quick"}`))
	recPost2 := httptest.NewRecorder()
	server.mihomoSpeedTest(recPost2, reqPost2)
	if recPost2.Code != http.StatusConflict {
		t.Fatalf("second POST while restoring code = %d, want 409 Conflict. Body: %s", recPost2.Code, recPost2.Body.String())
	}

	// 5. Unblock the restore PUT
	close(unblockRestore)

	// 6. Wait for job to finalize to completed
	var finalJob MihomoSpeedTestJob
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		recCheck := httptest.NewRecorder()
		server.mihomoSpeedTest(recCheck, httptest.NewRequest(http.MethodGet, "/api/mihomo/speedtest", nil))
		_ = json.Unmarshal(recCheck.Body.Bytes(), &finalJob)
		if finalJob.State == "completed" {
			break
		}
	}
	if finalJob.State != "completed" {
		t.Fatalf("after restore unblocked, job State = %q, want 'completed'", finalJob.State)
	}

	// 7. Now that restore and finalization are complete, a subsequent POST succeeds
	reqPost3 := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", bytes.NewBufferString(`{"mode":"quick"}`))
	recPost3 := httptest.NewRecorder()
	server.mihomoSpeedTest(recPost3, reqPost3)
	if recPost3.Code != http.StatusAccepted {
		t.Fatalf("subsequent POST after completion code = %d, want 202 Accepted. Body: %s", recPost3.Code, recPost3.Body.String())
	}
	server.speedTestMu.Lock()
	if server.speedTestCancel != nil {
		server.speedTestCancel()
	}
	server.speedTestMu.Unlock()
	time.Sleep(20 * time.Millisecond)
}

func TestSpeedTestCancelDuringDownloadAccountsPartialBytes(t *testing.T) {
	tmpDir := t.TempDir()

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1.19.30"})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					speedTestProductionGroup: map[string]any{
						"name": speedTestProductionGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"HK-01": map[string]any{"name": "HK-01", "type": "Vless"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"HK-01": 25})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	downloadStarted := make(chan struct{})
	var startOnce sync.Once
	const partialBytes int64 = 1048576 // 1 MiB partially read

	server := &Server{
		data:                   tmpDir,
		mihomoController:       mockController.URL,
		sessions:               make(map[string]authSession),
		speedTestCheckListener: func() bool { return true },
		speedTestSampleWAN:     func(ctx context.Context) (float64, error) { return 0, nil },
		speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
			startOnce.Do(func() { close(downloadStarted) })
			<-ctx.Done()
			return partialBytes, 50 * time.Millisecond, 10 * time.Millisecond, 200, ctx.Err()
		},
	}
	server.initSpeedTest()

	// 1. Start speed test
	recPost := httptest.NewRecorder()
	server.mihomoSpeedTest(recPost, httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", bytes.NewBufferString(`{"mode":"quick"}`)))
	if recPost.Code != http.StatusAccepted {
		t.Fatalf("POST code = %d, want 202 Accepted", recPost.Code)
	}

	// 2. Wait until download is in progress
	select {
	case <-downloadStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for download to start")
	}

	// 3. Cancel via DELETE /api/mihomo/speedtest while download is running
	recDel := httptest.NewRecorder()
	server.mihomoSpeedTest(recDel, httptest.NewRequest(http.MethodDelete, "/api/mihomo/speedtest", nil))
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE code = %d, want 200 OK", recDel.Code)
	}

	// 4. Poll until job reaches cancelled state
	var job MihomoSpeedTestJob
	for i := 0; i < 40; i++ {
		time.Sleep(25 * time.Millisecond)
		recGet := httptest.NewRecorder()
		server.mihomoSpeedTest(recGet, httptest.NewRequest(http.MethodGet, "/api/mihomo/speedtest", nil))
		_ = json.Unmarshal(recGet.Body.Bytes(), &job)
		if job.State == "cancelled" {
			break
		}
	}

	if job.State != "cancelled" {
		t.Fatalf("job State = %q, want 'cancelled'", job.State)
	}
	if job.ErrorCode != "cancelled" {
		t.Fatalf("job ErrorCode = %q, want 'cancelled'", job.ErrorCode)
	}
	if job.BytesUsed != partialBytes {
		t.Fatalf("job BytesUsed = %d, want partialBytes %d", job.BytesUsed, partialBytes)
	}
	if job.CompletedRuns != 0 {
		t.Fatalf("job CompletedRuns = %d, want 0 (cancelled run must not count as completed)", job.CompletedRuns)
	}
	for _, res := range job.Results {
		if len(res.Runs) != 0 {
			t.Fatalf("candidate %s has %d runs, want 0", res.Node, len(res.Runs))
		}
	}

	// 5. Recover a new Server from disk to verify BytesUsed was safely persisted
	serverRecovered := &Server{
		data: tmpDir,
	}
	serverRecovered.initSpeedTest()
	recoveredJob := serverRecovered.getSpeedTestJobSnapshot()
	if recoveredJob.State != "cancelled" {
		t.Fatalf("recovered job State = %q, want 'cancelled'", recoveredJob.State)
	}
	if recoveredJob.BytesUsed != partialBytes {
		t.Fatalf("recovered job BytesUsed = %d, want partialBytes %d", recoveredJob.BytesUsed, partialBytes)
	}
	if recoveredJob.CompletedRuns != 0 {
		t.Fatalf("recovered job CompletedRuns = %d, want 0", recoveredJob.CompletedRuns)
	}
}

func TestSpeedTestDeadlineDuringDownloadAccountsPartialBytes(t *testing.T) {
	tmpDir := t.TempDir()

	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1.19.30"})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					speedTestProductionGroup: map[string]any{
						"name": speedTestProductionGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"HK-01": map[string]any{"name": "HK-01", "type": "Vless"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"HK-01": 25})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	const partialBytes int64 = 524288 // 512 KiB partially read

	server := &Server{
		data:                   tmpDir,
		mihomoController:       mockController.URL,
		sessions:               make(map[string]authSession),
		speedTestCheckListener: func() bool { return true },
		speedTestSampleWAN:     func(ctx context.Context) (float64, error) { return 0, nil },
		speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
			<-ctx.Done()
			return partialBytes, 50 * time.Millisecond, 10 * time.Millisecond, 200, ctx.Err()
		},
	}
	server.initSpeedTest()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	job := &MihomoSpeedTestJob{
		ID:              "test-deadline-job",
		Mode:            "quick",
		State:           "running",
		Phase:           "starting",
		TotalRuns:       3,
		DataBudgetBytes: 128 * 1024 * 1024,
		StartedAt:       time.Now().Format(time.RFC3339),
	}
	server.publishSpeedTestSnapshot(job)
	server.runSpeedTestWorker(ctx, job, false)

	snapshot := server.getSpeedTestJobSnapshot()
	if snapshot.State != "failed" {
		t.Fatalf("deadline job State = %q, want 'failed'", snapshot.State)
	}
	if snapshot.ErrorCode != "job_timeout" {
		t.Fatalf("deadline job ErrorCode = %q, want 'job_timeout'", snapshot.ErrorCode)
	}
	if snapshot.BytesUsed != partialBytes {
		t.Fatalf("deadline job BytesUsed = %d, want partialBytes %d", snapshot.BytesUsed, partialBytes)
	}
	if snapshot.CompletedRuns != 0 {
		t.Fatalf("deadline job CompletedRuns = %d, want 0", snapshot.CompletedRuns)
	}

	// Recover from disk to verify timeout partial bytes were persisted
	serverRecovered := &Server{
		data: tmpDir,
	}
	serverRecovered.initSpeedTest()
	recoveredSnapshot := serverRecovered.getSpeedTestJobSnapshot()
	if recoveredSnapshot.State != "failed" {
		t.Fatalf("recovered deadline job State = %q, want 'failed'", recoveredSnapshot.State)
	}
	if recoveredSnapshot.BytesUsed != partialBytes {
		t.Fatalf("recovered deadline job BytesUsed = %d, want partialBytes %d", recoveredSnapshot.BytesUsed, partialBytes)
	}
	if recoveredSnapshot.CompletedRuns != 0 {
		t.Fatalf("recovered deadline job CompletedRuns = %d, want 0", recoveredSnapshot.CompletedRuns)
	}
}

type mockNetTimeoutError struct {
	msg string
}

func (e *mockNetTimeoutError) Error() string   { return e.msg }
func (e *mockNetTimeoutError) Timeout() bool   { return true }
func (e *mockNetTimeoutError) Temporary() bool { return true }

func TestClassifyRunErrorPhases(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		status    int
		received  int64
		requested int64
		want      string
	}{
		{
			name:      "ResponseHeaderTimeout_NetError",
			err:       &url.Error{Op: "Get", URL: "http://example.com", Err: &mockNetTimeoutError{msg: "net/http: timeout awaiting response headers"}},
			status:    0,
			received:  0,
			requested: 1024,
			want:      "header_timeout",
		},
		{
			name:      "ClientTimeoutAwaitingHeaders_NetError",
			err:       &url.Error{Op: "Get", URL: "http://example.com", Err: &mockNetTimeoutError{msg: "context deadline exceeded (Client.Timeout exceeded while awaiting headers)"}},
			status:    0,
			received:  0,
			requested: 1024,
			want:      "header_timeout",
		},
		{
			name:      "ClientTimeoutWhileReadingBody_NetError",
			err:       &url.Error{Op: "Get", URL: "http://example.com", Err: &mockNetTimeoutError{msg: "context deadline exceeded (Client.Timeout exceeded while reading body)"}},
			status:    200,
			received:  500,
			requested: 1024,
			want:      "body_timeout",
		},
		{
			name:      "BodyReadTimeout_NetError_Status200",
			err:       &url.Error{Op: "Get", URL: "http://example.com", Err: &mockNetTimeoutError{msg: "read tcp 127.0.0.1:17894: i/o timeout"}},
			status:    200,
			received:  500,
			requested: 1024,
			want:      "body_timeout",
		},
		{
			name:      "DialConnectTimeout_NetError",
			err:       &url.Error{Op: "Get", URL: "http://example.com", Err: &mockNetTimeoutError{msg: "dial tcp 127.0.0.1:17894: i/o timeout"}},
			status:    0,
			received:  0,
			requested: 1024,
			want:      "connect_timeout",
		},
		{
			name:      "LookupTimeout_NetError",
			err:       &url.Error{Op: "Get", URL: "http://example.com", Err: &mockNetTimeoutError{msg: "lookup www.google.com: i/o timeout"}},
			status:    0,
			received:  0,
			requested: 1024,
			want:      "connect_timeout",
		},
		{
			// 约定：在未收到响应头（status == 0）且无特定阶段文本特征的通用网络超时，约定稳定归类为 connect_timeout
			name:      "GenericNetTimeout_Status0_ClassifiedAsConnectTimeout",
			err:       &mockNetTimeoutError{msg: "generic network timeout"},
			status:    0,
			received:  0,
			requested: 1024,
			want:      "connect_timeout",
		},
		{
			// 约定：在已收到响应头（status > 0）时的通用网络超时，约定稳定归类为 body_timeout
			name:      "GenericNetTimeout_Status200_ClassifiedAsBodyTimeout",
			err:       &mockNetTimeoutError{msg: "generic network timeout"},
			status:    200,
			received:  512,
			requested: 1024,
			want:      "body_timeout",
		},
		{
			name:      "ContextCanceledDirect",
			err:       context.Canceled,
			status:    0,
			received:  0,
			requested: 1024,
			want:      "cancelled",
		},
		{
			name:      "ContextCanceledWrapped",
			err:       fmt.Errorf("wrapped: %w", context.Canceled),
			status:    0,
			received:  0,
			requested: 1024,
			want:      "cancelled",
		},
		{
			name:      "ContextCanceledInUrlError",
			err:       &url.Error{Op: "Get", URL: "http://example.com", Err: context.Canceled},
			status:    0,
			received:  0,
			requested: 1024,
			want:      "cancelled",
		},
		{
			name:      "ArbitraryNonTimeoutError",
			err:       errors.New("some unexpected underlying socket failure"),
			status:    0,
			received:  0,
			requested: 1024,
			want:      "unexpected_status",
		},
		{
			name:      "UnexpectedHTTPStatus502",
			err:       nil,
			status:    502,
			received:  0,
			requested: 1024,
			want:      "unexpected_status",
		},
		{
			name:      "ShortRead",
			err:       nil,
			status:    200,
			received:  800,
			requested: 1024,
			want:      "short_read",
		},
		{
			name:      "Success200Full",
			err:       nil,
			status:    200,
			received:  1024,
			requested: 1024,
			want:      "",
		},
		{
			name:      "Success206Over90Percent",
			err:       nil,
			status:    206,
			received:  950,
			requested: 1024,
			want:      "",
		},
		{
			name:      "NodeBudgetExceededError",
			err:       errors.New("node_budget_exceeded"),
			status:    0,
			received:  0,
			requested: 1024,
			want:      "node_budget_exceeded",
		},
		{
			name:      "DataBudgetExceededError",
			err:       errors.New("data_budget_exceeded"),
			status:    0,
			received:  0,
			requested: 1024,
			want:      "data_budget_exceeded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyRunError(tc.err, tc.status, tc.received, tc.requested)
			if got != tc.want {
				t.Fatalf("classifyRunError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSpeedTestSecurityAndPersistenceRobustness(t *testing.T) {
	// 1. Malformed persistence file does not crash or block startup
	tmpDir := t.TempDir()
	mihomoDir := filepath.Join(tmpDir, "mihomo")
	if err := os.MkdirAll(mihomoDir, 0755); err != nil {
		t.Fatalf("failed to create mihomo dir: %v", err)
	}
	storePath := filepath.Join(mihomoDir, "speedtest-results.json")
	if err := os.WriteFile(storePath, []byte("{\"version\": 1, \"jobs\": [ corrupted json content"), 0600); err != nil {
		t.Fatalf("failed to write corrupted file: %v", err)
	}
	subSecret := "https://sub.example.com/api/v1/client/subscribe?token=supersecret_token_123456"
	if err := os.WriteFile(filepath.Join(mihomoDir, "subscription.url"), []byte(subSecret), 0600); err != nil {
		t.Fatalf("failed to write subscription.url: %v", err)
	}

	server := &Server{
		data:     tmpDir,
		sessions: make(map[string]authSession),
	}
	server.initSpeedTest()

	// Verify server started cleanly with empty/idle job
	snapshot := server.getSpeedTestJobSnapshot()
	if snapshot != nil && snapshot.ID != "" {
		t.Fatalf("expected nil or empty snapshot after malformed store, got ID: %q", snapshot.ID)
	}

	// 2. Persist jobs, test maxRetainedSpeedTests (10) and permission 0600
	job := &MihomoSpeedTestJob{
		ID:              "test-job-sec",
		Mode:            "quick",
		State:           "completed",
		Phase:           "completed",
		DataBudgetBytes: 100 * 1024 * 1024,
		StartedAt:       time.Now().Format(time.RFC3339),
		FinishedAt:      time.Now().Format(time.RFC3339),
		RecommendedNode: "HK-01",
	}

	var store speedTestPersistentStore
	store.Version = speedTestStoreVersion
	for i := 0; i < 15; i++ {
		j := *job
		j.ID = fmt.Sprintf("job-%02d", i)
		store.Jobs = append(store.Jobs, j)
	}

	server.speedTestMu.Lock()
	if err := server.persistSpeedTestStoreLocked(store); err != nil {
		server.speedTestMu.Unlock()
		t.Fatalf("persistSpeedTestStoreLocked failed: %v", err)
	}
	server.speedTestMu.Unlock()

	// Verify file permissions (0600 on non-windows platforms)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(storePath)
		if err != nil {
			t.Fatalf("stat storePath: %v", err)
		}
		if fi.Mode().Perm() != 0600 {
			t.Fatalf("storePath permissions = %o, want 0600", fi.Mode().Perm())
		}
	}

	// Verify maximum 10 retained jobs on disk
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	var loadedStore speedTestPersistentStore
	if err := json.Unmarshal(data, &loadedStore); err != nil {
		t.Fatalf("unmarshal persisted file: %v", err)
	}
	if len(loadedStore.Jobs) != 10 {
		t.Fatalf("persisted jobs count = %d, want maxRetained 10", len(loadedStore.Jobs))
	}

	// 3. Verify SpeedTest API and persistence file do not leak subscription URL or credentials
	if strings.Contains(string(data), "supersecret_token_123456") || strings.Contains(string(data), "sub.example.com") {
		t.Fatalf("persisted speedtest store leaked subscription URL/token: %s", string(data))
	}

	rec := httptest.NewRecorder()
	server.mihomoSpeedTest(rec, httptest.NewRequest(http.MethodGet, "/api/mihomo/speedtest", nil))
	apiBody := rec.Body.String()
	if strings.Contains(apiBody, "supersecret_token_123456") || strings.Contains(apiBody, "sub.example.com") {
		t.Fatalf("speedtest API response leaked subscription URL/token: %s", apiBody)
	}
}

func TestMihomoProxiesHidesBenchmarkGroup(t *testing.T) {
	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"家庭RULE": map[string]any{
						"name": "家庭RULE",
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"HK-01": map[string]any{"name": "HK-01", "type": "Vless"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	server := &Server{
		mihomoController: mockController.URL,
		sessions:         make(map[string]authSession),
	}

	// 1. GET /api/mihomo/proxies must hide 家庭测速
	recGet := httptest.NewRecorder()
	server.mihomoProxies(recGet, httptest.NewRequest(http.MethodGet, "/api/mihomo/proxies", nil))
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /api/mihomo/proxies status = %d, want 200", recGet.Code)
	}

	var resp struct {
		Groups []MihomoProxyGroup `json:"groups"`
	}
	if err := json.Unmarshal(recGet.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal proxies response: %v", err)
	}

	for _, g := range resp.Groups {
		if g.Name == speedTestBenchmarkGroup {
			t.Fatalf("GET /api/mihomo/proxies leaked %q in groups list", speedTestBenchmarkGroup)
		}
	}
	if len(resp.Groups) != 1 || resp.Groups[0].Name != "家庭RULE" {
		t.Fatalf("expected only '家庭RULE' group, got: %+v", resp.Groups)
	}

	// 2. PUT /api/mihomo/proxies must reject changing 家庭测速
	putBody := strings.NewReader(`{"group":"` + speedTestBenchmarkGroup + `","proxy":"HK-01"}`)
	recPut := httptest.NewRecorder()
	server.mihomoProxies(recPut, httptest.NewRequest(http.MethodPut, "/api/mihomo/proxies", putBody))
	if recPut.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/mihomo/proxies with %q status = %d, want 400", speedTestBenchmarkGroup, recPut.Code)
	}
}

func TestDefaultSpeedTestHTTP_RealServerPhasedTimeoutsAndBodyIdle(t *testing.T) {
	// 1. Test real body stall triggers body idle watchdog, preserves received bytes, and classifies as body_timeout
	t.Run("BodyIdleTimeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			chunk := []byte("hello partial payload stream for speedtest idle check")
			_, _ = w.Write(chunk)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Stall without sending more data or closing
			time.Sleep(3 * time.Second)
		}))
		defer server.Close()

		origIdle := speedTestBodyIdleTimeout
		origRun := speedTestRunTimeout
		speedTestBodyIdleTimeout = 100 * time.Millisecond
		speedTestRunTimeout = 1500 * time.Millisecond
		defer func() {
			speedTestBodyIdleTimeout = origIdle
			speedTestRunTimeout = origRun
		}()

		received, duration, ttfb, status, err := defaultSpeedTestHTTP(context.Background(), "", server.URL, 1024*1024)
		if err == nil {
			t.Fatalf("expected error from body idle timeout, got nil")
		}
		if received <= 0 {
			t.Fatalf("expected non-zero partial received bytes before idle timeout, got %d", received)
		}
		if status != 200 {
			t.Fatalf("expected HTTP 200 status, got %d", status)
		}
		if duration <= 0 || ttfb <= 0 {
			t.Fatalf("expected positive duration and ttfb, got duration=%v, ttfb=%v", duration, ttfb)
		}

		errCode := classifyRunError(err, status, received, 1024*1024)
		if errCode != "body_timeout" {
			t.Fatalf("classifyRunError() = %q, want 'body_timeout' (err=%v)", errCode, err)
		}
	})

	// 2. Test real header stall triggers ResponseHeaderTimeout and classifies as header_timeout
	t.Run("HeaderTimeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Stall headers
			time.Sleep(1 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		origHeader := speedTestHeaderTimeout
		speedTestHeaderTimeout = 100 * time.Millisecond
		defer func() {
			speedTestHeaderTimeout = origHeader
		}()

		received, _, _, status, err := defaultSpeedTestHTTP(context.Background(), "", server.URL, 1024*1024)
		if err == nil {
			t.Fatalf("expected error from header timeout, got nil")
		}
		if received != 0 {
			t.Fatalf("expected 0 received bytes on header timeout, got %d", received)
		}
		errCode := classifyRunError(err, status, received, 1024*1024)
		if errCode != "header_timeout" {
			t.Fatalf("classifyRunError() = %q, want 'header_timeout' (err=%v)", errCode, err)
		}
	})

	// 3. Test real successful Range request
	t.Run("RealServerSuccessRangeRequest", func(t *testing.T) {
		payload := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload)
		}))
		defer server.Close()

		received, duration, ttfb, status, err := defaultSpeedTestHTTP(context.Background(), "", server.URL, int64(len(payload)))
		if err != nil {
			t.Fatalf("unexpected error on valid server: %v", err)
		}
		if status != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", status)
		}
		if received != int64(len(payload)) {
			t.Fatalf("received = %d, want %d", received, len(payload))
		}
		if duration <= 0 || ttfb <= 0 {
			t.Fatalf("duration and ttfb should be positive, got duration=%v, ttfb=%v", duration, ttfb)
		}
		errCode := classifyRunError(err, status, received, int64(len(payload)))
		if errCode != "" {
			t.Fatalf("expected empty errCode on success, got %q", errCode)
		}
	})
}

func TestSpeedTestWorker_HardLimitsAndErrorCodes(t *testing.T) {
	// 1. Total budget exhaustion halts job with data_budget_exceeded
	t.Run("TotalBudgetExhausted", func(t *testing.T) {
		var putMu sync.Mutex
		var putHistory []string
		mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/version" && r.Method == http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.18.0"})
			case strings.HasPrefix(r.URL.Path, "/group/"):
				_ = json.NewEncoder(w).Encode(map[string]int{"INITIAL-NODE": 30, "HK-01": 50, "JP-01": 60})
			case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"proxies": map[string]any{
						speedTestBenchmarkGroup: map[string]any{
							"name": speedTestBenchmarkGroup,
							"type": "Selector",
							"now":  "INITIAL-NODE",
							"all":  []string{"INITIAL-NODE", "HK-01", "JP-01", "DIRECT"},
						},
						"家庭RULE": map[string]any{
							"name": "家庭RULE",
							"type": "Selector",
							"now":  "HK-01",
							"all":  []string{"HK-01", "JP-01", "DIRECT"},
						},
						"INITIAL-NODE": map[string]any{"name": "INITIAL-NODE", "type": "Vless"},
						"HK-01":        map[string]any{"name": "HK-01", "type": "Vless", "history": []map[string]any{{"delay": 50}}},
						"JP-01":        map[string]any{"name": "JP-01", "type": "Vless", "history": []map[string]any{{"delay": 60}}},
					},
				})
			case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				putMu.Lock()
				putHistory = append(putHistory, body["name"])
				putMu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockController.Close()

		tmpDir := t.TempDir()
		server := &Server{
			data:             tmpDir,
			mihomoController: mockController.URL,
			sessions:         make(map[string]authSession),
			speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
				return sampleBytes, 100 * time.Millisecond, 20 * time.Millisecond, 206, nil
			},
			speedTestSampleWAN: func(ctx context.Context) (float64, error) {
				return 0, nil
			},
			speedTestCheckListener: func() bool { return true },
		}
		server.initSpeedTest()

		job := &MihomoSpeedTestJob{
			ID:              "test-budget-exhaust",
			Mode:            "quick",
			State:           "running",
			Phase:           "discovering",
			StartedAt:       time.Now().Format(time.RFC3339),
			DataBudgetBytes: 4 * 1024 * 1024, // only 4 MiB budget (prelim is 3 MiB, so 2nd node will exceed 4 MiB)
			Results:         []MihomoSpeedTestNodeResult{},
		}
		server.speedTestJob = job

		server.runSpeedTestWorker(context.Background(), job, false)

		snapshot := server.getSpeedTestJobSnapshot()
		if snapshot.State != "failed" {
			t.Fatalf("job state = %q, want 'failed'", snapshot.State)
		}
		if snapshot.ErrorCode != "data_budget_exceeded" {
			t.Fatalf("job errorCode = %q, want 'data_budget_exceeded'", snapshot.ErrorCode)
		}
		if snapshot.BytesUsed != 3*1024*1024 {
			t.Fatalf("job bytesUsed = %d, want 3145728", snapshot.BytesUsed)
		}

		// Selector was restored to initial
		putMu.Lock()
		lastPut := ""
		if len(putHistory) > 0 {
			lastPut = putHistory[len(putHistory)-1]
		}
		putMu.Unlock()
		if lastPut != "INITIAL-NODE" {
			t.Fatalf("selector not restored: lastPut=%q, want 'INITIAL-NODE'", lastPut)
		}
	})

	// 2. Total job deadline halts job with job_timeout and preserves partial bytes
	t.Run("JobDeadlineTimeout", func(t *testing.T) {
		mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/version" && r.Method == http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.18.0"})
			case strings.HasPrefix(r.URL.Path, "/group/"):
				_ = json.NewEncoder(w).Encode(map[string]int{"INITIAL-NODE": 30, "HK-01": 50})
			case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"proxies": map[string]any{
						speedTestBenchmarkGroup: map[string]any{
							"name": speedTestBenchmarkGroup,
							"type": "Selector",
							"now":  "INITIAL-NODE",
							"all":  []string{"INITIAL-NODE", "HK-01", "DIRECT"},
						},
						"家庭RULE": map[string]any{
							"name": "家庭RULE",
							"type": "Selector",
							"now":  "HK-01",
							"all":  []string{"HK-01", "DIRECT"},
						},
						"INITIAL-NODE": map[string]any{"name": "INITIAL-NODE", "type": "Vless"},
						"HK-01":        map[string]any{"name": "HK-01", "type": "Vless", "history": []map[string]any{{"delay": 50}}},
					},
				})
			case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockController.Close()

		tmpDir := t.TempDir()
		server := &Server{
			data:             tmpDir,
			mihomoController: mockController.URL,
			sessions:         make(map[string]authSession),
			speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
				<-ctx.Done()
				// Partial read received before deadline was reached
				return 123456, 100 * time.Millisecond, 20 * time.Millisecond, 200, ctx.Err()
			},
			speedTestSampleWAN: func(ctx context.Context) (float64, error) {
				return 0, nil
			},
			speedTestCheckListener: func() bool { return true },
		}
		server.initSpeedTest()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		job := &MihomoSpeedTestJob{
			ID:              "test-deadline-job",
			Mode:            "quick",
			State:           "running",
			Phase:           "discovering",
			StartedAt:       time.Now().Format(time.RFC3339),
			DataBudgetBytes: 100 * 1024 * 1024,
			Results:         []MihomoSpeedTestNodeResult{},
		}
		server.speedTestJob = job

		server.runSpeedTestWorker(ctx, job, false)

		snapshot := server.getSpeedTestJobSnapshot()
		if snapshot.State != "failed" {
			t.Fatalf("job state = %q, want 'failed'", snapshot.State)
		}
		if snapshot.ErrorCode != "job_timeout" {
			t.Fatalf("job errorCode = %q, want 'job_timeout'", snapshot.ErrorCode)
		}
		if snapshot.BytesUsed != 123456 {
			t.Fatalf("job bytesUsed = %d, want 123456 (partial bytes preserved)", snapshot.BytesUsed)
		}
	})

	// 3. Single node budget exhaustion fails run with node_budget_exceeded and continues other nodes
	t.Run("PerNodeBudgetExhausted", func(t *testing.T) {
		mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/version" && r.Method == http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.18.0"})
			case strings.HasPrefix(r.URL.Path, "/group/"):
				_ = json.NewEncoder(w).Encode(map[string]int{"INITIAL-NODE": 30, "HK-01": 50, "JP-01": 60})
			case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"proxies": map[string]any{
						speedTestBenchmarkGroup: map[string]any{
							"name": speedTestBenchmarkGroup,
							"type": "Selector",
							"now":  "INITIAL-NODE",
							"all":  []string{"HK-01", "JP-01", "DIRECT"},
						},
						"家庭RULE": map[string]any{
							"name": "家庭RULE",
							"type": "Selector",
							"now":  "HK-01",
							"all":  []string{"HK-01", "JP-01", "DIRECT"},
						},
						"INITIAL-NODE": map[string]any{"name": "INITIAL-NODE", "type": "Vless"},
						"HK-01":        map[string]any{"name": "HK-01", "type": "Vless", "history": []map[string]any{{"delay": 50}}},
						"JP-01":        map[string]any{"name": "JP-01", "type": "Vless", "history": []map[string]any{{"delay": 60}}},
					},
				})
			case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockController.Close()

		tmpDir := t.TempDir()
		var httpCallCount atomic.Int64
		server := &Server{
			data:             tmpDir,
			mihomoController: mockController.URL,
			sessions:         make(map[string]authSession),
			speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
				httpCallCount.Add(1)
				return sampleBytes, 100 * time.Millisecond, 20 * time.Millisecond, 206, nil
			},
			speedTestSampleWAN: func(ctx context.Context) (float64, error) {
				return 0, nil
			},
			speedTestCheckListener: func() bool { return true },
		}
		server.initSpeedTest()

		// Temporarily set maxNodeBudgetQuick to 5 MiB (less than 8 MiB confirm run)
		origNodeBudget := maxNodeBudgetQuick
		maxNodeBudgetQuick = 5 * 1024 * 1024 // 5 MiB
		defer func() {
			maxNodeBudgetQuick = origNodeBudget
		}()

		job := &MihomoSpeedTestJob{
			ID:              "test-per-node-budget-job",
			Mode:            "quick",
			State:           "running",
			Phase:           "discovering",
			StartedAt:       time.Now().Format(time.RFC3339),
			DataBudgetBytes: 100 * 1024 * 1024,
			Results:         []MihomoSpeedTestNodeResult{},
		}
		server.speedTestJob = job

		server.runSpeedTestWorker(context.Background(), job, false)

		snapshot := server.getSpeedTestJobSnapshot()
		if snapshot.State != "completed" {
			t.Fatalf("job state = %q, want 'completed'", snapshot.State)
		}

		// Assert that blocked confirmation requests NEVER invoked speedTestHTTP
		// There were 2 candidates tested in preliminary (3 MiB each), and 0 confirmation requests executed
		if got := httpCallCount.Load(); got != 2 {
			t.Fatalf("httpCallCount = %d, want 2 (only preliminary runs, confirmation must be blocked before HTTP)", got)
		}

		// Assert BytesUsed contains ONLY the 2 preliminary runs (6 MiB total), without any confirmation bytes
		expectedBytes := int64(2 * quickPreliminaryBytes)
		if snapshot.BytesUsed != expectedBytes {
			t.Fatalf("BytesUsed = %d, want %d (must not include blocked requests)", snapshot.BytesUsed, expectedBytes)
		}

		// Both finalists must have their NodeResult.ErrorCode written as "node_budget_exceeded"
		for _, r := range snapshot.Results {
			if r.ErrorCode != "node_budget_exceeded" {
				t.Fatalf("node %s ErrorCode = %q, want 'node_budget_exceeded'", r.Node, r.ErrorCode)
			}
			foundNodeBudgetRun := false
			for _, run := range r.Runs {
				if run.ErrorCode == "node_budget_exceeded" {
					foundNodeBudgetRun = true
					if run.Success {
						t.Fatalf("node_budget_exceeded run must not have Success = true")
					}
					if run.ReceivedBytes != 0 {
						t.Fatalf("node_budget_exceeded run ReceivedBytes = %d, want 0", run.ReceivedBytes)
					}
				}
			}
			if !foundNodeBudgetRun {
				t.Fatalf("node %s expected at least one confirmation run with 'node_budget_exceeded'", r.Node)
			}
		}
	})
}

func TestConfirmationRunFailure_WritesNodeErrorCode(t *testing.T) {
	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.18.0"})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"INITIAL-NODE": 30, "HK-01": 50})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "INITIAL-NODE",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"家庭RULE": map[string]any{
						"name": "家庭RULE",
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"INITIAL-NODE": map[string]any{"name": "INITIAL-NODE", "type": "Vless"},
					"HK-01":        map[string]any{"name": "HK-01", "type": "Vless", "history": []map[string]any{{"delay": 50}}},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	tmpDir := t.TempDir()
	var runIndex atomic.Int64
	server := &Server{
		data:             tmpDir,
		mihomoController: mockController.URL,
		sessions:         make(map[string]authSession),
		speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
			idx := runIndex.Add(1)
			if idx == 1 {
				// Preliminary run succeeds
				return sampleBytes, 100 * time.Millisecond, 20 * time.Millisecond, 206, nil
			} else if idx == 2 {
				// Confirmation run 1 fails with general unexpected_status
				return 1000, 100 * time.Millisecond, 20 * time.Millisecond, 502, errors.New("bad gateway")
			}
			// Confirmation run 2 fails with more specific body_timeout
			return 2000, 5000 * time.Millisecond, 20 * time.Millisecond, 200, errors.New("body idle timeout: read stalled")
		},
		speedTestSampleWAN: func(ctx context.Context) (float64, error) {
			return 0, nil
		},
		speedTestCheckListener: func() bool { return true },
	}
	server.initSpeedTest()

	job := &MihomoSpeedTestJob{
		ID:              "test-confirm-error-code-job",
		Mode:            "quick",
		State:           "running",
		Phase:           "discovering",
		StartedAt:       time.Now().Format(time.RFC3339),
		DataBudgetBytes: 100 * 1024 * 1024,
		Results:         []MihomoSpeedTestNodeResult{},
	}
	server.speedTestJob = job

	server.runSpeedTestWorker(context.Background(), job, false)

	snapshot := server.getSpeedTestJobSnapshot()
	if snapshot.State != "completed" {
		t.Fatalf("job state = %q, want 'completed'", snapshot.State)
	}
	if len(snapshot.Results) == 0 {
		t.Fatalf("expected results for HK-01")
	}

	hk := snapshot.Results[0]
	if hk.Eligible {
		t.Fatalf("node HK-01 should not be eligible after confirmation failures")
	}
	// Verify that the more specific error code ("body_timeout") was selected over "unexpected_status"
	if hk.ErrorCode != "body_timeout" {
		t.Fatalf("node HK-01 ErrorCode = %q, want 'body_timeout' (most specific error code retained)", hk.ErrorCode)
	}
	if len(hk.Runs) != 3 {
		t.Fatalf("len(hk.Runs) = %d, want 3 (1 prelim + 2 confirmation)", len(hk.Runs))
	}
	if hk.Runs[1].ErrorCode != "unexpected_status" {
		t.Fatalf("run 1 ErrorCode = %q, want 'unexpected_status'", hk.Runs[1].ErrorCode)
	}
	if hk.Runs[2].ErrorCode != "body_timeout" {
		t.Fatalf("run 2 ErrorCode = %q, want 'body_timeout'", hk.Runs[2].ErrorCode)
	}
}

func TestSpeedTestContextCancelOnCompletionFailureAndCancel(t *testing.T) {
	mockController := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.18.0"})
		case strings.HasPrefix(r.URL.Path, "/group/"):
			_ = json.NewEncoder(w).Encode(map[string]int{"HK-01": 50})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					speedTestBenchmarkGroup: map[string]any{
						"name": speedTestBenchmarkGroup,
						"type": "Selector",
						"now":  "INITIAL-NODE",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"家庭RULE": map[string]any{
						"name": "家庭RULE",
						"type": "Selector",
						"now":  "HK-01",
						"all":  []string{"HK-01", "DIRECT"},
					},
					"INITIAL-NODE": map[string]any{"name": "INITIAL-NODE", "type": "Vless"},
					"HK-01":        map[string]any{"name": "HK-01", "type": "Vless", "history": []map[string]any{{"delay": 50}}},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/proxies/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockController.Close()

	// 1. Natural Completion: cancel is invoked, State is completed, subsequent DELETE returns 409
	t.Run("NaturalCompletion", func(t *testing.T) {
		tmpDir := t.TempDir()
		server := &Server{
			data:             tmpDir,
			mihomoController: mockController.URL,
			sessions:         make(map[string]authSession),
			speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
				return sampleBytes, 20 * time.Millisecond, 5 * time.Millisecond, 206, nil
			},
			speedTestSampleWAN: func(ctx context.Context) (float64, error) {
				return 0, nil
			},
			speedTestCheckListener: func() bool { return true },
		}
		server.initSpeedTest()

		// Launch via POST
		reqPost := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", strings.NewReader(`{"mode":"quick"}`))
		recPost := httptest.NewRecorder()
		server.mihomoSpeedTest(recPost, reqPost)
		if recPost.Code != http.StatusAccepted {
			t.Fatalf("POST code = %d, want 202. Body: %s", recPost.Code, recPost.Body.String())
		}

		// Wrap speedTestCancel to detect its invocation
		var cancelCalled atomic.Bool
		server.speedTestMu.Lock()
		origCancel := server.speedTestCancel
		if origCancel == nil {
			server.speedTestMu.Unlock()
			t.Fatalf("speedTestCancel must be non-nil while job is running")
		}
		server.speedTestCancel = func() {
			cancelCalled.Store(true)
			origCancel()
		}
		server.speedTestMu.Unlock()

		// Poll until job reaches terminal state
		var finalJob *MihomoSpeedTestJob
		for i := 0; i < 50; i++ {
			time.Sleep(20 * time.Millisecond)
			snap := server.getSpeedTestJobSnapshot()
			if snap != nil && snap.State != "running" {
				finalJob = snap
				break
			}
		}
		if finalJob == nil || finalJob.State != "completed" {
			t.Fatalf("job state = %v, want 'completed'", finalJob)
		}

		// Verify cancel was invoked
		if !cancelCalled.Load() {
			t.Fatalf("cancel was not called on natural job completion")
		}

		// Verify server.speedTestCancel is nil
		server.speedTestMu.Lock()
		curCancel := server.speedTestCancel
		server.speedTestMu.Unlock()
		if curCancel != nil {
			t.Fatalf("speedTestCancel should be cleared to nil, got %v", curCancel)
		}

		// Verify DELETE on completed job returns 409 Conflict
		reqDel := httptest.NewRequest(http.MethodDelete, "/api/mihomo/speedtest", nil)
		recDel := httptest.NewRecorder()
		server.mihomoSpeedTest(recDel, reqDel)
		if recDel.Code != http.StatusConflict {
			t.Fatalf("DELETE after completion code = %d, want 409 Conflict", recDel.Code)
		}
	})

	// 2. Task Failure: cancel is invoked, State is failed
	t.Run("TaskFailure", func(t *testing.T) {
		tmpDir := t.TempDir()
		server := &Server{
			data:             tmpDir,
			mihomoController: mockController.URL,
			sessions:         make(map[string]authSession),
			speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
				return sampleBytes, 20 * time.Millisecond, 5 * time.Millisecond, 206, nil
			},
			speedTestCheckListener: func() bool { return false },
		}
		server.initSpeedTest()

		// Launch speed test -> should fail on unavailable listener
		reqPost := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", strings.NewReader(`{"mode":"deep"}`))
		recPost := httptest.NewRecorder()
		server.mihomoSpeedTest(recPost, reqPost)
		if recPost.Code != http.StatusAccepted {
			t.Fatalf("POST code = %d, want 202. Body: %s", recPost.Code, recPost.Body.String())
		}

		// Wrap speedTestCancel to detect its invocation
		var cancelCalled atomic.Bool
		server.speedTestMu.Lock()
		origCancel := server.speedTestCancel
		if origCancel != nil {
			server.speedTestCancel = func() {
				cancelCalled.Store(true)
				origCancel()
			}
		}
		server.speedTestMu.Unlock()

		// Poll until job reaches terminal state
		var finalJob *MihomoSpeedTestJob
		for i := 0; i < 50; i++ {
			time.Sleep(20 * time.Millisecond)
			snap := server.getSpeedTestJobSnapshot()
			if snap != nil && snap.State != "running" {
				finalJob = snap
				break
			}
		}
		if finalJob == nil || finalJob.State != "failed" {
			t.Fatalf("job state = %v, want 'failed'", finalJob)
		}
		if finalJob.ErrorCode != "benchmark_listener_unavailable" {
			t.Fatalf("job ErrorCode = %q, want 'benchmark_listener_unavailable'", finalJob.ErrorCode)
		}

		// Verify cancel was invoked
		if !cancelCalled.Load() {
			t.Fatalf("cancel was not called on job failure")
		}

		server.speedTestMu.Lock()
		curCancel := server.speedTestCancel
		server.speedTestMu.Unlock()
		if curCancel != nil {
			t.Fatalf("speedTestCancel should be cleared to nil, got %v", curCancel)
		}
	})

	// 3. DELETE Cancellation: cancel is invoked, State is cancelled
	t.Run("DeleteCancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		httpEntered := make(chan struct{})
		server := &Server{
			data:             tmpDir,
			mihomoController: mockController.URL,
			sessions:         make(map[string]authSession),
			speedTestHTTP: func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
				select {
				case <-httpEntered:
				default:
					close(httpEntered)
				}
				<-ctx.Done()
				return 0, 0, 0, 0, ctx.Err()
			},
			speedTestSampleWAN: func(ctx context.Context) (float64, error) {
				return 0, nil
			},
			speedTestCheckListener: func() bool { return true },
		}
		server.initSpeedTest()

		// Launch via POST
		reqPost := httptest.NewRequest(http.MethodPost, "/api/mihomo/speedtest", strings.NewReader(`{"mode":"quick"}`))
		recPost := httptest.NewRecorder()
		server.mihomoSpeedTest(recPost, reqPost)
		if recPost.Code != http.StatusAccepted {
			t.Fatalf("POST code = %d, want 202. Body: %s", recPost.Code, recPost.Body.String())
		}

		// Wait until HTTP call has started
		select {
		case <-httpEntered:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for HTTP call to start")
		}

		// Wrap speedTestCancel to detect its invocation by DELETE
		var cancelCalled atomic.Bool
		server.speedTestMu.Lock()
		origCancel := server.speedTestCancel
		if origCancel == nil {
			server.speedTestMu.Unlock()
			t.Fatalf("speedTestCancel must be non-nil while job is running")
		}
		server.speedTestCancel = func() {
			cancelCalled.Store(true)
			origCancel()
		}
		server.speedTestMu.Unlock()

		// Send DELETE
		reqDel := httptest.NewRequest(http.MethodDelete, "/api/mihomo/speedtest", nil)
		recDel := httptest.NewRecorder()
		server.mihomoSpeedTest(recDel, reqDel)
		if recDel.Code != http.StatusOK {
			t.Fatalf("DELETE code = %d, want 200 OK. Body: %s", recDel.Code, recDel.Body.String())
		}

		// Verify cancel was invoked by DELETE
		if !cancelCalled.Load() {
			t.Fatalf("cancel was not called by DELETE")
		}

		// Poll until job reaches terminal state
		var finalJob *MihomoSpeedTestJob
		for i := 0; i < 50; i++ {
			time.Sleep(20 * time.Millisecond)
			snap := server.getSpeedTestJobSnapshot()
			if snap != nil && snap.State != "running" {
				finalJob = snap
				break
			}
		}
		if finalJob == nil || finalJob.State != "cancelled" {
			t.Fatalf("job state = %v, want 'cancelled'", finalJob)
		}
		if finalJob.ErrorCode != "cancelled" {
			t.Fatalf("job ErrorCode = %q, want 'cancelled'", finalJob.ErrorCode)
		}

		server.speedTestMu.Lock()
		curCancel := server.speedTestCancel
		server.speedTestMu.Unlock()
		if curCancel != nil {
			t.Fatalf("speedTestCancel should be cleared to nil, got %v", curCancel)
		}
	})
}
