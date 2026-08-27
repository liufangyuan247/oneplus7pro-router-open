# 测试与验收

## 代码级检查

```bash
# POSIX/Android shell 语法；在有 shellcheck 时再做静态检查
find android services kernel -type f \( -name '*.sh' -o -name '*-run' \) -print0 |
  xargs -0 -n1 sh -n

cd admin/backend && go test ./...
cd ../frontend && npm ci && npm run lint && npm run export

cd ../../kernel/ax88179b
git apply --stat patches/asix-v4.1.0-oneplus-4.14.patch
```

Android `/system/bin/sh` 与桌面 `sh` 不完全相同。桌面语法通过后，还需在手机上运行脚本自带的 validate/selftest。

## 内核 Gate

- candidate 只能先 `fastboot boot`；
- kernel、ramdisk、DTB、Magisk cpio hash/状态符合自己的 base；
- Android Launcher、Chrome、Termux 和 `su` 正常；
- 两块网卡驱动正确，AX88179B configuration 1；
- dmesg 无 panic、NAPI budget、持续 TX timeout/reset；
- Hub 拔插至少三次可自动恢复；
- PD 角色和电量趋势正常；
- 临时启动稳定后才考虑刷入。

## 网络基线矩阵

每项双向跑至少 60 秒，记录吞吐、CPU、温度、丢包、重传和 dmesg：

| 路径 | 目的 |
| --- | --- |
| LAN 网卡 ↔ 电脑 | 单网卡/驱动上限 |
| WAN 网卡 ↔ 电脑 | CM418/r8152 上限 |
| WAN ↔ LAN，经手机转发 | NAT/策略路由上限 |
| USB HDD ↔ LAN | Hub 共享与存储上限 |
| PPPoE WAN ↔ LAN | 真实宽带与 MTU |
| 主 AP Wi-Fi ↔ LAN | 无线基线 |
| 中继 Wi-Fi ↔ LAN | 单独量化中继损耗 |
| Tailscale 移动网络 ↔ LAN | DERP/直连、运营商上行和 subnet route |

不要用 speedtest 单点结果判断网卡驱动；不要用视频播放器卡顿判断网络，先用 iperf/HTTP 下载和软件解码交叉验证。

## 功能验收

- PPPoE 断线后自动重拨，期间 LAN 管理仍可访问；
- DHCP 新租约、静态租约、DNS cache 正常；
- 光猫管理地址可达且不抢默认路由；
- OpenList 登录、WebDAV、大文件 range request 正常；
- OpenList 发送 aria2 成功，AriaNg 从其他 LAN 终端可访问；
- aria2 用户修改的并发/选项重启后保留；
- Mihomo 订阅更新失败不替换 last-good；
- 全屋代理开/关都不影响 Tailscale 和管理后台；
- CPA 通过专用 listener 的 HTTP、stream/WebSocket 路径正常；
- Usage Keeper 能同步价格并显示请求记录；
- Tailscale login、start/stop、subnet route 和移动访问正常；
- SSH 公钥、网页 SSH、屏幕控制和锁屏/解锁按预期；
- 所有服务配置在重启后保持，不被安装脚本覆盖。

## 稳定性与故障注入

按顺序执行：

1. 亮屏 1 小时压力；
2. 息屏 8 小时，持续 ping、iperf 小流量与服务心跳；
3. aria2 多任务 + WebDAV 播放 + WAN 流量并发；
4. Hub 拔插、单独拔 LAN、单独拔 HDD；
5. PD 拔插与 host/受电角色切换；
6. PPPoE 断线、光猫重启；
7. Mihomo 坏订阅、controller 热加载失败、核心重启；
8. 网络脚本坏 candidate，验证 trial rollback；
9. Android 正常重启和意外断电恢复；
10. 72 小时长期运行。

长期通过标准至少包括：无 kernel panic、无重复 Mihomo、无持续 USB reset、无 OOM 杀死核心服务、无电池持续下降至关机、日志受轮转限制、重启后配置和服务一致。

## 观测口径

- CPU：总利用率、各频率与 throttling，不以“是否打满”单独判断。
- 内存：`MemAvailable`、swap、主要服务 RSS、LMKD/OOM 日志。
- 温度：按 thermal zone type 保存曲线，区分 CPU hotspot、电池和 PMIC。
- 供电：battery current/voltage、电量趋势、PD limit、Type-C role 联合判断。
- 网络：实际 bytes/s、TCP retransmit、qdisc、USB driver counters、carrier/reset。
- 服务：PID identity、端口 connect、业务 API；仅 `pidof` 不足以证明健康。
