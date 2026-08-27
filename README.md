# OnePlus 7 Pro Android 软路由

把一台已解锁的 OnePlus 7 Pro（GM1910）改造成长期供电、双千兆网口、带本地存储与代理服务的 Android 软路由。

这不是一个通用刷机包，也不是“下载镜像后一键刷入”的项目。仓库公开的是已经在真实设备上迭代过的路由脚本、管理后台、服务编排、内核补丁、隔离构建环境和故障经验；固件、boot 镜像、第三方二进制、账号与设备备份不会进入 Git。

> **高风险提示**：解锁、刷 boot、修改 Magisk `service.d` 和接管家庭网络都有变砖或断网风险。先备份当前 boot 和用户数据，优先使用 `fastboot boot` 临时验证，并保留另一台可联网路由器。不要直接把示例配置用于生产。

## 做到了什么

- OxygenOS 11 + Magisk 保留 Android 的 USB、PD、显示和 Qualcomm vendor 支持。
- CM418 Hub 内置 RTL8153 固定为 WAN，另一块 USB 网卡动态识别为 LAN。
- root 网络管理器完成 PPPoE、DHCP、DNS、NAT、策略路由和 `fq_codel`。
- 外置 AX88179B 通过自定义 4.14 内核驱动工作；补丁包含 NAPI 和 USB reset 稳定性修复。
- OpenList、aria2、AriaNg、Mihomo、CPA、CPA Usage Keeper、Tailscale、SSH 和管理后台统一运行。
- Mihomo 默认不接管全屋；可切换全屋代理，同时让 CPA 固定使用独立代理组。
- Tailscale 子网路由与透明代理共存，避免表 52、CGNAT 地址和控制链路被错误代理。
- 息屏后通过 Magisk keeper、wake lock、Doze 白名单和 USB autosuspend 策略保持服务。
- 电池保护使用充电上下限滞回与 watchdog，降低长期满充和耗尽关机风险。
- 网络脚本支持 `backup -> pending -> trial -> confirm/rollback`，更新失败可恢复稳定版本。
- Go + Next.js 管理后台提供 CPU、内存、温度、接口、客户端、服务、Mihomo、Tailscale、网页 SSH 和网页屏幕控制。

## 最终拓扑

```text
光猫
  │ PPPoE
  ▼
CM418 内置 RTL8153（WAN）
  │
OnePlus 7 Pro / OxygenOS 11 / custom 4.14 kernel
  ├─ 外置 USB Ethernet（LAN） ── 主 AP ── 家庭终端
  ├─ USB HDD ── OpenList / aria2
  ├─ PD 输入与电池保护
  └─ Tailscale / Mihomo / CPA / SSH / Router Admin
```

CM418 内置口以物理 USB 路径或私有 MAC 标识为 WAN；LAN 从其余 USB Ethernet 中动态选择，因此更换外置网卡或 Android 重新编号 `eth0/eth1` 时不需要改死接口名。多个网口、硬盘和 HDMI 仍共享 Hub 与手机的 USB 上行带宽。

## 为什么不是 OpenWrt 虚拟机或 LineageOS

本项目最终保留 OxygenOS 11：

- Qualcomm/OnePlus 的 Type-C、PD、充电、热管理和显示驱动最完整。
- Android userspace 本身不是 300 Mbps 宽带的主要瓶颈，真正的问题集中在 USB 网卡驱动、策略路由和休眠行为。
- OpenWrt VM 会增加一层 TAP/bridge/NAT、内存占用和启动依赖，硬件仍由 Android 内核驱动，不能解决缺失的 AX88179B 驱动。
- LineageOS 更干净，但会引入新的 vendor、充电、相机/显示和 USB 兼容性变量；在现有 OOS 11 已经稳定时，收益小于迁移风险。

Android 11 可以正常解锁、Root 和使用 Magisk。早期“Android 11 不能安装 Magisk所以必须降级”的判断是错误的，也是项目第一个重要教训。

## 软件边界

| 层 | 运行身份 | 职责 |
| --- | --- | --- |
| Android/OOS 内核 | kernel/root | USB、网卡、PD、电池、路由与防火墙能力 |
| Magisk `service.d` | root | 网络、Tailscale、存储、供电保护、Termux 守护与回滚 |
| Termux + runit | Termux UID；个别核心由 root wrapper 启动 | OpenList、aria2、Caddy、SSH、后台、Mihomo、CPA |
| Router Admin | Termux UID + 严格 root helper | 展示、配置、受控服务操作与远程终端 |

典型监听端口如下。真正部署时必须通过防火墙、强密码和公钥认证收紧访问：

| 服务 | 默认监听 | 说明 |
| --- | --- | --- |
| Router Admin | `0.0.0.0:9090` | 一体化管理后台 |
| OpenList | `0.0.0.0:5244` | 文件与 WebDAV |
| aria2 RPC | `0.0.0.0:6800` | 必须配置 RPC secret |
| AriaNg/Caddy | `0.0.0.0:6880` | aria2 前端与同源 RPC 反代 |
| CPA | `0.0.0.0:8317` | CLIProxyAPI |
| Mihomo | `127.0.0.1:17890` | 通用本机入口 |
| Mihomo CPA | `127.0.0.1:17891` | 独立代理组 |
| Mihomo controller | `127.0.0.1:9091` | 只允许管理后端访问 |
| SSH | `0.0.0.0:8022` | 公钥认证；可自行改为 22 |

## 仓库结构

```text
android/router-stack/       Magisk、路由、回滚、存储、供电与诊断脚本
admin/backend/              Go 管理后端与测试
admin/frontend/             Next.js 管理界面和 Chrome 87 兼容处理
services/mihomo/            root 单实例、订阅热加载和验证脚本
services/cpa/               CPA 与 Usage Keeper 的 Android 集成
services/openlist/          OpenList runit 启动包装
kernel/ax88179b/            可复现内核 patch、config、Docker 构建与验证
kernel/r8152-experiment/    Android 9/r8152 早期失败实验，仅供考证
configs/                    不含真实值的配置模板
docs/                       架构、历程、部署、内核、安全和依赖说明
tools/windows/              ADB/scrcpy 网页入口辅助脚本，不含二进制
```

## 从零开始的正确顺序

1. 确认型号、当前 ROM、kernel、boot slot、USB VID/PID、PD 行为，备份原始和 Magisk boot。
2. 在 OOS 11 上完成解锁与 Magisk；安装 Termux，确认 `su -c id` 返回 UID 0。
3. 先只接 Hub，验证 OTG、PD 受电和 CM418 RTL8153；不要立即替换现有路由器。
4. 在隔离 Docker 环境构建 AX88179B 内核，先 `fastboot boot candidate.img` 临时启动。
5. 同时验证网卡、Magisk、Termux、Hub 热插拔、息屏、PD 与重启，再决定是否刷入。
6. 安装 Termux 服务栈，复制 `configs/*.example` 为私有配置并填入自己的值。
7. 离线验证网络脚本和 dnsmasq 配置，先在测试网线/热点下完成双网口测试。
8. 部署管理后台、Mihomo、CPA、Tailscale、OpenList 和存储；所有数据放在持久目录，不写死在安装脚本。
9. 做 WAN↔LAN、LAN↔硬盘、息屏、拔插 Hub、断电重启和 24/72 小时稳定性测试。
10. 最后一次性迁移 PPPoE 和 AP；任何远程更新都走 trial/rollback，不直接覆盖稳定脚本。

详细步骤见 [部署说明](docs/deployment.md)，内核流程见 [内核补丁与构建](docs/kernel.md)。

## 构建管理后台

后端需要 Go 1.24：

```bash
cd admin/backend
go test ./...
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -o router-admin-arm64 .
```

前端需要 Node.js 22.13 或更高版本：

```bash
cd admin/frontend
npm ci
npm run lint
npm run export
```

`npm run export` 会运行 Chrome 87 兼容脚本。OxygenOS 11 自带的旧 WebView/Chrome 无法直接解析部分现代 Next.js 输出，这是“手机打开后台一直连接、电脑正常”的根因之一。

## 构建内核

仓库不含约数百 MB 的内核源码、工具链、ASIX 原始驱动、`magiskboot`、设备 boot 备份或生成镜像。准备好 `kernel/ax88179b` 所需输入后：

```bash
cd kernel/ax88179b
docker compose build
docker compose run --rm kernel-builder bash scripts/prepare-workspace.sh
docker compose run --rm kernel-builder bash scripts/integrate-driver.sh
docker compose run --rm kernel-builder bash scripts/configure.sh
docker compose run --rm kernel-builder bash scripts/build-resetfix-fqcodel.sh
```

输入目录、固定版本、SHA256、打包和临时启动方法见 [docs/kernel.md](docs/kernel.md) 与 [docs/dependencies.md](docs/dependencies.md)。不要使用别人的 boot 镜像替代自己的备份。

## 最重要的稳定性原则

- Root 不等于内核自动支持所有硬件；驱动必须存在且匹配 ABI。
- 链路显示 `1000Mb/s` 只表示协商速率，不证明驱动/NAPI/USB 数据路径正常。
- 不要按 `eth0/eth1` 固定角色；优先使用 Hub 物理端口身份，LAN 自动选择剩余网卡。
- 网络更新必须先语法校验、离线验证、保存 backup，再在下次启动进入 trial。
- 透明代理失败必须 fail-open 回直连，不能把全屋流量黑洞。
- Mihomo 更新先验证新配置，再 controller 热加载；只有热加载失败才重启，失败后恢复 last-good。
- 代理和订阅测试不能切断正在操作设备的 SSH/Codex 控制链路。
- Tailscale 的接口、表 52、mark 和 `100.64.0.0/10` 必须绕过透明代理。
- 电池保护必须有上下限滞回、超时、watchdog 和“读数不可信时继续充电”的安全方向。
- Android 的缓存和冻结线程不等于 OOM；判断内存要看 `MemAvailable`、swap 和实际杀进程记录。

完整踩坑清单见 [项目历程与踩坑](docs/journey-and-pitfalls.md)。

## 本仓库刻意不包含

- OxygenOS/MSM 包、OPS、OTA ZIP、boot/system/vendor 镜像。
- 设备备份、ramdisk、Magisk patched boot、ADB/SSH 私钥。
- PPPoE 账号、订阅 URL、代理节点、OpenList/Aria2/CPA 数据库和口令。
- Tailscale identity/state、节点信息、auth key。
- Termux APK、scrcpy、Mihomo、OpenList、Tailscale、CPA 等第三方二进制。
- OnePlus/ASIX 完整源码 checkout、Android 工具链、Docker volume、构建产物。
- `node_modules`、`.next`、静态导出、Go/Android 二进制和运行日志。
- KMS/vlmcsd：它是个人现场的独立服务，不属于路由核心，也不纳入本项目。

发布前的检查规则见 [安全与开源发布](docs/security.md)。

## 文档索引

- [系统架构](docs/architecture.md)
- [项目历程与踩坑](docs/journey-and-pitfalls.md)
- [部署与迁移](docs/deployment.md)
- [内核补丁、构建与刷入](docs/kernel.md)
- [依赖下载与校验](docs/dependencies.md)
- [安全、备份与回滚](docs/security.md)
- [测试与验收](docs/testing.md)

## 许可证

本项目自有 userspace 代码采用 MIT License。Linux 内核补丁属于内核派生工作，按 GPL-2.0-only 发布。第三方组件继续遵守各自上游许可证；本仓库不重新分发其源码或二进制。
