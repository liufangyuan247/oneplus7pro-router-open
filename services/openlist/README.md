# OpenList

`openlist-service-run` 是 Termux/runit 启动入口，`inspect-phone-service.sh` 用于检查进程和日志。

## 升级后 Logo 一直转圈

从旧 OpenWrt 配置迁移到官方 OpenList 二进制后，数据库可能仍将 `logo`、`favicon`、`audio_cover` 指向 `/assets/logo.svg`，并在 `customize_head` 中加载 `/assets/polyfill.min.js`。在 v4.2.7 上，这两个失效路径会返回 HTTP 200 的 HTML 页面：图片无法解码，控制台出现 `Unexpected token '<'`。强制刷新不能修复数据库里的旧配置。

`repair-legacy-web-assets.py` 只替换值恰为 `/assets/logo.svg` 的三个设置，并移除上述旧 polyfill 的 script 标签，保留其他自定义内容。Logo 使用校验过的官方 SVG，以 data URI 存入设置，页面加载不再依赖外网图片服务。

将脚本放到手机后，以运行 OpenList 的 Termux 用户执行（需要 Python 3、curl 和 runit）：

```sh
mkdir -p "$HOME/.stage"
curl -fL https://res.oplist.org/logo/logo.svg -o "$HOME/.stage/openlist-logo.svg"
python repair-legacy-web-assets.py --logo "$HOME/.stage/openlist-logo.svg" --check
python repair-legacy-web-assets.py --logo "$HOME/.stage/openlist-logo.svg"
```

脚本内固定 SVG 的 SHA-256 为 `fe09f04c24101c9b4b8baada86cf77913bc50731082b102f61ad6260626b43db`。若上游文件变化，校验失败会在修改前退出，应先检查文件再决定是否更新校验值。

默认数据库是 `$HOME/router-data/openlist/data.db`，服务目录是 `$HOME/router-stack/services/openlist`，验证地址是 `http://127.0.0.1:5244`；可通过 `--home`、`--prefix`、`--base-url` 调整。`--check` 只报告待修复的设置名。已经修复时重复运行不会写库或重启服务。

实际修改会短暂停止 OpenList，在 `$HOME/router-data/backups/openlist/web-assets-*` 中备份完整数据库和原设置，再事务更新并启动服务，验证公共设置 API、首页和管理页。失败时自动恢复数据库并重新启动服务。备份目录权限为 0700，文件为 0600；完整数据库可能含有凭据，应留在设备上，不要提交到 Git。

完成后刷新浏览器，确认 Logo、管理页正常，控制台不再出现旧 polyfill 的解析错误。排查资源加载时同时检查响应类型和内容，不能仅凭 HTTP 200 判断资源存在。
