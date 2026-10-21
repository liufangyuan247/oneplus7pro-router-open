package main

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	speedTestTargetURL       = "https://dl.google.com/android/repository/platform-tools-latest-windows.zip"
	speedTestProxyURL        = "http://127.0.0.1:17894"
	speedTestBenchmarkGroup  = "家庭测速"
	speedTestProductionGroup = "家庭RULE"

	// Sample sizes
	quickPreliminaryBytes int64 = 3 * 1024 * 1024 // 3 MiB
	quickConfirmBytes     int64 = 8 * 1024 * 1024 // 8 MiB
	deepPreliminaryBytes  int64 = 4 * 1024 * 1024 // 4 MiB
	deepConfirmBytes      int64 = 8 * 1024 * 1024 // 8 MiB

	// Budgets
	quickBudgetBytes int64 = 100 * 1024 * 1024 // 100 MiB (104857600 bytes)
	deepBudgetBytes  int64 = 300 * 1024 * 1024 // 300 MiB (314572800 bytes)

	// Thresholds & Timeouts
	wanBusyThresholdBps   float64       = 5 * 1000 * 1000 // 5 Mbit/s
	speedTestQuickTimeout time.Duration = 10 * time.Minute
	speedTestDeepTimeout  time.Duration = 30 * time.Minute
	maxRetainedSpeedTests               = 10
	speedTestStoreVersion               = 1
)

var (
	maxNodeBudgetQuick int64 = 25 * 1024 * 1024 // 25 MiB
	maxNodeBudgetDeep  int64 = 35 * 1024 * 1024 // 35 MiB

	speedTestDialTimeout     = 5 * time.Second
	speedTestTLSTimeout      = 5 * time.Second
	speedTestHeaderTimeout   = 8 * time.Second
	speedTestBodyIdleTimeout = 5 * time.Second
	speedTestRunTimeout      = 30 * time.Second

	reHK   = regexp.MustCompile(`(?i)🇭🇰|香港|HK|Hong Kong|HongKong`)
	reJP   = regexp.MustCompile(`(?i)🇯🇵|日本|Japan|(^|[^A-Za-z])JP([^A-Za-z]|$)`)
	reTWSG = regexp.MustCompile(`(?i)🇹🇼|台湾|台灣|TW|Taiwan|🇸🇬|新加坡|狮城|SG|Singapore`)
)

type MihomoSpeedTestRun struct {
	Stage          string  `json:"stage"` // "preliminary" | "confirmation"
	RequestedBytes int64   `json:"requestedBytes"`
	ReceivedBytes  int64   `json:"receivedBytes"`
	DurationMs     int64   `json:"durationMs"`
	TTFBMs         int64   `json:"ttfbMs"`
	Mbps           float64 `json:"mbps"`
	HTTPStatus     int     `json:"httpStatus"`
	Success        bool    `json:"success"`
	ErrorCode      string  `json:"errorCode,omitempty"`
}

type MihomoSpeedTestNodeResult struct {
	Node          string               `json:"node"`
	Protocol      string               `json:"protocol"`
	DelayMs       int                  `json:"delayMs"`
	Runs          []MihomoSpeedTestRun `json:"runs"`
	MedianMbps    float64              `json:"medianMbps"`
	MinimumMbps   float64              `json:"minimumMbps"`
	EffectiveMbps float64              `json:"effectiveMbps"`
	Eligible      bool                 `json:"eligible"`
	VideoBadge    string               `json:"videoBadge"`
	ErrorCode     string               `json:"errorCode,omitempty"`
}

type MihomoSpeedTestJob struct {
	ID                    string                      `json:"id"`
	Mode                  string                      `json:"mode"`  // "quick" | "deep"
	State                 string                      `json:"state"` // "idle" | "running" | "completed" | "cancelled" | "failed" | "interrupted"
	Phase                 string                      `json:"phase"` // "idle" | "discovering" | "sampling_wan" | "preliminary" | "confirming" | "completed" | "cancelled" | "failed"
	StartedAt             string                      `json:"startedAt"`
	FinishedAt            string                      `json:"finishedAt"`
	CurrentNode           string                      `json:"currentNode"`
	CompletedRuns         int                         `json:"completedRuns"`
	TotalRuns             int                         `json:"totalRuns"`
	BytesUsed             int64                       `json:"bytesUsed"`
	DataBudgetBytes       int64                       `json:"dataBudgetBytes"`
	Contaminated          bool                        `json:"contaminated"`
	InitialProductionNode string                      `json:"initialProductionNode"`
	RecommendedNode       string                      `json:"recommendedNode"`
	CandidateFingerprint  string                      `json:"candidateFingerprint"`
	ErrorMessage          string                      `json:"errorMessage,omitempty"`
	ErrorCode             string                      `json:"errorCode,omitempty"`
	Results               []MihomoSpeedTestNodeResult `json:"results"`
}

type speedTestPersistentStore struct {
	Version int                  `json:"version"`
	Jobs    []MihomoSpeedTestJob `json:"jobs"`
}

type speedTestCandidate struct {
	name     string
	protocol string
	delay    int
}

// defaultSpeedTestHTTP executes a bounded range read through the loopback proxy listener.
func defaultSpeedTestHTTP(ctx context.Context, proxyURLStr, targetURLStr string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error) {
	var proxyURL *url.URL
	if proxyURLStr != "" {
		var err error
		proxyURL, err = url.Parse(proxyURLStr)
		if err != nil {
			return 0, 0, 0, 0, err
		}
	}

	transport := &http.Transport{
		DisableCompression:    true,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		ResponseHeaderTimeout: speedTestHeaderTimeout,
		TLSHandshakeTimeout:   speedTestTLSTimeout,
		DialContext: (&net.Dialer{
			Timeout:   speedTestDialTimeout,
			KeepAlive: -1,
		}).DialContext,
	}
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	defer transport.CloseIdleConnections()

	runCtx, runCancel := context.WithTimeout(ctx, speedTestRunTimeout)
	defer runCancel()

	req, err := http.NewRequestWithContext(runCtx, "GET", targetURLStr, nil)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", sampleBytes-1))
	req.Header.Set("User-Agent", "OnePlusRouterAdmin/1.0")

	var start time.Time
	var ttfb time.Duration
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			if !start.IsZero() {
				ttfb = time.Since(start)
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	client := &http.Client{
		Transport: transport,
	}

	start = time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer resp.Body.Close()

	if ttfb <= 0 {
		ttfb = time.Since(start)
		if ttfb <= 0 {
			ttfb = 1 * time.Millisecond
		}
	}

	bodyReadStart := time.Now()
	limited := io.LimitReader(resp.Body, sampleBytes)
	buf := make([]byte, 32*1024)
	var totalRead int64

	var bodyIdleExpired atomic.Bool
	idleTimer := time.AfterFunc(speedTestBodyIdleTimeout, func() {
		bodyIdleExpired.Store(true)
		_ = resp.Body.Close()
	})
	defer idleTimer.Stop()

	for {
		idleTimer.Reset(speedTestBodyIdleTimeout)
		n, rErr := limited.Read(buf)
		if n > 0 {
			totalRead += int64(n)
		}
		if rErr != nil {
			idleTimer.Stop()
			if errors.Is(rErr, io.EOF) {
				break
			}
			if bodyIdleExpired.Load() {
				return totalRead, time.Since(bodyReadStart), ttfb, resp.StatusCode, errors.New("body idle timeout: no data received within timeout")
			}
			return totalRead, time.Since(bodyReadStart), ttfb, resp.StatusCode, rErr
		}
	}
	bodyReadDuration := time.Since(bodyReadStart)
	if bodyReadDuration <= 0 {
		bodyReadDuration = time.Millisecond
	}

	return totalRead, bodyReadDuration, ttfb, resp.StatusCode, nil
}

// defaultSampleWAN samples WAN bytes across 3 seconds from /proc/net/dev if available.
func (s *Server) defaultSampleWAN(ctx context.Context) (float64, error) {
	sampleDuration := 3 * time.Second
	draft := s.loadNetworkDraft()
	wanInterface, _ := s.activeNetworkInterfaces(draft)

	getWANBytes := func() (uint64, bool) {
		devs := readNetDev()
		if wanInterface != "" {
			if c, ok := devs[wanInterface]; ok {
				return c.rx + c.tx, true
			}
		}
		if c, ok := devs["ppp0"]; ok {
			return c.rx + c.tx, true
		}
		// If neither found, check rmnet_data0 or any non-lo interface
		for iface, c := range devs {
			if strings.HasPrefix(iface, "rmnet") || strings.HasPrefix(iface, "eth") {
				return c.rx + c.tx, true
			}
		}
		return 0, false
	}

	bytes1, ok1 := getWANBytes()
	if !ok1 {
		return 0, nil
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(sampleDuration):
	}

	bytes2, ok2 := getWANBytes()
	if !ok2 || bytes2 < bytes1 {
		return 0, nil
	}

	bps := float64(bytes2-bytes1) * 8.0 / sampleDuration.Seconds()
	return bps, nil
}

func (s *Server) speedTestStorePath() string {
	return filepath.Join(s.data, "mihomo", "speedtest-results.json")
}

func (s *Server) initSpeedTest() {
	s.speedTestMu.Lock()
	defer s.speedTestMu.Unlock()

	if s.speedTestHTTP == nil {
		s.speedTestHTTP = defaultSpeedTestHTTP
	}
	if s.speedTestSampleWAN == nil {
		s.speedTestSampleWAN = s.defaultSampleWAN
	}

	storePath := s.speedTestStorePath()
	_ = os.MkdirAll(filepath.Dir(storePath), 0755)
	data, err := os.ReadFile(storePath)
	if err != nil {
		return
	}

	var store speedTestPersistentStore
	if err := json.Unmarshal(data, &store); err != nil {
		log.Printf("malformed speedtest store at %s: %v", storePath, err)
		return
	}

	if len(store.Jobs) > 0 {
		latest := store.Jobs[0]
		if latest.State == "running" {
			latest.State = "interrupted"
			latest.Phase = "interrupted"
			if latest.FinishedAt == "" {
				latest.FinishedAt = time.Now().Format(time.RFC3339)
			}
			store.Jobs[0] = latest
			s.speedTestJob = &latest
			_ = s.persistSpeedTestStoreLocked(store)
		} else {
			s.speedTestJob = &latest
		}
	}
}

func (s *Server) persistSpeedTestStoreLocked(store speedTestPersistentStore) error {
	dir := filepath.Dir(s.speedTestStorePath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if len(store.Jobs) > maxRetainedSpeedTests {
		store.Jobs = store.Jobs[:maxRetainedSpeedTests]
	}
	store.Version = speedTestStoreVersion

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := filepath.Join(dir, fmt.Sprintf(".speedtest-results-%d.tmp", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}
	_ = os.Chmod(tmpFile, 0600)
	if err := os.Rename(tmpFile, s.speedTestStorePath()); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}
	_ = os.Chmod(s.speedTestStorePath(), 0600)
	return nil
}

func deepCopyJob(src *MihomoSpeedTestJob) *MihomoSpeedTestJob {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Results != nil {
		dst.Results = make([]MihomoSpeedTestNodeResult, len(src.Results))
		for i, r := range src.Results {
			dst.Results[i] = r
			if r.Runs != nil {
				dst.Results[i].Runs = make([]MihomoSpeedTestRun, len(r.Runs))
				copy(dst.Results[i].Runs, r.Runs)
			}
		}
	}
	return &dst
}

func (s *Server) publishSpeedTestSnapshot(job *MihomoSpeedTestJob) {
	if job == nil {
		return
	}
	s.speedTestMu.Lock()
	defer s.speedTestMu.Unlock()
	if s.speedTestJob != nil && s.speedTestJob.ID != job.ID {
		return // Do not overwrite a newer active job
	}
	s.speedTestJob = deepCopyJob(job)
}

func (s *Server) finalizeSpeedTestJob(workerJobID string, job *MihomoSpeedTestJob) {
	if job == nil {
		return
	}
	var cancelToCall context.CancelFunc
	s.speedTestMu.Lock()
	if s.speedTestJob != nil && s.speedTestJob.ID == workerJobID {
		s.speedTestJob = deepCopyJob(job)
		cancelToCall = s.speedTestCancel
		s.speedTestCancel = nil
	}
	s.speedTestMu.Unlock()

	if cancelToCall != nil {
		cancelToCall()
	}

	s.persistJob(job)
}

func (s *Server) getSpeedTestJobSnapshot() *MihomoSpeedTestJob {
	s.speedTestMu.Lock()
	defer s.speedTestMu.Unlock()
	return deepCopyJob(s.speedTestJob)
}

func (s *Server) persistJob(job *MihomoSpeedTestJob) {
	if job == nil {
		return
	}
	s.speedTestMu.Lock()
	defer s.speedTestMu.Unlock()

	var store speedTestPersistentStore
	data, err := os.ReadFile(s.speedTestStorePath())
	if err == nil {
		_ = json.Unmarshal(data, &store)
	}

	cloned := *deepCopyJob(job)
	found := false
	for i, j := range store.Jobs {
		if j.ID == job.ID {
			store.Jobs[i] = cloned
			found = true
			break
		}
	}
	if !found {
		store.Jobs = append([]MihomoSpeedTestJob{cloned}, store.Jobs...)
	}

	if err := s.persistSpeedTestStoreLocked(store); err != nil {
		log.Printf("[speedtest] failed to persist speedtest job %s: %v", job.ID, err)
	}
}

// candidateFilter checks exclusion rules and provider metadata.
func isExcludedCandidate(name string, groupNames map[string]bool) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return true
	}
	if trimmed == "DIRECT" || trimmed == "REJECT" {
		return true
	}
	if groupNames[trimmed] {
		return true
	}
	if isSubscriptionMetadataNode(trimmed) {
		return true
	}
	return false
}

// computeCandidateFingerprint hashes the candidates list and subscription identity.
func computeCandidateFingerprint(candidates []string, subIdentity string) string {
	candJSON, _ := json.Marshal(candidates)
	h := sha256.New()
	h.Write(candJSON)
	h.Write([]byte("|"))
	h.Write([]byte(subIdentity))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// computeCurrentCandidateFingerprint discovers all group names and computes fingerprint of all valid candidate nodes in 家庭测速.
func (s *Server) computeCurrentCandidateFingerprint(proxiesData mihomoProxiesData) (string, []string, bool) {
	if proxiesData.Proxies == nil {
		return "", nil, false
	}
	groupNames := make(map[string]bool)
	for name, p := range proxiesData.Proxies {
		if len(p.All) > 0 || p.Type == "Selector" || p.Type == "URLTest" || p.Type == "Fallback" || p.Type == "LoadBalance" {
			groupNames[name] = true
		}
	}
	benchGroup, ok := proxiesData.Proxies[speedTestBenchmarkGroup]
	if !ok {
		return "", nil, false
	}
	var candidates []string
	for _, n := range benchGroup.All {
		if !isExcludedCandidate(n, groupNames) {
			candidates = append(candidates, n)
		}
	}
	fp := computeCandidateFingerprint(candidates, s.getSubscriptionIdentity())
	return fp, candidates, true
}

// calculateMedian computes median of a slice of floats.
func calculateMedian(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2.0
}

// calculateMinimum computes minimum of a slice of floats.
func calculateMinimum(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	min := vals[0]
	for _, v := range vals[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

// calculateVideoBadge maps minimum confirmed mbps to video badge.
func calculateVideoBadge(minMbps float64) string {
	if minMbps < 8.0 {
		return "unstable / below 1080p target"
	}
	if minMbps < 15.0 {
		return "1080p"
	}
	if minMbps < 25.0 {
		return "1440p"
	}
	if minMbps < 40.0 {
		return "1440p60"
	}
	return "4K candidate"
}

// buildShortlist filters candidates into regional quotas for Quick test (max 10 nodes).
func buildShortlist(candidates []speedTestCandidate, initialProductionNode string) []speedTestCandidate {
	var hk, jp, twsg, other []speedTestCandidate
	for _, c := range candidates {
		switch {
		case reHK.MatchString(c.name):
			hk = append(hk, c)
		case reJP.MatchString(c.name):
			jp = append(jp, c)
		case reTWSG.MatchString(c.name):
			twsg = append(twsg, c)
		default:
			other = append(other, c)
		}
	}

	byDelay := func(a, b speedTestCandidate) int {
		if a.delay != b.delay {
			return a.delay - b.delay
		}
		return strings.Compare(a.name, b.name)
	}
	slicesSortFunc(hk, byDelay)
	slicesSortFunc(jp, byDelay)
	slicesSortFunc(twsg, byDelay)
	slicesSortFunc(other, byDelay)

	take := func(src []speedTestCandidate, max int) []speedTestCandidate {
		if len(src) <= max {
			return src
		}
		return src[:max]
	}

	selected := make([]speedTestCandidate, 0, 10)
	selected = append(selected, take(hk, 3)...)
	selected = append(selected, take(jp, 2)...)
	selected = append(selected, take(twsg, 2)...)
	selected = append(selected, take(other, 2)...)

	// Check if initialProductionNode is already included
	hasCurrent := false
	for _, s := range selected {
		if s.name == initialProductionNode {
			hasCurrent = true
			break
		}
	}
	if !hasCurrent && initialProductionNode != "" {
		for _, c := range candidates {
			if c.name == initialProductionNode {
				selected = append(selected, c)
				break
			}
		}
	}

	return selected
}

func slicesSortFunc[T any](s []T, cmp func(a, b T) int) {
	sort.SliceStable(s, func(i, j int) bool {
		return cmp(s[i], s[j]) < 0
	})
}

// sortResults ranks results according to the specification.
func sortResults(results []MihomoSpeedTestNodeResult) {
	sort.SliceStable(results, func(i, j int) bool {
		a := results[i]
		b := results[j]

		// 1. Eligible before ineligible
		if a.Eligible != b.Eligible {
			return a.Eligible
		}

		// 2. Higher effective_mbps
		if math.Abs(a.EffectiveMbps-b.EffectiveMbps) > 1e-4 {
			return a.EffectiveMbps > b.EffectiveMbps
		}

		// 3. Higher minimum_mbps
		if math.Abs(a.MinimumMbps-b.MinimumMbps) > 1e-4 {
			return a.MinimumMbps > b.MinimumMbps
		}

		// 4. Lower median TTFB
		aTTFB := medianTTFB(a.Runs)
		bTTFB := medianTTFB(b.Runs)
		if aTTFB != bTTFB {
			return aTTFB < bTTFB
		}

		// 5. Lower median delay
		if a.DelayMs != b.DelayMs {
			return a.DelayMs < b.DelayMs
		}

		// 6. Stable node-name ordering
		return a.Node < b.Node
	})
}

func medianTTFB(runs []MihomoSpeedTestRun) int64 {
	var ttfbs []float64
	for _, r := range runs {
		if r.Success && r.TTFBMs > 0 {
			ttfbs = append(ttfbs, float64(r.TTFBMs))
		}
	}
	if len(ttfbs) == 0 {
		return 999999
	}
	return int64(math.Round(calculateMedian(ttfbs)))
}

type mihomoProxiesData struct {
	Proxies map[string]struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Now     string   `json:"now"`
		All     []string `json:"all"`
		History []struct {
			Delay int `json:"delay"`
		} `json:"history"`
	} `json:"proxies"`
}

func (s *Server) getMihomoProxies() (mihomoProxiesData, error) {
	var data mihomoProxiesData
	err := s.mihomoRequest("GET", "/proxies", nil, &data)
	return data, err
}

func (s *Server) getSubscriptionIdentity() string {
	content, err := os.ReadFile(filepath.Join(s.data, "mihomo", "subscription.url"))
	if err == nil && len(content) > 0 {
		h := sha256.Sum256(content)
		return hex.EncodeToString(h[:8])
	}
	return "default"
}

func (s *Server) checkMihomoControllerOnline() bool {
	var v struct {
		Version string `json:"version"`
	}
	return s.mihomoRequest("GET", "/version", nil, &v) == nil
}

func (s *Server) checkBenchmarkListenerOnline() bool {
	if s.speedTestCheckListener != nil {
		return s.speedTestCheckListener()
	}
	d := &net.Dialer{Timeout: 1 * time.Second}
	conn, err := d.Dial("tcp", "127.0.0.1:17894")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func classifyRunError(err error, status int, received, requested int64) string {
	if err != nil {
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
			return "cancelled"
		}

		errMsg := strings.ToLower(err.Error())

		if strings.Contains(errMsg, "node_budget_exceeded") {
			return "node_budget_exceeded"
		}
		if strings.Contains(errMsg, "data_budget_exceeded") {
			return "data_budget_exceeded"
		}

		// Phase 1: Response headers timeout
		if strings.Contains(errMsg, "header") {
			return "header_timeout"
		}

		// Phase 2: Dial / connect / TLS / DNS lookup
		if strings.Contains(errMsg, "connect") || strings.Contains(errMsg, "dial") || strings.Contains(errMsg, "lookup") || strings.Contains(errMsg, "name resolution") || strings.Contains(errMsg, "tls") || strings.Contains(errMsg, "handshake") {
			return "connect_timeout"
		}

		// Phase 3: Body read / client timeout / deadline / body idle
		if strings.Contains(errMsg, "body") || strings.Contains(errMsg, "read") || (status > 0 && (strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline"))) {
			return "body_timeout"
		}

		// Phase 4: Remaining net.Error.Timeout without explicit phase text.
		// 约定：已收到 HTTP 状态（status > 0）时的通用超时归类为 body_timeout；
		// 未收到响应头（status == 0）时的通用超时稳定归类为 connect_timeout。
		var netErr net.Error
		if (errors.As(err, &netErr) && netErr.Timeout()) || strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline") {
			if status > 0 {
				return "body_timeout"
			}
			return "connect_timeout"
		}

		return "unexpected_status"
	}
	if status != 200 && status != 206 {
		return "unexpected_status"
	}
	if float64(received) < float64(requested)*0.9 {
		return "short_read"
	}
	return ""
}

// pickSpecificErrorCode selects the most specific error code when multiple errors occur on a node.
func pickSpecificErrorCode(prev, next string) string {
	if prev == "" {
		return next
	}
	if next == "" {
		return prev
	}
	if prev == next {
		return prev
	}
	priority := func(code string) int {
		switch code {
		case "node_budget_exceeded":
			return 10
		case "body_timeout":
			return 9
		case "header_timeout":
			return 8
		case "connect_timeout":
			return 7
		case "cancelled":
			return 6
		case "short_read":
			return 5
		case "unexpected_status":
			return 4
		default:
			return 1
		}
	}
	if priority(next) >= priority(prev) {
		return next
	}
	return prev
}

// runSpeedTestWorker orchestrates the benchmark asynchronously.
func (s *Server) runSpeedTestWorker(ctx context.Context, job *MihomoSpeedTestJob, allowBusyNetwork bool) {
	var initialTestSelectorNode string
	var finalizeOnce sync.Once
	finalize := func() {
		finalizeOnce.Do(func() {
			// 1. Restore 家庭测速 selection before publishing terminal snapshot
			if initialTestSelectorNode != "" {
				_ = s.mihomoRequest("PUT", "/proxies/"+url.PathEscape(speedTestBenchmarkGroup), map[string]string{"name": initialTestSelectorNode}, nil)
			}

			// 2. Resolve final state
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				job.State = "failed"
				job.Phase = "failed"
				job.ErrorCode = "job_timeout"
				job.ErrorMessage = "测速任务超过最大执行时间"
			} else if ctx.Err() != nil && job.State != "failed" {
				job.State = "cancelled"
				job.Phase = "cancelled"
				job.ErrorCode = "cancelled"
				job.ErrorMessage = "测速已取消"
			} else if job.State == "running" {
				job.State = "completed"
				job.Phase = "completed"
			}
			if job.FinishedAt == "" {
				job.FinishedAt = time.Now().Format(time.RFC3339)
			}
			if job.State == "completed" {
				job.TotalRuns = job.CompletedRuns
			}
			// 3. Publish terminal snapshot and clean up cancel under lock
			s.finalizeSpeedTestJob(job.ID, job)
		})
	}
	defer finalize()

	failJob := func(code, msg string) {
		job.State = "failed"
		job.Phase = "failed"
		job.ErrorCode = code
		job.ErrorMessage = msg
		if job.FinishedAt == "" {
			job.FinishedAt = time.Now().Format(time.RFC3339)
		}
	}

	cancelJob := func() {
		job.State = "cancelled"
		job.Phase = "cancelled"
		job.ErrorCode = "cancelled"
		job.ErrorMessage = "测速已取消"
		if job.FinishedAt == "" {
			job.FinishedAt = time.Now().Format(time.RFC3339)
		}
	}

	checkContext := func() bool {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			failJob("job_timeout", "测速任务超过最大执行时间")
			return false
		}
		if ctx.Err() != nil {
			cancelJob()
			return false
		}
		return true
	}

	// 1. Controller health
	if !s.checkMihomoControllerOnline() {
		failJob("mihomo_offline", "Mihomo 控制器未在线")
		return
	}

	// 2. Discover groups and candidates
	proxiesData, err := s.getMihomoProxies()
	if err != nil {
		failJob("mihomo_offline", "读取代理列表失败")
		return
	}

	benchGroup, hasBenchGroup := proxiesData.Proxies[speedTestBenchmarkGroup]
	if !hasBenchGroup {
		failJob("benchmark_group_missing", "配置中缺少家庭测速组")
		return
	}

	// Compute candidate fingerprint from the full ordered filtered candidate list per design
	fp, candidateNames, ok := s.computeCurrentCandidateFingerprint(proxiesData)
	if !ok || len(candidateNames) == 0 {
		failJob("benchmark_group_missing", "家庭测速组中没有可用节点")
		return
	}
	job.CandidateFingerprint = fp
	s.publishSpeedTestSnapshot(job)
	s.persistJob(job)

	// Identify initial production concrete node in 家庭RULE
	initialProductionNode := ""
	if prodGroup, ok := proxiesData.Proxies[speedTestProductionGroup]; ok {
		curr := prodGroup.Now
		for i := 0; i < 5; i++ {
			if next, ok := proxiesData.Proxies[curr]; ok && len(next.All) > 0 && next.Now != "" {
				curr = next.Now
			} else {
				break
			}
		}
		initialProductionNode = curr
	}
	job.InitialProductionNode = initialProductionNode
	initialTestSelectorNode = benchGroup.Now

	// 3. Test listener online
	if !s.checkBenchmarkListenerOnline() {
		failJob("benchmark_listener_unavailable", "测速监听端口 17894 不可用")
		return
	}

	// 4. Delay screening via group delay API
	delayPath := "/group/" + url.PathEscape(speedTestBenchmarkGroup) + "/delay?url=" + url.QueryEscape("https://www.gstatic.com/generate_204") + "&timeout=5000"
	delays := make(map[string]int)
	_ = s.mihomoRequest("GET", delayPath, nil, &delays)

	var healthyCandidates []speedTestCandidate
	for _, name := range candidateNames {
		d, ok := delays[name]
		if ok && d > 0 {
			proto := ""
			if p, ok := proxiesData.Proxies[name]; ok {
				proto = p.Type
			}
			healthyCandidates = append(healthyCandidates, speedTestCandidate{name: name, protocol: proto, delay: d})
		}
	}

	if len(healthyCandidates) == 0 {
		failJob("benchmark_group_missing", "所有测速节点初步连通性检测失败")
		return
	}

	// Check cancellation
	if !checkContext() {
		return
	}

	// 5. WAN background traffic check bypassed: always ignore background traffic and directly start testing
	job.Contaminated = false

	// 6. Build test plan
	var testPool []speedTestCandidate
	var prelimSampleSize int64
	var prelimBudget int64
	var confirmSampleSize int64 = quickConfirmBytes

	if job.Mode == "quick" {
		prelimSampleSize = quickPreliminaryBytes
		prelimBudget = quickBudgetBytes
		testPool = buildShortlist(healthyCandidates, initialProductionNode)
	} else {
		prelimSampleSize = deepPreliminaryBytes
		prelimBudget = deepBudgetBytes
		testPool = healthyCandidates
	}
	if job.DataBudgetBytes <= 0 {
		job.DataBudgetBytes = prelimBudget
	}

	// Map of node name to result index
	nodeResultMap := make(map[string]int)
	for _, c := range testPool {
		idx := len(job.Results)
		job.Results = append(job.Results, MihomoSpeedTestNodeResult{
			Node:     c.name,
			Protocol: c.protocol,
			DelayMs:  c.delay,
			Runs:     []MihomoSpeedTestRun{},
		})
		nodeResultMap[c.name] = idx
	}

	// Estimate total runs (preliminary + 2 * finalists)
	maxFinalists := 3
	if job.Mode == "deep" {
		maxFinalists = 5
	}
	job.TotalRuns = len(testPool) + min(len(testPool), maxFinalists)*2

	verifyFingerprint := func() bool {
		pd, err := s.getMihomoProxies()
		if err != nil {
			return false
		}
		currFp, _, ok := s.computeCurrentCandidateFingerprint(pd)
		if !ok {
			return false
		}
		return currFp == job.CandidateFingerprint
	}

	nodeBytesUsed := make(map[string]int64)
	maxNodeBudget := maxNodeBudgetQuick
	if job.Mode == "deep" {
		maxNodeBudget = maxNodeBudgetDeep
	}

	executeRun := func(nodeName, stage string, sampleBytes int64) (MihomoSpeedTestRun, bool) {
		if !checkContext() {
			return MihomoSpeedTestRun{}, false
		}
		if !verifyFingerprint() {
			failJob("configuration_changed", "订阅或代理配置在测速期间已变更")
			return MihomoSpeedTestRun{}, false
		}
		if job.BytesUsed+sampleBytes > job.DataBudgetBytes {
			failJob("data_budget_exceeded", "已达到最大测试流量限额")
			return MihomoSpeedTestRun{}, false
		}
		if nodeBytesUsed[nodeName]+sampleBytes > maxNodeBudget {
			job.CompletedRuns++
			run := MihomoSpeedTestRun{
				Stage:          stage,
				RequestedBytes: sampleBytes,
				ReceivedBytes:  0,
				DurationMs:     0,
				Mbps:           0,
				HTTPStatus:     0,
				Success:        false,
				ErrorCode:      "node_budget_exceeded",
			}
			return run, true
		}

		job.CurrentNode = nodeName
		// Switch benchmark selector only (NEVER touch 家庭RULE!)
		if err := s.mihomoRequest("PUT", "/proxies/"+url.PathEscape(speedTestBenchmarkGroup), map[string]string{"name": nodeName}, nil); err != nil {
			failJob("mihomo_offline", "切换测速节点失败")
			return MihomoSpeedTestRun{}, false
		}

		received, duration, ttfb, status, runErr := s.speedTestHTTP(ctx, speedTestProxyURL, speedTestTargetURL, sampleBytes)
		if received > 0 {
			job.BytesUsed += received
			nodeBytesUsed[nodeName] += received
		}
		if !checkContext() {
			return MihomoSpeedTestRun{}, false
		}

		job.CompletedRuns++

		success := runErr == nil && (status == 200 || status == 206) && float64(received) >= float64(sampleBytes)*0.9
		errCode := classifyRunError(runErr, status, received, sampleBytes)

		var mbps float64
		if success && duration.Seconds() > 0 {
			mbps = math.Round((float64(received)*8.0)/(duration.Seconds()*1e6)*10) / 10
		}

		run := MihomoSpeedTestRun{
			Stage:          stage,
			RequestedBytes: sampleBytes,
			ReceivedBytes:  received,
			DurationMs:     duration.Milliseconds(),
			TTFBMs:         ttfb.Milliseconds(),
			Mbps:           mbps,
			HTTPStatus:     status,
			Success:        success,
			ErrorCode:      errCode,
		}
		return run, true
	}

	// 7. Preliminary Round
	job.Phase = "preliminary"
	s.publishSpeedTestSnapshot(job)
	s.persistJob(job)
	for _, c := range testPool {
		run, ok := executeRun(c.name, "preliminary", prelimSampleSize)
		if !ok {
			return
		}
		idx := nodeResultMap[c.name]
		job.Results[idx].Runs = append(job.Results[idx].Runs, run)
		if !run.Success && run.ErrorCode != "" {
			job.Results[idx].ErrorCode = pickSpecificErrorCode(job.Results[idx].ErrorCode, run.ErrorCode)
		}
		s.publishSpeedTestSnapshot(job)
		s.persistJob(job)
	}

	// 8. Select Finalists
	type prelimCandidate struct {
		node string
		mbps float64
		ok   bool
	}
	var prelimRank []prelimCandidate
	for _, r := range job.Results {
		if len(r.Runs) > 0 && r.Runs[0].Success {
			prelimRank = append(prelimRank, prelimCandidate{node: r.Node, mbps: r.Runs[0].Mbps, ok: true})
		}
	}
	sort.SliceStable(prelimRank, func(i, j int) bool {
		return prelimRank[i].mbps > prelimRank[j].mbps
	})

	var finalistNodes []string
	for i := 0; i < len(prelimRank) && i < maxFinalists; i++ {
		finalistNodes = append(finalistNodes, prelimRank[i].node)
	}

	// Update TotalRuns to reflect actual finalists selected
	job.TotalRuns = len(testPool) + len(finalistNodes)*2
	job.Phase = "confirming"
	s.publishSpeedTestSnapshot(job)
	s.persistJob(job)

	// 9. Confirmation Rounds (2 confirmation runs per finalist)
	if len(finalistNodes) > 0 {
		for round := 1; round <= 2; round++ {
			// Interleave finalists and randomize order within each round
			roundOrder := make([]string, len(finalistNodes))
			copy(roundOrder, finalistNodes)
			mrand.Shuffle(len(roundOrder), func(i, j int) {
				roundOrder[i], roundOrder[j] = roundOrder[j], roundOrder[i]
			})

			for _, nodeName := range roundOrder {
				run, ok := executeRun(nodeName, "confirmation", confirmSampleSize)
				if !ok {
					return
				}
				idx := nodeResultMap[nodeName]
				job.Results[idx].Runs = append(job.Results[idx].Runs, run)
				if !run.Success && run.ErrorCode != "" {
					job.Results[idx].ErrorCode = pickSpecificErrorCode(job.Results[idx].ErrorCode, run.ErrorCode)
				}
				s.publishSpeedTestSnapshot(job)
				s.persistJob(job)
			}
		}
	}

	// 10. Scoring and Recommendation
	if job.State != "running" {
		return
	}

	for i := range job.Results {
		r := &job.Results[i]
		var confirmSpeeds []float64
		hardFailures := 0
		for _, run := range r.Runs {
			if run.Stage == "confirmation" {
				if run.Success {
					confirmSpeeds = append(confirmSpeeds, run.Mbps)
				} else {
					hardFailures++
				}
			}
		}

		if len(confirmSpeeds) >= 2 && hardFailures == 0 {
			r.MinimumMbps = math.Round(calculateMinimum(confirmSpeeds)*10) / 10
			r.MedianMbps = math.Round(calculateMedian(confirmSpeeds)*10) / 10
			r.EffectiveMbps = math.Round((0.70*r.MinimumMbps+0.30*r.MedianMbps)*10) / 10
			r.Eligible = r.MinimumMbps >= 8.0
			r.VideoBadge = calculateVideoBadge(r.MinimumMbps)
		} else {
			r.Eligible = false
			if len(confirmSpeeds) > 0 {
				r.MinimumMbps = math.Round(calculateMinimum(confirmSpeeds)*10) / 10
				r.MedianMbps = math.Round(calculateMedian(confirmSpeeds)*10) / 10
				r.EffectiveMbps = math.Round((0.70*r.MinimumMbps+0.30*r.MedianMbps)*10) / 10
				r.VideoBadge = calculateVideoBadge(r.MinimumMbps)
			} else {
				r.VideoBadge = "unstable / below 1080p target"
			}
		}
	}

	// Sort results according to specification
	sortResults(job.Results)

	// Pick winner
	job.RecommendedNode = ""
	for _, r := range job.Results {
		if r.Eligible {
			job.RecommendedNode = r.Node
			break
		}
	}

	job.State = "completed"
	job.Phase = "completed"
	job.TotalRuns = job.CompletedRuns // Ensure terminal progress is 100%
	job.FinishedAt = time.Now().Format(time.RFC3339)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

// HTTP API Handlers

func (s *Server) mihomoSpeedTest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		job := s.getSpeedTestJobSnapshot()
		if job == nil {
			jsonOut(w, http.StatusOK, map[string]any{
				"id":      "",
				"mode":    "",
				"state":   "idle",
				"phase":   "idle",
				"results": []any{},
			})
			return
		}
		jsonOut(w, http.StatusOK, job)

	case http.MethodPost:
		var body struct {
			Mode             string `json:"mode"`
			AllowBusyNetwork bool   `json:"allowBusyNetwork"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求参数无效: " + err.Error()})
			return
		}
		if body.Mode != "quick" && body.Mode != "deep" {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "mode 必须为 quick 或 deep"})
			return
		}

		s.speedTestMu.Lock()
		if s.speedTestJob != nil && s.speedTestJob.State == "running" {
			currID := s.speedTestJob.ID
			s.speedTestMu.Unlock()
			jsonOut(w, http.StatusConflict, map[string]any{
				"error": "测速任务正在运行中",
				"jobId": currID,
			})
			return
		}

		var timeout time.Duration
		if body.Mode == "quick" {
			timeout = speedTestQuickTimeout
		} else {
			timeout = speedTestDeepTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		jobID := fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405Z"), randomHex(3))
		budget := quickBudgetBytes
		if body.Mode == "deep" {
			budget = deepBudgetBytes
		}

		job := &MihomoSpeedTestJob{
			ID:              jobID,
			Mode:            body.Mode,
			State:           "running",
			Phase:           "discovering",
			StartedAt:       time.Now().Format(time.RFC3339),
			DataBudgetBytes: budget,
			Results:         []MihomoSpeedTestNodeResult{},
		}
		resp := deepCopyJob(job)
		s.speedTestJob = deepCopyJob(job)
		s.speedTestCancel = cancel
		s.speedTestMu.Unlock()

		s.persistJob(resp)

		go func() {
			defer cancel()
			s.runSpeedTestWorker(ctx, job, body.AllowBusyNetwork)
		}()
		jsonOut(w, http.StatusAccepted, resp)

	case http.MethodDelete:
		s.speedTestMu.Lock()
		if s.speedTestJob == nil || s.speedTestJob.State != "running" || s.speedTestCancel == nil {
			s.speedTestMu.Unlock()
			jsonOut(w, http.StatusConflict, map[string]string{"error": "当前没有正在运行的测速任务"})
			return
		}
		cancel := s.speedTestCancel
		s.speedTestCancel = nil
		s.speedTestMu.Unlock()

		cancel()
		jsonOut(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "方法不支持"})
	}
}

func (s *Server) mihomoSpeedTestApply(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPut) {
		return
	}

	var body struct {
		JobID string `json:"jobId"`
		Node  string `json:"node"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求参数无效"})
		return
	}

	body.Node = strings.TrimSpace(body.Node)
	if body.JobID == "" || body.Node == "" || len(body.Node) > 256 || !utf8.ValidString(body.Node) {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "任务 ID 或节点名称无效"})
		return
	}
	for _, c := range body.Node {
		if c < 0x20 || c == 0x7f {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "节点名称包含非法字符"})
			return
		}
	}

	job := s.getSpeedTestJobSnapshot()
	if job == nil || job.ID != body.JobID {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "测速任务不存在或不匹配"})
		return
	}
	if job.State != "completed" {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "只能应用已完成的测速结果"})
		return
	}

	// Verify node was eligible
	eligible := false
	for _, r := range job.Results {
		if r.Node == body.Node && r.Eligible {
			eligible = true
			break
		}
	}
	if !eligible {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "该节点未达到推荐标准或不合格，无法应用"})
		return
	}

	// Verify Mihomo online
	proxiesData, err := s.getMihomoProxies()
	if err != nil {
		jsonOut(w, http.StatusBadGateway, map[string]string{"error": "Mihomo 未在线"})
		return
	}

	// Verify candidate fingerprint still matches using full ordered candidate list
	currFp, _, ok := s.computeCurrentCandidateFingerprint(proxiesData)
	if !ok || currFp != job.CandidateFingerprint {
		jsonOut(w, http.StatusConflict, map[string]string{"error": "配置或订阅已变更，测速结果已失效"})
		return
	}

	// Verify node belongs to 家庭RULE
	prodGroup, ok := proxiesData.Proxies[speedTestProductionGroup]
	if !ok {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "家庭RULE组不存在"})
		return
	}
	foundInRule := false
	for _, n := range prodGroup.All {
		if n == body.Node {
			foundInRule = true
			break
		}
	}
	if !foundInRule {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "所选节点不属于家庭RULE代理组"})
		return
	}

	oldNode := prodGroup.Now

	// Apply node to 家庭RULE
	if err := s.mihomoRequest("PUT", "/proxies/"+url.PathEscape(speedTestProductionGroup), map[string]string{"name": body.Node}, nil); err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "切换家庭RULE节点失败"})
		return
	}

	jsonOut(w, http.StatusOK, map[string]any{
		"ok":      true,
		"oldNode": oldNode,
		"newNode": body.Node,
	})
}
