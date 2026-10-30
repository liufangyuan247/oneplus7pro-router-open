# PairDrop（设备在线互传）

浏览器打开同一台路由器的 `http://<router>:3000/`，即可发现在线设备并传输文件或文字。收发双方须保持页面在线；没有服务器文件箱，也不保存上传文件或提供离线下载。接收文件的位置由接收设备的浏览器决定，服务端不能控制或定时清理客户端的下载。

普通入口自动兼容 VPN 和浏览器的 WebRTC 限制，无需添加 URL 参数。先尝试直连，8 秒未建立数据通道、连接失败或浏览器拒绝创建 RTC 时，仅让对应的两台设备切换到路由器 WebSocket 内存转发；已连接的其他设备继续直传。需要服务端开启 `WS_FALLBACK=true`（本部署默认已开），双方保持页面在线。

可选的 `?transport=ws` 参数用于手动直接选择转发，普通使用无需填写。转发时文件流量经过路由器，但不写入路由器磁盘。

PairDrop 1.11.2 会先通过服务器发现设备，等 WebRTC 数据通道建立后才显示设备图标。VPN 的 WebRTC 防泄漏、UDP 限制或局域网访问限制可能让直连无法完成；网页可访问并不能证明直连可用。原有 fallback 主要用于不支持 WebRTC 的浏览器，不会因所有直连故障而自动切换。

## 运行方式

- 官方源码固定为 `schlagmichdoch/PairDrop` 的 `1b0c9c9d903c3cfc706df1303dc75c3b54d04c77`（1.11.2）。第三方源码和二进制不进入本仓库。
- `package-lock.json` 基于上游依赖范围更新兼容的依赖，修复原锁文件的已知问题；部署使用 `npm ci --omit=dev --ignore-scripts`。
- Termux Node.js LTS + runit，服务名 `pairdrop`，日志轮转为 4 × 1 MiB。runit 随已有路由服务栈开机运行，停止/启动/重启及日志入口集成在 Router Admin。
- 优先 WebRTC 直传；不支持或无法建立 WebRTC 的设备自动走路由器 WebSocket 内存转发。两种方式均不把文件写入服务器磁盘。
- 默认 `iceServers: []`，局域网无需依赖外部 STUN/TURN。远程设备的 WebRTC 连通性取决于网络环境；WebSocket fallback 需要双方在线。
- 运行临时目录为手机内部 `$PREFIX/tmp/pairdrop`，与硬盘、媒体、订阅和服务数据隔离；传输文件不会写入此目录。
- HTTP 支持局域网基本互传；部分浏览器的安装为 PWA、剪贴板及通知功能需要可信 HTTPS。需要这些功能时，通过已有反向代理单独提供 HTTPS。
- 只向可信 LAN / Tailscale 提供访问，不开放 WAN 入口。

## 安装与更新

先在 Termux 安装 `nodejs-lts`（不需要全量升级其他路由服务），准备固定版本官方源码归档。归档根目录包含 `server/`、`public/`、`package.json` 和上游许可证，不含顶层目录。

本目录的 `package.json` 记录对应上游的依赖范围。将源码归档以 `source.tar.gz` 命名，与本目录的 `package-lock.json`、`pairdrop-service-run`、`rtc-config.json`、`apply-local-overlay.py`、`router-compat-client.js`、`router-compat-server.js`、`deploy-pairdrop.sh` 一起传入 `~/.stage/pairdrop/`。用 Termux 用户执行：

```sh
sh ~/.stage/pairdrop/deploy-pairdrop.sh <source-archive-sha256>
```

脚本安装到独立 release 目录，检查依赖与语法，保存旧版本和启动配置后切换软链接；健康检查失败则恢复旧版本，首次部署失败则禁用新服务。已有 `service.env`、RTC 配置保留。默认健康探测端口为 3000。

`apply-local-overlay.py` 为固定上游安装客户端和服务端的自动兼容扩展，并更新 service worker 缓存版本；脚本可重复执行，遇到不匹配的源码则拒绝修改。`LOCAL_OVERLAY` 记录补丁版本 `op7-auto-ws2`，第三方原始源码仍不提交 Git。服务端仅接受同一房间内真实成员的切换请求，重复请求不会重复断开设备。

已有安装需先备份 `public/scripts/util.js`、`public/service-worker.js`、`public/scripts/main.js`、`public/scripts/router-compat.js`、`server/ws-server.js`、`server/router-compat.js` 和 `LOCAL_OVERLAY`，记录不存在的新文件以便回滚。先执行 `python apply-local-overlay.py <release> --check`，再应用补丁并检查 JavaScript 语法。服务端扩展需要重启一次 PairDrop；重启会中断正在进行的传输，应选择没有传输的时候操作。浏览器刷新普通入口即可加载新逻辑。

服务端测试：`node --test services/pairdrop/router-compat-server.test.mjs`。浏览器验证覆盖正常直连、RTC 卡住、构造失败及不支持 RTC，均需验证设备发现和文件逐字节一致。

若新 Node 需要较新的 `libc++_shared.so`，可从相应 Termux 包中提取到 `~/router-apps/pairdrop/runtime/lib/`，仅为 PairDrop 设置 `LD_LIBRARY_PATH`，避免替换现有服务共用的库。私有网络配置放在 `~/router-data/pairdrop/service.env`；真实凭据与部署快照仅存 private 的忽略目录。

上游：[PairDrop](https://github.com/schlagmichdoch/PairDrop)。
