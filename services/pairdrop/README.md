# PairDrop（设备在线互传）

浏览器打开同一台路由器的 `http://<router>:3000/`，即可发现在线设备并传输文件或文字。收发双方须保持页面在线；没有服务器文件箱，也不保存上传文件或提供离线下载。接收文件的位置由接收设备的浏览器决定，服务端不能控制或定时清理客户端的下载。

## 运行方式

- 官方源码固定为 `schlagmichdoch/PairDrop` 的 `1b0c9c9d903c3cfc706df1303dc75c3b54d04c77`（1.11.2）。第三方源码和二进制不进入本仓库。
- `package-lock.json` 基于上游依赖范围更新兼容的依赖，修复原锁文件的已知问题；部署使用 `npm ci --omit=dev --ignore-scripts`。
- Termux Node.js LTS + runit，服务名 `pairdrop`，日志轮转为 4 × 1 MiB。runit 随已有路由服务栈开机运行，停止/启动/重启及日志入口集成在 Router Admin。
- 优先 WebRTC 直传；不支持 WebRTC 的浏览器可走路由器 WebSocket 内存转发。两种方式均不把文件写入服务器磁盘。
- 默认 `iceServers: []`，局域网无需依赖外部 STUN/TURN。远程设备的 WebRTC 连通性取决于网络环境；WebSocket fallback 需要双方在线。
- 运行临时目录为手机内部 `$PREFIX/tmp/pairdrop`，与硬盘、媒体、订阅和服务数据隔离；传输文件不会写入此目录。
- HTTP 支持局域网基本互传；部分浏览器的安装为 PWA、剪贴板及通知功能需要可信 HTTPS。需要这些功能时，通过已有反向代理单独提供 HTTPS。
- 只向可信 LAN / Tailscale 提供访问，不开放 WAN 入口。

## 安装与更新

先在 Termux 安装 `nodejs-lts`（不需要全量升级其他路由服务），准备固定版本官方源码归档。归档根目录包含 `server/`、`public/`、`package.json` 和上游许可证，不含顶层目录。

本目录的 `package.json` 记录对应上游的依赖范围。将源码归档以 `source.tar.gz` 命名，与本目录的 `package-lock.json`、`pairdrop-service-run`、`rtc-config.json`、`deploy-pairdrop.sh` 一起传入 `~/.stage/pairdrop/`。用 Termux 用户执行：

```sh
sh ~/.stage/pairdrop/deploy-pairdrop.sh <source-archive-sha256>
```

脚本安装到独立 release 目录，检查依赖与语法，保存旧版本和启动配置后切换软链接；健康检查失败则恢复旧版本，首次部署失败则禁用新服务。已有 `service.env`、RTC 配置保留。默认健康探测端口为 3000。

若新 Node 需要较新的 `libc++_shared.so`，可从相应 Termux 包中提取到 `~/router-apps/pairdrop/runtime/lib/`，仅为 PairDrop 设置 `LD_LIBRARY_PATH`，避免替换现有服务共用的库。私有网络配置放在 `~/router-data/pairdrop/service.env`；真实凭据与部署快照仅存 private 的忽略目录。

上游：[PairDrop](https://github.com/schlagmichdoch/PairDrop)。
