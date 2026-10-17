# 项目历程与踩坑

这份记录强调“为什么最后这样做”。很多问题表面上像 Android 性能不足，实际根因分别位于内核驱动、USB 角色、Wi-Fi 中继、策略路由、进程监督和视频解码。

## 1. 从 Android 9 误判开始

最初担心 Android 11 无法安装 Magisk，因此主动降级 Android 9，并计划 LineageOS。后来验证发现 Android 11 可以正常解锁、Magisk root 和运行 Termux。降级没有解决目标问题，反而让内核源码、vendor 驱动和最终 OOS 版本发生偏移。

结论：先验证当前系统能否 root，不要把“某个旧教程只写 Android 9”当成 Android 11 不支持。

## 2. Root 不等于拥有所有驱动

取得 UID 0 后仍无法使用部分 USB 网卡。Root 可以改 sysfs、iptables、启动 root 进程和刷 boot，但不能凭空增加内核里没有的驱动，也不能把任意桌面 Linux `.ko` 加载进 OOS 4.14。

CM418 内置 RTL8153 在 OOS 11 已由 `r8152` 支持；外置 AX88179B 虽然同为千兆 USB 网卡，旧 in-tree AX88179 驱动不能正确覆盖其路径，必须移植 ASIX v4.1.0 驱动并匹配 kernel ABI。

结论：先看 `lsusb`、`dmesg`、`readlink /sys/class/net/*/device/driver` 和 `ethtool -i`，再决定是 userspace 配置还是 kernel 问题。

## 3. r8152 和 Android 9 内核实验

早期为 CM418 尝试 Android 9 / NetHunter / r8152 内核。它证明了隔离构建、备份和临时启动流程的重要性，但不是最终实现：

- 目标 ROM 与源码版本不一致；
- Windows checkout 遇到 Linux 内核保留文件名问题；
- 源码 checkout 后来不完整，Git 状态表现为大面积删除；
- CM418 在 OOS 11 stock kernel 实际已经有 r8152；
- 真正缺失的是另一块 AX88179B 网卡的正确驱动。

因此 `kernel/r8152-experiment` 只作为历史实验保留，不能当成生产构建链。

## 4. MSM 恢复与 OOS 11

为了回到可控基线，使用 MSM 恢复工具刷回 OxygenOS。期间遇到 EDL 驱动、握手失败、工具登录界面、factory 下拉项和 start 错误。最终的经验是：

- Qualcomm EDL 驱动必须正确安装；
- 关闭其他占用端口的工具，确认设备确实进入 9008；
- MSM 包与机型/区域必须匹配；
- 工具出现 login/factory 页面不代表设备或包一定错误；
- 刷完先完整进入系统，再重新解锁和修补自己的 boot。

MSM/OPS 包有版权和体积问题，本仓库不提供。

## 5. 自定义内核第一次启动：驱动、Magisk 与 Android userspace 是三项验收

内核能启动并不代表候选镜像成功。曾出现：

- 开机动画长时间转圈；
- Launcher 报错和黑屏；
- 浏览器、Termux 等 app 无法打开；
- 系统起来但 `su` 消失；
- 网卡没有枚举，PD 状态也异常。

原因包括 boot repack 没有完整保留当前 Magisk ramdisk/DTB、kernel 与 public source 的细微差异，以及 USB configuration 选择错误。

最终把验收拆为：

1. kernel release、启动日志和 panic；
2. Magisk cpio 状态、`su -c id`、Termux app 启动；
3. Hub、RTL8153、AX88179B、PD、热插拔和息屏。

优先 `fastboot boot` 临时启动；临时镜像只在本次开机有效，重启会回到已刷 boot，这是最重要的回退手段。

## 6. AX88179B 内核补丁

ASIX v4.1.0 驱动面向更新的 Linux API，直接放进 4.14 会遇到 `select_queue`、skb frag、timeout 参数等接口差异。运行时还遇到 TX NAPI 返回值超过 budget 与 USB reset 后队列状态不一致。

最终 patch 完成：

- 4.14 API 兼容；
- TX NAPI `work_done` clamp；
- reset 前 `ax_disable()`；
- 避免 post-reset 重复/错误启用 TX NAPI；
- 禁用旧 in-tree AX88179，启用 vendor AX USB NIC；
- 保留 `fq_codel` 模块能力。

整理开源仓库时还发现原 patch 两行 context 丢了 diff 空格前缀，导致 `git apply` 报 `corrupt patch at line 47`。已修复并用 `git apply --stat` 验证为 1 个文件、16 insertions、9 deletions。这个坑说明“曾经构建成功”不能代替对发布补丁本身的可应用性检查。

## 7. USB configuration 与热插拔

AX88179B 会暴露两个 USB configuration。OOS 有时先选 configuration 2，被通用 `cdc_ncm` 绑定成 `usb0`，vendor 驱动无法接管。改为 configuration 1 后才走 `ax_usb_nic`。

一次手改 sysfs 不能解决重启和拔插，因此 keeper 每次热插拔检查 `0b95:1790` 并重设 configuration 1。同时关闭 USB autosuspend。

## 8. PD“插着却不充电”

CM418 的 Type-C host/device/power role 会变化。相同线材、充电器和 Hub，也可能在重新插拔后进入“手机给本地设备充电”，表现为 PD 输入为 0、手机持续掉电，甚至最终关机。手动关闭“给本地设备充电”后恢复受电。

这不是单纯的线材功率问题。长期运行必须同时观察：

- Type-C data role / power role；
- battery current/voltage；
- USB-PD input limit；
- 电量随时间趋势；
- Hub 网卡是否仍保持 host 数据连接。

Android 电池节点通常是电池侧功率，不等于 PD 适配器的总输入功率；屏幕显示 0 也不能单独证明完全未供电。

## 9. 链路千兆但只有约 5 Mbps

旧网卡阶段出现过链路协商千兆、实际吞吐只有约 5 Mbps。更换网卡并不能自动排除驱动问题，因为 `ethtool` 的 Speed 只来自 PHY 协商。最终通过端到端 iperf、驱动统计、NAPI/reset 修复和直连测试确认数据路径。

判断瓶颈必须分层：

1. 单网口接电脑的 iperf；
2. 两网口直连、手机做转发；
3. PPPoE WAN↔LAN；
4. USB HDD↔LAN；
5. Wi-Fi AP/中继；
6. 应用层 WebDAV/视频。

## 10. Wi-Fi 中继让一切看起来像手机路由器变慢

部署后 WebDAV、YouTube、scrcpy 都曾严重卡顿，而之前某次 WebDAV 又能跑得很快。根因之一是下游 K2P 的无线中继/信道路径。直连主 AP、调整信道后明显恢复。

R2S 时代“不明显”不表示中继没有问题；拓扑、MTU、buffering 和并发模式变化后，同一中继的抖动会更容易暴露。网络排障要先消除中继，使用有线或主 AP 做基线。

另一个独立问题是 PotPlayer 硬解码：下载速度正常但播放卡，切软解后恢复。应用解码故障不能归咎于路由吞吐。

## 11. Android `ip rule ipproto` 的静默陷阱

Android 这版 `ip`/kernel 组合会接受带 `ipproto` 的命令，却在实际规则里忽略协议条件。原本计划分别匹配 TCP/UDP 的四条规则，最后变成重复的全协议规则，ICMP 也被送入 TUN，引发 ping、Tailscale 和本地访问异常。

最危险之处是命令返回成功。正确检查不是只看 exit code，而是重新读取 `ip rule show` 并对实际语义做断言。当前实现避免依赖这个不可靠条件，并在健康检查中核对规则唯一性和 TUN 路由。

## 12. 每 5 秒重建规则造成短暂断网

早期网络循环为了“确保状态”频繁 `rule del/add`，造成短暂转发窗口、Tailscale netlink 日志刷屏和偶发卡顿。修复后先比较期望状态，仅在缺失或错误时替换规则。

幂等不等于反复删除重建；稳定网络的幂等应是“状态正确则不写”。

## 13. PPP 队列长度与 `fq_codel`

PPPoE 默认队列太小时，突发吞吐和移动网络经 Tailscale 访问会受影响。运行时把 `tx_queue_len` 调到 1024 后改善。它主要消耗少量内核队列内存，CPU/RAM 充足不意味着应该无限增大；过大的队列会增加延迟。

新内核把 `fq_codel` 编译为模块，但 Android 模块签名、加载路径和 ABI 仍需验证。脚本的 `PPP_QDISC=auto` 会在可用时启用，不可用时保留现有 qdisc，而不是让 PPP 启动失败。

## 14. Mihomo root 进程变成孤儿

通过 `su -c exec mihomo` 启动后，真正的 root 核心会被 reparent 到 Magisk daemon。runit 只看得到/杀得到前面的 `su` 客户端，停止服务后核心仍占用端口；再次启动会产生重复进程。

修复方案：

- root wrapper 记录真实核心 PID；
- TERM/INT trap 转发信号并等待退出；
- 健康检查核对 PID file、UID 0、cmdline 中的当前配置；
- 枚举全部 Mihomo，要求恰好一个匹配核心。

## 15. 订阅更新把 Codex 自己切断

旧更新器直接 `sv down/up`。有一次新核心约 3 秒已经恢复，但错误的组合健康检查仍硬等 30 秒，然后判失败、再次停机回滚。操作端的 Codex provider 正经过同一个 CPA/Mihomo 路径，连续中断超过客户端重试窗口，于是远程操作本身失联。

当前流程先离线校验，再热加载，只有必要时才重启。健康检查直接验证：

- controller 9091 API；
- 9091、17890、17891 的真实 TCP connect；
- root PID identity；
- 当前配置 cmdline；
- 单核心约束。

自动更新周期为 6 小时；失败保留 last-good，不把一次坏订阅扩散到正在运行的核心。

## 16. 全屋代理与电脑本地 Clash 二次代理

全屋 TUN 开启时，电脑本地 Clash 连接机场入口 IP 的流量也经过手机 Mihomo；外层 Mihomo 再试图代理这个入口，形成二次代理/环路，节点延迟全部 timeout。

修复是从当前订阅解析代理入口域名/IP，写入 direct bypass；Tailscale 和管理链路也显式绕过。切换全屋代理时必须保证失败回直连。

## 17. CPA WebSocket、日志与 Usage Keeper

CPA 普通 HTTP 请求可用但 WebSocket 长连接曾无响应；打开全屋代理后正常，说明其独立 listener/路由和上游连接策略需要对齐。请求日志并非后台自带页面默认展示，需要在 CPA 配置中启用日志到文件。新版 CPA 把用量和价格统计拆到 `cpa-usage-keeper`，因此本项目只做部署与服务入口，不复刻第三方功能。

Usage Keeper 的 Models.dev 同步超时同样是“手机本机服务是否进入全屋代理”问题。最终取消额外 Android system proxy，规则简化为：手机本机与 LAN 在全屋模式下统一进入代理；关闭全屋时直连；CPA 仍可通过专用 listener 使用指定组。

## 18. Tailscale 的误解与共存

手机显示在线不等于移动网络上的手机一定能 ping 任意内网电脑。还需要：

- subnet route 已在控制台批准；
- 远端客户端接受 route；
- LAN 主机回程、SNAT 和防火墙正确；
- ACL 允许；
- 透明代理未截获 Tailscale 流量；
- DERP/直连质量足够。

项目排障时明确禁止修改仍在生产的 R2S Tailscale，直到迁移真正开始。这是重要的变更范围原则：替换系统时不要先破坏当前回退路径。

## 19. OpenList、Aria2 与视频问题

OpenList 升级到 v4.2.7 后，旧配置里的 `/assets/logo.svg` 和自定义 head 中的 `/assets/polyfill.min.js` 已不存在，但 SPA 回退仍返回 HTTP 200 的 HTML。表现是页面可用、Logo 一直转圈，控制台报 `Unexpected token '<'`。修复需清理数据库中的旧引用，不能靠强制刷新；Logo 改为内嵌官方 SVG，移除失效的 polyfill 标签。可复用的备份、修复和失败回滚脚本见 [OpenList 说明](../services/openlist/README.md)。检查静态资源时必须同时看响应类型与正文。

OpenList“发送到 aria2”失败可能发生在前端 RPC 地址、同源/跨域、RPC secret、aria2 未就绪、任务队列和网盘 URL 失效任一层。仅看 aria2 进程在线无法证明 OpenList 前端发出的 RPC 正确。

Aria2 并发配置最终不再被脚本写死：AriaNg 通过 RPC 修改全局参数，后台同步器只把用户改动持久化到 `aria2.conf`。429 常来自网盘限流或旧签名 URL，不应自动归因于路由器 OOM。

## 20. 内存、温度与充电功率读数

Android 显示 3.5 GB“已用”并不代表只剩很少内存。页缓存、zram、冻结进程和可回收 slab 都包含在不同口径中。稳定性判断看 `MemAvailable`、swap、LMKD/OOM 日志和服务 RSS。

不同 thermal zone 可能是 CPU hotspot、电池、PMIC 或算法估计，瞬时排序跳变不能当整机表面温度。后台改为同时显示传感器类型和时间曲线。

## 21. 网页远程屏幕黑屏

网页 SSH 只需 PTY/WebSocket，稳定后可独立工作。屏幕控制还依赖 ADB、scrcpy server、Android Surface、锁屏状态、H.264 编码器和浏览器 decoder。右侧黑屏不影响 PPPoE、NAT、OpenList 等后台服务。控制页面加入锁屏/解锁动作，但生产路由健康不能依赖屏幕持续可用。

## 22. 最终工程原则

- 先用可回退的小实验定位层次，再改系统。
- 保留 stock/OOS 的硬件优势，只替换确实缺失的驱动与服务。
- 现场值全部进入私有配置，代码保持可发布。
- 所有更新都离线验证、原子替换、保留 last-good。
- 控制面与被测试的数据面隔离；至少保留 USB ADB、热点或旧路由器一种回退。
- 性能结论只来自端到端基线，不由链路速率、单个传感器或某个播放器推断。
