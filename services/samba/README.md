# Samba 文件共享

为有 root 权限的 Android / Termux 路由器提供 SMB2/3 文件共享。默认仅绑定回环与 LAN `eth1`，允许 `192.168.2.0/24`，TCP 445；更换网络时应先调整 `bootstrap.py` 的接口和网段。

Windows 文件资源管理器地址栏输入 `\\192.168.2.1\Shared`，用户名 `router`。首次安装默认使用安装时 Router Admin 的密码；之后修改管理后台密码不会自动修改 Samba 密码。共享对应手机目录 `/storage/emulated/0/RouterShare`，不包含云盘挂载、外接盘和其他手机目录。

## 安装

1. Termux 执行 `pkg install samba`，授予 Termux 共享存储访问权限，并确认 `su` 可用。本方案验证版本为 Termux Samba 4.25.0。
2. 将本目录脚本复制到 `~/.stage/samba/`，执行 `sh ~/.stage/samba/install-service.sh`。依赖现有 `~/router-stack/services` runit 服务树。
3. `bootstrap.py` 拒绝覆盖已有配置；配置、账号数据库与 root 启动脚本位于 `/data/adb/router-samba/`，父目录权限 0700。也可在 root 初始化进程中设置 `SMB_PASSWORD_FILE` 指向仅本机可读的密码文件。

Termux Samba 的 Android NSS 补丁将 Unix 身份固定为 root，并移除了常规的用户切换；因此此部署以 root 启动，SMB 别名 `router` 映射到 Samba 数据库的 `root` 账号。这不是可用于隔离多个 Unix 用户的部署。通过独立共享目录、禁止符号链接和 wide links、禁止访客、强制签名限制访问；不开放 SMB1、NetBIOS 或 WAN 接口。

协议范围为 SMB 2.1–3.1.1，要求签名；支持客户端请求加密，但不默认强制加密。配置中的 TDB 兼容选项解决 Android 构建不支持共享互斥锁的初始化问题；启动日志仍可能出现 `mutexes requested but not available`，应以认证和实际读写结果判定可用性。

## 管理与验证

Router Admin → 服务 → 文件共享 → Samba 提供连接说明、启停、重启和日志。服务名为 `samba`；runit 包装器转发停止信号到已核实的 root PID。日志轮转为 4 × 1 MiB。

部署验证包括：Windows 原生 SMB 使用密码登录、中文小文件读写与清理；SMB3 客户端登录；访客不能访问共享；服务停止后端口关闭且无遗留 smbd，启动及重启后端口恢复。为避免影响正在运行的任务，不执行大文件吞吐测试。

后台新增服务卡片使用 `admin/backend/deploy-samba-service-card.sh`，参数为暂存后端程序及前端压缩包的 SHA-256。部署带备份、健康检查和回滚，并校验原订阅缓存未变化。
