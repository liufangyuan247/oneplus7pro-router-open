# Rclone 云同步与 WebUI 服务

`rclone-service-run` 是 Termux/runit 启动脚本。

## 服务概要
- **服务类型**：Rclone Remote Control 守护进程与现有 Web 控制台 (`rclone rcd --rc-files=... --rc-serve`)
- **端口**：本机 RC `127.0.0.1:5572`，带认证的 Caddy 入口 `5573`
- **管理地址**：`http://192.168.2.1:5572`（IPv4 防火墙转到认证入口），也可直接访问 `5573`；IPv6 使用 `5573`
- **凭据**：独立随机密码，只保存在私有的 `$HOME/router-data/security/web-ui.env`，与 AriaNg 共用；没有默认密码
- **登录方式**：在 Rclone 网页中手动登录，成功后由网页保存登录状态，刷新和重新打开页面可继续使用；点击退出后清除状态。页面不预填密码，也不强制跳过登录。
- **配置文件**：`$HOME/router-data/rclone/rclone.conf`
- **静态资源缓存**：`$HOME/.cache/rclone/webgui/current/build`
- **服务目录**：`$HOME/router-stack/services/rclone`

必须一并部署 `router-security.sh`、启动入口和 Caddy 认证配置。RC 无认证仅限本机调用，不允许发布到网络。对已有运行任务的进程可先部署认证代理和防火墙，待自然重启后再应用 loopback 监听，无需中断任务。

Caddy 直接提供不含凭据的登录 HTML 和静态资源；所有 RC 接口及远端文件路径仍需认证。`patch-rclone-webgui.py` 会移除旧的固定账号注入和跳过登录逻辑，并清理旧登录状态。

## 升级与回滚

Rclone 1.76.0 移除了旧的 `--rc-web-gui` 启动方式。服务使用新旧版本均支持的
`--rc-files` 提供现有静态页面，保留原访问地址、认证入口和手动登录状态。
已有静态页面必须存在于上述缓存目录，启动时不会自动下载另一套界面。

Router Admin 的升级与回滚由 `$PREFIX/bin/upgrade-rclone.py` 执行。部署时将本目录
同名脚本安装到该位置（0700），并更新 runit 的 `run` 文件。
升级前检查 RC 异步任务、正在传输和校验的文件；非空闲时拒绝升级。
助手下载准确的 APT 候选包，在隔离端口验证候选程序、RC 和现有 WebUI，再备份实际
安装文件为完整恢复包。显式安装可修复旧版“只回滚程序、dpkg 仍记录新版”的不一致。
失败后同时恢复软件包记录、程序与启动脚本，配置和同步数据不变。
恢复文件保存在 `~/router-data/backups/services/rclone/<时间>-apt/`，包含私有升级日志，
不提交 Git。升级锁与 qBittorrent 共用，避免两个助手并发操作包管理器。

## 与 OpenList 本地联动
OpenList 位于本机 `:5244`，在 Rclone 中添加 WebDAV Remote 时推荐配置：
- **Type**: `webdav`
- **URL**: `http://127.0.0.1:5244/dav`
- **Vendor**: `other`
- **User / Pass**: OpenList 管理员账号与密码

通过 127.0.0.1 本地回环传输，跨盘备份（如百度网盘 -> 夸克网盘）无需走外部网络，具备极高的本地中转吞吐与自动断点续传能力。
