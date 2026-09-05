"use client";

import {
  FormEvent,
  Fragment,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { BrowserScreenControl } from "./web-screen";

type Tab =
  | "overview"
  | "network"
  | "clients"
  | "services"
  | "mihomo"
  | "storage"
  | "system"
  | "remote";
type RemoteMode = "terminal" | "screen";
type AuthState = "checking" | "login" | "ready" | "demo";

type Snapshot = {
  time: string;
  trustedLocal: boolean;
  device: {
    Model: string;
    Android: string;
    Kernel: string;
    uptimeSec: number;
    adminUptimeSec: number;
  };
  cpu: {
    usage: number;
    Load1: number;
    Load5: number;
    Load15: number;
    Online: number;
    avgMHz: number;
    maxMHz: number;
    temperature: number;
    cores: {
      id: number;
      online: boolean;
      usage: number;
      currentMHz: number;
      maxMHz: number;
      governor: string;
    }[];
  };
  memory: {
    Total: number;
    Available: number;
    Used: number;
    Cached: number;
    Buffers: number;
    Reclaimable: number;
    SwapTotal: number;
    SwapUsed: number;
  };
  power: {
    batteryPercent: number;
    BatteryTemp: number;
    Voltage: number;
    Current: number;
    BatteryWatts: number;
    AverageCurrent: number;
    CoulombCurrent: number;
    ChargeCounterMAh: number;
    timeToEmptySec: number;
    Status: string;
    ChargingEnabled: boolean;
    InputSuspended: boolean;
    usbPresent: boolean;
    usbOnline: boolean;
    usbType: string;
    USBVoltage: number;
    USBInputCurrent: number;
    USBInputWatts: number;
    USBInputFiltered: boolean;
    PDVoltage: number;
    PDCurrentMax: number;
    PDWattsMax: number;
    TypeCMode: string;
    PowerRole: string;
    OTGActive: boolean;
    chargeGuard: boolean;
    chargePaused: boolean;
    guardLow?: number;
    guardHigh?: number;
  };
  thermals: { Name: string; Type: string; Temp: number }[];
  interfaces: {
    Name: string;
    MAC: string;
    State: string;
    Carrier: string;
    Role: string;
    MTU: number;
    SpeedMbps: number;
    Addresses: string[];
    RxBytes: number;
    TxBytes: number;
    RxBps: number;
    TxBps: number;
  }[];
  clients: {
    IP: string;
    MAC: string;
    Hostname: string;
    Interface: string;
    State: string;
    Source: string;
    Expires: number;
    StaticLease: boolean;
    StaticIP: string;
  }[];
  services: {
    Name: string;
    Label: string;
    State: string;
    PID: number;
    UptimeSec: number;
    Port: number;
    webUrl: string;
  }[];
  aria2: {
    online: boolean;
    downloadSpeed: number;
    uploadSpeed: number;
    numActive: number;
    numWaiting: number;
    numPaused: number;
    numStopped: number;
    tasks: {
      GID: string;
      Name: string;
      Status: string;
      TotalLength: number;
      CompletedLength: number;
      DownloadSpeed: number;
      UploadSpeed: number;
      Connections: number;
    }[];
  };
  storage: {
    Name: string;
    Mount: string;
    Filesystem: string;
    Model: string;
    Total: number;
    Used: number;
    Free: number;
    UsedPercent: number;
    External: boolean;
  }[];
  usb: {
    VendorID: string;
    ProductID: string;
    Manufacturer: string;
    Product: string;
    Speed: string;
  }[];
  networkDraft: NetworkDraft;
};

type NetworkDraft = {
  WANInterface: string;
  LANInterface: string;
  WANMode: string;
  LANAddress: string;
  DHCPStart: string;
  DHCPEnd: string;
  DNS: string;
  MTU: number;
};
type MihomoStatus = {
  online: boolean;
  mode: string;
  version: string;
  proxyCount: number;
  connections: number;
  downloadTotal: number;
  uploadTotal: number;
  memory: number;
  mixedPort: number;
  httpPort: number;
  socksPort: number;
  redirPort: number;
  tproxyPort: number;
  allowLan: boolean;
  bindAddress: string;
  tunEnabled: boolean;
  tunStack: string;
  tunDevice: string;
  cpaLinked: boolean;
  cpaUsageEnabled: boolean;
  cpaAccountCount: number;
  cpaProxyPort: number;
  cpaProxyGroup: string;
  cpaProxyNode: string;
  cpaAutoNode: string;
  cpaJapaneseNodeCount: number;
};
type MihomoSettings = {
  mixedPort: number;
  httpPort: number;
  socksPort: number;
  redirPort: number;
  tproxyPort: number;
  allowLan: boolean;
  bindAddress: string;
};
type MihomoProxyGroup = {
  name: string;
  type: string;
  now: string;
  all: string[];
};
type MihomoMetrics = {
  downloadTotal: number;
  uploadTotal: number;
  memory: number;
  connections: number;
};
type WholeHomeProxy = {
  enabled: boolean;
  active: boolean;
  ready: boolean;
  localApps: boolean;
  mode: string;
  engine: string;
  tunDevice: string;
  listener: string;
};
type TailscaleState = {
  installed: boolean;
  enabled: boolean;
  supervisor: boolean;
  daemon: boolean;
  online: boolean;
  loginRequired: boolean;
  loginPending: boolean;
  backend: string;
  hostname: string;
  ip: string;
  advertiseRoutes: string;
  primaryRoutes: string;
  routeStatus: "active" | "configured" | "off";
  authUrl?: string;
};
type SubscriptionItem = {
  id: string;
  name: string;
  source: string;
  active: boolean;
  createdAt: string;
  updatedAt: string;
};
type SubscriptionList = {
  activeId: string;
  items: SubscriptionItem[];
  autoUpdateHours: number;
};
type SubscriptionInfo = {
  configured: boolean;
  source: string;
  lastUpdated: string;
  autoUpdateHours: number;
};
type HistoryPoint = {
  at: number;
  cpu: number;
  temperature: number;
  memory: number;
  wanDown: number;
  wanUp: number;
  lanDown: number;
  lanUp: number;
  ariaDown: number;
  ariaUp: number;
  batteryVoltage: number;
  batteryCurrent: number;
  batteryAverageCurrent: number;
  batteryCoulombCurrent: number;
  batteryPower: number;
  batteryPercent: number;
  chargeCounter: number;
  usbVoltage: number;
  usbCurrent: number;
  usbPower: number;
};

const navItems: { id: Tab; label: string; icon: string }[] = [
  { id: "overview", label: "总览", icon: "OV" },
  { id: "network", label: "网络", icon: "NET" },
  { id: "clients", label: "客户端", icon: "LAN" },
  { id: "services", label: "服务", icon: "SVC" },
  { id: "mihomo", label: "代理", icon: "VPN" },
  { id: "storage", label: "存储", icon: "SSD" },
  { id: "system", label: "系统", icon: "SYS" },
  { id: "remote", label: "远程", icon: "WEB" },
];

const demo: Snapshot = {
  time: new Date().toISOString(),
  trustedLocal: true,
  device: {
    Model: "GM1910",
    Android: "11",
    Kernel: "4.14.190-perf+",
    uptimeSec: 25800,
    adminUptimeSec: 9600,
  },
  cpu: {
    usage: 12,
    Load1: 0.42,
    Load5: 0.36,
    Load15: 0.31,
    Online: 8,
    avgMHz: 1240,
    maxMHz: 2841,
    temperature: 64.7,
    cores: Array.from({ length: 8 }, (_, id) => ({
      id,
      online: true,
      usage: 8 + id * 2,
      currentMHz: id < 4 ? 1209 : id < 7 ? 710 : 825,
      maxMHz: id < 4 ? 1785 : id < 7 ? 2419 : 2841,
      governor: "schedutil",
    })),
  },
  memory: {
    Total: 8_000_000_000,
    Available: 5_100_000_000,
    Used: 2_900_000_000,
    Cached: 2_100_000_000,
    Buffers: 64_000_000,
    Reclaimable: 420_000_000,
    SwapTotal: 2_200_000_000,
    SwapUsed: 0,
  },
  power: {
    batteryPercent: 71,
    BatteryTemp: 42.9,
    Voltage: 3.92,
    Current: 0.89,
    BatteryWatts: 3.49,
    AverageCurrent: 0.84,
    CoulombCurrent: 0.86,
    ChargeCounterMAh: 2547.2,
    timeToEmptySec: 10800,
    Status: "Discharging",
    ChargingEnabled: true,
    InputSuspended: false,
    usbPresent: false,
    usbOnline: false,
    usbType: "USB_PD",
    USBVoltage: 5,
    USBInputCurrent: 0,
    USBInputWatts: 0,
    USBInputFiltered: false,
    PDVoltage: 5,
    PDCurrentMax: 2,
    PDWattsMax: 10,
    TypeCMode: "Nothing attached",
    PowerRole: "source",
    OTGActive: true,
    chargeGuard: true,
    chargePaused: false,
    guardLow: 40,
    guardHigh: 60,
  },
  thermals: [
    { Name: "thermal_zone14", Type: "cpu-1-7-usr", Temp: 64.7 },
    { Name: "thermal_zone80", Type: "battery", Temp: 42.9 },
  ],
  interfaces: [
    {
      Name: "wlan0",
      MAC: "46:1f:c3:0b:89:d1",
      State: "up",
      Carrier: "1",
      Role: "管理网络",
      MTU: 1500,
      SpeedMbps: 0,
      Addresses: ["192.168.2.107/24"],
      RxBytes: 5_420_000_000,
      TxBytes: 1_860_000_000,
      RxBps: 32_660_000,
      TxBps: 1_200_000,
    },
    {
      Name: "eth0",
      MAC: "2c:16:db:a8:e7:20",
      State: "down",
      Carrier: "0",
      Role: "未分配",
      MTU: 1500,
      SpeedMbps: -1,
      Addresses: [],
      RxBytes: 0,
      TxBytes: 0,
      RxBps: 0,
      TxBps: 0,
    },
  ],
  clients: [
    {
      IP: "192.168.2.151",
      MAC: "02:00:00:00:00:01",
      Hostname: "演示手机",
      Interface: "wlan0",
      State: "已发现",
      Source: "邻居缓存",
      Expires: 0,
      StaticLease: false,
      StaticIP: "",
    },
    {
      IP: "192.168.2.200",
      MAC: "02:00:00:00:00:02",
      Hostname: "演示摄像头",
      Interface: "wlan0",
      State: "已发现",
      Source: "邻居缓存",
      Expires: 0,
      StaticLease: true,
      StaticIP: "192.168.2.200",
    },
  ],
  services: [
    "aria2",
    "caddy",
    "cpa",
    "cpa-usage-keeper",
    "mihomo",
    "healthd",
    "openlist",
    "qbittorrent",
    "sshd",
  ].map((name, i) => ({
    Name: name,
    Label:
      name === "caddy"
        ? "AriaNg / Caddy"
        : name === "cpa"
          ? "CPA · AI API"
          : name === "cpa-usage-keeper"
            ? "CPA Usage Keeper"
          : name === "mihomo"
            ? "Mihomo · 代理核心"
          : name,
    State: "运行中",
    PID: 4400 + i,
    UptimeSec: 43000,
    Port:
      {
        openlist: 5244,
        aria2: 6800,
        caddy: 6880,
        cpa: 8317,
        "cpa-usage-keeper": 8320,
        mihomo: 17890,
        qbittorrent: 8080,
        sshd: 8022,
      }[name as "openlist"] || 0,
    webUrl: "",
  })),
  aria2: {
    online: true,
    downloadSpeed: 10_400_000,
    uploadSpeed: 0,
    numActive: 4,
    numWaiting: 36,
    numPaused: 36,
    numStopped: 3,
    tasks: [],
  },
  storage: [
    {
      Name: "5CE9BC3029E46B06",
      Mount: "/mnt/media_rw/5CE9BC3029E46B06",
      Filesystem: "fuseblk",
      Model: "SSD 870 EVO 500G",
      Total: 450e9,
      Used: 286e9,
      Free: 164e9,
      UsedPercent: 63.6,
      External: true,
    },
  ],
  usb: [
    {
      VendorID: "174c",
      ProductID: "235c",
      Manufacturer: "Ugreen",
      Product: "Ugreen Storage Device",
      Speed: "5000",
    },
    {
      VendorID: "0bda",
      ProductID: "8153",
      Manufacturer: "Realtek",
      Product: "USB 10/100/1000 LAN",
      Speed: "5000",
    },
  ],
  networkDraft: {
    WANInterface: "",
    LANInterface: "",
    WANMode: "pppoe",
    LANAddress: "192.168.2.1/24",
    DHCPStart: "192.168.2.100",
    DHCPEnd: "192.168.2.249",
    DNS: "192.168.2.1",
    MTU: 1492,
  },
};

const gb = (value: number) =>
  `${(value / 1e9).toFixed(value >= 10e9 ? 0 : 1)} GB`;
const rate = (value: number) =>
  value >= 1e6
    ? `${(value / 1e6).toFixed(1)} MB/s`
    : value >= 1e3
      ? `${(value / 1e3).toFixed(0)} KB/s`
      : `${value.toFixed(0)} B/s`;
const bytes = (value: number) =>
  value >= 1e9
    ? `${(value / 1e9).toFixed(2)} GB`
    : value >= 1e6
      ? `${(value / 1e6).toFixed(1)} MB`
      : value >= 1e3
        ? `${(value / 1e3).toFixed(0)} KB`
        : `${value.toFixed(0)} B`;
const duration = (seconds: number) =>
  seconds < 60
    ? `${seconds} 秒`
    : seconds < 3600
      ? `${Math.floor(seconds / 60)} 分钟`
      : seconds < 86400
        ? `${Math.floor(seconds / 3600)} 小时 ${Math.floor((seconds % 3600) / 60)} 分`
        : `${Math.floor(seconds / 86400)} 天 ${Math.floor((seconds % 86400) / 3600)} 小时`;

export default function Home() {
  const [auth, setAuth] = useState<AuthState>("checking");
  const [data, setData] = useState<Snapshot>(demo);
  const [active, setActive] = useState<Tab>("overview");
  const [remoteMode, setRemoteMode] = useState<RemoteMode>("terminal");
  const [history, setHistory] = useState<HistoryPoint[]>([]);
  const [notice, setNotice] = useState("");
  const [logText, setLogText] = useState("");
  const [logService, setLogService] = useState("");
  const refreshSequence = useRef(0);

  const refresh = useCallback(async (quiet = false) => {
    const sequence = ++refreshSequence.current;
    try {
      const response = await fetch("/api/snapshot", { cache: "no-store" });
      if (sequence !== refreshSequence.current) return;
      if (response.status === 401) {
        setAuth("login");
        return;
      }
      if (!response.ok) throw new Error("API unavailable");
      const next = (await response.json()) as Snapshot;
      if (sequence !== refreshSequence.current) return;
      const wan =
        next.interfaces.find((i) => i.Name.startsWith("ppp")) ||
        next.interfaces.find((i) => i.Role.includes("WAN"));
      const lan =
        next.interfaces.find((i) => i.Role.includes("LAN")) ||
        next.interfaces.find((i) => i.Name === next.networkDraft.LANInterface);
      const memory = next.memory.Total
        ? (next.memory.Used / next.memory.Total) * 100
        : 0;
      const point: HistoryPoint = {
        at: Date.now(),
        cpu: next.cpu.usage,
        temperature: next.cpu.temperature,
        memory,
        wanDown: wan?.RxBps || 0,
        wanUp: wan?.TxBps || 0,
        lanDown: lan?.RxBps || 0,
        lanUp: lan?.TxBps || 0,
        ariaDown: next.aria2?.downloadSpeed || 0,
        ariaUp: next.aria2?.uploadSpeed || 0,
        batteryVoltage: next.power.Voltage,
        batteryCurrent: next.power.Current * 1000,
        batteryAverageCurrent: next.power.AverageCurrent * 1000,
        batteryCoulombCurrent: next.power.CoulombCurrent * 1000,
        batteryPower: next.power.BatteryWatts,
        batteryPercent: next.power.batteryPercent,
        chargeCounter: next.power.ChargeCounterMAh,
        usbVoltage: next.power.USBVoltage,
        usbCurrent: next.power.USBInputCurrent * 1000,
        usbPower: next.power.USBInputWatts,
      };
      setData(next);
      setAuth("ready");
      setHistory((old) => [...old.slice(-239), point]);
      if (!quiet) setNotice("数据已刷新");
    } catch {
      if (sequence !== refreshSequence.current) return;
      if (
        location.hostname === "localhost" ||
        location.hostname === "127.0.0.1"
      ) {
        setAuth("demo");
        setHistory((old) => [
          ...old.slice(-239),
          {
            at: Date.now(),
            cpu: demo.cpu.usage,
            temperature: demo.cpu.temperature,
            memory: (demo.memory.Used / demo.memory.Total) * 100,
            wanDown: 0,
            wanUp: 0,
            lanDown: 0,
            lanUp: 0,
            ariaDown: demo.aria2.downloadSpeed,
            ariaUp: 0,
            batteryVoltage: demo.power.Voltage,
            batteryCurrent: demo.power.Current * 1000,
            batteryAverageCurrent: demo.power.AverageCurrent * 1000,
            batteryCoulombCurrent: demo.power.CoulombCurrent * 1000,
            batteryPower: demo.power.BatteryWatts,
            batteryPercent: demo.power.batteryPercent,
            chargeCounter: demo.power.ChargeCounterMAh,
            usbVoltage: demo.power.USBVoltage,
            usbCurrent: demo.power.USBInputCurrent * 1000,
            usbPower: demo.power.USBInputWatts,
          },
        ]);
      } else if (!quiet) setNotice("暂时无法读取实时数据");
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    let timer = 0;
    const poll = async () => {
      await refresh(true);
      if (!stopped) timer = window.setTimeout(poll, 2500);
    };
    poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [refresh]);
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(""), 2200);
    return () => clearTimeout(timer);
  }, [notice]);

  if (auth === "checking")
    return (
      <div className="boot-screen">
        <span className="brand-mark">1+</span>
        <p>正在连接路由核心…</p>
      </div>
    );
  if (auth === "login") return <Login onSuccess={() => refresh(true)} />;

  const title = navItems.find((item) => item.id === active)?.label || "总览";
  const running = data.services.filter((s) => s.State === "运行中").length;
  const external =
    data.storage
      .filter((s) => s.External)
      .sort((a, b) => b.Total - a.Total)[0] || data.storage[0];
  const health = Math.max(
    0,
    100 -
      (data.cpu.temperature > 85 ? 20 : data.cpu.temperature > 75 ? 8 : 0) -
      (data.power.BatteryTemp > 45 ? 8 : 0) -
      data.services.filter((s) => s.State === "停止").length * 5,
  );
  const openRemote = (mode: RemoteMode) => {
    setRemoteMode(mode);
    setActive("remote");
  };

  return (
    <main className="admin-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">1+</span>
          <span className="brand-copy">
            <strong>OnePlus</strong>
            <small>Router OS</small>
          </span>
        </div>
        <nav aria-label="主要导航">
          {navItems.map((item) => (
            <button
              className={`nav-item ${active === item.id ? "active" : ""}`}
              key={item.id}
              type="button"
              onClick={() => setActive(item.id)}
            >
              <span className="nav-icon">{item.icon}</span>
              <span>{item.label}</span>
            </button>
          ))}
        </nav>
        <div className="sidebar-foot">
          <span className="status-dot" />
          <span>
            <strong>路由核心正常</strong>
            <small>已运行 {duration(data.device.uptimeSec)}</small>
          </span>
        </div>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <p className="eyebrow">{title}</p>
            <h1>
              {active === "overview"
                ? `一加 7 Pro 状态${health >= 90 ? "稳定" : "需要注意"}`
                : title}
            </h1>
          </div>
          <div className="top-actions">
            {auth === "demo" && <span className="demo-pill">预览数据</span>}
            {data.trustedLocal && auth !== "demo" && (
              <span className="local-pill">本机免密</span>
            )}
            <span className="live-pill">
              <i /> 实时
            </span>
            <button type="button" onClick={() => refresh()}>
              刷新
            </button>
            {!data.trustedLocal && (
              <button
                type="button"
                onClick={async () => {
                  await fetch("/api/logout", { method: "POST" });
                  setAuth("login");
                }}
              >
                退出
              </button>
            )}
          </div>
        </header>

        {active === "overview" && (
          <Overview
            data={data}
            history={history}
            health={health}
            running={running}
            external={external}
            setActive={setActive}
            onRemote={openRemote}
          />
        )}
        {active === "network" && (
          <Network data={data} history={history} onNotice={setNotice} />
        )}
        {active === "clients" && (
          <Clients data={data} refresh={refresh} setNotice={setNotice} />
        )}
        {active === "services" && (
          <Services
            data={data}
            history={history}
            refresh={refresh}
            setNotice={setNotice}
            onLog={async (name) => {
              const r = await fetch(
                `/api/logs?service=${encodeURIComponent(name)}`,
              );
              if (r.ok) {
                const body = (await r.json()) as { log?: string };
                setLogService(name);
                setLogText(body.log || "暂无日志");
              }
            }}
          />
        )}
        {active === "mihomo" && (
          <MihomoManager data={data} refresh={refresh} setNotice={setNotice} />
        )}
        {active === "storage" && <Storage data={data} />}
        {active === "system" && (
          <System data={data} history={history} onRefresh={() => refresh(true)} />
        )}
        {active === "remote" && (
          <RemoteMaintenance mode={remoteMode} onMode={setRemoteMode} />
        )}
      </section>
      {notice && <div className="toast">{notice}</div>}
      {logService && (
        <div className="modal-backdrop" onClick={() => setLogService("")}>
          <section className="log-modal" onClick={(e) => e.stopPropagation()}>
            <header>
              <strong>{logService} 日志</strong>
              <button type="button" onClick={() => setLogService("")}>
                关闭
              </button>
            </header>
            <pre>{logText}</pre>
          </section>
        </div>
      )}
    </main>
  );
}

function Login({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPassword] = useState("");
  const [remember, setRemember] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const r = await fetch("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password, remember }),
      });
      if (!r.ok) {
        const b = (await r.json()) as { error?: string };
        throw new Error(b.error || "登录失败");
      }
      onSuccess();
    } catch (e) {
      setError(e instanceof Error ? e.message : "登录失败");
    } finally {
      setBusy(false);
    }
  };
  return (
    <main className="login-screen">
      <section className="login-card">
        <div className="login-brand">
          <span className="brand-mark">1+</span>
          <div>
            <strong>OnePlus Router OS</strong>
            <small>ALL-IN-ONE CONTROL CENTER</small>
          </div>
        </div>
        <p className="eyebrow">安全管理入口</p>
        <h1>管理你的软路由</h1>
        <p className="login-copy">
          监控网络、温度、PD供电、存储与全部后台服务。
        </p>
        <form onSubmit={submit}>
          <label>
            管理员密码
            <input
              autoFocus
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="输入初始管理员密码"
            />
          </label>
          <label className="remember-login">
            <input
              type="checkbox"
              checked={remember}
              onChange={(e) => setRemember(e.target.checked)}
            />
            <span>记住密码（长期有效）</span>
          </label>
          {error && <p className="form-error">{error}</p>}
          <button disabled={busy} type="submit">
            {busy ? "正在验证…" : "进入控制台"}
          </button>
        </form>
        <small className="login-hint">
          勾选后持续保持登录，主动退出前不会失效；不保存明文密码
        </small>
      </section>
    </main>
  );
}

function Overview({
  data,
  history,
  health,
  running,
  external,
  setActive,
  onRemote,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  health: number;
  running: number;
  external?: Snapshot["storage"][number];
  setActive: (tab: Tab) => void;
  onRemote: (mode: RemoteMode) => void;
}) {
  const wan =
    data.interfaces.find((i) => i.Name.startsWith("ppp")) ||
    data.interfaces.find((i) => i.Role.includes("WAN"));
  const lan =
    data.interfaces.find((i) => i.Role.includes("LAN")) ||
    data.interfaces.find((i) => i.Name === data.networkDraft.LANInterface);
  const traffic = wan || lan;
  const wanOnline = Boolean(wan?.Addresses?.length);
  const lanOnline = Boolean(lan?.Addresses?.length);
  const metrics = [
    {
      label: "CPU 负载",
      value: `${data.cpu.usage.toFixed(1)}%`,
      detail: `${data.cpu.Online} 核 · ${data.cpu.avgMHz} MHz`,
      tone: "cyan",
    },
    {
      label: "最高核心温度",
      value: `${data.cpu.temperature.toFixed(1)}°C`,
      detail: data.cpu.temperature < 80 ? "未触发高温警告" : "温度偏高",
      tone: data.cpu.temperature < 80 ? "green" : "amber",
    },
    {
      label: "内存",
      value: gb(data.memory.Used),
      detail: `可用 ${gb(data.memory.Available)}`,
      tone: "violet",
    },
    {
      label: "PD 输入",
      value: data.power.usbOnline
        ? `${data.power.USBInputWatts.toFixed(1)} W`
        : "未接入",
      detail: data.power.usbOnline
        ? `手机口估算 · 合同 ${data.power.PDWattsMax.toFixed(1)} W`
        : data.power.Status,
      tone: "amber",
    },
  ];
  return (
    <div className="view">
      <div className="hero-grid">
        <article className="network-hero">
          <div className="hero-heading">
            <span>正式路由拓扑</span>
            <strong>
              <i /> {wanOnline && lanOnline ? "WAN / LAN 在线" : "检查链路"}
            </strong>
          </div>
          <div className="flow">
            <div className="flow-node">
              <small>WAN · PPPoE</small>
              <strong>{wanOnline ? "已拨号" : "未连接"}</strong>
              <span>
                {wan?.Addresses?.[0] || data.networkDraft.WANInterface}
              </span>
            </div>
            <div className="flow-line">
              <i />
              <i />
              <i />
            </div>
            <div className="phone-node">
              <span className="phone-ring">1+</span>
              <strong>{data.device.Model || "OnePlus 7 Pro"}</strong>
              <small>
                {lan?.Addresses?.[0] || data.networkDraft.LANAddress}
              </small>
            </div>
            <div className="flow-line live">
              <i />
              <i />
              <i />
            </div>
            <div className="flow-node right">
              <small>LAN · AP 桥接</small>
              <strong>{lanOnline ? "局域网在线" : "未连接"}</strong>
              <span>{lan?.Name || data.networkDraft.LANInterface}</span>
            </div>
          </div>
          <div className="traffic-row">
            <span>WAN 实时吞吐</span>
            <strong>↓ {rate(traffic?.RxBps || 0)}</strong>
            <strong>↑ {rate(traffic?.TxBps || 0)}</strong>
          </div>
        </article>
        <article className="quick-card">
          <div>
            <span className="quick-label">整机健康度</span>
            <strong className="health-score">{health}</strong>
            <small>/ 100</small>
          </div>
          <div
            className="health-ring"
            style={{
              background: `radial-gradient(circle,#102631 57%,transparent 58%),conic-gradient(var(--green) 0 ${health}%,#27414e ${health}%)`,
            }}
          >
            <span>{health >= 90 ? "稳定" : "注意"}</span>
          </div>
          <p>
            <i /> {running} 个服务运行，{data.usb.length} 个USB设备
          </p>
        </article>
      </div>
      <section className="metrics-grid">
        {metrics.map((m) => (
          <article className="metric-card" key={m.label}>
            <div className={`metric-glyph ${m.tone}`}>
              <span />
            </div>
            <p>{m.label}</p>
            <strong>{m.value}</strong>
            <small>{m.detail}</small>
          </article>
        ))}
      </section>
      <section className="remote-tools panel">
        <div className="remote-tools-copy">
          <p className="eyebrow">远程维护</p>
          <h2>浏览器内维护</h2>
        </div>
        <div className="remote-tools-actions">
          <button type="button" onClick={() => onRemote("screen")}>
            <span>SCREEN</span>
            <strong>网页控制屏幕</strong>
            <small>按需启动 · 浏览器内显示</small>
          </button>
          <button type="button" onClick={() => onRemote("terminal")}>
            <span>SSH</span>
            <strong>网页终端</strong>
            <small>普通 / root shell</small>
          </button>
        </div>
      </section>
      <section className="lower-grid">
        <article className="panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">最近 10 分钟</p>
              <h2>CPU 与核心温度</h2>
            </div>
            <strong>{data.cpu.usage.toFixed(1)}%</strong>
          </div>
          <TimeSeriesChart
            history={history}
            series={[
              { label: "CPU %", color: "#30c7d8", value: (p) => p.cpu },
              {
                label: "温度 °C",
                color: "#ffad66",
                value: (p) => p.temperature,
              },
            ]}
            format={(value) => value.toFixed(1)}
            domain={[0, 100]}
          />
        </article>
        <article className="panel storage-panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">存储</p>
              <h2>{external?.Model || "等待外置存储"}</h2>
            </div>
            <button type="button" onClick={() => setActive("storage")}>
              详情
            </button>
          </div>
          {external ? (
            <>
              <div className="storage-number">
                <strong>{(external.Free / 1e9).toFixed(0)}</strong>
                <span>GB 可用</span>
              </div>
              <div className="storage-bar">
                <i style={{ width: `${external.UsedPercent}%` }} />
              </div>
              <div className="storage-legend">
                <span>
                  <i className="used" /> 已用 {gb(external.Used)}
                </span>
                <span>
                  <i /> 总计 {gb(external.Total)}
                </span>
              </div>
            </>
          ) : (
            <p className="empty-copy">未检测到外置硬盘</p>
          )}
        </article>
      </section>
    </div>
  );
}

function RemoteMaintenance({
  mode,
  onMode,
}: {
  mode: RemoteMode;
  onMode: (mode: RemoteMode) => void;
}) {
  const [root, setRoot] = useState(false);
  return (
    <div
      className={`view remote-console ${mode === "screen" ? "screen-mode" : ""}`}
    >
      <article className="panel remote-console-hero">
        <div>
          <p className="eyebrow">远程维护</p>
          <h2>浏览器控制中心</h2>
        </div>
        <div className="remote-mode-switch" role="tablist" aria-label="远程维护方式">
          <button
            type="button"
            className={mode === "terminal" ? "active" : ""}
            onClick={() => onMode("terminal")}
          >
            网页终端
          </button>
          <button
            type="button"
            className={mode === "screen" ? "active" : ""}
            onClick={() => onMode("screen")}
          >
            屏幕控制
          </button>
        </div>
      </article>
      {mode === "terminal" ? (
        <WebTerminal root={root} onRootChange={setRoot} />
      ) : (
        <WebScreenControl />
      )}
    </div>
  );
}

function WebTerminal({
  root,
  onRootChange,
}: {
  root: boolean;
  onRootChange: (root: boolean) => void;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [connection, setConnection] = useState<
    "connecting" | "connected" | "closed" | "error"
  >("connecting");
  const [enabled, setEnabled] = useState(true);
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    if (!enabled || !hostRef.current) return;
    let disposed = false;
    let socket: WebSocket | null = null;
    let resizeObserver: ResizeObserver | null = null;
    let dataSubscription: { dispose: () => void } | null = null;
    let terminal: import("@xterm/xterm").Terminal | null = null;
    setConnection("connecting");

    Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")])
      .then(([xterm, fit]) => {
        if (disposed || !hostRef.current) return;
        terminal = new xterm.Terminal({
          cursorBlink: true,
          cursorStyle: "bar",
          convertEol: true,
          fontFamily:
            '"Cascadia Mono", "SFMono-Regular", Consolas, "Liberation Mono", monospace',
          fontSize: 13,
          lineHeight: 1.2,
          scrollback: 5000,
          theme: {
            background: "#06151d",
            foreground: "#d6e5eb",
            cursor: "#5ce1e6",
            selectionBackground: "#25556699",
            black: "#071219",
            brightBlack: "#607884",
            red: "#ff7b83",
            green: "#58d79b",
            yellow: "#ffc570",
            blue: "#65aef4",
            magenta: "#b49bff",
            cyan: "#5ce1e6",
            white: "#d6e5eb",
          },
        });
        const fitAddon = new fit.FitAddon();
        terminal.loadAddon(fitAddon);
        terminal.open(hostRef.current);

        const scheme = location.protocol === "https:" ? "wss" : "ws";
        socket = new WebSocket(
          `${scheme}://${location.host}/api/terminal?root=${root ? "1" : "0"}`,
        );
        socket.binaryType = "arraybuffer";

        const sendResize = () => {
          if (!terminal || socket?.readyState !== WebSocket.OPEN) return;
          socket.send(
            JSON.stringify({
              type: "resize",
              cols: terminal.cols,
              rows: terminal.rows,
            }),
          );
        };
        const fitTerminal = () => {
          try {
            fitAddon.fit();
            sendResize();
          } catch {
            // The container can briefly be hidden while switching tabs.
          }
        };

        socket.onopen = () => {
          if (disposed) return;
          setConnection("connected");
          window.setTimeout(() => {
            fitTerminal();
            terminal?.focus();
          }, 30);
        };
        socket.onmessage = (event) => {
          if (!terminal) return;
          if (event.data instanceof ArrayBuffer) {
            terminal.write(new Uint8Array(event.data));
          } else if (typeof event.data === "string") {
            terminal.write(event.data);
          }
        };
        socket.onerror = () => !disposed && setConnection("error");
        socket.onclose = () => !disposed && setConnection("closed");
        dataSubscription = terminal.onData((data) => {
          if (socket?.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify({ type: "input", data }));
          }
        });
        resizeObserver = new ResizeObserver(fitTerminal);
        resizeObserver.observe(hostRef.current);
      })
      .catch(() => !disposed && setConnection("error"));

    return () => {
      disposed = true;
      resizeObserver?.disconnect();
      dataSubscription?.dispose();
      socket?.close(1000, "page closed");
      terminal?.dispose();
    };
  }, [enabled, generation, root]);

  const reconnect = () => {
    setEnabled(true);
    setGeneration((value) => value + 1);
  };
  const labels = {
    connecting: "正在连接",
    connected: root ? "ROOT 已连接" : "TERMUX 已连接",
    closed: "连接已关闭",
    error: "连接失败",
  };

  return (
    <article className="panel terminal-panel">
      <header className="terminal-toolbar">
        <div>
          <span className={`terminal-state ${connection}`} />
          <strong>{labels[connection]}</strong>
          <small>{root ? "root shell" : "Termux shell"}</small>
        </div>
        <div className="terminal-actions">
          <label>
            <input
              type="checkbox"
              checked={root}
              onChange={(event) => onRootChange(event.target.checked)}
            />
            ROOT
          </label>
          {enabled && connection === "connected" ? (
            <button type="button" onClick={() => setEnabled(false)}>
              断开
            </button>
          ) : (
            <button type="button" onClick={reconnect}>
              重新连接
            </button>
          )}
        </div>
      </header>
      <div className="terminal-host" ref={hostRef} aria-label="手机路由器终端" />
    </article>
  );
}

function WebScreenControl() {
  return <BrowserScreenControl />;
}

function TimeSeriesChart({
  history,
  series,
  format,
  zeroBaseline = true,
  domain,
}: {
  history: HistoryPoint[];
  series: {
    label: string;
    color: string;
    value: (point: HistoryPoint) => number;
  }[];
  format: (value: number) => string;
  zeroBaseline?: boolean;
  domain?: readonly [number, number];
}) {
  const points = history.length
    ? history
    : [
        {
          at: 0,
          cpu: 0,
          temperature: 0,
          memory: 0,
          wanDown: 0,
          wanUp: 0,
          lanDown: 0,
          lanUp: 0,
          ariaDown: 0,
          ariaUp: 0,
          batteryVoltage: 0,
          batteryCurrent: 0,
          batteryAverageCurrent: 0,
          batteryCoulombCurrent: 0,
          batteryPower: 0,
          batteryPercent: 0,
          chargeCounter: 0,
          usbVoltage: 0,
          usbCurrent: 0,
          usbPower: 0,
        },
      ];
  const windowMs = 10 * 60 * 1000;
  const endAt = points[points.length - 1].at;
  const startAt = endAt - windowMs;
  const visiblePoints = points.filter((point) => point.at >= startAt);
  const values = series
    .flatMap((item) => visiblePoints.map(item.value))
    .filter(Number.isFinite);
  const rawMin = values.length ? Math.min(...values) : 0;
  const rawMax = values.length ? Math.max(...values) : 0;
  const naturalRange = Math.max(Math.abs(rawMax - rawMin), Math.abs(rawMax) * 0.02, 0.01);
  const min = domain
    ? domain[0]
    : zeroBaseline
      ? Math.min(0, rawMin * 1.12)
      : rawMin - naturalRange * 0.12;
  const max = domain
    ? domain[1]
    : zeroBaseline
      ? Math.max(1, rawMax * 1.12)
      : rawMax + naturalRange * 0.12;
  const range = Math.max(0.0001, max - min);
  const ticks = [0, 0.25, 0.5, 0.75, 1];
  const yTicks = ticks.map((position) => max - range * position);
  const width = 760;
  const height = 190;
  const top = 10;
  const bottom = 18;
  const line = (getter: (point: HistoryPoint) => number) =>
    visiblePoints
      .map(
        (point) =>
          `${Math.max(0, Math.min(width, ((point.at - startAt) / windowMs) * width))},${Math.max(top, Math.min(height - bottom, top + ((max - getter(point)) / range) * (height - top - bottom)))}`,
      )
      .join(" ");
  const timeLabel = (at: number) =>
    at > 0
      ? new Date(at).toLocaleTimeString("zh-CN", {
          hour12: false,
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })
      : "--:--:--";
  return (
    <div className="time-chart">
      <div className="chart-legend">
        {series.map((item) => {
          const current = item.value(points[points.length - 1]);
          return (
            <span key={item.label}>
              <i style={{ background: item.color }} />
              <b>{item.label}</b>
              <strong>{format(current)}</strong>
            </span>
          );
        })}
      </div>
      <div className="chart-frame">
        <div className="chart-y-axis" aria-label="数值范围">
          {yTicks.map((value, index) => (
            <span key={`${index}-${value}`}>{format(value)}</span>
          ))}
        </div>
        <svg
          viewBox={`0 0 ${width} ${height}`}
          preserveAspectRatio="none"
          aria-label="时间序列曲线"
        >
          <g className="chart-grid">
            {ticks.map((value) => (
              <line
                key={value}
                x1="0"
                x2={width}
                y1={top + value * (height - top - bottom)}
                y2={top + value * (height - top - bottom)}
              />
            ))}
          </g>
          {series.map((item) => (
            <polyline
              key={item.label}
              points={line(item.value)}
              fill="none"
              stroke={item.color}
              strokeWidth="2.4"
              vectorEffect="non-scaling-stroke"
            />
          ))}
        </svg>
        <div className="chart-axis">
          <span>{timeLabel(startAt)}</span>
          <span>{timeLabel(startAt + windowMs / 2)}</span>
          <span>{timeLabel(endAt)}</span>
        </div>
      </div>
    </div>
  );
}

function Network({
  data,
  history,
  onNotice,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  onNotice: (value: string) => void;
}) {
  const [draft, setDraft] = useState(data.networkDraft);
  const update = (key: keyof NetworkDraft, value: string | number) =>
    setDraft((old) => ({ ...old, [key]: value }));
  const save = async () => {
    const r = await fetch("/api/network-draft", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(draft),
    });
    const b = (await r.json()) as { error?: string };
    onNotice(r.ok ? "网络配置记录已保存" : b.error || "保存失败");
  };
  return (
    <div className="view">
      <div className="section-heading">
        <div>
          <p className="eyebrow">正式部署 · 接口与流量</p>
          <h2>网络接口</h2>
        </div>
        <span className="state-good">PPPoE 路由已接管</span>
      </div>
      <article className="panel history-panel">
        <div className="panel-title">
          <div>
            <p className="eyebrow">最近 10 分钟</p>
            <h2>WAN / LAN 实时吞吐</h2>
          </div>
        </div>
        <TimeSeriesChart
          history={history}
          series={[
            { label: "WAN 下载", color: "#30c7d8", value: (p) => p.wanDown },
            { label: "WAN 上传", color: "#52d395", value: (p) => p.wanUp },
            { label: "LAN 发送", color: "#a897ff", value: (p) => p.lanUp },
          ]}
          format={rate}
        />
      </article>
      <div className="interface-grid">
        {data.interfaces.map((iface) => (
          <article className="panel interface-card" key={iface.Name}>
            <header>
              <div>
                <span
                  className={`interface-light ${iface.State === "up" ? "up" : ""}`}
                />
                <strong>{iface.Name}</strong>
              </div>
              <span>{iface.Role}</span>
            </header>
            <div className="interface-address">
              {iface.Addresses?.[0] || "未分配 IP"}
            </div>
            <dl>
              <div>
                <dt>链路</dt>
                <dd>
                  {iface.Carrier === "1"
                    ? `${iface.SpeedMbps > 0 ? iface.SpeedMbps : ""} Mbps`
                    : iface.State === "up"
                      ? "已启用"
                      : "未连接"}
                </dd>
              </div>
              <div>
                <dt>MTU</dt>
                <dd>{iface.MTU}</dd>
              </div>
              <div>
                <dt>接收</dt>
                <dd>{rate(iface.RxBps)}</dd>
              </div>
              <div>
                <dt>发送</dt>
                <dd>{rate(iface.TxBps)}</dd>
              </div>
            </dl>
            <small>{iface.MAC}</small>
          </article>
        ))}
      </div>
      <article className="panel config-panel">
        <div className="panel-title">
          <div>
            <p className="eyebrow">当前生效拓扑</p>
            <h2>WAN / LAN 配置记录</h2>
          </div>
          <button type="button" onClick={save}>
            保存记录
          </button>
        </div>
        <div className="form-grid">
          <label>
            WAN 网口
            <select
              value={draft.WANInterface}
              onChange={(e) => update("WANInterface", e.target.value)}
            >
              {data.interfaces
                .filter((i) => i.Name.startsWith("eth"))
                .map((i) => (
                  <option key={i.Name}>{i.Name}</option>
                ))}
            </select>
          </label>
          <label>
            LAN 网口
            <select
              value={draft.LANInterface}
              onChange={(e) => update("LANInterface", e.target.value)}
            >
              {data.interfaces
                .filter((i) => i.Name.startsWith("eth"))
                .map((i) => (
                  <option key={i.Name}>{i.Name}</option>
                ))}
            </select>
          </label>
          <label>
            WAN 模式
            <select
              value={draft.WANMode}
              onChange={(e) => update("WANMode", e.target.value)}
            >
              <option value="pppoe">PPPoE 拨号</option>
              <option value="dhcp">DHCP</option>
              <option value="static">静态地址</option>
            </select>
          </label>
          <label>
            MTU
            <input
              type="number"
              value={draft.MTU}
              onChange={(e) => update("MTU", Number(e.target.value))}
            />
          </label>
          <label>
            LAN 地址
            <input
              value={draft.LANAddress}
              onChange={(e) => update("LANAddress", e.target.value)}
            />
          </label>
          <label>
            DNS
            <input
              value={draft.DNS}
              onChange={(e) => update("DNS", e.target.value)}
            />
          </label>
          <label>
            DHCP 起始
            <input
              value={draft.DHCPStart}
              onChange={(e) => update("DHCPStart", e.target.value)}
            />
          </label>
          <label>
            DHCP 结束
            <input
              value={draft.DHCPEnd}
              onChange={(e) => update("DHCPEnd", e.target.value)}
            />
          </label>
        </div>
        <p className="safe-note">
          当前正式映射为 eth0（CM418 / RTL8153）连接光猫并承载 PPPoE，eth1（TIH
          USB NetCard）连接 AP，LAN 网关为
          192.168.2.1。这里保存的是运维记录，不会直接重启网络或中断现有连接。
        </p>
      </article>
    </div>
  );
}

function Clients({
  data,
  refresh,
  setNotice,
}: {
  data: Snapshot;
  refresh: (quiet?: boolean) => Promise<void>;
  setNotice: (value: string) => void;
}) {
  const [editingMac, setEditingMac] = useState("");
  const [staticIP, setStaticIP] = useState("");
  const [pendingMac, setPendingMac] = useState("");
  const [error, setError] = useState("");

  const beginEdit = (client: Snapshot["clients"][number]) => {
    setEditingMac(client.MAC);
    setStaticIP(client.StaticIP || client.IP);
    setError("");
  };

  const readError = async (response: Response, fallback: string) => {
    try {
      const body = (await response.json()) as { error?: string };
      return body.error || fallback;
    } catch {
      return fallback;
    }
  };

  const saveStaticLease = async (mac: string) => {
    const ip = staticIP.trim();
    const octets = ip.split(".");
    if (
      octets.length !== 4 ||
      octets.some(
        (octet) =>
          !/^\d{1,3}$/.test(octet) ||
          Number(octet) > 255 ||
          (octet.length > 1 && octet.startsWith("0")),
      )
    ) {
      setError("请输入有效的 IPv4 地址，例如 192.168.2.200");
      return;
    }

    setPendingMac(mac);
    setError("");
    try {
      const response = await fetch("/api/clients/static-lease", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mac, ip }),
      });
      if (!response.ok) {
        throw new Error(await readError(response, "设置静态租约失败"));
      }
      setEditingMac("");
      setNotice(`已将 ${mac} 固定为 ${ip}`);
      await refresh(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "设置静态租约失败");
    } finally {
      setPendingMac("");
    }
  };

  const clearStaticLease = async (client: Snapshot["clients"][number]) => {
    if (
      !window.confirm(
        `清除 ${client.Hostname || client.MAC} 的静态租约？设备下次续租时可能获得其他地址。`,
      )
    ) {
      return;
    }

    setPendingMac(client.MAC);
    setError("");
    try {
      const response = await fetch("/api/clients/static-lease", {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mac: client.MAC }),
      });
      if (!response.ok) {
        throw new Error(await readError(response, "清除静态租约失败"));
      }
      setEditingMac("");
      setNotice(`已清除 ${client.Hostname || client.MAC} 的静态租约`);
      await refresh(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "清除静态租约失败");
    } finally {
      setPendingMac("");
    }
  };

  return (
    <div className="view">
      <div className="section-heading">
        <div>
          <p className="eyebrow">eth1 · AP 桥接局域网</p>
          <h2>LAN 客户端</h2>
        </div>
        <strong className="count-badge">{data.clients.length} 台</strong>
      </div>
      <article className="panel clients-panel">
        {error && !editingMac && (
          <p className="static-lease-page-error" role="alert">
            {error}
          </p>
        )}
        <div className="table-wrap">
          <table className="clients-table">
            <thead>
              <tr>
                <th>设备</th>
                <th>IP 地址</th>
                <th>MAC 地址</th>
                <th>接口</th>
                <th>来源</th>
                <th>状态</th>
                <th>地址分配</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {data.clients.map((client) => {
                const busy = pendingMac === client.MAC;
                const editing = editingMac === client.MAC;
                return (
                  <Fragment key={`${client.MAC}-${client.IP}`}>
                    <tr className={editing ? "client-row editing" : "client-row"}>
                      <td data-label="设备">
                        <div className="device-cell">
                          <span>
                            {(client.Hostname || "?").slice(0, 2).toUpperCase()}
                          </span>
                          <strong>{client.Hostname || "未知设备"}</strong>
                        </div>
                      </td>
                      <td data-label="当前 IP">{client.IP}</td>
                      <td data-label="MAC 地址" className="mono">
                        {client.MAC}
                      </td>
                      <td data-label="接口">{client.Interface || "LAN"}</td>
                      <td data-label="来源">{client.Source}</td>
                      <td data-label="状态">
                        <span
                          className={
                            client.State === "在线"
                              ? "state-good"
                              : "state-muted"
                          }
                        >
                          {client.State || "租约"}
                        </span>
                      </td>
                      <td data-label="地址分配">
                        <div className="lease-state">
                          <span
                            className={
                              client.StaticLease
                                ? "lease-badge static"
                                : "lease-badge dynamic"
                            }
                          >
                            {client.StaticLease ? "静态租约" : "动态租约"}
                          </span>
                          {client.StaticLease && (
                            <small>{client.StaticIP || client.IP}</small>
                          )}
                        </div>
                      </td>
                      <td data-label="操作">
                        <div className="client-actions">
                          <button
                            type="button"
                            onClick={() => beginEdit(client)}
                            disabled={busy}
                          >
                            {client.StaticLease ? "修改" : "设置固定"}
                          </button>
                          {client.StaticLease && (
                            <button
                              className="danger-quiet"
                              type="button"
                              onClick={() => void clearStaticLease(client)}
                              disabled={busy}
                            >
                              {busy ? "处理中…" : "清除"}
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                    {editing && (
                      <tr className="client-editor-row">
                        <td colSpan={8}>
                          <form
                            className="static-lease-editor"
                            onSubmit={(event) => {
                              event.preventDefault();
                              void saveStaticLease(client.MAC);
                            }}
                          >
                            <div>
                              <strong>固定设备地址</strong>
                              <small>
                                为 {client.Hostname || client.MAC} 保留一个 LAN
                                地址
                              </small>
                            </div>
                            <label>
                              <span>固定 IP</span>
                              <input
                                value={staticIP}
                                onChange={(event) => {
                                  setStaticIP(event.target.value);
                                  setError("");
                                }}
                                inputMode="decimal"
                                autoComplete="off"
                                spellCheck={false}
                                aria-invalid={Boolean(error)}
                                disabled={busy}
                                autoFocus
                              />
                            </label>
                            <div className="static-lease-actions">
                              <button type="submit" disabled={busy}>
                                {busy ? "正在应用…" : "应用"}
                              </button>
                              <button
                                type="button"
                                onClick={() => {
                                  setEditingMac("");
                                  setError("");
                                }}
                                disabled={busy}
                              >
                                取消
                              </button>
                            </div>
                            {error && (
                              <p className="static-lease-error" role="alert">
                                {error}
                              </p>
                            )}
                          </form>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
        {data.clients.length === 0 && (
          <p className="empty-copy">
            当前 eth1 局域网没有有效 DHCP 租约或在线邻居。
          </p>
        )}
      </article>
      <p className="safe-note">
        列表现在只统计 LAN 网口 eth1 /
        192.168.2.0/24，已排除光猫上联、路由器自身地址和过期租约。“在线”来自当前邻居表，“租约”表示设备曾获取地址但此刻未必活跃。设置固定地址后，设备可能需要重新连接网络或续租才会切换到新地址。
      </p>
    </div>
  );
}

// Kept temporarily for rollback compatibility with the legacy single-subscription API.
// eslint-disable-next-line @typescript-eslint/no-unused-vars
function MihomoManagerLegacy({
  data,
  refresh,
  setNotice,
}: {
  data: Snapshot;
  refresh: (quiet?: boolean) => Promise<void>;
  setNotice: (value: string) => void;
}) {
  const [info, setInfo] = useState<SubscriptionInfo>({
    configured: false,
    source: "",
    lastUpdated: "",
    autoUpdateHours: 6,
  });
  const [subscriptionUrl, setSubscriptionUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const service = data.services.find((item) => item.Name === "mihomo");
  const running = Boolean(service?.PID);

  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/mihomo/subscription", {
      cache: "no-store",
      signal: controller.signal,
    })
      .then((response) =>
        response.ok ? (response.json() as Promise<SubscriptionInfo>) : null,
      )
      .then((next) => {
        if (next) setInfo(next);
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, []);

  const serviceAction = async (action: "start" | "stop" | "restart") => {
    if (
      action === "stop" &&
      !confirm("确定停止 Mihomo？停止后代理流量将不可用。")
    )
      return;
    const response = await fetch("/api/services/action", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ Name: "mihomo", Action: action }),
    });
    const body = (await response.json()) as { error?: string };
    setNotice(
      response.ok
        ? `Mihomo ${action === "start" ? "已启动" : action === "stop" ? "已停止" : "已重启"}`
        : body.error || "操作失败",
    );
    setTimeout(() => refresh(true), 800);
  };

  const updateNow = async () => {
    setBusy(true);
    try {
      const response = await fetch("/api/mihomo/subscription", {
        method: "POST",
      });
      const body = (await response.json()) as {
        error?: string;
        subscription?: SubscriptionInfo;
      };
      if (!response.ok) throw new Error(body.error || "订阅更新失败");
      if (body.subscription) setInfo(body.subscription);
      setNotice("订阅已更新，Mihomo 已加载新配置");
      setTimeout(() => refresh(true), 800);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "订阅更新失败");
    } finally {
      setBusy(false);
    }
  };

  const saveAndUpdate = async (event: FormEvent) => {
    event.preventDefault();
    if (!subscriptionUrl.trim()) {
      setNotice("请粘贴订阅地址");
      return;
    }
    setBusy(true);
    try {
      const response = await fetch("/api/mihomo/subscription", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: subscriptionUrl.trim() }),
      });
      const body = (await response.json()) as SubscriptionInfo & {
        error?: string;
      };
      if (!response.ok) throw new Error(body.error || "保存订阅失败");
      setInfo(body);
      setSubscriptionUrl("");
      const updateResponse = await fetch("/api/mihomo/subscription", {
        method: "POST",
      });
      const updateBody = (await updateResponse.json()) as {
        error?: string;
        subscription?: SubscriptionInfo;
      };
      if (!updateResponse.ok)
        throw new Error(updateBody.error || "地址已保存，但立即更新失败");
      if (updateBody.subscription) setInfo(updateBody.subscription);
      setNotice("新订阅已保存并生效");
      setTimeout(() => refresh(true), 800);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "订阅操作失败");
    } finally {
      setBusy(false);
    }
  };

  const openDashboard = () => {
    if (
      location.hostname !== "127.0.0.1" &&
      location.hostname !== "localhost"
    ) {
      setNotice("请在手机本机的 127.0.0.1:9090 打开管理后台");
      return;
    }
    window.open("http://127.0.0.1:9091/ui/", "_blank", "noopener");
  };

  const updated = info.lastUpdated
    ? new Date(info.lastUpdated).toLocaleString("zh-CN")
    : "尚未更新";
  return (
    <div className="view mihomo-manager-wrap">
      <article className="panel mihomo-manager">
        <header>
          <div>
            <p className="eyebrow">代理核心与订阅</p>
            <h2>Mihomo 管理</h2>
          </div>
          <span className={running ? "state-good" : "state-muted"}>
            {running ? "运行中" : "已停止"}
          </span>
        </header>
        <div className="mihomo-actions">
          <button
            type="button"
            onClick={() => serviceAction(running ? "stop" : "start")}
          >
            {running ? "停止 Mihomo" : "启动 Mihomo"}
          </button>
          <button type="button" onClick={() => serviceAction("restart")}>
            重启
          </button>
          <button type="button" onClick={openDashboard}>
            打开 MetaCubeXD
          </button>
        </div>
        <div className="subscription-status">
          <strong>{info.configured ? "当前订阅已配置" : "尚未配置订阅"}</strong>
          <span>
            {info.source || "粘贴订阅 URL 后即可使用"} · 最近更新 {updated} · 每{" "}
            {info.autoUpdateHours} 小时自动更新
          </span>
        </div>
        <form onSubmit={saveAndUpdate}>
          <input
            type="url"
            value={subscriptionUrl}
            onChange={(event) => setSubscriptionUrl(event.target.value)}
            placeholder="粘贴新的订阅 URL（保存后替换当前订阅）"
            autoComplete="off"
          />
          <button type="submit" disabled={busy}>
            {busy ? "处理中…" : "添加/替换并更新"}
          </button>
          <button
            type="button"
            disabled={busy || !info.configured}
            onClick={updateNow}
          >
            立即更新
          </button>
        </form>
        <p className="safe-note">
          MetaCubeXD 只负责切换节点和查看连接；订阅地址由这里管理，完整 URL
          不会回显到页面。
        </p>
      </article>
    </div>
  );
}

function MihomoManager({
  data,
  refresh,
  setNotice,
}: {
  data: Snapshot;
  refresh: (quiet?: boolean) => Promise<void>;
  setNotice: (value: string) => void;
}) {
  const [status, setStatus] = useState<MihomoStatus>({
    online: false,
    mode: "",
    version: "",
    proxyCount: 0,
    connections: 0,
    downloadTotal: 0,
    uploadTotal: 0,
    memory: 0,
    mixedPort: 17890,
    httpPort: 0,
    socksPort: 0,
    redirPort: 0,
    tproxyPort: 0,
    allowLan: false,
    bindAddress: "127.0.0.1",
    tunEnabled: false,
    tunStack: "",
    tunDevice: "",
    cpaLinked: false,
    cpaUsageEnabled: false,
    cpaAccountCount: 0,
    cpaProxyPort: 17891,
    cpaProxyGroup: "AI",
    cpaProxyNode: "",
    cpaAutoNode: "",
    cpaJapaneseNodeCount: 0,
  });
  const [settings, setSettings] = useState<MihomoSettings>({
    mixedPort: 17890,
    httpPort: 0,
    socksPort: 0,
    redirPort: 0,
    tproxyPort: 0,
    allowLan: false,
    bindAddress: "127.0.0.1",
  });
  const [groups, setGroups] = useState<MihomoProxyGroup[]>([]);
  const [delays, setDelays] = useState<Record<string, number>>({});
  const [wholeHomeProxy, setWholeHomeProxy] = useState<WholeHomeProxy>({
    enabled: false,
    active: false,
    ready: false,
    localApps: false,
    mode: "direct",
    engine: "mihomo",
    tunDevice: "",
    listener: "127.0.0.1:17890",
  });
  const [traffic, setTraffic] = useState({ download: 0, upload: 0 });
  const [logText, setLogText] = useState("");
  const [showLogs, setShowLogs] = useState(false);
  const [subscriptions, setSubscriptions] = useState<SubscriptionList>({
    activeId: "",
    items: [],
    autoUpdateHours: 6,
  });
  const [subscriptionName, setSubscriptionName] = useState("");
  const [subscriptionUrl, setSubscriptionUrl] = useState("");
  const [busy, setBusy] = useState("");
  const trafficPrevious = useRef({ download: 0, upload: 0, at: 0 });
  const running = Boolean(
    data.services.find((item) => item.Name === "mihomo")?.PID,
  );

  const loadMihomo = useCallback(async () => {
    const [
      statusResponse,
      subscriptionsResponse,
      settingsResponse,
      groupsResponse,
      wholeHomeProxyResponse,
    ] = await Promise.all([
      fetch("/api/mihomo/status", { cache: "no-store" }),
      fetch("/api/mihomo/subscriptions", { cache: "no-store" }),
      fetch("/api/mihomo/settings", { cache: "no-store" }),
      fetch("/api/mihomo/proxies", { cache: "no-store" }),
      fetch("/api/mihomo/whole-home-proxy", { cache: "no-store" }),
    ]);
    if (statusResponse.ok)
      setStatus((await statusResponse.json()) as MihomoStatus);
    if (subscriptionsResponse.ok)
      setSubscriptions(
        (await subscriptionsResponse.json()) as SubscriptionList,
      );
    if (settingsResponse.ok)
      setSettings((await settingsResponse.json()) as MihomoSettings);
    if (groupsResponse.ok)
      setGroups(
        ((await groupsResponse.json()) as { groups: MihomoProxyGroup[] })
          .groups,
      );
    if (wholeHomeProxyResponse.ok)
      setWholeHomeProxy(
        (await wholeHomeProxyResponse.json()) as WholeHomeProxy,
      );
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    Promise.all([
      fetch("/api/mihomo/status", {
        cache: "no-store",
        signal: controller.signal,
      }),
      fetch("/api/mihomo/subscriptions", {
        cache: "no-store",
        signal: controller.signal,
      }),
      fetch("/api/mihomo/settings", {
        cache: "no-store",
        signal: controller.signal,
      }),
      fetch("/api/mihomo/proxies", {
        cache: "no-store",
        signal: controller.signal,
      }),
      fetch("/api/mihomo/whole-home-proxy", {
        cache: "no-store",
        signal: controller.signal,
      }),
    ])
      .then(
        async ([
          statusResponse,
          subscriptionsResponse,
          settingsResponse,
          groupsResponse,
          wholeHomeProxyResponse,
        ]) => {
          if (statusResponse.ok)
            setStatus((await statusResponse.json()) as MihomoStatus);
          if (subscriptionsResponse.ok)
            setSubscriptions(
              (await subscriptionsResponse.json()) as SubscriptionList,
            );
          if (settingsResponse.ok)
            setSettings((await settingsResponse.json()) as MihomoSettings);
          if (groupsResponse.ok)
            setGroups(
              ((await groupsResponse.json()) as { groups: MihomoProxyGroup[] })
                .groups,
            );
          if (wholeHomeProxyResponse.ok)
            setWholeHomeProxy(
              (await wholeHomeProxyResponse.json()) as WholeHomeProxy,
            );
        },
      )
      .catch(() => undefined);
    return () => controller.abort();
  }, []);

  useEffect(() => {
    let stopped = false;
    let timer = 0;
    const poll = async () => {
      try {
        const connectionsResponse = await fetch(
          "/api/mihomo/connections?summary=1",
          { cache: "no-store" },
        );
        if (stopped) return;
        if (connectionsResponse.ok) {
          const next = (await connectionsResponse.json()) as MihomoMetrics;
          const now = performance.now();
          const previous = trafficPrevious.current;
          if (previous.at > 0 && now > previous.at) {
            const seconds = (now - previous.at) / 1000;
            setTraffic({
              download:
                Math.max(0, next.downloadTotal - previous.download) / seconds,
              upload: Math.max(0, next.uploadTotal - previous.upload) / seconds,
            });
          }
          trafficPrevious.current = {
            download: next.downloadTotal,
            upload: next.uploadTotal,
            at: now,
          };
          setStatus((old) => ({
            ...old,
            downloadTotal: next.downloadTotal,
            uploadTotal: next.uploadTotal,
            memory: next.memory,
            connections: next.connections,
          }));
        }
      } catch {
        /* The next poll will retry. */
      }
      if (!stopped) timer = window.setTimeout(poll, 1500);
    };
    timer = window.setTimeout(poll, 1000);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, []);

  const responseError = async (response: Response, fallback: string) => {
    const body = (await response.json().catch(() => ({}))) as {
      error?: string;
    };
    return body.error || fallback;
  };

  const finishLater = () =>
    window.setTimeout(() => {
      void loadMihomo();
      void refresh(true);
    }, 900);

  const serviceAction = async (action: "start" | "stop" | "restart") => {
    if (
      action === "stop" &&
      !confirm("确定停止 Mihomo？普通网络会自动直连，但 CPA/Codex 专线会暂时不可用。")
    )
      return;
    setBusy(`service-${action}`);
    try {
      if (action === "stop" && wholeHomeProxy.enabled) {
        const wholeHomeResponse = await fetch(
          "/api/mihomo/whole-home-proxy",
          {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ enabled: false }),
          },
        );
        if (!wholeHomeResponse.ok)
          throw new Error("无法先关闭全屋代理，已取消停止核心");
        setWholeHomeProxy(
          (await wholeHomeResponse.json()) as WholeHomeProxy,
        );
      }
      const response = await fetch("/api/services/action", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ Name: "mihomo", Action: action }),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "服务操作失败"));
      if (action === "stop") {
        setStatus((old) => ({
          ...old,
          online: false,
          version: "",
          proxyCount: 0,
          connections: 0,
          downloadTotal: 0,
          uploadTotal: 0,
          memory: 0,
          cpaLinked: false,
          cpaProxyNode: "",
          cpaAutoNode: "",
          cpaJapaneseNodeCount: 0,
        }));
        setGroups([]);
        setTraffic({ download: 0, upload: 0 });
      }
      setNotice(
        `Mihomo ${action === "start" ? "已启动" : action === "stop" ? "已停止" : "已重启"}`,
      );
      finishLater();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "服务操作失败");
    } finally {
      setBusy("");
    }
  };

  const changeMode = async (mode: "rule" | "global" | "direct") => {
    setBusy(`mode-${mode}`);
    try {
      const response = await fetch("/api/mihomo/mode", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode }),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "模式切换失败"));
      setStatus((old) => ({ ...old, mode }));
      setWholeHomeProxy((old) => ({
        ...old,
        enabled: mode !== "direct",
        active: mode !== "direct" && old.ready,
        mode,
      }));
      setNotice(
        `代理模式已切换为 ${mode === "rule" ? "规则" : mode === "global" ? "全局" : "直连"}`,
      );
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "模式切换失败");
    } finally {
      setBusy("");
    }
  };

  const saveSettings = async (event: FormEvent) => {
    event.preventDefault();
    setBusy("settings");
    try {
      const safeSettings = {
        ...settings,
        allowLan: false,
        bindAddress: "127.0.0.1",
      };
      const response = await fetch("/api/mihomo/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(safeSettings),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "端口设置保存失败"));
      setSettings((await response.json()) as MihomoSettings);
      setNotice("端口设置已保存，Mihomo 已安全重载");
      finishLater();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "端口设置保存失败");
    } finally {
      setBusy("");
    }
  };

  const changeProxy = async (group: MihomoProxyGroup, proxy: string) => {
    setBusy(`proxy-${group.name}`);
    try {
      const response = await fetch("/api/mihomo/proxies", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ group: group.name, proxy }),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "节点切换失败"));
      setGroups((old) =>
        old.map((item) =>
          item.name === group.name ? { ...item, now: proxy } : item,
        ),
      );
      setNotice(`“${group.name}”已切换到“${proxy}”`);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "节点切换失败");
    } finally {
      setBusy("");
    }
  };

  const testDelays = async () => {
    if (!groups.length) return;
    setBusy("delay");
    try {
      const results = await Promise.all(
        groups.map(async (group) => {
          const response = await fetch(
            `/api/mihomo/delay?group=${encodeURIComponent(group.name)}`,
            { cache: "no-store" },
          );
          if (!response.ok) return {};
          return ((await response.json()) as { delays?: Record<string, number> })
            .delays || {};
        }),
      );
      setDelays(Object.assign({}, ...results));
      setNotice("节点延迟测试已完成");
    } catch {
      setNotice("节点延迟测试失败");
    } finally {
      setBusy("");
    }
  };

  const openLogs = async () => {
    const response = await fetch("/api/logs?service=mihomo", {
      cache: "no-store",
    });
    if (!response.ok) {
      setNotice("暂时无法读取 Mihomo 日志");
      return;
    }
    const body = (await response.json()) as { log?: string };
    setLogText(body.log || "暂无日志");
    setShowLogs(true);
  };

  const changeSubscription = async (item: SubscriptionItem) => {
    setBusy(`subscription-${item.id}`);
    try {
      const response = await fetch("/api/mihomo/subscriptions", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          id: item.id,
          action: item.active ? "update" : "activate",
        }),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "订阅更新失败"));
      setSubscriptions((await response.json()) as SubscriptionList);
      setNotice(
        item.active
          ? "订阅已更新，配置已重新加载"
          : `已切换到订阅“${item.name}”`,
      );
      finishLater();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "订阅更新失败");
    } finally {
      setBusy("");
    }
  };

  const deleteSubscription = async (item: SubscriptionItem) => {
    if (
      !confirm(
        `确定删除订阅“${item.name}”？${item.active ? "将自动切换到列表中的其他订阅。" : ""}`,
      )
    )
      return;
    setBusy(`delete-${item.id}`);
    try {
      const response = await fetch(
        `/api/mihomo/subscriptions?id=${encodeURIComponent(item.id)}`,
        { method: "DELETE" },
      );
      if (!response.ok)
        throw new Error(await responseError(response, "删除订阅失败"));
      setSubscriptions((await response.json()) as SubscriptionList);
      setNotice("订阅已删除");
      finishLater();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "删除订阅失败");
    } finally {
      setBusy("");
    }
  };

  const addSubscription = async (event: FormEvent) => {
    event.preventDefault();
    if (!subscriptionUrl.trim()) {
      setNotice("请粘贴订阅地址");
      return;
    }
    setBusy("add");
    try {
      const response = await fetch("/api/mihomo/subscriptions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: subscriptionName.trim(),
          url: subscriptionUrl.trim(),
        }),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "添加订阅失败"));
      setSubscriptions((await response.json()) as SubscriptionList);
      setSubscriptionName("");
      setSubscriptionUrl("");
      setNotice("订阅已添加、校验并启用");
      finishLater();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "添加订阅失败");
    } finally {
      setBusy("");
    }
  };

  const openDashboard = () => {
    window.open("/mihomo-dashboard/", "_blank", "noopener");
  };

  const modes: {
    id: "rule" | "global" | "direct";
    label: string;
    detail: string;
  }[] = [
    { id: "rule", label: "规则", detail: "按规则分流" },
    { id: "global", label: "全局", detail: "全部走代理" },
    { id: "direct", label: "直连", detail: "绕过代理" },
  ];

  return (
    <div className="view mihomo-console">
      <article className="panel mihomo-hero">
        <div className="mihomo-hero-copy">
          <p className="eyebrow">代理核心控制中心</p>
          <h2>Mihomo</h2>
          <p>
            {status.online
              ? "集成核心状态、实时流量、代理组、监听端口和订阅管理。"
              : "核心当前已停止；配置和订阅仍然保留，可以安全查看或重新启动。"}
          </p>
        </div>
        <div className="mihomo-hero-state">
          <span
            className={status.online && running ? "state-good" : "state-muted"}
          >
            {status.online && running
              ? "核心在线"
              : running
                ? "正在启动"
                : "核心已停止"}
          </span>
          <strong>{status.version || "—"}</strong>
          <small>{status.proxyCount} 个代理与策略组</small>
        </div>
        <div className="mihomo-actions">
          <button
            type="button"
            disabled={Boolean(busy)}
            onClick={() => serviceAction(running ? "stop" : "start")}
          >
            {busy === `service-${running ? "stop" : "start"}`
              ? "处理中…"
              : running
                ? "停止核心"
                : "启动核心"}
          </button>
          <button
            type="button"
            disabled={Boolean(busy) || !running}
            onClick={() => serviceAction("restart")}
          >
            重启核心
          </button>
          <button
            type="button"
            disabled={!status.online || !running}
            onClick={openDashboard}
          >
            进入 Dashboard
          </button>
          <button type="button" onClick={openLogs}>
            核心日志
          </button>
        </div>
      </article>

      <section className="mihomo-live-grid">
        <article className="panel mihomo-live-card download">
          <span>实时下载</span>
          <strong>{rate(traffic.download)}</strong>
          <small>累计 {bytes(status.downloadTotal)}</small>
        </article>
        <article className="panel mihomo-live-card upload">
          <span>实时上传</span>
          <strong>{rate(traffic.upload)}</strong>
          <small>累计 {bytes(status.uploadTotal)}</small>
        </article>
        <article className="panel mihomo-live-card">
          <span>活动连接</span>
          <strong>{status.connections}</strong>
          <small>当前经过 Mihomo</small>
        </article>
        <article className="panel mihomo-live-card">
          <span>核心内存</span>
          <strong>{bytes(status.memory)}</strong>
          <small>
            {status.tunEnabled
              ? `TUN ${status.tunDevice || "op7tun"} · ${status.tunStack || "已启用"}`
              : "TUN 未启用"}
          </small>
        </article>
      </section>

      <article className="panel mihomo-section cpa-integration">
        <div className="section-heading small">
          <div>
            <p className="eyebrow">AI API 出口</p>
            <h2>CPA 集成</h2>
          </div>
          <span className={status.cpaLinked ? "state-good" : "state-muted"}>
            {status.cpaLinked ? "已连接 Mihomo" : "专线未连接"}
          </span>
        </div>
        <div className="cpa-integration-grid">
          <span>
            <small>Codex 专线</small>
            <strong>127.0.0.1:{status.cpaProxyPort || 17891}</strong>
          </span>
          <span>
            <small>日本出口</small>
            <strong>{status.cpaProxyNode || "等待核心"}</strong>
          </span>
          <span>
            <small>自动优选节点</small>
            <strong>{status.cpaAutoNode || "尚未测速"}</strong>
          </span>
          <span>
            <small>日本节点</small>
            <strong>{status.cpaJapaneseNodeCount} 个</strong>
          </span>
          <span>
            <small>用量统计</small>
            <strong>{status.cpaUsageEnabled ? "已开启" : "未开启"}</strong>
          </span>
          <span>
            <small>Codex OAuth 账号</small>
            <strong>{status.cpaAccountCount} 个</strong>
          </span>
          <button
            type="button"
            onClick={() =>
              window.open(
                `${location.protocol}//${location.hostname}:8317/management.html`,
                "_blank",
                "noopener",
              )
            }
          >
            打开 CPA 管理中心
          </button>
        </div>
        {!status.cpaAccountCount && (
          <p className="safe-note">
            网络出口已经就绪，但还需要在 CPA 管理中心完成一次 Codex OAuth
            授权，之后本地 Codex 请求才能由 CPA 转发并统计。
          </p>
        )}
        <p className="safe-note">
          CPA 的所有请求固定进入“{status.cpaProxyGroup || "AI"}”组；普通
          17890 端口跟随全局、规则或直连模式。CPA 专线不受模式和全屋代理开关影响。
        </p>
      </article>

      <article className="panel mihomo-section">
        <div className="section-heading small">
          <div>
            <p className="eyebrow">运行方式</p>
            <h2>代理模式</h2>
          </div>
          <span className="mode-current">
            当前：
            {modes.find((mode) => mode.id === status.mode)?.label || "未知"}
          </span>
        </div>
        <div className="mode-options">
          {modes.map((mode) => (
            <button
              key={mode.id}
              type="button"
              className={status.mode === mode.id ? "active" : ""}
              disabled={!status.online || Boolean(busy)}
              onClick={() => changeMode(mode.id)}
            >
              <strong>{mode.label}</strong>
              <small>{mode.detail}</small>
            </button>
          ))}
        </div>
      </article>

      <article className="panel mihomo-section">
        <div className="section-heading small">
          <div>
            <p className="eyebrow">策略组</p>
            <h2>代理组与节点</h2>
          </div>
          <span className="count-badge">{groups.length} 个组</span>
          <button
            className="section-action"
            type="button"
            disabled={!status.online || Boolean(busy) || !groups.length}
            onClick={testDelays}
          >
            {busy === "delay" ? "测速中…" : "节点测速"}
          </button>
        </div>
        <div className="proxy-group-grid">
          {groups.map((group) => (
            <label key={group.name}>
              <span>
                <strong>{group.name}</strong>
                <small>{group.type}</small>
              </span>
              <select
                value={group.now}
                disabled={Boolean(busy) || !group.all.length}
                onChange={(event) => changeProxy(group, event.target.value)}
              >
                {group.all.map((proxy) => (
                  <option key={proxy} value={proxy}>
                    {proxy}
                    {delays[proxy] ? ` · ${delays[proxy]} ms` : ""}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </div>
        {!groups.length && (
          <p className="empty-copy">
            核心未运行，或当前配置中没有可选择的代理组。
          </p>
        )}
      </article>

      <article className="panel mihomo-section">
        <div className="section-heading small">
          <div>
            <p className="eyebrow">本机监听</p>
            <h2>本地服务端口</h2>
          </div>
          <span className={settings.allowLan ? "warning-chip" : "state-good"}>
            仅手机本机
          </span>
        </div>
        <form className="mihomo-settings" onSubmit={saveSettings}>
          <div className="mihomo-port-grid">
            <label>
              Mixed 端口
              <input
                type="number"
                min="0"
                max="65535"
                value={settings.mixedPort}
                onChange={(event) =>
                  setSettings((old) => ({
                    ...old,
                    mixedPort: Number(event.target.value),
                  }))
                }
              />
              <small>HTTP 与 SOCKS 共用，当前主要入口</small>
            </label>
            <label>
              HTTP 端口
              <input
                type="number"
                min="0"
                max="65535"
                value={settings.httpPort}
                onChange={(event) =>
                  setSettings((old) => ({
                    ...old,
                    httpPort: Number(event.target.value),
                  }))
                }
              />
              <small>0 表示关闭独立 HTTP 端口</small>
            </label>
            <label>
              SOCKS 端口
              <input
                type="number"
                min="0"
                max="65535"
                value={settings.socksPort}
                onChange={(event) =>
                  setSettings((old) => ({
                    ...old,
                    socksPort: Number(event.target.value),
                  }))
                }
              />
              <small>0 表示关闭独立 SOCKS 端口</small>
            </label>
            <label>
              监听地址
              <input
                value={settings.bindAddress}
                disabled
              />
              <small>固定为 127.0.0.1，不向物理网卡暴露</small>
            </label>
          </div>
          <button type="submit" disabled={Boolean(busy)}>
            {busy === "settings" ? "正在校验并重载…" : "保存端口设置"}
          </button>
        </form>
        <p className="safe-note">
          `127.0.0.1:{settings.mixedPort}` 是跟随当前模式的本机调试入口；CPA 单独使用
          `127.0.0.1:17891` 并固定进入“AI”。实际分流由原生 op7tun 接管，
          同时覆盖 LAN 与 Android 本机应用；不依赖 Redirect/TProxy 端口，也不会向 LAN
          暴露代理监听端口。
        </p>
      </article>

      <article className="panel mihomo-section">
        <div className="section-heading small">
          <div>
            <p className="eyebrow">配置来源</p>
            <h2>订阅管理</h2>
          </div>
          <span className="count-badge">
            {subscriptions.items.length} 个订阅
          </span>
        </div>
        <div className="subscription-list">
          {subscriptions.items.map((item) => (
            <section
              className={`subscription-card ${item.active ? "active" : ""}`}
              key={item.id}
            >
              <div className="subscription-card-copy">
                <div>
                  <strong>{item.name}</strong>
                  {item.active && (
                    <span className="active-subscription">当前使用</span>
                  )}
                </div>
                <span>{item.source}</span>
                <small>
                  最近更新：
                  {item.updatedAt
                    ? new Date(item.updatedAt).toLocaleString("zh-CN")
                    : "尚未更新"}
                </small>
              </div>
              <div className="subscription-card-actions">
                <button
                  type="button"
                  disabled={Boolean(busy)}
                  onClick={() => changeSubscription(item)}
                >
                  {busy === `subscription-${item.id}`
                    ? "更新中…"
                    : item.active
                      ? "立即更新"
                      : "启用并更新"}
                </button>
                <button
                  className="danger-quiet"
                  type="button"
                  disabled={Boolean(busy)}
                  onClick={() => deleteSubscription(item)}
                >
                  {busy === `delete-${item.id}` ? "删除中…" : "删除"}
                </button>
              </div>
            </section>
          ))}
        </div>
        {subscriptions.items.length === 0 && (
          <div className="subscription-empty">
            <strong>还没有订阅</strong>
            <span>
              在下方粘贴订阅地址，系统会先下载和校验，再替换当前配置。
            </span>
          </div>
        )}
        <form className="subscription-add" onSubmit={addSubscription}>
          <div>
            <label>
              订阅名称（可选）
              <input
                value={subscriptionName}
                onChange={(event) => setSubscriptionName(event.target.value)}
                placeholder="例如：主力机场"
                maxLength={128}
                autoComplete="off"
              />
            </label>
            <label>
              订阅 URL
              <input
                type="url"
                value={subscriptionUrl}
                onChange={(event) => setSubscriptionUrl(event.target.value)}
                placeholder="https://example.com/subscribe?..."
                autoComplete="off"
              />
            </label>
          </div>
          <button type="submit" disabled={Boolean(busy)}>
            {busy === "add" ? "正在下载并校验…" : "添加并启用订阅"}
          </button>
        </form>
        <p className="safe-note">
          当前订阅每 {subscriptions.autoUpdateHours} 小时自动更新。完整 URL
          只保存在手机本机，管理页面仅显示已隐藏敏感路径的地址。
        </p>
      </article>
      {showLogs && (
        <div className="modal-backdrop" onClick={() => setShowLogs(false)}>
          <section
            className="log-modal"
            onClick={(event) => event.stopPropagation()}
          >
            <header>
              <strong>Mihomo 核心日志</strong>
              <div>
                <button type="button" onClick={openLogs}>
                  刷新
                </button>
                <button type="button" onClick={() => setShowLogs(false)}>
                  关闭
                </button>
              </div>
            </header>
            <pre>{logText}</pre>
          </section>
        </div>
      )}
    </div>
  );
}

function Aria2Monitor({
  data,
  history,
}: {
  data: Snapshot;
  history: HistoryPoint[];
}) {
  const aria = data.aria2;
  const eta = (task: Snapshot["aria2"]["tasks"][number]) => {
    const remain = Math.max(0, task.TotalLength - task.CompletedLength);
    return task.DownloadSpeed > 0
      ? duration(Math.round(remain / task.DownloadSpeed))
      : "—";
  };
  return (
    <article className="panel aria-monitor">
      <div className="aria-summary">
        <div>
          <p className="eyebrow">下载引擎 · 实时 RPC</p>
          <h2>Aria2 下载监控</h2>
          <span className={aria.online ? "state-good" : "state-muted"}>
            {aria.online ? "核心在线" : "RPC 不可达"}
          </span>
        </div>
        <div className="aria-kpis">
          <span>
            <small>总下载</small>
            <strong>{rate(aria.downloadSpeed)}</strong>
          </span>
          <span>
            <small>活动</small>
            <strong>{aria.numActive}</strong>
          </span>
          <span>
            <small>等待 / 暂停</small>
            <strong>
              {aria.numWaiting} / {aria.numPaused}
            </strong>
          </span>
          <span>
            <small>已结束</small>
            <strong>{aria.numStopped}</strong>
          </span>
        </div>
      </div>
      <TimeSeriesChart
        history={history}
        series={[
          { label: "Aria2 下载", color: "#30c7d8", value: (p) => p.ariaDown },
          { label: "Aria2 上传", color: "#52d395", value: (p) => p.ariaUp },
        ]}
        format={rate}
      />
      <div className="aria-task-list">
        {aria.tasks.map((task) => {
          const progress = task.TotalLength
            ? Math.min(100, (task.CompletedLength / task.TotalLength) * 100)
            : 0;
          return (
            <section key={task.GID}>
              <div>
                <strong title={task.Name}>{task.Name}</strong>
                <span>
                  {progress.toFixed(1)}% · {rate(task.DownloadSpeed)} ·{" "}
                  {task.Connections} 连接 · ETA {eta(task)}
                </span>
              </div>
              <div className="aria-progress">
                <i style={{ width: `${progress}%` }} />
              </div>
              <small>
                {bytes(task.CompletedLength)} / {bytes(task.TotalLength)}
              </small>
            </section>
          );
        })}
      </div>
      {!aria.tasks.length && (
        <p className="empty-copy">当前没有活动下载任务。</p>
      )}
      <p className="safe-note">
        当前下载源对批量旧签名返回 HTTP 429/500。剩余任务已保留为暂停队列；请从
        OpenList 刷新链接后分批启动，建议同时保持 2–4 个活动任务。
      </p>
    </article>
  );
}

function TailscaleService({
  setNotice,
}: {
  setNotice: (value: string) => void;
}) {
  const [state, setState] = useState<TailscaleState>({
    installed: true,
    enabled: false,
    supervisor: false,
    daemon: false,
    online: false,
    loginRequired: false,
    loginPending: false,
    backend: "读取中",
    hostname: "",
    ip: "",
    advertiseRoutes: "",
    primaryRoutes: "",
    routeStatus: "off",
  });
  const [busy, setBusy] = useState("");

  const load = useCallback(async () => {
    const response = await fetch("/api/tailscale", { cache: "no-store" });
    if (!response.ok) return;
    setState((await response.json()) as TailscaleState);
  }, []);

  useEffect(() => {
    let stopped = false;
    let timer = 0;
    const poll = async () => {
      try {
        await load();
      } finally {
        if (!stopped) timer = window.setTimeout(poll, 5000);
      }
    };
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [load]);

  const action = async (
    name: "start" | "stop" | "restart" | "login" | "reauth",
  ) => {
    if (
      name === "stop" &&
      !confirm(
        "确定停止 Tailscale？远程访问和 192.168.2.0/24 子网路由会立即中断，但登录状态会保留。",
      )
    )
      return;
    if (
      name === "reauth" &&
      !confirm("确定重新授权 Tailscale？授权完成前远程连接可能暂时中断。")
    )
      return;

    const loginWindow =
      name === "login" || name === "reauth"
        ? window.open("about:blank", "_blank")
        : null;
    setBusy(name);
    try {
      const response = await fetch("/api/tailscale", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: name }),
      });
      const body = (await response.json().catch(() => ({}))) as
        | (TailscaleState & { error?: string })
        | { error?: string };
      if (!response.ok) throw new Error(body.error || "Tailscale 操作失败");
      const next = body as TailscaleState;
      setState(next);
      if (next.authUrl) {
        if (loginWindow) loginWindow.location.href = next.authUrl;
        else window.open(next.authUrl, "_blank", "noopener");
        setNotice("已打开 Tailscale 官方授权页面，完成后状态会自动刷新");
      } else {
        loginWindow?.close();
        setNotice(
          name === "start"
            ? "Tailscale 已启动"
            : name === "stop"
              ? "Tailscale 已停止，设备登录信息已保留"
              : name === "restart"
                ? "Tailscale 已重启"
                : "Tailscale 已处于授权状态",
        );
      }
      window.setTimeout(() => void load(), 1200);
    } catch (error) {
      loginWindow?.close();
      setNotice(error instanceof Error ? error.message : "Tailscale 操作失败");
    } finally {
      setBusy("");
    }
  };

  const routeText =
    state.routeStatus === "active"
      ? "子网路由已生效"
      : state.routeStatus === "configured"
        ? "已发布，等待控制台批准"
        : "未发布子网路由";
  return (
    <article className="panel tailscale-service">
      <header className="tailscale-service-head">
        <div className="tailscale-brand">
          <span>TS</span>
          <div>
            <p className="eyebrow">ROOT 守护 · 远程管理基础设施</p>
            <h2>Tailscale</h2>
          </div>
        </div>
        <span className={state.online ? "state-good" : "state-muted"}>
          {state.online
            ? "在线"
            : state.loginRequired
              ? "需要登录"
              : state.daemon
                ? state.backend || "正在连接"
                : "已停止"}
        </span>
      </header>
      <div className="tailscale-facts">
        <span>
          <small>设备</small>
          <strong>{state.hostname || "oneplus7-router"}</strong>
        </span>
        <span>
          <small>Tailscale IP</small>
          <strong className="mono">{state.ip || "尚未分配"}</strong>
        </span>
        <span>
          <small>子网路由</small>
          <strong>{routeText}</strong>
          <em>{state.advertiseRoutes || "192.168.2.0/24"}</em>
        </span>
        <span>
          <small>守护状态</small>
          <strong>
            {state.enabled ? "开机自启" : "自动启动已关闭"} ·{" "}
            {state.supervisor ? "监督器正常" : "监督器停止"}
          </strong>
        </span>
      </div>
      <div className="tailscale-actions">
        {!state.daemon ? (
          <button
            type="button"
            disabled={Boolean(busy)}
            onClick={() => action("start")}
          >
            {busy === "start" ? "正在启动…" : "启动"}
          </button>
        ) : (
          <button
            type="button"
            disabled={Boolean(busy)}
            onClick={() => action("restart")}
          >
            {busy === "restart" ? "正在重启…" : "重启"}
          </button>
        )}
        <button
          type="button"
          disabled={Boolean(busy) || !state.daemon}
          onClick={() => action(state.online ? "reauth" : "login")}
        >
          {busy === "login" || busy === "reauth"
            ? "正在生成授权链接…"
            : state.online
              ? "重新授权"
              : "登录 / 授权"}
        </button>
        {state.authUrl && (
          <button
            type="button"
            onClick={() => window.open(state.authUrl, "_blank", "noopener")}
          >
            打开授权页
          </button>
        )}
        <button
          className="danger-quiet"
          type="button"
          disabled={Boolean(busy) || !state.daemon}
          onClick={() => action("stop")}
        >
          {busy === "stop" ? "正在停止…" : "停止"}
        </button>
      </div>
      <p className="safe-note">
        停止只关闭当前守护进程，不会注销设备；再次启动会用现有节点密钥自动上线。
        Tailscale 流量和 100.64.0.0/10 永远绕过全屋代理，防止代理切换中断远程维护。
      </p>
    </article>
  );
}

function Services({
  data,
  history,
  refresh,
  setNotice,
  onLog,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  refresh: (quiet?: boolean) => Promise<void>;
  setNotice: (value: string) => void;
  onLog: (name: string) => void;
}) {
  const act = async (name: string, action: string) => {
    if (action === "stop" && !confirm(`确定停止 ${name}？`)) return;
    const r = await fetch("/api/services/action", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ Name: name, Action: action }),
    });
    const b = (await r.json()) as { error?: string };
    setNotice(r.ok ? `${name} 操作完成` : b.error || "操作失败");
    setTimeout(() => refresh(true), 800);
  };
  const open = (service: Snapshot["services"][number]) => {
    if (!service.webUrl) {
      setNotice("该服务没有网页管理入口");
      return;
    }
    const path = service.Name === "cpa" ? "/management.html" : "";
    window.open(
      `${location.protocol}//${location.hostname}${service.webUrl}${path}`,
      "_blank",
      "noopener",
    );
  };
  return (
    <div className="view">
      <div className="section-heading">
        <div>
          <p className="eyebrow">Runit 托管</p>
          <h2>应用与系统服务</h2>
        </div>
        <span className="live-pill">
          <i /> 自动守护
        </span>
      </div>
      <TailscaleService setNotice={setNotice} />
      <Aria2Monitor data={data} history={history} />
      <div className="service-cards">
        {data.services.map((service, index) => (
          <article className="panel service-card" key={service.Name}>
            <div className="service-main">
              <span className={`service-icon s${index % 4}`}>
                {service.Label.slice(0, 2)}
              </span>
              <div>
                <strong>{service.Label}</strong>
                <small>
                  {service.Port ? `TCP ${service.Port}` : "后台服务"} ·{" "}
                  {service.PID ? `PID ${service.PID}` : "无进程"}
                </small>
              </div>
              <span
                className={
                  service.State === "运行中" ? "state-good" : "state-muted"
                }
              >
                {service.State}
              </span>
            </div>
            <div className="service-meta">
              <span>运行时长 {duration(service.UptimeSec)}</span>
              {[
                "openlist",
                "caddy",
                "qbittorrent",
                "cpa",
                "cpa-usage-keeper",
              ].includes(service.Name) && (
                <button type="button" onClick={() => open(service)}>
                  打开
                </button>
              )}
              <button type="button" onClick={() => onLog(service.Name)}>
                日志
              </button>
              {service.Name !== "router-admin" && (
                <>
                  <button
                    type="button"
                    onClick={() => act(service.Name, "restart")}
                  >
                    重启
                  </button>
                  <button
                    className="danger-quiet"
                    type="button"
                    onClick={() =>
                      act(
                        service.Name,
                        service.State === "运行中" ? "stop" : "start",
                      )
                    }
                  >
                    {service.State === "运行中" ? "停止" : "启动"}
                  </button>
                </>
              )}
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}

function Storage({ data }: { data: Snapshot }) {
  return (
    <div className="view">
      <div className="section-heading">
        <div>
          <p className="eyebrow">正式部署 · 磁盘与 USB 总线</p>
          <h2>存储设备</h2>
        </div>
        <span className="usb-badge">实时挂载状态</span>
      </div>
      <div className="storage-grid">
        {data.storage.map((disk) => (
          <article className="panel disk-card" key={disk.Mount}>
            <header>
              <div>
                <span className="disk-glyph">
                  {disk.External ? "HDD" : "UFS"}
                </span>
                <div>
                  <strong>{disk.Model || disk.Name}</strong>
                  <small>
                    {disk.Filesystem} · {disk.Mount}
                  </small>
                </div>
              </div>
              <span>{disk.External ? "外置硬盘" : "内部"}</span>
            </header>
            <div className="disk-numbers">
              <strong>{gb(disk.Free)}</strong>
              <span>可用，共 {gb(disk.Total)}</span>
            </div>
            <div className="storage-bar">
              <i style={{ width: `${disk.UsedPercent}%` }} />
            </div>
            <footer>
              <span>已用 {gb(disk.Used)}</span>
              <span>{disk.UsedPercent.toFixed(1)}%</span>
            </footer>
          </article>
        ))}
      </div>
      <div className="section-heading small">
        <div>
          <p className="eyebrow">USB 拓扑</p>
          <h2>已连接设备</h2>
        </div>
      </div>
      <article className="panel">
        <div className="usb-list">
          {data.usb.map((usb, i) => {
            const limitedNIC =
              usb.VendorID === "38a2" && usb.ProductID === "0153";
            return (
              <div
                className="usb-row"
                key={`${usb.VendorID}:${usb.ProductID}:${i}`}
              >
                <span className="usb-port">
                  {usb.Speed === "5000" ? "SS" : "HS"}
                </span>
                <div>
                  <strong>
                    {usb.Product || "USB Device"}
                    {limitedNIC ? " · 性能受限" : ""}
                  </strong>
                  <small>
                    {usb.Manufacturer || "未知厂商"} · {usb.VendorID}:
                    {usb.ProductID}
                  </small>
                </div>
                <span>
                  {limitedNIC
                    ? "CDC ECM"
                    : usb.Speed
                      ? `${Number(usb.Speed) / 1000} Gbps`
                      : "未知速度"}
                </span>
              </div>
            );
          })}
        </div>
      </article>
      <p className="safe-note">
        当前外置 Toshiba 机械硬盘以 NTFS 读写挂载，OpenList 与下载服务使用
        /mnt/usb_disk。TIH 38a2:0153 网卡虽然枚举为 USB 5Gbps，但使用通用 CDC
        ECM 驱动，当前实测存在严重的单向吞吐限制。
      </p>
    </div>
  );
}

function BatteryGuardSettings({
  guardLow,
  guardHigh,
  onRefresh,
}: {
  guardLow?: number;
  guardHigh?: number;
  onRefresh?: () => void | Promise<void>;
}) {
  const [userLow, setUserLow] = useState<string | null>(null);
  const [userHigh, setUserHigh] = useState<string | null>(null);
  const [status, setStatus] = useState<
    "idle" | "saving" | "waiting" | "applied" | "error"
  >("idle");
  const [statusMessage, setStatusMessage] = useState<string>("");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const disabled = status === "saving" || status === "waiting";

  const lowInput =
    userLow ?? (guardLow != null && guardLow > 0 ? String(guardLow) : "40");
  const highInput =
    userHigh ?? (guardHigh != null && guardHigh > 0 ? String(guardHigh) : "60");

  const applyPreset = (low: number, high: number) => {
    if (disabled) return;
    setUserLow(String(low));
    setUserHigh(String(high));
    if (errorMessage) {
      setErrorMessage(null);
      setStatus("idle");
    }
  };

  const handleApply = async () => {
    const trimmedLow = lowInput.trim();
    const trimmedHigh = highInput.trim();

    if (!trimmedLow || !trimmedHigh) {
      setStatus("error");
      setErrorMessage("请输入起充和停充阈值");
      return;
    }

    const low = Number(trimmedLow);
    const high = Number(trimmedHigh);

    if (
      !Number.isFinite(low) ||
      !Number.isFinite(high) ||
      !Number.isInteger(low) ||
      !Number.isInteger(high)
    ) {
      setStatus("error");
      setErrorMessage("阈值必须为有效整数");
      return;
    }

    if (low < 20 || high > 95 || low >= high) {
      setStatus("error");
      setErrorMessage("阈值需满足 20% ≤ 起充 < 停充 ≤ 95%");
      return;
    }

    setErrorMessage(null);
    setStatus("saving");
    setStatusMessage("正在保存配置...");

    try {
      const res = await fetch("/api/power/battery-guard", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ low, high }),
      });
      const result = (await res.json()) as {
        error?: string;
        ok?: boolean;
        low?: number;
        high?: number;
      };

      if (!res.ok || !result.ok) {
        setStatus("error");
        setErrorMessage(result.error ?? "保存失败，请重试");
        return;
      }

      setStatus("waiting");
      setStatusMessage("配置已保存，等待后台控制器生效（约需10秒）...");

      const maxAttempts = 14;
      const pollInterval = 1500;
      let applied = false;

      for (let attempt = 0; attempt < maxAttempts; attempt++) {
        await new Promise((resolve) => setTimeout(resolve, pollInterval));
        try {
          const getRes = await fetch("/api/power/battery-guard", {
            cache: "no-store",
          });
          if (getRes.ok) {
            const getStatus = (await getRes.json()) as {
              healthy?: boolean;
              enabled?: boolean;
              paused?: boolean;
              low?: number;
              high?: number;
            };
            if (
              getStatus.healthy &&
              getStatus.low === low &&
              getStatus.high === high
            ) {
              applied = true;
              break;
            }
          }
        } catch {
          // ignore network glitch during polling
        }
      }

      if (applied) {
        setStatus("waiting");
        setStatusMessage("配置已生效，正在同步最新系统数据...");
        try {
          if (onRefresh) {
            await Promise.resolve(onRefresh());
          }
          setUserLow(null);
          setUserHigh(null);
          setStatus("applied");
          setStatusMessage(`✓ 阈值已成功生效（${low}%–${high}%）`);
          setTimeout(() => {
            setStatus((cur) => (cur === "applied" ? "idle" : cur));
            setStatusMessage("");
          }, 4000);
        } catch (refreshErr) {
          // Backend apply succeeded; retain local overrides to prevent reversion to stale snapshot
          setStatus("applied");
          setStatusMessage(`✓ 阈值已成功生效（${low}%–${high}%）`);
          setErrorMessage(
            "阈值已生效，但系统数据同步异常: " +
              (refreshErr instanceof Error ? refreshErr.message : "网络异常")
          );
        }
      } else {
        setStatus("error");
        setErrorMessage(
          "配置已保存，但控制器未在预期间内确认生效，请检查电池保护服务"
        );
      }
    } catch (err) {
      setStatus("error");
      setErrorMessage(
        "请求失败: " + (err instanceof Error ? err.message : "网络异常")
      );
    }
  };

  const parsedLow = Number.parseInt(lowInput, 10);
  const parsedHigh = Number.parseInt(highInput, 10);
  const displayLow = Number.isInteger(parsedLow) ? parsedLow : (guardLow ?? 40);
  const displayHigh = Number.isInteger(parsedHigh)
    ? parsedHigh
    : (guardHigh ?? 60);

  return (
    <div className="battery-guard-section">
      <div className="battery-guard-header">
        <span className="battery-guard-title">自定义电池保护阈值</span>
      </div>

      <div className="battery-guard-flow" aria-label="充电保护迟滞工作流程">
        <div className="battery-guard-flow-step">
          <span className="battery-guard-flow-tag start">起充点</span>
          <strong>≤ {displayLow}%</strong>
          <small>开始充电</small>
        </div>
        <div className="battery-guard-flow-arrow">
          <span className="battery-guard-flow-line" />
          <span className="battery-guard-flow-band">
            {displayLow}% ~ {displayHigh}% 维持当前状态
          </span>
          <span className="battery-guard-flow-line" />
        </div>
        <div className="battery-guard-flow-step">
          <span className="battery-guard-flow-tag stop">停充点</span>
          <strong>≥ {displayHigh}%</strong>
          <small>暂停充电 (旁路供电)</small>
        </div>
      </div>

      <div className="battery-guard-form">
        <div className="battery-guard-inputs">
          <label className="battery-guard-field">
            <span>起充:</span>
            <div className="battery-guard-input-wrap">
              <input
                type="number"
                min={20}
                max={94}
                value={lowInput}
                disabled={disabled}
                onChange={(e) => {
                  setUserLow(e.target.value);
                  if (errorMessage) {
                    setErrorMessage(null);
                    setStatus("idle");
                  }
                }}
                className="battery-guard-input"
              />
              <span className="battery-guard-unit">%</span>
            </div>
          </label>
          <label className="battery-guard-field">
            <span>停充:</span>
            <div className="battery-guard-input-wrap">
              <input
                type="number"
                min={21}
                max={95}
                value={highInput}
                disabled={disabled}
                onChange={(e) => {
                  setUserHigh(e.target.value);
                  if (errorMessage) {
                    setErrorMessage(null);
                    setStatus("idle");
                  }
                }}
                className="battery-guard-input"
              />
              <span className="battery-guard-unit">%</span>
            </div>
          </label>
        </div>

        <button
          type="button"
          className="battery-guard-btn-apply"
          disabled={disabled}
          onClick={handleApply}
        >
          {status === "saving"
            ? "正在保存..."
            : status === "waiting"
            ? "等待生效..."
            : "应用阈值"}
        </button>
      </div>

      <div className="battery-guard-presets">
        <span className="battery-guard-preset-label">预设:</span>
        {[
          { label: "40%–60% · 防胀气", low: 40, high: 60 },
          { label: "50%–70% · 平衡", low: 50, high: 70 },
          { label: "60%–75% · 续航", low: 60, high: 75 },
        ].map((preset) => (
          <button
            key={preset.label}
            type="button"
            className="battery-guard-preset-btn"
            disabled={disabled}
            onClick={() => applyPreset(preset.low, preset.high)}
          >
            {preset.label}
          </button>
        ))}
      </div>

      {errorMessage && (
        <div className="battery-guard-alert" role="alert">
          <span>⚠️ {errorMessage}</span>
        </div>
      )}

      {status === "waiting" && (
        <div className="battery-guard-waiting" role="status">
          <span className="battery-guard-spinner" />
          <span>{statusMessage}</span>
        </div>
      )}

      {status === "applied" && (
        <div className="battery-guard-success" role="status">
          <span>{statusMessage}</span>
        </div>
      )}
    </div>
  );
}

function System({
  data,
  history,
  onRefresh,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  onRefresh?: () => void | Promise<void>;
}) {
  const memoryUsedPercent = data.memory.Total
    ? (data.memory.Used / data.memory.Total) * 100
    : 0;
  const swapUsedPercent = data.memory.SwapTotal
    ? (data.memory.SwapUsed / data.memory.SwapTotal) * 100
    : 0;
  const batteryDirection =
    data.power.Current > 0.02
      ? "放电"
      : data.power.Current < -0.02
        ? "充电"
        : "基本平衡";
  const isBatteryThermal = (sensor: Snapshot["thermals"][number]) =>
    `${sensor.Type} ${sensor.Name}`.toLowerCase().includes("battery");
  const batteryThermal = data.thermals.find(isBatteryThermal);
  const batteryTemperature = batteryThermal?.Temp ?? data.power.BatteryTemp;
  const secondaryThermals = data.thermals.filter(
    (sensor) => !isBatteryThermal(sensor),
  );
  const hottestSecondary = secondaryThermals.reduce<
    Snapshot["thermals"][number] | undefined
  >(
    (hottest, sensor) =>
      !hottest || sensor.Temp > hottest.Temp ? sensor : hottest,
    undefined,
  );
  return (
    <div className="view">
      <div className="system-grid">
        <article className="panel power-card">
          <div className="panel-title">
            <div>
              <p className="eyebrow">电池与 PD</p>
              <h2>供电状态</h2>
            </div>
            <span
              className={data.power.usbOnline ? "state-good" : "warning-chip"}
            >
              {data.power.usbOnline
                ? `${data.power.usbType} 在线`
                : data.power.PowerRole === "source"
                  ? "OTG 供电源 · 未回充"
                  : "未接电源"}
            </span>
          </div>
          <div className="power-primary">
            <strong>{data.power.batteryPercent}%</strong>
            <span>{data.power.Status}</span>
          </div>
          {!data.power.usbOnline && data.power.PowerRole === "source" && (
            <p className="power-warning">
              Type-C 当前锁定为 source/host：手机正在给 Hub 供电，PD
              回充没有进入手机。
            </p>
          )}
          <dl>
            <div>
              <dt>电池温度</dt>
              <dd>{data.power.BatteryTemp.toFixed(1)}°C</dd>
            </div>
            <div>
              <dt>电池电压 / 瞬时电流</dt>
              <dd>
                {data.power.Voltage.toFixed(3)} V /{" "}
                {Math.abs(data.power.Current).toFixed(3)} A · {batteryDirection}
              </dd>
            </div>
            <div>
              <dt>BMS 平均 / 电量计估算</dt>
              <dd>
                {Math.abs(data.power.AverageCurrent).toFixed(3)} A /{" "}
                {Math.abs(data.power.CoulombCurrent).toFixed(3)} A
              </dd>
            </div>
            <div>
              <dt>剩余电荷 / 预计续航</dt>
              <dd>
                {data.power.ChargeCounterMAh.toFixed(1)} mAh /{" "}
                {data.power.timeToEmptySec > 0
                  ? duration(data.power.timeToEmptySec)
                  : "计算中"}
              </dd>
            </div>
            <div>
              <dt>手机口输入（估算）</dt>
              <dd>
                {data.power.USBInputWatts.toFixed(2)} W
                {data.power.USBInputFiltered ? " · 已滤波" : ""}
              </dd>
            </div>
            <div>
              <dt>输入电压 / 电流</dt>
              <dd>
                {data.power.USBVoltage.toFixed(3)} V /{" "}
                {data.power.USBInputCurrent.toFixed(3)} A
              </dd>
            </div>
            <div>
              <dt>电池净功率</dt>
              <dd>
                {Math.abs(data.power.BatteryWatts).toFixed(2)} W ·{" "}
                {data.power.BatteryWatts < -0.1
                  ? "充电"
                  : data.power.BatteryWatts > 0.1
                    ? "放电"
                    : "基本平衡"}
              </dd>
            </div>
            <div>
              <dt>PD 合同上限</dt>
              <dd>
                {data.power.PDVoltage.toFixed(1)} V ×{" "}
                {data.power.PDCurrentMax.toFixed(1)} A ={" "}
                {data.power.PDWattsMax.toFixed(1)} W
              </dd>
            </div>
            <div>
              <dt>Type-C 角色</dt>
              <dd>
                {data.power.PowerRole || "未知"} / {data.power.TypeCMode || "未知"}
                {data.power.OTGActive ? " · OTG 开启" : ""}
              </dd>
            </div>
            <div>
              <dt>内核充电开关</dt>
              <dd>
                {data.power.ChargingEnabled ? "允许充电" : "禁止充电"} ·{" "}
                {data.power.InputSuspended ? "输入挂起" : "输入未挂起"}
              </dd>
            </div>
            <div>
              <dt>充电保护</dt>
              <dd>
                {data.power.chargeGuard && (data.power.guardLow ?? 0) > 0 && (data.power.guardHigh ?? 0) > 0
                  ? `已启用（${data.power.guardLow}%–${data.power.guardHigh}%）· ${
                      data.power.chargePaused
                        ? "当前暂停充电"
                        : "当前允许充电"
                    }`
                  : data.power.chargeGuard
                  ? `已启用 · ${
                      data.power.chargePaused
                        ? "当前暂停充电"
                        : "当前允许充电"
                    }`
                  : "未启用"}
              </dd>
            </div>
          </dl>
          <BatteryGuardSettings
            guardLow={data.power.guardLow}
            guardHigh={data.power.guardHigh}
            onRefresh={onRefresh}
          />
        </article>
        <article className="panel device-card">
          <div className="panel-title">
            <div>
              <p className="eyebrow">设备</p>
              <h2>{data.device.Model}</h2>
            </div>
            <span className="brand-mark small">1+</span>
          </div>
          <dl>
            <div>
              <dt>Android</dt>
              <dd>{data.device.Android}</dd>
            </div>
            <div>
              <dt>内核</dt>
              <dd>{data.device.Kernel}</dd>
            </div>
            <div>
              <dt>系统运行</dt>
              <dd>{duration(data.device.uptimeSec)}</dd>
            </div>
            <div>
              <dt>管理后台</dt>
              <dd>{duration(data.device.adminUptimeSec)}</dd>
            </div>
            <div>
              <dt>负载（1/5/15 分）</dt>
              <dd>
                {data.cpu.Load1.toFixed(2)} / {data.cpu.Load5.toFixed(2)} /{" "}
                {data.cpu.Load15.toFixed(2)}
              </dd>
            </div>
          </dl>
        </article>
      </div>
      <div className="system-grid power-history-grid">
        <article className="panel history-panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">最近 10 分钟</p>
              <h2>电池电流趋势（正值为放电）</h2>
            </div>
          </div>
          <TimeSeriesChart
            history={history}
            series={[
              { label: "瞬时 mA", color: "#ffad66", value: (p) => p.batteryCurrent },
              { label: "BMS 平均 mA", color: "#a897ff", value: (p) => p.batteryAverageCurrent },
              { label: "电量计估算 mA", color: "#30c7d8", value: (p) => p.batteryCoulombCurrent },
            ]}
            format={(value) => `${value.toFixed(0)} mA`}
          />
        </article>
        <article className="panel history-panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">最近 10 分钟</p>
              <h2>电池与 USB 输入电压</h2>
            </div>
          </div>
          <TimeSeriesChart
            history={history}
            series={[
              { label: "电池 V", color: "#52d395", value: (p) => p.batteryVoltage },
              { label: "USB 输入 V", color: "#30c7d8", value: (p) => p.usbVoltage },
            ]}
            format={(value) => `${value.toFixed(3)} V`}
            zeroBaseline={false}
          />
        </article>
      </div>
      <article className="panel history-panel system-history">
        <div className="panel-title">
          <div>
            <p className="eyebrow">最近 10 分钟</p>
            <h2>CPU、内存与温度趋势</h2>
          </div>
        </div>
        <TimeSeriesChart
          history={history}
          series={[
            { label: "CPU %", color: "#30c7d8", value: (p) => p.cpu },
            { label: "内存 %", color: "#a897ff", value: (p) => p.memory },
            { label: "温度 °C", color: "#ffad66", value: (p) => p.temperature },
          ]}
          format={(value) => value.toFixed(1)}
          domain={[0, 100]}
        />
      </article>
      <div className="section-heading small">
        <div>
          <p className="eyebrow">处理器与内存</p>
          <h2>实时性能</h2>
        </div>
      </div>
      <div className="system-grid">
        <article className="panel cpu-panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">CPU</p>
              <h2>
                {data.cpu.usage.toFixed(1)}% · 平均 {data.cpu.avgMHz} MHz
              </h2>
            </div>
            <strong>{data.cpu.Online} 核在线</strong>
          </div>
          <div className="cpu-core-grid">
            {data.cpu.cores.map((core) => (
              <div className="cpu-core-row" key={core.id}>
                <span>CPU {core.id}</span>
                <div>
                  <b style={{ width: `${Math.min(100, core.usage)}%` }} />
                </div>
                <strong>
                  {core.online ? `${core.currentMHz} MHz` : "离线"}
                </strong>
                <small>
                  {core.usage.toFixed(0)}% · 最高 {core.maxMHz} ·{" "}
                  {core.governor || "未知调速器"}
                </small>
              </div>
            ))}
          </div>
        </article>
        <article className="panel memory-panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">RAM</p>
              <h2>
                {gb(data.memory.Used)} / {gb(data.memory.Total)}
              </h2>
            </div>
            <strong>{memoryUsedPercent.toFixed(1)}%</strong>
          </div>
          <div className="memory-bar">
            <i style={{ width: `${Math.min(100, memoryUsedPercent)}%` }} />
          </div>
          <dl>
            <div>
              <dt>可用</dt>
              <dd>{gb(data.memory.Available)}</dd>
            </div>
            <div>
              <dt>文件缓存</dt>
              <dd>{gb(data.memory.Cached)}</dd>
            </div>
            <div>
              <dt>可回收内核缓存</dt>
              <dd>{gb(data.memory.Reclaimable)}</dd>
            </div>
            <div>
              <dt>缓冲区</dt>
              <dd>{gb(data.memory.Buffers)}</dd>
            </div>
            <div>
              <dt>Swap</dt>
              <dd>
                {gb(data.memory.SwapUsed)} / {gb(data.memory.SwapTotal)}
              </dd>
            </div>
            <div>
              <dt>Swap 使用率</dt>
              <dd>{swapUsedPercent.toFixed(1)}%</dd>
            </div>
          </dl>
          <p className="memory-note">
            Linux/Android 会主动把空闲 RAM
            用作缓存；判断内存压力应看“可用”，不是只看“已用”。
          </p>
        </article>
      </div>
      <div className="section-heading small">
        <div>
          <p className="eyebrow">热状态</p>
          <h2>温度</h2>
        </div>
        <span
          className={batteryTemperature < 45 ? "state-good" : "warning-chip"}
        >
          电池 {batteryTemperature.toFixed(1)}°C
        </span>
      </div>
      <article className="panel thermal-summary">
        <div className="battery-thermal">
          <div>
            <small>电池温度</small>
            <strong>{batteryTemperature.toFixed(1)}°C</strong>
          </div>
          <span>{batteryThermal?.Name || "battery"}</span>
        </div>
        {secondaryThermals.length > 0 && (
          <details className="thermal-secondary">
            <summary>
              <span>其他传感器 · {secondaryThermals.length}</span>
              <strong>
                最高 {hottestSecondary?.Temp.toFixed(1)}°C
              </strong>
            </summary>
            <div className="thermal-compact-list">
              {secondaryThermals.map((sensor) => (
                <div key={sensor.Name}>
                  <span>{sensor.Type}</span>
                  <strong className={sensor.Temp > 80 ? "hot" : ""}>
                    {sensor.Temp.toFixed(1)}°C
                  </strong>
                </div>
              ))}
            </div>
          </details>
        )}
      </article>
    </div>
  );
}
