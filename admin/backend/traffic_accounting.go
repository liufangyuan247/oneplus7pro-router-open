package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	trafficAccountingVersion      = 1
	defaultAccountingPollInterval = time.Second
	defaultAccountingSaveInterval = 10 * time.Second
	maxAccountingPollTimeout      = 800 * time.Millisecond
	maxAccountingResponseBytes    = 16 * 1024 * 1024 // 16 MiB
	maxAccountingConnections      = 10000
	maxAccountingTargets          = 5000
	maxAccountingDevices          = 500
	maxAccountingSubscriptions    = 100
	maxAccountingNodes            = 1000
	maxAccountingGroups           = 200
	maxAccountingDeviceRoutes     = 5000
	maxAPIRankingRows             = 200
	pricingBackfillVersion        = 1
	minMultiplierMilli            = 100    // 0.1x
	maxMultiplierMilli            = 100000 // 100x
	otherKey                      = "_other"
)

// initialHummingbirdPresetMultipliers is a bootstrap fallback only. Runtime
// settings persisted in traffic-accounting-settings.json are authoritative and
// are never replaced by this table on restart or subscription refresh. Rates
// may change over time; already-priced traffic retains its original rate, while
// pending unpriced traffic is backfilled once a rate becomes available.
var initialHummingbirdPresetMultipliers = map[string]uint32{
	"美国17 | 直连 | 三网优化":   2000,
	"美国 | 07":            2000,
	"美国05 | 直连 | 三网优化":   1500,
	"美国11 | A | 三网优化":    1500,
	"美国08 | 直连 | 三网优化":   1500,
	"美国02 | A | 三网优化":    1500,
	"美国16 | 直连 | 三网优化":   1500,
	"美国06| 直连 | 三网优化":    1500,
	"美国09 | 自研 | 三网优化":   2000,
	"香港17 | 自研 | 三网优化":   1500,
	"香港 | 01":            2000,
	"香港03 | 三网优化 | 网页":   1500,
	"香港04 | 三网优化 | 网页":   1500,
	"香港 | 05":            2000,
	"香港06 | Hy2 | 三网优化":  1500,
	"香港 | 08":            2000,
	"香港10 | A | 三网优化":    2000,
	"香港09 | 自研 | 移动":     1500,
	"香港12 | A | 三网优化":    1500,
	"台湾 | 01":            2000,
	"台湾 | 03":            2000,
	"韩国 | 01":            2000,
	"日本 | 03":            1500,
	"日本 | 04":            1500,
	"日本05 | 直连 | 移动":     1000,
	"日本06 | 直连":          1000,
	"日本07 | 直连 | 三网优化":   2000,
	"新加坡02":              2000,
	"新加坡04":              2000,
	"美国 | 01":            2000,
	"美国 | 03":            1500,
	"美国10 | 直连 | 三网优化":   2000,
	"越南01 | 三网优化 | 住宅IP": 2000,
	"德国 | 01":            1500,
	"德国02 | 直连 | 三网优化":   1500,
	"英国 | 01":            1500,
	"法国 | 01":            1500,
	"意大利 | 01":           1500,
	"澳大利亚01 | 三网优化":      2000,
}

// TrafficStats captures logical payload bytes and connection counts.
type TrafficStats struct {
	UploadBytes           uint64 `json:"uploadBytes"`
	DownloadBytes         uint64 `json:"downloadBytes"`
	RawBytes              uint64 `json:"rawBytes"`
	WeightedUploadBytes   uint64 `json:"weightedUploadBytes"`
	WeightedDownloadBytes uint64 `json:"weightedDownloadBytes"`
	WeightedBytes         uint64 `json:"weightedBytes"`
	UnpricedRawBytes      uint64 `json:"unpricedRawBytes"`
	ConnectionCount       uint64 `json:"connectionCount"`
	LastSeenAt            string `json:"lastSeenAt,omitempty"`
}

// DirectionalBytes preserves the upload/download split for traffic that could
// not be priced yet. Keeping this outside TrafficStats prevents internal
// backfill metadata from leaking into the public accounting API.
type DirectionalBytes struct {
	UploadBytes   uint64 `json:"uploadBytes"`
	DownloadBytes uint64 `json:"downloadBytes"`
}

func (b *DirectionalBytes) Add(up, down uint64) {
	b.UploadBytes = safeAddUint64(b.UploadBytes, up)
	b.DownloadBytes = safeAddUint64(b.DownloadBytes, down)
}

// PendingNodePricing is the exact cross-dimensional journal for one
// subscription/node pair while it has no multiplier. Once a multiplier exists,
// the journal can be replayed into every affected aggregate.
type PendingNodePricing struct {
	SubscriptionID string                      `json:"subscriptionId"`
	Node           string                      `json:"node"`
	Proxy          DirectionalBytes            `json:"proxy"`
	Unassigned     DirectionalBytes            `json:"unassigned,omitempty"`
	Speedtest      DirectionalBytes            `json:"speedtest,omitempty"`
	Devices        map[string]DirectionalBytes `json:"devices,omitempty"`
	Subscriptions  map[string]DirectionalBytes `json:"subscriptions,omitempty"`
	Nodes          map[string]DirectionalBytes `json:"nodes,omitempty"`
	Groups         map[string]DirectionalBytes `json:"groups,omitempty"`
	Targets        map[string]DirectionalBytes `json:"targets,omitempty"`
	DeviceRoutes   map[string]DirectionalBytes `json:"deviceRoutes,omitempty"`
	Hourly         map[string]DirectionalBytes `json:"hourly,omitempty"`
	Daily          map[string]DirectionalBytes `json:"daily,omitempty"`
}

func (s *TrafficStats) Add(up, down, wUp, wDown, unpriced uint64, incConn bool, at string) {
	s.UploadBytes = safeAddUint64(s.UploadBytes, up)
	s.DownloadBytes = safeAddUint64(s.DownloadBytes, down)
	s.RawBytes = safeAddUint64(s.UploadBytes, s.DownloadBytes)
	s.WeightedUploadBytes = safeAddUint64(s.WeightedUploadBytes, wUp)
	s.WeightedDownloadBytes = safeAddUint64(s.WeightedDownloadBytes, wDown)
	s.WeightedBytes = safeAddUint64(s.WeightedUploadBytes, s.WeightedDownloadBytes)
	s.UnpricedRawBytes = safeAddUint64(s.UnpricedRawBytes, unpriced)
	if incConn {
		s.ConnectionCount = safeAddUint64(s.ConnectionCount, 1)
	}
	if at != "" {
		s.LastSeenAt = at
	}
}

func (s *TrafficStats) Merge(other TrafficStats) {
	s.UploadBytes = safeAddUint64(s.UploadBytes, other.UploadBytes)
	s.DownloadBytes = safeAddUint64(s.DownloadBytes, other.DownloadBytes)
	s.RawBytes = safeAddUint64(s.UploadBytes, s.DownloadBytes)
	s.WeightedUploadBytes = safeAddUint64(s.WeightedUploadBytes, other.WeightedUploadBytes)
	s.WeightedDownloadBytes = safeAddUint64(s.WeightedDownloadBytes, other.WeightedDownloadBytes)
	s.WeightedBytes = safeAddUint64(s.WeightedUploadBytes, s.WeightedDownloadBytes)
	s.UnpricedRawBytes = safeAddUint64(s.UnpricedRawBytes, other.UnpricedRawBytes)
	s.ConnectionCount = safeAddUint64(s.ConnectionCount, other.ConnectionCount)
	if other.LastSeenAt > s.LastSeenAt {
		s.LastSeenAt = other.LastSeenAt
	}
}

type DeviceAccounting struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	TrafficStats
}

type SubscriptionAccounting struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	TrafficStats
}

type NodeAccounting struct {
	Node             string  `json:"node"`
	SubscriptionID   string  `json:"subscriptionId,omitempty"`
	SubscriptionName string  `json:"subscriptionName,omitempty"`
	MultiplierMilli  *uint32 `json:"multiplierMilli,omitempty"`
	PricingStatus    string  `json:"pricingStatus"` // "node_override" | "subscription_default" | "unpriced"
	TrafficStats
}

type GroupAccounting struct {
	Group string `json:"group"`
	TrafficStats
}

type TargetAccounting struct {
	Target string `json:"target"`
	TrafficStats
}

type DeviceRouteAccounting struct {
	IP               string `json:"ip"`
	Hostname         string `json:"hostname"`
	RouteKind        string `json:"routeKind"` // "direct" | "reject" | "proxy"
	SubscriptionID   string `json:"subscriptionId,omitempty"`
	SubscriptionName string `json:"subscriptionName,omitempty"`
	Node             string `json:"node,omitempty"`
	Group            string `json:"group,omitempty"`
	TrafficStats
}

type TimeAccounting struct {
	Time string `json:"time"` // RFC3339 hour or YYYY-MM-DD date
	TrafficStats
}

type ProxyRate struct {
	UploadBps   float64 `json:"uploadBps"`
	DownloadBps float64 `json:"downloadBps"`
}

type TrafficAccountingSettings struct {
	Version       int                                      `json:"version"`
	Enabled       bool                                     `json:"enabled"`
	Subscriptions map[string]*SubscriptionAccountingConfig `json:"subscriptions"`
}

// UnpricedNodeInfo exposes the complete pending-pricing journal to the
// settings UI. It is intentionally kept out of TrafficAccountingSettings so
// runtime accounting state can never be persisted as configuration.
type UnpricedNodeInfo struct {
	SubscriptionID   string `json:"subscriptionId"`
	SubscriptionName string `json:"subscriptionName,omitempty"`
	Node             string `json:"node"`
	UnpricedRawBytes uint64 `json:"unpricedRawBytes"`
	LastSeenAt       string `json:"lastSeenAt,omitempty"`
	Configurable     bool   `json:"configurable"`
}

type TrafficAccountingSettingsResponse struct {
	Version       int                                      `json:"version"`
	Enabled       bool                                     `json:"enabled"`
	Subscriptions map[string]*SubscriptionAccountingConfig `json:"subscriptions"`
	UnpricedNodes []UnpricedNodeInfo                       `json:"unpricedNodes"`
}

type SubscriptionAccountingConfig struct {
	DefaultMultiplierMilli        *uint32           `json:"defaultMultiplierMilli"`
	NodeMultipliersMilli          map[string]uint32 `json:"nodeMultipliersMilli"`
	AutomaticNodeMultipliersMilli map[string]uint32 `json:"automaticNodeMultipliersMilli,omitempty"`
	AutomaticPricingEnabled       bool              `json:"automaticPricingEnabled,omitempty"`
	AutomaticPricingUpdatedAt     string            `json:"automaticPricingUpdatedAt,omitempty"`
	KnownNodes                    []string          `json:"knownNodes"`
}

type UpdateTrafficSettingsRequest struct {
	Enabled       *bool                                       `json:"enabled,omitempty"`
	Subscriptions map[string]*UpdateSubscriptionConfigRequest `json:"subscriptions,omitempty"`
}

type UpdateSubscriptionConfigRequest struct {
	DefaultMultiplierMilli *uint32           `json:"defaultMultiplierMilli"`
	NodeMultipliersMilli   map[string]uint32 `json:"nodeMultipliersMilli"`
}

type TrafficLedger struct {
	Version              int                                `json:"version"`
	StartedAt            string                             `json:"startedAt"`
	UpdatedAt            string                             `json:"updatedAt"`
	LastSuccessfulPollAt string                             `json:"lastSuccessfulPollAt"`
	PollSuccessCount     uint64                             `json:"pollSuccessCount"`
	PollFailCount        uint64                             `json:"pollFailCount"`
	ConsecutiveFailures  uint64                             `json:"consecutiveFailures"`
	RejectedSnapshots    uint64                             `json:"rejectedSnapshots"`
	CorruptWarning       string                             `json:"corruptWarning,omitempty"`
	TotalObserved        TrafficStats                       `json:"totalObserved"`
	Direct               TrafficStats                       `json:"direct"`
	Proxy                TrafficStats                       `json:"proxy"`
	Reject               TrafficStats                       `json:"reject"`
	UnassignedProxy      TrafficStats                       `json:"unassignedProxy"`
	Speedtest            TrafficStats                       `json:"speedtest"`
	Devices              map[string]*DeviceAccounting       `json:"devices"`
	Subscriptions        map[string]*SubscriptionAccounting `json:"subscriptions"`
	Nodes                map[string]*NodeAccounting         `json:"nodes"`
	Groups               map[string]*GroupAccounting        `json:"groups"`
	Targets              map[string]*TargetAccounting       `json:"targets"`
	DeviceRoutes         map[string]*DeviceRouteAccounting  `json:"deviceRoutes"`
	Hourly               map[string]*TimeAccounting         `json:"hourly"`
	Daily                map[string]*TimeAccounting         `json:"daily"`
	PendingPricing       map[string]*PendingNodePricing     `json:"pendingPricing,omitempty"`
	PricingBackfill      int                                `json:"pricingBackfillVersion,omitempty"`
}

type TrafficAccountingResponse struct {
	Online               bool                     `json:"online"`
	StartedAt            string                   `json:"startedAt"`
	UpdatedAt            string                   `json:"updatedAt"`
	LastSuccessfulPollAt string                   `json:"lastSuccessfulPollAt"`
	PollSuccessCount     uint64                   `json:"pollSuccessCount"`
	PollFailCount        uint64                   `json:"pollFailCount"`
	ConsecutiveFailures  uint64                   `json:"consecutiveFailures"`
	RejectedSnapshots    uint64                   `json:"rejectedSnapshots"`
	Warning              string                   `json:"warning,omitempty"`
	PersistenceError     string                   `json:"persistenceError,omitempty"`
	CurrentProxyRate     ProxyRate                `json:"currentProxyRate"`
	TotalObserved        TrafficStats             `json:"totalObserved"`
	Direct               TrafficStats             `json:"direct"`
	Proxy                TrafficStats             `json:"proxy"`
	Reject               TrafficStats             `json:"reject"`
	UnassignedProxy      TrafficStats             `json:"unassignedProxy"`
	Speedtest            TrafficStats             `json:"speedtest"`
	Devices              []DeviceAccounting       `json:"devices"`
	Subscriptions        []SubscriptionAccounting `json:"subscriptions"`
	Nodes                []NodeAccounting         `json:"nodes"`
	Groups               []GroupAccounting        `json:"groups"`
	Targets              []TargetAccounting       `json:"targets"`
	DeviceRoutes         []DeviceRouteAccounting  `json:"deviceRoutes"`
	Hourly               []TimeAccounting         `json:"hourly"`
	Daily                []TimeAccounting         `json:"daily"`
}

type mihomoRawConnectionsResponse struct {
	DownloadTotal uint64                `json:"downloadTotal"`
	UploadTotal   uint64                `json:"uploadTotal"`
	Memory        uint64                `json:"memory"`
	Connections   []mihomoRawConnection `json:"connections"`
}

type mihomoRawConnection struct {
	ID       string   `json:"id"`
	Upload   uint64   `json:"upload"`
	Download uint64   `json:"download"`
	Start    string   `json:"start"`
	Chains   []string `json:"chains"`
	Rule     string   `json:"rule"`
	Metadata struct {
		Host          string `json:"host"`
		DestinationIP string `json:"destinationIP"`
		SourceIP      string `json:"sourceIP"`
		SourcePort    string `json:"sourcePort"`
		Network       string `json:"network"`
		Type          string `json:"type"`
	} `json:"metadata"`
}

type trackedConnection struct {
	ID               string
	Start            string
	LastUpload       uint64
	LastDownload     uint64
	SubscriptionID   string
	SubscriptionName string
	LeafNode         string
	ProxyGroup       string
	RouteKind        string // "direct" | "reject" | "proxy"
	IsSpeedtest      bool
	SourceIP         string
	DeviceName       string
	Target           string
	CycleID          uint64
}

type proxyRateSample struct {
	at        time.Time
	upBytes   uint64
	downBytes uint64
}

type TrafficAccountingConfig struct {
	DataDir                 string
	PollInterval            time.Duration
	SaveInterval            time.Duration
	FetchConnections        func(ctx context.Context) (*mihomoRawConnectionsResponse, error)
	GetActiveSubscription   func() (id string, name string)
	GetValidSubscriptionIDs func() []string
	GetClientHostnames      func() map[string]string
	LookupClientHost        func(ip string) string
	GetKnownNodes           func() []string
	NowFunc                 func() time.Time
}

type TrafficAccountingManager struct {
	mu                       sync.Mutex
	dataDir                  string
	ledgerPath               string
	settingsPath             string
	hasBaseline              bool
	resetGeneration          uint64
	currentCycleID           uint64
	lastSaveTime             time.Time
	bytesSinceLastSave       uint64
	ledgerDirty              bool
	lastKnownNodesFetch      time.Time
	cachedKnownNodes         []string
	lastClientHostnamesFetch time.Time
	cachedClientHostnames    map[string]string
	ledgerPersistErr         string
	settingsPersistErr       string
	tracked                  map[string]*trackedConnection
	ledger                   TrafficLedger
	settings                 TrafficAccountingSettings
	pollInterval             time.Duration
	saveInterval             time.Duration
	fetchConnections         func(ctx context.Context) (*mihomoRawConnectionsResponse, error)
	getActiveSubscription    func() (id string, name string)
	getValidSubscriptionIDs  func() []string
	getClientHostnames       func() map[string]string
	lookupClientHost         func(ip string) string
	getKnownNodes            func() []string
	nowFunc                  func() time.Time
	rateSamples              []proxyRateSample
	stopCh                   chan struct{}
	doneCh                   chan struct{}
}

func NewTrafficAccountingManager(cfg TrafficAccountingConfig) *TrafficAccountingManager {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultAccountingPollInterval
	}
	saveInterval := cfg.SaveInterval
	if saveInterval <= 0 {
		saveInterval = defaultAccountingSaveInterval
	}
	nowFunc := cfg.NowFunc
	if nowFunc == nil {
		nowFunc = time.Now
	}

	m := &TrafficAccountingManager{
		dataDir:                 cfg.DataDir,
		ledgerPath:              filepath.Join(cfg.DataDir, "traffic-accounting.json"),
		settingsPath:            filepath.Join(cfg.DataDir, "traffic-accounting-settings.json"),
		pollInterval:            pollInterval,
		saveInterval:            saveInterval,
		fetchConnections:        cfg.FetchConnections,
		getActiveSubscription:   cfg.GetActiveSubscription,
		getValidSubscriptionIDs: cfg.GetValidSubscriptionIDs,
		getClientHostnames:      cfg.GetClientHostnames,
		lookupClientHost:        cfg.LookupClientHost,
		getKnownNodes:           cfg.GetKnownNodes,
		nowFunc:                 nowFunc,
		cachedClientHostnames:   make(map[string]string),
		tracked:                 make(map[string]*trackedConnection),
		stopCh:                  make(chan struct{}),
		doneCh:                  make(chan struct{}),
	}

	m.initSettings()
	m.initLedger()
	pricingChanged := m.repriceResolvedPendingLocked()
	if m.reconcileLegacyUnpricedLocked() {
		pricingChanged = true
	}
	if pricingChanged {
		_ = m.persistLedgerLocked()
	}
	m.lastSaveTime = m.now()

	return m
}

func (m *TrafficAccountingManager) now() time.Time {
	if m.nowFunc != nil {
		return m.nowFunc()
	}
	return time.Now()
}

func (m *TrafficAccountingManager) initSettings() {
	_ = os.MkdirAll(m.dataDir, 0700)
	data, err := os.ReadFile(m.settingsPath)
	if err == nil {
		var s TrafficAccountingSettings
		if unmarshalErr := json.Unmarshal(data, &s); unmarshalErr == nil {
			if s.Version != trafficAccountingVersion {
				s.Version = trafficAccountingVersion
			}
			if s.Subscriptions == nil {
				s.Subscriptions = make(map[string]*SubscriptionAccountingConfig)
			}
			m.settings = s
			normalized := false
			for _, cfg := range m.settings.Subscriptions {
				if normalizeSubscriptionAccountingConfig(cfg) {
					normalized = true
				}
			}
			if normalized {
				_ = m.persistSettingsLocked()
			}
			return
		}
		// Settings file exists but is corrupted: back it up and rebuild defaults
		timestamp := m.now().Format("20060102-150405")
		corruptPath := fmt.Sprintf("%s.corrupt-%s", m.settingsPath, timestamp)
		_ = os.Rename(m.settingsPath, corruptPath)
	}

	// Settings file doesn't exist or is invalid: create default
	m.settings = TrafficAccountingSettings{
		Version:       trafficAccountingVersion,
		Enabled:       true,
		Subscriptions: make(map[string]*SubscriptionAccountingConfig),
	}

	// Preset for active subscription if available
	var activeID string
	if m.getActiveSubscription != nil {
		activeID, _ = m.getActiveSubscription()
	}
	if activeID != "" {
		m.applyHummingbirdPresetLocked(activeID)
	}

	_ = m.persistSettingsLocked()
}

func (m *TrafficAccountingManager) applyHummingbirdPresetLocked(subID string) {
	cfg := &SubscriptionAccountingConfig{
		NodeMultipliersMilli: make(map[string]uint32),
		KnownNodes:           make([]string, 0, len(initialHummingbirdPresetMultipliers)),
	}
	for node, mult := range initialHummingbirdPresetMultipliers {
		cfg.NodeMultipliersMilli[node] = mult
		cfg.KnownNodes = append(cfg.KnownNodes, node)
	}
	sort.Strings(cfg.KnownNodes)
	m.settings.Subscriptions[subID] = cfg
}

// normalizeSubscriptionAccountingConfig keeps discovered node names and
// multiplier keys in the same canonical namespace. Mihomo node names commonly
// carry a leading country flag, while provider pricing tables omit it; storing
// both forms creates duplicate rows where one appears to have no multiplier.
func normalizeSubscriptionAccountingConfig(cfg *SubscriptionAccountingConfig) bool {
	if cfg == nil {
		return false
	}

	originalMultipliers := cfg.NodeMultipliersMilli
	normalizedMultipliers := make(map[string]uint32, len(originalMultipliers))
	for node, multiplier := range originalMultipliers {
		canonical := normalizeNodeName(node)
		if canonical != "" && !isBuiltinGroup(canonical) {
			normalizedMultipliers[canonical] = multiplier
		}
	}
	// If both decorated and canonical keys exist, the explicitly canonical key
	// wins deterministically rather than depending on Go map iteration order.
	for node, multiplier := range originalMultipliers {
		canonical := normalizeNodeName(node)
		if strings.TrimSpace(node) == canonical && canonical != "" && !isBuiltinGroup(canonical) {
			normalizedMultipliers[canonical] = multiplier
		}
	}

	known := make(map[string]struct{}, len(cfg.KnownNodes)+len(normalizedMultipliers))
	for _, node := range cfg.KnownNodes {
		canonical := normalizeNodeName(node)
		if canonical != "" && !isBuiltinGroup(canonical) {
			known[canonical] = struct{}{}
		}
	}
	for node := range normalizedMultipliers {
		known[node] = struct{}{}
	}
	normalizedKnown := make([]string, 0, len(known))
	for node := range known {
		normalizedKnown = append(normalizedKnown, node)
	}
	sort.Strings(normalizedKnown)

	changed := len(originalMultipliers) != len(normalizedMultipliers) || len(cfg.KnownNodes) != len(normalizedKnown)
	if !changed {
		for node, multiplier := range originalMultipliers {
			if normalizedMultipliers[node] != multiplier {
				changed = true
				break
			}
		}
	}
	if !changed {
		for i := range cfg.KnownNodes {
			if cfg.KnownNodes[i] != normalizedKnown[i] {
				changed = true
				break
			}
		}
	}

	cfg.NodeMultipliersMilli = normalizedMultipliers

	originalAutomatic := cfg.AutomaticNodeMultipliersMilli
	normalizedAutomatic := make(map[string]uint32, len(originalAutomatic))
	for node, multiplier := range originalAutomatic {
		canonical := normalizeNodeName(node)
		if canonical != "" && !isBuiltinGroup(canonical) {
			normalizedAutomatic[canonical] = multiplier
		}
	}
	for node, multiplier := range originalAutomatic {
		canonical := normalizeNodeName(node)
		if strings.TrimSpace(node) == canonical && canonical != "" && !isBuiltinGroup(canonical) {
			normalizedAutomatic[canonical] = multiplier
		}
	}
	if len(originalAutomatic) != len(normalizedAutomatic) {
		changed = true
	} else {
		for node, multiplier := range originalAutomatic {
			if normalizedAutomatic[node] != multiplier {
				changed = true
				break
			}
		}
	}
	cfg.AutomaticNodeMultipliersMilli = normalizedAutomatic
	for node := range normalizedAutomatic {
		known[node] = struct{}{}
	}
	if len(known) != len(normalizedKnown) {
		normalizedKnown = normalizedKnown[:0]
		for node := range known {
			normalizedKnown = append(normalizedKnown, node)
		}
		sort.Strings(normalizedKnown)
		changed = true
	}
	cfg.KnownNodes = normalizedKnown
	return changed
}

func (m *TrafficAccountingManager) persistSettingsLocked() error {
	m.settings.Version = trafficAccountingVersion
	encoded, err := json.MarshalIndent(m.settings, "", "  ")
	if err != nil {
		m.settingsPersistErr = fmt.Sprintf("设置持久化失败: %v", err)
		return err
	}
	err = atomicWriteFile(m.settingsPath, append(encoded, '\n'), 0600)
	if err != nil {
		m.settingsPersistErr = fmt.Sprintf("设置持久化失败: %v", err)
		return err
	}
	m.settingsPersistErr = ""
	return nil
}

func (m *TrafficAccountingManager) newEmptyLedger() TrafficLedger {
	nowStr := m.now().Format(time.RFC3339)
	return TrafficLedger{
		Version:         trafficAccountingVersion,
		StartedAt:       nowStr,
		UpdatedAt:       nowStr,
		Devices:         make(map[string]*DeviceAccounting),
		Subscriptions:   make(map[string]*SubscriptionAccounting),
		Nodes:           make(map[string]*NodeAccounting),
		Groups:          make(map[string]*GroupAccounting),
		Targets:         make(map[string]*TargetAccounting),
		DeviceRoutes:    make(map[string]*DeviceRouteAccounting),
		Hourly:          make(map[string]*TimeAccounting),
		Daily:           make(map[string]*TimeAccounting),
		PendingPricing:  make(map[string]*PendingNodePricing),
		PricingBackfill: pricingBackfillVersion,
	}
}

func (m *TrafficAccountingManager) initLedger() {
	_ = os.MkdirAll(m.dataDir, 0700)
	data, err := os.ReadFile(m.ledgerPath)
	if err != nil {
		m.ledger = m.newEmptyLedger()
		_ = m.persistLedgerLocked()
		return
	}

	var loaded TrafficLedger
	if err := json.Unmarshal(data, &loaded); err != nil {
		timestamp := m.now().Format("20060102-150405")
		corruptPath := fmt.Sprintf("%s.corrupt-%s", m.ledgerPath, timestamp)
		_ = os.Rename(m.ledgerPath, corruptPath)
		m.ledger = m.newEmptyLedger()
		m.ledger.CorruptWarning = fmt.Sprintf("账本文件已损坏并备份为 %s，已重建空账本", filepath.Base(corruptPath))
		_ = m.persistLedgerLocked()
		return
	}

	if loaded.Devices == nil {
		loaded.Devices = make(map[string]*DeviceAccounting)
	}
	if loaded.Subscriptions == nil {
		loaded.Subscriptions = make(map[string]*SubscriptionAccounting)
	}
	if loaded.Nodes == nil {
		loaded.Nodes = make(map[string]*NodeAccounting)
	} else {
		// Migrate old non-composite node keys if present
		migratedNodes := make(map[string]*NodeAccounting, len(loaded.Nodes))
		for k, v := range loaded.Nodes {
			if v == nil {
				continue
			}
			if k == otherKey || v.Node == otherKey {
				migratedNodes[otherKey] = v
				continue
			}
			if !strings.Contains(k, "\x00") {
				migratedNodes[nodeCompositeKey(v.SubscriptionID, v.Node)] = v
			} else {
				migratedNodes[k] = v
			}
		}
		loaded.Nodes = migratedNodes
	}
	if loaded.Groups == nil {
		loaded.Groups = make(map[string]*GroupAccounting)
	}
	if loaded.Targets == nil {
		loaded.Targets = make(map[string]*TargetAccounting)
	}
	if loaded.DeviceRoutes == nil {
		loaded.DeviceRoutes = make(map[string]*DeviceRouteAccounting)
	}
	if loaded.Hourly == nil {
		loaded.Hourly = make(map[string]*TimeAccounting)
	}
	if loaded.Daily == nil {
		loaded.Daily = make(map[string]*TimeAccounting)
	}
	if loaded.PendingPricing == nil {
		loaded.PendingPricing = make(map[string]*PendingNodePricing)
	}

	m.ledger = loaded
	m.hasBaseline = false // Restart requires establishing baseline before counting
}

func (m *TrafficAccountingManager) persistLedgerLocked() error {
	m.ledger.Version = trafficAccountingVersion
	m.ledger.UpdatedAt = m.now().Format(time.RFC3339)
	// The ledger is machine-owned and can become large. Compact JSON reduces
	// serialization work and atomic-write volume without changing its schema.
	encoded, err := json.Marshal(m.ledger)
	if err != nil {
		m.ledgerPersistErr = fmt.Sprintf("账本持久化失败: %v", err)
		return err
	}
	err = atomicWriteFile(m.ledgerPath, append(encoded, '\n'), 0600)
	if err != nil {
		m.ledgerPersistErr = fmt.Sprintf("账本持久化失败: %v", err)
		return err
	}
	m.lastSaveTime = m.now()
	m.bytesSinceLastSave = 0
	m.ledgerDirty = false
	m.ledgerPersistErr = ""
	return nil
}

func (m *TrafficAccountingManager) Start(ctx context.Context) {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	defer close(m.doneCh)

	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			_ = m.persistLedgerLocked()
			m.mu.Unlock()
			return
		case <-m.stopCh:
			m.mu.Lock()
			_ = m.persistLedgerLocked()
			m.mu.Unlock()
			return
		case <-ticker.C:
			pollCtx, cancel := context.WithTimeout(ctx, maxAccountingPollTimeout)
			_ = m.Poll(pollCtx)
			cancel()
		}
	}
}

func (m *TrafficAccountingManager) Stop() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
	<-m.doneCh
}

// Reset clears the local traffic accounting ledger and re-establishes current connection baselines.
func (m *TrafficAccountingManager) Reset() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.resetGeneration++
	m.currentCycleID++
	m.tracked = make(map[string]*trackedConnection)
	newLedger := m.newEmptyLedger()
	m.ledger = newLedger
	m.rateSamples = nil
	m.hasBaseline = false // Next poll will establish clean baseline without recounting old bytes

	err := m.persistLedgerLocked()
	return newLedger.StartedAt, err
}

// Poll fetches a single connections snapshot and performs incremental accounting.
func (m *TrafficAccountingManager) Poll(ctx context.Context) error {
	if m.fetchConnections == nil {
		return errors.New("no fetchConnections callback configured")
	}

	// Capture reset generation before fetching connections outside lock
	m.mu.Lock()
	pollGen := m.resetGeneration
	now := m.now()
	needKnownNodes := m.getKnownNodes != nil && (m.lastKnownNodesFetch.IsZero() || now.Sub(m.lastKnownNodesFetch) >= 60*time.Second)
	needClientHostnames := m.getClientHostnames != nil && (m.lastClientHostnamesFetch.IsZero() || now.Sub(m.lastClientHostnamesFetch) >= 30*time.Second)
	m.mu.Unlock()

	// Slow external callbacks fetched outside manager lock so they do not block Reset
	var fetchedKnownNodes []string
	if needKnownNodes {
		fetchedKnownNodes = m.getKnownNodes()
	}

	var fetchedClientHostnames map[string]string
	if needClientHostnames {
		fetchedClientHostnames = m.getClientHostnames()
	}

	resp, err := m.fetchConnections(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	now = m.now()
	nowStr := now.Format(time.RFC3339)

	if needKnownNodes {
		m.lastKnownNodesFetch = now
		m.cachedKnownNodes = fetchedKnownNodes
	}

	if needClientHostnames {
		m.lastClientHostnamesFetch = now
		if fetchedClientHostnames != nil {
			m.cachedClientHostnames = make(map[string]string, len(fetchedClientHostnames))
			for k, v := range fetchedClientHostnames {
				m.cachedClientHostnames[k] = v
			}
		}
	}

	if err != nil {
		m.ledger.PollFailCount = safeAddUint64(m.ledger.PollFailCount, 1)
		m.ledger.ConsecutiveFailures = safeAddUint64(m.ledger.ConsecutiveFailures, 1)
		m.hasBaseline = false // Offline recovery requires fresh baseline establishment
		return err
	}

	// Check if Reset completed while fetch was in-flight
	if m.resetGeneration != pollGen {
		return nil
	}

	if len(resp.Connections) > maxAccountingConnections {
		m.ledger.RejectedSnapshots = safeAddUint64(m.ledger.RejectedSnapshots, 1)
		m.ledger.ConsecutiveFailures = safeAddUint64(m.ledger.ConsecutiveFailures, 1)
		return fmt.Errorf("snapshot rejected: connection count %d exceeds %d", len(resp.Connections), maxAccountingConnections)
	}

	m.ledger.PollSuccessCount = safeAddUint64(m.ledger.PollSuccessCount, 1)
	m.ledger.ConsecutiveFailures = 0
	m.ledger.LastSuccessfulPollAt = nowStr

	if !m.settings.Enabled {
		// When disabled, keep tracked baselines aligned so enabling doesn't jump
		for _, c := range resp.Connections {
			if tc, ok := m.tracked[c.ID]; ok {
				tc.LastUpload = c.Upload
				tc.LastDownload = c.Download
			}
		}
		return nil
	}

	currentActiveSubID, currentActiveSubName := "", ""
	if m.getActiveSubscription != nil {
		currentActiveSubID, currentActiveSubName = m.getActiveSubscription()
	}

	// Synchronize candidate nodes from 家庭测速 (cached every 60s and
	// attributed only to the active subscription). Configured multiplier keys
	// are retained, while provider nodes that disappeared are pruned.
	settingsChanged := false
	if currentActiveSubID != "" && len(m.cachedKnownNodes) > 0 && needKnownNodes {
		settingsChanged = m.syncKnownNodesLocked(currentActiveSubID, m.cachedKnownNodes)
	}

	seenIDs := make(map[string]bool, len(resp.Connections))
	var roundProxyUp, roundProxyDown uint64
	isFirstPoll := !m.hasBaseline

	roundHostCache := make(map[string]string)
	resolveHost := func(ip string) string {
		if cached, ok := roundHostCache[ip]; ok {
			return cached
		}
		name := m.resolveHostLocked(ip)
		roundHostCache[ip] = name
		return name
	}

	for _, c := range resp.Connections {
		seenIDs[c.ID] = true
		routeKind, leafNode, proxyGroup, isSpeedtest := classifyRoute(c.Chains)
		var subID, subName string
		if routeKind == "proxy" && currentActiveSubID != "" {
			subID = currentActiveSubID
			subName = currentActiveSubName
		}
		devIP := c.Metadata.SourceIP
		devName := resolveHost(devIP)
		target := normalizeTarget(c.Metadata.Host, c.Metadata.DestinationIP)

		tc, exists := m.tracked[c.ID]
		if !exists {
			tc = &trackedConnection{
				ID:               c.ID,
				Start:            c.Start,
				LastUpload:       c.Upload,
				LastDownload:     c.Download,
				SubscriptionID:   subID,
				SubscriptionName: subName,
				LeafNode:         leafNode,
				ProxyGroup:       proxyGroup,
				RouteKind:        routeKind,
				IsSpeedtest:      isSpeedtest,
				SourceIP:         devIP,
				DeviceName:       devName,
				Target:           target,
				CycleID:          m.currentCycleID,
			}
			m.tracked[c.ID] = tc

			if isFirstPoll {
				// Startup, reset, or offline recovery first snapshot: baseline only, do not count
				continue
			}

			// New connection in normal running state: count initial bytes
			deltaUp := c.Upload
			deltaDown := c.Download
			m.accumulateDeltaLocked(tc, deltaUp, deltaDown, true, nowStr)

			if tc.RouteKind == "proxy" {
				roundProxyUp = safeAddUint64(roundProxyUp, deltaUp)
				roundProxyDown = safeAddUint64(roundProxyDown, deltaDown)
				if tc.SubscriptionID != "" && m.discoverNodeLocked(tc.SubscriptionID, tc.LeafNode) {
					settingsChanged = true
				}
			}
			continue
		}

		// Tracked connection exists
		if isFirstPoll {
			// Re-baseline on offline recovery
			tc.Start = c.Start
			tc.LastUpload = c.Upload
			tc.LastDownload = c.Download
			tc.SubscriptionID = subID
			tc.SubscriptionName = subName
			tc.LeafNode = leafNode
			tc.ProxyGroup = proxyGroup
			tc.RouteKind = routeKind
			tc.IsSpeedtest = isSpeedtest
			tc.SourceIP = devIP
			tc.DeviceName = devName
			tc.Target = target
			tc.CycleID = m.currentCycleID
			continue
		}

		// Check if connection ID was reused by a new connection without counter rollback
		reused := false
		if tc.Start != "" && c.Start != "" && tc.Start != c.Start {
			reused = true
		} else if tc.SourceIP != "" && devIP != "" && tc.SourceIP != devIP {
			reused = true
		} else if tc.Target != "" && target != "" && tc.Target != target {
			reused = true
		} else if tc.LeafNode != leafNode || tc.RouteKind != routeKind || tc.ProxyGroup != proxyGroup {
			reused = true
		}

		if reused {
			// Connection ID was reused: re-establish baseline and do not attribute old counts
			tc.Start = c.Start
			tc.LastUpload = c.Upload
			tc.LastDownload = c.Download
			tc.SubscriptionID = subID
			tc.SubscriptionName = subName
			tc.LeafNode = leafNode
			tc.ProxyGroup = proxyGroup
			tc.RouteKind = routeKind
			tc.IsSpeedtest = isSpeedtest
			tc.SourceIP = devIP
			tc.DeviceName = devName
			tc.Target = target
			tc.CycleID = m.currentCycleID
			continue
		}

		// Counter rollback / wrap detection
		if c.Upload < tc.LastUpload || c.Download < tc.LastDownload {
			tc.LastUpload = c.Upload
			tc.LastDownload = c.Download
			continue
		}

		deltaUp := c.Upload - tc.LastUpload
		deltaDown := c.Download - tc.LastDownload
		tc.LastUpload = c.Upload
		tc.LastDownload = c.Download

		incConn := false
		if tc.CycleID != m.currentCycleID {
			tc.CycleID = m.currentCycleID
			incConn = true
		}

		if devName != "" && devName != tc.DeviceName {
			tc.DeviceName = devName
		}

		if deltaUp > 0 || deltaDown > 0 || incConn {
			m.accumulateDeltaLocked(tc, deltaUp, deltaDown, incConn, nowStr)
			if tc.RouteKind == "proxy" {
				roundProxyUp = safeAddUint64(roundProxyUp, deltaUp)
				roundProxyDown = safeAddUint64(roundProxyDown, deltaDown)
				if tc.SubscriptionID != "" && m.discoverNodeLocked(tc.SubscriptionID, tc.LeafNode) {
					settingsChanged = true
				}
			}
		}
	}

	// Purge closed connections from memory
	for id := range m.tracked {
		if !seenIDs[id] {
			delete(m.tracked, id)
		}
	}

	m.hasBaseline = true

	// Persist settings if any new nodes were discovered
	if settingsChanged {
		_ = m.persistSettingsLocked()
	}

	// Record proxy sample for current rate calculation
	m.rateSamples = append(m.rateSamples, proxyRateSample{
		at:        now,
		upBytes:   roundProxyUp,
		downBytes: roundProxyDown,
	})
	m.pruneRateSamplesLocked(now)

	// Flush at most once per save interval and only after accounting data
	// changed. The previous size shortcut rewrote the whole ledger every second
	// on fast downloads, while idle polling rewrote it every ten seconds.
	if m.ledgerDirty && now.Sub(m.lastSaveTime) >= m.saveInterval {
		_ = m.persistLedgerLocked()
	}

	return nil
}

func (m *TrafficAccountingManager) pruneRateSamplesLocked(now time.Time) {
	windowStart := now.Add(-5 * time.Second)
	idx := 0
	for idx < len(m.rateSamples) && m.rateSamples[idx].at.Before(windowStart) {
		idx++
	}
	if idx > 0 {
		m.rateSamples = m.rateSamples[idx:]
	}
}

func (m *TrafficAccountingManager) calculateCurrentRateLocked() ProxyRate {
	if len(m.rateSamples) == 0 {
		return ProxyRate{}
	}
	var totalUp, totalDown uint64
	for _, s := range m.rateSamples {
		totalUp = safeAddUint64(totalUp, s.upBytes)
		totalDown = safeAddUint64(totalDown, s.downBytes)
	}
	duration := 5.0
	if len(m.rateSamples) > 1 {
		span := m.rateSamples[len(m.rateSamples)-1].at.Sub(m.rateSamples[0].at).Seconds()
		if span > 1.0 {
			duration = span
		}
	}
	return ProxyRate{
		UploadBps:   float64(totalUp) * 8.0 / duration,
		DownloadBps: float64(totalDown) * 8.0 / duration,
	}
}

// discoverNodeLocked attributes newly discovered nodes only to the specified subscription.
// Returns true if a new node was added to knownNodes, false otherwise.
func (m *TrafficAccountingManager) discoverNodeLocked(subID, nodeName string) bool {
	trimmed := normalizeNodeName(nodeName)
	if trimmed == "" || isBuiltinGroup(trimmed) || subID == "" {
		return false
	}

	cfg, ok := m.settings.Subscriptions[subID]
	if !ok || cfg == nil {
		cfg = &SubscriptionAccountingConfig{
			NodeMultipliersMilli: make(map[string]uint32),
			KnownNodes:           []string{},
		}
		m.settings.Subscriptions[subID] = cfg
	}
	for _, n := range cfg.KnownNodes {
		if n == trimmed {
			return false
		}
	}
	cfg.KnownNodes = append(cfg.KnownNodes, trimmed)
	sort.Strings(cfg.KnownNodes)
	return true
}

func (m *TrafficAccountingManager) syncKnownNodesLocked(subID string, nodes []string) bool {
	if subID == "" {
		return false
	}
	cfg, ok := m.settings.Subscriptions[subID]
	if !ok || cfg == nil {
		cfg = &SubscriptionAccountingConfig{
			NodeMultipliersMilli: make(map[string]uint32),
		}
		m.settings.Subscriptions[subID] = cfg
	}

	known := make(map[string]struct{}, len(nodes)+len(cfg.NodeMultipliersMilli)+len(cfg.AutomaticNodeMultipliersMilli))
	for _, node := range nodes {
		canonical := normalizeNodeName(node)
		if canonical != "" && !isBuiltinGroup(canonical) {
			known[canonical] = struct{}{}
		}
	}
	for node := range cfg.NodeMultipliersMilli {
		canonical := normalizeNodeName(node)
		if canonical != "" && !isBuiltinGroup(canonical) {
			known[canonical] = struct{}{}
		}
	}
	for node := range cfg.AutomaticNodeMultipliersMilli {
		canonical := normalizeNodeName(node)
		if canonical != "" && !isBuiltinGroup(canonical) {
			known[canonical] = struct{}{}
		}
	}

	next := make([]string, 0, len(known))
	for node := range known {
		next = append(next, node)
	}
	sort.Strings(next)
	if len(next) == len(cfg.KnownNodes) {
		equal := true
		for i := range next {
			if next[i] != cfg.KnownNodes[i] {
				equal = false
				break
			}
		}
		if equal {
			return false
		}
	}
	cfg.KnownNodes = next
	return true
}

func (m *TrafficAccountingManager) resolveMultiplierLocked(subID, nodeName string) (*uint32, string) {
	if subID == "" {
		return nil, "unpriced"
	}
	subCfg, ok := m.settings.Subscriptions[subID]
	if !ok || subCfg == nil {
		return nil, "unpriced"
	}

	exact := strings.TrimSpace(nodeName)
	if subCfg.AutomaticPricingEnabled {
		if mult, ok := subCfg.AutomaticNodeMultipliersMilli[exact]; ok && mult >= minMultiplierMilli && mult <= maxMultiplierMilli {
			return &mult, "automatic"
		}
		norm := normalizeNodeName(exact)
		if mult, ok := subCfg.AutomaticNodeMultipliersMilli[norm]; ok && mult >= minMultiplierMilli && mult <= maxMultiplierMilli {
			return &mult, "automatic"
		}
		return nil, "unpriced"
	}

	if subCfg.NodeMultipliersMilli != nil {
		if mult, ok := subCfg.NodeMultipliersMilli[exact]; ok && mult >= minMultiplierMilli && mult <= maxMultiplierMilli {
			return &mult, "node_override"
		}
		norm := normalizeNodeName(exact)
		if mult, ok := subCfg.NodeMultipliersMilli[norm]; ok && mult >= minMultiplierMilli && mult <= maxMultiplierMilli {
			return &mult, "node_override"
		}
	}

	if subCfg.DefaultMultiplierMilli != nil {
		d := *subCfg.DefaultMultiplierMilli
		if d >= minMultiplierMilli && d <= maxMultiplierMilli {
			return &d, "subscription_default"
		}
	}

	return nil, "unpriced"
}

func (m *TrafficAccountingManager) resolveHostLocked(ip string) string {
	trimmed := strings.TrimSpace(ip)
	if trimmed == "127.0.0.1" {
		return "路由器本机 / CPA"
	}
	if trimmed == "198.18.0.1" {
		return "Mihomo TUN 本机"
	}
	if trimmed == "" {
		return "路由器本机 / 来源未知"
	}
	if m.getClientHostnames != nil {
		if m.cachedClientHostnames != nil {
			if name, ok := m.cachedClientHostnames[trimmed]; ok {
				trimmedName := strings.TrimSpace(name)
				if trimmedName != "" && trimmedName != "未知设备" {
					return trimmedName
				}
			}
		}
		return trimmed
	}
	if m.lookupClientHost != nil {
		name := strings.TrimSpace(m.lookupClientHost(trimmed))
		if name != "" && name != "未知设备" {
			return name
		}
	}
	return trimmed
}

func nodeCompositeKey(subID, leafNode string) string {
	return subID + "\x00" + leafNode
}

func deviceRouteCompositeKey(sourceIP, routeKind, subscriptionID, leafNode, proxyGroup string) string {
	return sourceIP + "\x00" + routeKind + "\x00" + subscriptionID + "\x00" + leafNode + "\x00" + proxyGroup
}

func addDirectionalMap(values map[string]DirectionalBytes, key string, up, down uint64) {
	if key == "" {
		return
	}
	value := values[key]
	value.Add(up, down)
	values[key] = value
}

func newPendingNodePricing(subscriptionID, node string) *PendingNodePricing {
	return &PendingNodePricing{
		SubscriptionID: subscriptionID,
		Node:           normalizeNodeName(node),
		Devices:        make(map[string]DirectionalBytes),
		Subscriptions:  make(map[string]DirectionalBytes),
		Nodes:          make(map[string]DirectionalBytes),
		Groups:         make(map[string]DirectionalBytes),
		Targets:        make(map[string]DirectionalBytes),
		DeviceRoutes:   make(map[string]DirectionalBytes),
		Hourly:         make(map[string]DirectionalBytes),
		Daily:          make(map[string]DirectionalBytes),
	}
}

func (m *TrafficAccountingManager) recordPendingPricingLocked(
	tc *trackedConnection,
	up, down uint64,
	deviceKey, subscriptionKey, nodeKey, groupKey, targetKey, deviceRouteKey, hourKey, dayKey string,
) {
	if safeAddUint64(up, down) == 0 {
		return
	}
	if m.ledger.PendingPricing == nil {
		m.ledger.PendingPricing = make(map[string]*PendingNodePricing)
	}
	canonicalNode := normalizeNodeName(tc.LeafNode)
	key := nodeCompositeKey(tc.SubscriptionID, canonicalNode)
	pending := m.ledger.PendingPricing[key]
	if pending == nil {
		pending = newPendingNodePricing(tc.SubscriptionID, canonicalNode)
		m.ledger.PendingPricing[key] = pending
	}
	pending.Proxy.Add(up, down)
	if tc.SubscriptionID == "" {
		pending.Unassigned.Add(up, down)
	}
	if tc.IsSpeedtest {
		pending.Speedtest.Add(up, down)
	}
	addDirectionalMap(pending.Devices, deviceKey, up, down)
	addDirectionalMap(pending.Subscriptions, subscriptionKey, up, down)
	addDirectionalMap(pending.Nodes, nodeKey, up, down)
	addDirectionalMap(pending.Groups, groupKey, up, down)
	addDirectionalMap(pending.Targets, targetKey, up, down)
	addDirectionalMap(pending.DeviceRoutes, deviceRouteKey, up, down)
	addDirectionalMap(pending.Hourly, hourKey, up, down)
	addDirectionalMap(pending.Daily, dayKey, up, down)
}

func (m *TrafficAccountingManager) accumulateDeltaLocked(tc *trackedConnection, deltaUp, deltaDown uint64, incConn bool, at string) {
	rawDelta := safeAddUint64(deltaUp, deltaDown)
	m.bytesSinceLastSave = safeAddUint64(m.bytesSinceLastSave, rawDelta)
	m.ledgerDirty = true

	if m.ledger.Devices == nil {
		m.ledger.Devices = make(map[string]*DeviceAccounting)
	}
	if m.ledger.Subscriptions == nil {
		m.ledger.Subscriptions = make(map[string]*SubscriptionAccounting)
	}
	if m.ledger.Nodes == nil {
		m.ledger.Nodes = make(map[string]*NodeAccounting)
	}
	if m.ledger.Groups == nil {
		m.ledger.Groups = make(map[string]*GroupAccounting)
	}
	if m.ledger.Targets == nil {
		m.ledger.Targets = make(map[string]*TargetAccounting)
	}
	if m.ledger.DeviceRoutes == nil {
		m.ledger.DeviceRoutes = make(map[string]*DeviceRouteAccounting)
	}
	if m.ledger.Hourly == nil {
		m.ledger.Hourly = make(map[string]*TimeAccounting)
	}
	if m.ledger.Daily == nil {
		m.ledger.Daily = make(map[string]*TimeAccounting)
	}

	// 1. Total Observed
	m.ledger.TotalObserved.Add(deltaUp, deltaDown, 0, 0, 0, incConn, at)

	// 2. Device Dimension
	devKey := tc.SourceIP
	if devKey == "" {
		devKey = "unknown"
	}
	dev, ok := m.ledger.Devices[devKey]
	if !ok {
		if len(m.ledger.Devices) >= maxAccountingDevices {
			devKey = otherKey
			dev = m.ledger.Devices[otherKey]
			if dev == nil {
				dev = &DeviceAccounting{IP: otherKey, Hostname: "其他设备"}
				m.ledger.Devices[otherKey] = dev
			}
		} else {
			dev = &DeviceAccounting{IP: devKey, Hostname: tc.DeviceName}
			m.ledger.Devices[devKey] = dev
		}
	}
	if dev != nil {
		dev.Hostname = tc.DeviceName
	}

	// 3. Route Classification
	var wUp, wDown, unpriced uint64
	var multiplier *uint32
	var pricingStatus string
	var accountingSubKey, accountingNodeKey, accountingGroupKey string

	switch tc.RouteKind {
	case "direct":
		m.ledger.Direct.Add(deltaUp, deltaDown, 0, 0, 0, incConn, at)
		if dev != nil {
			dev.Add(deltaUp, deltaDown, 0, 0, 0, incConn, at)
		}
	case "reject":
		m.ledger.Reject.Add(deltaUp, deltaDown, 0, 0, 0, incConn, at)
		if dev != nil {
			dev.Add(deltaUp, deltaDown, 0, 0, 0, incConn, at)
		}
	case "proxy":
		mult, status := m.resolveMultiplierLocked(tc.SubscriptionID, tc.LeafNode)
		multiplier = mult
		pricingStatus = status

		if mult != nil {
			wUp = scaleBytes(deltaUp, *mult)
			wDown = scaleBytes(deltaDown, *mult)
			unpriced = 0
		} else {
			wUp = 0
			wDown = 0
			unpriced = rawDelta
		}

		m.ledger.Proxy.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
		if dev != nil {
			dev.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
		}

		if tc.SubscriptionID == "" {
			m.ledger.UnassignedProxy.Add(deltaUp, deltaDown, 0, 0, unpriced, incConn, at)
		} else {
			subKey := tc.SubscriptionID
			sub, ok := m.ledger.Subscriptions[subKey]
			if !ok {
				if len(m.ledger.Subscriptions) >= maxAccountingSubscriptions {
					subKey = otherKey
					sub = m.ledger.Subscriptions[otherKey]
					if sub == nil {
						sub = &SubscriptionAccounting{ID: otherKey, Name: "其他订阅"}
						m.ledger.Subscriptions[otherKey] = sub
					}
				} else {
					sub = &SubscriptionAccounting{ID: subKey, Name: tc.SubscriptionName}
					m.ledger.Subscriptions[subKey] = sub
				}
			}
			if sub != nil {
				accountingSubKey = subKey
				sub.Name = tc.SubscriptionName
				sub.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
			}
		}

		// Node Dimension with compound internal key (subscriptionID + leafNode)
		leaf := tc.LeafNode
		if leaf == "" {
			leaf = "unknown"
		}
		nodeKey := nodeCompositeKey(tc.SubscriptionID, leaf)
		node, ok := m.ledger.Nodes[nodeKey]
		if !ok {
			if len(m.ledger.Nodes) >= maxAccountingNodes {
				nodeKey = otherKey
				node = m.ledger.Nodes[otherKey]
				if node == nil {
					node = &NodeAccounting{Node: otherKey, PricingStatus: "unpriced"}
					m.ledger.Nodes[otherKey] = node
				}
			} else {
				node = &NodeAccounting{
					Node:             leaf,
					SubscriptionID:   tc.SubscriptionID,
					SubscriptionName: tc.SubscriptionName,
					MultiplierMilli:  multiplier,
					PricingStatus:    pricingStatus,
				}
				m.ledger.Nodes[nodeKey] = node
			}
		} else if nodeKey != otherKey {
			node.SubscriptionID = tc.SubscriptionID
			node.SubscriptionName = tc.SubscriptionName
			node.MultiplierMilli = multiplier
			node.PricingStatus = pricingStatus
		}
		if node != nil {
			accountingNodeKey = nodeKey
			node.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
		}

		// Group Dimension
		if tc.ProxyGroup != "" {
			grpKey := tc.ProxyGroup
			grp, ok := m.ledger.Groups[grpKey]
			if !ok {
				if len(m.ledger.Groups) >= maxAccountingGroups {
					grpKey = otherKey
					grp = m.ledger.Groups[otherKey]
					if grp == nil {
						grp = &GroupAccounting{Group: otherKey}
						m.ledger.Groups[otherKey] = grp
					}
				} else {
					grp = &GroupAccounting{Group: grpKey}
					m.ledger.Groups[grpKey] = grp
				}
			}
			if grp != nil {
				accountingGroupKey = grpKey
				grp.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
			}
		}

		// Speedtest Dimension
		if tc.IsSpeedtest {
			m.ledger.Speedtest.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
		}
	}

	// 4. Device Route Dimension (cross-dimension of source device and routing destination)
	var drSubID, drSubName, drNode, drGroup string
	if tc.RouteKind == "proxy" {
		drSubID = tc.SubscriptionID
		drSubName = tc.SubscriptionName
		drNode = tc.LeafNode
		drGroup = tc.ProxyGroup
	}

	drKey := deviceRouteCompositeKey(devKey, tc.RouteKind, drSubID, drNode, drGroup)
	dr, ok := m.ledger.DeviceRoutes[drKey]
	if !ok {
		if len(m.ledger.DeviceRoutes) >= maxAccountingDeviceRoutes {
			drKey = otherKey
			dr = m.ledger.DeviceRoutes[otherKey]
			if dr == nil {
				dr = &DeviceRouteAccounting{
					IP:        otherKey,
					Hostname:  "其他终端路由",
					RouteKind: otherKey,
				}
				m.ledger.DeviceRoutes[otherKey] = dr
			}
		} else {
			dr = &DeviceRouteAccounting{
				IP:               devKey,
				Hostname:         tc.DeviceName,
				RouteKind:        tc.RouteKind,
				SubscriptionID:   drSubID,
				SubscriptionName: drSubName,
				Node:             drNode,
				Group:            drGroup,
			}
			m.ledger.DeviceRoutes[drKey] = dr
		}
	} else if drKey != otherKey {
		if tc.DeviceName != "" {
			dr.Hostname = tc.DeviceName
		}
		if drSubName != "" {
			dr.SubscriptionName = drSubName
		}
	}
	if dr != nil {
		dr.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
	}

	// 5. Target Dimension
	targetKey := tc.Target
	if targetKey == "" {
		targetKey = "unknown"
	}
	tgt, ok := m.ledger.Targets[targetKey]
	if !ok {
		if len(m.ledger.Targets) >= maxAccountingTargets {
			targetKey = otherKey
			tgt = m.ledger.Targets[otherKey]
			if tgt == nil {
				tgt = &TargetAccounting{Target: otherKey}
				m.ledger.Targets[otherKey] = tgt
			}
		} else {
			tgt = &TargetAccounting{Target: targetKey}
			m.ledger.Targets[targetKey] = tgt
		}
	}
	if tgt != nil {
		tgt.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
	}

	// 5. Time Buckets (Hourly: strictly <= 24 hours, Daily: strictly <= 90 days)
	now := m.now()
	hourKey := now.Truncate(time.Hour).Format(time.RFC3339)
	dateKey := now.Format("2006-01-02")

	hr, ok := m.ledger.Hourly[hourKey]
	if !ok {
		hr = &TimeAccounting{Time: hourKey}
		m.ledger.Hourly[hourKey] = hr
		m.pruneTimeBucketsLocked(now)
	}
	if hr != nil {
		hr.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
	}

	day, ok := m.ledger.Daily[dateKey]
	if !ok {
		day = &TimeAccounting{Time: dateKey}
		m.ledger.Daily[dateKey] = day
		m.pruneTimeBucketsLocked(now)
	}
	if day != nil {
		day.Add(deltaUp, deltaDown, wUp, wDown, unpriced, incConn, at)
	}

	if unpriced > 0 {
		m.recordPendingPricingLocked(
			tc,
			deltaUp,
			deltaDown,
			devKey,
			accountingSubKey,
			accountingNodeKey,
			accountingGroupKey,
			targetKey,
			drKey,
			hourKey,
			dateKey,
		)
	}
}

func (m *TrafficAccountingManager) pruneTimeBucketsLocked(now time.Time) {
	if m.ledger.Hourly == nil {
		m.ledger.Hourly = make(map[string]*TimeAccounting)
	}
	if m.ledger.Daily == nil {
		m.ledger.Daily = make(map[string]*TimeAccounting)
	}

	// Keep at most 24 hours: current hour + 23 previous hours
	hourCutoff := now.Add(-23 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
	for k := range m.ledger.Hourly {
		if k < hourCutoff {
			delete(m.ledger.Hourly, k)
		}
	}
	if len(m.ledger.Hourly) > 24 {
		keys := make([]string, 0, len(m.ledger.Hourly))
		for k := range m.ledger.Hourly {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for len(keys) > 24 {
			delete(m.ledger.Hourly, keys[0])
			keys = keys[1:]
		}
	}

	// Keep at most 90 days: current date + 89 previous days
	dateCutoff := now.AddDate(0, 0, -89).Format("2006-01-02")
	for k := range m.ledger.Daily {
		if k < dateCutoff {
			delete(m.ledger.Daily, k)
		}
	}
	if len(m.ledger.Daily) > 90 {
		keys := make([]string, 0, len(m.ledger.Daily))
		for k := range m.ledger.Daily {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for len(keys) > 90 {
			delete(m.ledger.Daily, keys[0])
			keys = keys[1:]
		}
	}
}

func applyPendingBytes(stats *TrafficStats, pending DirectionalBytes, multiplier uint32) bool {
	if stats == nil {
		return false
	}
	raw := safeAddUint64(pending.UploadBytes, pending.DownloadBytes)
	if raw == 0 {
		return false
	}
	stats.WeightedUploadBytes = safeAddUint64(
		stats.WeightedUploadBytes,
		scaleBytes(pending.UploadBytes, multiplier),
	)
	stats.WeightedDownloadBytes = safeAddUint64(
		stats.WeightedDownloadBytes,
		scaleBytes(pending.DownloadBytes, multiplier),
	)
	stats.WeightedBytes = safeAddUint64(stats.WeightedUploadBytes, stats.WeightedDownloadBytes)
	if raw >= stats.UnpricedRawBytes {
		stats.UnpricedRawBytes = 0
	} else {
		stats.UnpricedRawBytes -= raw
	}
	return true
}

func (m *TrafficAccountingManager) applyPendingNodePricingLocked(pending *PendingNodePricing, multiplier uint32) {
	if pending == nil {
		return
	}
	applyPendingBytes(&m.ledger.Proxy, pending.Proxy, multiplier)
	applyPendingBytes(&m.ledger.UnassignedProxy, pending.Unassigned, multiplier)
	applyPendingBytes(&m.ledger.Speedtest, pending.Speedtest, multiplier)
	for key, value := range pending.Devices {
		if item := m.ledger.Devices[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
	for key, value := range pending.Subscriptions {
		if item := m.ledger.Subscriptions[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
	for key, value := range pending.Nodes {
		if item := m.ledger.Nodes[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
	for key, value := range pending.Groups {
		if item := m.ledger.Groups[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
	for key, value := range pending.Targets {
		if item := m.ledger.Targets[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
	for key, value := range pending.DeviceRoutes {
		if item := m.ledger.DeviceRoutes[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
	for key, value := range pending.Hourly {
		if item := m.ledger.Hourly[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
	for key, value := range pending.Daily {
		if item := m.ledger.Daily[key]; item != nil {
			applyPendingBytes(&item.TrafficStats, value, multiplier)
		}
	}
}

// repriceResolvedPendingLocked replays every exact pending journal whose node
// now has a multiplier. Already-priced traffic is never recalculated when a
// multiplier changes.
func (m *TrafficAccountingManager) repriceResolvedPendingLocked() bool {
	changed := false
	for key, pending := range m.ledger.PendingPricing {
		if pending == nil {
			delete(m.ledger.PendingPricing, key)
			changed = true
			continue
		}
		multiplier, _ := m.resolveMultiplierLocked(pending.SubscriptionID, pending.Node)
		if multiplier == nil {
			continue
		}
		m.applyPendingNodePricingLocked(pending, *multiplier)
		delete(m.ledger.PendingPricing, key)
		changed = true
	}
	if changed {
		m.ledgerDirty = true
	}
	return changed
}

func mulDivUint64(value, numerator, denominator uint64) uint64 {
	if value == 0 || numerator == 0 || denominator == 0 {
		return 0
	}
	hi, lo := bits.Mul64(value, numerator)
	if hi >= denominator {
		return math.MaxUint64
	}
	quotient, _ := bits.Div64(hi, lo, denominator)
	return quotient
}

func splitLegacyUnpriced(stats *TrafficStats) DirectionalBytes {
	if stats == nil || stats.UnpricedRawBytes == 0 {
		return DirectionalBytes{}
	}
	raw := stats.UnpricedRawBytes
	if stats.RawBytes == 0 {
		return DirectionalBytes{DownloadBytes: raw}
	}
	up := mulDivUint64(raw, stats.UploadBytes, stats.RawBytes)
	if up > raw {
		up = raw
	}
	return DirectionalBytes{UploadBytes: up, DownloadBytes: raw - up}
}

func applyLegacyMultiplier(stats *TrafficStats, multiplier uint32) uint64 {
	pending := splitLegacyUnpriced(stats)
	if !applyPendingBytes(stats, pending, multiplier) {
		return 0
	}
	return safeAddUint64(
		scaleBytes(pending.UploadBytes, multiplier),
		scaleBytes(pending.DownloadBytes, multiplier),
	)
}

func applyLegacyRatio(stats *TrafficStats, weightedNumerator, rawDenominator uint64) {
	if stats == nil || stats.UnpricedRawBytes == 0 || rawDenominator == 0 {
		return
	}
	raw := stats.UnpricedRawBytes
	weighted := mulDivUint64(raw, weightedNumerator, rawDenominator)
	weightedUp := uint64(0)
	if stats.RawBytes > 0 {
		weightedUp = mulDivUint64(weighted, stats.UploadBytes, stats.RawBytes)
		if weightedUp > weighted {
			weightedUp = weighted
		}
	}
	stats.WeightedUploadBytes = safeAddUint64(stats.WeightedUploadBytes, weightedUp)
	stats.WeightedDownloadBytes = safeAddUint64(
		stats.WeightedDownloadBytes,
		weighted-weightedUp,
	)
	stats.WeightedBytes = safeAddUint64(stats.WeightedUploadBytes, stats.WeightedDownloadBytes)
	stats.UnpricedRawBytes = 0
}

// reconcileLegacyUnpricedLocked handles ledgers written before the exact
// pending journal existed. It runs only when every historical unpriced node is
// now resolvable; node totals use their exact current multipliers, and the same
// resulting blended rate clears the legacy residual from aggregate dimensions.
func (m *TrafficAccountingManager) reconcileLegacyUnpricedLocked() bool {
	if m.ledger.PricingBackfill >= pricingBackfillVersion {
		return false
	}
	if m.ledger.Proxy.UnpricedRawBytes == 0 {
		m.ledger.PricingBackfill = pricingBackfillVersion
		m.ledgerDirty = true
		return true
	}
	if m.ledger.UnassignedProxy.UnpricedRawBytes > 0 {
		return false
	}

	totalRaw := uint64(0)
	for _, node := range m.ledger.Nodes {
		if node == nil || node.UnpricedRawBytes == 0 {
			continue
		}
		multiplier, _ := m.resolveMultiplierLocked(node.SubscriptionID, node.Node)
		if multiplier == nil {
			return false
		}
		totalRaw = safeAddUint64(totalRaw, node.UnpricedRawBytes)
	}
	if totalRaw == 0 || totalRaw != m.ledger.Proxy.UnpricedRawBytes {
		return false
	}

	totalWeighted := uint64(0)
	for _, node := range m.ledger.Nodes {
		if node == nil || node.UnpricedRawBytes == 0 {
			continue
		}
		multiplier, _ := m.resolveMultiplierLocked(node.SubscriptionID, node.Node)
		totalWeighted = safeAddUint64(
			totalWeighted,
			applyLegacyMultiplier(&node.TrafficStats, *multiplier),
		)
	}

	applyLegacyRatio(&m.ledger.Proxy, totalWeighted, totalRaw)
	applyLegacyRatio(&m.ledger.Speedtest, totalWeighted, totalRaw)
	for _, item := range m.ledger.Devices {
		if item != nil {
			applyLegacyRatio(&item.TrafficStats, totalWeighted, totalRaw)
		}
	}
	for _, item := range m.ledger.Subscriptions {
		if item != nil {
			applyLegacyRatio(&item.TrafficStats, totalWeighted, totalRaw)
		}
	}
	for _, item := range m.ledger.Groups {
		if item != nil {
			applyLegacyRatio(&item.TrafficStats, totalWeighted, totalRaw)
		}
	}
	for _, item := range m.ledger.Targets {
		if item != nil {
			applyLegacyRatio(&item.TrafficStats, totalWeighted, totalRaw)
		}
	}
	for _, item := range m.ledger.DeviceRoutes {
		if item != nil {
			applyLegacyRatio(&item.TrafficStats, totalWeighted, totalRaw)
		}
	}
	for _, item := range m.ledger.Hourly {
		if item != nil {
			applyLegacyRatio(&item.TrafficStats, totalWeighted, totalRaw)
		}
	}
	for _, item := range m.ledger.Daily {
		if item != nil {
			applyLegacyRatio(&item.TrafficStats, totalWeighted, totalRaw)
		}
	}
	m.ledger.PricingBackfill = pricingBackfillVersion
	m.ledgerDirty = true
	return true
}

// GetSnapshot returns the current stats with bounded rankings (top 200).
func (m *TrafficAccountingManager) GetSnapshot() TrafficAccountingResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	var persistWarns []string
	if m.ledgerPersistErr != "" {
		persistWarns = append(persistWarns, m.ledgerPersistErr)
	}
	if m.settingsPersistErr != "" {
		persistWarns = append(persistWarns, m.settingsPersistErr)
	}
	persistErrStr := strings.Join(persistWarns, "; ")

	var allWarnings []string
	if m.ledger.CorruptWarning != "" {
		allWarnings = append(allWarnings, m.ledger.CorruptWarning)
	}
	if len(persistWarns) > 0 {
		allWarnings = append(allWarnings, persistWarns...)
	}
	warningStr := strings.Join(allWarnings, "; ")

	res := TrafficAccountingResponse{
		Online:               m.ledger.PollSuccessCount > 0 && m.ledger.ConsecutiveFailures == 0,
		StartedAt:            m.ledger.StartedAt,
		UpdatedAt:            m.ledger.UpdatedAt,
		LastSuccessfulPollAt: m.ledger.LastSuccessfulPollAt,
		PollSuccessCount:     m.ledger.PollSuccessCount,
		PollFailCount:        m.ledger.PollFailCount,
		ConsecutiveFailures:  m.ledger.ConsecutiveFailures,
		RejectedSnapshots:    m.ledger.RejectedSnapshots,
		Warning:              warningStr,
		PersistenceError:     persistErrStr,
		CurrentProxyRate:     m.calculateCurrentRateLocked(),
		TotalObserved:        m.ledger.TotalObserved,
		Direct:               m.ledger.Direct,
		Proxy:                m.ledger.Proxy,
		Reject:               m.ledger.Reject,
		UnassignedProxy:      m.ledger.UnassignedProxy,
		Speedtest:            m.ledger.Speedtest,
		Devices:              boundDevicesRanking(m.ledger.Devices),
		Subscriptions:        boundSubscriptionsRanking(m.ledger.Subscriptions),
		Nodes:                m.boundNodesRankingLocked(),
		Groups:               boundGroupsRanking(m.ledger.Groups),
		Targets:              boundTargetsRanking(m.ledger.Targets),
		DeviceRoutes:         boundDeviceRoutesRanking(m.ledger.DeviceRoutes),
		Hourly:               boundHourlyBuckets(m.ledger.Hourly),
		Daily:                boundDailyBuckets(m.ledger.Daily),
	}

	return res
}

// boundNodesRankingLocked reports the multiplier that is configured now.
// Persisted node metadata reflects the multiplier used by the most recent
// traffic increment and would otherwise remain stale after a settings update.
func (m *TrafficAccountingManager) boundNodesRankingLocked() []NodeAccounting {
	nodes := boundNodesRanking(m.ledger.Nodes)
	for i := range nodes {
		if nodes[i].Node == otherKey {
			continue
		}
		nodes[i].MultiplierMilli, nodes[i].PricingStatus = m.resolveMultiplierLocked(
			nodes[i].SubscriptionID,
			nodes[i].Node,
		)
	}
	return nodes
}

func boundDevicesRanking(m map[string]*DeviceAccounting) []DeviceAccounting {
	if m == nil {
		return nil
	}
	var list []DeviceAccounting
	var other *DeviceAccounting
	for k, v := range m {
		if v == nil {
			continue
		}
		if k == otherKey || v.IP == otherKey {
			c := *v
			other = &c
			continue
		}
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RawBytes != list[j].RawBytes {
			return list[i].RawBytes > list[j].RawBytes
		}
		return list[i].IP < list[j].IP
	})
	if len(list) > maxAPIRankingRows {
		overflow := list[maxAPIRankingRows:]
		list = list[:maxAPIRankingRows]
		if other == nil {
			other = &DeviceAccounting{IP: otherKey, Hostname: "其他设备"}
		}
		for _, it := range overflow {
			other.Merge(it.TrafficStats)
		}
	}
	if other != nil && other.RawBytes > 0 {
		list = append(list, *other)
	}
	return list
}

func boundSubscriptionsRanking(m map[string]*SubscriptionAccounting) []SubscriptionAccounting {
	if m == nil {
		return nil
	}
	var list []SubscriptionAccounting
	var other *SubscriptionAccounting
	for k, v := range m {
		if v == nil {
			continue
		}
		if k == otherKey || v.ID == otherKey {
			c := *v
			other = &c
			continue
		}
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RawBytes != list[j].RawBytes {
			return list[i].RawBytes > list[j].RawBytes
		}
		return list[i].ID < list[j].ID
	})
	if len(list) > maxAPIRankingRows {
		overflow := list[maxAPIRankingRows:]
		list = list[:maxAPIRankingRows]
		if other == nil {
			other = &SubscriptionAccounting{ID: otherKey, Name: "其他订阅"}
		}
		for _, it := range overflow {
			other.Merge(it.TrafficStats)
		}
	}
	if other != nil && other.RawBytes > 0 {
		list = append(list, *other)
	}
	return list
}

func boundNodesRanking(m map[string]*NodeAccounting) []NodeAccounting {
	if m == nil {
		return nil
	}
	var list []NodeAccounting
	var other *NodeAccounting
	for k, v := range m {
		if v == nil {
			continue
		}
		if k == otherKey || v.Node == otherKey {
			c := *v
			other = &c
			continue
		}
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RawBytes != list[j].RawBytes {
			return list[i].RawBytes > list[j].RawBytes
		}
		if list[i].SubscriptionID != list[j].SubscriptionID {
			return list[i].SubscriptionID < list[j].SubscriptionID
		}
		return list[i].Node < list[j].Node
	})
	if len(list) > maxAPIRankingRows {
		overflow := list[maxAPIRankingRows:]
		list = list[:maxAPIRankingRows]
		if other == nil {
			other = &NodeAccounting{Node: otherKey, PricingStatus: "unpriced"}
		}
		for _, it := range overflow {
			other.Merge(it.TrafficStats)
		}
	}
	if other != nil && other.RawBytes > 0 {
		list = append(list, *other)
	}
	return list
}

func boundGroupsRanking(m map[string]*GroupAccounting) []GroupAccounting {
	if m == nil {
		return nil
	}
	var list []GroupAccounting
	var other *GroupAccounting
	for k, v := range m {
		if v == nil {
			continue
		}
		if k == otherKey || v.Group == otherKey {
			c := *v
			other = &c
			continue
		}
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RawBytes != list[j].RawBytes {
			return list[i].RawBytes > list[j].RawBytes
		}
		return list[i].Group < list[j].Group
	})
	if len(list) > maxAPIRankingRows {
		overflow := list[maxAPIRankingRows:]
		list = list[:maxAPIRankingRows]
		if other == nil {
			other = &GroupAccounting{Group: otherKey}
		}
		for _, it := range overflow {
			other.Merge(it.TrafficStats)
		}
	}
	if other != nil && other.RawBytes > 0 {
		list = append(list, *other)
	}
	return list
}

func boundTargetsRanking(m map[string]*TargetAccounting) []TargetAccounting {
	if m == nil {
		return nil
	}
	var list []TargetAccounting
	var other *TargetAccounting
	for k, v := range m {
		if v == nil {
			continue
		}
		if k == otherKey || v.Target == otherKey {
			c := *v
			other = &c
			continue
		}
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RawBytes != list[j].RawBytes {
			return list[i].RawBytes > list[j].RawBytes
		}
		return list[i].Target < list[j].Target
	})
	if len(list) > maxAPIRankingRows {
		overflow := list[maxAPIRankingRows:]
		list = list[:maxAPIRankingRows]
		if other == nil {
			other = &TargetAccounting{Target: otherKey}
		}
		for _, it := range overflow {
			other.Merge(it.TrafficStats)
		}
	}
	if other != nil && other.RawBytes > 0 {
		list = append(list, *other)
	}
	return list
}

func boundDeviceRoutesRanking(m map[string]*DeviceRouteAccounting) []DeviceRouteAccounting {
	if m == nil {
		return nil
	}
	var list []DeviceRouteAccounting
	var other *DeviceRouteAccounting
	for k, v := range m {
		if v == nil {
			continue
		}
		if k == otherKey || v.IP == otherKey {
			c := *v
			other = &c
			continue
		}
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RawBytes != list[j].RawBytes {
			return list[i].RawBytes > list[j].RawBytes
		}
		if list[i].IP != list[j].IP {
			return list[i].IP < list[j].IP
		}
		if list[i].RouteKind != list[j].RouteKind {
			return list[i].RouteKind < list[j].RouteKind
		}
		if list[i].SubscriptionID != list[j].SubscriptionID {
			return list[i].SubscriptionID < list[j].SubscriptionID
		}
		if list[i].Node != list[j].Node {
			return list[i].Node < list[j].Node
		}
		return list[i].Group < list[j].Group
	})
	if len(list) > maxAPIRankingRows {
		overflow := list[maxAPIRankingRows:]
		list = list[:maxAPIRankingRows]
		if other == nil {
			other = &DeviceRouteAccounting{
				IP:        otherKey,
				Hostname:  "其他终端路由",
				RouteKind: otherKey,
			}
		}
		for _, it := range overflow {
			other.Merge(it.TrafficStats)
		}
	}
	if other != nil && other.RawBytes > 0 {
		list = append(list, *other)
	}
	return list
}

func boundHourlyBuckets(m map[string]*TimeAccounting) []TimeAccounting {
	if m == nil {
		return nil
	}
	var list []TimeAccounting
	for _, v := range m {
		if v != nil {
			list = append(list, *v)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Time < list[j].Time
	})
	if len(list) > 24 {
		list = list[len(list)-24:]
	}
	return list
}

func boundDailyBuckets(m map[string]*TimeAccounting) []TimeAccounting {
	if m == nil {
		return nil
	}
	var list []TimeAccounting
	for _, v := range m {
		if v != nil {
			list = append(list, *v)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Time < list[j].Time
	})
	if len(list) > 90 {
		list = list[len(list)-90:]
	}
	return list
}

func (m *TrafficAccountingManager) getSettingsLocked() TrafficAccountingSettings {
	res := TrafficAccountingSettings{
		Version:       m.settings.Version,
		Enabled:       m.settings.Enabled,
		Subscriptions: make(map[string]*SubscriptionAccountingConfig, len(m.settings.Subscriptions)),
	}

	for id, cfg := range m.settings.Subscriptions {
		if cfg == nil {
			continue
		}
		c := &SubscriptionAccountingConfig{
			KnownNodes: make([]string, len(cfg.KnownNodes)),
		}
		copy(c.KnownNodes, cfg.KnownNodes)
		if cfg.DefaultMultiplierMilli != nil {
			d := *cfg.DefaultMultiplierMilli
			c.DefaultMultiplierMilli = &d
		}
		if cfg.NodeMultipliersMilli != nil {
			c.NodeMultipliersMilli = make(map[string]uint32, len(cfg.NodeMultipliersMilli))
			for k, v := range cfg.NodeMultipliersMilli {
				c.NodeMultipliersMilli[k] = v
			}
		}
		if cfg.AutomaticNodeMultipliersMilli != nil {
			c.AutomaticNodeMultipliersMilli = make(map[string]uint32, len(cfg.AutomaticNodeMultipliersMilli))
			for k, v := range cfg.AutomaticNodeMultipliersMilli {
				c.AutomaticNodeMultipliersMilli[k] = v
			}
		}
		c.AutomaticPricingEnabled = cfg.AutomaticPricingEnabled
		c.AutomaticPricingUpdatedAt = cfg.AutomaticPricingUpdatedAt
		res.Subscriptions[id] = c
	}

	return res
}

// UpdateAutomaticMultipliers atomically replaces one subscription's last-good
// provider pricing snapshot. Existing manually configured node multipliers keep
// precedence, and newly resolvable pending traffic is priced immediately.
func (m *TrafficAccountingManager) UpdateAutomaticMultipliers(subID string, values map[string]uint32, updatedAt string) (*TrafficAccountingSettingsResponse, error) {
	canonical := make(map[string]uint32, len(values))
	for node, multiplier := range values {
		name := normalizeNodeName(node)
		if name == "" || isBuiltinGroup(name) {
			continue
		}
		if multiplier < minMultiplierMilli || multiplier > maxMultiplierMilli {
			return nil, fmt.Errorf("node %q multiplier is outside 0.1x-100x", name)
		}
		if previous, exists := canonical[name]; exists && previous != multiplier {
			return nil, fmt.Errorf("node %q has conflicting automatic multipliers", name)
		}
		canonical[name] = multiplier
	}
	if len(canonical) == 0 {
		return nil, errors.New("automatic pricing returned no valid nodes")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(subID) == "" || len(subID) > 64 {
		return nil, errors.New("invalid subscription ID")
	}

	previous := m.getSettingsLocked()
	cfg := m.settings.Subscriptions[subID]
	if cfg == nil {
		cfg = &SubscriptionAccountingConfig{NodeMultipliersMilli: make(map[string]uint32)}
		m.settings.Subscriptions[subID] = cfg
	}
	cfg.AutomaticNodeMultipliersMilli = canonical
	cfg.AutomaticPricingEnabled = true
	cfg.AutomaticPricingUpdatedAt = strings.TrimSpace(updatedAt)
	// An enabled provider source is authoritative. Keeping old manual values
	// would silently mask future provider rate changes, including removals.
	cfg.NodeMultipliersMilli = make(map[string]uint32)
	cfg.DefaultMultiplierMilli = nil
	normalizeSubscriptionAccountingConfig(cfg)
	if err := m.persistSettingsLocked(); err != nil {
		m.settings = previous
		return nil, fmt.Errorf("save automatic pricing: %w", err)
	}

	pricingChanged := m.repriceResolvedPendingLocked()
	if m.reconcileLegacyUnpricedLocked() {
		pricingChanged = true
	}
	if pricingChanged {
		if err := m.persistLedgerLocked(); err != nil {
			return nil, fmt.Errorf("automatic pricing applied but pending traffic persistence failed: %w", err)
		}
	}

	result := m.getSettingsResponseLocked()
	return &result, nil
}

// DisableAutomaticPricing stops using the cached provider snapshot without
// deleting it. A later successful synchronization replaces and re-enables it.
func (m *TrafficAccountingManager) DisableAutomaticPricing(subID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.settings.Subscriptions[subID]
	if cfg == nil || !cfg.AutomaticPricingEnabled {
		return nil
	}
	cfg.AutomaticPricingEnabled = false
	if err := m.persistSettingsLocked(); err != nil {
		cfg.AutomaticPricingEnabled = true
		return fmt.Errorf("disable automatic pricing: %w", err)
	}
	return nil
}

func (m *TrafficAccountingManager) getUnpricedNodesLocked() []UnpricedNodeInfo {
	// Old ledgers can contain differently decorated spellings of the same
	// node. Merge them into the canonical namespace used by multiplier keys.
	byNode := make(map[string]*UnpricedNodeInfo, len(m.ledger.PendingPricing))
	for _, pending := range m.ledger.PendingPricing {
		if pending == nil {
			continue
		}
		node := normalizeNodeName(pending.Node)
		rawBytes := safeAddUint64(pending.Proxy.UploadBytes, pending.Proxy.DownloadBytes)
		if node == "" || rawBytes == 0 {
			continue
		}

		key := nodeCompositeKey(pending.SubscriptionID, node)
		item := byNode[key]
		if item == nil {
			item = &UnpricedNodeInfo{
				SubscriptionID: pending.SubscriptionID,
				Node:           node,
				Configurable:   pending.SubscriptionID != "",
			}
			byNode[key] = item
		}
		item.UnpricedRawBytes = safeAddUint64(item.UnpricedRawBytes, rawBytes)

		if sub := m.ledger.Subscriptions[pending.SubscriptionID]; sub != nil {
			if item.SubscriptionName == "" {
				item.SubscriptionName = sub.Name
			}
			if sub.LastSeenAt > item.LastSeenAt {
				item.LastSeenAt = sub.LastSeenAt
			}
		}
		if ledgerNode := m.ledger.Nodes[key]; ledgerNode != nil {
			if item.SubscriptionName == "" {
				item.SubscriptionName = ledgerNode.SubscriptionName
			}
			if ledgerNode.LastSeenAt > item.LastSeenAt {
				item.LastSeenAt = ledgerNode.LastSeenAt
			}
		}
		for nodeKey := range pending.Nodes {
			if ledgerNode := m.ledger.Nodes[nodeKey]; ledgerNode != nil {
				if item.SubscriptionName == "" {
					item.SubscriptionName = ledgerNode.SubscriptionName
				}
				if ledgerNode.LastSeenAt > item.LastSeenAt {
					item.LastSeenAt = ledgerNode.LastSeenAt
				}
			}
		}
	}

	if m.getValidSubscriptionIDs != nil {
		valid := make(map[string]struct{})
		for _, id := range m.getValidSubscriptionIDs() {
			if id != "" {
				valid[id] = struct{}{}
			}
		}
		for _, item := range byNode {
			_, item.Configurable = valid[item.SubscriptionID]
		}
	}

	items := make([]UnpricedNodeInfo, 0, len(byNode))
	for _, item := range byNode {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UnpricedRawBytes != items[j].UnpricedRawBytes {
			return items[i].UnpricedRawBytes > items[j].UnpricedRawBytes
		}
		if items[i].SubscriptionID != items[j].SubscriptionID {
			return items[i].SubscriptionID < items[j].SubscriptionID
		}
		return items[i].Node < items[j].Node
	})
	return items
}

func (m *TrafficAccountingManager) getSettingsResponseLocked() TrafficAccountingSettingsResponse {
	settings := m.getSettingsLocked()
	return TrafficAccountingSettingsResponse{
		Version:       settings.Version,
		Enabled:       settings.Enabled,
		Subscriptions: settings.Subscriptions,
		UnpricedNodes: m.getUnpricedNodesLocked(),
	}
}

// GetSettings returns a deep copy of current settings.
func (m *TrafficAccountingManager) GetSettings() TrafficAccountingSettingsResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getSettingsResponseLocked()
}

// UpdateSettings validates and applies settings modifications.
func (m *TrafficAccountingManager) UpdateSettings(req UpdateTrafficSettingsRequest) (*TrafficAccountingSettingsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Validation against valid subscription IDs if configured
	if m.getValidSubscriptionIDs != nil {
		validList := m.getValidSubscriptionIDs()
		validMap := make(map[string]bool, len(validList))
		for _, id := range validList {
			if id != "" {
				validMap[id] = true
			}
		}
		for subID := range req.Subscriptions {
			if !validMap[subID] {
				return nil, fmt.Errorf("未知或无效的订阅 ID: %s", subID)
			}
		}
	}

	for subID, subCfg := range req.Subscriptions {
		if len(subID) == 0 || len(subID) > 64 {
			return nil, errors.New("订阅 ID 格式无效")
		}
		if subCfg == nil {
			return nil, errors.New("订阅配置不能为空")
		}
		if subCfg.DefaultMultiplierMilli != nil {
			d := *subCfg.DefaultMultiplierMilli
			if d < minMultiplierMilli || d > maxMultiplierMilli {
				return nil, fmt.Errorf("默认倍率必须在 %d 到 %d 千分比之间 (0.1x - 100x)", minMultiplierMilli, maxMultiplierMilli)
			}
		}
		seenNodes := make(map[string]bool)
		for node, mult := range subCfg.NodeMultipliersMilli {
			trimmed := normalizeNodeName(node)
			if len(trimmed) == 0 || len(trimmed) > 256 {
				return nil, errors.New("节点名称格式无效")
			}
			if seenNodes[trimmed] {
				return nil, fmt.Errorf("存在重复的节点名称: %s", trimmed)
			}
			seenNodes[trimmed] = true
			if mult < minMultiplierMilli || mult > maxMultiplierMilli {
				return nil, fmt.Errorf("节点 '%s' 倍率必须在 %d 到 %d 千分比之间 (0.1x - 100x)", trimmed, minMultiplierMilli, maxMultiplierMilli)
			}
		}
	}

	// Snapshot current settings before mutation for rollback on persistence failure
	prevSettings := m.getSettingsLocked()

	// 2. Application
	if req.Enabled != nil {
		m.settings.Enabled = *req.Enabled
	}

	for subID, subReq := range req.Subscriptions {
		cfg, ok := m.settings.Subscriptions[subID]
		if !ok || cfg == nil {
			cfg = &SubscriptionAccountingConfig{
				NodeMultipliersMilli: make(map[string]uint32),
				KnownNodes:           []string{},
			}
			m.settings.Subscriptions[subID] = cfg
		}

		cfg.DefaultMultiplierMilli = subReq.DefaultMultiplierMilli
		cfg.NodeMultipliersMilli = make(map[string]uint32, len(subReq.NodeMultipliersMilli))
		for k, v := range subReq.NodeMultipliersMilli {
			trimmed := normalizeNodeName(k)
			cfg.NodeMultipliersMilli[trimmed] = v
			// Also add to knownNodes if not present
			found := false
			for _, kn := range cfg.KnownNodes {
				if kn == trimmed {
					found = true
					break
				}
			}
			if !found {
				cfg.KnownNodes = append(cfg.KnownNodes, trimmed)
			}
		}
		normalizeSubscriptionAccountingConfig(cfg)
	}

	if err := m.persistSettingsLocked(); err != nil {
		m.settings = prevSettings
		return nil, fmt.Errorf("保存配置失败: %w", err)
	}

	pricingChanged := m.repriceResolvedPendingLocked()
	if m.reconcileLegacyUnpricedLocked() {
		pricingChanged = true
	}
	if pricingChanged {
		if err := m.persistLedgerLocked(); err != nil {
			return nil, fmt.Errorf("倍率已保存，但未计价流量补算持久化失败: %w", err)
		}
	}

	settingsCopy := m.getSettingsResponseLocked()
	return &settingsCopy, nil
}

// Route classification
func classifyRoute(chains []string) (routeKind string, leafNode string, proxyGroup string, isSpeedtest bool) {
	for _, c := range chains {
		if strings.TrimSpace(c) == speedTestBenchmarkGroup {
			isSpeedtest = true
			break
		}
	}

	if len(chains) == 0 {
		return "direct", "", "", isSpeedtest
	}

	for _, c := range chains {
		if strings.ToUpper(strings.TrimSpace(c)) == "DIRECT" {
			return "direct", "", "", isSpeedtest
		}
	}

	for _, c := range chains {
		if strings.ToUpper(strings.TrimSpace(c)) == "REJECT" {
			return "reject", "", "", isSpeedtest
		}
	}

	routeKind = "proxy"
	leafNode = strings.TrimSpace(chains[0])

	// Scan chains from right to left (after leaf node) for group closest to rule entry
	for i := len(chains) - 1; i >= 1; i-- {
		candidate := strings.TrimSpace(chains[i])
		if !isBuiltinGroup(candidate) && candidate != "" {
			proxyGroup = candidate
			break
		}
	}

	return routeKind, leafNode, proxyGroup, isSpeedtest
}

func isBuiltinGroup(name string) bool {
	s := strings.ToUpper(strings.TrimSpace(name))
	return s == "DIRECT" || s == "REJECT" || s == "GLOBAL" || s == "COMPATIBLE" || s == "PASS"
}

// normalizeNodeName removes leading country flag emoji and whitespace.
func normalizeNodeName(name string) string {
	s := strings.TrimSpace(name)
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		// Regional indicator symbols (flags): 0x1F1E6 to 0x1F1FF
		// Black flag: 0x1F3F4, VS16: 0xFE0F, ZWJ: 0x200D, Tag characters: 0xE0061 - 0xE007A, Tag cancel: 0xE007F
		if (r >= 0x1F1E6 && r <= 0x1F1FF) || r == 0x1F3F4 || r == 0xFE0F || r == 0x200D || (r >= 0xE0061 && r <= 0xE007A) || r == 0xE007F {
			i++
			continue
		}
		break
	}
	return strings.TrimSpace(string(runes[i:]))
}

// normalizeTarget extracts clean host/domain or IP.
func normalizeTarget(host, destinationIP string) string {
	if t := cleanTargetCandidate(host); t != "" {
		return t
	}
	if t := cleanTargetCandidate(destinationIP); t != "" {
		return t
	}
	return "unknown"
}

func cleanTargetCandidate(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// If it has port like [::1]:80 or 1.2.3.4:80 or host:80
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	// Bracketed IPv6 without port: [2001:db8::1]
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSpace(s)

	// If it is a valid IP (IPv4 or bare IPv6), preserve it in canonical form
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}

	// Not an IP address: treat as domain / hostname
	// Sanitize out path, query, fragment, auth credentials
	if idx := strings.IndexAny(s, "/?#@; \t\r\n"); idx != -1 {
		s = s[:idx]
	}
	// Strip any trailing colon or port if left
	if idx := strings.IndexByte(s, ':'); idx != -1 {
		s = s[:idx]
	}
	s = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
	return s
}

// scaleBytes scales raw bytes with integer arithmetic using千分整数.
func scaleBytes(b uint64, milli uint32) uint64 {
	if milli == 0 || b == 0 {
		return 0
	}
	hi, lo := bits.Mul64(b, uint64(milli))
	if hi >= 1000 {
		return math.MaxUint64
	}
	q, _ := bits.Div64(hi, lo, 1000)
	return q
}

func safeAddUint64(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	_ = os.Chmod(tmp, perm)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(path, perm)
	return nil
}
