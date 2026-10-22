#!/data/data/com.termux/files/usr/bin/bash
# ==============================================================================
# 网盘照片迁移同步脚本 (百度网盘 / 阿里云盘 -> 夸克网盘)
# 基于 Rclone 与 本地 OpenList WebDAV 高速中转
# ==============================================================================

RCLONE_BIN="/data/data/com.termux/files/usr/bin/rclone"
CONF_FILE="/data/data/com.termux/files/home/router-data/rclone/rclone.conf"
LOG_DIR="/data/data/com.termux/files/home/router-data/rclone/logs"
mkdir -p "$LOG_DIR"

DEST_BASE="quark:百度网盘备份"

# 默认并发优化参数：4 并发传输，8 检查线程，大文件自动分片，不重新覆盖同大小同时间文件
RCLONE_OPTS=(
  --config "$CONF_FILE"
  --disable copy
  --transfers 4
  --checkers 8
  --fast-list
  --buffer-size 16M
  --low-level-retries 10
  --retries 3
  --stats 2s
  --stats-one-line
  -P
)

usage() {
  echo "=================================================="
  echo "  网盘照片同步工具 (Rclone -> Quark)"
  echo "=================================================="
  echo "用法: $0 [选项]"
  echo ""
  echo "快捷命令:"
  echo "  $0 all              同步所有照片备份目录到 夸克网盘/百度网盘备份/"
  echo "  $0 wedding          同步 '婚纱照' 到 夸克网盘"
  echo "  $0 baidu-photos     同步 '来自：百度相册' 到 夸克网盘"
  echo "  $0 iphone           同步 '来自：iPhone' 到 夸克网盘"
  echo "  $0 gm1910           同步 '来自：GM1910' (一加手机) 到 夸克网盘"
  echo "  $0 pja110           同步 '来自：PJA110' (OPPO手机) 到 夸克网盘"
  echo "  $0 wechat           同步 '来自：微信备份' 到 夸克网盘"
  echo "  $0 trip             同步 '港澳行留念' 到 夸克网盘"
  echo "  $0 custom <src> <dst>  自定义源目录和目标目录同步"
  echo "  $0 status           查看当前正在运行的后台同步进程与传输状态"
  echo ""
  echo "提示: 也可直接在 Rclone WebUI (http://192.168.2.1:5572/#/remoteExplorer) 的双栏布局中拖拽复制。"
  echo "=================================================="
}

run_copy() {
  local src="$1"
  local dst="$2"
  local log_file="$LOG_DIR/$(date +%Y%m%d_%H%M%S)_copy.log"

  echo "[+] 准备同步: $src -> $dst"

  # 优先检测 Rclone WebUI / RC 守护进程是否正常在线
  if "$RCLONE_BIN" rc rc/noop >/dev/null 2>&1; then
    echo "[+] 检测到 Rclone RC 守护进程在线，直接提交至 WebUI 任务队列..."
    local res
    res=$("$RCLONE_BIN" rc sync/copy srcFs="$src" dstFs="$dst" _async=true 2>&1)
    local jobid
    jobid=$(echo "$res" | grep -o '"jobid": [0-9]*' | awk '{print $2}')
    if [ -n "$jobid" ]; then
      echo "[✓] 任务已成功提交到 Rclone WebUI! Job ID: $jobid"
      echo "[i] 您可在 WebUI (http://192.168.2.1:5572/#/dashboard) 实时监控进度。"
      echo "[*] 开始跟踪任务进度 (每 5 秒刷新一次)..."

      while true; do
        sleep 5
        local status
        status=$("$RCLONE_BIN" rc job/status jobid="$jobid" 2>/dev/null)
        local finished
        finished=$(echo "$status" | grep -o '"finished": [a-z]*' | awk '{print $2}')

        # 获取当前任务的传输速率和进度
        local stats
        stats=$("$RCLONE_BIN" rc core/stats group="job/$jobid" 2>/dev/null)
        local speed
        speed=$(echo "$stats" | grep -o '"speed": [0-9.]*' | awk '{print $2}')
        local bytes
        bytes=$(echo "$stats" | grep -o '"bytes": [0-9]*' | awk '{print $2}')
        local total
        total=$(echo "$stats" | grep -o '"totalBytes": [0-9]*' | awk '{print $2}')

        if [ "$finished" = "true" ]; then
          local success
          success=$(echo "$status" | grep -o '"success": [a-z]*' | awk '{print $2}')
          if [ "$success" = "true" ]; then
            echo ""
            echo "[✓] 任务 $jobid ($src) 同步完成！"
            return 0
          else
            local err
            err=$(echo "$status" | grep -o '"error": "[^"]*"' | cut -d'"' -f4)
            echo ""
            echo "[✗] 任务 $jobid 同步失败: $err"
            return 1
          fi
        fi

        local speed_kb=0
        if [ -n "$speed" ]; then
          speed_kb=$(awk "BEGIN {printf \"%.1f\", $speed/1024}")
        fi
        local bytes_mb=0
        if [ -n "$bytes" ]; then
          bytes_mb=$(awk "BEGIN {printf \"%.1f\", $bytes/1048576}")
        fi
        local total_mb=0
        if [ -n "$total" ]; then
          total_mb=$(awk "BEGIN {printf \"%.1f\", $total/1048576}")
        fi
        printf "\r[同步中] 已传输: %s MB / %s MB | 实时网速: %s KB/s | 任务ID: %s   " "$bytes_mb" "$total_mb" "$speed_kb" "$jobid"
      done
    else
      echo "[!] RC 提交失败，回退至命令行直接执行模式: $res"
    fi
  fi

  echo "[+] 命令行回退模式: 日志写入 $log_file"
  "$RCLONE_BIN" copy "$src" "$dst" "${RCLONE_OPTS[@]}" --log-file="$log_file"
  local ret=$?
  if [ $ret -eq 0 ]; then
    echo "[✓] 同步完成: $src -> $dst"
  else
    echo "[✗] 同步异常退出 (代码: $ret)，请检查日志: $log_file"
  fi
  return $ret
}

case "$1" in
  all)
    echo "[*] 开始批量同步照片备份文件夹到 夸克网盘..."
    run_copy "baidu:婚纱照" "$DEST_BASE/婚纱照"
    run_copy "baidu:来自：百度相册" "$DEST_BASE/来自：百度相册"
    run_copy "baidu:来自：iPhone" "$DEST_BASE/来自：iPhone"
    run_copy "baidu:来自：GM1910" "$DEST_BASE/来自：GM1910"
    run_copy "baidu:来自：PJA110" "$DEST_BASE/来自：PJA110"
    run_copy "baidu:来自：微信备份" "$DEST_BASE/来自：微信备份"
    run_copy "baidu:港澳行留念" "$DEST_BASE/港澳行留念"
    run_copy "baidu:flashes" "$DEST_BASE/flashes"
    echo "[✓] 所有照片目录同步任务处理完毕！"
    ;;
  wedding)
    run_copy "baidu:婚纱照" "$DEST_BASE/婚纱照"
    ;;
  baidu-photos)
    run_copy "baidu:来自：百度相册" "$DEST_BASE/来自：百度相册"
    ;;
  iphone)
    run_copy "baidu:来自：iPhone" "$DEST_BASE/来自：iPhone"
    ;;
  gm1910)
    run_copy "baidu:来自：GM1910" "$DEST_BASE/来自：GM1910"
    ;;
  pja110)
    run_copy "baidu:来自：PJA110" "$DEST_BASE/来自：PJA110"
    ;;
  wechat)
    run_copy "baidu:来自：微信备份" "$DEST_BASE/来自：微信备份"
    ;;
  trip)
    run_copy "baidu:港澳行留念" "$DEST_BASE/港澳行留念"
    ;;
  custom)
    if [ -z "$2" ] || [ -z "$3" ]; then
      echo "错误: 自定义同步需要指定源和目标路径！"
      exit 1
    fi
    run_copy "$2" "$3"
    ;;
  status)
    echo "=== 当前活跃的 Rclone 传输进程 ==="
    pgrep -fl rclone | grep -v 'rcd' || echo "当前无活动的后台传输脚本进程。"
    echo ""
    echo "=== Rclone WebUI 守护进程状态 ==="
    curl -s -X POST http://127.0.0.1:5572/core/stats | python3 -m json.tool 2>/dev/null || echo "无法获取 RC 状态"
    ;;
  *)
    usage
    ;;
esac
