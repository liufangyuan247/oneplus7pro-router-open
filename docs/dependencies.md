# 依赖下载与校验

仓库只发布集成代码。大文件从各自上游下载，固定版本后在本地校验 SHA256。不要从网盘或不明镜像下载 boot、Magisk、MSM 或工具链。

## Userspace 组件

| 组件 | 已验证版本 | 上游/下载方案 |
| --- | --- | --- |
| Termux | `0.118.3` GitHub debug arm64-v8a | [termux/termux-app Releases](https://github.com/termux/termux-app/releases)；Android 11 使用 GitHub/F-Droid 构建，不使用已停止更新的 Play 商店旧版 |
| OpenList | `v4.2.5` linux-musl-arm64 | [OpenListTeam/OpenList Releases](https://github.com/OpenListTeam/OpenList/releases) |
| AriaNg | `1.3.14` AllInOne | [mayswind/AriaNg Releases](https://github.com/mayswind/AriaNg/releases) |
| Mihomo | `v1.19.30` android-arm64-v8 | [MetaCubeX/mihomo Releases](https://github.com/MetaCubeX/mihomo/releases)；需要新协议时可升级，但先离线验证订阅 |
| Tailscale | `1.102.3` arm64 | [Tailscale stable packages](https://pkgs.tailscale.com/stable/) |
| CLIProxyAPI | `v7.2.140` linux-aarch64 no-plugin | [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)；Android 兼容构建见 `services/cpa` |
| CPA Usage Keeper | `v1.14.8`, commit `c0129050def1f3e980ed68492014326d35710917` | [Willxup/cpa-usage-keeper](https://github.com/Willxup/cpa-usage-keeper)；Android 上按集成脚本源码构建 |
| scrcpy | Windows `v4.1`；web server 组件按管理端代码要求 | [Genymobile/scrcpy Releases](https://github.com/Genymobile/scrcpy/releases)；仓库不包含 EXE/DLL/server |
| MetaCubeXD | 安装时固定 release/commit | [MetaCubeX/metacubexd](https://github.com/MetaCubeX/metacubexd) |

aria2、Caddy、OpenSSH、dnsmasq、pppoe、socat、jq 等优先通过 Termux `pkg` 安装，让包管理器负责签名与升级。不要把 `$PREFIX/bin` 整个复制进 Git。

## 内核输入

| 输入 | 获取方法 |
| --- | --- |
| OnePlus SM8150 R kernel | [OnePlusOSS/android_kernel_oneplus_sm8150](https://github.com/OnePlusOSS/android_kernel_oneplus_sm8150)，checkout/归档 `oneplus/SM8150_R_10.0` 与文档指定 commit |
| OnePlus overlay archive | 与上述官方源码发布配套的 `oneplus-opensource-kernel-SM8150_R_10.0` 内容 |
| ASIX driver v4.1.0 | [ASIX AX88179B support/download](https://www.asix.com.tw/en/product/USBEthernet/Super-Speed_USB_Ethernet/AX88179B)；接受其下载条件后取得 Linux source v4.1.0 |
| clang r377782c | Android 11 对应的 `platform/prebuilts/clang/host/linux-x86`，打包 `clang-r377782c` 目录 |
| GCC 4.9 toolchains | Android `platform/prebuilts/gcc/linux-x86/aarch64/aarch64-linux-android-4.9` 和 `arm/arm-linux-androideabi-4.9` |
| `magiskboot` | 从自己实际安装的 Magisk APK/设备 `/data/adb/magisk` 提取；版本必须与 boot 修补流程一致 |
| boot/config | 从自己的、与目标 ROM/slot 匹配的设备取得 |

Android Gooblesource 可用 `git clone` 后只归档需要的子目录，或使用官方 `+archive` 接口。不同归档工具产生的 tar gzip 元数据不同，因此“解压内容相同”时 archive SHA256 也可能不同。`checksums/dependencies.sha256` 记录的是本项目实测输入文件 SHA256；若从官方源重新打包，应另外记录 commit、目录内容 hash 和新的 archive hash。

## 已验证文件 SHA256

```text
72fdb596045116bf5ba1b5bdf5b26fddb9acc0bd074ad9f2da9eb0ae85e83a4e  termux-app_v0.118.3+github-debug_arm64-v8a.apk
a0fa1b154af8c61f862a2259f559f7396d96c0225f4a863eae2333e1546bbe25  tailscale_1.102.3_arm64.tgz
19afeb40fca190fc2e3906a4e3b87c74a0c2120626fd3cb3ae0cf4092cb780ad  mihomo-android-arm64-v8-v1.19.30.gz
65bc5ed3573ef05313ea953a5c5363c8b33a4996849b2986c78660eab1a9edb2  AriaNg-1.3.14-AllInOne.zip
82d1c8b10d2cd75fca7889610a6aadc6035605045affd5a80d9ed63e5e71e64a  openlist-linux-musl-arm64-v4.2.5.tar.gz
4a825e2a10e723788fb4e4b567349e1d3ccfad118ba80863541d96cc674caa70  CLIProxyAPI_7.2.140_linux_aarch64_no-plugin.tar.gz
f4b5446e2114bd216274a98e81a634b9b669ac3efeb42f366ca1ab40b6391919  cpa-usage-keeper-v1.14.8-android-source.tar.gz
fd650b715ee41d2a871e7a1c5ab5c42e01aa5a252cc9a4b4fcdb89f1287482b6  ASIX_USB_NIC_Linux_Driver_Source_v4.1.0.tar.bz2
dfac13612ecbbe9567ccc5fb6ed0fe3bce367cecc848b17c3064affe3d7ca6ae  oneplus-sm8150-SM8150_R_10.0.tar.gz
da80b1675fb1098bbd47ec3c6683a2563e5dcc4b7bc8217438682c2176bf8516  oneplus-opensource-kernel-SM8150_R_10.0.tar.gz
d1f60886d517acd282b26c10a1877a662e28b8b048a8530744feedafd56e07d8  clang-r377782c-android11.tar.gz
30b45b1f97164fbc9c3118a68667a55f4957c0ba606562540e23c4c8830b69d6  aarch64-linux-android-4.9.tar.gz
c18381d2980f59a1cd15e606a1cef755978badbb64f128cea35d90905c671c7e  arm-linux-androideabi-4.9.tar.gz
a18ecbd7981179494b7d281453d6c4e25b5c719e7d2ef7f6eba3c6be3043c58e  magiskboot
```

这些 hash 不是“最新版本推荐”，而是复现实测环境的锁定值。升级任一组件后，先更新私有测试环境、执行完整验收，再修改公开锁定记录。

## 校验命令

Linux/Termux：

```sh
sha256sum -c checksums/dependencies.sha256
```

PowerShell 可使用仓库的校验脚本：

```powershell
.\tools\verify-dependencies.ps1 -DownloadDirectory C:\path\to\downloads
```

脚本只校验已经存在的文件；不会自动下载固件、boot 或受下载条款约束的 ASIX 包。
