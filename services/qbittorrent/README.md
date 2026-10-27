# qBittorrent 软件包升级

Router Admin 的 qBittorrent 升级通过 `upgrade-qbittorrent.py` 执行。Termux 的 qBittorrent、Qt、libtorrent 和 libc++ 必须配套；仅升级程序可能因 Qt 符号缺失而无法启动，仅恢复程序又会让实际版本和 dpkg 记录不一致。

## 升级与恢复

- 下载 APT 当前候选的 `qbittorrent-nox`、`qt6-qtbase`、`libtorrent-rasterbar`、`libc++`，不做系统全量升级。
- 解包到独立目录，使用独立库路径、独立配置和回环 18080 端口启动候选，核对真实版本 API。此时不停止现网 qBittorrent。
- 根据已安装文件重建四个旧软件包，保留权限、符号链接及维护脚本。若旧程序曾单独回滚，恢复包使用实际程序版本。
- 停止服务，备份配置及 `BT_backup` 恢复元数据，再安装已经预检的包。
- 同时确认实际程序版本、运行 PID 的 executable、WebUI 和 dpkg 记录；失败时恢复完整包集合和配置。下载文件不在恢复或删除范围内。
- 使用文件锁避免本助手的升级和回滚并发执行。运行助手期间不要另外运行 APT/dpkg。

备份在 `$HOME/router-data/backups/services/qbittorrent/<时间>-apt/`。`upgrade.log`、配置和 BT 元数据可能含敏感信息，只保留在设备或 private 的忽略目录，勿提交。

```sh
python "$PREFIX/bin/upgrade-qbittorrent.py"
python "$PREFIX/bin/upgrade-qbittorrent.py" --rollback "$HOME/router-data/backups/services/qbittorrent/<时间>-apt"
```

旧的仅程序备份不支持完整软件包回滚，管理 API 会明确拒绝。

## 部署

先构建包含 `service_upgrade_helper.go` 的 Android ARM64 PIE 管理后台，将其命名为 `router-admin-qbittorrent`，与本目录助手放入设备 `~/.stage/qbittorrent-upgrade/`。把 `deploy-upgrader.sh` 复制到设备，传入后台和助手各自的 SHA-256：

```sh
sh deploy-upgrader.sh <后台SHA256> <助手SHA256>
```

脚本备份并原子替换后台及助手，健康检查失败时恢复，同时核对已有订阅缓存未变。部署本身不升级 qBittorrent；升级由管理页面或助手单独触发。

## 验证

在 Termux 运行 `python test_upgrade.py`，验证版本解析、恢复包权限和链接、路径边界及保留下载文件的回滚。后台执行 `go test ./...`。

2026-10-27 真机验证：qBittorrent 5.3.0、Qt 6.12.0、libtorrent 2.1.2、libc++ 30，实际服务与软件包版本一致，WebUI 正常；再次调用升级 API 返回“已经是最新版本”。
