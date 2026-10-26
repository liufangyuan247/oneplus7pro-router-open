"use client";

import { useEffect, useState } from "react";

type Relay = { enabled: boolean; path: string; name: string; cached: boolean };

async function copyText(text: string) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try { await navigator.clipboard.writeText(text); return; } catch { /* LAN fallback */ }
  }
  const input = document.createElement("textarea");
  input.value = text;
  input.style.position = "fixed";
  input.style.opacity = "0";
  document.body.appendChild(input);
  input.select();
  const copied = document.execCommand("copy");
  input.remove();
  if (!copied) throw new Error("请选中下方地址手动复制");
}

export function SubscriptionRelay({ id, name, onClose, onChanged }: {
  id: string; name: string; onClose: () => void; onChanged: () => void;
}) {
  const [relay, setRelay] = useState<Relay | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [origin, setOrigin] = useState("");
  const [qrVersion, setQRVersion] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    fetch(`/api/mihomo/subscriptions/relay?id=${encodeURIComponent(id)}`, { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        const body = await response.json();
        if (!response.ok) throw new Error(body.error || "无法读取中转设置");
        setOrigin(window.location.origin);
        setRelay(body);
      }).catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "无法读取中转设置"); });
    return () => controller.abort();
  }, [id]);
  useEffect(() => {
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape" && !busy) onClose(); };
    document.addEventListener("keydown", escape);
    return () => document.removeEventListener("keydown", escape);
  }, [busy, onClose]);

  const action = async (value: "enable" | "disable" | "rotate") => {
    if (value === "rotate" && !window.confirm("重新生成后，旧地址和二维码立即失效，已导入的设备需要重新添加。继续吗？")) return;
    setBusy(true); setError(""); setMessage("");
    try {
      const response = await fetch("/api/mihomo/subscriptions/relay", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id, action: value }),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(body.error || "保存中转设置失败");
      setRelay(body); setQRVersion((previous) => previous + 1); onChanged();
      setMessage(value === "disable" ? "中转已关闭，旧地址已失效" : value === "rotate" ? "已生成新地址，请重新添加到其他设备" : "已开启中转，没有请求原始订阅链接");
    } catch (cause) { setError(cause instanceof Error ? cause.message : "保存中转设置失败"); }
    finally { setBusy(false); }
  };
  const url = relay?.path ? origin + relay.path : "";
  const qr = `/api/mihomo/subscriptions/relay?id=${encodeURIComponent(id)}&qr=1&scheme=${origin.startsWith("https:") ? "https" : "http"}&v=${qrVersion}`;

  return (
    <div className="modal-backdrop" onClick={() => { if (!busy) onClose(); }}>
      <section className="log-modal subscription-relay-modal" role="dialog" aria-modal="true" aria-labelledby="subscription-relay-title" onClick={(event) => event.stopPropagation()}>
        <header>
          <div><strong id="subscription-relay-title">{name} · 订阅中转</strong><p className="modal-header-desc">给其他 Clash / Mihomo / Clash Verge 设备使用</p></div>
          <button type="button" disabled={busy} onClick={onClose}>关闭</button>
        </header>
        <div className="subscription-relay-content">
          <p className="subscription-relay-note">直接返回路由器保存的原始订阅文件，完整保留节点、分组、规则和 DNS；订阅名称为「{name}（中转）」。中转不访问原始订阅链接，不占用机场链接的拉取次数，也不受原始链接一小时有效期影响。</p>
          {error && <p className="subscription-node-error" role="alert">{error}</p>}
          {message && <p className="subscription-relay-message" role="status">{message}</p>}
          {!relay && !error && <p>正在读取中转设置…</p>}
          {relay?.enabled ? <>
            {!relay.cached && <p className="subscription-node-error">当前节点缓存不可用，其他设备暂时无法拉取；中转不会自动请求原始订阅。</p>}
            <div className="subscription-relay-share">
              {/* QR is generated on this router; no subscription URL goes to a third party. */}
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img className="subscription-relay-qr" src={qr} alt={`${name}的订阅地址二维码`} width={256} height={256} onError={() => setError("二维码加载失败，可复制下方地址添加")} />
              <div className="subscription-relay-instructions">
                <h3>扫码或复制地址添加</h3>
                <p>在客户端的「订阅 / 配置」中新增远程订阅，粘贴下方 URL；支持扫码导入的客户端可直接扫描二维码。</p>
                <p>设备需能访问当前路由器地址。外出使用时，请先通过 Tailscale 连接路由器，再用 Tailscale 地址打开此页面复制订阅。</p>
              </div>
            </div>
            <label className="subscription-field"><span className="subscription-field-label">订阅地址</span><input className="subscription-relay-url" value={url} readOnly onFocus={(event) => event.target.select()} aria-label="中转订阅地址" /></label>
            <div className="subscription-relay-actions">
              <button type="button" className="btn-primary" onClick={async () => { try { await copyText(url); setMessage("订阅地址已复制"); } catch (cause) { setError(cause instanceof Error ? cause.message : "复制失败"); } }}>复制订阅 URL</button>
              <button type="button" disabled={busy} onClick={() => action("rotate")}>重新生成地址</button>
              <button type="button" className="danger-quiet" disabled={busy} onClick={() => action("disable")}>关闭中转</button>
            </div>
            <p className="safe-note">持有此地址即可读取节点，请只给自己的设备使用。路由器获取到新节点后，此地址会自动提供最新缓存；中转本身不会更新机场订阅。</p>
          </> : relay && <>
            <p>{relay.cached ? "当前已有节点缓存，开启后即可供其他设备订阅。" : "当前没有可用节点缓存，开启中转不会自动拉取机场订阅。"}</p>
            <button type="button" className="btn-primary" disabled={busy || !relay.cached} onClick={() => action("enable")}>{busy ? "正在开启…" : "开启订阅中转"}</button>
          </>}
        </div>
      </section>
    </div>
  );
}
