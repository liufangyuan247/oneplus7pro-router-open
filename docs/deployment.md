# 部署与迁移

本文给出安全顺序，不承诺跨机型一键安装。命令路径以 Termux 默认包名和 GM1910/OOS 11 为基线。

## 0. 必须准备的回退

- 仍可用的旧路由器或手机热点；
- USB 直连电脑的 ADB/fastboot；
- 当前 stock boot、当前 Magisk boot 和各自 SHA256；
- 正确机型的 MSM 恢复包与已验证 EDL 驱动；
- PPPoE 账号密码的离线副本；
- OpenList、Aria2、Tailscale、CPA 配置备份。

迁移前不要关闭或改动仍在生产的 R2S。手机的网络脚本、Hub、PD 和服务全部在旁路状态验证后，再一次切换光猫和 AP。

## 1. Android 基线

1. 使用匹配设备的 OxygenOS 11。
2. 解锁 bootloader，修补并启动自己的 Magisk boot。
3. 安装 Termux，授予 root 后验证：

```sh
pkg update
pkg install tsu
su -c id
```

`tsu` 本身报错但 `id` 返回 UID 0 时，应先确认报错发生在哪个 wrapper；真正验收是 root 命令能执行，不是某个前端是否显示成功。

关闭 OTA 自动安装，启用 USB 调试。需要每次连 USB 自动 MTP/ADB 时可参考 `android/router-stack/usb-mtp-adb.sh`，但设备处于 Hub host mode 时无法同时作为电脑的普通 USB device。

## 2. 核对设备相关常量

公开代码保留了现场默认的 Termux UID/GID 形态，但另一台设备的 UID 可能不同：

```sh
su -c 'dumpsys package com.termux | grep userId='
id
```

检查并调整 `20-oneplus-router.device-current.sh`、诊断脚本和需要 `setpriv` 的位置。不要复制别人的 app UID。

## 3. 安装 Termux 基础包

先阅读 `bootstrap-packages.sh`，再运行：

```sh
cd ~/oneplus7pro-router-open/android/router-stack
sh bootstrap-packages.sh
```

第三方二进制按 [dependencies.md](dependencies.md) 下载并校验，放入安装脚本预期的 staging 路径。运行数据统一放 `~/router-data`，升级代码时不得覆盖。

## 4. 配置网络但不接管

在电脑工作树创建私有文件，确保它不被 Git 追踪：

```sh
cp configs/router-network.example configs/router-network.conf
cp configs/dnsmasq-hosts.example configs/dnsmasq-hosts.conf
```

填入自己的 PPPoE、WAN 身份和静态租约。通过 ADB push 整个仓库到：

```text
/data/local/tmp/oneplus7pro-router
```

再以 root 安装：

```sh
ROUTER_CONFIG_SRC=/data/local/tmp/oneplus7pro-router/configs/router-network.conf \
  sh /data/local/tmp/oneplus7pro-router/android/router-stack/install-router-network.sh
```

安装器默认 **不 armed**。它会执行 shell/config/dnsmasq 验证并提示下一步。复核 `/data/adb/router-network/config` 权限为 600 后，测试启动：

```sh
touch /data/adb/router-network/armed
sh /data/adb/service.d/22-router-network.sh
/data/adb/router-network/router-network status
```

若还没有 WAN 网线，可用 Hub 两网口直连电脑/测试终端验证 interface 角色、DHCP 和 iperf，但不能据此证明 PPPoE 成功。

## 5. 部署回滚保护

使用 `install-router-safety.sh` 安装 guard 和 safe updater。更新稳定网络脚本时：

```sh
sh safe-update-router-network.sh stage /data/local/tmp/router-network.candidate
```

它要求当前网络健康、候选语法/validate 通过，保存 backup，再把候选标为 pending；运行中的守护进程不立即被杀。下次启动进入 trial，必须在管理链路确认健康。未确认或再次启动仍发现 trial 时自动恢复 backup。

不要在白天或只有一条远程控制链路时首次部署回滚 guard。

## 6. 存储

连接目标硬盘后取得 UUID：

```sh
su -c 'blkid /dev/block/sd*'
```

复制 `configs/router-storage.example` 到 `/data/adb/router-storage.conf`，填入 UUID，权限 600。然后安装 `23-router-storage.sh`。确认：

- `/mnt/media_rw/<UUID>` 已挂载；
- `/mnt/usb_disk` 是指向它的 symlink；
- Termux UID/GID 能遍历；
- OpenList 的存储 root 指向稳定路径；
- 拔插后 OpenList 自动 refresh。

`force,rw` 和 `ntfsfix` 有数据风险，只对可接受风险、已有备份的卷使用。

## 7. 应用服务

`install-termux.sh` 创建 runit 服务、日志轮转、随机 aria2 RPC secret 和持久配置。顺序建议：

1. aria2 + AriaNg/Caddy；
2. OpenList，并从 WebUI 验证“发送到 aria2”；
3. SSH 公钥；
4. Router Admin；
5. Mihomo + MetaCubeXD；
6. CPA + Usage Keeper；
7. Tailscale。

服务配置只能首次生成，后续升级不得覆盖 `~/router-data`。aria2 并发由 AriaNg 修改，`aria2-config-sync` 负责把变更写回配置，不设置固定最大并发。

## 8. Mihomo

把真实订阅 URL 以单行写到 `~/router-data/mihomo/subscription.url`，权限 600。安装后先：

```sh
~/router-stack/bin/mihomo-update-subscription
~/router-stack/bin/routerctl status mihomo
curl -fsS http://127.0.0.1:9091/version
```

默认全屋代理关闭，家庭设备直连。CPA 可配置使用 `127.0.0.1:17891`。全屋代理切换前保留 USB ADB/热点或不经过 Mihomo 的 SSH 路径。

## 9. Tailscale

Tailscale state 放 `/data/adb/tailscale/state`，不是 Termux home。安装 root supervisor 后通过 `tailscale-control.sh login` 获取登录 URL，在自己的浏览器授权。发布 LAN subnet route 后还要到 Tailscale 控制台批准。

不要把 auth key、state 文件或登录 URL提交到 Git。测试时先保持旧 R2S Tailscale 不变，确认手机 route 后再迁移。

## 10. 管理后台

按 README 构建前端静态文件和 Android/arm64 后端。安装脚本会生成随机初始密码并以 600 保存。不要把生成后的 web bundle、密码文件或运行快照复制回 Git。

网页屏幕控制需要额外下载 scrcpy server；仓库只提供集成代码和 Windows 启动脚本。

## 11. 正式迁移

迁移前完成 [testing.md](testing.md) 的旁路项目。切换顺序：

1. 记录旧 R2S PPPoE、LAN、静态租约、Tailscale、OpenList 状态；
2. 关闭旧 R2S PPPoE，避免运营商限制同账号并发拨号；
3. 光猫接 CM418 WAN；
4. 外置 LAN 接主 AP 的 LAN/上联口，AP 保持桥接；
5. 观察 `router-network status`、PPPoE 日志、DHCP lease；
6. 从有线和 Wi-Fi 终端测试 DNS、互联网、OpenList、Tailscale；
7. 最后打开需要的代理模式。

如果失败，先恢复旧 R2S 与原拓扑，不在全家断网状态下继续试验。
