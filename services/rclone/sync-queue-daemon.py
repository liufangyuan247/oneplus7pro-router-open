#!/usr/bin/env python3
# ==============================================================================
# 网盘照片后台自动队列管理器 (通过 Rclone RC API 顺序调度)
# 保证所有任务均在 Rclone WebUI (http://192.168.2.1:5572/#/dashboard) 实时可见
# ==============================================================================

import json
import os
import signal
import sys
import time
import urllib.error
import urllib.request

RC_URL = "http://127.0.0.1:5572"
LOG_DIR = "/data/data/com.termux/files/home/router-data/rclone/logs"
LOG_FILE = os.path.join(LOG_DIR, "sync_queue.log")

TASKS = [
    ("baidu:婚纱照", "quark:百度网盘备份/婚纱照"),
    ("baidu:来自：百度相册", "quark:百度网盘备份/来自：百度相册"),
    ("baidu:来自：iPhone", "quark:百度网盘备份/来自：iPhone"),
    ("baidu:来自：GM1910", "quark:百度网盘备份/来自：GM1910"),
    ("baidu:来自：PJA110", "quark:百度网盘备份/来自：PJA110"),
    ("baidu:来自：微信备份", "quark:百度网盘备份/来自：微信备份"),
    ("baidu:港澳行留念", "quark:百度网盘备份/港澳行留念"),
    ("baidu:flashes", "quark:百度网盘备份/flashes"),
]

running = True

def log(msg):
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    line = f"[{ts}] {msg}"
    print(line, flush=True)
    try:
        os.makedirs(LOG_DIR, exist_ok=True)
        with open(LOG_FILE, "a", encoding="utf-8") as f:
            f.write(line + "\n")
    except Exception as e:
        print(f"写入日志失败: {e}", file=sys.stderr)

def sig_handler(signum, frame):
    global running
    log(f"收到终止信号 ({signum})，正在安全退出队列调度器...")
    running = False

signal.signal(signal.SIGINT, sig_handler)
signal.signal(signal.SIGTERM, sig_handler)

def rc_call(endpoint, data=None):
    url = f"{RC_URL}/{endpoint}"
    payload = json.dumps(data or {}).encode("utf-8")
    req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as resp:
        return json.loads(resp.read().decode("utf-8"))

def get_job_status(jobid):
    try:
        return rc_call("job/status", {"jobid": jobid})
    except Exception as e:
        return {"error": str(e), "finished": False}

def get_running_jobs():
    try:
        res = rc_call("job/list")
        return res.get("runningIds", [])
    except Exception as e:
        return []

def main():
    global running
    log("=== Rclone 网盘同步队列调度守护程序启动 ===")

    active_ids = get_running_jobs()
    log(f"当前 Rclone 守护进程中活跃任务 ID: {active_ids}")

    for idx, (src, dst) in enumerate(TASKS, 1):
        if not running:
            break

        folder_name = src.split(":")[1]
        log(f"[{idx}/{len(TASKS)}] 检查目录同步: {src} -> {dst}")

        # 检查是否已有 Job 正在同步该目录
        current_jobid = None
        for jid in get_running_jobs():
            try:
                stats = rc_call("core/stats", {"group": f"job/{jid}"})
                for tr in stats.get("transferring", []):
                    if folder_name in tr.get("srcFs", ""):
                        current_jobid = jid
                        log(f"发现该目录已有正在运行的 Job ID: {current_jobid} (源路径: {tr.get('srcFs')})")
                        break
            except Exception:
                pass
            if current_jobid:
                break

        # 如果没有在运行，则通过 RC 提交新 Job
        if not current_jobid:
            try:
                res = rc_call("sync/copy", {"srcFs": src, "dstFs": dst, "_async": True})
                current_jobid = res.get("jobid")
                log(f"已成功提交任务到 Rclone WebUI: Job ID {current_jobid} ({src})")
            except Exception as e:
                log(f"提交任务失败: {e}，跳过此任务")
                continue

        # 等待该 Job 结束
        last_logged_transfers = 0
        while running:
            time.sleep(10)
            status = get_job_status(current_jobid)
            if status.get("finished"):
                if status.get("success"):
                    log(f"[✓] 任务 {current_jobid} ({src}) 同步完成！")
                else:
                    log(f"[✗] 任务 {current_jobid} ({src}) 异常结束: {status.get('error')}")
                break

            # 周期性记录心跳日志
            try:
                stats = rc_call("core/stats", {"group": f"job/{current_jobid}"})
                speed = stats.get("speed", 0) / 1024
                bytes_mb = stats.get("bytes", 0) / (1024 * 1024)
                total_mb = stats.get("totalBytes", 0) / (1024 * 1024)
                transfers = stats.get("transfers", 0)
                if transfers != last_logged_transfers or (int(time.time()) % 60 < 10):
                    last_logged_transfers = transfers
                    log(f"  -> Job {current_jobid} 进度: 已传输 {bytes_mb:.1f}MB/{total_mb:.1f}MB | 速度: {speed:.1f}KB/s | 已完成文件数: {transfers}")
            except Exception:
                pass

    log("=== 所有网盘同步任务队列处理完毕 ===")

if __name__ == "__main__":
    main()
