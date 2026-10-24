"use client";

import {
  FormEvent,
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { BrowserScreenControl } from "./web-screen";
import {
  isTab,
  ModuleContextBar,
  navItems,
  RemoteMode,
  resolveSection,
  SecondaryNav,
  secondaryNavItems,
  SectionId,
  Tab,
} from "./navigation";

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
    CustomName?: boolean;
    RxBytes?: number;
    TxBytes?: number;
    RxBps?: number;
    TxBps?: number;
  }[];
  services: {
    Name: string;
    Label: string;
    State: string;
    PID: number;
    UptimeSec: number;
    Port: number;
    webUrl: string;
    currentVersion?: string;
    baseVersion?: string;
    customBuild?: boolean;
    latestVersion?: string;
    upgradable?: boolean;
    hasBackup?: boolean;
    canUpgrade?: boolean;
    upgradeHint?: string;
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
  history?: HistoryPoint[];
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
  PPPoEUser?: string;
  PPPoEPass?: string;
  PPPoEBackend?: string;
  WANMAC?: string;
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
type MihomoSpeedTestRun = {
  stage: "preliminary" | "confirmation";
  requestedBytes: number;
  receivedBytes: number;
  durationMs: number;
  ttfbMs: number;
  mbps: number;
  httpStatus: number;
  success: boolean;
  errorCode?: string;
};
type MihomoSpeedTestNodeResult = {
  node: string;
  protocol: string;
  delayMs: number;
  runs: MihomoSpeedTestRun[];
  medianMbps: number;
  minimumMbps: number;
  effectiveMbps: number;
  eligible: boolean;
  videoBadge: string;
  errorCode?: string;
};
type MihomoSpeedTestJob = {
  id: string;
  mode: "quick" | "deep" | "";
  state: "idle" | "running" | "completed" | "cancelled" | "failed" | "interrupted";
  phase: "idle" | "discovering" | "sampling_wan" | "preliminary" | "confirming" | "completed" | "cancelled" | "failed" | "interrupted";
  startedAt?: string;
  finishedAt?: string;
  currentNode?: string;
  completedRuns: number;
  totalRuns: number;
  bytesUsed: number;
  dataBudgetBytes: number;
  contaminated: boolean;
  initialProductionNode?: string;
  recommendedNode?: string;
  candidateFingerprint?: string;
  errorMessage?: string;
  errorCode?: string;
  results: MihomoSpeedTestNodeResult[];
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
  cached: boolean;
  nodeCount: number;
  needsUpdate: boolean;
  active: boolean;
  autoUpdateEnabled?: boolean;
  autoUpdateHours?: number;
  nextAutoUpdateAt?: string;
  createdAt: string;
  updatedAt: string;
};
type SubscriptionNodeInfo = {
  name: string;
  type?: string;
  server?: string;
  port?: string;
};
type SubscriptionList = {
  activeId: string;
  items: SubscriptionItem[];
  autoUpdateHours: number;
  autoUpdateEnabled?: boolean;
};
type SubscriptionInfo = {
  configured: boolean;
  source: string;
  lastUpdated: string;
  autoUpdateHours: number;
  autoUpdateEnabled?: boolean;
};
type HistoryPoint = {
  at: number;
  cpu: number;
  temperature: number;
  batteryTemperature: number;
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

type LogSource =
  | "events"
  | "kernel"
  | "storage"
  | "network"
  | "aria2"
  | "openlist"
  | "rclone"
  | "admin";
type SystemLogResponse = {
  source: LogSource;
  label: string;
  updatedAt: string;
  entries: {
    time: string;
    level: "info" | "warning" | "error";
    source: string;
    message: string;
  }[];
  info: number;
  warning: number;
  error: number;
};

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
      Name: "ppp0",
      MAC: "46:1f:c3:0b:89:d0",
      State: "up",
      Carrier: "1",
      Role: "WAN / 宽带拨号",
      MTU: 1492,
      SpeedMbps: 1000,
      Addresses: ["100.64.12.88/32"],
      RxBytes: 18_420_000_000,
      TxBytes: 4_230_000_000,
      RxBps: 8_450_000,
      TxBps: 920_000,
    },
    {
      Name: "eth1",
      MAC: "46:1f:c3:0b:89:d1",
      State: "up",
      Carrier: "1",
      Role: "LAN / AP 桥接",
      MTU: 1500,
      SpeedMbps: 1000,
      Addresses: ["192.168.2.1/24"],
      RxBytes: 4_890_000_000,
      TxBytes: 19_150_000_000,
      RxBps: 1_280_000,
      TxBps: 8_620_000,
    },
    {
      Name: "wlan0",
      MAC: "46:1f:c3:0b:89:d2",
      State: "up",
      Carrier: "1",
      Role: "管理网络",
      MTU: 1500,
      SpeedMbps: 0,
      Addresses: ["192.168.10.107/24"],
      RxBytes: 520_000_000,
      TxBytes: 180_000_000,
      RxBps: 12_000,
      TxBps: 5_000,
    },
  ],
  clients: [
    {
      IP: "192.168.2.151",
      MAC: "02:00:00:00:00:01",
      Hostname: "演示手机",
      Interface: "eth1",
      State: "在线",
      Source: "DHCP",
      Expires: 3600,
      StaticLease: false,
      StaticIP: "",
      RxBytes: 1_420_000_000,
      TxBytes: 14_850_000_000,
      RxBps: 245_000,
      TxBps: 7_120_000,
    },
    {
      IP: "192.168.2.200",
      MAC: "02:00:00:00:00:02",
      Hostname: "演示摄像头",
      Interface: "eth1",
      State: "在线",
      Source: "静态 DHCP",
      Expires: 0,
      StaticLease: true,
      StaticIP: "192.168.2.200",
      RxBytes: 3_450_000_000,
      TxBytes: 180_000_000,
      RxBps: 1_020_000,
      TxBps: 25_000,
    },
    {
      IP: "192.168.2.105",
      MAC: "02:00:00:00:00:03",
      Hostname: "办公笔记本",
      Interface: "eth1",
      State: "在线",
      Source: "DHCP",
      Expires: 7200,
      StaticLease: false,
      StaticIP: "",
      RxBytes: 20_000_000,
      TxBytes: 4_120_000_000,
      RxBps: 15_000,
      TxBps: 1_475_000,
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
    "rclone",
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
          : name === "rclone"
            ? "Rclone · 云同步"
          : name,
    State: "运行中",
    PID: 4400 + i,
    UptimeSec: 43000,
    Port:
      {
        openlist: 5244,
        rclone: 5572,
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
    PPPoEUser: "",
    PPPoEPass: "",
    PPPoEBackend: "auto",
    WANMAC: "",
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
  const [standalone, setStandalone] = useState(false);
  const [activeSections, setActiveSections] = useState<
    Partial<Record<Tab, SectionId>>
  >({});
  const [history, setHistory] = useState<HistoryPoint[]>([]);
  const [notice, setNotice] = useState("");
  const [logText, setLogText] = useState("");
  const [logService, setLogService] = useState("");
  const refreshSequence = useRef(0);

  const navigate = useCallback(
    (
      view: Tab,
      requestedSection?: SectionId,
      historyMode: "push" | "replace" = "push",
    ) => {
      if (view === "remote") {
        const section =
          requestedSection || activeSections.remote || "screen";
        if (standalone) {
          setActiveSections((old) => ({ ...old, remote: section }));
          const url = new URL(window.location.href);
          url.searchParams.set("view", "remote");
          url.searchParams.set("section", section);
          url.searchParams.set("standalone", "1");
          window.history.replaceState(null, "", url);
          return;
        }
        window.open(
          `/?view=remote&section=${section}&standalone=1`,
          "_blank",
        );
        return;
      }

      const section = resolveSection(
        view,
        requestedSection || activeSections[view],
      );
      setActive(view);
      if (section) {
        setActiveSections((old) => ({ ...old, [view]: section }));
      }
      if (typeof window === "undefined") return;
      const url = new URL(window.location.href);
      url.searchParams.set("view", view);
      if (section) url.searchParams.set("section", section);
      else url.searchParams.delete("section");
      url.searchParams.delete("standalone");
      if (historyMode === "replace") {
        window.history.replaceState(null, "", url);
      } else {
        window.history.pushState(null, "", url);
      }
    },
    [activeSections, standalone],
  );

  useEffect(() => {
    const syncFromLocation = (normalize = false) => {
      const url = new URL(window.location.href);
      const requestedView = url.searchParams.get("view");
      const isStandalone =
        url.searchParams.get("standalone") === "1" ||
        url.searchParams.get("standalone") === "true" ||
        requestedView === "remote";
      setStandalone(isStandalone);
      const view = isTab(requestedView)
        ? requestedView
        : isStandalone
          ? "remote"
          : "overview";
      const section = resolveSection(view, url.searchParams.get("section"));
      setActive(view);
      if (section) {
        setActiveSections((old) => ({ ...old, [view]: section }));
      }
      if (!normalize) return;
      url.searchParams.set("view", view);
      if (section) url.searchParams.set("section", section);
      else url.searchParams.delete("section");
      if (isStandalone) url.searchParams.set("standalone", "1");
      else url.searchParams.delete("standalone");
      window.history.replaceState(null, "", url);
    };
    syncFromLocation(true);
    const onPopState = () => syncFromLocation(true);
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const refresh = useCallback(async (quiet = false, force = !quiet) => {
    const sequence = ++refreshSequence.current;
    try {
      const response = await fetch(
        force ? "/api/snapshot?fresh=1" : "/api/snapshot",
        { cache: "no-store" },
      );
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
        batteryTemperature: next.power.BatteryTemp,
        memory,
        wanDown: wan?.RxBps || 0,
        wanUp: wan?.TxBps || 0,
        lanDown: lan?.TxBps || 0,
        lanUp: lan?.RxBps || 0,
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
      setHistory((old) =>
        next.history?.length
          ? next.history.slice(-1500)
          : [...old.slice(-1499), point],
      );
      if (!quiet) setNotice("数据已刷新");
    } catch {
      if (sequence !== refreshSequence.current) return;
      if (
        location.hostname === "localhost" ||
        location.hostname === "127.0.0.1"
      ) {
        setAuth("demo");
        setHistory((old) => [
          ...old.slice(-1499),
          {
            at: Date.now(),
            cpu: demo.cpu.usage,
            temperature: demo.cpu.temperature,
            batteryTemperature: demo.power.BatteryTemp,
            memory: (demo.memory.Used / demo.memory.Total) * 100,
            wanDown: demo.interfaces[0]?.RxBps || 0,
            wanUp: demo.interfaces[0]?.TxBps || 0,
            lanDown: demo.interfaces[1]?.TxBps || 0,
            lanUp: demo.interfaces[1]?.RxBps || 0,
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
    const poll = async (force = false) => {
      if (document.hidden) return;
      await refresh(true, force);
      if (!stopped && !document.hidden) {
        timer = window.setTimeout(poll, 10000);
      }
    };
    const onVisibilityChange = () => {
      clearTimeout(timer);
      if (!document.hidden && !stopped) void poll(true);
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibilityChange);
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
  const activeSection = resolveSection(active, activeSections[active]);
  const sectionTitle = secondaryNavItems[active]?.find(
    (item) => item.id === activeSection,
  )?.label;
  const remoteMode: RemoteMode = activeSection === "screen" ? "screen" : "terminal";

  if (standalone) {
    return (
      <main className="standalone-shell">
        <header className="standalone-header">
          <div className="standalone-brand">
            <span className="brand-mark">1+</span>
            <strong>一加 7 Pro 路由器</strong>
            <span className="standalone-badge">
              {remoteMode === "screen" ? "全屏控制" : "网页终端"}
            </span>
          </div>
          <div className="standalone-header-actions">
            <div className="remote-mode-switch">
              <button
                type="button"
                className={remoteMode === "screen" ? "active" : ""}
                onClick={() => navigate("remote", "screen")}
              >
                全屏控制
              </button>
              <button
                type="button"
                className={remoteMode === "terminal" ? "active" : ""}
                onClick={() => navigate("remote", "terminal")}
              >
                网页终端
              </button>
            </div>
          </div>
        </header>
        <div className="standalone-content">
          <RemoteMaintenance mode={remoteMode} />
        </div>
      </main>
    );
  }

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
    window.open(`/?view=remote&section=${mode}&standalone=1`, "_blank");
  };

  const viewContent = (
    <>
      {active === "overview" && (
        <Overview
          data={data}
          history={history}
          health={health}
          running={running}
          external={external}
          setActive={(tab) => navigate(tab)}
          onRemote={openRemote}
        />
      )}
      {active === "network" && activeSection && (
        <Network
          data={data}
          history={history}
          onNotice={setNotice}
          section={activeSection}
        />
      )}
      {active === "clients" && activeSection && (
        <Clients
          data={data}
          history={history}
          refresh={refresh}
          setNotice={setNotice}
          section={activeSection}
        />
      )}
      {active === "services" && activeSection && (
        <Services
          data={data}
          history={history}
          refresh={refresh}
          setNotice={setNotice}
          section={activeSection}
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
      {active === "mihomo" && activeSection && (
        <MihomoManager
          data={data}
          refresh={refresh}
          setNotice={setNotice}
          section={activeSection}
          onNavigateSection={(nextSection) => navigate("mihomo", nextSection)}
        />
      )}
      {active === "storage" && <Storage data={data} />}
      {active === "logs" && <SystemLogs />}
      {active === "system" && activeSection && (
        <System
          data={data}
          history={history}
          onRefresh={() => refresh(true)}
          section={activeSection}
        />
      )}
    </>
  );

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
              onClick={() => navigate(item.id)}
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
            <p className="eyebrow">
              {sectionTitle ? `${title} / ${sectionTitle}` : title}
            </p>
            <h1>
              {active === "overview"
                ? `一加 7 Pro 状态${health >= 90 ? "稳定" : "需要注意"}`
                : sectionTitle
                  ? `${title} · ${sectionTitle}`
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
        {activeSection ? (
          <>
            <SecondaryNav
              view={active}
              active={activeSection}
              onSelect={(section) => navigate(active, section)}
            />
            <section
              className="secondary-panel-host"
              id={`secondary-panel-${active}`}
              role="tabpanel"
              aria-labelledby={`secondary-${active}-${activeSection}`}
            >
              {viewContent}
            </section>
          </>
        ) : (
          viewContent
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
  const batteryTemperature = data.power.BatteryTemp;
  const metrics = [
    {
      label: "CPU 负载",
      value: `${data.cpu.usage.toFixed(1)}%`,
      detail: `${data.cpu.Online} 核 · ${data.cpu.avgMHz} MHz`,
      tone: "cyan",
    },
    {
      label: "电池温度",
      value: `${batteryTemperature.toFixed(1)}°C`,
      detail:
        batteryTemperature < 40
          ? "温度正常"
          : batteryTemperature < 45
            ? "温度偏高，留意通风"
            : "温度过高，检查散热",
      tone: batteryTemperature < 40 ? "green" : "amber",
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
          <h2>独立工作台</h2>
          <p>全屏屏幕控制与 Web 终端已全面采用新 Tab 独立运行，后台长效保活，不中断维护会话。</p>
        </div>
        <div className="remote-tools-actions">
          <button
            type="button"
            title="在新标签页中独立打开全屏屏幕控制，切后台长效保活"
            onClick={() => onRemote("screen")}
          >
            <span>SCREEN</span>
            <strong>全屏控制 ↗</strong>
            <small>独立新标签页 · 实时屏幕与键鼠触控</small>
          </button>
          <button
            type="button"
            title="在新标签页中独立打开网页终端，切后台长效保活"
            onClick={() => onRemote("terminal")}
          >
            <span>SSH</span>
            <strong>网页终端 ↗</strong>
            <small>独立新标签页 · root / Termux 终端</small>
          </button>
        </div>
      </section>
      <section className="lower-grid">
        <article className="panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">最近 10 分钟</p>
              <h2>CPU 与温度趋势</h2>
            </div>
            <strong>{data.cpu.usage.toFixed(1)}%</strong>
          </div>
          <TimeSeriesChart
            history={history}
            series={[
              { label: "CPU %", color: "#30c7d8", value: (p) => p.cpu },
              {
                label: "电池 °C",
                color: "#52d395",
                value: (p) => p.batteryTemperature,
              },
              {
                label: "CPU 最高 °C",
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

function RemoteMaintenance({ mode }: { mode: RemoteMode }) {
  const [root, setRoot] = useState(false);
  return (
    <div
      className={`view remote-console ${mode === "screen" ? "screen-mode" : ""}`}
    >
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
  windowMs = 10 * 60 * 1000,
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
  windowMs?: number;
}) {
  const points = history.length
    ? history
    : [
        {
          at: 0,
          cpu: 0,
          temperature: 0,
          batteryTemperature: 0,
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
  section,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  onNotice: (value: string) => void;
  section: SectionId;
}) {
  const [draft, setDraft] = useState(data.networkDraft);
  const [saving, setSaving] = useState(false);
  const [reconnecting, setReconnecting] = useState(false);
  const [showPass, setShowPass] = useState(false);
  const [confirmReconnect, setConfirmReconnect] = useState(false);

  useEffect(() => {
    if (data.networkDraft) {
      setDraft((prev) => {
        if (!prev.WANInterface && !prev.PPPoEUser) {
          return data.networkDraft;
        }
        return prev;
      });
    }
  }, [data.networkDraft]);

  const wan = data.interfaces.find((item) => item.Role.includes("WAN"));
  const lan = data.interfaces.find((item) => item.Role.includes("LAN"));
  const pppInterface = data.interfaces.find((item) => item.Name.startsWith("ppp"));

  const update = (key: keyof NetworkDraft, value: string | number) =>
    setDraft((old) => ({ ...old, [key]: value }));

  const syncFromDevice = () => {
    setDraft(data.networkDraft);
    onNotice("已从路由器底层重新同步配置记录");
  };

  const save = async (quiet = false): Promise<boolean> => {
    setSaving(true);
    try {
      const r = await fetch("/api/network-draft", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(draft),
      });
      const b = (await r.json()) as { error?: string; ok?: boolean };
      if (!r.ok || !b.ok) {
        onNotice(b.error || "保存失败");
        return false;
      }
      if (!quiet) {
        onNotice("网络与宽带参数已成功保存并同步至系统底层配置");
      }
      return true;
    } catch (e: any) {
      onNotice(`保存请求异常: ${e?.message || e}`);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const handleSaveAndReconnect = async () => {
    const saved = await save(true);
    if (!saved) return;
    setReconnecting(true);
    onNotice("已下发重新拨号指令，正在平滑释放 ISP 会话（约需 10-15 秒，局域网与后台保持连通）...");
    try {
      const r = await fetch("/api/network/reconnect", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ backend: draft.PPPoEBackend || "auto" }),
      });
      const b = (await r.json()) as { error?: string; message?: string; ok?: boolean };
      if (r.ok && b.ok) {
        onNotice(b.message || "宽带重新拨号请求已成功生效，正在重新连接...");
      } else {
        onNotice(b.error || "重新拨号执行失败");
      }
    } catch (e: any) {
      onNotice(`重拨请求异常: ${e?.message || e}`);
    } finally {
      setReconnecting(false);
      setConfirmReconnect(false);
    }
  };

  return (
    <div className="view">
      <ModuleContextBar>
        <strong>PPPoE 路由已接管</strong>
        <span>WAN {wan?.Name || "未识别"}</span>
        <span>LAN {lan?.Name || "未识别"}</span>
      </ModuleContextBar>
      {section === "status" && (
        <>
          <div className="section-heading">
            <div>
              <p className="eyebrow">正式部署 · 接口与流量</p>
              <h2>网络接口</h2>
            </div>
            <span className="state-good">实时状态</span>
          </div>
          <article className="panel history-panel">
            <div className="panel-title">
              <div>
                <p className="eyebrow">最近 10 分钟 · 5 秒滑动平均</p>
                <h2>WAN / LAN 吞吐</h2>
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
        </>
      )}
      {section === "config" && (
        <article className="panel config-panel section-page-first">
          <div className="panel-title" style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", flexWrap: "wrap", gap: "12px" }}>
            <div>
              <p className="eyebrow">网口拓扑 · PPPoE 宽带 · 局域网 DHCP</p>
              <h2>网络配置中心</h2>
            </div>
            <div style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
              <button
                type="button"
                onClick={syncFromDevice}
                disabled={saving || reconnecting}
                style={{ background: "#102a36", color: "#8da5b3", border: "1px solid #244453", borderRadius: "8px", padding: "8px 13px", cursor: "pointer", fontSize: "11px" }}
              >
                从设备同步
              </button>
              <button
                type="button"
                onClick={() => save(false)}
                disabled={saving || reconnecting}
                style={{ background: "#163f4f", color: "#54d7e6", border: "1px solid #28647a", borderRadius: "8px", padding: "8px 14px", cursor: "pointer", fontSize: "11px", fontWeight: "bold" }}
              >
                {saving ? "正在保存..." : "仅保存配置"}
              </button>
              <button
                type="button"
                onClick={() => setConfirmReconnect(true)}
                disabled={saving || reconnecting}
                style={{ background: "linear-gradient(90deg, #d97706, #ea580c)", color: "#ffffff", border: 0, borderRadius: "8px", padding: "8px 16px", cursor: "pointer", fontSize: "11px", fontWeight: "bold", boxShadow: "0 4px 12px rgba(217, 119, 6, 0.3)" }}
              >
                {reconnecting ? "正在重拨 (约10秒)..." : "保存并重新拨号"}
              </button>
            </div>
          </div>

          {confirmReconnect && (
            <div style={{
              margin: "16px 0",
              padding: "14px 18px",
              borderRadius: "10px",
              border: "1px solid #ea580c",
              background: "#231508",
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: "14px",
              flexWrap: "wrap",
            }}>
              <div style={{ color: "#fdba74", fontSize: "11px", lineHeight: "1.7", flex: 1, minWidth: "260px" }}>
                <strong>⚠️ 确认执行重新拨号？</strong><br />
                系统将保存当前设置并调用 PPPoE 守护进程平滑释放旧连接后重新发起协商（耗时约 10-15 秒）。<br />
                <strong>局域网管理与本网页（192.168.2.1）将始终保持连接，不会中断。</strong>
              </div>
              <div style={{ display: "flex", gap: "8px" }}>
                <button
                  type="button"
                  disabled={reconnecting}
                  onClick={handleSaveAndReconnect}
                  style={{ background: "#ea580c", color: "#ffffff", fontWeight: "bold", border: 0, borderRadius: "6px", padding: "8px 16px", cursor: "pointer", fontSize: "11px" }}
                >
                  {reconnecting ? "处理中..." : "确认重新拨号"}
                </button>
                <button
                  type="button"
                  disabled={reconnecting}
                  onClick={() => setConfirmReconnect(false)}
                  style={{ background: "#1c2328", border: "1px solid #4b5563", color: "#9ca3af", borderRadius: "6px", padding: "8px 12px", cursor: "pointer", fontSize: "11px" }}
                >
                  取消
                </button>
              </div>
            </div>
          )}

          {/* WAN 上联与 PPPoE 配置区域 */}
          <div style={{ marginTop: "24px" }}>
            <h3 style={{ fontSize: "13px", color: "#4ecdc4", margin: "0 0 12px", display: "flex", alignItems: "center", gap: "8px" }}>
              <span style={{ display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: "#4ecdc4" }} />
              WAN 物理上联与宽带接入
            </h3>
            <div className="form-grid">
              <label>
                WAN 物理网口
                <select
                  value={draft.WANInterface}
                  onChange={(e) => update("WANInterface", e.target.value)}
                >
                  {data.interfaces
                    .filter((i) => i.Name.startsWith("eth"))
                    .map((i) => (
                      <option key={i.Name} value={i.Name}>{i.Name} {i.MAC ? `(${i.MAC})` : ""}</option>
                    ))}
                </select>
              </label>

              <label>
                WAN 接入模式
                <select
                  value={draft.WANMode}
                  onChange={(e) => update("WANMode", e.target.value)}
                >
                  <option value="pppoe">PPPoE 宽带拨号 (推荐)</option>
                  <option value="dhcp">DHCP 自动获取 IP</option>
                  <option value="static">静态 IP 地址</option>
                </select>
              </label>

              {draft.WANMode === "pppoe" && (
                <>
                  <label>
                    宽带账号 (Username)
                    <input
                      value={draft.PPPoEUser || ""}
                      placeholder="例如 057101234567"
                      onChange={(e) => update("PPPoEUser", e.target.value)}
                    />
                  </label>

                  <label>
                    宽带密码 (Password)
                    <div style={{ position: "relative" }}>
                      <input
                        type={showPass ? "text" : "password"}
                        value={draft.PPPoEPass || ""}
                        placeholder="宽带密码"
                        onChange={(e) => update("PPPoEPass", e.target.value)}
                        style={{ paddingRight: "36px" }}
                      />
                      <button
                        type="button"
                        onClick={() => setShowPass(!showPass)}
                        style={{
                          position: "absolute",
                          right: "8px",
                          top: "50%",
                          transform: "translateY(-50%)",
                          background: "transparent",
                          border: 0,
                          color: "#6b8292",
                          cursor: "pointer",
                          fontSize: "12px",
                          padding: "4px",
                        }}
                        title={showPass ? "隐藏密码" : "显示密码"}
                      >
                        {showPass ? "🙈" : "👁️"}
                      </button>
                    </div>
                  </label>

                  <label>
                    PPPoE 驱动后端
                    <select
                      value={draft.PPPoEBackend || "auto"}
                      onChange={(e) => update("PPPoEBackend", e.target.value)}
                    >
                      <option value="auto">auto · 自动 (首选内核驱动，失败降级用户态)</option>
                      <option value="kernel">kernel · 强制内核驱动 (rp-pppoe 零拷贝极低延迟)</option>
                      <option value="userspace">userspace · 强制用户态驱动 (高兼容性)</option>
                    </select>
                  </label>

                  <label>
                    <span style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                      克隆 MAC (WAN MAC)
                      <button
                        type="button"
                        onClick={() => {
                          const targetIface = data.interfaces.find((i) => i.Name === draft.WANInterface);
                          if (targetIface?.MAC) {
                            update("WANMAC", targetIface.MAC);
                            onNotice(`已填入 ${draft.WANInterface} 网卡硬件 MAC: ${targetIface.MAC}`);
                          }
                        }}
                        style={{
                          background: "none",
                          border: 0,
                          color: "#30c7d8",
                          fontSize: "9px",
                          cursor: "pointer",
                          textDecoration: "underline",
                          padding: 0,
                        }}
                      >
                        读取网卡 MAC
                      </button>
                    </span>
                    <input
                      value={draft.WANMAC || ""}
                      placeholder="例如 12:34:56:78:9a:bc"
                      onChange={(e) => update("WANMAC", e.target.value)}
                    />
                  </label>

                  <label>
                    MTU 最大传输单元
                    <input
                      type="number"
                      min={576}
                      max={9000}
                      value={draft.MTU || 1492}
                      onChange={(e) => update("MTU", Number(e.target.value))}
                    />
                  </label>
                </>
              )}
            </div>
          </div>

          {/* LAN 局域网配置区域 */}
          <div style={{ marginTop: "24px" }}>
            <h3 style={{ fontSize: "13px", color: "#a897ff", margin: "0 0 12px", display: "flex", alignItems: "center", gap: "8px" }}>
              <span style={{ display: "inline-block", width: "6px", height: "6px", borderRadius: "50%", background: "#a897ff" }} />
              LAN 局域网与 DHCP 服务
            </h3>
            <div className="form-grid">
              <label>
                LAN 物理网口
                <select
                  value={draft.LANInterface}
                  onChange={(e) => update("LANInterface", e.target.value)}
                >
                  {data.interfaces
                    .filter((i) => i.Name.startsWith("eth"))
                    .map((i) => (
                      <option key={i.Name} value={i.Name}>{i.Name} {i.MAC ? `(${i.MAC})` : ""}</option>
                    ))}
                </select>
              </label>

              <label>
                LAN 网关地址 / 掩码
                <input
                  value={draft.LANAddress}
                  placeholder="192.168.2.1/24"
                  onChange={(e) => update("LANAddress", e.target.value)}
                />
              </label>

              <label>
                本地 DNS 服务器
                <input
                  value={draft.DNS}
                  placeholder="192.168.2.1"
                  onChange={(e) => update("DNS", e.target.value)}
                />
              </label>

              <label>
                DHCP 地址池起始
                <input
                  value={draft.DHCPStart}
                  placeholder="192.168.2.100"
                  onChange={(e) => update("DHCPStart", e.target.value)}
                />
              </label>

              <label>
                DHCP 地址池结束
                <input
                  value={draft.DHCPEnd}
                  placeholder="192.168.2.249"
                  onChange={(e) => update("DHCPEnd", e.target.value)}
                />
              </label>
            </div>
          </div>

          {/* 当前状态汇总与防护说明 */}
          <div style={{ marginTop: "20px", padding: "14px", borderRadius: "10px", background: "#0a1f2b", border: "1px solid #1c3c4f" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: "8px", marginBottom: "8px" }}>
              <span style={{ fontSize: "11px", color: "#8daab9" }}>当前实时会话状态：</span>
              <span style={{ fontSize: "11px", color: pppInterface ? "#52d395" : "#e2b469", fontWeight: "bold" }}>
                {pppInterface ? `● PPPoE 已连接 (${pppInterface.Name} · ${pppInterface.Addresses?.[0] || "已获 IP"})` : "○ 正在等待链路或重新拨号"}
              </span>
            </div>
            <p className="safe-note" style={{ margin: 0 }}>
              当前硬件映射：<strong>{draft.WANInterface || "eth0"}</strong>（RTL8153 千兆网卡）上联光猫并承载 PPPoE，<strong>{draft.LANInterface || "eth1"}</strong> 下联无线 AP，局域网管理地址为 <strong>{draft.LANAddress || "192.168.2.1"}</strong>。<br />
              • 点击<strong>【仅保存配置】</strong>将配置原子写入系统 <code>/data/adb/router-network/config</code> 并自动生成 <code>.bak</code> 备份，不重启或中断当前正在运行的网络连接。<br />
              • 点击<strong>【保存并重新拨号】</strong>会平滑释放运营商 BRAS 会话并在 10 秒安全防抖周期后重新协商，局域网管理、DHCP 与本 Web 控制台全程保持在线。
            </p>
          </div>
        </article>
      )}
    </div>
  );
}

function Clients({
  data,
  history,
  refresh,
  setNotice,
  section,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  refresh: (quiet?: boolean) => Promise<void>;
  setNotice: (value: string) => void;
  section: SectionId;
}) {
  const [editingMac, setEditingMac] = useState("");
  const [editMode, setEditMode] = useState<"name" | "lease">("name");
  const [clientName, setClientName] = useState("");
  const [staticIP, setStaticIP] = useState("");
  const [pendingMac, setPendingMac] = useState("");
  const [error, setError] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [filterType, setFilterType] = useState<"all" | "online" | "active" | "static">("all");
  type SortField = "hostname" | "ip" | "txBps" | "rxBps" | "txBytes" | "rxBytes";
  type SortDirection = "asc" | "desc";

  const [sortField, setSortField] = useState<SortField>("hostname");
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      if (
        field === "txBps" ||
        field === "rxBps" ||
        field === "txBytes" ||
        field === "rxBytes"
      ) {
        setSortDirection("desc");
      } else {
        setSortDirection("asc");
      }
    }
  };

  const beginNameEdit = (client: Snapshot["clients"][number]) => {
    setEditingMac(client.MAC);
    setEditMode("name");
    setClientName(
      client.Hostname === "未知设备" || client.Hostname === "固定 IP 客户端"
        ? ""
        : client.Hostname,
    );
    setError("");
  };

  const beginLeaseEdit = (client: Snapshot["clients"][number]) => {
    setEditingMac(client.MAC);
    setEditMode("lease");
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

  const saveClientName = async (mac: string) => {
    const name = clientName.trim();
    if (!name) {
      setError("请输入设备名称");
      return;
    }
    if (Array.from(name).length > 64 || /[\u0000-\u001f\u007f]/.test(name)) {
      setError("设备名称不能包含控制字符，且最长为 64 个字符");
      return;
    }

    setPendingMac(mac);
    setError("");
    try {
      const response = await fetch("/api/clients/name", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mac, name }),
      });
      if (!response.ok) {
        throw new Error(await readError(response, "保存设备名称失败"));
      }
      setEditingMac("");
      setNotice(`已将设备命名为 ${name}`);
      await refresh(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存设备名称失败");
    } finally {
      setPendingMac("");
    }
  };

  const clearClientName = async (client: Snapshot["clients"][number]) => {
    setPendingMac(client.MAC);
    setError("");
    try {
      const response = await fetch("/api/clients/name", {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mac: client.MAC }),
      });
      if (!response.ok) {
        throw new Error(await readError(response, "恢复自动名称失败"));
      }
      setEditingMac("");
      setNotice("已恢复设备自动名称");
      await refresh(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "恢复自动名称失败");
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

  // 接口识别：WAN (互联网出入口) 与 LAN (局域网 AP 桥接)
  const wan =
    data.interfaces.find((i) => i.Name.startsWith("ppp")) ||
    data.interfaces.find((i) => i.Role.includes("WAN")) ||
    data.interfaces.find((i) => i.Name === data.networkDraft.WANInterface);
  const lan =
    data.interfaces.find((i) => i.Role.includes("LAN")) ||
    data.interfaces.find((i) => i.Name === data.networkDraft.LANInterface) ||
    data.interfaces.find((i) => i.Name === "eth1");

  // 客户端聚合流量
  const clientUploadTotal = data.clients.reduce((acc, c) => acc + (c.RxBytes || 0), 0);
  const clientDownloadTotal = data.clients.reduce((acc, c) => acc + (c.TxBytes || 0), 0);
  const clientUploadRate = data.clients.reduce((acc, c) => acc + (c.RxBps || 0), 0);
  const clientDownloadRate = data.clients.reduce((acc, c) => acc + (c.TxBps || 0), 0);

  // LAN 局域网总计：
  // 软路由发送至 LAN = 客户端下行 (Client Download)
  // 软路由从 LAN 接收 = 客户端上行 (Client Upload)
  const lanDownloadSpeed = lan?.TxBps ?? clientDownloadRate;
  const lanUploadSpeed = lan?.RxBps ?? clientUploadRate;
  const lanDownloadTotal = lan?.TxBytes ?? clientDownloadTotal;
  const lanUploadTotal = lan?.RxBytes ?? clientUploadTotal;
  const lanTotalTraffic = lanDownloadTotal + lanUploadTotal;

  // WAN 外网总计：
  // 软路由从 WAN 接收 = 互联网下载 (WAN Download)
  // 软路由向 WAN 发送 = 互联网上传 (WAN Upload)
  const wanDownSpeed = wan?.RxBps ?? 0;
  const wanUpSpeed = wan?.TxBps ?? 0;
  const wanDownTotal = wan?.RxBytes ?? 0;
  const wanUpTotal = wan?.TxBytes ?? 0;
  const wanTotalTraffic = wanDownTotal + wanUpTotal;

  // 状态统计
  const onlineCount = data.clients.filter(
    (c) => c.State === "在线" || (c.RxBps || 0) > 0 || (c.TxBps || 0) > 0,
  ).length;
  const activeTrafficCount = data.clients.filter(
    (c) => (c.RxBps || 0) > 500 || (c.TxBps || 0) > 500,
  ).length;
  const staticLeaseCount = data.clients.filter((c) => c.StaticLease).length;

  const filteredClients = useMemo(() => {
    let list = [...data.clients];
    const q = searchQuery.trim().toLowerCase();
    if (q) {
      list = list.filter(
        (c) =>
          (c.Hostname || "").toLowerCase().includes(q) ||
          (c.IP || "").toLowerCase().includes(q) ||
          (c.MAC || "").toLowerCase().includes(q),
      );
    }
    if (filterType === "online") {
      list = list.filter(
        (c) => c.State === "在线" || (c.RxBps || 0) > 0 || (c.TxBps || 0) > 0,
      );
    } else if (filterType === "active") {
      list = list.filter((c) => (c.RxBps || 0) > 0 || (c.TxBps || 0) > 0);
    } else if (filterType === "static") {
      list = list.filter((c) => c.StaticLease);
    }

    const parseIP = (ip: string) => {
      const parts = (ip || "").split(".").map((p) => parseInt(p, 10) || 0);
      return (
        ((parts[0] || 0) << 24) +
        ((parts[1] || 0) << 16) +
        ((parts[2] || 0) << 8) +
        (parts[3] || 0)
      );
    };

    list.sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case "hostname": {
          const nameA = a.Hostname || a.MAC || "";
          const nameB = b.Hostname || b.MAC || "";
          cmp = nameA.localeCompare(nameB, "zh-CN");
          break;
        }
        case "ip":
          cmp = parseIP(a.IP) - parseIP(b.IP);
          break;
        case "txBps":
          cmp = (a.TxBps || 0) - (b.TxBps || 0);
          break;
        case "rxBps":
          cmp = (a.RxBps || 0) - (b.RxBps || 0);
          break;
        case "txBytes":
          cmp = (a.TxBytes || 0) - (b.TxBytes || 0);
          break;
        case "rxBytes":
          cmp = (a.RxBytes || 0) - (b.RxBytes || 0);
          break;
        default:
          cmp = 0;
      }
      if (cmp !== 0) {
        return sortDirection === "asc" ? cmp : -cmp;
      }
      return parseIP(a.IP) - parseIP(b.IP);
    });
    return list;
  }, [data.clients, searchQuery, filterType, sortField, sortDirection]);

  return (
    <div className="view">
      <ModuleContextBar>
        <strong>{onlineCount} 台在线</strong>
        <span>{data.clients.length} 台已发现</span>
        <span>WAN ↓ {rate(wanDownSpeed)} · ↑ {rate(wanUpSpeed)}</span>
        {activeTrafficCount > 0 && <span>{activeTrafficCount} 台传输中</span>}
      </ModuleContextBar>

      {section === "traffic" && (
        <>
          <div className="section-heading">
            <div>
              <p className="eyebrow">eth1 · AP 桥接局域网与流量监控</p>
              <h2>外网与内网流量</h2>
            </div>
            <span className="live-badge">实时</span>
          </div>
          {/* WAN vs LAN 双域流量统计看板 */}
          <div className="client-traffic-overview">
        {/* WAN 外网看板 */}
        <article className="panel client-traffic-card wan-card">
          <header className="traffic-card-header">
            <div>
              <p className="eyebrow wan-eyebrow">互联网出入 · 外网流量</p>
              <h3>
                {wan?.Name
                  ? `${wan.Name} · ${wan.Role || "WAN"}`
                  : "WAN 外网链路"}
              </h3>
            </div>
            <span className="domain-pill wan-pill">WAN 链路</span>
          </header>
          <div className="traffic-metric-grid">
            <div className="metric-box">
              <small>WAN 实时下行 (外网下载)</small>
              <strong className="rate-val wan-down">
                ↓ {rate(wanDownSpeed)}
              </strong>
              <span>累计下载: {bytes(wanDownTotal)}</span>
            </div>
            <div className="metric-box">
              <small>WAN 实时上行 (外网上载)</small>
              <strong className="rate-val wan-up">↑ {rate(wanUpSpeed)}</strong>
              <span>累计上传: {bytes(wanUpTotal)}</span>
            </div>
            <div className="metric-box total">
              <small>外网累计总流量</small>
              <strong className="total-val">{bytes(wanTotalTraffic)}</strong>
              <span>
                链路:{" "}
                {wan?.Carrier === "1" || wan?.State === "up"
                  ? "已连接"
                  : "未连接"}
              </span>
            </div>
          </div>
        </article>

        {/* LAN 内网看板 */}
        <article className="panel client-traffic-card lan-card">
          <header className="traffic-card-header">
            <div>
              <p className="eyebrow lan-eyebrow">局域网设备 · 内网流量</p>
              <h3>
                {lan?.Name
                  ? `${lan.Name} · ${lan.Role || "LAN"}`
                  : "LAN 局域网 AP"}
              </h3>
            </div>
            <span className="domain-pill lan-pill">LAN 内网</span>
          </header>
          <div className="traffic-metric-grid">
            <div className="metric-box">
              <small>LAN 实时下行 (路由发往设备)</small>
              <strong className="rate-val lan-down">
                ↓ {rate(lanDownloadSpeed)}
              </strong>
              <span>局域网累计下发: {bytes(lanDownloadTotal)}</span>
            </div>
            <div className="metric-box">
              <small>LAN 实时上行 (设备发往路由)</small>
              <strong className="rate-val lan-up">
                ↑ {rate(lanUploadSpeed)}
              </strong>
              <span>局域网累计接收: {bytes(lanUploadTotal)}</span>
            </div>
            <div className="metric-box total">
              <small>内网累计总吞吐</small>
              <strong className="total-val">{bytes(lanTotalTraffic)}</strong>
              <span>已分配 {staticLeaseCount} 条静态租约</span>
            </div>
          </div>
        </article>
          </div>

          {/* WAN 与 LAN 实时吞吐趋势折线图 */}
          <article className="panel history-panel client-chart-panel">
        <div className="panel-title">
          <div>
            <p className="eyebrow">最近 10 分钟 · 5 秒滑动平均 · WAN vs LAN 双域对比</p>
            <h2>外网 / 内网实时吞吐走势</h2>
          </div>
          <div className="chart-legend-row">
            <span className="legend-item wan-down">
              <i />
              WAN 下行 (外网下载)
            </span>
            <span className="legend-item wan-up">
              <i />
              WAN 上行 (外网上载)
            </span>
            <span className="legend-item lan-down">
              <i />
              LAN 下行 (内网发送)
            </span>
            <span className="legend-item lan-up">
              <i />
              LAN 上传 (内网接收)
            </span>
          </div>
        </div>
        <TimeSeriesChart
          history={history}
          series={[
            { label: "WAN 下行", color: "#30c7d8", value: (p) => p.wanDown },
            { label: "WAN 上传", color: "#52d395", value: (p) => p.wanUp },
            { label: "LAN 下行", color: "#a897ff", value: (p) => p.lanDown },
            { label: "LAN 上传", color: "#f6ad55", value: (p) => p.lanUp },
          ]}
          format={rate}
        />
          </article>
          <p className="safe-note">
            说明：青/绿表示 WAN 互联网出入，紫/橙表示 LAN 局域网吞吐。累计量来自当前接口计数器。
          </p>
        </>
      )}

      {section === "devices" && (
        <>
          <div className="section-heading">
            <div>
              <p className="eyebrow">租约、实时速率与固定地址</p>
              <h2>客户端设备管理</h2>
            </div>
            <strong className="count-badge">{data.clients.length} 台设备</strong>
          </div>
          {/* 客户端列表与交互工具栏 */}
          <article className="panel clients-panel">
        <div className="panel-title clients-list-heading">
          <div>
            <p className="eyebrow">局域网设备连接与流量明细</p>
            <h2>已发现客户端</h2>
          </div>
          <span className="filtered-count">
            显示 {filteredClients.length} / 共 {data.clients.length} 台
          </span>
        </div>

        {/* 筛选与搜索工具条 */}
        <div className="clients-toolbar">
          <div className="clients-search-box">
            <input
              type="text"
              placeholder="按主机名、IP、MAC 地址过滤..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
            {searchQuery && (
              <button
                type="button"
                className="clear-search-btn"
                onClick={() => setSearchQuery("")}
                title="清空搜索"
              >
                ✕
              </button>
            )}
          </div>
          <div className="clients-filter-tabs">
            <button
              type="button"
              className={filterType === "all" ? "active" : ""}
              onClick={() => setFilterType("all")}
            >
              全部 ({data.clients.length})
            </button>
            <button
              type="button"
              className={filterType === "online" ? "active" : ""}
              onClick={() => setFilterType("online")}
            >
              在线 ({onlineCount})
            </button>
            <button
              type="button"
              className={filterType === "active" ? "active" : ""}
              onClick={() => setFilterType("active")}
            >
              传输中 ({activeTrafficCount})
            </button>
            <button
              type="button"
              className={filterType === "static" ? "active" : ""}
              onClick={() => setFilterType("static")}
            >
              静态租约 ({staticLeaseCount})
            </button>
          </div>
          <div className="clients-sort-box">
            <label htmlFor="client-sort-select">排序：</label>
            <select
              id="client-sort-select"
              value={`${sortField}-${sortDirection}`}
              onChange={(e) => {
                const [f, d] = e.target.value.split("-") as [SortField, SortDirection];
                setSortField(f);
                setSortDirection(d);
              }}
            >
              <option value="hostname-asc">设备名称 (A→Z)</option>
              <option value="hostname-desc">设备名称 (Z→A)</option>
              <option value="ip-asc">IP 地址 (升序)</option>
              <option value="ip-desc">IP 地址 (降序)</option>
              <option value="txBps-desc">实时下载速率 (高→低)</option>
              <option value="txBps-asc">实时下载速率 (低→高)</option>
              <option value="rxBps-desc">实时上传速率 (高→低)</option>
              <option value="rxBps-asc">实时上传速率 (低→高)</option>
              <option value="txBytes-desc">累计下载流量 (多→少)</option>
              <option value="txBytes-asc">累计下载流量 (少→多)</option>
              <option value="rxBytes-desc">累计上传流量 (多→少)</option>
              <option value="rxBytes-asc">累计上传流量 (少→多)</option>
            </select>
          </div>
        </div>

        {error && !editingMac && (
          <p className="static-lease-page-error" role="alert">
            {error}
          </p>
        )}

        <div className="table-wrap">
          <table className="clients-table">
            <thead>
              <tr>
                <th
                  className={`sortable-th ${sortField === "hostname" ? "sorted" : ""}`}
                  onClick={() => handleSort("hostname")}
                  title={`点击按设备名称${sortField === "hostname" && sortDirection === "asc" ? "降序" : "升序"}排序`}
                >
                  <div className="th-content">
                    <span>设备</span>
                    <span className="sort-icon">{sortField === "hostname" ? (sortDirection === "asc" ? "▲" : "▼") : "↕"}</span>
                  </div>
                </th>
                <th
                  className={`sortable-th ${sortField === "ip" ? "sorted" : ""}`}
                  onClick={() => handleSort("ip")}
                  title={`点击按 IP 地址${sortField === "ip" && sortDirection === "asc" ? "降序" : "升序"}排序`}
                >
                  <div className="th-content">
                    <span>IP 地址</span>
                    <span className="sort-icon">{sortField === "ip" ? (sortDirection === "asc" ? "▲" : "▼") : "↕"}</span>
                  </div>
                </th>
                <th
                  className={`sortable-th th-rate down ${sortField === "txBps" ? "sorted" : ""}`}
                  onClick={() => handleSort("txBps")}
                  title={`点击按实时下载速率${sortField === "txBps" && sortDirection === "desc" ? "升序" : "降序"}排序`}
                >
                  <div className="th-content">
                    <span>实时下载</span>
                    <span className="sort-icon">{sortField === "txBps" ? (sortDirection === "asc" ? "▲" : "▼") : "↕"}</span>
                  </div>
                </th>
                <th
                  className={`sortable-th th-rate up ${sortField === "rxBps" ? "sorted" : ""}`}
                  onClick={() => handleSort("rxBps")}
                  title={`点击按实时上传速率${sortField === "rxBps" && sortDirection === "desc" ? "升序" : "降序"}排序`}
                >
                  <div className="th-content">
                    <span>实时上传</span>
                    <span className="sort-icon">{sortField === "rxBps" ? (sortDirection === "asc" ? "▲" : "▼") : "↕"}</span>
                  </div>
                </th>
                <th
                  className={`sortable-th th-traffic down ${sortField === "txBytes" ? "sorted" : ""}`}
                  onClick={() => handleSort("txBytes")}
                  title={`点击按累计下载流量${sortField === "txBytes" && sortDirection === "desc" ? "升序" : "降序"}排序`}
                >
                  <div className="th-content">
                    <span>累计下载</span>
                    <span className="sort-icon">{sortField === "txBytes" ? (sortDirection === "asc" ? "▲" : "▼") : "↕"}</span>
                  </div>
                </th>
                <th
                  className={`sortable-th th-traffic up ${sortField === "rxBytes" ? "sorted" : ""}`}
                  onClick={() => handleSort("rxBytes")}
                  title={`点击按累计上传流量${sortField === "rxBytes" && sortDirection === "desc" ? "升序" : "降序"}排序`}
                >
                  <div className="th-content">
                    <span>累计上传</span>
                    <span className="sort-icon">{sortField === "rxBytes" ? (sortDirection === "asc" ? "▲" : "▼") : "↕"}</span>
                  </div>
                </th>
                <th>MAC 地址</th>
                <th>接口</th>
                <th>来源</th>
                <th>状态</th>
                <th>地址分配</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {filteredClients.map((client) => {
                const busy = pendingMac === client.MAC;
                const editing = editingMac === client.MAC;
                const isDownActive = (client.TxBps || 0) > 1000;
                const isUpActive = (client.RxBps || 0) > 1000;
                const isDownZero = (client.TxBps || 0) === 0;
                const isUpZero = (client.RxBps || 0) === 0;
                const isTxBytesZero = (client.TxBytes || 0) === 0;
                const isRxBytesZero = (client.RxBytes || 0) === 0;
                return (
                  <Fragment key={`${client.MAC}-${client.IP}`}>
                    <tr className={editing ? "client-row editing" : "client-row"}>
                      <td data-label="设备">
                        <div className="device-cell">
                          <span>
                            {(client.Hostname || "?").slice(0, 2).toUpperCase()}
                          </span>
                          <div>
                            <strong>{client.Hostname || "未知设备"}</strong>
                          </div>
                        </div>
                      </td>
                      <td data-label="当前 IP" className="mono">{client.IP}</td>
                      <td data-label="实时下载">
                        <span
                          className={`live-rate-cell down ${isDownActive ? "active" : ""} ${isDownZero ? "zero" : ""}`}
                          title="局域网下载速率 (路由器发给此设备)"
                        >
                          <span className="dir-arrow">↓</span>
                          {rate(client.TxBps || 0)}
                        </span>
                      </td>
                      <td data-label="实时上传">
                        <span
                          className={`live-rate-cell up ${isUpActive ? "active" : ""} ${isUpZero ? "zero" : ""}`}
                          title="局域网上传速率 (此设备发给路由器)"
                        >
                          <span className="dir-arrow">↑</span>
                          {rate(client.RxBps || 0)}
                        </span>
                      </td>
                      <td data-label="累计下载">
                        <span
                          className={`live-traffic-cell down ${isTxBytesZero ? "zero" : ""}`}
                          title="局域网累计下载流量"
                        >
                          <span className="dir-arrow">↓</span>
                          {bytes(client.TxBytes || 0)}
                        </span>
                      </td>
                      <td data-label="累计上传">
                        <span
                          className={`live-traffic-cell up ${isRxBytesZero ? "zero" : ""}`}
                          title="局域网累计上传流量"
                        >
                          <span className="dir-arrow">↑</span>
                          {bytes(client.RxBytes || 0)}
                        </span>
                      </td>
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
                            onClick={() => beginNameEdit(client)}
                            disabled={busy}
                          >
                            改名
                          </button>
                          <button
                            type="button"
                            onClick={() => beginLeaseEdit(client)}
                            disabled={busy}
                          >
                            {client.StaticLease ? "修改固定" : "设置固定"}
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
                        <td colSpan={12}>
                          <form
                            className={`static-lease-editor ${editMode === "name" ? "name-editor" : ""}`}
                            onSubmit={(event) => {
                              event.preventDefault();
                              if (editMode === "name") {
                                void saveClientName(client.MAC);
                              } else {
                                void saveStaticLease(client.MAC);
                              }
                            }}
                          >
                            <div>
                              <strong>{editMode === "name" ? "修改设备名称" : "固定设备地址"}</strong>
                              <small>
                                {editMode === "name"
                                  ? "名称独立保存，IP 仍可保持 DHCP 动态分配"
                                  : `为 ${client.Hostname || client.MAC} 保留一个 LAN 地址`}
                              </small>
                            </div>
                            {editMode === "name" ? (
                              <label>
                                <span>设备名称</span>
                                <input
                                  value={clientName}
                                  onChange={(event) => {
                                    setClientName(event.target.value);
                                    setError("");
                                  }}
                                  placeholder="例如 客厅平板"
                                  maxLength={64}
                                  autoComplete="off"
                                  spellCheck={false}
                                  aria-invalid={Boolean(error)}
                                  disabled={busy}
                                  autoFocus
                                />
                              </label>
                            ) : (
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
                            )}
                            <div className="static-lease-actions">
                              <button type="submit" disabled={busy}>
                                {busy ? "正在应用…" : editMode === "name" ? "保存名称" : "应用"}
                              </button>
                              {editMode === "name" && client.CustomName && (
                                <button
                                  type="button"
                                  onClick={() => void clearClientName(client)}
                                  disabled={busy}
                                >
                                  恢复自动名称
                                </button>
                              )}
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
        {filteredClients.length === 0 && (
          <p className="empty-copy">
            {searchQuery
              ? `未找到与 “${searchQuery}” 匹配的客户端设备。`
              : "当前 eth1 局域网没有有效 DHCP 租约或在线邻居。"}
          </p>
        )}
          </article>
          <p className="safe-note">
            列表只统计 LAN 网口 eth1 / 192.168.2.0/24，已排除光猫上联与路由器自身地址。设备流量是其通过软路由转发的上下行流量。
          </p>
        </>
      )}
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
  section,
  onNavigateSection,
}: {
  data: Snapshot;
  refresh: (quiet?: boolean) => Promise<void>;
  setNotice: (value: string) => void;
  section: SectionId;
  onNavigateSection: (section: SectionId) => void;
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
  const [newSubAutoUpdate, setNewSubAutoUpdate] = useState(true);
  const [newSubInterval, setNewSubInterval] = useState(6);
  const [viewingSubscription, setViewingSubscription] = useState<SubscriptionItem | null>(null);
  const [subscriptionNodes, setSubscriptionNodes] = useState<SubscriptionNodeInfo[]>([]);
  const [subscriptionNodesError, setSubscriptionNodesError] = useState("");
  const [editingSubscription, setEditingSubscription] = useState<SubscriptionItem | null>(null);
  const [editSubscriptionName, setEditSubscriptionName] = useState("");
  const [editSubscriptionUrl, setEditSubscriptionUrl] = useState("");
  const [editAutoUpdateEnabled, setEditAutoUpdateEnabled] = useState(true);
  const [editAutoUpdateHours, setEditAutoUpdateHours] = useState(6);
  const [highlightAddCard, setHighlightAddCard] = useState(false);
  const [busy, setBusy] = useState("");
  const addFormRef = useRef<HTMLFormElement | null>(null);
  const addNameInputRef = useRef<HTMLInputElement | null>(null);
  const addUrlInputRef = useRef<HTMLInputElement | null>(null);
  const [speedTestJob, setSpeedTestJob] = useState<MihomoSpeedTestJob | null>(null);
  const [speedTestNow, setSpeedTestNow] = useState<number>(0);
  const [speedTestBusy, setSpeedTestBusy] = useState<string | null>(null);
  const [speedTestError, setSpeedTestError] = useState<string | null>(null);
  const [speedTestApplying, setSpeedTestApplying] = useState<string | null>(null);
  const [speedTestSuccessMsg, setSpeedTestSuccessMsg] = useState<string | null>(null);
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
      speedTestResponse,
    ] = await Promise.all([
      fetch("/api/mihomo/status", { cache: "no-store" }),
      fetch("/api/mihomo/subscriptions", { cache: "no-store" }),
      fetch("/api/mihomo/settings", { cache: "no-store" }),
      fetch("/api/mihomo/proxies", { cache: "no-store" }),
      fetch("/api/mihomo/whole-home-proxy", { cache: "no-store" }),
      fetch("/api/mihomo/speedtest", { cache: "no-store" }),
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
    if (speedTestResponse.ok) {
      const st = (await speedTestResponse.json()) as MihomoSpeedTestJob;
      setSpeedTestJob(st.id ? st : null);
      setSpeedTestNow(Date.now());
    }
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
      fetch("/api/mihomo/speedtest", {
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
          speedTestResponse,
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
          if (speedTestResponse.ok) {
            const st = (await speedTestResponse.json()) as MihomoSpeedTestJob;
            setSpeedTestJob(st.id ? st : null);
            setSpeedTestNow(Date.now());
          }
        },
      )
      .catch(() => undefined);
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (section !== "speedtest" || speedTestJob?.state !== "running") return;
    const timer = setInterval(async () => {
      if (document.hidden) return;
      try {
        const res = await fetch("/api/mihomo/speedtest", { cache: "no-store" });
        if (res.ok) {
          const data = (await res.json()) as MihomoSpeedTestJob;
          setSpeedTestJob(data.id ? data : null);
          setSpeedTestNow(Date.now());
          if (data.state !== "running") {
            setSpeedTestBusy(null);
          }
        }
      } catch {
        // ignore polling error
      }
    }, 1000);
    return () => clearInterval(timer);
  }, [section, speedTestJob?.state]);

  useEffect(() => {
    let stopped = false;
    let timer = 0;
    const poll = async () => {
      if (document.hidden) return;
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
      if (!stopped && !document.hidden) {
        timer = window.setTimeout(poll, 3000);
      }
    };
    const onVisibilityChange = () => {
      clearTimeout(timer);
      if (!document.hidden && !stopped) void poll();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    timer = window.setTimeout(poll, 500);
    return () => {
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibilityChange);
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

  const phaseLabels: Record<string, string> = {
    idle: "空闲",
    discovering: "发现节点与测延迟",
    sampling_wan: "检测背景流量",
    preliminary: "初筛下载测速",
    confirming: "深度复测中",
    completed: "测速完成",
    cancelled: "已取消",
    failed: "测速失败",
    interrupted: "测速中断",
  };

  const getVideoBadgeClass = (badge: string) => {
    switch (badge) {
      case "4K candidate":
        return "badge-tag video-4k";
      case "1440p60":
        return "badge-tag video-1440p60";
      case "1440p":
        return "badge-tag video-1440p";
      case "1080p":
        return "badge-tag video-1080p";
      default:
        return "badge-tag video-unstable";
    }
  };

  const resolveConcreteNode = useCallback(
    (initialTarget: string, groupsList: MihomoProxyGroup[]): string => {
      let curr = initialTarget;
      const visited = new Set<string>();
      while (curr && visited.size < 16) {
        if (visited.has(curr)) break;
        visited.add(curr);
        const grp = groupsList.find((g) => g.name === curr);
        if (!grp || !grp.now) {
          return curr;
        }
        curr = grp.now;
      }
      return curr;
    },
    [],
  );

  const startSpeedTest = async (mode: "quick" | "deep", allowBusyNetwork = true) => {
    setSpeedTestBusy(mode);
    setSpeedTestError(null);
    setSpeedTestSuccessMsg(null);
    try {
      const response = await fetch("/api/mihomo/speedtest", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode, allowBusyNetwork: true }),
      });
      const data = (await response.json()) as MihomoSpeedTestJob & { error?: string };
      if (!response.ok) {
        setSpeedTestError(data.error || "启动测速失败");
      } else {
        setSpeedTestJob(data);
        setSpeedTestNow(Date.now());
        setNotice(mode === "quick" ? "快速真实测速已启动" : "深度真实测速已启动");
      }
    } catch (err) {
      setSpeedTestError(err instanceof Error ? err.message : "请求失败");
    } finally {
      setSpeedTestBusy(null);
    }
  };

  const cancelSpeedTest = async () => {
    if (speedTestBusy === "cancelling") return;
    setSpeedTestBusy("cancelling");
    setSpeedTestError(null);
    try {
      const response = await fetch("/api/mihomo/speedtest", { method: "DELETE" });
      const data = (await response.json()) as { error?: string; ok?: boolean };
      if (!response.ok) {
        setSpeedTestError(data.error || "取消测速失败");
        setSpeedTestBusy(null);
      } else {
        setNotice("正在停止测速并恢复网络配置…");
        const pollRes = await fetch("/api/mihomo/speedtest", { cache: "no-store" });
        if (pollRes.ok) {
          const pollData = (await pollRes.json()) as MihomoSpeedTestJob;
          setSpeedTestJob(pollData.id ? pollData : null);
          if (pollData.state !== "running") {
            setSpeedTestBusy(null);
            setNotice("测速已取消");
          }
        }
      }
    } catch (err) {
      setSpeedTestError(err instanceof Error ? err.message : "取消失败");
      setSpeedTestBusy(null);
    }
  };

  const applySpeedTestNode = async (node: string) => {
    if (!speedTestJob) return;
    setSpeedTestApplying(node);
    setSpeedTestError(null);
    setSpeedTestSuccessMsg(null);
    try {
      const response = await fetch("/api/mihomo/speedtest/apply", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ jobId: speedTestJob.id, node }),
      });
      const data = (await response.json()) as { error?: string; ok?: boolean };
      if (!response.ok) {
        setSpeedTestError(data.error || "应用节点失败");
      } else {
        setSpeedTestSuccessMsg(
          `已将家庭出口切换为 [${node}]。长连接应用（如正在播放的 YouTube 视频）可能需要刷新网页或重连以生效。`,
        );
        setNotice(`家庭代理已切换至“${node}”`);
        await loadMihomo();
      }
    } catch (err) {
      setSpeedTestError(err instanceof Error ? err.message : "应用失败");
    } finally {
      setSpeedTestApplying(null);
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

  const subscriptionAction = async (
    item: SubscriptionItem,
    action: "activate" | "update",
  ) => {
    setBusy(`${action}-${item.id}`);
    try {
      const response = await fetch("/api/mihomo/subscriptions", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          id: item.id,
          action,
        }),
      });
      if (!response.ok)
        throw new Error(
          await responseError(response, action === "activate" ? "启用订阅失败" : "更新订阅失败"),
        );
      const nextSubscriptions = (await response.json()) as SubscriptionList;
      setSubscriptions(nextSubscriptions);
      setNotice(
        action === "activate"
          ? `已启用订阅“${item.name}”`
          : item.active
            ? "当前订阅已更新并重新加载"
            : `订阅“${item.name}”已更新，当前订阅未切换`,
      );
      finishLater();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "订阅操作失败");
    } finally {
      setBusy("");
    }
  };

  const viewSubscriptionNodes = async (item: SubscriptionItem) => {
    setViewingSubscription(item);
    setSubscriptionNodes([]);
    setSubscriptionNodesError("");
    setBusy(`nodes-${item.id}`);
    try {
      const response = await fetch(
        `/api/mihomo/subscriptions/nodes?id=${encodeURIComponent(item.id)}`,
        { cache: "no-store" },
      );
      const body = (await response.json()) as {
        nodes?: SubscriptionNodeInfo[];
        error?: string;
      };
      if (!response.ok) throw new Error(body.error || "读取订阅节点失败");
      setSubscriptionNodes(body.nodes || []);
    } catch (error) {
      setSubscriptionNodesError(error instanceof Error ? error.message : "读取订阅节点失败");
    } finally {
      setBusy("");
    }
  };

  const openEditSubscription = (item: SubscriptionItem) => {
    setEditingSubscription(item);
    setEditSubscriptionName(item.name);
    setEditSubscriptionUrl("");
    setEditAutoUpdateEnabled(item.autoUpdateEnabled !== false);
    setEditAutoUpdateHours(item.autoUpdateHours || 6);
  };

  const saveEditedSubscription = async (event: FormEvent) => {
    event.preventDefault();
    if (!editingSubscription) return;
    setBusy(`edit-${editingSubscription.id}`);
    try {
      const response = await fetch("/api/mihomo/subscriptions", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          id: editingSubscription.id,
          action: "edit",
          name: editSubscriptionName.trim(),
          url: editSubscriptionUrl.trim(),
          autoUpdateEnabled: editAutoUpdateEnabled,
          autoUpdateHours: editAutoUpdateHours,
        }),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "保存订阅信息失败"));
      setSubscriptions((await response.json()) as SubscriptionList);
      setEditingSubscription(null);
      setNotice("订阅信息与更新设置已保存");
      finishLater();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "保存订阅信息失败");
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
          autoUpdateEnabled: newSubAutoUpdate,
          autoUpdateHours: newSubInterval,
        }),
      });
      if (!response.ok)
        throw new Error(await responseError(response, "添加订阅失败"));
      const nextSubscriptions = (await response.json()) as SubscriptionList;
      setSubscriptions(nextSubscriptions);
      setSubscriptionName("");
      setSubscriptionUrl("");
      setNewSubAutoUpdate(true);
      setNewSubInterval(6);
      setNotice(
        nextSubscriptions.items.length === 1
          ? "订阅已添加、更新并启用"
          : "订阅已添加并更新，当前订阅未切换",
      );
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

  useEffect(() => {
    if (!highlightAddCard) return;
    const timer = window.setTimeout(() => setHighlightAddCard(false), 1800);
    return () => clearTimeout(timer);
  }, [highlightAddCard]);

  const jumpToAddSubscription = (focusInput = true) => {
    onNavigateSection("subscriptions");
    setHighlightAddCard(true);
    window.setTimeout(() => {
      addFormRef.current?.scrollIntoView({ behavior: "smooth", block: "center" });
      if (focusInput) {
        const target = subscriptionName.trim()
          ? addUrlInputRef.current
          : addNameInputRef.current;
        target?.focus();
      }
    }, 80);
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
      <ModuleContextBar>
        <strong>{status.online && running ? "Mihomo 运行中" : "Mihomo 未就绪"}</strong>
        <span>{modes.find((mode) => mode.id === status.mode)?.label || "未知模式"}</span>
        <span>
          <button
            type="button"
            className="context-sub-jump"
            title="点击前往订阅管理 / 添加新订阅"
            onClick={() => jumpToAddSubscription(!subscriptions.items.length)}
          >
            {(() => {
              const activeItem = subscriptions.items.find((item) => item.active);
              if (!activeItem) return "未启用订阅 · 点击添加";
              const policyText =
                activeItem.autoUpdateEnabled !== false
                  ? `${activeItem.autoUpdateHours || 6}h 自动更新`
                  : "仅手动更新";
              return `${activeItem.name} (${policyText})`;
            })()}
          </button>
        </span>
        <span>{status.connections} 条连接</span>
      </ModuleContextBar>
      {section === "overview" && (
        <>
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
            className="mihomo-action-sub"
            onClick={() => jumpToAddSubscription(true)}
          >
            ＋ 添加 / 管理订阅 ({subscriptions.items.length})
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
        </>
      )}

      {section === "policies" && (
        <article className="panel mihomo-section section-page-first">
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
            {busy === "delay" ? "测延迟中…" : "测延迟"}
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
      )}

      {section === "speedtest" && (
        <article className="panel mihomo-section speedtest-panel section-page-first">
        <div className="section-heading small">
          <div>
            <p className="eyebrow">测速与推荐</p>
            <h2>真实体验测速</h2>
          </div>
          {speedTestJob?.state === "running" && (
            <span className="speedtest-phase-tag running">
              ● {speedTestBusy === "cancelling" ? "正在取消…" : (phaseLabels[speedTestJob.phase] || speedTestJob.phase)}
            </span>
          )}
        </div>

        <p className="speedtest-desc">
          在手机路由器本机直接向 Google 测速源发起真实下载测速，不经过电脑 Wi-Fi 链路、不切换家庭当前节点。
          测速会消耗代理流量并与家庭上网共享带宽，建议空闲时运行。
        </p>

        <div className="speedtest-actions">
          {speedTestJob?.state === "running" ? (
            <button
              type="button"
              className="danger"
              disabled={speedTestBusy === "cancelling"}
              onClick={cancelSpeedTest}
            >
              {speedTestBusy === "cancelling" ? "停止中…" : "停止测速"}
            </button>
          ) : (
            <>
              <button
                type="button"
                className="primary"
                disabled={!status.online || Boolean(busy) || Boolean(speedTestBusy)}
                onClick={() => startSpeedTest("quick")}
              >
                {speedTestBusy === "quick" ? "启动中…" : "⚡ 快速测速"}
              </button>
              <button
                type="button"
                className="deep"
                disabled={!status.online || Boolean(busy) || Boolean(speedTestBusy)}
                onClick={() => startSpeedTest("deep")}
              >
                {speedTestBusy === "deep" ? "启动中…" : "🔍 深度测速"}
              </button>
            </>
          )}
          <span
            className="speedtest-budget-hint"
            title="测速安全保护边界：总流量上限、单节点流量上限与整任务时间上限"
          >
            保护边界：总流量（快速 100 MiB / 深度 300 MiB）· 单节点（快速 25 MiB / 深度 35 MiB）· 超时（快速 10 分钟 / 深度 30 分钟）
          </span>
        </div>

        {(() => {
          const displayError =
            speedTestError ||
            (speedTestJob?.state === "failed" ? speedTestJob.errorMessage || "测速任务执行失败" : null);

          if (!displayError) return null;
          return (
            <div className="speedtest-notice error">
              <span>{displayError}</span>
            </div>
          );
        })()}

        {speedTestJob?.state === "interrupted" && (
          <div className="speedtest-notice warning">
            <span>⚠️ 上次测速因核心重启或系统原因中断，已保留中断前完成的节点数据。</span>
          </div>
        )}

        {speedTestSuccessMsg && (
          <div className="speedtest-notice success">
            <span>{speedTestSuccessMsg}</span>
          </div>
        )}

        {speedTestJob && speedTestJob.state !== "idle" && (
          <div className="speedtest-progress-box">
            <div className="speedtest-progress-header">
              <span className={`speedtest-phase-tag ${speedTestJob.state === "running" ? "running" : ""}`}>
                {phaseLabels[speedTestJob.phase] || speedTestJob.phase}
              </span>
              {speedTestJob.currentNode && speedTestJob.state === "running" && (
                <span style={{ fontSize: "10px", color: "#85dce3" }}>
                  当前测试: <strong>{speedTestJob.currentNode}</strong>
                </span>
              )}
            </div>

            <div className="speedtest-bar-track">
              <div
                className="speedtest-bar-fill"
                style={{
                  width: `${
                    speedTestJob.state === "completed"
                      ? 100
                      : speedTestJob.totalRuns > 0
                      ? Math.min(100, Math.round((speedTestJob.completedRuns / speedTestJob.totalRuns) * 100))
                      : 0
                  }%`,
                }}
              />
            </div>

            <div className="speedtest-progress-metrics">
              <div className="speedtest-metric">
                <span>测试轮次</span>
                <strong>
                  {speedTestJob.state === "completed"
                    ? `${speedTestJob.completedRuns} / ${speedTestJob.completedRuns}`
                    : `${speedTestJob.completedRuns} / ${speedTestJob.totalRuns || "-"}`}
                </strong>
              </div>
              <div className="speedtest-metric">
                <span>消耗流量</span>
                <strong>
                  {(speedTestJob.bytesUsed / (1024 * 1024)).toFixed(1)} MB /{" "}
                  {(speedTestJob.dataBudgetBytes / (1024 * 1024)).toFixed(0)} MB
                </strong>
              </div>
              <div className="speedtest-metric">
                <span>测试模式</span>
                <strong>{speedTestJob.mode === "deep" ? "深度测速" : "快速测速"}</strong>
              </div>
              <div className="speedtest-metric">
                <span>已用时间</span>
                <strong>
                  {(() => {
                    if (!speedTestJob.startedAt) return "-";
                    const start = new Date(speedTestJob.startedAt).getTime();
                    if (isNaN(start)) return "-";
                    const end = speedTestJob.finishedAt
                      ? new Date(speedTestJob.finishedAt).getTime()
                      : speedTestNow > 0
                      ? speedTestNow
                      : start;
                    const sec = Math.max(0, Math.floor((end - start) / 1000));
                    const m = Math.floor(sec / 60);
                    const s = sec % 60;
                    return `${m}分${s < 10 ? "0" : ""}${s}秒`;
                  })()}
                </strong>
              </div>
              {speedTestJob.recommendedNode && (
                <div className="speedtest-metric">
                  <span>推荐优胜</span>
                  <strong style={{ color: "#5bf0bf" }}>{speedTestJob.recommendedNode}</strong>
                </div>
              )}
            </div>
          </div>
        )}

        {speedTestJob && speedTestJob.results && speedTestJob.results.length > 0 && (
          <div className="speedtest-results-card">
            <div className="speedtest-table-wrap">
              <table className="speedtest-table">
                <thead>
                  <tr>
                    <th>推荐/状态</th>
                    <th>节点名称</th>
                    <th>协议</th>
                    <th>延迟</th>
                    <th>复测表现 (Mbps)</th>
                    <th>最低保障</th>
                    <th>综合体验分</th>
                    <th>体验评级</th>
                    <th style={{ textAlign: "right" }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {speedTestJob.results.map((r) => {
                    const currentProductionNode = resolveConcreteNode("家庭RULE", groups);
                    const isWinner = speedTestJob.recommendedNode === r.node;
                    const isCurrent = r.node === currentProductionNode;
                    const prelimRun = r.runs?.find((run) => run.stage === "preliminary");
                    const confirmRuns = r.runs?.filter((run) => run.stage === "confirmation") || [];

                    return (
                      <tr
                        key={r.node}
                        className={`${isWinner ? "winner-row" : ""} ${isCurrent ? "current-row" : ""}`}
                      >
                        <td>
                          {isWinner && <span className="badge-tag winner">★ 推荐出口</span>}
                          {isCurrent && <span className="badge-tag current">当前家庭</span>}
                          {!isWinner && !isCurrent && !r.eligible && (
                            <span style={{ color: "#6c8896", fontSize: "8px" }}>
                              {r.errorCode || (r.runs?.length === 0 ? "未测试" : "未达标")}
                            </span>
                          )}
                        </td>
                        <td>
                          <strong style={{ color: isWinner ? "#6bf0ff" : "#e0f0f5" }}>{r.node}</strong>
                        </td>
                        <td>
                          <span style={{ color: "#7493a3", fontSize: "9px" }}>{r.protocol || "-"}</span>
                        </td>
                        <td>{r.delayMs > 0 ? `${r.delayMs} ms` : "-"}</td>
                        <td>
                          {confirmRuns.length > 0 ? (
                            confirmRuns.map((cr, idx) => (
                              <span key={idx} style={{ marginRight: "6px" }}>
                                {cr.success ? `${cr.mbps.toFixed(1)}` : "失败"}
                              </span>
                            ))
                          ) : prelimRun ? (
                            <span style={{ color: "#7c9baa" }}>初筛 {prelimRun.mbps.toFixed(1)}</span>
                          ) : (
                            "-"
                          )}
                        </td>
                        <td>
                          <strong style={{ color: r.minimumMbps >= 8 ? "#94ecd5" : "#e59095" }}>
                            {r.minimumMbps > 0 ? `${r.minimumMbps.toFixed(1)} M` : "-"}
                          </strong>
                        </td>
                        <td>
                          <strong style={{ color: "#9ee6f5" }}>
                            {r.effectiveMbps > 0 ? `${r.effectiveMbps.toFixed(1)} M` : "-"}
                          </strong>
                        </td>
                        <td>
                          <span className={getVideoBadgeClass(r.videoBadge)}>{r.videoBadge}</span>
                        </td>
                        <td style={{ textAlign: "right" }}>
                          {isCurrent ? (
                            <span className="btn-speedtest-apply active-current">✓ 生效中</span>
                          ) : (
                            <button
                              type="button"
                              className="btn-speedtest-apply"
                              disabled={
                                !r.eligible ||
                                speedTestJob.state !== "completed" ||
                                Boolean(speedTestApplying)
                              }
                              onClick={() => applySpeedTestNode(r.node)}
                            >
                              {speedTestApplying === r.node ? "应用中…" : "应用"}
                            </button>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
        )}
        </article>
      )}

      {section === "settings" && (
        <>
      <article className="panel mihomo-section section-page-first">
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

      <article className="panel mihomo-section subscription-relocated-card">
        <div>
          <p className="eyebrow">配置来源</p>
          <h3>订阅管理与添加新订阅</h3>
          <p>
            当前已配置 {subscriptions.items.length} 个订阅（
            {subscriptions.items.filter((i) => i.autoUpdateEnabled !== false).length}{" "}
            个开启定时自动更新）。订阅管理已独立至顶部「订阅管理」标签页，方便快速添加与切换。
          </p>
        </div>
        <button
          type="button"
          className="btn-jump-add-sub"
          onClick={() => jumpToAddSubscription(true)}
        >
          ＋ 前往添加 / 管理订阅 →
        </button>
      </article>
        </>
      )}

      {section === "subscriptions" && (
        <article className="panel mihomo-section section-page-first">
          <div className="section-heading small subscription-section-heading">
            <div>
              <p className="eyebrow">配置来源 · 独立更新策略</p>
              <h2>订阅管理</h2>
            </div>
            <div className="subscription-heading-actions">
              <span className="count-badge">
                {subscriptions.items.length} 个订阅
                {subscriptions.items.length > 0 &&
                  ` · ${
                    subscriptions.items.filter((i) => i.autoUpdateEnabled !== false)
                      .length
                  } 个开启自动更新`}
              </span>
              <button
                type="button"
                className="btn-jump-add-sub"
                onClick={() => jumpToAddSubscription(true)}
              >
                ＋ 添加新订阅
              </button>
            </div>
          </div>

          {subscriptions.items.length > 0 ? (
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
                      <span
                        className={`subscription-autoupdate-badge ${
                          item.autoUpdateEnabled !== false ? "enabled" : "disabled"
                        }`}
                      >
                        {item.autoUpdateEnabled !== false
                          ? `⟳ 自动更新 · 每 ${item.autoUpdateHours || 6} 小时`
                          : "⏸ 自动更新已关闭 · 仅手动"}
                      </span>
                    </div>
                    <span>{item.source}</span>
                    <small>
                      最近更新：
                      {item.updatedAt
                        ? new Date(item.updatedAt).toLocaleString("zh-CN")
                        : "尚未更新"}
                      {item.autoUpdateEnabled !== false
                        ? item.nextAutoUpdateAt
                          ? ` · 下次自动更新：${new Date(
                              item.nextAutoUpdateAt,
                            ).toLocaleString("zh-CN")}`
                          : ` · 周期 ${item.autoUpdateHours || 6} 小时`
                        : " · 不自动更新"}
                      {item.cached ? ` · ${item.nodeCount} 个节点` : " · 需要更新"}
                    </small>
                  </div>
                  <div className="subscription-card-actions">
                    {!item.active && (
                      <button
                        type="button"
                        disabled={Boolean(busy) || !item.cached}
                        onClick={() => subscriptionAction(item, "activate")}
                      >
                        {busy === `activate-${item.id}` ? "启用中…" : "启用"}
                      </button>
                    )}
                    <button
                      type="button"
                      disabled={Boolean(busy)}
                      onClick={() => subscriptionAction(item, "update")}
                    >
                      {busy === `update-${item.id}` ? "更新中…" : "更新"}
                    </button>
                    <button
                      type="button"
                      disabled={Boolean(busy) || !item.cached}
                      onClick={() => viewSubscriptionNodes(item)}
                    >
                      {busy === `nodes-${item.id}` ? "读取中…" : "节点"}
                    </button>
                    <button
                      type="button"
                      disabled={Boolean(busy)}
                      onClick={() => openEditSubscription(item)}
                    >
                      编辑
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
          ) : (
            <div className="subscription-empty">
              <strong>还没有任何订阅</strong>
              <span>
                在下方「添加新订阅」卡片中粘贴订阅链接，系统会自动下载并校验节点；首个订阅将自动启用。
              </span>
            </div>
          )}

          <form
            ref={addFormRef}
            className={`subscription-add-card ${highlightAddCard ? "highlight" : ""}`}
            onSubmit={addSubscription}
          >
            <div className="subscription-add-header">
              <div className="subscription-add-title-group">
                <span className="subscription-add-icon" aria-hidden="true">
                  ＋
                </span>
                <div>
                  <h3>添加新订阅</h3>
                  <p>
                    支持 Clash / Mihomo 订阅链接，每个订阅可独立设置自动更新或仅手动更新
                  </p>
                </div>
              </div>
              <span className="subscription-privacy-pill">
                URL 仅存手机本机
              </span>
            </div>

            <div className="subscription-add-fields">
              <label className="subscription-field">
                <span className="subscription-field-label">
                  订阅名称 <em>选填</em>
                </span>
                <input
                  ref={addNameInputRef}
                  value={subscriptionName}
                  onChange={(event) => setSubscriptionName(event.target.value)}
                  placeholder="例如：主力机场 / 备用节点"
                  maxLength={128}
                  autoComplete="off"
                />
              </label>
              <label className="subscription-field">
                <span className="subscription-field-label">
                  订阅链接 URL <em className="required">必填</em>
                </span>
                <input
                  ref={addUrlInputRef}
                  type="url"
                  value={subscriptionUrl}
                  onChange={(event) => setSubscriptionUrl(event.target.value)}
                  placeholder="https://example.com/api/v1/client/subscribe?token=..."
                  autoComplete="off"
                />
              </label>
            </div>

            <div className="subscription-add-footer">
              <div className="subscription-policy-control">
                <span className="subscription-policy-label">此订阅更新策略</span>
                <div
                  className="subscription-mode-switch"
                  role="group"
                  aria-label="新订阅更新策略"
                >
                  <button
                    type="button"
                    className={newSubAutoUpdate ? "active auto" : ""}
                    onClick={() => setNewSubAutoUpdate(true)}
                  >
                    ⟳ 定时自动更新
                  </button>
                  <button
                    type="button"
                    className={!newSubAutoUpdate ? "active manual" : ""}
                    onClick={() => setNewSubAutoUpdate(false)}
                  >
                    ⏸ 仅手动更新
                  </button>
                </div>
                {newSubAutoUpdate ? (
                  <label className="subscription-interval-picker">
                    <span>更新间隔</span>
                    <select
                      value={newSubInterval}
                      onChange={(event) =>
                        setNewSubInterval(Number(event.target.value))
                      }
                    >
                      <option value={1}>每 1 小时</option>
                      <option value={2}>每 2 小时</option>
                      <option value={4}>每 4 小时</option>
                      <option value={6}>每 6 小时（推荐）</option>
                      <option value={12}>每 12 小时</option>
                      <option value={24}>每 24 小时 (1 天)</option>
                      <option value={48}>每 48 小时 (2 天)</option>
                      <option value={168}>每 168 小时 (1 周)</option>
                    </select>
                  </label>
                ) : (
                  <span className="subscription-manual-note">
                    仅添加时拉取一次节点，后台不自动请求（适合阅后即焚 / 备用订阅）
                  </span>
                )}
              </div>

              <button
                className="subscription-submit-btn"
                type="submit"
                disabled={Boolean(busy)}
              >
                {busy === "add" ? "正在下载并校验…" : "＋ 添加订阅并获取节点"}
              </button>
            </div>
          </form>

          <p className="safe-note">
            每个订阅均可独立配置自动更新周期（点击任意订阅卡片右侧「编辑」可随时修改）；关闭自动更新后仅在手动点击「更新」时请求订阅源。完整 URL 只保存在手机本机，页面仅回显脱敏地址。
          </p>
        </article>
      )}
      {viewingSubscription && (
        <div className="modal-backdrop" onClick={() => setViewingSubscription(null)}>
          <section
            className="log-modal subscription-nodes-modal"
            onClick={(event) => event.stopPropagation()}
          >
            <header>
              <div>
                <strong>{viewingSubscription.name}</strong>
                <p className="modal-header-desc">
                  {subscriptionNodes.length} 个节点 ·{" "}
                  {viewingSubscription.autoUpdateEnabled !== false
                    ? `自动更新（每 ${viewingSubscription.autoUpdateHours || 6} 小时）`
                    : "自动更新已关闭（仅手动）"}
                </p>
              </div>
              <button type="button" onClick={() => setViewingSubscription(null)}>
                关闭
              </button>
            </header>
            {subscriptionNodesError ? (
              <div className="subscription-node-error">{subscriptionNodesError}</div>
            ) : subscriptionNodes.length === 0 ? (
              <div className="subscription-node-empty">正在读取节点…</div>
            ) : (
              <div className="subscription-node-table-wrap">
                <table className="subscription-node-table">
                  <thead>
                    <tr>
                      <th>节点</th>
                      <th>协议</th>
                      <th>服务器</th>
                      <th>端口</th>
                    </tr>
                  </thead>
                  <tbody>
                    {subscriptionNodes.map((node, index) => (
                      <tr key={`${node.name}-${index}`}>
                        <td>{node.name}</td>
                        <td>{node.type || "—"}</td>
                        <td className="mono">{node.server || "—"}</td>
                        <td>{node.port || "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </div>
      )}
      {editingSubscription && (
        <div className="modal-backdrop" onClick={() => setEditingSubscription(null)}>
          <section
            className="log-modal subscription-edit-modal"
            onClick={(event) => event.stopPropagation()}
          >
            <header>
              <div>
                <strong>编辑订阅 · {editingSubscription.name}</strong>
                <p className="modal-header-desc">单独调整此订阅的名称、链接或自动更新策略</p>
              </div>
              <button type="button" onClick={() => setEditingSubscription(null)}>
                关闭
              </button>
            </header>
            <form onSubmit={saveEditedSubscription}>
              <label className="subscription-field">
                <span className="subscription-field-label">订阅名称</span>
                <input
                  value={editSubscriptionName}
                  maxLength={128}
                  onChange={(event) => setEditSubscriptionName(event.target.value)}
                />
              </label>
              <label className="subscription-field">
                <span className="subscription-field-label">
                  替换订阅 URL <em>留空保持原链接不变</em>
                </span>
                <input
                  type="url"
                  value={editSubscriptionUrl}
                  placeholder={`${editingSubscription.source}（留空保持不变）`}
                  autoComplete="off"
                  onChange={(event) => setEditSubscriptionUrl(event.target.value)}
                />
              </label>
              <div className="subscription-edit-section">
                <div className="subscription-edit-policy-row">
                  <span className="subscription-policy-label">此订阅更新策略</span>
                  <div
                    className="subscription-mode-switch"
                    role="group"
                    aria-label="编辑订阅更新策略"
                  >
                    <button
                      type="button"
                      className={editAutoUpdateEnabled ? "active auto" : ""}
                      onClick={() => setEditAutoUpdateEnabled(true)}
                    >
                      ⟳ 定时自动更新
                    </button>
                    <button
                      type="button"
                      className={!editAutoUpdateEnabled ? "active manual" : ""}
                      onClick={() => setEditAutoUpdateEnabled(false)}
                    >
                      ⏸ 仅手动更新
                    </button>
                  </div>
                </div>
                {editAutoUpdateEnabled && (
                  <label className="subscription-interval-picker">
                    <span>更新间隔</span>
                    <select
                      value={editAutoUpdateHours}
                      onChange={(event) =>
                        setEditAutoUpdateHours(Number(event.target.value))
                      }
                    >
                      <option value={1}>每 1 小时</option>
                      <option value={2}>每 2 小时</option>
                      <option value={4}>每 4 小时</option>
                      <option value={6}>每 6 小时（推荐）</option>
                      <option value={12}>每 12 小时</option>
                      <option value={24}>每 24 小时 (1 天)</option>
                      <option value={48}>每 48 小时 (2 天)</option>
                      <option value={168}>每 168 小时 (1 周)</option>
                    </select>
                  </label>
                )}
                <p className="subscription-edit-tip">
                  {editAutoUpdateEnabled
                    ? `开启后，后台将每 ${editAutoUpdateHours} 小时自动拉取该订阅最新节点并持久化缓存。`
                    : "已关闭自动更新，此订阅仅在手动点击「更新」时请求订阅源（适合阅后即焚链接）。"}
                </p>
              </div>
              <div className="subscription-edit-actions">
                <button
                  type="button"
                  className="btn-modal-secondary"
                  onClick={() => setEditingSubscription(null)}
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="btn-modal-primary"
                  disabled={Boolean(busy)}
                >
                  {busy === `edit-${editingSubscription.id}` ? "保存中…" : "保存设置"}
                </button>
              </div>
            </form>
          </section>
        </div>
      )}
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
      if (document.hidden) return;
      try {
        await load();
      } finally {
        if (!stopped && !document.hidden) {
          timer = window.setTimeout(poll, 10000);
        }
      }
    };
    const onVisibilityChange = () => {
      clearTimeout(timer);
      if (!document.hidden && !stopped) void poll();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibilityChange);
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
  section,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  refresh: (quiet?: boolean) => Promise<void>;
  setNotice: (value: string) => void;
  onLog: (name: string) => void;
  section: SectionId;
}) {
  const [upgradingService, setUpgradingService] = useState<Snapshot["services"][number] | null>(null);
  const [upgradeProcessing, setUpgradeProcessing] = useState(false);
  const [upgradeStatusMsg, setUpgradeStatusMsg] = useState("");
  const [upgradeError, setUpgradeError] = useState("");

  const [checkingUpdates, setCheckingUpdates] = useState(false);

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

  const handleCheckUpdates = async () => {
    setCheckingUpdates(true);
    try {
      const res = await fetch("/api/services/updates?refresh=1");
      if (res.ok) {
        setNotice("服务更新版本信息已刷新");
        await refresh(true);
      } else {
        setNotice("检查更新失败");
      }
    } catch (error: unknown) {
      setNotice(`检查更新异常: ${error instanceof Error ? error.message : String(error)}`);
    } finally {
      setCheckingUpdates(false);
    }
  };

  const handleUpgrade = async (service: Snapshot["services"][number]) => {
    if (!service) return;
    setUpgradeProcessing(true);
    setUpgradeError("");
    setUpgradeStatusMsg("正在拉取发布资产并校验哈希完整性...");

    const t1 = setTimeout(() => {
      setUpgradeStatusMsg("正在执行沙箱隔离语法与兼容性预检 (Preflight)...");
    }, 2800);
    const t2 = setTimeout(() => {
      setUpgradeStatusMsg("预检通过，正在全量快照备份现网二进制并原子替换...");
    }, 6000);
    const t3 = setTimeout(() => {
      setUpgradeStatusMsg("服务已启动，正在执行连续 12 秒多频健康探测与防故障监控...");
    }, 9000);

    try {
      const res = await fetch("/api/services/upgrade", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          service: service.Name,
          version: service.latestVersion,
        }),
      });
      clearTimeout(t1);
      clearTimeout(t2);
      clearTimeout(t3);

      const json = await res.json();
      if (!res.ok || !json.ok) {
        const errMsg = json.error || "升级未完成";
        if (json.rolledBack) {
          setUpgradeError(`升级异常（健康探测未通过），系统已自动秒级回滚至原版本: ${errMsg}`);
        } else {
          setUpgradeError(errMsg);
        }
        setNotice(errMsg);
      } else {
        setNotice(json.message || `${service.Label} 升级成功！`);
        setUpgradingService(null);
      }
    } catch (error: unknown) {
      clearTimeout(t1);
      clearTimeout(t2);
      clearTimeout(t3);
      const message = error instanceof Error ? error.message : String(error);
      setUpgradeError(`网络请求异常: ${message}`);
      setNotice(`升级失败: ${message}`);
    } finally {
      setUpgradeProcessing(false);
      await refresh(true);
    }
  };

  const open = (service: Snapshot["services"][number]) => {
    if (!service.webUrl) {
      setNotice("该服务没有网页管理入口");
      return;
    }
    let path = "";
    if (service.Name === "cpa") {
      path = "/management.html";
    } else if (service.Name === "rclone") {
      path = "/#/dashboard";
    }
    window.open(
      `${location.protocol}//${location.hostname}${service.webUrl}${path}`,
      "_blank",
      "noopener",
    );
  };

  const directUpgradeCount = data.services.filter(
    (service) => service.upgradable && service.canUpgrade,
  ).length;
  const rebuildCount = data.services.filter(
    (service) => service.upgradable && !service.canUpgrade,
  ).length;

  return (
    <div className="view">
      <ModuleContextBar>
        <strong>
          {data.services.filter((service) => service.State === "运行中").length} / {data.services.length} 项运行中
        </strong>
        <span>Runit 自动守护</span>
        {directUpgradeCount > 0 ? (
          <span style={{ color: "#36d6a8", fontWeight: 600 }}>
            {directUpgradeCount} 个服务可直接升级
          </span>
        ) : rebuildCount > 0 ? (
          <span>{rebuildCount} 个定制服务上游有更新</span>
        ) : null}
        <span>{data.aria2.online ? `Aria2 ${data.aria2.numActive} 项活动` : "Aria2 离线"}</span>
      </ModuleContextBar>
      {section === "tailscale" && <TailscaleService setNotice={setNotice} />}
      {section === "aria2" && <Aria2Monitor data={data} history={history} />}
      {section === "all" && (
        <>
          <div className="section-heading">
            <div>
              <p className="eyebrow">Runit 托管 · 升级与自动回滚保障</p>
              <h2>应用与系统服务</h2>
            </div>
            <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
              <button
                type="button"
                className="service-check-btn"
                disabled={checkingUpdates}
                onClick={handleCheckUpdates}
                title="立即联网检查 GitHub 及源更新"
              >
                {checkingUpdates ? "检查中..." : "⟳ 检查更新"}
              </button>
              <span className="live-pill">
                <i /> 自动守护
              </span>
            </div>
          </div>
          <div className="service-cards">
            {data.services.map((service, index) => (
              <article className="panel service-card" key={service.Name}>
                <div className="service-main">
                  <span className={`service-icon s${index % 4}`}>
                    {service.Label.slice(0, 2)}
                  </span>
                  <div>
                    <div style={{ display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                      <strong>{service.Label}</strong>
                      {!service.customBuild && service.currentVersion && (
                        <span className="service-version-tag current" title={`当前运行版本: ${service.currentVersion}`}>
                          当前: {service.currentVersion.startsWith("v") ? service.currentVersion : `v${service.currentVersion}`}
                        </span>
                      )}
                      {service.customBuild && service.baseVersion && (
                        <span className="service-version-tag current" title={`设备定制构建，基于上游 ${service.baseVersion}`}>
                          自构建 · 基于：{service.baseVersion.startsWith("v") ? service.baseVersion : `v${service.baseVersion}`}
                        </span>
                      )}
                      {service.upgradable && service.latestVersion && (
                        <span className="service-version-tag upgradable" title={`可升级至: ${service.latestVersion}`}>
                          发现新版: {service.latestVersion.startsWith("v") ? service.latestVersion : `v${service.latestVersion}`}
                        </span>
                      )}
                    </div>
                    <small>
                      {service.Port ? `TCP ${service.Port}` : "后台服务"} ·{" "}
                      {service.PID ? `PID ${service.PID}` : "无进程"}
                    </small>
                    {!service.canUpgrade && service.upgradable && service.upgradeHint && (
                      <small className="service-rebuild-note" title={service.upgradeHint}>
                        上游有更新 · 定制版本需重新构建
                      </small>
                    )}
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
                  {service.canUpgrade && service.upgradable && (
                    <button
                      type="button"
                      className="service-upgrade-btn"
                      title={`升级至 ${service.latestVersion}（带沙箱预检与自动回滚保护）`}
                      onClick={() => {
                        setUpgradeError("");
                        setUpgradeStatusMsg("");
                        setUpgradingService(service);
                      }}
                    >
                      ✨ 升级到 {service.latestVersion ? (service.latestVersion.startsWith("v") ? service.latestVersion : `v${service.latestVersion}`) : "新版"}
                    </button>
                  )}
                  {[
                    "openlist",
                    "rclone",
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
        </>
      )}

      {/* Upgrade Modal with Rollback Guarantee */}
      {upgradingService && (
        <div
          className="modal-backdrop"
          onClick={() => !upgradeProcessing && setUpgradingService(null)}
        >
          <section
            className="log-modal service-upgrade-modal"
            onClick={(e) => e.stopPropagation()}
          >
            <header>
              <div>
                <strong>升级服务 · {upgradingService.Label}</strong>
                <p className="modal-header-desc">
                  版本跃升：{upgradingService.currentVersion || "当前版本"} → {upgradingService.latestVersion || "最新版本"}
                </p>
              </div>
              <button
                type="button"
                disabled={upgradeProcessing}
                onClick={() => setUpgradingService(null)}
              >
                关闭
              </button>
            </header>

            <div className="upgrade-modal-body">
              <div className="rollback-safety-card">
                <div className="safety-title">
                  <span className="safety-icon">🛡️</span>
                  <strong>多重容灾防线与全自动无损回滚机制</strong>
                </div>
                <ul className="safety-steps">
                  <li>
                    <strong>1. 隔离沙箱预检：</strong>
                    新版本下载后，后台先在沙箱隔离区执行完整语法与运行环境校验（如 <code>mihomo -t</code>、<code>openlist version</code>），若预检异常立即终止，绝不影响现网正常服务。
                  </li>
                  <li>
                    <strong>2. 二进制全量快照备份：</strong>
                    在替换前自动将当前稳定版本复制至持久化备份区（<code>$DATA/backups/services/{upgradingService.Name}/</code>），记录版本哈希与元数据。
                  </li>
                  <li>
                    <strong>3. 连续 12 秒多频健康探测与自动秒级回滚：</strong>
                    服务启动后执行深度健康检查与端口连通性探测。若启动失败或健康检查超时，<strong>系统将立即自动无损回滚至原版本并拉起服务</strong>，避免服务瘫痪。
                  </li>
                  <li>
                    <strong>4. 故障自动回滚自愈：</strong>
                    升级成功后服务平稳接管，无需任何人工干预；若拉起异常或探针未通过，系统将全自动秒级回滚原版本自愈。
                  </li>
                </ul>
              </div>

              {upgradeProcessing && (
                <div className="upgrade-progress-box">
                  <div className="upgrade-spinner" />
                  <p className="upgrade-progress-text">{upgradeStatusMsg || "正在执行安全升级流程..."}</p>
                  <small>请勿关闭本页面或重启路由器</small>
                </div>
              )}

              {upgradeError && (
                <div className="upgrade-error-banner">
                  <strong>⚠️ 升级中断：</strong>
                  <span>{upgradeError}</span>
                </div>
              )}

              <div className="upgrade-actions-footer">
                <button
                  type="button"
                  className="secondary-btn"
                  disabled={upgradeProcessing}
                  onClick={() => setUpgradingService(null)}
                >
                  取消
                </button>
                <button
                  type="button"
                  className="primary-upgrade-btn"
                  disabled={upgradeProcessing}
                  onClick={() => handleUpgrade(upgradingService)}
                >
                  {upgradeProcessing ? "升级进行中..." : `立即升级到 ${upgradingService.latestVersion || "最新版本"}`}
                </button>
              </div>
            </div>
          </section>
        </div>
      )}
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

const logSources: { id: LogSource; label: string; description: string }[] = [
  { id: "events", label: "异常事件", description: "自动筛选错误与警告" },
  { id: "kernel", label: "内核", description: "USB、块设备与驱动" },
  { id: "storage", label: "存储", description: "挂载和掉盘监控" },
  { id: "network", label: "网络", description: "PPPoE、DNS 与路由" },
  { id: "aria2", label: "aria2", description: "下载进程日志" },
  { id: "openlist", label: "OpenList", description: "网盘与离线下载" },
  { id: "rclone", label: "Rclone", description: "云存储与同步服务" },
  { id: "admin", label: "管理后台", description: "Router Admin 自身" },
];

function SystemLogs() {
  const [source, setSource] = useState<LogSource>("events");
  const [data, setData] = useState<SystemLogResponse | null>(null);
  const [level, setLevel] = useState<"all" | "error" | "warning" | "info">(
    "all",
  );
  const [query, setQuery] = useState("");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [loading, setLoading] = useState(false);
  const [failure, setFailure] = useState("");

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true);
    try {
      const response = await fetch(
        `/api/system-logs?source=${encodeURIComponent(source)}&limit=500`,
        { cache: "no-store" },
      );
      const body = (await response.json()) as SystemLogResponse & {
        error?: string;
      };
      if (!response.ok) throw new Error(body.error || "日志读取失败");
      setData(body);
      setFailure("");
    } catch (error) {
      setFailure(error instanceof Error ? error.message : "日志读取失败");
    } finally {
      if (!quiet) setLoading(false);
    }
  }, [source]);

  useEffect(() => {
    const initial = window.setTimeout(() => void load(), 0);
    if (!autoRefresh) return () => window.clearTimeout(initial);
    const timer = window.setInterval(() => {
      if (!document.hidden) void load(true);
    }, 5000);
    return () => {
      window.clearTimeout(initial);
      window.clearInterval(timer);
    };
  }, [autoRefresh, load]);

  const needle = query.trim().toLowerCase();
  const entries = (data?.entries || []).filter(
    (entry) =>
      (level === "all" || entry.level === level) &&
      (!needle ||
        entry.message.toLowerCase().includes(needle) ||
        entry.source.toLowerCase().includes(needle) ||
        entry.time.toLowerCase().includes(needle)),
  );

  return (
    <div className="view log-view">
      <div className="section-heading log-heading">
        <div>
          <p className="eyebrow">只读诊断 · 类 OpenWrt 日志</p>
          <h2>系统日志与异常事件</h2>
        </div>
        <div className="log-heading-actions">
          <label>
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(event) => setAutoRefresh(event.target.checked)}
            />
            5 秒自动刷新
          </label>
          <button type="button" disabled={loading} onClick={() => void load()}>
            {loading ? "读取中…" : "立即刷新"}
          </button>
        </div>
      </div>

      <div className="log-source-grid">
        {logSources.map((item) => (
          <button
            type="button"
            key={item.id}
            className={source === item.id ? "active" : ""}
            onClick={() => {
              setSource(item.id);
              setLevel("all");
            }}
          >
            <strong>{item.label}</strong>
            <small>{item.description}</small>
          </button>
        ))}
      </div>

      <article className="panel log-console">
        <header className="log-console-toolbar">
          <div className="log-counters" aria-label="日志等级统计">
            <button
              type="button"
              className={`log-count error ${level === "error" ? "active" : ""}`}
              onClick={() => setLevel(level === "error" ? "all" : "error")}
            >
              错误 <strong>{data?.error || 0}</strong>
            </button>
            <button
              type="button"
              className={`log-count warning ${level === "warning" ? "active" : ""}`}
              onClick={() =>
                setLevel(level === "warning" ? "all" : "warning")
              }
            >
              警告 <strong>{data?.warning || 0}</strong>
            </button>
            <button
              type="button"
              className={`log-count info ${level === "info" ? "active" : ""}`}
              onClick={() => setLevel(level === "info" ? "all" : "info")}
            >
              信息 <strong>{data?.info || 0}</strong>
            </button>
          </div>
          <input
            className="log-search"
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索 I/O error、USB、aria2…"
            aria-label="搜索日志"
          />
        </header>

        {failure ? (
          <div className="log-empty error">{failure}</div>
        ) : entries.length === 0 ? (
          <div className="log-empty">
            {loading ? "正在读取日志…" : "当前筛选条件下没有日志"}
          </div>
        ) : (
          <div className="log-table" role="log" aria-live="polite">
            {entries.map((entry, index) => (
              <div
                className={`log-row ${entry.level}`}
                key={`${entry.time}-${entry.source}-${index}`}
              >
                <time>{entry.time || "时间未知"}</time>
                <span className={`log-level ${entry.level}`}>
                  {entry.level === "error"
                    ? "错误"
                    : entry.level === "warning"
                      ? "警告"
                      : "信息"}
                </span>
                <span className="log-source">{entry.source}</span>
                <code>{entry.message}</code>
              </div>
            ))}
          </div>
        )}
        <footer className="log-console-footer">
          <span>显示 {entries.length} 条</span>
          <span>
            {data?.updatedAt
              ? `更新于 ${new Date(data.updatedAt).toLocaleTimeString()}`
              : "尚未更新"}
          </span>
          <span>URL 查询参数与敏感字段已自动隐藏</span>
        </footer>
      </article>
      <p className="safe-note">
        “异常事件”会汇总内核和服务日志中的 I/O 错误、USB
        断连、挂载失败、超时及下载错误。内核日志从本次开机开始计算，重启后会重新开始。
      </p>
    </div>
  );
}

function BatteryGuardSettings({
  guardLow,
  guardHigh,
  onRefresh,
  userLow,
  userHigh,
  onUserLowChange,
  onUserHighChange,
}: {
  guardLow?: number;
  guardHigh?: number;
  onRefresh?: () => void | Promise<void>;
  userLow: string | null;
  userHigh: string | null;
  onUserLowChange: (value: string | null) => void;
  onUserHighChange: (value: string | null) => void;
}) {
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
    onUserLowChange(String(low));
    onUserHighChange(String(high));
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
          onUserLowChange(null);
          onUserHighChange(null);
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
                  onUserLowChange(e.target.value);
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
                  onUserHighChange(e.target.value);
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
  section,
}: {
  data: Snapshot;
  history: HistoryPoint[];
  onRefresh?: () => void | Promise<void>;
  section: SectionId;
}) {
  const [guardDraft, setGuardDraft] = useState<{
    low: string | null;
    high: string | null;
  }>({ low: null, high: null });
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
      <ModuleContextBar>
        <strong>{data.power.batteryPercent}% 电量</strong>
        <span>{data.power.usbOnline ? `${data.power.usbType} 供电在线` : "未接入 USB 电源"}</span>
        <span>电池 {batteryTemperature.toFixed(1)}°C</span>
        <span>CPU {data.cpu.usage.toFixed(1)}%</span>
        <span>CPU 最高 {data.cpu.temperature.toFixed(1)}°C</span>
      </ModuleContextBar>
      <div className="system-grid single-section-grid">
        {section === "power" && (
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
            userLow={guardDraft.low}
            userHigh={guardDraft.high}
            onUserLowChange={(low) =>
              setGuardDraft((old) => ({ ...old, low }))
            }
            onUserHighChange={(high) =>
              setGuardDraft((old) => ({ ...old, high }))
            }
          />
        </article>
        )}
        {section === "performance" && (
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
        )}
      </div>
      {section === "power" && (
        <div className="system-grid power-history-grid">
        <article className="panel history-panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">最近 2 小时</p>
              <h2>电池温度趋势</h2>
            </div>
            <span
              className={batteryTemperature < 40 ? "state-good" : "warning-chip"}
            >
              {batteryTemperature < 40
                ? "温度正常"
                : batteryTemperature < 45
                  ? "温度偏高"
                  : "温度过高"}
            </span>
          </div>
          <TimeSeriesChart
            history={history}
            windowMs={2 * 60 * 60 * 1000}
            series={[
              {
                label: "电池 °C",
                color: "#52d395",
                value: (p) => p.batteryTemperature,
              },
            ]}
            format={(value) => `${value.toFixed(1)}°C`}
            zeroBaseline={false}
          />
        </article>
        <article className="panel history-panel">
          <div className="panel-title">
            <div>
              <p className="eyebrow">最近 2 小时</p>
              <h2>电池电流趋势（正值为放电）</h2>
            </div>
          </div>
          <TimeSeriesChart
            history={history}
            windowMs={2 * 60 * 60 * 1000}
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
              <p className="eyebrow">最近 2 小时</p>
              <h2>电池与 USB 输入电压</h2>
            </div>
          </div>
          <TimeSeriesChart
            history={history}
            windowMs={2 * 60 * 60 * 1000}
            series={[
              { label: "电池 V", color: "#52d395", value: (p) => p.batteryVoltage },
              { label: "USB 输入 V", color: "#30c7d8", value: (p) => p.usbVoltage },
            ]}
            format={(value) => `${value.toFixed(3)} V`}
            zeroBaseline={false}
          />
        </article>
        </div>
      )}
      {section === "performance" && (
        <>
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
        </>
      )}
    </div>
  );
}
