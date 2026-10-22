# Rclone 云同步与 WebUI 服务

`rclone-service-run` 是 Termux/runit 启动脚本。

## 服务概要
- **服务类型**：Rclone Remote Control 守护进程与 Web 控制台 (`rclone rcd --rc-web-gui`)
- **端口**：`5572` (TCP)
- **管理地址**：`http://192.168.2.1:5572` (或带 Token 直达：`http://192.168.2.1:5572/?login_token=YWRtaW46YWRtaW4%3D`)
- **默认凭据**：用户名 `admin`，密码 `admin`
- **配置文件**：`$HOME/router-data/rclone/rclone.conf`
- **静态资源缓存**：`$HOME/.cache/rclone/webgui/current/build`
- **服务目录**：`$HOME/router-stack/services/rclone`

## 与 OpenList 本地联动
OpenList 位于本机 `:5244`，在 Rclone 中添加 WebDAV Remote 时推荐配置：
- **Type**: `webdav`
- **URL**: `http://127.0.0.1:5244/dav`
- **Vendor**: `other`
- **User / Pass**: OpenList 管理员账号与密码

通过 127.0.0.1 本地回环传输，跨盘备份（如百度网盘 -> 夸克网盘）无需走外部网络，具备极高的本地中转吞吐与自动断点续传能力。
