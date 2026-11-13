# 设备传输站（PairDrop 直传与离线暂存）

浏览器打开同一台路由器的 `http://<router>:3000/`，选择设备发送文字或文件。
浏览器会保存本机身份，已登记的设备离线时仍保留在列表中；清除网站数据、换浏览器
或换访问地址后会成为新的身份，可在页面修改设备名称。

界面采用浅蓝白色会话布局：桌面左侧搜索和选择设备，右侧按收发方向显示待领取的
文字消息和文件卡片，底部输入文字或发送文件；手机上切换设备列表与单个会话。客户端设置只修改本机名称；
全局过期时间、单文件上限和暂存总容量由 Router Admin 的“服务 → 文件共享 → 设备传输站 → 设置”管理。
原有雷达背景、配对/房间入口与工具栏不再显示，保留在线收件确认
和下载所需的核心弹窗。这里显示的是暂存内容，不是永久聊天记录。
首页直接输出传输站标题、浅色主题和加载提示，主题样式在 head 中加载。
即使业务脚本或翻译加载缓慢，也不会先显示原版 PairDrop；新会话界面插入后
加载提示才消失。网页图标与 PWA 名称同步使用设备传输站，安装预览不再引用旧界面截图。

文字统一经路由器持久暂存，在线设备实时收到刷新通知，离线设备下次打开页面领取。
输入框支持多行：电脑 Enter 发送、Shift+Enter 换行，手机 Enter 换行、点击“发送”。
文字气泡支持复制、收件人清理和发送人撤回；HTTP 局域网入口提供兼容复制方式，
浏览器拒绝剪贴板时也可长按或选中手动复制。接收内容始终按纯文字显示。
每条文字最多 16 KiB（UTF-8），同时计入服务端单项大小、总容量及 1,000 项配额，
沿用相同过期时间（默认 2 小时）和收发设备鉴权；不会因已读自动删除。
`POST /mailbox/api/messages` 接受 `{to: "设备 UUID", text: "文字"}`，返回与文件箱兼容的
`file` 记录（`kind: "text"`），支持 UUID 格式的 `Idempotency-Key` 去重重试。
`GET /mailbox/api/files` 同时列出收发文字和文件；文字的确认清理、撤回沿用 `/files/:id/ack`
和 `/files/:id`。WebSocket 的 `mailbox-updated` 只通知收发双方刷新，不广播消息正文。

双方在线且直传通道已就绪时，沿用 PairDrop 在线互传；否则流式上传到路由器的
临时文件箱。接收设备下次打开页面即可看到待收文件，点击“保存文件”，保存完成后
点击“确认收到并清理”。默认 **2 小时**后自动过期，由路由器管理后台统一修改。
修改时长对现有暂存文件同样生效。自动清理只管理路由器上的副本，不删除接收设备下载。

单文件默认上限 512 MiB，暂存总容量 2 GiB，最多 1,000 个文件；上传中的文件同样占用
限额。调低空间上限不会删除已有文件，超出上限时暂停接收新的暂存上传。
普通客户端的 `PUT /mailbox/api/settings` 被拒绝；管理后台经认证接口
`/api/pairdrop/settings` 访问本机私有管理通道。内部接口同时检查实际回环来源和
`mailbox/admin.key`，该密钥不返回浏览器，也不写入公共仓库。
断开的上传不会成为待收文件。下载通过浏览器流式保存，页面不会把整个文件
读入内存，也不会把下载完成等同于用户已保存确认。暂存模式不要求收发双方同时在线；
纯网页无法在接收端关闭页面时自动下载，打开页面后领取即可。

普通入口自动兼容 VPN 和浏览器的 WebRTC 限制，无需添加 URL 参数。先尝试直连，8 秒未建立数据通道、连接失败或浏览器拒绝创建 RTC 时，仅让对应的两台设备切换到路由器 WebSocket 内存转发；已连接的其他设备继续直传。需要服务端开启 `WS_FALLBACK=true`（本部署默认已开），双方保持页面在线。

登记设备通过认证接口取得私有房间密钥，并沿用 PairDrop secret room 建立在线通道，
不改变上游对来源地址的分组。在线传输提示与设备列表使用保存的设备名称。
设备在线状态通过同一个 WebSocket 的身份认证、连接与断开实时推送，
不依赖每 10 秒的数据刷新。多个标签页共用一个设备身份时，最后一个连接关闭才离线；
“已连接服务器”和“直传通道就绪”分别判断。真实断网仍由原有协议心跳超时确认。

可选的 `?transport=ws` 参数用于手动直接选择转发，普通使用无需填写。转发时文件流量经过路由器，但不写入路由器磁盘。

PairDrop 1.11.2 会先通过服务器发现设备，等 WebRTC 数据通道建立后才显示设备图标。VPN 的 WebRTC 防泄漏、UDP 限制或局域网访问限制可能让直连无法完成；网页可访问并不能证明直连可用。原有 fallback 主要用于不支持 WebRTC 的浏览器，不会因所有直连故障而自动切换。

在线检测使用 WebSocket 协议层 ping/pong，浏览器网络层可在网页 JavaScript 暂停时
自动回复。旧版通过 JSON 消息要求网页 JS 回应，会在手机打开系统照片选择器时
误判掉线。检测间隔仍为 1 秒，未收到协议 pong 超过约 5 秒仍会移除连接，未增加
宽限、会话保留或文件重传。若系统暂停了整个浏览器的网络进程，连接仍可能中断。
真实 Android 照片选择器仍有掉线反馈；持久设备选择与离线暂存使交付不再依赖持续在线，
不代表解决了系统暂停网络，也不包含中断在线传输的断点续传。

## 运行方式

- 官方源码固定为 `schlagmichdoch/PairDrop` 的 `1b0c9c9d903c3cfc706df1303dc75c3b54d04c77`（1.11.2）。第三方源码和二进制不进入本仓库。
- `package-lock.json` 基于上游依赖范围更新兼容的依赖，修复原锁文件的已知问题；部署使用 `npm ci --omit=dev --ignore-scripts`。
- Termux Node.js LTS + runit，服务名 `pairdrop`，日志轮转为 4 × 1 MiB。runit 随已有路由服务栈开机运行，停止/启动/重启及日志入口集成在 Router Admin。
- 在线互传优先 WebRTC，不支持或无法建立 WebRTC 时走路由器 WebSocket 内存转发。
  原有在线通道不落盘；离线暂存单独写入文件箱并按时清理。
- 默认 `iceServers: []`，局域网无需依赖外部 STUN/TURN。远程设备的 WebRTC 连通性取决于网络环境；WebSocket fallback 需要双方在线。
- 进程临时目录为手机内部 `$PREFIX/tmp/pairdrop`；离线文件箱与身份、设置在
  `~/router-data/pairdrop/mailbox/`，可用 `PAIRDROP_MAILBOX_DIR` 改目录。
  均不占用挂载硬盘，不触碰媒体或订阅数据。文件内容按流写盘，设置与身份在重启后保留。
- HTTP 支持局域网基本互传；部分浏览器的安装为 PWA、剪贴板及通知功能需要可信 HTTPS。需要这些功能时，通过已有反向代理单独提供 HTTPS。
- 只向可信 LAN / Tailscale 提供访问，不开放 WAN 入口。

## 安装与更新

先在 Termux 安装 `nodejs-lts`（不需要全量升级其他路由服务），准备固定版本官方源码归档。归档根目录包含 `server/`、`public/`、`package.json` 和上游许可证，不含顶层目录。

本目录的 `package.json` 记录对应上游的依赖范围。将源码归档以 `source.tar.gz` 命名，与本目录的 `package-lock.json`、`pairdrop-service-run`、`rtc-config.json`、`apply-local-overlay.py`、`router-compat-client.js`、`router-compat-server.js`、`router-mailbox-client.js`、`router-mailbox-server.js`、`router-mailbox.css`、`router-mailbox.svg`、`deploy-pairdrop.sh` 一起传入 `~/.stage/pairdrop/`。用 Termux 用户执行：

```sh
sh ~/.stage/pairdrop/deploy-pairdrop.sh <source-archive-sha256>
```

脚本安装到独立 release 目录，检查依赖与语法，保存旧版本和启动配置后切换软链接；健康检查失败则恢复旧版本，首次部署失败则禁用新服务。已有 `service.env`、RTC 配置保留。默认健康探测端口为 3000。

`apply-local-overlay.py` 为固定上游安装兼容扩展与文件箱，更新 service worker 缓存版本；脚本可重复执行，遇到不匹配的源码则拒绝修改。`LOCAL_OVERLAY` 记录 `op7-mailbox5-text1-initial-shell1-native-ping1`，service worker 为 `v1.11.2-op7-mailbox5`，第三方原始源码仍不提交 Git。原有 RTC 转发只接受同房间真实成员的请求。文件箱新增前端资源，更新后需重新打开页面。

已有安装需备份 `public/scripts/util.js`、`public/service-worker.js`、`public/scripts/main.js`、两个 `router-*.js` 客户端、`public/styles/router-mailbox.css`、`server/server.js`、`server/ws-server.js`、两个服务端扩展和 `LOCAL_OVERLAY`，记录原先不存在的新文件以便回滚。先执行 `python apply-local-overlay.py <release> --check`，再应用并检查 JS 语法。服务端扩展需要重启一次 PairDrop；应选择没有传输的时候操作。

已安装文件箱后的纯界面更新只需备份并更新 `public/scripts/router-mailbox.js`、
`public/styles/router-mailbox.css`、`public/service-worker.js` 和 `LOCAL_OVERLAY`。
确认后端文件一致即可直接替换静态资源，无需重启服务或迁移设备、文件数据。
首屏清理还需一并部署 `public/index.html`、`public/manifest.json` 和
`public/images/router-mailbox.svg`。先更新客户端脚本，再切换首页，最后更新 service worker；
初始加载提示通过 DOM 兄弟选择器兼容旧版缓存客户端，不依赖新脚本才能隐藏。

服务端测试：`node --test services/pairdrop/router-compat-server.test.mjs`。浏览器验证覆盖正常直连、RTC 卡住、构造失败及不支持 RTC，均需验证设备发现和文件逐字节一致。

心跳集成测试：指定已安装依赖并应用扩展的上游目录，运行
`PAIRDROP_RELEASE=/path/to/release node --test services/pairdrop/transport-heartbeat.test.mjs`。
测试忽略应用心跳时保持在线，并验证协议 pong 真正缺失时仍按原阈值断开。
Termux Node 可增加 `--test-isolation=none` 避免测试子进程启动限制。

实时状态集成测试：应用扩展后运行
`PAIRDROP_RELEASE=/path/to/release node --test services/pairdrop/mailbox-presence-integration.test.mjs`。
验证认证后立即上线、改名通知、多标签页连接和最后一个标签关闭后的离线推送。

文件箱测试：在本目录安装锁定依赖后运行
`node --test router-mailbox-server.test.mjs`。身份凭据保存在浏览器本地，服务端只存令牌
哈希；文件 API 检查收发设备归属，原生下载使用 HttpOnly、SameSite=Strict Cookie。
运行文件目录 0700、文件 0600，不能把真实身份、下载 Cookie、文件内容或运行配置提交 Git。
此实例供可信 LAN/Tailscale 使用，不应直接暴露在公网。

若新 Node 需要较新的 `libc++_shared.so`，可从相应 Termux 包中提取到 `~/router-apps/pairdrop/runtime/lib/`，仅为 PairDrop 设置 `LD_LIBRARY_PATH`，避免替换现有服务共用的库。私有网络配置放在 `~/router-data/pairdrop/service.env`；真实凭据与部署快照仅存 private 的忽略目录。

上游：[PairDrop](https://github.com/schlagmichdoch/PairDrop)。
