# 系统架构

## 目标与约束

目标不是把 Android 伪装成 OpenWrt，而是把手机已有的稳定硬件支持保留下来，在其上建立一个可维护的路由 appliance：双网口、PPPoE、DHCP/NAT、透明代理、Tailscale、下载与存储、网页管理、远程恢复。

主要约束：

- OnePlus 7 Pro 只有一个 Type-C 上行，网口、硬盘和供电集中在同一 Hub。
- OxygenOS 11 使用 Qualcomm/OnePlus 4.14 vendor kernel，主线 Linux 驱动不能直接当模块插入。
- Android app sandbox、SELinux、Doze、LMKD 和 credential-encrypted data 仍然存在。
- PPPoE 和家庭 DHCP 一旦失败，会影响全家网络，因此更新链路必须比普通服务器更保守。
- 路由器同时承载远程控制；错误重启代理可能切断执行更新的客户端自身。

## 硬件数据路径

CM418 的内置 RTL8153 是固定 WAN。它在 OOS 11 stock kernel 上由内置 `r8152` 驱动支持，不需要为它编译新内核。外置 LAN 网卡最终采用 AX88179B（USB ID `0b95:1790`），需要 vendor driver v4.1.0 与本项目的 4.14 兼容补丁。

WAN 识别优先级：

1. 私有配置中的 WAN MAC；
2. Hub 内部固定 USB 拓扑路径，例如 `2-1.4`；
3. 仅当 VID/PID 唯一时使用 VID/PID。

LAN 不依赖 `eth1`：排除 WAN 后，如果只剩一个 USB Ethernet 就直接选择；存在多个候选时优先保留上一块 LAN 的 MAC，否则选择唯一有 carrier 的接口。Android 重新枚举或更换 LAN 网卡不会要求修改接口名。

## 启动层次

```text
Magisk post-fs-data/service.d
  ├─ rollback guard：检查 pending/trial，必要时恢复 backup
  ├─ router network：PPPoE、dnsmasq、NAT、策略路由
  ├─ storage monitor：UUID 发现、兼容软链、OpenList refresh
  ├─ tailscaled root supervisor
  ├─ battery guard + watchdog
  └─ Termux keeper
       └─ runsvdir
            ├─ openlist / aria2 / caddy / sshd
            ├─ router-admin
            ├─ mihomo root wrapper
            ├─ cpa / usage-keeper
            └─ healthd / log services
```

Magisk 只负责必须是 root、必须早于用户解锁或必须独立于 Termux 生命周期的部分。普通应用服务由 Termux/runit 管理，这样日志、配置和服务状态都可持久化，不把所有逻辑塞进一个 root 启动脚本。

## 网络平面

`router-network.sh` 管理以下状态：

- WAN/LAN 动态解析；
- PPPoE 进程与 `ppp0` 探测；
- LAN 地址、dnsmasq lease 与静态租约片段；
- IPv4 forwarding、filter、NAT、光猫管理地址；
- Android 自己的策略表和 WAN 默认路由；
- PPP `tx_queue_len` 与可用时的 `fq_codel`；
- 全屋代理 TUN、fail-open 回直连和代理入口 IP 绕过；
- Tailscale 与本地管理流量绕过；
- 状态输出、验证、启停和代理切换。

静态 DHCP 不再写死在脚本里，而从私有 `DNSMASQ_HOSTS_FILE` 追加。示例见 `configs/dnsmasq-hosts.example`。

## 代理平面

Mihomo 维持单个 root 核心：

- `17890`：通用本机 mixed listener；
- `17891`：CPA/Codex 专用 listener，绑定日本 `AI` 代理组；
- `17892`：Antigravity 专用 listener，绑定美国 `Antigravity` 代理组；
- `9091`：controller，仅回环；
- 默认规则为直连；
- 全屋代理由路由管理器显式创建 TUN 和策略路由。

Antigravity 的 Cloud Code、OAuth、Unleash 和自动更新域名单独维护，规则与
DNS/DoH 均绑定美国 `Antigravity` 组；其 CPA 账号配置同时固定使用回环监听
`127.0.0.1:17892`。因此账号级请求和透明代理兜底不会误落到日本 `AI` 或家庭
代理组。

订阅更新流程为：下载到临时目录 → 大小检查 → 适配端口/组/规则 → `mihomo -t` 离线校验 → 保存 last-good → controller 热加载 → 完整健康检查。热加载失败才短暂重启；重启后仍失败则回滚。

为了不形成代理环路，Mihomo 订阅中实际节点的入口 IP 会加入 direct bypass。否则“电脑自己的 Clash 测延迟”或代理核心访问上游节点可能再次进入全屋 TUN，表现为所有节点 timeout。

## Tailscale 平面

Tailscale 由独立 root supervisor 管理，不并入 Mihomo/runit 的生命周期。透明代理必须绕过：

- `tailscale0`；
- Tailscale 路由表 52；
- Tailscale fwmark；
- `100.64.0.0/10`；
- 控制面与 DERP 路径。

加入 tailnet 不代表任意 LAN 电脑自动成为 Tailscale 节点。远端设备通过手机发布的 subnet route 访问 LAN；路由批准、SNAT 与 ACL 仍由 Tailscale 控制台决定。

## 存储平面

Android 对 USB 硬盘通常同时暴露 `/mnt/media_rw/<UUID>` 与 `/storage/<UUID>`。服务端进程的 UID/GID、SELinux 域、FUSE 视图和 NTFS 挂载参数会影响访问。

项目以 UUID 识别真实硬盘，维护 `/mnt/usb_disk` 兼容软链，使从 R2S 导入的 OpenList 存储路径无需随 Android mount name 变化。挂载状态变化后重启 OpenList，让它重新探测目录。真实 UUID 只存在 `/data/adb/router-storage.conf`。

## 供电与息屏

- `service.d` 设置 OnePlus OTG 持久属性并关闭十分钟 OTG 自动关闭。
- Termux 加入 Doze whitelist、active standby bucket，允许后台与 wake lock。
- keeper 在路由启用时持有 kernel suspend blocker。
- USB Ethernet 关闭 autosuspend，热插拔后重复应用。
- battery guard 使用上下限滞回；watchdog 处理守护进程退出、传感器异常和长时间不充电。

Android “灭屏”不等于内核一定降频到不可用。真正需要验证的是灭屏前后端到端吞吐、USB link、PPPoE 和服务心跳，而不是只盯 CPU governor。

## 管理与远程控制

Go 后端读取 `/proc`、`/sys`、Termux 服务和 root helper 的有限输出。前端将瞬时数据保留为约十分钟的移动时间序列，不拉伸历史曲线。

网页 SSH 使用 PTY + WebSocket。网页屏幕控制依赖 ADB/scrcpy 视频流；黑屏可能来自锁屏、Surface、编码器或浏览器解码能力，不代表路由服务停止。锁屏/解锁按钮应视为设备维护功能，与网络健康解耦。

## 持久化边界

代码和运行数据严格分开：

- `~/router-stack`：可替换程序与服务定义；
- `~/router-data`：OpenList、aria2、Mihomo、CPA、后台等持久配置；
- `/data/adb/router-network`：root 网络配置、backup、pending/trial 和日志；
- `/data/adb/tailscale`：Tailscale 二进制、identity/state、socket 与日志；
- `/data/adb/service.d`：小而稳定的启动入口。

安装脚本不得覆盖已经存在的用户配置；更新二进制和 service `run` 文件时先写 `.new`、校验，再原子 `mv`。
