# AdGuard Home

Android / Termux 路由器的 DNS 广告拦截，管理界面 TCP 3001，过滤 DNS UDP/TCP 5354。官方 v0.107.79 Linux ARM64 静态程序已在现机验证；通过清除 `LD_PRELOAD` 后启动系统 shell，绕开 Termux 对非 PIE 可执行文件的拦截。第三方程序不提交 Git。

## DNS 路径与故障绕过

```text
LAN 设备 -> 路由器 IPv4:53 -> PREROUTING REDIRECT :5354
                                     |
                                AdGuard Home
                                     |
                             127.0.0.1:53 dnsmasq
                                     |
                       现有 Mihomo / direct DNS 策略
```

`dns-guard.sh` 仅在当前 LAN 接口、目的地址为路由器 IPv4、目标端口为 53 时重定向 TCP/UDP，不修改 DHCP、dnsmasq、Mihomo、上游订阅或 WAN 防火墙。AdGuard 的回环上游不经过 PREROUTING，避免 DNS 循环；关闭 AdGuard DNS 缓存，保留已有 DNS 策略切换行为。

独立 runit 服务 `adguardhome-dns` 每 5 秒检查本地 DNS。健康则维护专用 `OP7_ADGUARD` 链，故障则撤销入口，让原有 dnsmasq 直接响应。服务恢复后自动重新接入。守护程序作为 root 运行，包装脚本显式转发停止信号；AdGuard 主程序初始化后作为 Termux 用户运行。

保留自定义健康规则 `||adguard-health.invalid^$dnsrewrite=NOERROR;A;127.0.0.1`。删除此规则会使守护程序安全绕过过滤。关闭 AdGuard 保护也可能进入绕过状态。

此部署覆盖使用 DHCP 提供的路由器 IPv4 DNS 的设备。客户端自行使用加密 DNS、VPN 自带 DNS 或其他 IPv6 DNS 时可能绕过。DNS 过滤不能移除网页中的所有广告元素，或与视频共用域名的插播广告。

## 初始化

官方发布地址：`https://github.com/AdguardTeam/AdGuardHome/releases/tag/v0.107.79`。
`AdGuardHome_linux_arm64.tar.gz` 的 SHA-256：
`3f7893c18e8aaadc456d0452839190561c306ca95175a2254958be80a769c1ae`。

将校验后的 `AdGuardHome` 放到 `$PREFIX/bin/AdGuardHome`，权限 0700。把本目录脚本复制到 `~/.stage/adguardhome/`，另行准备仅用户可读的 `credentials.json`，包含 `username` 和随机强密码 `password`；真实凭据只保存在 private 的忽略目录，不进入 Git。

执行 `python ~/.stage/adguardhome/bootstrap.py`。首次权限检查临时使用 root，安装向导仅监听回环 13001，完成后关闭；配置和运行数据归还 Termux 用户。脚本拒绝覆盖已有配置，只有显式 `ADGUARD_RESUME=1` 才重用已初始化配置继续应用初始化设置。再执行 `sh ~/.stage/adguardhome/install-services.sh`，安装独立 runit 服务和 root DNS 守护。脚本只用于首次安装，失败时关闭新服务及其 DNS 重定向。

默认启用 AdGuard DNS filter；可在管理页“过滤器 → DNS 黑名单”增加 `https://anti-ad.net/easylist.txt`（中文 anti-AD）。规则每 24 小时更新，查询日志保留 1 天，统计保留 7 天，误拦截可在查询日志中加入允许列表。

配置、过滤器缓存和日志在 `~/router-data/adguardhome/`；主服务名 `adguardhome`，已接入 Router Admin 的打开、启停、重启和日志入口。服务日志轮转为 4 × 1 MiB。启用前验证真实 LAN 的 TCP/UDP 拦截和放行；停主服务验证正常 DNS 仍可用，恢复后验证拦截重新生效。完成后清理暂存的明文凭据。
