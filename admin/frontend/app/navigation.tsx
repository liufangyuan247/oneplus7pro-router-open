"use client";

import { ReactNode, useEffect } from "react";

export type Tab =
  | "overview"
  | "network"
  | "clients"
  | "services"
  | "mihomo"
  | "storage"
  | "logs"
  | "system"
  | "remote";

export type RemoteMode = "terminal" | "screen";

export type SectionId =
  | "status"
  | "config"
  | "traffic"
  | "devices"
  | "all"
  | "tailscale"
  | "aria2"
  | "overview"
  | "policies"
  | "speedtest"
  | "settings"
  | "power"
  | "performance"
  | "terminal"
  | "screen";

export const navItems: { id: Tab; label: string; icon: string }[] = [
  { id: "overview", label: "总览", icon: "OV" },
  { id: "network", label: "网络", icon: "NET" },
  { id: "clients", label: "客户端", icon: "LAN" },
  { id: "services", label: "服务", icon: "SVC" },
  { id: "mihomo", label: "代理", icon: "VPN" },
  { id: "storage", label: "存储", icon: "SSD" },
  { id: "logs", label: "日志", icon: "LOG" },
  { id: "system", label: "系统", icon: "SYS" },
  { id: "remote", label: "远程", icon: "WEB" },
];

export const secondaryNavItems: Partial<
  Record<Tab, { id: SectionId; label: string }[]>
> = {
  network: [
    { id: "status", label: "运行状态" },
    { id: "config", label: "接口配置" },
  ],
  clients: [
    { id: "traffic", label: "流量概览" },
    { id: "devices", label: "设备管理" },
  ],
  services: [
    { id: "all", label: "全部服务" },
    { id: "tailscale", label: "Tailscale" },
    { id: "aria2", label: "Aria2" },
  ],
  mihomo: [
    { id: "overview", label: "运行概览" },
    { id: "policies", label: "策略与节点" },
    { id: "speedtest", label: "体验测速" },
    { id: "traffic", label: "流量审计" },
    { id: "settings", label: "设置" },
  ],
  system: [
    { id: "power", label: "供电与电池" },
    { id: "performance", label: "性能与温度" },
  ],
  remote: [
    { id: "terminal", label: "终端" },
    { id: "screen", label: "屏幕" },
  ],
};

export const isTab = (value: string | null): value is Tab =>
  navItems.some((item) => item.id === value);

export const resolveSection = (
  view: Tab,
  requested?: string | null,
): SectionId | undefined => {
  const items = secondaryNavItems[view];
  if (!items?.length) return undefined;
  return items.find((item) => item.id === requested)?.id || items[0].id;
};

export function SecondaryNav({
  view,
  active,
  onSelect,
}: {
  view: Tab;
  active: SectionId;
  onSelect: (section: SectionId) => void;
}) {
  const items = secondaryNavItems[view] || [];
  const viewLabel = navItems.find((item) => item.id === view)?.label || "当前模块";
  useEffect(() => {
    document
      .getElementById(`secondary-${view}-${active}`)
      ?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [active, view]);

  return (
    <nav
      className="secondary-nav"
      role="tablist"
      aria-label={`${viewLabel}二级导航`}
      onKeyDown={(event) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key))
          return;
        event.preventDefault();
        const buttons = Array.from(
          event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
        );
        const current = Math.max(
          0,
          buttons.indexOf(document.activeElement as HTMLButtonElement),
        );
        const next =
          event.key === "Home"
            ? 0
            : event.key === "End"
              ? buttons.length - 1
              : (current +
                  (event.key === "ArrowRight" ? 1 : -1) +
                  buttons.length) %
                buttons.length;
        buttons[next]?.focus();
      }}
    >
      {items.map((item) => (
        <button
          key={item.id}
          id={`secondary-${view}-${item.id}`}
          type="button"
          role="tab"
          aria-selected={item.id === active}
          aria-controls={`secondary-panel-${view}`}
          tabIndex={item.id === active ? 0 : -1}
          onClick={() => onSelect(item.id)}
        >
          {item.label}
        </button>
      ))}
    </nav>
  );
}

export function ModuleContextBar({ children }: { children: ReactNode }) {
  return (
    <div className="module-context" aria-label="模块当前状态">
      {children}
    </div>
  );
}
