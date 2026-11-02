"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

export interface PairdropSettingsProps {
  onClose: () => void;
  onSaved?: (notice: string) => void;
  demo?: boolean;
}

type ServerSettings = {
  retentionSeconds: number;
  maxFileBytes: number;
  maxTotalBytes: number;
  maxFiles: number;
  storedBytes: number;
};

function formatBytes(bytes: number): string {
  if (bytes === undefined || bytes === null || Number.isNaN(bytes)) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val.toFixed(val >= 10 ? 1 : 2)} ${units[i]}`;
}

function formatDurationSeconds(totalSec: number): string {
  if (totalSec < 60) return `${totalSec} 秒`;
  const minutes = Math.floor(totalSec / 60);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  if (remainingMinutes === 0) return `${hours} 小时`;
  return `${hours} 小时 ${remainingMinutes} 分钟`;
}

export function PairdropSettings({ onClose, onSaved, demo }: PairdropSettingsProps) {
  const [loading, setLoading] = useState(!demo);
  const [loadedSettings, setLoadedSettings] = useState(Boolean(demo));
  const [reload, setReload] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const [retentionHours, setRetentionHours] = useState("2");
  const [maxFileMiB, setMaxFileMiB] = useState("512");
  const [maxTotalMiB, setMaxTotalMiB] = useState("2048");

  const [storedBytes, setStoredBytes] = useState(0);
  const [maxFiles, setMaxFiles] = useState(1000);

  const dialogRef = useRef<HTMLElement>(null);
  const firstInputRef = useRef<HTMLInputElement>(null);
  const closeRef = useRef(onClose);
  const savingRef = useRef(saving);
  useEffect(() => {
    closeRef.current = onClose;
    savingRef.current = saving;
  }, [onClose, saving]);

  // Focus trap, initial focus, escape handling, and focus restore
  useEffect(() => {
    const previousActiveElement = document.activeElement as HTMLElement | null;

    // Initial focus after render
    const timer = setTimeout(() => {
      (firstInputRef.current || dialogRef.current?.querySelector<HTMLButtonElement>("button"))?.focus();
    }, 50);

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !savingRef.current) {
        closeRef.current();
        return;
      }
      if (e.key === "Tab" && dialogRef.current) {
        const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
          'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'
        );
        if (focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];

        if (e.shiftKey && document.activeElement === first) {
          last.focus();
          e.preventDefault();
        } else if (!e.shiftKey && document.activeElement === last) {
          first.focus();
          e.preventDefault();
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => {
      clearTimeout(timer);
      window.removeEventListener("keydown", handleKeyDown);
      previousActiveElement?.focus?.();
    };
  }, []);

  useEffect(() => {
    if (!loading && loadedSettings) firstInputRef.current?.focus();
  }, [loading, loadedSettings]);

  // Load current settings from API (preserve exact values, no unwanted rounding)
  useEffect(() => {
    if (demo) return;

    const controller = new AbortController();
    fetch("/api/pairdrop/settings", {
      cache: "no-store",
      signal: controller.signal,
    })
      .then(async (res) => {
        const body = await res.json().catch(() => ({}));
        if (!res.ok) {
          throw new Error(body.error || "无法获取设备传输站设置");
        }
        const data = body as ServerSettings;
        setRetentionHours((data.retentionSeconds / 3600).toString());
        // Preserve exact float/integer representation without silent rounding
        setMaxFileMiB((data.maxFileBytes / (1024 * 1024)).toString());
        setMaxTotalMiB((data.maxTotalBytes / (1024 * 1024)).toString());
        setStoredBytes(data.storedBytes);
        setMaxFiles(data.maxFiles);
        setLoadedSettings(true);
      })
      .catch((err) => {
        if (!controller.signal.aborted) {
          setError(err instanceof Error ? err.message : "读取传输站设置失败");
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });

    return () => controller.abort();
  }, [demo, reload]);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (saving || loading) return;

    if (demo) {
      setNotice("演示模式下已模拟更新设置");
      onSaved?.("设备传输站暂存设置已更新（演示模式）");
      return;
    }

    setError("");
    setNotice("");

    const numHours = Number(retentionHours);
    if (!Number.isFinite(numHours) || numHours <= 0) {
      setError("暂存保留时长必须为有效正数");
      return;
    }
    const retentionSeconds = Math.round(numHours * 3600);
    if (retentionSeconds < 60 || retentionSeconds > 604800) {
      setError("保留时长超出有效范围（1分钟至7天，即 0.017 至 168 小时）");
      return;
    }

    const numFileMiB = Number(maxFileMiB);
    if (!Number.isFinite(numFileMiB) || !Number.isSafeInteger(numFileMiB) || numFileMiB <= 0) {
      setError("单文件上限必须为有效正整数 (MiB)");
      return;
    }
    const maxFileBytes = numFileMiB * 1024 * 1024;
    if (maxFileBytes < 1 || maxFileBytes > 10737418240) {
      setError("单文件上限超出有效范围（1 MiB 至 10240 MiB）");
      return;
    }

    const numTotalMiB = Number(maxTotalMiB);
    if (!Number.isFinite(numTotalMiB) || !Number.isSafeInteger(numTotalMiB) || numTotalMiB <= 0) {
      setError("暂存总容量必须为有效正整数 (MiB)");
      return;
    }
    const maxTotalBytes = numTotalMiB * 1024 * 1024;
    if (maxTotalBytes < 1 || maxTotalBytes > 107374182400) {
      setError("暂存总容量超出有效范围（1 MiB 至 102400 MiB）");
      return;
    }

    if (maxFileBytes > maxTotalBytes) {
      setError("单文件上限不能大于暂存总容量");
      return;
    }

    setSaving(true);
    try {
      const res = await fetch("/api/pairdrop/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          retentionSeconds,
          maxFileBytes,
          maxTotalBytes,
        }),
      });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error(body.error || "保存传输站设置失败");
      }
      const data = body as ServerSettings;
      setRetentionHours((data.retentionSeconds / 3600).toString());
      setMaxFileMiB((data.maxFileBytes / (1024 * 1024)).toString());
      setMaxTotalMiB((data.maxTotalBytes / (1024 * 1024)).toString());
      setStoredBytes(data.storedBytes);
      setMaxFiles(data.maxFiles);

      setNotice("暂存配置已成功保存并即时生效");
      onSaved?.("设备传输站暂存设置已保存并生效");
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存设置异常");
    } finally {
      setSaving(false);
    }
  };

  const parsedHours = Number(retentionHours);
  const calculatedSec = Number.isFinite(parsedHours) && parsedHours > 0 ? Math.round(parsedHours * 3600) : 0;

  const parsedFile = Number(maxFileMiB);
  const fileHint = Number.isFinite(parsedFile) && parsedFile > 0
    ? parsedFile >= 1024
      ? `≈ ${(parsedFile / 1024).toFixed(1)} GiB`
      : `${parsedFile} MiB`
    : "";

  const parsedTotal = Number(maxTotalMiB);
  const totalHint = Number.isFinite(parsedTotal) && parsedTotal > 0
    ? parsedTotal >= 1024
      ? `≈ ${(parsedTotal / 1024).toFixed(1)} GiB`
      : `${parsedTotal} MiB`
    : "";

  return (
    <div
      className="modal-backdrop"
      onClick={() => {
        if (!saving) onClose();
      }}
    >
      <section
        ref={dialogRef}
        className="log-modal pairdrop-settings-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="pairdrop-settings-title"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="pairdrop-modal-header">
          <div>
            <strong id="pairdrop-settings-title">设备传输站 · 暂存设置</strong>
            <span className="pairdrop-header-badge">离线传输与文件箱</span>
          </div>
          <button
            type="button"
            className="pairdrop-btn-close"
            disabled={saving}
            onClick={onClose}
            aria-label="关闭设置窗口"
          >
            ✕
          </button>
        </header>

        <div className="pairdrop-settings-body">
          {loading && <p className="pairdrop-status-loading">正在读取服务端传输站配置…</p>}

          {error && (
            <div className="pairdrop-alert pairdrop-alert-error" role="alert">
              {error}
            </div>
          )}
          {!loading && !loadedSettings && (
            <button type="button" className="pairdrop-btn-secondary" onClick={() => {
              setLoading(true);
              setLoadedSettings(false);
              setError("");
              setReload(value => value + 1);
            }}>
              重新读取配置
            </button>
          )}

          {notice && (
            <div className="pairdrop-alert pairdrop-alert-success" role="status">
              {notice}
            </div>
          )}

          {!loading && loadedSettings && (
            <form onSubmit={handleSubmit} className="pairdrop-settings-form">
              <div className="pairdrop-stats-row">
                <div className="pairdrop-stat-card">
                  <small>暂存箱当前占用</small>
                  <strong>{formatBytes(storedBytes)}</strong>
                </div>
                <div className="pairdrop-stat-card">
                  <small>文件数容量上限</small>
                  <strong>{maxFiles.toLocaleString()} 个</strong>
                </div>
              </div>

              <div className="pairdrop-fields-grid">
                <label className="pairdrop-field-group">
                  <div className="pairdrop-label-row">
                    <span className="pairdrop-label-text">保留时长（小时）</span>
                    <span className="pairdrop-label-hint">
                      {calculatedSec >= 60 ? `≈ ${formatDurationSeconds(calculatedSec)}` : "过短"}
                    </span>
                  </div>
                  <input
                    ref={firstInputRef}
                    type="number"
                    step="any"
                    min="0.016"
                    max="168"
                    value={retentionHours}
                    disabled={saving}
                    onChange={(e) => setRetentionHours(e.target.value)}
                    required
                    aria-describedby="retention-desc"
                  />
                  <small id="retention-desc" className="pairdrop-hint">
                    默认 2 小时，支持小数（分钟级精度）；允许范围 1 分钟 ~ 7 天。
                  </small>
                </label>

                <label className="pairdrop-field-group">
                  <div className="pairdrop-label-row">
                    <span className="pairdrop-label-text">单文件上限 (MiB)</span>
                    {fileHint && <span className="pairdrop-label-hint">{fileHint}</span>}
                  </div>
                  <input
                    type="number"
                    step="1"
                    min="1"
                    max="10240"
                    value={maxFileMiB}
                    disabled={saving}
                    onChange={(e) => setMaxFileMiB(e.target.value)}
                    required
                    aria-describedby="maxfile-desc"
                  />
                  <small id="maxfile-desc" className="pairdrop-hint">
                    默认 512 MiB；单文件暂存上限，最大支持 10240 MiB (10 GiB)。
                  </small>
                </label>

                <label className="pairdrop-field-group">
                  <div className="pairdrop-label-row">
                    <span className="pairdrop-label-text">暂存总容量 (MiB)</span>
                    {totalHint && <span className="pairdrop-label-hint">{totalHint}</span>}
                  </div>
                  <input
                    type="number"
                    step="1"
                    min="1"
                    max="102400"
                    value={maxTotalMiB}
                    disabled={saving}
                    onChange={(e) => setMaxTotalMiB(e.target.value)}
                    required
                    aria-describedby="maxtotal-desc"
                  />
                  <small id="maxtotal-desc" className="pairdrop-hint">
                    默认 2048 MiB (2 GiB)；所有设备离线待收文件总上限，最大 100 GiB。
                  </small>
                </label>
              </div>

              <div className="pairdrop-note-box">
                <p>
                  <strong>清理与容量规则：</strong>
                </p>
                <ul>
                  <li>
                    <strong>保留时长即时生效：</strong>修改保留时长会重新计算所有暂存文件的到期时间，保存时会清理已到期文件。
                  </li>
                  <li>
                    <strong>缩减容量不删已有文件：</strong>调小暂存总容量不会强制销毁已存在的暂存文件，但空间降至新限额前将拒绝新的离线暂存。
                  </li>
                  <li>
                    <strong>仅管理路由器副本：</strong>清理仅针对保存在路由器文件箱中的离线中转副本，已下载保存到接收设备本地的文件不受任何影响。
                  </li>
                </ul>
              </div>

              <div className="pairdrop-actions">
                <button
                  type="button"
                  className="pairdrop-btn-secondary"
                  disabled={saving}
                  onClick={onClose}
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="pairdrop-btn-primary"
                  disabled={saving}
                >
                  {saving ? "正在保存…" : "保存设置"}
                </button>
              </div>
            </form>
          )}
        </div>

        <style jsx>{`
          .pairdrop-settings-modal {
            width: min(640px, calc(100vw - 32px));
            background: linear-gradient(180deg, #0e2433 0%, #081722 100%);
            border: 1px solid #1e4b63;
            border-radius: 16px;
            box-shadow: 0 24px 60px rgba(0, 0, 0, 0.65), 0 0 0 1px rgba(56, 189, 248, 0.2);
            overflow: hidden;
            display: flex;
            flex-direction: column;
            color: #dceaf2;
          }
          .pairdrop-modal-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 14px 20px;
            background: linear-gradient(90deg, #102e40 0%, #0d2535 100%);
            border-bottom: 1px solid #1c455c;
          }
          .pairdrop-modal-header strong {
            font-size: 14px;
            color: #f0f9ff;
            font-weight: 600;
          }
          .pairdrop-header-badge {
            display: inline-block;
            font-size: 11px;
            font-weight: 500;
            color: #38bdf8;
            background: rgba(56, 189, 248, 0.12);
            border: 1px solid rgba(56, 189, 248, 0.28);
            border-radius: 6px;
            padding: 2px 7px;
            margin-left: 10px;
          }
          .pairdrop-btn-close {
            background: transparent;
            border: none;
            color: #8da6b6;
            font-size: 16px;
            line-height: 1;
            padding: 6px 8px;
            cursor: pointer;
            border-radius: 6px;
            transition: color 0.15s, background 0.15s;
          }
          .pairdrop-btn-close:hover {
            color: #f0f9ff;
            background: rgba(255, 255, 255, 0.08);
          }
          .pairdrop-settings-body {
            padding: 20px;
            display: flex;
            flex-direction: column;
            gap: 16px;
            overflow-y: auto;
            max-height: calc(85vh - 65px);
          }
          .pairdrop-status-loading {
            margin: 20px 0;
            text-align: center;
            color: #7ba3b8;
            font-size: 13px;
          }
          .pairdrop-alert {
            padding: 10px 14px;
            border-radius: 8px;
            font-size: 12px;
            line-height: 1.5;
          }
          .pairdrop-alert-error {
            background: rgba(239, 68, 68, 0.12);
            border: 1px solid rgba(239, 68, 68, 0.35);
            color: #fca5a5;
          }
          .pairdrop-alert-success {
            background: rgba(16, 185, 129, 0.12);
            border: 1px solid rgba(16, 185, 129, 0.35);
            color: #6ee7b7;
          }
          .pairdrop-stats-row {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 12px;
          }
          .pairdrop-stat-card {
            background: rgba(12, 33, 47, 0.7);
            border: 1px solid #1a3e52;
            border-radius: 10px;
            padding: 10px 14px;
            display: flex;
            flex-direction: column;
            gap: 4px;
          }
          .pairdrop-stat-card small {
            color: #7294a6;
            font-size: 11px;
          }
          .pairdrop-stat-card strong {
            color: #e0f2fe;
            font-size: 15px;
            font-family: ui-monospace, monospace;
          }
          .pairdrop-settings-form {
            display: flex;
            flex-direction: column;
            gap: 16px;
          }
          .pairdrop-fields-grid {
            display: flex;
            flex-direction: column;
            gap: 14px;
          }
          .pairdrop-field-group {
            display: flex;
            flex-direction: column;
            gap: 6px;
          }
          .pairdrop-label-row {
            display: flex;
            justify-content: space-between;
            align-items: center;
          }
          .pairdrop-label-text {
            font-size: 12.5px;
            font-weight: 500;
            color: #e2eef5;
          }
          .pairdrop-label-hint {
            font-size: 11px;
            color: #38bdf8;
            font-family: ui-monospace, monospace;
          }
          .pairdrop-field-group input {
            background: #07151e;
            border: 1px solid #1d465c;
            border-radius: 8px;
            color: #f0f9ff;
            padding: 9px 12px;
            font-size: 13px;
            font-family: ui-monospace, monospace;
            outline: none;
            transition: border-color 0.15s, box-shadow 0.15s;
          }
          .pairdrop-field-group input:focus {
            border-color: #38bdf8;
            box-shadow: 0 0 0 2px rgba(56, 189, 248, 0.2);
          }
          .pairdrop-field-group input:disabled {
            opacity: 0.6;
            cursor: not-allowed;
          }
          .pairdrop-hint {
            color: #6e8e9f;
            font-size: 11px;
            line-height: 1.4;
          }
          .pairdrop-note-box {
            background: rgba(14, 42, 60, 0.55);
            border: 1px solid #1c455c;
            border-radius: 10px;
            padding: 12px 14px;
            font-size: 11.5px;
            line-height: 1.6;
            color: #9cbccc;
          }
          .pairdrop-note-box p {
            margin: 0 0 6px 0;
            color: #bae6fd;
          }
          .pairdrop-note-box ul {
            margin: 0;
            padding-left: 18px;
            display: flex;
            flex-direction: column;
            gap: 4px;
          }
          .pairdrop-note-box li strong {
            color: #e0f2fe;
          }
          .pairdrop-actions {
            display: flex;
            justify-content: flex-end;
            gap: 10px;
            padding-top: 8px;
            border-top: 1px solid #18384b;
          }
          .pairdrop-btn-secondary {
            background: #0e2635;
            border: 1px solid #1f4559;
            color: #a0b8c6;
            border-radius: 8px;
            padding: 8px 16px;
            font-size: 12.5px;
            cursor: pointer;
            transition: background 0.15s, color 0.15s;
          }
          .pairdrop-btn-secondary:hover:not(:disabled) {
            background: #143245;
            color: #f0f9ff;
          }
          .pairdrop-btn-primary {
            background: linear-gradient(180deg, #0284c7 0%, #0369a1 100%);
            border: 1px solid #38bdf8;
            color: #ffffff;
            border-radius: 8px;
            padding: 8px 20px;
            font-size: 12.5px;
            font-weight: 500;
            cursor: pointer;
            box-shadow: 0 2px 6px rgba(2, 132, 199, 0.35);
            transition: filter 0.15s, box-shadow 0.15s;
          }
          .pairdrop-btn-primary:hover:not(:disabled) {
            filter: brightness(1.1);
            box-shadow: 0 3px 10px rgba(2, 132, 199, 0.5);
          }
          .pairdrop-btn-primary:disabled,
          .pairdrop-btn-secondary:disabled {
            opacity: 0.5;
            cursor: not-allowed;
          }
          @media (max-width: 520px) {
            .pairdrop-stats-row {
              grid-template-columns: 1fr;
            }
          }
        `}</style>
      </section>
    </div>
  );
}
