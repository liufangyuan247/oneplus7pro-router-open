# 安全、备份与开源发布

## 威胁边界

这台设备同时拥有 root、家庭默认网关、硬盘、代理账号、Tailscale identity 和远程终端。任何一个 Web 服务暴露过宽，都可能扩大到整个 LAN。因此“家里只有我用”不能代替认证和最小权限。

## 生产配置规则

- PPPoE、订阅、API key、RPC secret、后台密码只放私有 data/config 文件，权限 600。
- SSH 仅允许公钥，关闭密码登录；不要提交 `authorized_keys`，由部署者安装自己的 key。
- Mihomo controller 保持 `127.0.0.1`；需要远程管理时由认证后台反代。
- aria2 RPC 必须有随机 secret；AriaNg 通过同源反代访问，公开模板不嵌入 token。
- Router Admin 首次启动生成随机密码；生产环境应置于 LAN/Tailscale，并限制暴露接口。
- CPA、OpenList、Usage Keeper 都使用各自认证，数据库不进入代码仓库。
- Tailscale auth key 使用 ephemeral/reusable 策略时按上游建议管理，state 目录权限 700。
- 关闭 OTA 自动安装，避免 unattended boot/kernel 被系统更新替换；安全更新应先在临时 boot/旁路环境验证。

## 网络脚本回滚模型

稳定文件集合：

```text
router-network            当前候选/稳定版本
router-network.backup     上一个已知稳定版本
router-network.pending    已安装、待试运行
router-network.trial      本次启动已经进入试运行
```

更新器只在当前路由健康、候选 shell 语法和 `validate` 全部通过时 staging。下一次 boot：

- 有 pending、无 trial：创建 trial，启动候选，并等待确认；
- pending + trial 再次出现：上一次没有确认，启动早期恢复 backup；
- watchdog 超时：恢复 backup 并重启稳定 supervisor；
- 管理员确认健康：删除 pending/trial，候选成为稳定版。

回滚 guard 本身比普通脚本更敏感。更新 guard 时要使用独立 backup 和独立 trial，不要让“修复回滚机制”的失败同时破坏回滚机制。

## 开源仓库允许和禁止的内容

允许：

- 自主开发的 shell、Go、TypeScript 和 PowerShell；
- `.example` 配置；
- Linux kernel patch、config diff、构建/验证脚本；
- 只含公开上游 URL、版本和 SHA256 的依赖清单；
- 不含设备身份的测试夹具。

禁止：

- `boot*.img`、OPS/OFP、firmware、ramdisk、DTB、MSM 包；
- APK、EXE、DLL、`.ko`、第三方 ELF、完整工具链；
- OpenList/CPA/Tailscale/Mihomo 数据库和生成配置；
- SSH 私钥、authorized_keys、Tailscale state/auth key；
- PPPoE 用户名密码、订阅 URL、节点、provider token；
- 真实 MAC、硬盘 UUID、主机名、设备序列号；
- 日志、截图、性能 CSV、ADB dump、`/proc` 现场快照；
- `node_modules`、`.next`、Go binary、Docker volume 和 kernel checkout。

## 发布前验收

只扫描 `git ls-files`，不要扫描并输出整个私有工作目录的匹配内容。推荐：

```bash
git grep -n -I -E 'BEGIN (RSA |OPENSSH |EC )?PRIVATE KEY'
git grep -n -I -E '(PPPOE_PASS|PPPOE_USER|api[_-]?key|access[_-]?token|password|secret)'
git grep -n -I -E 'https?://[^[:space:]"'"']+[?&](token|key|secret|auth)='
git ls-files -z | xargs -0 -I{} sh -c 'test $(wc -c < "$1") -le 5242880 || echo "$1"' _ {}
```

第二条会命中模板字段和正常代码，必须人工判断；不要简单地“无结果才通过”。另行检查：

- 所有 URL query 是否为公开文档参数；
- 示例 MAC 是否为 `02:00:00:...` locally administered；
- IP 是否只是文档网段、LAN 默认值或 Tailscale CGNAT 网段；
- SHA256 是否仅对应公开依赖，不对应私有 boot/ramdisk；
- Git 历史是否曾提交过秘密。删除当前文件不能清除历史，泄露后必须轮换凭据并重写历史。

## GitHub 发布建议

1. 在本独立目录初始化 Git，不在包含固件/备份的父目录初始化。
2. `git add -n .` 先查看拟加入内容。
3. 完成 Go、前端、shell 语法和 patch 格式测试。
4. `git add .` 后再次以 `git ls-files` 做大小和敏感模式检查。
5. 首次 commit 后再创建空 GitHub repository 并 push。
6. 启用 GitHub secret scanning、Dependabot 和 branch protection。
7. Release 只发布自有源码；第三方和内核输入指向上游。

如果之后要发布自编 boot，应另建 release 流程，明确机型/build/slot/Magisk 前提、源码与 GPL 对应关系，并提供 recovery；本仓库默认不这样做。
