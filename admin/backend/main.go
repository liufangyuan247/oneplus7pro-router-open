package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const (
	defaultListen          = "0.0.0.0:9090"
	defaultHome            = "/data/data/com.termux/files/home"
	defaultPrefix          = "/data/data/com.termux/files/usr"
	defaultSessionTTL      = 24 * time.Hour
	rememberedCookieTTL    = 400 * 24 * time.Hour
	mihomoController       = "http://127.0.0.1:9091"
	mihomoDashboardPrefix  = "/mihomo-dashboard/"
	mihomoAPIPrefix        = "/mihomo-api/"
	mihomoDefaultMixedPort = 17890
	mihomoRequestTimeout   = 800 * time.Millisecond
	mihomoMaxResponseBytes = 16 * 1024 * 1024
	routerNetworkManager   = "/data/adb/router-network/router-network"
	clientPolicyManager    = routerNetworkManager
	tailscaleControl       = "/data/adb/tailscale/bin/tailscale-control"
)

var allowedServices = map[string]ServiceMeta{
	"openlist":         {Label: "OpenList", Port: 5244},
	"rclone":           {Label: "Rclone · 云同步", Port: 5572},
	"aria2":            {Label: "aria2", Port: 6800},
	"cpa":              {Label: "CPA · AI API", Port: 8317},
	"cpa-usage-keeper": {Label: "CPA Usage Keeper", Port: 8320},
	"mihomo":           {Label: "Mihomo · 代理核心", Port: 17890},
	"qbittorrent":      {Label: "qBittorrent", Port: 8080},
	"caddy":            {Label: "AriaNg / Caddy", Port: 6880},
	"sshd":             {Label: "SSH", Port: 8022},
	"healthd":          {Label: "Health Monitor", Port: 0},
	"router-admin":     {Label: "Router Admin", Port: 9090},
}

type ServiceMeta struct {
	Label string
	Port  int
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

type authSession struct {
	Expires    time.Time `json:"expires"`
	Persistent bool      `json:"persistent,omitempty"`
}

type Server struct {
	home, prefix, stack, data, webRoot string
	password                           string
	started                            time.Time
	mu                                 sync.Mutex
	subscriptionMu                     sync.Mutex
	clientPolicyMu                     sync.Mutex
	sessions                           map[string]authSession
	lastCPU                            cpuCounters
	lastCoreCPU                        map[int]cpuCounters
	netSamples                         []timedNetSample
	netStats                           map[string]netCounter
	netRates                           map[string]netRate
	clientTrafficMu                    sync.Mutex
	clientTrafficStats                 map[string]netCounter
	clientTrafficRates                 map[string]netRate
	clientTrafficSamples               []timedClientSample
	lastUSBInputWatts                  float64
	lastUSBInputAt                     time.Time
	lastChargeCounter                  int64
	lastChargeCounterAt                time.Time
	lastCoulombCurrent                 float64
	storageCache                       []StorageInfo
	storageAt                          time.Time
	lastConfigCheck                    time.Time
	terminalSessions                   atomic.Int32
	screenSessions                     atomic.Int32
	runSu                              func(command string) ([]byte, error)
	commandRunner                      func(name string, args ...string) string
	subscriptionUpdater                func(item SubscriptionRecord, activate, useCache bool) ([]byte, error)
	mihomoController                   string
	speedTestMu                        sync.Mutex
	speedTestJob                       *MihomoSpeedTestJob
	speedTestCancel                    context.CancelFunc
	speedTestHTTP                      func(ctx context.Context, proxyURL, targetURL string, sampleBytes int64) (int64, time.Duration, time.Duration, int, error)
	speedTestSampleWAN                 func(ctx context.Context) (float64, error)
	speedTestCheckListener             func() bool
	snapshotMu                         sync.Mutex
	snapshotCache                      Snapshot
	snapshotAt                         time.Time
	snapshotCollecting                 bool
	snapshotWait                       chan struct{}
	collectSnapshot                    func() (Snapshot, error)
	mihomoConnectionsMu                sync.Mutex
	mihomoConnectionsCache             *mihomoRawConnectionsResponse
	mihomoConnectionsAt                time.Time
	mihomoConnectionsLoading           bool
	mihomoConnectionsWait              chan struct{}
	metricsMu                          sync.RWMutex
	metricHistory                      []MetricHistoryPoint
	metricLastCPU                      cpuCounters
}

const (
	maxTerminalSessions            = 2
	snapshotCacheTTL               = 8 * time.Second
	mihomoConnectionsCacheTTL      = 2 * time.Second
	metricSampleInterval           = 5 * time.Second
	metricHistoryWindow            = 2 * time.Hour
	scrcpyServerVersion            = "3.3.1"
	scrcpyVideoChannel        byte = 0
	scrcpyControlChannel      byte = 1
	scrcpyDeviceChannel       byte = 2
)

var terminalUpgrader = websocket.Upgrader{
	HandshakeTimeout: 5 * time.Second,
	ReadBufferSize:   4096,
	WriteBufferSize:  32768,
	CheckOrigin:      sameOrigin,
}

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type scrcpySession struct {
	cmd       *exec.Cmd
	video     *net.UnixConn
	control   *net.UnixConn
	output    *bytes.Buffer
	scid      string
	su        string
	shellPath string
}

type cpuCounters struct{ total, idle uint64 }
type netCounter struct{ rx, tx uint64 }
type netRate struct{ rx, tx float64 }
type timedNetSample struct {
	at    time.Time
	stats map[string]netCounter
}
type timedClientSample struct {
	at    time.Time
	stats map[string]netCounter
}

const (
	networkSampleInterval = time.Second
	networkRateWindow     = 5 * time.Second
)

type Snapshot struct {
	Time         string               `json:"time"`
	TrustedLocal bool                 `json:"trustedLocal"`
	Device       DeviceInfo           `json:"device"`
	CPU          CPUInfo              `json:"cpu"`
	Memory       MemoryInfo           `json:"memory"`
	Power        PowerInfo            `json:"power"`
	Thermals     []ThermalInfo        `json:"thermals"`
	Interfaces   []InterfaceInfo      `json:"interfaces"`
	Clients      []ClientInfo         `json:"clients"`
	Services     []ServiceInfo        `json:"services"`
	Aria2        Aria2Info            `json:"aria2"`
	Storage      []StorageInfo        `json:"storage"`
	USB          []USBInfo            `json:"usb"`
	NetworkDraft NetworkDraft         `json:"networkDraft"`
	History      []MetricHistoryPoint `json:"history"`
}

type MetricHistoryPoint struct {
	At                    int64   `json:"at"`
	CPU                   float64 `json:"cpu"`
	Temperature           float64 `json:"temperature"`
	Memory                float64 `json:"memory"`
	WANDown               float64 `json:"wanDown"`
	WANUp                 float64 `json:"wanUp"`
	LANDown               float64 `json:"lanDown"`
	LANUp                 float64 `json:"lanUp"`
	AriaDown              float64 `json:"ariaDown"`
	AriaUp                float64 `json:"ariaUp"`
	BatteryTemperature    float64 `json:"batteryTemperature"`
	BatteryVoltage        float64 `json:"batteryVoltage"`
	BatteryCurrent        float64 `json:"batteryCurrent"`
	BatteryAverageCurrent float64 `json:"batteryAverageCurrent"`
	BatteryCoulombCurrent float64 `json:"batteryCoulombCurrent"`
	BatteryPower          float64 `json:"batteryPower"`
	BatteryPercent        int     `json:"batteryPercent"`
	ChargeCounter         float64 `json:"chargeCounter"`
	USBVoltage            float64 `json:"usbVoltage"`
	USBCurrent            float64 `json:"usbCurrent"`
	USBPower              float64 `json:"usbPower"`
}

type DeviceInfo struct {
	Model, Android, Kernel string
	UptimeSec              int64 `json:"uptimeSec"`
	AdminUptimeSec         int64 `json:"adminUptimeSec"`
}
type CPUInfo struct {
	Usage                float64 `json:"usage"`
	Load1, Load5, Load15 float64
	Online               int
	AvgMHz               int           `json:"avgMHz"`
	MaxMHz               int           `json:"maxMHz"`
	Temperature          float64       `json:"temperature"`
	Cores                []CPUCoreInfo `json:"cores"`
}
type CPUCoreInfo struct {
	ID         int     `json:"id"`
	Online     bool    `json:"online"`
	Usage      float64 `json:"usage"`
	CurrentMHz int     `json:"currentMHz"`
	MaxMHz     int     `json:"maxMHz"`
	Governor   string  `json:"governor"`
}
type MemoryInfo struct {
	Total, Available, Used, Cached, Buffers, Reclaimable, SwapTotal, SwapUsed uint64
}
type PowerInfo struct {
	BatteryPercent                              int `json:"batteryPercent"`
	BatteryTemp, Voltage, Current, BatteryWatts float64
	AverageCurrent, CoulombCurrent              float64
	ChargeCounterMAh                            float64
	TimeToEmptySec                              int `json:"timeToEmptySec"`
	Status                                      string
	ChargingEnabled, InputSuspended             bool
	USBPresent                                  bool   `json:"usbPresent"`
	USBOnline                                   bool   `json:"usbOnline"`
	USBType                                     string `json:"usbType"`
	USBVoltage, USBInputCurrent, USBInputWatts  float64
	USBInputFiltered                            bool
	PDVoltage, PDCurrentMax, PDWattsMax         float64
	TypeCMode, PowerRole                        string
	OTGActive                                   bool
	ChargeGuard                                 bool `json:"chargeGuard"`
	ChargePaused                                bool `json:"chargePaused"`
	GuardLow                                    int  `json:"guardLow"`
	GuardHigh                                   int  `json:"guardHigh"`
}
type ThermalInfo struct {
	Name, Type string
	Temp       float64
}
type InterfaceInfo struct {
	Name, MAC, State, Carrier, Role string
	MTU, SpeedMbps                  int
	Addresses                       []string
	RxBytes, TxBytes                uint64
	RxBps, TxBps                    float64
}
type ClientInfo struct {
	IP, MAC, Hostname, Interface, State, Source string
	Expires                                     int64
	StaticLease                                 bool
	StaticIP                                    string
	CustomName                                  bool
	RxBytes, TxBytes                            uint64
	RxBps, TxBps                                float64
}

type StaticLease struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
}
type ServiceInfo struct {
	Name, Label, State   string
	PID, UptimeSec, Port int
	WebURL               string `json:"webUrl"`
	CurrentVersion       string `json:"currentVersion,omitempty"`
	BaseVersion          string `json:"baseVersion,omitempty"`
	CustomBuild          bool   `json:"customBuild,omitempty"`
	LatestVersion        string `json:"latestVersion,omitempty"`
	Upgradable           bool   `json:"upgradable,omitempty"`
	HasBackup            bool   `json:"hasBackup,omitempty"`
	CanUpgrade           bool   `json:"canUpgrade,omitempty"`
	UpgradeHint          string `json:"upgradeHint,omitempty"`
}
type Aria2Info struct {
	Online        bool        `json:"online"`
	DownloadSpeed uint64      `json:"downloadSpeed"`
	UploadSpeed   uint64      `json:"uploadSpeed"`
	NumActive     int         `json:"numActive"`
	NumWaiting    int         `json:"numWaiting"`
	NumPaused     int         `json:"numPaused"`
	NumStopped    int         `json:"numStopped"`
	Tasks         []Aria2Task `json:"tasks"`
}
type Aria2Task struct {
	GID, Name, Status                                        string
	TotalLength, CompletedLength, DownloadSpeed, UploadSpeed uint64
	Connections                                              int
}
type StorageInfo struct {
	Name, Mount, Filesystem, Model string
	Total, Used, Free              uint64
	UsedPercent                    float64
	External                       bool
}
type SystemLogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
}
type SystemLogResponse struct {
	Source    string           `json:"source"`
	Label     string           `json:"label"`
	UpdatedAt string           `json:"updatedAt"`
	Entries   []SystemLogEntry `json:"entries"`
	Info      int              `json:"info"`
	Warning   int              `json:"warning"`
	Error     int              `json:"error"`
}
type USBInfo struct{ VendorID, ProductID, Manufacturer, Product, Speed string }
type NetworkDraft struct {
	WANInterface string `json:"WANInterface"`
	LANInterface string `json:"LANInterface"`
	WANMode      string `json:"WANMode"`
	LANAddress   string `json:"LANAddress"`
	DHCPStart    string `json:"DHCPStart"`
	DHCPEnd      string `json:"DHCPEnd"`
	DNS          string `json:"DNS"`
	MTU          int    `json:"MTU"`
	PPPoEUser    string `json:"PPPoEUser"`
	PPPoEPass    string `json:"PPPoEPass"`
	PPPoEBackend string `json:"PPPoEBackend"`
	WANMAC       string `json:"WANMAC"`
}

type SubscriptionInfo struct {
	Configured      bool   `json:"configured"`
	Source          string `json:"source"`
	LastUpdated     string `json:"lastUpdated"`
	AutoUpdateHours int    `json:"autoUpdateHours"`
}

type SubscriptionRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	CachedURL string    `json:"cachedUrl,omitempty"`
	NodeCount int       `json:"nodeCount,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SubscriptionStore struct {
	Version  int                  `json:"version"`
	ActiveID string               `json:"activeId"`
	Items    []SubscriptionRecord `json:"items"`
}

type PublicSubscription struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	Cached      bool   `json:"cached"`
	NodeCount   int    `json:"nodeCount"`
	NeedsUpdate bool   `json:"needsUpdate"`
	Active      bool   `json:"active"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type MihomoSettings struct {
	MixedPort   int    `json:"mixedPort"`
	HTTPPort    int    `json:"httpPort"`
	SocksPort   int    `json:"socksPort"`
	RedirPort   int    `json:"redirPort"`
	TProxyPort  int    `json:"tproxyPort"`
	AllowLAN    bool   `json:"allowLan"`
	BindAddress string `json:"bindAddress"`
}

type MihomoProxyGroup struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

type WholeHomeProxyState struct {
	Enabled  bool   `json:"enabled"`
	Active   bool   `json:"active"`
	Ready    bool   `json:"ready"`
	Listener string `json:"listener"`
}

type TailscaleState struct {
	Installed       bool   `json:"installed"`
	Enabled         bool   `json:"enabled"`
	Supervisor      bool   `json:"supervisor"`
	Daemon          bool   `json:"daemon"`
	Online          bool   `json:"online"`
	LoginRequired   bool   `json:"loginRequired"`
	LoginPending    bool   `json:"loginPending"`
	Backend         string `json:"backend"`
	Hostname        string `json:"hostname"`
	IP              string `json:"ip"`
	AdvertiseRoutes string `json:"advertiseRoutes"`
	PrimaryRoutes   string `json:"primaryRoutes"`
	RouteStatus     string `json:"routeStatus"`
	AuthURL         string `json:"authUrl,omitempty"`
}

type MihomoConnection struct {
	ID       string   `json:"id"`
	Host     string   `json:"host"`
	Source   string   `json:"source"`
	Network  string   `json:"network"`
	Type     string   `json:"type"`
	Rule     string   `json:"rule"`
	Chains   []string `json:"chains"`
	Upload   uint64   `json:"upload"`
	Download uint64   `json:"download"`
	Start    string   `json:"start"`
}

func main() {
	if os.Getenv("SSL_CERT_FILE") == "" {
		for _, certPath := range []string{
			"/data/data/com.termux/files/usr/etc/tls/cert.pem",
			"/data/data/com.termux/files/usr/etc/ssl/cert.pem",
		} {
			if _, err := os.Stat(certPath); err == nil {
				_ = os.Setenv("SSL_CERT_FILE", certPath)
				break
			}
		}
	}
	home := env("ROUTER_HOME", defaultHome)
	s := &Server{
		home:    home,
		prefix:  env("ROUTER_PREFIX", defaultPrefix),
		stack:   filepath.Join(home, "router-stack"),
		data:    filepath.Join(home, "router-data"),
		webRoot: env("ROUTER_ADMIN_WEB", filepath.Join(home, "router-admin", "web")),
		started: time.Now(), sessions: make(map[string]authSession), lastCoreCPU: make(map[int]cpuCounters), netStats: make(map[string]netCounter), netRates: make(map[string]netRate), clientTrafficStats: make(map[string]netCounter), clientTrafficRates: make(map[string]netRate),
	}
	password, created, err := ensurePassword(filepath.Join(s.data, "admin", "password"))
	if err != nil {
		log.Fatal(err)
	}
	s.password = password
	if created {
		log.Printf("INITIAL_ADMIN_PASSWORD=%s", password)
	}
	if err := s.loadRememberedSessions(); err != nil {
		log.Printf("load remembered sessions: %v", err)
	}
	s.startNetworkSampler()
	s.startMetricSampler()
	s.initSpeedTest()
	s.initServiceUpdates()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/login", s.login)
	mux.HandleFunc("/api/logout", s.auth(s.logout))
	mux.HandleFunc("/api/snapshot", s.auth(s.snapshot))
	mux.HandleFunc("/api/clients/name", s.auth(s.clientNameAPI))
	mux.HandleFunc("/api/clients/static-lease", s.auth(s.staticLeaseAPI))
	mux.HandleFunc("/api/services/action", s.auth(s.serviceAction))
	mux.HandleFunc("/api/services/updates", s.auth(s.servicesUpdatesAPI))
	mux.HandleFunc("/api/services/upgrade", s.auth(s.servicesUpgradeAPI))
	mux.HandleFunc("/api/services/rollback", s.auth(s.servicesRollbackAPI))
	mux.HandleFunc("/api/logs", s.auth(s.logs))
	mux.HandleFunc("/api/system-logs", s.auth(s.systemLogs))
	mux.HandleFunc("/api/tailscale", s.auth(s.tailscaleAPI))
	mux.HandleFunc("/api/terminal", s.auth(s.terminal))
	mux.HandleFunc("/api/screen", s.auth(s.screen))
	mux.HandleFunc("/api/device/power", s.auth(s.devicePower))
	mux.HandleFunc("/api/power/battery-guard", s.auth(s.batteryGuardConfig))
	mux.HandleFunc("/api/mihomo/status", s.auth(s.mihomoStatus))
	mux.HandleFunc("/api/mihomo/mode", s.auth(s.mihomoMode))
	mux.HandleFunc("/api/mihomo/settings", s.auth(s.mihomoSettings))
	mux.HandleFunc("/api/mihomo/whole-home-proxy", s.auth(s.mihomoWholeHomeProxy))
	mux.HandleFunc("/api/mihomo/proxies", s.auth(s.mihomoProxies))
	mux.HandleFunc("/api/mihomo/speedtest", s.auth(s.mihomoSpeedTest))
	mux.HandleFunc("/api/mihomo/speedtest/apply", s.auth(s.mihomoSpeedTestApply))
	mux.HandleFunc("/api/mihomo/delay", s.auth(s.mihomoDelay))
	mux.HandleFunc("/api/mihomo/connections", s.auth(s.mihomoConnections))
	mux.HandleFunc("/api/mihomo/subscription", s.auth(s.mihomoSubscription))
	mux.HandleFunc("/api/mihomo/subscriptions", s.auth(s.mihomoSubscriptionsV2))
	mux.HandleFunc("/api/mihomo/subscriptions/nodes", s.auth(s.mihomoSubscriptionNodes))
	mux.HandleFunc(mihomoDashboardPrefix, s.auth(s.mihomoDashboard))
	mux.HandleFunc(mihomoAPIPrefix, s.auth(s.mihomoAPIProxy))
	mux.HandleFunc("/_nuxt/", s.auth(s.mihomoDashboardAssets))
	mux.HandleFunc("/_fonts/", s.auth(s.mihomoDashboardFonts))
	mux.HandleFunc("/api/network-draft", s.auth(s.networkDraft))
	mux.HandleFunc("/api/network/reconnect", s.auth(s.networkReconnect))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, http.StatusNotFound, map[string]string{"error": "API not found"})
	})
	mux.HandleFunc("/", s.static)

	listen := env("ROUTER_ADMIN_LISTEN", defaultListen)
	log.Printf("OnePlus Router Admin listening on http://%s", listen)
	server := &http.Server{Addr: listen, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	log.Fatal(server.ListenAndServe())
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func ensurePassword(path string) (string, bool, error) {
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b)), false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", false, err
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", false, err
	}
	p := "OP7-" + base64.RawURLEncoding.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(p+"\n"), 0600); err != nil {
		return "", false, err
	}
	return p, true, nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func method(w http.ResponseWriter, r *http.Request, want string) bool {
	if r.Method != want {
		jsonOut(w, 405, map[string]string{"error": "method not allowed"})
		return false
	}
	return true
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, 200, map[string]any{"ok": true, "uptime": int64(time.Since(s.started).Seconds())})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var body struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonOut(w, 400, map[string]string{"error": "请求格式错误"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.password)) != 1 {
		time.Sleep(350 * time.Millisecond)
		jsonOut(w, 401, map[string]string{"error": "密码错误"})
		return
	}
	tokenBytes := make([]byte, 32)
	_, _ = rand.Read(tokenBytes)
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	session := authSession{Expires: time.Now().Add(defaultSessionTTL), Persistent: body.Remember}
	if body.Remember {
		session.Expires = time.Time{}
	}
	s.mu.Lock()
	s.sessions[sessionKey(token)] = session
	s.pruneSessionsLocked()
	if body.Remember {
		if err := s.saveRememberedSessionsLocked(); err != nil {
			delete(s.sessions, sessionKey(token))
			s.mu.Unlock()
			log.Printf("save remembered session: %v", err)
			jsonOut(w, 500, map[string]string{"error": "failed to remember login"})
			return
		}
	}
	s.mu.Unlock()
	cookie := &http.Cookie{Name: "router_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode}
	if body.Remember {
		cookie.MaxAge = int(rememberedCookieTTL.Seconds())
		cookie.Expires = time.Now().Add(rememberedCookieTTL)
	}
	http.SetCookie(w, cookie)
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	if c, e := r.Cookie("router_session"); e == nil {
		s.mu.Lock()
		key := sessionKey(c.Value)
		remembered := s.sessions[key].Persistent
		delete(s.sessions, key)
		if remembered {
			if err := s.saveRememberedSessionsLocked(); err != nil {
				log.Printf("remove remembered session: %v", err)
			}
		}
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "router_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && !sameOrigin(r) {
			jsonOut(w, 403, map[string]string{"error": "origin rejected"})
			return
		}
		// Requests made to 127.0.0.1 originate on the phone itself. They can
		// bypass the password without weakening access from Wi-Fi or Ethernet.
		if isLoopbackRequest(r) {
			next(w, r)
			return
		}
		c, err := r.Cookie("router_session")
		if err != nil {
			jsonOut(w, 401, map[string]string{"error": "login required"})
			return
		}
		s.mu.Lock()
		key := sessionKey(c.Value)
		session, ok := s.sessions[key]
		expired := ok && !session.Persistent && time.Now().After(session.Expires)
		if ok && !expired && !session.Persistent {
			session.Expires = time.Now().Add(defaultSessionTTL)
			s.sessions[key] = session
		}
		if expired {
			delete(s.sessions, key)
			if session.Persistent {
				if err := s.saveRememberedSessionsLocked(); err != nil {
					log.Printf("prune remembered session: %v", err)
				}
			}
		}
		s.mu.Unlock()
		if !ok || expired {
			jsonOut(w, 401, map[string]string{"error": "login required"})
			return
		}
		if session.Persistent {
			http.SetCookie(w, &http.Cookie{
				Name: "router_session", Value: c.Value, Path: "/", HttpOnly: true,
				SameSite: http.SameSiteStrictMode, MaxAge: int(rememberedCookieTTL.Seconds()),
				Expires: time.Now().Add(rememberedCookieTTL),
			})
		}
		next(w, r)
	}
}

func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		jsonOut(w, http.StatusUpgradeRequired, map[string]string{"error": "websocket required"})
		return
	}
	if !sameOrigin(r) {
		jsonOut(w, http.StatusForbidden, map[string]string{"error": "origin rejected"})
		return
	}
	if s.terminalSessions.Add(1) > maxTerminalSessions {
		s.terminalSessions.Add(-1)
		jsonOut(w, http.StatusTooManyRequests, map[string]string{"error": "too many terminal sessions"})
		return
	}
	defer s.terminalSessions.Add(-1)

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	root := r.URL.Query().Get("root") == "1"
	cmd := s.terminalCommand(root)
	cmd.Env = s.terminalEnvironment(root)
	cmd.Dir = s.home
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 32, Cols: 120})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n[terminal] start failed: "+err.Error()+"\r\n"))
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	conn.SetReadLimit(64 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	})

	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }
	go func() {
		defer closeDone()
		buf := make([]byte, 32<<10)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ping.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					closeDone()
					return
				}
			}
		}
	}()

	for {
		_, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		var message terminalMessage
		if json.Unmarshal(payload, &message) != nil {
			continue
		}
		switch message.Type {
		case "input":
			if len(message.Data) <= 64<<10 {
				_, _ = io.WriteString(ptmx, message.Data)
			}
		case "resize":
			if message.Cols >= 20 && message.Cols <= 500 && message.Rows >= 5 && message.Rows <= 300 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: message.Rows, Cols: message.Cols})
			}
		}
	}
	closeDone()
}

func (s *Server) terminalCommand(root bool) *exec.Cmd {
	shell := filepath.Join(s.prefix, "bin", "bash")
	if root {
		su := filepath.Join(s.prefix, "bin", "su")
		return exec.Command(su, "-c", shell+" -l")
	}
	return exec.Command(shell, "-l")
}

func (s *Server) terminalEnvironment(root bool) []string {
	path := filepath.Join(s.prefix, "bin") + ":" + filepath.Join(s.prefix, "bin", "applets") + ":/system/bin:/system/xbin:/vendor/bin"
	env := []string{
		"HOME=" + s.home,
		"PREFIX=" + s.prefix,
		"PATH=" + path,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=C.UTF-8",
		"SHELL=" + filepath.Join(s.prefix, "bin", "bash"),
	}
	if root {
		env = append(env, "ROUTER_WEB_ROOT=1")
	}
	return env
}

func (s *Server) screen(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		jsonOut(w, http.StatusUpgradeRequired, map[string]string{"error": "websocket required"})
		return
	}
	if !sameOrigin(r) {
		jsonOut(w, http.StatusForbidden, map[string]string{"error": "origin rejected"})
		return
	}
	if s.screenSessions.Add(1) > 1 {
		s.screenSessions.Add(-1)
		jsonOut(w, http.StatusTooManyRequests, map[string]string{"error": "screen session already active"})
		return
	}
	defer s.screenSessions.Add(-1)

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(256 << 10)

	session, err := s.startScrcpySession()
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}
	defer session.close()

	if err := conn.WriteJSON(map[string]any{
		"type":    "ready",
		"version": scrcpyServerVersion,
		"maxSize": 1280,
		"maxFps":  30,
		"bitRate": 3000000,
	}); err != nil {
		return
	}

	var writeMu sync.Mutex
	streamDone := make(chan error, 2)
	go func() {
		streamDone <- relayScrcpyStream(conn, session.video, scrcpyVideoChannel, &writeMu)
		_ = conn.Close()
	}()
	go func() {
		streamDone <- relayScrcpyStream(conn, session.control, scrcpyDeviceChannel, &writeMu)
		_ = conn.Close()
	}()

	for {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		if messageType != websocket.BinaryMessage || len(payload) < 2 || payload[0] != scrcpyControlChannel {
			continue
		}
		if _, writeErr := session.control.Write(payload[1:]); writeErr != nil {
			break
		}
	}
	select {
	case streamErr := <-streamDone:
		if streamErr != nil {
			log.Printf("scrcpy relay ended: %v", streamErr)
		}
	default:
	}
}

func devicePowerCommand(action string) (string, bool) {
	switch action {
	case "unlock":
		// WAKEUP and SLEEP are deterministic, unlike POWER which toggles and
		// could accidentally wake an already-locked device. dismiss-keyguard
		// only bypasses an unsecured swipe lock; Android still requires the
		// user's credential when a PIN, pattern, or password is configured.
		return "/system/bin/input keyevent 224; sleep 0.2; /system/bin/wm dismiss-keyguard; sleep 0.2; /system/bin/input keyevent 3", true
	case "lock":
		return "/system/bin/input keyevent 223", true
	default:
		return "", false
	}
}

func (s *Server) devicePower(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if decodeJSON(r, &body) != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	command, ok := devicePowerCommand(body.Action)
	if !ok {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "无效电源操作"})
		return
	}
	if output, err := s.execSu(command); err != nil {
		log.Printf("device power %s failed: %s", body.Action, tailLines(string(output), 10))
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "手机屏幕操作失败"})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true, "action": body.Action})
}

func (s *Server) execSu(command string) ([]byte, error) {
	if s.runSu != nil {
		return s.runSu(command)
	}
	su := filepath.Join(s.prefix, "bin", "su")
	return exec.Command(su, "-c", command).CombinedOutput()
}

func (s *Server) batteryGuardConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		statusPath := filepath.Join(s.data, "admin", "battery-guard.status")
		st := readBatteryGuardStatus(statusPath, time.Now())
		jsonOut(w, http.StatusOK, st)
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		Low  int `json:"low"`
		High int `json:"high"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if payload.Low < 20 || payload.High > 95 || payload.Low >= payload.High {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "阈值范围无效 (需满足 20 <= 最低 < 最高 <= 95)"})
		return
	}
	confContent := fmt.Sprintf("LOW=%d\nHIGH=%d\n", payload.Low, payload.High)
	tmpPath := fmt.Sprintf("/data/adb/router-stack/battery-guard.conf.tmp.%d", time.Now().UnixNano())
	targetPath := "/data/adb/router-stack/battery-guard.conf"
	cmdStr := fmt.Sprintf("tmp=%s; trap 'rm -f \"$tmp\"' EXIT; printf '%%s' %s > \"$tmp\" && chmod 600 \"$tmp\" && [ \"$(sed -n '1p' \"$tmp\")\" = 'LOW=%d' ] && [ \"$(sed -n '2p' \"$tmp\")\" = 'HIGH=%d' ] && [ \"$(wc -l < \"$tmp\")\" -eq 2 ] && mv -f \"$tmp\" %s",
		shellQuote(tmpPath),
		shellQuote(confContent),
		payload.Low,
		payload.High,
		shellQuote(targetPath),
	)
	if output, err := s.execSu(cmdStr); err != nil {
		log.Printf("battery guard config failed: %s", tailLines(string(output), 10))
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "保存电池保护配置失败"})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{
		"ok":   true,
		"low":  payload.Low,
		"high": payload.High,
	})
}

func (s *Server) startScrcpySession() (*scrcpySession, error) {
	serverPath := env("ROUTER_SCRCPY_SERVER", filepath.Join(s.data, "remote", "scrcpy-server-v"+scrcpyServerVersion))
	if info, err := os.Stat(serverPath); err != nil || info.IsDir() {
		return nil, fmt.Errorf("scrcpy server missing: %s", serverPath)
	}
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	value := uint32(random[0])<<24 | uint32(random[1])<<16 | uint32(random[2])<<8 | uint32(random[3])
	value &= 0x7fffffff
	scid := strconv.FormatUint(uint64(value), 16)
	socketName := "scrcpy_" + strings.Repeat("0", max(0, 8-len(scid))) + scid

	serverArgs := []string{
		"/system/bin/app_process",
		"/",
		"com.genymobile.scrcpy.Server",
		scrcpyServerVersion,
		"log_level=info",
		"max_size=1280",
		"max_fps=30",
		"tunnel_forward=true",
		"power_on=false",
		"clipboard_autosync=true",
		"cleanup=false",
		"video_bit_rate=3000000",
		// The dashboard is normally opened over plain HTTP on a LAN address.
		// WebCodecs is unavailable in that context, so the browser falls back
		// to TinyH264, which supports AVC Baseline up to Level 4.
		"video_codec_options=profile=1,level=2048",
		"audio=false",
		"scid=" + scid,
	}
	su := filepath.Join(s.prefix, "bin", "su")
	// scrcpy identifies itself to Android services as com.android.shell. It
	// must therefore run as UID 2000; running it as root makes OxygenOS 11's
	// clipboard service reject the package/UID pair and terminate control.
	shellPath := "/data/local/tmp/router-admin-scrcpy-server-" + scid
	prepare := "/system/bin/cp " + shellQuote(serverPath) + " " + shellQuote(shellPath) +
		" && /system/bin/chown 2000:2000 " + shellQuote(shellPath) +
		" && /system/bin/chmod 600 " + shellQuote(shellPath)
	if prepareOutput, err := exec.Command(su, "-c", prepare).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("prepare scrcpy for shell UID: %w: %s", err, strings.TrimSpace(string(prepareOutput)))
	}
	innerCommand := "exec env HOME=/data/local/tmp TMPDIR=/data/local/tmp CLASSPATH=" +
		shellQuote(shellPath) + " " + shellJoin(serverArgs)
	command := "exec " + shellQuote(su) + " 2000 -c " + shellQuote(innerCommand)
	cmd := exec.Command(su, "-c", command)
	cmd.Dir = "/data/local/tmp"
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		_ = exec.Command(su, "-c", "/system/bin/rm -f "+shellQuote(shellPath)).Run()
		return nil, fmt.Errorf("start scrcpy: %w", err)
	}

	session := &scrcpySession{cmd: cmd, output: &output, scid: scid, su: su, shellPath: shellPath}
	video, err := dialAbstractUnix(socketName, 8*time.Second)
	if err != nil {
		session.close()
		message := strings.TrimSpace(output.String())
		if message != "" {
			return nil, fmt.Errorf("scrcpy did not open video socket: %s", message)
		}
		return nil, fmt.Errorf("scrcpy did not open video socket: %w", err)
	}
	session.video = video
	control, err := dialAbstractUnix(socketName, 5*time.Second)
	if err != nil {
		session.close()
		return nil, fmt.Errorf("scrcpy did not open control socket: %w", err)
	}
	session.control = control
	_ = session.video.SetReadDeadline(time.Now().Add(5 * time.Second))
	dummy := []byte{0}
	if _, err := io.ReadFull(session.video, dummy); err != nil {
		session.close()
		return nil, fmt.Errorf("scrcpy video handshake failed: %w", err)
	}
	_ = session.video.SetReadDeadline(time.Time{})
	return session, nil
}

func dialAbstractUnix(name string, timeout time.Duration) (*net.UnixConn, error) {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: "\x00" + name, Net: "unix"})
		if err == nil {
			return conn, nil
		}
		last = err
		time.Sleep(80 * time.Millisecond)
	}
	return nil, last
}

func relayScrcpyStream(ws *websocket.Conn, stream *net.UnixConn, channel byte, writeMu *sync.Mutex) error {
	buf := make([]byte, (64<<10)+1)
	buf[0] = channel
	for {
		n, err := stream.Read(buf[1:])
		if n > 0 {
			writeMu.Lock()
			writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n+1])
			writeMu.Unlock()
			if writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			return err
		}
	}
}

func (s *scrcpySession) close() {
	if s.video != nil {
		_ = s.video.Close()
	}
	if s.control != nil {
		_ = s.control.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
	if s.output != nil {
		if message := strings.TrimSpace(s.output.String()); message != "" {
			log.Printf("scrcpy server output: %s", tailLines(message, 30))
		}
	}
	if s.su != "" && s.scid != "" {
		pattern := "com.genymobile.scrcpy.Server " + scrcpyServerVersion + ".*scid=" + s.scid
		_ = exec.Command(s.su, "-c", "pkill -f "+shellQuote(pattern)).Run()
	}
	if s.su != "" && s.shellPath != "" {
		_ = exec.Command(s.su, "-c", "/system/bin/rm -f "+shellQuote(s.shellPath)).Run()
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shellJoin(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = shellQuote(value)
	}
	return strings.Join(quoted, " ")
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = strings.Trim(r.RemoteAddr, "[]")
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	origin, err := url.Parse(o)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") {
		return false
	}
	return origin.Host == r.Host
}
func (s *Server) pruneSessionsLocked() {
	now := time.Now()
	for k, v := range s.sessions {
		if !v.Persistent && now.After(v.Expires) {
			delete(s.sessions, k)
		}
	}
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Server) rememberedSessionsPath() string {
	return filepath.Join(s.data, "admin", "sessions.json")
}

func (s *Server) loadRememberedSessions() error {
	data, err := os.ReadFile(s.rememberedSessionsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored map[string]authSession
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	for key, session := range stored {
		if session.Persistent {
			s.sessions[key] = session
		}
	}
	return nil
}

func (s *Server) saveRememberedSessionsLocked() error {
	stored := make(map[string]authSession)
	for key, session := range s.sessions {
		if session.Persistent {
			stored[key] = session
		}
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	path := s.rememberedSessionsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, payload, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	snap, err := s.cachedSnapshot(r.Context(), r.URL.Query().Get("fresh") == "1")
	if err != nil {
		jsonOut(w, 500, map[string]string{"error": err.Error()})
		return
	}
	snap.TrustedLocal = isLoopbackRequest(r)
	jsonOut(w, 200, snap)
}

// cachedSnapshot makes expensive system discovery independent of the number of
// open dashboards. At most one collector runs at a time and every client shares
// the same short-lived immutable result.
func (s *Server) cachedSnapshot(ctx context.Context, force bool) (Snapshot, error) {
	for {
		s.snapshotMu.Lock()
		if !force && !s.snapshotAt.IsZero() && time.Since(s.snapshotAt) < snapshotCacheTTL {
			snap := s.snapshotCache
			s.snapshotMu.Unlock()
			return snap, nil
		}
		if s.snapshotCollecting {
			wait := s.snapshotWait
			s.snapshotMu.Unlock()
			select {
			case <-wait:
				// A forced request can reuse the collection it waited for.
				force = false
				continue
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			}
		}

		s.snapshotCollecting = true
		s.snapshotWait = make(chan struct{})
		wait := s.snapshotWait
		s.snapshotMu.Unlock()

		collector := s.collect
		if s.collectSnapshot != nil {
			collector = s.collectSnapshot
		}
		snap, err := collector()

		s.snapshotMu.Lock()
		if err == nil {
			s.snapshotCache = snap
			s.snapshotAt = time.Now()
		}
		s.snapshotCollecting = false
		close(wait)
		s.snapshotMu.Unlock()
		return snap, err
	}
}

func (s *Server) collect() (Snapshot, error) {
	thermals := readThermals()
	cpuTemp := 0.0
	for _, t := range thermals {
		if (strings.HasPrefix(t.Type, "cpu") || strings.HasPrefix(t.Type, "cpuss")) && t.Temp > cpuTemp {
			cpuTemp = t.Temp
		}
	}
	return Snapshot{Time: time.Now().Format(time.RFC3339), Device: readDevice(s.started), CPU: s.readCPU(cpuTemp), Memory: readMemory(), Power: s.readPower(), Thermals: thermals, Interfaces: s.readInterfaces(), Clients: s.readClients(), Services: s.readServices(), Aria2: s.readAria2(), Storage: s.readStorage(), USB: readUSB(), NetworkDraft: s.loadNetworkDraft(), History: s.metricHistorySnapshot()}, nil
}

func readDevice(started time.Time) DeviceInfo {
	up, _ := readFirstFloat("/proc/uptime")
	return DeviceInfo{Model: commandText("/system/bin/getprop", "ro.product.model"), Android: commandText("/system/bin/getprop", "ro.build.version.release"), Kernel: strings.TrimSpace(readText("/proc/sys/kernel/osrelease")), UptimeSec: int64(up), AdminUptimeSec: int64(time.Since(started).Seconds())}
}

func (s *Server) readCPU(temp float64) CPUInfo {
	current, currentCores := readAllCPUCounters()
	usage := 0.0
	coreUsage := make(map[int]float64, len(currentCores))
	s.mu.Lock()
	if s.lastCPU.total > 0 && current.total > s.lastCPU.total {
		dt := current.total - s.lastCPU.total
		di := current.idle - s.lastCPU.idle
		usage = 100 * float64(dt-di) / float64(dt)
	}
	for id, counters := range currentCores {
		previous, ok := s.lastCoreCPU[id]
		if ok && counters.total > previous.total {
			dt := counters.total - previous.total
			di := counters.idle - previous.idle
			coreUsage[id] = 100 * float64(dt-di) / float64(dt)
		}
	}
	s.lastCPU = current
	s.lastCoreCPU = currentCores
	s.mu.Unlock()
	load := strings.Fields(readText("/proc/loadavg"))
	var l1, l5, l15 float64
	if len(load) >= 3 {
		l1, _ = strconv.ParseFloat(load[0], 64)
		l5, _ = strconv.ParseFloat(load[1], 64)
		l15, _ = strconv.ParseFloat(load[2], 64)
	}
	curFiles, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	total, online, maxMHz := 0, 0, 0
	cores := make([]CPUCoreInfo, 0, len(curFiles))
	for _, f := range curFiles {
		cpuDir := filepath.Dir(filepath.Dir(f))
		id, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(cpuDir), "cpu"))
		isOnline := id == 0 || strings.TrimSpace(readText(filepath.Join(cpuDir, "online"))) != "0"
		v := readInt(f) / 1000
		if isOnline && v > 0 {
			total += v
			online++
		}
		m := readInt(filepath.Join(filepath.Dir(f), "cpuinfo_max_freq")) / 1000
		if m > maxMHz {
			maxMHz = m
		}
		cores = append(cores, CPUCoreInfo{ID: id, Online: isOnline, Usage: round(coreUsage[id], 1), CurrentMHz: v, MaxMHz: m, Governor: strings.TrimSpace(readText(filepath.Join(filepath.Dir(f), "scaling_governor")))})
	}
	sort.Slice(cores, func(i, j int) bool { return cores[i].ID < cores[j].ID })
	avg := 0
	if online > 0 {
		avg = total / online
	}
	return CPUInfo{Usage: round(usage, 1), Load1: l1, Load5: l5, Load15: l15, Online: online, AvgMHz: avg, MaxMHz: maxMHz, Temperature: temp, Cores: cores}
}

func readAllCPUCounters() (cpuCounters, map[int]cpuCounters) {
	f, e := os.Open("/proc/stat")
	if e != nil {
		return cpuCounters{}, map[int]cpuCounters{}
	}
	defer f.Close()
	all := cpuCounters{}
	cores := map[int]cpuCounters{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) < 5 || (p[0] != "cpu" && !strings.HasPrefix(p[0], "cpu")) {
			if len(p) > 0 && p[0] != "cpu" && !strings.HasPrefix(p[0], "cpu") {
				break
			}
			continue
		}
		var values []uint64
		for _, value := range p[1:] {
			v, _ := strconv.ParseUint(value, 10, 64)
			values = append(values, v)
		}
		counter := cpuCounters{}
		for _, v := range values {
			counter.total += v
		}
		counter.idle = values[3]
		if len(values) > 4 {
			counter.idle += values[4]
		}
		if p[0] == "cpu" {
			all = counter
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(p[0], "cpu"))
		if err == nil {
			cores[id] = counter
		}
	}
	return all, cores
}

func readMemory() MemoryInfo {
	vals := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(readText("/proc/meminfo")))
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) >= 2 {
			v, _ := strconv.ParseUint(p[1], 10, 64)
			vals[strings.TrimSuffix(p[0], ":")] = v * 1024
		}
	}
	used := vals["MemTotal"] - vals["MemAvailable"]
	swapUsed := vals["SwapTotal"] - vals["SwapFree"]
	return MemoryInfo{Total: vals["MemTotal"], Available: vals["MemAvailable"], Used: used, Cached: vals["Cached"], Buffers: vals["Buffers"], Reclaimable: vals["SReclaimable"], SwapTotal: vals["SwapTotal"], SwapUsed: swapUsed}
}

func powerSupplyVoltage(raw int) float64 {
	if raw < 0 {
		raw = -raw
	}
	if raw == 0 {
		return 0
	}
	// Qualcomm kernels are inconsistent here: battery voltage_now is in uV,
	// while this OnePlus USB power-supply reports voltage_now in mV.
	if raw < 100000 {
		return float64(raw) / 1e3
	}
	return float64(raw) / 1e6
}

func (s *Server) readPower() PowerInfo {
	b := "/sys/class/power_supply/battery"
	bms := "/sys/class/power_supply/bms"
	u := "/sys/class/power_supply/usb"
	voltage := powerSupplyVoltage(readInt(b + "/voltage_now"))
	current := float64(readInt(b+"/current_now")) / 1e6
	averageCurrent := float64(readInt(bms+"/current_avg")) / 1e6
	chargeCounter := readInt64(b + "/charge_counter")
	if chargeCounter == 0 {
		chargeCounter = readInt64(bms + "/charge_counter")
	}
	usbPresent := readInt(u+"/present") == 1
	usbOnline := readInt(u+"/online") == 1
	usbVoltage := powerSupplyVoltage(readInt(u + "/voltage_now"))
	usbInputCurrent := float64(readInt(u+"/input_current_now")) / 1e6
	usbInputWatts := usbVoltage * usbInputCurrent
	batteryWatts := voltage * current
	filtered := false

	// input_current_now occasionally falls to a bogus ~10 mA for one sample.
	// If that would claim less input power than the battery is receiving, retain
	// the last physically plausible reading for a short period.
	s.mu.Lock()
	now := time.Now()
	coulombCurrent := s.lastCoulombCurrent
	if chargeCounter > 0 && s.lastChargeCounter > 0 && now.After(s.lastChargeCounterAt) {
		hours := now.Sub(s.lastChargeCounterAt).Hours()
		if hours > 0 {
			// OnePlus reports positive current_now while discharging. Match that
			// convention: a falling charge counter produces a positive value.
			measured := (float64(s.lastChargeCounter-chargeCounter) / 1e6) / hours
			if measured > -20 && measured < 20 {
				if s.lastCoulombCurrent == 0 {
					coulombCurrent = measured
				} else {
					coulombCurrent = s.lastCoulombCurrent*0.65 + measured*0.35
				}
			}
		}
	}
	if chargeCounter > 0 {
		s.lastChargeCounter = chargeCounter
		s.lastChargeCounterAt = now
		s.lastCoulombCurrent = coulombCurrent
	}
	invalidDip := usbOnline && usbInputWatts >= 0 && batteryWatts < -1 && usbInputWatts < (-batteryWatts+0.5)
	if invalidDip && s.lastUSBInputWatts > 0 && now.Sub(s.lastUSBInputAt) <= 15*time.Second {
		usbInputWatts = s.lastUSBInputWatts
		filtered = true
	} else if usbOnline && usbVoltage >= 4 && usbVoltage <= 24 && usbInputCurrent >= 0.05 {
		s.lastUSBInputWatts = usbInputWatts
		s.lastUSBInputAt = now
	} else if !usbOnline {
		s.lastUSBInputWatts = 0
		s.lastUSBInputAt = time.Time{}
	}
	s.mu.Unlock()

	pdVoltage := powerSupplyVoltage(readInt(u + "/pd_voltage_max"))
	if pdVoltage == 0 {
		pdVoltage = powerSupplyVoltage(readInt(u + "/voltage_max"))
	}
	pdCurrent := float64(readInt(u+"/pd_current_max")) / 1e6
	if pdCurrent == 0 {
		pdCurrent = float64(readInt(u+"/current_max")) / 1e6
	}
	usbType := strings.TrimSpace(readText(u + "/real_type"))
	if usbType == "" {
		usbType = strings.TrimSpace(readText(u + "/type"))
	}
	paused := false
	paths, _ := filepath.Glob("/sys/module/*/parameters/op_disable_charge")
	paths = append(paths, b+"/op_disable_charge")
	for _, p := range paths {
		v := strings.TrimSpace(readText(p))
		if v == "1" || strings.EqualFold(v, "Y") {
			paused = true
		}
	}
	st := readBatteryGuardStatus(filepath.Join(s.data, "admin", "battery-guard.status"), now)
	guard := st.Healthy && st.Enabled
	if guard {
		paused = st.Paused
	} else if paused {
		// Keep compatibility with kernels where the charge node is readable by
		// Termux even before the heartbeat file has been created.
		guard = true
	}
	return PowerInfo{BatteryPercent: readInt(b + "/capacity"), BatteryTemp: float64(readInt(b+"/temp")) / 10, Voltage: voltage, Current: current, BatteryWatts: round(batteryWatts, 2), AverageCurrent: round(averageCurrent, 3), CoulombCurrent: round(coulombCurrent, 3), ChargeCounterMAh: round(float64(chargeCounter)/1000, 1), TimeToEmptySec: readInt(bms + "/time_to_empty_avg"), Status: strings.TrimSpace(readText(b + "/status")), ChargingEnabled: readInt(b+"/charging_enabled") == 1, InputSuspended: readInt(b+"/input_suspend") == 1, USBPresent: usbPresent, USBOnline: usbOnline, USBType: usbType, USBVoltage: round(usbVoltage, 3), USBInputCurrent: round(usbInputCurrent, 3), USBInputWatts: round(usbInputWatts, 2), USBInputFiltered: filtered, PDVoltage: round(pdVoltage, 2), PDCurrentMax: round(pdCurrent, 3), PDWattsMax: round(pdVoltage*pdCurrent, 1), TypeCMode: strings.TrimSpace(readText(u + "/typec_mode")), PowerRole: strings.TrimSpace(readText(u + "/typec_power_role")), OTGActive: readInt(u+"/otg_switch") == 1, ChargeGuard: guard, ChargePaused: paused, GuardLow: st.Low, GuardHigh: st.High}
}

type BatteryGuardStatus struct {
	Healthy bool  `json:"healthy"`
	Enabled bool  `json:"enabled"`
	Paused  bool  `json:"paused"`
	Low     int   `json:"low"`
	High    int   `json:"high"`
	Updated int64 `json:"updated"`
}

func readBatteryGuardStatus(path string, now time.Time) BatteryGuardStatus {
	content, err := os.ReadFile(path)
	if err != nil {
		return BatteryGuardStatus{}
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[key] = value
		}
	}
	updated, err := strconv.ParseInt(values["updated"], 10, 64)
	if err != nil || updated <= 0 {
		return BatteryGuardStatus{}
	}
	age := now.Sub(time.Unix(updated, 0))
	if age < -30*time.Second || age > 45*time.Second || values["healthy"] != "1" {
		return BatteryGuardStatus{
			Healthy: false,
			Enabled: false,
			Paused:  false,
			Low:     0,
			High:    0,
			Updated: updated,
		}
	}
	var low, high int
	if parsedLow, err := strconv.Atoi(values["low"]); err == nil && parsedLow >= 20 && parsedLow <= 95 {
		low = parsedLow
	}
	if parsedHigh, err := strconv.Atoi(values["high"]); err == nil && parsedHigh >= 20 && parsedHigh <= 95 {
		high = parsedHigh
	}
	if low >= high || low == 0 || high == 0 {
		return BatteryGuardStatus{
			Healthy: false,
			Enabled: false,
			Paused:  false,
			Low:     0,
			High:    0,
			Updated: updated,
		}
	}
	return BatteryGuardStatus{
		Healthy: true,
		Enabled: values["enabled"] == "1",
		Paused:  values["paused"] == "1",
		Low:     low,
		High:    high,
		Updated: updated,
	}
}

func readThermals() []ThermalInfo {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	out := []ThermalInfo{}
	for _, z := range zones {
		typ := strings.TrimSpace(readText(z + "/type"))
		keep := strings.HasPrefix(typ, "cpu") || strings.HasPrefix(typ, "cpuss") || strings.Contains(typ, "gpu") || typ == "battery" || strings.Contains(typ, "xo-therm") || strings.Contains(typ, "skin")
		if !keep {
			continue
		}
		raw := readInt(z + "/temp")
		temp := float64(raw)
		if raw > 1000 {
			temp /= 1000
		}
		if temp < 0 || temp > 150 {
			continue
		}
		out = append(out, ThermalInfo{Name: filepath.Base(z), Type: typ, Temp: round(temp, 1)})
	}
	// Keep positions stable between polls. Sorting by temperature made adjacent
	// sensors swap rows every few seconds and looked like a full-page refresh.
	sort.Slice(out, func(i, j int) bool {
		return thermalZoneNumber(out[i].Name) < thermalZoneNumber(out[j].Name)
	})
	return out
}

func thermalZoneNumber(name string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(name, "thermal_zone"))
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return n
}

func (s *Server) readInterfaces() []InterfaceInfo {
	draft := s.loadNetworkDraft()
	wanInterface, lanInterface := s.activeNetworkInterfaces(draft)
	stats, rates := s.networkSnapshot()
	ifs, _ := net.Interfaces()
	out := []InterfaceInfo{}
	for _, iface := range ifs {
		name := iface.Name
		if !(name == "wlan0" || name == "tailscale0" || strings.HasPrefix(name, "eth") || strings.HasPrefix(name, "ppp") || strings.HasPrefix(name, "br") || strings.HasPrefix(name, "tun")) {
			continue
		}
		addrs, _ := iface.Addrs()
		as := []string{}
		for _, a := range addrs {
			as = append(as, a.String())
		}
		c := stats[name]
		rate := rates[name]
		role := "未分配"
		switch {
		case name == wanInterface:
			role = "WAN 物理上联"
		case name == lanInterface:
			role = "LAN / AP"
		case strings.HasPrefix(name, "ppp"):
			role = "WAN · PPPoE"
		case name == "tailscale0":
			role = "Tailscale"
		case name == "wlan0":
			role = "备用管理网络"
		}
		out = append(out, InterfaceInfo{Name: name, MAC: iface.HardwareAddr.String(), State: strings.TrimSpace(readText("/sys/class/net/" + name + "/operstate")), Carrier: strings.TrimSpace(readText("/sys/class/net/" + name + "/carrier")), Role: role, MTU: iface.MTU, SpeedMbps: readInt("/sys/class/net/" + name + "/speed"), Addresses: as, RxBytes: c.rx, TxBytes: c.tx, RxBps: round(rate.rx, 1), TxBps: round(rate.tx, 1)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// startNetworkSampler decouples interface-rate sampling from dashboard requests.
// Every client therefore observes the same fixed-window metric without changing
// the baseline used by other clients.
func (s *Server) startNetworkSampler() {
	s.sampleNetwork(time.Now(), readNetDev())
	go func() {
		ticker := time.NewTicker(networkSampleInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.sampleNetwork(time.Now(), readNetDev())
		}
	}()
}

// startMetricSampler keeps chart continuity independent of browser tabs. It
// samples only cheap local counters/sysfs values plus one small aria2 RPC; full
// client, service, storage and connection discovery remains on-demand.
func (s *Server) startMetricSampler() {
	current, _ := readAllCPUCounters()
	s.metricsMu.Lock()
	s.metricLastCPU = current
	s.metricsMu.Unlock()

	go func() {
		// Establish a useful CPU delta shortly after startup, then settle on the
		// same five-second cadence used by the charts.
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		<-timer.C
		s.sampleMetrics(time.Now())

		ticker := time.NewTicker(metricSampleInterval)
		defer ticker.Stop()
		for at := range ticker.C {
			s.sampleMetrics(at)
		}
	}()
}

func (s *Server) sampleMetrics(at time.Time) {
	currentCPU, _ := readAllCPUCounters()
	s.metricsMu.Lock()
	previousCPU := s.metricLastCPU
	s.metricLastCPU = currentCPU
	s.metricsMu.Unlock()
	cpuUsage := 0.0
	if previousCPU.total > 0 && currentCPU.total > previousCPU.total {
		deltaTotal := currentCPU.total - previousCPU.total
		deltaIdle := currentCPU.idle - previousCPU.idle
		cpuUsage = 100 * float64(deltaTotal-deltaIdle) / float64(deltaTotal)
	}

	thermals := readThermals()
	cpuTemp := 0.0
	for _, thermal := range thermals {
		if (strings.HasPrefix(thermal.Type, "cpu") || strings.HasPrefix(thermal.Type, "cpuss")) && thermal.Temp > cpuTemp {
			cpuTemp = thermal.Temp
		}
	}
	memory := readMemory()
	memoryUsage := 0.0
	if memory.Total > 0 {
		memoryUsage = 100 * float64(memory.Used) / float64(memory.Total)
	}
	power := s.readPower()
	aria := s.readAria2Global()
	_, rates := s.networkSnapshot()
	draft := s.loadNetworkDraft()
	wanInterface, lanInterface := s.activeNetworkInterfaces(draft)
	wanRate := rates[wanInterface]
	lanRate := rates[lanInterface]

	s.appendMetricPoint(MetricHistoryPoint{
		At:                    at.UnixMilli(),
		CPU:                   round(cpuUsage, 1),
		Temperature:           cpuTemp,
		Memory:                round(memoryUsage, 1),
		WANDown:               round(wanRate.rx, 1),
		WANUp:                 round(wanRate.tx, 1),
		LANDown:               round(lanRate.tx, 1),
		LANUp:                 round(lanRate.rx, 1),
		AriaDown:              float64(aria.DownloadSpeed),
		AriaUp:                float64(aria.UploadSpeed),
		BatteryTemperature:    power.BatteryTemp,
		BatteryVoltage:        power.Voltage,
		BatteryCurrent:        power.Current * 1000,
		BatteryAverageCurrent: power.AverageCurrent * 1000,
		BatteryCoulombCurrent: power.CoulombCurrent * 1000,
		BatteryPower:          power.BatteryWatts,
		BatteryPercent:        power.BatteryPercent,
		ChargeCounter:         power.ChargeCounterMAh,
		USBVoltage:            power.USBVoltage,
		USBCurrent:            power.USBInputCurrent * 1000,
		USBPower:              power.USBInputWatts,
	})
}

func (s *Server) appendMetricPoint(point MetricHistoryPoint) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metricHistory = append(s.metricHistory, point)
	cutoff := point.At - metricHistoryWindow.Milliseconds()
	first := 0
	for first < len(s.metricHistory) && s.metricHistory[first].At < cutoff {
		first++
	}
	if first > 0 {
		s.metricHistory = append([]MetricHistoryPoint(nil), s.metricHistory[first:]...)
	}
}

func (s *Server) metricHistorySnapshot() []MetricHistoryPoint {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	return append([]MetricHistoryPoint(nil), s.metricHistory...)
}

func (s *Server) sampleNetwork(at time.Time, stats map[string]netCounter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.netSamples = append(s.netSamples, timedNetSample{at: at, stats: stats})
	cutoff := at.Add(-networkRateWindow)
	first := 0
	for first+1 < len(s.netSamples) && !s.netSamples[first+1].at.After(cutoff) {
		first++
	}
	if first > 0 {
		s.netSamples = append([]timedNetSample(nil), s.netSamples[first:]...)
	}

	rates := make(map[string]netRate, len(stats))
	baseline := s.netSamples[0]
	elapsed := at.Sub(baseline.at).Seconds()
	if elapsed > 0 {
		for name, current := range stats {
			previous, ok := baseline.stats[name]
			if !ok || current.rx < previous.rx || current.tx < previous.tx {
				continue
			}
			rates[name] = netRate{
				rx: float64(current.rx-previous.rx) / elapsed,
				tx: float64(current.tx-previous.tx) / elapsed,
			}
		}
	}
	s.netStats = stats
	s.netRates = rates
}

func (s *Server) networkSnapshot() (map[string]netCounter, map[string]netRate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.netStats, s.netRates
}

func (s *Server) activeNetworkInterfaces(draft NetworkDraft) (string, string) {
	fields := strings.Fields(readText(filepath.Join(s.data, "admin", "active-interfaces")))
	if len(fields) >= 2 && fields[0] != fields[1] {
		return fields[0], fields[1]
	}
	return draft.WANInterface, draft.LANInterface
}

func readNetDev() map[string]netCounter {
	out := map[string]netCounter{}
	sc := bufio.NewScanner(strings.NewReader(readText("/proc/net/dev")))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		p := strings.Fields(strings.Replace(line, ":", " ", 1))
		if len(p) < 10 {
			continue
		}
		rx, _ := strconv.ParseUint(p[1], 10, 64)
		tx, _ := strconv.ParseUint(p[9], 10, 64)
		out[p[0]] = netCounter{rx: rx, tx: tx}
	}
	return out
}

func normalizeClientMAC(value string) (string, error) {
	mac, err := net.ParseMAC(strings.TrimSpace(value))
	if err != nil || len(mac) != 6 {
		return "", errors.New("MAC 地址格式无效")
	}
	allZero := true
	for _, octet := range mac {
		if octet != 0 {
			allZero = false
			break
		}
	}
	if allZero || mac[0]&1 != 0 {
		return "", errors.New("MAC 地址必须是有效的单播地址")
	}
	return strings.ToLower(mac.String()), nil
}

func validateStaticIPv4(value, lanAddress string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.To4() == nil || strings.Contains(value, ":") {
		return "", errors.New("固定 IP 必须是 IPv4 地址")
	}
	ip = ip.To4()
	routerIP, lanNet, err := net.ParseCIDR(lanAddress)
	if err != nil || routerIP.To4() == nil || !lanNet.Contains(ip) {
		return "", errors.New("固定 IP 必须属于当前 LAN 子网")
	}
	networkIP := routerIP.Mask(lanNet.Mask).To4()
	broadcast := make(net.IP, net.IPv4len)
	for i := range broadcast {
		broadcast[i] = networkIP[i] | ^lanNet.Mask[i]
	}
	if ip.Equal(routerIP) || ip.Equal(networkIP) || ip.Equal(broadcast) {
		return "", errors.New("固定 IP 不能使用路由器、网络或广播地址")
	}
	return ip.String(), nil
}

func validStaticHostname(value string) bool {
	if value == "" {
		return true
	}
	return len(value) <= 63 && regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(value)
}

func parseStaticLeases(text string) map[string]StaticLease {
	out := make(map[string]StaticLease)
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 {
			continue
		}
		mac, err := normalizeClientMAC(fields[0])
		ip := net.ParseIP(strings.TrimSpace(fields[1]))
		if err != nil || ip == nil || ip.To4() == nil {
			continue
		}
		lease := StaticLease{MAC: mac, IP: ip.To4().String()}
		if len(fields) >= 3 && validStaticHostname(strings.TrimSpace(fields[2])) {
			lease.Hostname = strings.TrimSpace(fields[2])
		}
		out[mac] = lease
	}
	return out
}

func (s *Server) readStaticLeases() map[string]StaticLease {
	output, err := s.execSu(shellJoin([]string{clientPolicyManager, "list-static"}))
	if err != nil {
		return map[string]StaticLease{}
	}
	return parseStaticLeases(string(output))
}

func policyCommandError(output []byte, fallback string) string {
	message := strings.TrimSpace(tailLines(string(output), 1))
	if message == "" || len(message) > 240 {
		return fallback
	}
	return message
}

func (s *Server) staticLeaseAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		leases := s.readStaticLeases()
		out := make([]StaticLease, 0, len(leases))
		for _, lease := range leases {
			out = append(out, lease)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
		jsonOut(w, http.StatusOK, out)
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, PUT, DELETE")
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var payload struct {
		MAC      string `json:"mac"`
		IP       string `json:"ip"`
		Hostname string `json:"hostname"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	mac, err := normalizeClientMAC(payload.MAC)
	if err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.clientPolicyMu.Lock()
	defer s.clientPolicyMu.Unlock()

	switch r.Method {
	case http.MethodPut:
		ip, err := validateStaticIPv4(payload.IP, s.loadNetworkDraft().LANAddress)
		if err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		hostname := strings.TrimSpace(payload.Hostname)
		for _, client := range s.readClients() {
			clientMAC, _ := normalizeClientMAC(client.MAC)
			if client.IP == ip && clientMAC != mac {
				jsonOut(w, http.StatusConflict, map[string]string{"error": "该 IP 已被其他客户端或静态租约占用"})
				return
			}
			if hostname == "" && clientMAC == mac && validStaticHostname(client.Hostname) {
				hostname = client.Hostname
			}
		}
		if !validStaticHostname(hostname) {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "主机名格式无效"})
			return
		}
		command := shellJoin([]string{clientPolicyManager, "set-static", mac, ip, hostname})
		if output, err := s.execSu(command); err != nil {
			log.Printf("static lease set failed: %s", tailLines(string(output), 10))
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": policyCommandError(output, "设置静态租约失败，原配置未改变")})
			return
		}
		jsonOut(w, http.StatusOK, map[string]any{"ok": true, "mac": mac, "ip": ip})
	case http.MethodDelete:
		command := shellJoin([]string{clientPolicyManager, "clear-static", mac})
		if output, err := s.execSu(command); err != nil {
			log.Printf("static lease clear failed: %s", tailLines(string(output), 10))
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": policyCommandError(output, "清除静态租约失败，原配置未改变")})
			return
		}
		jsonOut(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
	}
}

func validClientDisplayName(value string) bool {
	if value == "" || len([]rune(value)) > 64 {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (s *Server) clientNamesPath() string {
	return filepath.Join(s.data, "admin", "client-names.json")
}

func (s *Server) readClientNames() map[string]string {
	names := make(map[string]string)
	encoded, err := os.ReadFile(s.clientNamesPath())
	if err != nil || json.Unmarshal(encoded, &names) != nil {
		return make(map[string]string)
	}
	for mac, name := range names {
		normalized, err := normalizeClientMAC(mac)
		trimmed := strings.TrimSpace(name)
		if err != nil || !validClientDisplayName(trimmed) {
			delete(names, mac)
			continue
		}
		if normalized != mac {
			delete(names, mac)
			names[normalized] = trimmed
		}
	}
	return names
}

func (s *Server) saveClientNames(names map[string]string) error {
	path := s.clientNamesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Server) clientNameAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "PUT, DELETE")
		jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		MAC  string `json:"mac"`
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	mac, err := normalizeClientMAC(payload.MAC)
	if err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(payload.Name)
	if r.Method == http.MethodPut && !validClientDisplayName(name) {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "设备名称不能为空、不能包含控制字符，且最长为 64 个字符"})
		return
	}

	s.clientPolicyMu.Lock()
	defer s.clientPolicyMu.Unlock()
	names := s.readClientNames()
	if r.Method == http.MethodDelete {
		delete(names, mac)
	} else {
		names[mac] = name
	}
	if err := s.saveClientNames(names); err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "保存设备名称失败"})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true, "mac": mac, "name": name})
}

func (s *Server) readClients() []ClientInfo {
	draft := s.loadNetworkDraft()
	staticLeases := s.readStaticLeases()
	_, lanInterface := s.activeNetworkInterfaces(draft)
	if lanInterface == "" {
		lanInterface = "eth1"
	}
	var lanNet *net.IPNet
	if _, parsed, err := net.ParseCIDR(draft.LANAddress); err == nil {
		lanNet = parsed
	}
	ownIPs := map[string]bool{}
	ownMACs := map[string]bool{}
	if ifs, err := net.Interfaces(); err == nil {
		for _, iface := range ifs {
			if mac := strings.ToLower(iface.HardwareAddr.String()); mac != "" {
				ownMACs[mac] = true
			}
			if addrs, err := iface.Addrs(); err == nil {
				for _, addr := range addrs {
					if ip, _, err := net.ParseCIDR(addr.String()); err == nil {
						ownIPs[ip.String()] = true
					}
				}
			}
		}
	}
	// Keep DHCP identity by IP. Some wireless APs in bridge/repeater mode proxy
	// ARP and expose the AP's MAC for every downstream station; keying live
	// neighbors by MAC therefore collapses several clients into one and can
	// leave an actively connected DHCP client looking offline.
	byIP := map[string]ClientInfo{}
	now := time.Now().Unix()
	leasePaths := []string{filepath.Join(s.data, "dnsmasq", "dnsmasq.leases"), "/data/misc/dhcp/dnsmasq.leases"}
	for _, p := range leasePaths {
		for _, line := range strings.Split(readText(p), "\n") {
			f := strings.Fields(line)
			if len(f) >= 4 {
				exp, _ := strconv.ParseInt(f[0], 10, 64)
				ip := net.ParseIP(f[2])
				mac := strings.ToLower(f[1])
				if (exp > 0 && exp < now) || ownIPs[f[2]] || ownMACs[mac] || (lanNet != nil && (ip == nil || !lanNet.Contains(ip))) {
					continue
				}
				hostname := f[3]
				if hostname == "OnePlus-7Pro" {
					continue
				}
				if hostname == "*" {
					hostname = "未知设备"
				}
				byIP[f[2]] = ClientInfo{IP: f[2], MAC: mac, Hostname: hostname, Interface: lanInterface, State: "租约", Source: "DHCP", Expires: exp}
			}
		}
	}
	arp := readText("/proc/net/arp")
	lines := strings.Split(arp, "\n")
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) < 6 || f[2] == "0x0" || f[3] == "00:00:00:00:00:00" || f[5] != lanInterface || ownIPs[f[0]] || ownMACs[strings.ToLower(f[3])] {
			continue
		}
		ip := net.ParseIP(f[0])
		if lanNet != nil && (ip == nil || !lanNet.Contains(ip)) {
			continue
		}
		mac := strings.ToLower(f[3])
		c := byIP[f[0]]
		c.IP = f[0]
		if c.MAC == "" {
			c.MAC = mac
		}
		c.Interface = f[5]
		if c.Hostname == "" {
			c.Hostname = "未知设备"
		}
		if c.Source == "" {
			c.Source = "邻居缓存"
		} else if c.Source == "DHCP" && c.MAC != mac {
			c.Source = "DHCP + AP 桥接"
		}
		c.State = "在线"
		byIP[c.IP] = c
	}
	applyStaticLeasesToClients(byIP, staticLeases, lanInterface)
	applyClientNamesToClients(byIP, s.readClientNames())
	clientStats := s.collectClientTrafficStats()
	s.updateClientTraffic(time.Now(), clientStats)
	trafficStats, trafficRates := s.getClientTrafficSnapshot()

	out := make([]ClientInfo, 0, len(byIP))
	for _, c := range byIP {
		if stat, ok := trafficStats[c.IP]; ok {
			c.RxBytes = stat.rx
			c.TxBytes = stat.tx
		}
		if rate, ok := trafficRates[c.IP]; ok {
			c.RxBps = round(rate.rx, 1)
			c.TxBps = round(rate.tx, 1)
		}
		if c.RxBps > 0 || c.TxBps > 0 {
			c.State = "在线"
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out
}

func applyClientNamesToClients(byIP map[string]ClientInfo, names map[string]string) {
	for ip, client := range byIP {
		mac, err := normalizeClientMAC(client.MAC)
		if err != nil {
			continue
		}
		if name, ok := names[mac]; ok && validClientDisplayName(name) {
			client.Hostname = name
			client.CustomName = true
			byIP[ip] = client
		}
	}
}

// applyStaticLeasesToClients treats the configured static lease as the stable
// client identity. This matters behind bridge/repeater APs which may proxy ARP
// and expose their own MAC address for a downstream client. Match the reserved
// IP first, then fall back to MAC when the client has moved to another address.
func applyStaticLeasesToClients(byIP map[string]ClientInfo, staticLeases map[string]StaticLease, lanInterface string) {
	for mac, lease := range staticLeases {
		if client, ok := byIP[lease.IP]; ok {
			observedMAC, _ := normalizeClientMAC(client.MAC)
			client.StaticLease = true
			client.StaticIP = lease.IP
			if lease.Hostname != "" {
				client.Hostname = lease.Hostname
			}
			if client.Source == "邻居缓存" && observedMAC != "" && observedMAC != mac {
				client.MAC = mac
				client.Source = "静态 DHCP + AP 桥接"
			}
			byIP[lease.IP] = client
			continue
		}

		found := false
		for ip, client := range byIP {
			clientMAC, _ := normalizeClientMAC(client.MAC)
			if clientMAC != mac {
				continue
			}
			client.StaticLease = true
			client.StaticIP = lease.IP
			if lease.Hostname != "" {
				client.Hostname = lease.Hostname
			}
			byIP[ip] = client
			found = true
		}
		if !found {
			hostname := lease.Hostname
			if hostname == "" {
				hostname = "固定 IP 客户端"
			}
			if _, occupied := byIP[lease.IP]; !occupied {
				byIP[lease.IP] = ClientInfo{IP: lease.IP, MAC: mac, Hostname: hostname, Interface: lanInterface, State: "静态租约", Source: "静态 DHCP", StaticLease: true, StaticIP: lease.IP}
			}
		}
	}
}

func parseConntrackClientTraffic(text string, stats map[string]netCounter) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "bytes=") {
			continue
		}
		fields := strings.Fields(line)
		var srcIP string
		var origBytes, replyBytes uint64
		foundOrigBytes := false

		for _, f := range fields {
			if strings.HasPrefix(f, "src=") && srcIP == "" {
				srcIP = strings.TrimPrefix(f, "src=")
			} else if strings.HasPrefix(f, "bytes=") {
				b, _ := strconv.ParseUint(strings.TrimPrefix(f, "bytes="), 10, 64)
				if !foundOrigBytes {
					origBytes = b
					foundOrigBytes = true
				} else {
					replyBytes = b
				}
			}
		}

		if srcIP != "" && strings.HasPrefix(srcIP, "192.168.") {
			curr := stats[srcIP]
			if origBytes > curr.rx {
				curr.rx = origBytes
			}
			if replyBytes > curr.tx {
				curr.tx = replyBytes
			}
			stats[srcIP] = curr
		}
	}
}

func (s *Server) collectClientTrafficStats() map[string]netCounter {
	stats := make(map[string]netCounter)
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	data, _ := s.getMihomoConnections(ctx, mihomoConnectionsCacheTTL)
	if data == nil {
		data = &mihomoRawConnectionsResponse{}
	}
	for _, c := range data.Connections {
		ip := c.Metadata.SourceIP
		if ip == "" || ip == "127.0.0.1" {
			continue
		}
		curr := stats[ip]
		curr.rx += c.Upload
		curr.tx += c.Download
		stats[ip] = curr
	}

	if conntrackText := readText("/proc/net/nf_conntrack"); conntrackText != "" {
		parseConntrackClientTraffic(conntrackText, stats)
	}

	return stats
}

func (s *Server) updateClientTraffic(at time.Time, stats map[string]netCounter) {
	s.clientTrafficMu.Lock()
	defer s.clientTrafficMu.Unlock()

	if s.clientTrafficStats == nil {
		s.clientTrafficStats = make(map[string]netCounter)
	}
	if s.clientTrafficRates == nil {
		s.clientTrafficRates = make(map[string]netRate)
	}

	for ip, counter := range stats {
		prev := s.clientTrafficStats[ip]
		if counter.rx < prev.rx {
			counter.rx = prev.rx
		}
		if counter.tx < prev.tx {
			counter.tx = prev.tx
		}
		s.clientTrafficStats[ip] = counter
	}

	s.clientTrafficSamples = append(s.clientTrafficSamples, timedClientSample{at: at, stats: stats})
	cutoff := at.Add(-networkRateWindow)
	first := 0
	for first+1 < len(s.clientTrafficSamples) && !s.clientTrafficSamples[first+1].at.After(cutoff) {
		first++
	}
	if first > 0 {
		s.clientTrafficSamples = append([]timedClientSample(nil), s.clientTrafficSamples[first:]...)
	}

	rates := make(map[string]netRate, len(stats))
	if len(s.clientTrafficSamples) > 1 {
		baseline := s.clientTrafficSamples[0]
		elapsed := at.Sub(baseline.at).Seconds()
		if elapsed > 0 {
			for ip, current := range stats {
				previous, ok := baseline.stats[ip]
				if !ok || current.rx < previous.rx || current.tx < previous.tx {
					continue
				}
				rates[ip] = netRate{
					rx: float64(current.rx-previous.rx) / elapsed,
					tx: float64(current.tx-previous.tx) / elapsed,
				}
			}
		}
	}
	s.clientTrafficRates = rates
}

func (s *Server) getClientTrafficSnapshot() (map[string]netCounter, map[string]netRate) {
	s.clientTrafficMu.Lock()
	defer s.clientTrafficMu.Unlock()

	stats := make(map[string]netCounter, len(s.clientTrafficStats))
	for k, v := range s.clientTrafficStats {
		stats[k] = v
	}
	rates := make(map[string]netRate, len(s.clientTrafficRates))
	for k, v := range s.clientTrafficRates {
		rates[k] = v
	}
	return stats, rates
}

func (s *Server) readServices() []ServiceInfo {
	names := make([]string, 0, len(allowedServices))
	for n := range allowedServices {
		names = append(names, n)
	}
	sort.Strings(names)
	args := []string{"status"}
	for _, name := range names {
		args = append(args, filepath.Join(s.stack, "services", name))
	}
	lines := strings.Split(commandText(filepath.Join(s.prefix, "bin", "sv"), args...), "\n")
	out := []ServiceInfo{}
	for index, name := range names {
		meta := allowedServices[name]
		dir := filepath.Join(s.stack, "services", name)
		state := "未安装"
		pid, up := 0, 0
		if _, err := os.Stat(dir); err == nil {
			text := ""
			if index < len(lines) {
				text = lines[index]
			}
			state = "停止"
			if strings.HasPrefix(text, "run:") {
				state = "运行中"
			}
			if strings.HasPrefix(text, "down:") {
				state = "已禁用"
			}
			re := regexp.MustCompile(`\(pid ([0-9]+)\) ([0-9]+)s`)
			if m := re.FindStringSubmatch(text); len(m) == 3 {
				pid, _ = strconv.Atoi(m[1])
				up, _ = strconv.Atoi(m[2])
			}
		}
		web := ""
		if meta.Port > 0 {
			web = fmt.Sprintf(":%d", meta.Port)
		}
		out = append(out, ServiceInfo{Name: name, Label: meta.Label, State: state, PID: pid, UptimeSec: up, Port: meta.Port, WebURL: web})
	}
	s.enrichServiceUpdates(out)
	return out
}

func (s *Server) aria2RPC(method string, args []any, dst any) error {
	secret := ""
	for _, line := range strings.Split(readText(filepath.Join(s.data, "aria2", "aria2.conf")), "\n") {
		if strings.HasPrefix(line, "rpc-secret=") {
			secret = strings.TrimSpace(strings.TrimPrefix(line, "rpc-secret="))
			break
		}
	}
	params := aria2RPCParams(secret, args)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "router-admin", "method": method, "params": params})
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:6800/jsonrpc", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 900 * time.Millisecond}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("aria2 RPC HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return errors.New("aria2 RPC error")
	}
	return json.Unmarshal(envelope.Result, dst)
}

func aria2RPCParams(secret string, args []any) []any {
	params := make([]any, 0, len(args)+1)
	if secret != "" {
		params = append(params, "token:"+secret)
	}
	return append(params, args...)
}

func ariaUint(value string) uint64 {
	n, _ := strconv.ParseUint(value, 10, 64)
	return n
}

func (s *Server) readAria2Global() Aria2Info {
	info := Aria2Info{Tasks: []Aria2Task{}}
	var stat struct {
		DownloadSpeed, UploadSpeed        string
		NumActive, NumWaiting, NumStopped string
	}
	if err := s.aria2RPC("aria2.getGlobalStat", nil, &stat); err != nil {
		return info
	}
	info.Online = true
	info.DownloadSpeed = ariaUint(stat.DownloadSpeed)
	info.UploadSpeed = ariaUint(stat.UploadSpeed)
	info.NumActive, _ = strconv.Atoi(stat.NumActive)
	info.NumWaiting, _ = strconv.Atoi(stat.NumWaiting)
	info.NumStopped, _ = strconv.Atoi(stat.NumStopped)
	return info
}

func (s *Server) readAria2() Aria2Info {
	info := s.readAria2Global()
	if !info.Online {
		return info
	}

	var active []struct {
		GID, Status, TotalLength, CompletedLength, DownloadSpeed, UploadSpeed, Connections string
		Files                                                                              []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	fields := []string{"gid", "status", "totalLength", "completedLength", "downloadSpeed", "uploadSpeed", "connections", "files"}
	if s.aria2RPC("aria2.tellActive", []any{fields}, &active) == nil {
		for _, task := range active {
			name := task.GID
			if len(task.Files) > 0 && task.Files[0].Path != "" {
				name = filepath.Base(task.Files[0].Path)
			}
			connections, _ := strconv.Atoi(task.Connections)
			info.Tasks = append(info.Tasks, Aria2Task{GID: task.GID, Name: name, Status: task.Status, TotalLength: ariaUint(task.TotalLength), CompletedLength: ariaUint(task.CompletedLength), DownloadSpeed: ariaUint(task.DownloadSpeed), UploadSpeed: ariaUint(task.UploadSpeed), Connections: connections})
		}
	}
	var waiting []struct{ Status string }
	if s.aria2RPC("aria2.tellWaiting", []any{0, 100, []string{"status"}}, &waiting) == nil {
		for _, task := range waiting {
			if task.Status == "paused" {
				info.NumPaused++
			}
		}
	}
	return info
}

func (s *Server) readStorage() []StorageInfo {
	s.mu.Lock()
	if len(s.storageCache) > 0 && time.Since(s.storageAt) < 15*time.Second {
		cached := append([]StorageInfo(nil), s.storageCache...)
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()
	out := collectStorage()
	s.mu.Lock()
	s.storageCache = append([]StorageInfo(nil), out...)
	s.storageAt = time.Now()
	s.mu.Unlock()
	return out
}

func collectStorage() []StorageInfo {
	mounts := parseMounts()
	out := []StorageInfo{}
	seen := map[string]bool{}
	for _, m := range mounts {
		externalMount := (strings.HasPrefix(m.mount, "/storage/") && !strings.HasPrefix(m.mount, "/storage/emulated")) || strings.HasPrefix(m.mount, "/mnt/media_rw/")
		if m.mount != "/data" && !externalMount {
			continue
		}
		if seen[m.mount] {
			continue
		}
		seen[m.mount] = true
		total, used, free, ok := diskSpace(m.mount)
		if !ok {
			continue
		}
		name := "内部存储"
		external := false
		model := "UFS"
		if externalMount {
			name = filepath.Base(m.mount)
			external = true
			model = findUSBStorageModel()
		}
		pct := 0.0
		if total > 0 {
			pct = 100 * float64(used) / float64(total)
		}
		out = append(out, StorageInfo{Name: name, Mount: m.mount, Filesystem: m.fs, Model: model, Total: total, Used: used, Free: free, UsedPercent: round(pct, 1), External: external})
	}
	return out
}

func diskSpace(mount string) (uint64, uint64, uint64, bool) {
	out := commandText("/data/data/com.termux/files/usr/bin/df", "-k", mount)
	if strings.Contains(out, "Permission denied") {
		valid := regexp.MustCompile(`^/(data|storage/[A-Za-z0-9_-]+|mnt/media_rw/[A-Za-z0-9_-]+)$`)
		if valid.MatchString(mount) {
			out = commandText("/product/bin/su", "-c", "/data/data/com.termux/files/usr/bin/df -k "+mount)
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, 0, 0, false
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, 0, 0, false
	}
	total, e1 := strconv.ParseUint(fields[1], 10, 64)
	used, e2 := strconv.ParseUint(fields[2], 10, 64)
	free, e3 := strconv.ParseUint(fields[3], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, 0, 0, false
	}
	return total * 1024, used * 1024, free * 1024, true
}

type mountInfo struct{ source, mount, fs string }

func parseMounts() []mountInfo {
	out := []mountInfo{}
	sc := bufio.NewScanner(strings.NewReader(readText("/proc/mounts")))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 3 {
			out = append(out, mountInfo{f[0], strings.ReplaceAll(f[1], "\\040", " "), f[2]})
		}
	}
	return out
}
func findUSBStorageModel() string {
	blocks, _ := filepath.Glob("/sys/class/block/sd*")
	for _, b := range blocks {
		if _, e := os.Stat(filepath.Join(b, "partition")); e == nil {
			continue
		}
		real, _ := filepath.EvalSymlinks(b)
		if strings.Contains(real, "/usb") {
			m := strings.TrimSpace(readText(filepath.Join(b, "device", "model")))
			if m != "" {
				return m
			}
		}
	}
	return "USB 存储"
}

func readUSB() []USBInfo {
	dirs, _ := filepath.Glob("/sys/bus/usb/devices/*")
	out := []USBInfo{}
	for _, d := range dirs {
		vid := strings.TrimSpace(readText(d + "/idVendor"))
		if vid == "" {
			continue
		}
		out = append(out, USBInfo{VendorID: vid, ProductID: strings.TrimSpace(readText(d + "/idProduct")), Manufacturer: strings.TrimSpace(readText(d + "/manufacturer")), Product: strings.TrimSpace(readText(d + "/product")), Speed: strings.TrimSpace(readText(d + "/speed"))})
	}
	return out
}

func (s *Server) serviceAction(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var b struct{ Name, Action string }
	if decodeJSON(r, &b) != nil {
		jsonOut(w, 400, map[string]string{"error": "请求错误"})
		return
	}
	if _, ok := allowedServices[b.Name]; !ok || b.Name == "router-admin" {
		jsonOut(w, 400, map[string]string{"error": "不允许操作该服务"})
		return
	}
	if b.Action != "start" && b.Action != "stop" && b.Action != "restart" {
		jsonOut(w, 400, map[string]string{"error": "无效操作"})
		return
	}
	if b.Name == "mihomo" {
		disabled := filepath.Join(s.data, "mihomo", "disabled")
		if b.Action == "stop" {
			// Transparent LAN forwarding depends on the local Mihomo listener.
			// Withdraw it first so household traffic returns to the direct path.
			if _, err := s.setWholeHomeProxy(false); err != nil {
				jsonOut(w, 500, map[string]string{"error": "无法先关闭全屋代理，Mihomo 保持运行"})
				return
			}
			if err := os.MkdirAll(filepath.Dir(disabled), 0700); err != nil || os.WriteFile(disabled, []byte("disabled by router admin\n"), 0600) != nil {
				jsonOut(w, 500, map[string]string{"error": "无法保存 Mihomo 停止状态"})
				return
			}
		} else if err := os.Remove(disabled); err != nil && !os.IsNotExist(err) {
			jsonOut(w, 500, map[string]string{"error": "无法清除 Mihomo 停止状态"})
			return
		}
	}
	svAction := map[string]string{"start": "up", "stop": "down", "restart": "restart"}[b.Action]
	cmd := exec.Command(filepath.Join(s.prefix, "bin", "sv"), svAction, filepath.Join(s.stack, "services", b.Name))
	out, err := cmd.CombinedOutput()
	if err != nil {
		jsonOut(w, 500, map[string]string{"error": strings.TrimSpace(string(out))})
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	name := r.URL.Query().Get("service")
	if _, ok := allowedServices[name]; !ok {
		jsonOut(w, 400, map[string]string{"error": "无效服务"})
		return
	}
	path := filepath.Join(s.stack, "services", name, "log", "main", "current")
	text := tailLines(readText(path), 160)
	jsonOut(w, 200, map[string]string{"service": name, "log": text})
}

var (
	kernelLogTime = regexp.MustCompile(`^\[[^]]+\]\s+\[(\d{4})(\d{2})(\d{2})_(\d{2}:\d{2}:\d{2})(?:\.\d+)?\]@\d+\s*`)
	plainLogTime  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[ _](\d{2}:\d{2}:\d{2})(?:\.\d+)?\s*`)
	slashLogTime  = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2})[ _](\d{2}:\d{2}:\d{2})(?:\.\d+)?\s*`)
	logURL        = regexp.MustCompile(`https?://[^\s"'<>]+`)
	logSecret     = regexp.MustCompile(`(?i)(rpc-secret|authorization|password|secret|token)(\s*[=:]\s*)[^\s,;]+`)
)

var systemLogLabels = map[string]string{
	"events":   "异常事件",
	"kernel":   "内核日志",
	"storage":  "存储监控",
	"network":  "网络服务",
	"aria2":    "aria2",
	"openlist": "OpenList",
	"rclone":   "Rclone",
	"admin":    "管理后台",
}

func (s *Server) systemLogs(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source == "" {
		source = "events"
	}
	label, ok := systemLogLabels[source]
	if !ok {
		jsonOut(w, http.StatusBadRequest, map[string]string{"error": "无效日志来源"})
		return
	}
	limit := 400
	if requested, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && requested > 0 {
		limit = requested
	}
	if limit < 50 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	entries, err := s.readSystemLogs(source, limit)
	if err != nil {
		log.Printf("read %s logs: %v", source, err)
		jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "日志读取失败"})
		return
	}
	response := SystemLogResponse{Source: source, Label: label, UpdatedAt: time.Now().Format(time.RFC3339), Entries: entries}
	for _, entry := range entries {
		switch entry.Level {
		case "error":
			response.Error++
		case "warning":
			response.Warning++
		default:
			response.Info++
		}
	}
	jsonOut(w, http.StatusOK, response)
}

func (s *Server) readSystemLogs(source string, limit int) ([]SystemLogEntry, error) {
	if source == "events" {
		entries := []SystemLogEntry{}
		for _, name := range []string{"kernel", "storage", "network", "aria2", "openlist", "rclone", "admin"} {
			var part []SystemLogEntry
			var err error
			if name == "kernel" {
				part, err = s.readKernelEventLogs()
			} else {
				part, err = s.readSystemLogs(name, limit)
			}
			if err != nil {
				continue
			}
			for _, entry := range part {
				if entry.Level != "info" {
					entries = append(entries, entry)
				}
			}
		}
		entries = deduplicateSystemLogs(entries)
		sortSystemLogs(entries)
		if len(entries) > limit {
			entries = entries[:limit]
		}
		return entries, nil
	}

	text, err := s.readSystemLogText(source)
	if err != nil {
		return nil, err
	}
	parseText := text
	if source != "kernel" {
		parseText = tailLines(text, limit*4)
	}
	entries := parseSystemLogText(source, parseText)
	if source == "storage" {
		entries = append(entries, currentStorageAlerts()...)
	}
	sortSystemLogs(entries)
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *Server) readKernelEventLogs() ([]SystemLogEntry, error) {
	command := "/system/bin/dmesg -T | /system/bin/grep -Ei 'I/O error|Buffer I/O|USB disconnect|DID_NO_CONNECT|read-only|corrupt|mount failed|timed out|timeout|connection reset|reset SuperSpeed|no space left|HTTP 429|failed'"
	out, err := s.execSu(command)
	if err != nil && len(out) == 0 {
		return []SystemLogEntry{}, nil
	}
	return parseSystemLogText("kernel", string(out)), nil
}

func (s *Server) readSystemLogText(source string) (string, error) {
	switch source {
	case "kernel":
		out, err := s.execSu("/system/bin/dmesg -T")
		return string(out), err
	case "storage":
		out, err := s.execSu("/system/bin/cat /data/adb/router-storage/storage.log")
		if err != nil && len(out) == 0 {
			return "", nil
		}
		return string(out), nil
	case "network":
		var all strings.Builder
		for _, command := range []string{
			"/system/bin/cat /data/adb/router-network/service.log",
			"/system/bin/cat /data/adb/router-network/router-network.log",
			"/system/bin/cat /data/adb/router-network/pppd.log",
		} {
			out, _ := s.execSu(command)
			if len(out) > 0 {
				all.Write(out)
				if out[len(out)-1] != '\n' {
					all.WriteByte('\n')
				}
			}
		}
		return all.String(), nil
	case "aria2", "openlist", "rclone", "admin":
		service := source
		if source == "admin" {
			service = "router-admin"
		}
		return readText(filepath.Join(s.stack, "services", service, "log", "main", "current")), nil
	default:
		return "", errors.New("unsupported log source")
	}
}

func parseSystemLogText(source, text string) []SystemLogEntry {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	entries := make([]SystemLogEntry, 0, len(lines))
	for index := len(lines) - 1; index >= 0; index-- {
		message := strings.TrimSpace(lines[index])
		if message == "" {
			continue
		}
		stamp := ""
		if match := kernelLogTime.FindStringSubmatch(message); len(match) == 5 {
			stamp = fmt.Sprintf("%s-%s-%s %s", match[1], match[2], match[3], match[4])
			message = strings.TrimSpace(kernelLogTime.ReplaceAllString(message, ""))
		} else if match := plainLogTime.FindStringSubmatch(message); len(match) == 3 {
			stamp = match[1] + " " + match[2]
			message = strings.TrimSpace(plainLogTime.ReplaceAllString(message, ""))
		} else if match := slashLogTime.FindStringSubmatch(message); len(match) == 5 {
			stamp = fmt.Sprintf("%s-%s-%s %s", match[1], match[2], match[3], match[4])
			message = strings.TrimSpace(slashLogTime.ReplaceAllString(message, ""))
		}
		message = redactSystemLog(message)
		entries = append(entries, SystemLogEntry{Time: stamp, Level: systemLogLevel(message), Source: systemLogLabels[source], Message: message})
	}
	return entries
}

func redactSystemLog(message string) string {
	message = logSecret.ReplaceAllString(message, "$1$2[已隐藏]")
	return logURL.ReplaceAllStringFunc(message, func(raw string) string {
		if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
			return raw[:cut] + "?[查询参数已隐藏]"
		}
		return raw
	})
}

func systemLogLevel(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "driver failed to report") || strings.Contains(lower, "avc: denied") {
		return "info"
	}
	for _, marker := range []string{
		"i/o error", "buffer i/o", "input/output error", "read-only file system",
		"no space left", "corrupt", "synchronize cache(10) failed", "did_no_connect",
		"errorcode=", "http 429", "status=429",
	} {
		if strings.Contains(lower, marker) {
			return "error"
		}
	}
	for _, marker := range []string{
		"usb disconnect", "mount failed", "disk absent", "not mounted", "device or resource busy",
		"timed out", "timeout", "connection reset", "reset super", "failed", "failure", "retry",
	} {
		if strings.Contains(lower, marker) {
			return "warning"
		}
	}
	return "info"
}

func currentStorageAlerts() []SystemLogEntry {
	now := time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")
	byMount := make(map[string][]mountInfo)
	for _, mount := range parseMounts() {
		if strings.HasPrefix(mount.mount, "/mnt/media_rw/") {
			byMount[mount.mount] = append(byMount[mount.mount], mount)
		}
	}
	alerts := []SystemLogEntry{}
	for mount, records := range byMount {
		seenSource := make(map[string]bool)
		for _, record := range records {
			if seenSource[record.source] {
				continue
			}
			seenSource[record.source] = true
			if strings.HasPrefix(record.source, "/dev/block/sd") {
				if _, err := os.Stat(record.source); err != nil {
					alerts = append(alerts, SystemLogEntry{Time: now, Level: "error", Source: systemLogLabels["storage"], Message: fmt.Sprintf("挂载记录 %s 指向已不存在的设备 %s，可能是掉盘后遗留挂载", mount, record.source)})
				}
			}
		}
		if len(seenSource) > 1 {
			sources := make([]string, 0, len(seenSource))
			for source := range seenSource {
				sources = append(sources, source)
			}
			sort.Strings(sources)
			alerts = append(alerts, SystemLogEntry{Time: now, Level: "warning", Source: systemLogLabels["storage"], Message: fmt.Sprintf("挂载点 %s 同时存在多个设备来源：%s", mount, strings.Join(sources, "、"))})
		}
	}
	return alerts
}

func deduplicateSystemLogs(entries []SystemLogEntry) []SystemLogEntry {
	seen := make(map[string]bool, len(entries))
	out := make([]SystemLogEntry, 0, len(entries))
	for _, entry := range entries {
		key := entry.Time + "\x00" + entry.Level + "\x00" + entry.Source + "\x00" + entry.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	return out
}

func sortSystemLogs(entries []SystemLogEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Time == entries[j].Time {
			return logLevelWeight(entries[i].Level) > logLevelWeight(entries[j].Level)
		}
		return entries[i].Time > entries[j].Time
	})
}

func logLevelWeight(level string) int {
	switch level {
	case "error":
		return 2
	case "warning":
		return 1
	default:
		return 0
	}
}

func parseTailscaleState(text string) TailscaleState {
	values := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	return TailscaleState{
		Installed:       values["installed"] == "yes",
		Enabled:         values["enabled"] == "yes",
		Supervisor:      values["supervisor"] == "running",
		Daemon:          values["daemon"] == "running",
		Online:          values["online"] == "yes",
		LoginRequired:   values["login_required"] == "yes",
		LoginPending:    values["login_pending"] == "yes",
		Backend:         values["backend"],
		Hostname:        values["hostname"],
		IP:              values["ip"],
		AdvertiseRoutes: values["advertise_routes"],
		PrimaryRoutes:   values["primary_routes"],
		RouteStatus:     values["route_status"],
		AuthURL:         values["auth_url"],
	}
}

func (s *Server) tailscaleCommand(action string) (string, error) {
	su := filepath.Join(s.prefix, "bin", "su")
	out, err := exec.Command(su, "-c", tailscaleControl+" "+action).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(tailLines(string(out), 12))
		if message == "" {
			message = "Tailscale 操作失败"
		}
		return "", errors.New(message)
	}
	return string(out), nil
}

func (s *Server) readTailscaleState() (TailscaleState, error) {
	out, err := s.tailscaleCommand("status")
	if err != nil {
		return TailscaleState{}, err
	}
	return parseTailscaleState(out), nil
}

func (s *Server) tailscaleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		state, err := s.readTailscaleState()
		if err != nil {
			jsonOut(w, 503, map[string]string{"error": err.Error()})
			return
		}
		jsonOut(w, 200, state)
		return
	}
	if !method(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if decodeJSON(r, &body) != nil {
		jsonOut(w, 400, map[string]string{"error": "请求格式错误"})
		return
	}
	allowed := map[string]bool{"start": true, "stop": true, "restart": true, "login": true, "reauth": true}
	if !allowed[body.Action] {
		jsonOut(w, 400, map[string]string{"error": "无效的 Tailscale 操作"})
		return
	}
	out, err := s.tailscaleCommand(body.Action)
	if err != nil {
		jsonOut(w, 500, map[string]string{"error": err.Error()})
		return
	}
	state, stateErr := s.readTailscaleState()
	if stateErr != nil {
		jsonOut(w, 500, map[string]string{"error": stateErr.Error()})
		return
	}
	if actionState := parseTailscaleState(out); actionState.AuthURL != "" {
		state.AuthURL = actionState.AuthURL
	}
	jsonOut(w, 200, state)
}

func (s *Server) subscriptionInfo() SubscriptionInfo {
	info := SubscriptionInfo{AutoUpdateHours: 6}
	urlPath := filepath.Join(s.data, "mihomo", "subscription.url")
	raw := strings.TrimSpace(readText(urlPath))
	if raw != "" {
		info.Configured = true
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			info.Source = parsed.Scheme + "://" + parsed.Host + "/…"
		} else {
			info.Source = "已配置（地址已隐藏）"
		}
	}
	if stat, err := os.Stat(filepath.Join(s.data, "mihomo", "config.yaml")); err == nil {
		info.LastUpdated = stat.ModTime().Format(time.RFC3339)
	}
	return info
}

func validateSubscriptionURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 8192 {
		return "", errors.New("订阅地址为空或过长")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("订阅地址必须是有效的 HTTP 或 HTTPS URL")
	}
	return raw, nil
}

func (s *Server) mihomoSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.subscriptionMu.Lock()
		defer s.subscriptionMu.Unlock()
	}
	switch r.Method {
	case "GET":
		jsonOut(w, 200, s.subscriptionInfo())
	case "PUT":
		var body struct {
			URL string `json:"url"`
		}
		if decodeJSON(r, &body) != nil {
			jsonOut(w, 400, map[string]string{"error": "请求格式错误"})
			return
		}
		raw, err := validateSubscriptionURL(body.URL)
		if err != nil {
			jsonOut(w, 400, map[string]string{"error": err.Error()})
			return
		}
		dir := filepath.Join(s.data, "mihomo")
		if err := os.MkdirAll(dir, 0700); err != nil {
			jsonOut(w, 500, map[string]string{"error": "无法创建订阅目录"})
			return
		}
		target := filepath.Join(dir, "subscription.url")
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, []byte(raw+"\n"), 0600); err != nil || os.Rename(tmp, target) != nil {
			_ = os.Remove(tmp)
			jsonOut(w, 500, map[string]string{"error": "保存订阅失败"})
			return
		}
		jsonOut(w, 200, s.subscriptionInfo())
	case "POST":
		if !s.subscriptionInfo().Configured {
			jsonOut(w, 400, map[string]string{"error": "请先添加订阅地址"})
			return
		}
		updater := filepath.Join(s.stack, "bin", "mihomo-update-subscription")
		out, err := exec.Command(updater).CombinedOutput()
		if err != nil {
			log.Printf("mihomo subscription update failed: %s", sanitizeLogSecrets(tailLines(string(out), 30)))
			jsonOut(w, 500, map[string]string{"error": "订阅更新失败，旧配置已保留；可查看 Mihomo 日志"})
			return
		}
		jsonOut(w, 200, map[string]any{"ok": true, "subscription": s.subscriptionInfo()})
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		jsonOut(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) mihomoBaseURL() string {
	if s.mihomoController != "" {
		return s.mihomoController
	}
	return mihomoController
}

func (s *Server) mihomoRequest(method, path string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(encoded))
	}
	req, err := http.NewRequest(method, s.mihomoBaseURL()+"/"+strings.TrimPrefix(path, "/"), body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mihomo returned HTTP %d", resp.StatusCode)
	}
	if target != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(target)
	}
	return nil
}

func reverseProxyRequest(w http.ResponseWriter, r *http.Request, routePrefix, upstreamPrefix string) {
	target, _ := url.Parse(mihomoController)
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		remainder := strings.TrimPrefix(request.URL.Path, routePrefix)
		request.URL.Path = strings.TrimRight(upstreamPrefix, "/") + "/" + remainder
		request.URL.RawPath = ""
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("mihomo reverse proxy failed: %v", err)
		jsonOut(writer, http.StatusBadGateway, map[string]string{"error": "Mihomo 控制器不可用"})
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) mihomoDashboard(w http.ResponseWriter, r *http.Request) {
	reverseProxyRequest(w, r, mihomoDashboardPrefix, "/ui")
}

func (s *Server) mihomoDashboardAssets(w http.ResponseWriter, r *http.Request) {
	reverseProxyRequest(w, r, "/_nuxt/", "/ui/_nuxt")
}

func (s *Server) mihomoDashboardFonts(w http.ResponseWriter, r *http.Request) {
	reverseProxyRequest(w, r, "/_fonts/", "/ui/_fonts")
}

func (s *Server) mihomoAPIProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch && strings.Trim(strings.TrimPrefix(r.URL.Path, mihomoAPIPrefix), "/") == "configs" {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		if err != nil || len(body) > 1<<20 {
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "invalid or oversized Mihomo config request"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil {
			if rawMode, exists := payload["mode"]; exists {
				mode, ok := rawMode.(string)
				mode = strings.ToLower(strings.TrimSpace(mode))
				if !ok || !validMihomoMode(mode) {
					jsonOut(w, http.StatusBadRequest, map[string]string{"error": "Mihomo mode must be global, rule, or direct"})
					return
				}
				s.subscriptionMu.Lock()
				defer s.subscriptionMu.Unlock()
				if err := s.setMihomoMode(mode); err != nil {
					jsonOut(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
					return
				}
				delete(payload, "mode")
				if len(payload) > 0 {
					if err := s.mihomoRequest(http.MethodPatch, "/configs", payload, nil); err != nil {
						jsonOut(w, http.StatusServiceUnavailable, map[string]string{"error": "remaining Mihomo config update failed"})
						return
					}
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
	}
	reverseProxyRequest(w, r, mihomoAPIPrefix, "/")
}

func (s *Server) mihomoStatus(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	var config struct {
		Mode        string `json:"mode"`
		MixedPort   int    `json:"mixed-port"`
		HTTPPort    int    `json:"port"`
		SocksPort   int    `json:"socks-port"`
		RedirPort   int    `json:"redir-port"`
		TProxyPort  int    `json:"tproxy-port"`
		AllowLAN    bool   `json:"allow-lan"`
		BindAddress string `json:"bind-address"`
		Tun         struct {
			Enable bool   `json:"enable"`
			Stack  string `json:"stack"`
		} `json:"tun"`
	}
	if err := s.mihomoRequest("GET", "/configs", nil, &config); err != nil {
		mode := strings.TrimSpace(readText(filepath.Join(s.data, "mihomo", "runtime-mode")))
		jsonOut(w, 200, map[string]any{
			"online": false, "mode": mode, "version": "", "proxyCount": 0,
			"connections": 0, "downloadTotal": 0, "uploadTotal": 0, "memory": 0,
			"mixedPort": mihomoDefaultMixedPort, "httpPort": 0, "socksPort": 0,
			"redirPort": 0, "tproxyPort": 0, "allowLan": false,
			"bindAddress": "127.0.0.1", "tunEnabled": false, "tunStack": "",
		})
		return
	}
	var version struct {
		Version string `json:"version"`
	}
	_ = s.mihomoRequest("GET", "/version", nil, &version)
	var proxyData struct {
		Proxies map[string]json.RawMessage `json:"proxies"`
	}
	_ = s.mihomoRequest("GET", "/proxies", nil, &proxyData)
	var connections struct {
		DownloadTotal uint64            `json:"downloadTotal"`
		UploadTotal   uint64            `json:"uploadTotal"`
		Connections   []json.RawMessage `json:"connections"`
		Memory        uint64            `json:"memory"`
	}
	_ = s.mihomoRequest("GET", "/connections", nil, &connections)
	if connections.Memory == 0 {
		connections.Memory = processRSSBytes("mihomo")
	}
	jsonOut(w, 200, map[string]any{
		"online": true, "mode": strings.ToLower(config.Mode), "version": version.Version,
		"proxyCount": len(proxyData.Proxies), "connections": len(connections.Connections),
		"downloadTotal": connections.DownloadTotal, "uploadTotal": connections.UploadTotal, "memory": connections.Memory,
		"mixedPort": config.MixedPort, "httpPort": config.HTTPPort, "socksPort": config.SocksPort,
		"redirPort": config.RedirPort, "tproxyPort": config.TProxyPort, "allowLan": config.AllowLAN,
		"bindAddress": config.BindAddress, "tunEnabled": config.Tun.Enable, "tunStack": config.Tun.Stack,
	})
}

func validMihomoMode(mode string) bool {
	return mode == "global" || mode == "rule" || mode == "direct"
}

func (s *Server) setMihomoMode(mode string) error {
	if !validMihomoMode(mode) {
		return errors.New("Mihomo mode must be global, rule, or direct")
	}
	out, err := s.execSu(routerNetworkManager + " proxy-mode " + mode)
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = "Mihomo mode change or persistence failed"
		}
		return errors.New(message)
	}
	return nil
}

func (s *Server) mihomoMode(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "PUT") {
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if decodeJSON(r, &body) != nil {
		jsonOut(w, 400, map[string]string{"error": "请求格式错误"})
		return
	}
	body.Mode = strings.ToLower(strings.TrimSpace(body.Mode))
	if !validMihomoMode(body.Mode) {
		jsonOut(w, 400, map[string]string{"error": "模式必须是 global、rule 或 direct"})
		return
	}
	s.subscriptionMu.Lock()
	defer s.subscriptionMu.Unlock()
	if err := s.setMihomoMode(body.Mode); err != nil {
		jsonOut(w, 503, map[string]string{"error": "Mihomo 未运行或模式切换失败"})
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "mode": body.Mode})
}

func mihomoSettingsFromConfig(config struct {
	MixedPort   int    `json:"mixed-port"`
	HTTPPort    int    `json:"port"`
	SocksPort   int    `json:"socks-port"`
	RedirPort   int    `json:"redir-port"`
	TProxyPort  int    `json:"tproxy-port"`
	AllowLAN    bool   `json:"allow-lan"`
	BindAddress string `json:"bind-address"`
}) MihomoSettings {
	return MihomoSettings{MixedPort: config.MixedPort, HTTPPort: config.HTTPPort, SocksPort: config.SocksPort, RedirPort: config.RedirPort, TProxyPort: config.TProxyPort, AllowLAN: config.AllowLAN, BindAddress: config.BindAddress}
}

func (s *Server) currentMihomoSettings() (MihomoSettings, error) {
	var config struct {
		MixedPort   int    `json:"mixed-port"`
		HTTPPort    int    `json:"port"`
		SocksPort   int    `json:"socks-port"`
		RedirPort   int    `json:"redir-port"`
		TProxyPort  int    `json:"tproxy-port"`
		AllowLAN    bool   `json:"allow-lan"`
		BindAddress string `json:"bind-address"`
	}
	if err := s.mihomoRequest("GET", "/configs", nil, &config); err != nil {
		return MihomoSettings{}, err
	}
	return mihomoSettingsFromConfig(config), nil
}

func persistedMihomoSettings(path string) MihomoSettings {
	settings := MihomoSettings{MixedPort: mihomoDefaultMixedPort, BindAddress: "127.0.0.1"}
	for _, line := range strings.Split(readText(path), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		port, _ := strconv.Atoi(value)
		switch key {
		case "mixed-port":
			settings.MixedPort = port
		case "port":
			settings.HTTPPort = port
		case "socks-port":
			settings.SocksPort = port
		case "redir-port":
			settings.RedirPort = port
		case "tproxy-port":
			settings.TProxyPort = port
		case "allow-lan":
			settings.AllowLAN = value == "true"
		case "bind-address":
			settings.BindAddress = value
		}
	}
	return settings
}

func validateMihomoSettings(settings *MihomoSettings) error {
	ports := []int{settings.MixedPort, settings.HTTPPort, settings.SocksPort, settings.RedirPort, settings.TProxyPort}
	for _, port := range ports {
		if port < 0 || port > 65535 {
			return errors.New("端口必须在 0 到 65535 之间，0 表示关闭")
		}
	}
	if settings.MixedPort == 0 && settings.HTTPPort == 0 && settings.SocksPort == 0 {
		return errors.New("Mixed、HTTP 和 SOCKS 端口不能全部关闭")
	}
	// LAN clients are captured by the root transparent-forwarding switch.
	// Never expose this unauthenticated mixed listener on a physical interface.
	settings.AllowLAN = false
	settings.BindAddress = "127.0.0.1"
	if settings.BindAddress != "*" && net.ParseIP(settings.BindAddress) == nil {
		return errors.New("监听地址无效")
	}
	return nil
}

func mihomoSettingLines(settings MihomoSettings) map[string]string {
	return map[string]string{
		"mixed-port":   fmt.Sprintf("mixed-port: %d", settings.MixedPort),
		"port":         fmt.Sprintf("port: %d", settings.HTTPPort),
		"socks-port":   fmt.Sprintf("socks-port: %d", settings.SocksPort),
		"redir-port":   fmt.Sprintf("redir-port: %d", settings.RedirPort),
		"tproxy-port":  fmt.Sprintf("tproxy-port: %d", settings.TProxyPort),
		"allow-lan":    fmt.Sprintf("allow-lan: %t", settings.AllowLAN),
		"bind-address": fmt.Sprintf("bind-address: %s", settings.BindAddress),
	}
}

func rewriteMihomoTopLevelConfig(source string, settings MihomoSettings) string {
	overrides := mihomoSettingLines(settings)
	order := []string{"mixed-port", "port", "socks-port", "redir-port", "tproxy-port", "allow-lan", "bind-address"}
	seen := make(map[string]bool)
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines)+len(order))
	for _, line := range lines {
		replaced := false
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			if colon := strings.IndexByte(line, ':'); colon > 0 {
				key := strings.TrimSpace(line[:colon])
				if replacement, ok := overrides[key]; ok {
					if !seen[key] {
						result = append(result, replacement)
						seen[key] = true
					}
					replaced = true
				}
			}
		}
		if !replaced {
			result = append(result, line)
		}
	}
	missing := make([]string, 0, len(order))
	for _, key := range order {
		if !seen[key] {
			missing = append(missing, overrides[key])
		}
	}
	if len(missing) > 0 {
		result = append(missing, result...)
	}
	return strings.TrimRight(strings.Join(result, "\n"), "\n") + "\n"
}

func (s *Server) saveMihomoSettings(settings MihomoSettings) error {
	dataDir := filepath.Join(s.data, "mihomo")
	target := filepath.Join(dataDir, "config.yaml")
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	updated := rewriteMihomoTopLevelConfig(string(current), settings)
	temporary := target + ".settings-new"
	backup := target + ".before-settings"
	settingsPath := filepath.Join(dataDir, "runtime-settings.yaml")
	if err := os.WriteFile(temporary, []byte(updated), 0600); err != nil {
		return err
	}
	defer os.Remove(temporary)
	mihomo := filepath.Join(s.prefix, "bin", "mihomo")
	if out, err := exec.Command(mihomo, "-t", "-d", dataDir, "-f", temporary).CombinedOutput(); err != nil {
		log.Printf("mihomo settings validation failed: %s", tailLines(string(out), 30))
		return errors.New("配置校验失败，原配置未改变")
	}
	if err := os.WriteFile(settingsPath+".tmp", []byte(rewriteMihomoTopLevelConfig("", settings)), 0600); err != nil {
		return err
	}
	if err := os.Rename(settingsPath+".tmp", settingsPath); err != nil {
		return err
	}
	if err := os.WriteFile(backup, current, 0600); err != nil {
		return err
	}
	service := filepath.Join(s.stack, "services", "mihomo")
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "down", service).Run()
	if err := os.Rename(temporary, target); err != nil {
		_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "up", service).Run()
		return err
	}
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "up", service).Run()
	// The runit finish hook intentionally delays rapid restarts to avoid a
	// crash loop. Allow enough time for that guard plus Mihomo initialization.
	for attempt := 0; attempt < 80; attempt++ {
		time.Sleep(250 * time.Millisecond)
		if _, err := s.currentMihomoSettings(); err == nil {
			return nil
		}
	}
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "down", service).Run()
	previous, _ := os.ReadFile(backup)
	_ = os.WriteFile(target, previous, 0600)
	_ = exec.Command(filepath.Join(s.prefix, "bin", "sv"), "up", service).Run()
	return errors.New("Mihomo 使用新端口启动失败，已恢复原配置")
}

func (s *Server) mihomoSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		settings, err := s.currentMihomoSettings()
		if err != nil {
			jsonOut(w, 200, persistedMihomoSettings(filepath.Join(s.data, "mihomo", "runtime-settings.yaml")))
			return
		}
		jsonOut(w, 200, settings)
		return
	}
	if !method(w, r, "PUT") {
		return
	}
	var settings MihomoSettings
	if decodeJSON(r, &settings) != nil {
		jsonOut(w, 400, map[string]string{"error": "请求格式错误"})
		return
	}
	if err := validateMihomoSettings(&settings); err != nil {
		jsonOut(w, 400, map[string]string{"error": err.Error()})
		return
	}
	s.subscriptionMu.Lock()
	err := s.saveMihomoSettings(settings)
	s.subscriptionMu.Unlock()
	if err != nil {
		jsonOut(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonOut(w, 200, settings)
}

func (s *Server) readWholeHomeProxy() (WholeHomeProxyState, error) {
	state := WholeHomeProxyState{Listener: fmt.Sprintf("127.0.0.1:%d", mihomoDefaultMixedPort)}
	su := filepath.Join(s.prefix, "bin", "su")
	out, err := exec.Command(su, "-c", routerNetworkManager+" status").CombinedOutput()
	if err != nil {
		return state, fmt.Errorf("router network status: %s", strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "whole_home_proxy":
			state.Enabled = parts[1] == "enabled"
		case "proxy_tun":
			state.Active = parts[1] == "up"
		case "mihomo_router":
			state.Ready = parts[1] == "ready" || parts[1] == "available"
		}
	}
	return state, nil
}

func (s *Server) setWholeHomeProxy(enabled bool) (WholeHomeProxyState, error) {
	if enabled {
		var version map[string]any
		if err := s.mihomoRequest("GET", "/version", nil, &version); err != nil {
			return WholeHomeProxyState{}, errors.New("Mihomo 未运行，无法开启全屋代理")
		}
	}
	action := "proxy-off"
	if enabled {
		action = "proxy-on"
	}
	su := filepath.Join(s.prefix, "bin", "su")
	out, err := exec.Command(su, "-c", routerNetworkManager+" "+action).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = "透明转发切换失败"
		}
		return WholeHomeProxyState{}, errors.New(message)
	}
	return s.readWholeHomeProxy()
}

func (s *Server) mihomoWholeHomeProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		state, err := s.readWholeHomeProxy()
		if err != nil {
			jsonOut(w, 500, map[string]string{"error": "无法读取全屋代理状态"})
			return
		}
		jsonOut(w, 200, state)
		return
	}
	if !method(w, r, http.MethodPut) {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if decodeJSON(r, &body) != nil {
		jsonOut(w, 400, map[string]string{"error": "请求格式错误"})
		return
	}
	state, err := s.setWholeHomeProxy(body.Enabled)
	if err != nil {
		jsonOut(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonOut(w, 200, state)
}

func (s *Server) mihomoProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		var data struct {
			Proxies map[string]struct {
				Type string   `json:"type"`
				Now  string   `json:"now"`
				All  []string `json:"all"`
			} `json:"proxies"`
		}
		if err := s.mihomoRequest("GET", "/proxies", nil, &data); err != nil {
			jsonOut(w, 200, map[string]any{"online": false, "groups": []MihomoProxyGroup{}})
			return
		}
		groups := make([]MihomoProxyGroup, 0)
		for name, proxy := range data.Proxies {
			if name == speedTestBenchmarkGroup {
				continue
			}
			if len(proxy.All) > 0 {
				groups = append(groups, MihomoProxyGroup{Name: name, Type: proxy.Type, Now: proxy.Now, All: proxy.All})
			}
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
		jsonOut(w, 200, map[string]any{"groups": groups})
		return
	}
	if !method(w, r, "PUT") {
		return
	}
	var body struct {
		Group string `json:"group"`
		Proxy string `json:"proxy"`
	}
	if decodeJSON(r, &body) != nil || strings.TrimSpace(body.Group) == "" || strings.TrimSpace(body.Proxy) == "" || body.Group == speedTestBenchmarkGroup {
		jsonOut(w, 400, map[string]string{"error": "代理组和节点不能为空"})
		return
	}
	if err := s.mihomoRequest("PUT", "/proxies/"+url.PathEscape(body.Group), map[string]string{"name": body.Proxy}, nil); err != nil {
		jsonOut(w, 400, map[string]string{"error": "节点切换失败，请确认该节点属于代理组"})
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "group": body.Group, "proxy": body.Proxy})
}

func (s *Server) mihomoDelay(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	group := strings.TrimSpace(r.URL.Query().Get("group"))
	if group == "" || len(group) > 256 {
		jsonOut(w, 400, map[string]string{"error": "代理组名称无效"})
		return
	}
	path := "/group/" + url.PathEscape(group) + "/delay?url=" + url.QueryEscape("https://www.gstatic.com/generate_204") + "&timeout=8000"
	delays := map[string]int{}
	if err := s.mihomoRequest("GET", path, nil, &delays); err != nil {
		jsonOut(w, 502, map[string]string{"error": "节点测速失败"})
		return
	}
	jsonOut(w, 200, map[string]any{"group": group, "delays": delays})
}

// getMihomoConnections coalesces the dashboard summary and client list onto
// one Mihomo /connections decode. Returned snapshots are
// immutable after publication and can therefore be shared safely.
func (s *Server) getMihomoConnections(ctx context.Context, maxAge time.Duration) (*mihomoRawConnectionsResponse, error) {
	for {
		s.mihomoConnectionsMu.Lock()
		if s.mihomoConnectionsCache != nil && time.Since(s.mihomoConnectionsAt) < maxAge {
			cached := s.mihomoConnectionsCache
			s.mihomoConnectionsMu.Unlock()
			return cached, nil
		}
		if s.mihomoConnectionsLoading {
			wait := s.mihomoConnectionsWait
			s.mihomoConnectionsMu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		s.mihomoConnectionsLoading = true
		s.mihomoConnectionsWait = make(chan struct{})
		wait := s.mihomoConnectionsWait
		s.mihomoConnectionsMu.Unlock()

		var response mihomoRawConnectionsResponse
		err := s.mihomoRequestWithTimeout(ctx, "GET", "/connections", nil, &response, mihomoRequestTimeout, mihomoMaxResponseBytes)

		s.mihomoConnectionsMu.Lock()
		if err == nil {
			s.mihomoConnectionsCache = &response
			s.mihomoConnectionsAt = time.Now()
		}
		s.mihomoConnectionsLoading = false
		close(wait)
		s.mihomoConnectionsMu.Unlock()
		return &response, err
	}
}

func (s *Server) invalidateMihomoConnections() {
	s.mihomoConnectionsMu.Lock()
	s.mihomoConnectionsAt = time.Time{}
	s.mihomoConnectionsMu.Unlock()
}

func (s *Server) mihomoConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			jsonOut(w, 400, map[string]string{"error": "连接 ID 不能为空"})
			return
		}
		if err := s.mihomoRequest("DELETE", "/connections/"+url.PathEscape(id), nil, nil); err != nil {
			jsonOut(w, 400, map[string]string{"error": "关闭连接失败"})
			return
		}
		s.invalidateMihomoConnections()
		jsonOut(w, 200, map[string]bool{"ok": true})
		return
	}
	if !method(w, r, "GET") {
		return
	}
	summaryOnly := r.URL.Query().Get("summary") == "1"
	data, err := s.getMihomoConnections(r.Context(), mihomoConnectionsCacheTTL)
	if err != nil {
		connections := any([]MihomoConnection{})
		if summaryOnly {
			connections = 0
		}
		jsonOut(w, 200, map[string]any{
			"online": false, "downloadTotal": 0, "uploadTotal": 0,
			"memory": 0, "connections": connections,
		})
		return
	}
	memory := data.Memory
	if memory == 0 {
		memory = processRSSBytes("mihomo")
	}
	if summaryOnly {
		jsonOut(w, 200, map[string]any{"downloadTotal": data.DownloadTotal, "uploadTotal": data.UploadTotal, "memory": memory, "connections": len(data.Connections)})
		return
	}
	connections := make([]MihomoConnection, 0, len(data.Connections))
	for _, connection := range data.Connections {
		connections = append(connections, MihomoConnection{ID: connection.ID, Host: connection.Metadata.Host, Source: net.JoinHostPort(connection.Metadata.SourceIP, connection.Metadata.SourcePort), Network: connection.Metadata.Network, Type: connection.Metadata.Type, Rule: connection.Rule, Chains: connection.Chains, Upload: connection.Upload, Download: connection.Download, Start: connection.Start})
	}
	jsonOut(w, 200, map[string]any{"downloadTotal": data.DownloadTotal, "uploadTotal": data.UploadTotal, "memory": memory, "connections": connections})
}

func processRSSBytes(name string) uint64 {
	fields := strings.Fields(commandText("pidof", name))
	if len(fields) == 0 {
		return 0
	}
	for _, line := range strings.Split(readText(filepath.Join("/proc", fields[0], "status")), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "VmRSS:" {
			value, _ := strconv.ParseUint(parts[1], 10, 64)
			return value * 1024
		}
	}
	return 0
}

func newSubscriptionID() (string, error) {
	raw := make([]byte, 9)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Server) subscriptionStorePath() string {
	return filepath.Join(s.data, "mihomo", "subscriptions.json")
}

func (s *Server) saveSubscriptionStore(store SubscriptionStore) error {
	store.Version = 1
	dir := filepath.Dir(s.subscriptionStorePath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.subscriptionStorePath() + ".tmp"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.subscriptionStorePath()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Server) loadSubscriptionStore() (SubscriptionStore, error) {
	store := SubscriptionStore{Version: 1, Items: []SubscriptionRecord{}}
	encoded, err := os.ReadFile(s.subscriptionStorePath())
	if err == nil {
		if err := json.Unmarshal(encoded, &store); err != nil {
			return store, err
		}
		if store.Items == nil {
			store.Items = []SubscriptionRecord{}
		}
		if s.migrateActiveSubscriptionCache(&store) {
			if err := s.saveSubscriptionStore(store); err != nil {
				return store, err
			}
		}
		return store, nil
	}
	if !os.IsNotExist(err) {
		return store, err
	}
	legacy := strings.TrimSpace(readText(filepath.Join(s.data, "mihomo", "subscription.url")))
	if legacy == "" {
		return store, nil
	}
	if _, err := validateSubscriptionURL(legacy); err != nil {
		return store, err
	}
	id, err := newSubscriptionID()
	if err != nil {
		return store, err
	}
	now := time.Now()
	store.ActiveID = id
	store.Items = append(store.Items, SubscriptionRecord{ID: id, Name: "R2S 原订阅", URL: legacy, CreatedAt: now, UpdatedAt: now})
	return store, s.saveSubscriptionStore(store)
}

func maskedSubscriptionSource(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "地址已隐藏"
	}
	return parsed.Scheme + "://" + parsed.Host + "/…"
}

var (
	sensitiveQueryParamRegex = regexp.MustCompile(`(?i)(token|password|secret|key|auth|signature)=([^\s&"'<>]+)`)
	secretURLRegex           = regexp.MustCompile(`https?://[^\s"'<>]+`)
)

func sanitizeLogSecrets(s string) string {
	redacted := sensitiveQueryParamRegex.ReplaceAllString(s, "$1=[REDACTED]")
	return secretURLRegex.ReplaceAllStringFunc(redacted, func(raw string) string {
		return maskedSubscriptionSource(raw)
	})
}

func (s *Server) publicSubscriptions(store SubscriptionStore) map[string]any {
	items := make([]PublicSubscription, 0, len(store.Items))
	activeUpdated := ""
	if stat, err := os.Stat(filepath.Join(s.data, "mihomo", "config.yaml")); err == nil {
		activeUpdated = stat.ModTime().Format(time.RFC3339)
	}
	for _, item := range store.Items {
		updated := item.UpdatedAt.Format(time.RFC3339)
		if item.ID == store.ActiveID && activeUpdated != "" {
			updated = activeUpdated
		}
		cached := item.CachedURL == item.URL && item.URL != "" && fileExistsNonEmpty(s.subscriptionCachePath(item.ID))
		publicItem := PublicSubscription{ID: item.ID, Name: item.Name, Source: maskedSubscriptionSource(item.URL), Active: item.ID == store.ActiveID, Cached: cached, NodeCount: item.NodeCount, NeedsUpdate: !cached, CreatedAt: item.CreatedAt.Format(time.RFC3339), UpdatedAt: updated}
		if !cached {
			publicItem.NodeCount = 0
		}
		items = append(items, publicItem)
	}
	return map[string]any{"activeId": store.ActiveID, "items": items, "autoUpdateHours": 6}
}

func (s *Server) writeActiveSubscriptionURL(raw string) error {
	target := filepath.Join(s.data, "mihomo", "subscription.url")
	if raw == "" {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(raw+"\n"), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Server) updateAndActivateSubscription(raw string) error {
	target := filepath.Join(s.data, "mihomo", "subscription.url")
	previous, readErr := os.ReadFile(target)
	hadPrevious := readErr == nil
	if err := s.writeActiveSubscriptionURL(raw); err != nil {
		return err
	}
	updater := filepath.Join(s.stack, "bin", "mihomo-update-subscription")
	out, err := exec.Command(updater).CombinedOutput()
	if err == nil {
		return nil
	}
	if hadPrevious {
		_ = s.writeActiveSubscriptionURL(strings.TrimSpace(string(previous)))
	} else {
		_ = s.writeActiveSubscriptionURL("")
	}
	log.Printf("mihomo subscription activation failed: %s", sanitizeLogSecrets(tailLines(string(out), 30)))
	return errors.New("订阅下载或校验失败，当前配置未改变")
}

func (s *Server) mihomoSubscriptions(w http.ResponseWriter, r *http.Request) {
	s.subscriptionMu.Lock()
	defer s.subscriptionMu.Unlock()
	store, err := s.loadSubscriptionStore()
	if err != nil {
		jsonOut(w, 500, map[string]string{"error": "读取订阅列表失败"})
		return
	}

	switch r.Method {
	case "GET":
		jsonOut(w, 200, s.publicSubscriptions(store))
	case "POST":
		var body struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if decodeJSON(r, &body) != nil {
			jsonOut(w, 400, map[string]string{"error": "请求格式错误"})
			return
		}
		raw, err := validateSubscriptionURL(body.URL)
		if err != nil {
			jsonOut(w, 400, map[string]string{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = maskedSubscriptionSource(raw)
		}
		if len(name) > 128 {
			jsonOut(w, 400, map[string]string{"error": "订阅名称过长"})
			return
		}
		id, err := newSubscriptionID()
		if err != nil {
			jsonOut(w, 500, map[string]string{"error": "无法生成订阅 ID"})
			return
		}
		if err := s.updateAndActivateSubscription(raw); err != nil {
			jsonOut(w, 502, map[string]string{"error": err.Error()})
			return
		}
		now := time.Now()
		store.Items = append(store.Items, SubscriptionRecord{ID: id, Name: name, URL: raw, CreatedAt: now, UpdatedAt: now})
		store.ActiveID = id
		if err := s.saveSubscriptionStore(store); err != nil {
			jsonOut(w, 500, map[string]string{"error": "订阅已生效，但列表保存失败"})
			return
		}
		jsonOut(w, 200, s.publicSubscriptions(store))
	case "PATCH":
		var body struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if decodeJSON(r, &body) != nil || (body.Action != "activate" && body.Action != "update") {
			jsonOut(w, 400, map[string]string{"error": "无效订阅操作"})
			return
		}
		index := -1
		for i := range store.Items {
			if store.Items[i].ID == body.ID {
				index = i
				break
			}
		}
		if index < 0 {
			jsonOut(w, 404, map[string]string{"error": "订阅不存在"})
			return
		}
		if err := s.updateAndActivateSubscription(store.Items[index].URL); err != nil {
			jsonOut(w, 502, map[string]string{"error": err.Error()})
			return
		}
		store.ActiveID = store.Items[index].ID
		store.Items[index].UpdatedAt = time.Now()
		if err := s.saveSubscriptionStore(store); err != nil {
			jsonOut(w, 500, map[string]string{"error": "保存订阅状态失败"})
			return
		}
		jsonOut(w, 200, s.publicSubscriptions(store))
	case "DELETE":
		id := r.URL.Query().Get("id")
		index := -1
		for i := range store.Items {
			if store.Items[i].ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			jsonOut(w, 404, map[string]string{"error": "订阅不存在"})
			return
		}
		if store.ActiveID == id {
			next := -1
			for i := range store.Items {
				if i != index {
					next = i
					break
				}
			}
			if next >= 0 {
				if err := s.updateAndActivateSubscription(store.Items[next].URL); err != nil {
					jsonOut(w, 502, map[string]string{"error": "无法切换到剩余订阅，未执行删除"})
					return
				}
				store.ActiveID = store.Items[next].ID
				store.Items[next].UpdatedAt = time.Now()
			} else {
				store.ActiveID = ""
				_ = s.writeActiveSubscriptionURL("")
			}
		}
		store.Items = append(store.Items[:index], store.Items[index+1:]...)
		if err := s.saveSubscriptionStore(store); err != nil {
			jsonOut(w, 500, map[string]string{"error": "删除订阅失败"})
			return
		}
		jsonOut(w, 200, s.publicSubscriptions(store))
	default:
		w.Header().Set("Allow", "GET, POST, PATCH, DELETE")
		jsonOut(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func defaultNetworkDraft() NetworkDraft {
	return NetworkDraft{
		WANInterface: "eth0",
		LANInterface: "eth1",
		WANMode:      "pppoe",
		LANAddress:   "192.168.2.1/24",
		DHCPStart:    "192.168.2.100",
		DHCPEnd:      "192.168.2.249",
		DNS:          "192.168.2.1",
		MTU:          1492,
		PPPoEUser:    "",
		PPPoEPass:    "",
		PPPoEBackend: "auto",
		WANMAC:       "",
	}
}

func (s *Server) readRouterNetworkConfig() (map[string]string, error) {
	out, err := s.execSu("cat /data/adb/router-network/config")
	if err != nil {
		return nil, err
	}
	res := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		v = strings.Trim(v, `"'`)
		res[k] = v
	}
	return res, nil
}

func (s *Server) updateRouterNetworkConfig(d NetworkDraft) error {
	raw, err := s.execSu("cat /data/adb/router-network/config")
	if err != nil {
		if s.runSu == nil && runtime.GOOS != "linux" {
			return nil
		}
		return fmt.Errorf("读取当前配置失败: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	keysToUpdate := map[string]string{
		"PPPOE_USER":    d.PPPoEUser,
		"PPPOE_PASS":    d.PPPoEPass,
		"PPPOE_BACKEND": d.PPPoEBackend,
	}
	if d.WANMAC != "" {
		keysToUpdate["WAN_MAC"] = strings.ToLower(d.WANMAC)
	}
	updatedKeys := make(map[string]bool)
	var newLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			newLines = append(newLines, line)
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		k := strings.TrimSpace(parts[0])
		if v, ok := keysToUpdate[k]; ok {
			newLines = append(newLines, fmt.Sprintf("%s=%s", k, shellQuote(v)))
			updatedKeys[k] = true
		} else {
			newLines = append(newLines, line)
		}
	}
	for k, v := range keysToUpdate {
		if !updatedKeys[k] && v != "" {
			newLines = append(newLines, fmt.Sprintf("%s=%s", k, shellQuote(v)))
		}
	}
	newContent := strings.Join(newLines, "\n")
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	targetPath := "/data/adb/router-network/config"
	bakPath := "/data/adb/router-network/config.bak"
	tmpPath := fmt.Sprintf("/data/adb/router-network/config.tmp.%d", time.Now().UnixNano())
	cmd := fmt.Sprintf(
		"tmp=%s; trap 'rm -f \"$tmp\"' EXIT; printf '%%s' %s > \"$tmp\" && chmod 600 \"$tmp\" && cp -p %s %s && mv -f \"$tmp\" %s",
		shellQuote(tmpPath),
		shellQuote(newContent),
		shellQuote(targetPath),
		shellQuote(bakPath),
		shellQuote(targetPath),
	)
	if out, err := s.execSu(cmd); err != nil {
		return fmt.Errorf("写入网络配置失败: %s (%w)", tailLines(string(out), 5), err)
	}
	return nil
}

func (s *Server) loadNetworkDraft() NetworkDraft {
	d := defaultNetworkDraft()
	draftFile := filepath.Join(s.data, "admin", "network-draft.json")
	b, e := os.ReadFile(draftFile)
	if e == nil {
		_ = json.Unmarshal(b, &d)
	}
	if (d.PPPoEUser == "" || d.WANMAC == "") && time.Since(s.lastConfigCheck) > 10*time.Second {
		s.lastConfigCheck = time.Now()
		if cfg, err := s.readRouterNetworkConfig(); err == nil && len(cfg) > 0 {
			changed := false
			if d.PPPoEUser == "" && cfg["PPPOE_USER"] != "" {
				d.PPPoEUser = cfg["PPPOE_USER"]
				changed = true
			}
			if d.PPPoEPass == "" && cfg["PPPOE_PASS"] != "" {
				d.PPPoEPass = cfg["PPPOE_PASS"]
				changed = true
			}
			if (d.PPPoEBackend == "" || d.PPPoEBackend == "auto") && cfg["PPPOE_BACKEND"] != "" {
				d.PPPoEBackend = cfg["PPPOE_BACKEND"]
				changed = true
			}
			if d.WANMAC == "" && cfg["WAN_MAC"] != "" {
				d.WANMAC = cfg["WAN_MAC"]
				changed = true
			}
			if changed {
				if bOut, err := json.MarshalIndent(d, "", "  "); err == nil {
					_ = os.WriteFile(draftFile, bOut, 0600)
				}
			}
		}
	}
	return d
}

func (s *Server) networkDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		jsonOut(w, 200, s.loadNetworkDraft())
		return
	}
	if !method(w, r, "PUT") {
		return
	}
	var d NetworkDraft
	if decodeJSON(r, &d) != nil {
		jsonOut(w, 400, map[string]string{"error": "请求错误"})
		return
	}
	if d.PPPoEBackend == "" {
		d.PPPoEBackend = "auto"
	}
	if err := validateDraft(d); err != nil {
		jsonOut(w, 400, map[string]string{"error": err.Error()})
		return
	}
	dir := filepath.Join(s.data, "admin")
	_ = os.MkdirAll(dir, 0700)
	b, _ := json.MarshalIndent(d, "", "  ")
	tmp := filepath.Join(dir, "network-draft.json.tmp")
	if os.WriteFile(tmp, b, 0600) != nil || os.Rename(tmp, filepath.Join(dir, "network-draft.json")) != nil {
		jsonOut(w, 500, map[string]string{"error": "保存失败"})
		return
	}
	if d.WANMode == "pppoe" && d.PPPoEUser != "" {
		if err := s.updateRouterNetworkConfig(d); err != nil {
			log.Printf("更新底层网络配置失败: %v", err)
			jsonOut(w, 500, map[string]string{"error": fmt.Sprintf("草稿已保存，但更新系统配置失败: %v", err)})
			return
		}
	}
	jsonOut(w, 200, map[string]any{"ok": true, "draft": d})
}

func (s *Server) networkReconnect(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Backend string `json:"backend"`
	}
	_ = decodeJSON(r, &req)

	draft := s.loadNetworkDraft()
	backend := draft.PPPoEBackend
	if req.Backend != "" {
		backend = req.Backend
	}
	if backend == "" {
		backend = "auto"
	}

	subcmd := "pppoe-retry-kernel"
	if backend == "userspace" {
		subcmd = "pppoe-retry-userspace"
	}
	cmd := fmt.Sprintf("/data/adb/router-network/router-network %s", subcmd)
	out, err := s.execSu(cmd)
	if err != nil {
		log.Printf("network reconnect (%s) failed: %s (%v)", subcmd, tailLines(string(out), 10), err)
		jsonOut(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("重新拨号执行失败: %s", strings.TrimSpace(string(out))),
		})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{
		"ok":      true,
		"backend": backend,
		"message": "宽带重拨请求已成功下发，ISP 会话已释放，守护进程正在重新协商连接",
	})
}

func validateDraft(d NetworkDraft) error {
	iface := regexp.MustCompile(`^[a-zA-Z0-9_.-]{0,15}$`)
	if !iface.MatchString(d.WANInterface) || !iface.MatchString(d.LANInterface) {
		return errors.New("网口名称无效")
	}
	if d.WANMode != "pppoe" && d.WANMode != "dhcp" && d.WANMode != "static" {
		return errors.New("WAN 模式无效")
	}
	if _, _, e := net.ParseCIDR(d.LANAddress); e != nil {
		return errors.New("LAN 地址必须包含掩码")
	}
	if net.ParseIP(d.DHCPStart) == nil || net.ParseIP(d.DHCPEnd) == nil || net.ParseIP(d.DNS) == nil {
		return errors.New("DHCP 或 DNS 地址无效")
	}
	if d.MTU < 576 || d.MTU > 9000 {
		return errors.New("MTU 超出范围")
	}
	if d.WANMode == "pppoe" {
		if strings.TrimSpace(d.PPPoEUser) == "" {
			return errors.New("PPPoE 宽带账号不能为空")
		}
		if strings.TrimSpace(d.PPPoEPass) == "" {
			return errors.New("PPPoE 宽带密码不能为空")
		}
		if d.PPPoEBackend != "" && d.PPPoEBackend != "auto" && d.PPPoEBackend != "kernel" && d.PPPoEBackend != "userspace" {
			return errors.New("PPPoE 后端模式无效 (应为 auto, kernel 或 userspace)")
		}
	}
	if d.WANMAC != "" {
		macRe := regexp.MustCompile(`^([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}$`)
		if !macRe.MatchString(d.WANMAC) {
			return errors.New("WAN MAC 地址格式无效 (例如 12:34:56:78:9a:bc)")
		}
	}
	return nil
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method not allowed", 405)
		return
	}
	path := filepath.Clean("/" + r.URL.Path)
	if path == "/" {
		path = "/index.html"
	}
	full := filepath.Join(s.webRoot, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	root, _ := filepath.Abs(s.webRoot)
	abs, _ := filepath.Abs(full)
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		http.NotFound(w, r)
		return
	}
	if st, e := os.Stat(abs); e != nil || st.IsDir() {
		abs = filepath.Join(s.webRoot, "index.html")
	}
	if ext := filepath.Ext(abs); ext != "" {
		if typ := mime.TypeByExtension(ext); typ != "" {
			w.Header().Set("Content-Type", typ)
		}
	}
	if strings.Contains(abs, string(os.PathSeparator)+"_next"+string(os.PathSeparator)) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFile(w, r, abs)
}

func readText(path string) string {
	b, e := os.ReadFile(path)
	if e != nil {
		return ""
	}
	return string(b)
}
func readInt(path string) int { v, _ := strconv.Atoi(strings.TrimSpace(readText(path))); return v }

func readInt64(path string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(readText(path)), 10, 64)
	return v
}
func readFirstFloat(path string) (float64, error) {
	f := strings.Fields(readText(path))
	if len(f) == 0 {
		return 0, errors.New("empty")
	}
	return strconv.ParseFloat(f[0], 64)
}
func (s *Server) runCmd(name string, args ...string) string {
	if s != nil && s.commandRunner != nil {
		return s.commandRunner(name, args...)
	}
	return commandText(name, args...)
}

func commandText(name string, args ...string) string {
	b, e := exec.Command(name, args...).CombinedOutput()
	if e != nil {
		return strings.TrimSpace(string(b))
	}
	return strings.TrimSpace(string(b))
}
func round(v float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}
func tailLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\x00", ""), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

var _ = runtime.GOOS

func (s *Server) mihomoRequestWithTimeout(ctx context.Context, method, path string, payload any, target any, timeout time.Duration, maxBytes int64) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.mihomoBaseURL()+"/"+strings.TrimPrefix(path, "/"), body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mihomo returned HTTP %d", resp.StatusCode)
	}
	if target != nil {
		if maxBytes <= 0 {
			maxBytes = mihomoMaxResponseBytes
		}
		return json.NewDecoder(io.LimitReader(resp.Body, maxBytes)).Decode(target)
	}
	return nil
}

func (s *Server) readClientHostnames() map[string]string {
	mapping := make(map[string]string)
	mapping["127.0.0.1"] = "路由器本机 / CPA"
	mapping["198.18.0.1"] = "Mihomo TUN 本机"
	mapping[""] = "路由器本机 / 来源未知"

	now := time.Now().Unix()
	leasePaths := []string{
		filepath.Join(s.data, "dnsmasq", "dnsmasq.leases"),
		"/data/misc/dhcp/dnsmasq.leases",
	}
	for _, p := range leasePaths {
		text := readText(p)
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			f := strings.Fields(line)
			if len(f) >= 4 {
				exp, _ := strconv.ParseInt(f[0], 10, 64)
				if exp > 0 && exp < now {
					continue
				}
				ip := strings.TrimSpace(f[2])
				if ip == "" || net.ParseIP(ip) == nil {
					continue
				}
				hostname := strings.TrimSpace(f[3])
				if hostname == "OnePlus-7Pro" || hostname == "*" || hostname == "未知设备" {
					continue
				}
				if hostname != "" {
					mapping[ip] = hostname
				}
			}
		}
	}

	for _, lease := range s.readStaticLeases() {
		ip := strings.TrimSpace(lease.IP)
		hostname := strings.TrimSpace(lease.Hostname)
		if ip != "" && hostname != "" && hostname != "未知设备" {
			mapping[ip] = hostname
		}
	}

	return mapping
}

func (s *Server) resolveClientHostname(ip string) string {
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
	mapping := s.readClientHostnames()
	if name, ok := mapping[trimmed]; ok && name != "" && name != "未知设备" {
		return name
	}
	return trimmed
}
