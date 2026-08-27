# 内核补丁、构建与刷入

## 目标基线

- 设备：OnePlus 7 Pro GM1910。
- 运行系统：OxygenOS 11，设备 build 标识曾记录为 `GM1910_21_211210`。
- 运行内核：`4.14.190-perf+`。
- 可公开源码：OnePlusOSS `oneplus/SM8150_R_10.0`，commit `5c3a4299de5522b86304ae2daaeb1eedd63c6c24`。
- 源码标记更接近 OOS 11.0.6.1，而现场系统曾是 11.0.5.1 系列，因此必须把 ABI、DTB、ramdisk 和设备实测作为额外 gate。
- 编译器：Android clang `r377782c`（clang 10.0.5）+ Android GCC 4.9 cross toolchains。

本项目不提供可直接刷入的 boot。boot 包含设备/版本相关 kernel、DTB、ramdisk 和 Magisk 状态，必须从自己的设备和匹配 ROM 生成。

## 为什么 CM418 不需要新 r8152

CM418 内置网卡为 RTL8153，USB ID `0bda:8153`。OOS 11 stock kernel 已启用 `CONFIG_USB_RTL8152=y`，可绑定 `r8152`。早期 Android 9/r8152 编译不是最终依赖。

外置 AX88179B 的 USB ID 为 `0b95:1790`。旧内核自带 `CONFIG_USB_NET_AX88179_178A` 不能可靠覆盖 AX88179B 的 vendor 路径，所以最终：

```text
# CONFIG_USB_NET_AX88179_178A is not set
CONFIG_ASIX_AX_USB_NIC=y
# CONFIG_MODULE_SIG_FORCE is not set
CONFIG_NET_SCH_FQ_CODEL=m
```

完整最终 config 和差异位于 `kernel/ax88179b/configs/`。

## 补丁内容

`patches/asix-v4.1.0-oneplus-4.14.patch` 应用于已经复制进 kernel tree 的 ASIX v4.1.0 `ax_main.c`，不是直接应用于原始 OnePlus tree。

它包含：

1. **TX NAPI budget clamp**：一个完成的 USB TX descriptor 可能包含多个 skb，`work_done` 会超过 NAPI 传入 budget。poll callback 返回大于 budget 会触发异常调度行为，因此返回前 clamp。
2. **4.14 timeout API**：为旧内核补足 `txqueue` 兼容变量。
3. **`ndo_select_queue` 签名**：把更新驱动的参数适配到 OnePlus 4.14 的 `accel_priv` / fallback 形式。
4. **skb fragment 访问**：使用 4.14 可用的 `frag->page_offset`。
5. **USB reset 前清理**：`pre_reset` 调用 `ax_disable()`，取消所有仍在飞行的 endpoint request。
6. **post-reset NAPI 状态**：删除错误的 TX 循环启用/禁用，避免 reset 后队列状态失配。

发布前检查：

```bash
git apply --stat patches/asix-v4.1.0-oneplus-4.14.patch
```

当前预期结果为 `ax_main.c` 16 insertions、9 deletions。它只能证明 patch 格式有效；真正的 `--check` 必须在完成 `integrate-driver.sh` 的源码树中运行。

## 输入目录

构建脚本延续现场验证过的布局。以下目录均被 `.gitignore` 排除：

```text
kernel/ax88179b/
  01-device-backup/
    proc-config-oos11.gz
    rollback-magisk-oos11.img
    current-boot-a-v2-magisk30.7.img   # 仅打包脚本使用，必须是自己的
  02-source/
    oneplus-sm8150-SM8150_R_10.0.tar.gz
    oneplus-opensource-kernel-SM8150_R_10.0.tar.gz
    ASIX_USB_NIC_Linux_Driver_Source_v4.1.0.tar.bz2
  03-toolchains/
    clang-r377782c-android11.tar.gz
    aarch64-linux-android-4.9.tar.gz
    arm-linux-androideabi-4.9.tar.gz
  04-tools/
    magiskboot
  05-build/
  06-artifacts/
```

`rollback-magisk-oos11.img` 是恢复保障，不应从第三方下载。`proc-config-oos11.gz` 可从运行设备 `/proc/config.gz` 取得。`magiskboot` 应从你实际使用的 Magisk 版本提取。

## 隔离构建

构建环境固定为 Ubuntu 18.04 digest，工作树和工具链放 Docker named volume，宿主项目只读挂载，输出只写 `06-artifacts`。这避免 Windows 文件名、大小写、symlink 和权限问题污染 kernel tree。

```bash
cd kernel/ax88179b
mkdir -p 01-device-backup 02-source 03-toolchains 04-tools 06-artifacts
docker compose build
docker compose run --rm kernel-builder bash scripts/prepare-workspace.sh
docker compose run --rm kernel-builder bash scripts/integrate-driver.sh
docker compose run --rm kernel-builder bash scripts/configure.sh
docker compose run --rm kernel-builder bash scripts/build-resetfix-fqcodel.sh
```

阶段职责：

- `prepare-workspace.sh`：校验输入、解压源码/工具链、合并 OnePlus overlay、补回遗漏的 uxcore Kbuild glue。
- `integrate-driver.sh`：复制 vendor driver、CRLF 归一化、应用兼容 patch、写 Kconfig/Makefile。
- `configure.sh`：从设备 config 生成构建 config，并断言关键选项。
- `build-resetfix-fqcodel.sh`：构建 Image 和 `sch_fq_codel.ko`，输出 manifest 和 SHA256。
- `package-resetfix-fqcodel.sh`：用自己的 Magisk boot 作为 base，替换 kernel 并核对 ramdisk/DTB。
- `finalize-minimal-magisk-candidate.sh`：在设备上做最小 `skip_initramfs -> want_initramfs` 四字节 patch。
- `verify-v4-final.sh`：比较最终 kernel、Magisk cpio 与候选内容。

`package-resetfix-fqcodel.sh` 中现场 base boot 文件名和 SHA256 只是强约束示例。公开复现时应改为环境变量或先更新为自己备份的精确 SHA256；绝不能删除这项校验后盲目打包。

## 为什么编译没有打满 CPU

Linux kernel 构建不是全程完全并行：配置、依赖扫描、少量链接和 I/O 阶段会限制并发；Docker Desktop/WSL 的 CPU 配额、Windows Defender 扫描、named volume I/O 也会造成空闲。`OP7P_BUILD_JOBS` 只控制 make job 数，不保证所有时刻 100% CPU。

建议：

- 给 Docker/WSL 分配足够 CPU 和内存；
- 源码放 Linux volume，不放 NTFS bind mount；
- 工具链下载完成后离线编译；
- 仅在确认散热和内存足够时增大 `OP7P_BUILD_JOBS`。

编译本身不需要持续外网；只有首次拉取基础镜像和准备源码/工具链需要网络。

## 打包与 Magisk

boot repack 是本项目风险最高的一步。候选必须保持：

- base boot 与当前 slot/ROM 完全匹配；
- ramdisk byte-for-byte 保留（除 Magisk 必需 patch）；
- DTB/boot header/page size 不变；
- Magisk cpio `test` 状态一致；
- 新 kernel release、vermagic 与 config 符合预期；
- 输出大小未超过分区/原 boot 约束。

不要把仓库里的 manifest SHA256 当作你的 boot SHA256；公开仓库不提供对应镜像。

## 临时启动与验证

在 bootloader unlocked 且设备通过 USB 直连电脑时：

```bash
adb reboot bootloader
fastboot devices
fastboot boot 06-artifacts/<candidate>.img
```

不要先 `fastboot flash boot`。启动后依次验证：

```bash
adb shell uname -a
adb shell su -c id
adb shell su -c 'dmesg | grep -Ei "ax_usb_nic|0b95|usb|napi|reset|panic"'
adb shell su -c 'readlink /sys/class/net/eth*/device/driver'
adb shell su -c '/data/data/com.termux/files/usr/bin/ethtool -i eth1'
```

随后测试：

- Android app 能打开，Launcher/Chrome/Termux 不黑屏；
- Magisk root、模块和 service.d 正常；
- CM418 RTL8153 与 AX88179B 同时存在；
- AX88179B 使用 USB configuration 1 和 `ax_usb_nic`；
- PD 受电、Hub host mode、硬盘同时工作；
- 拔插 Hub/网卡后能恢复；
- iperf 双向、PPPoE、息屏和温度稳定；
- dmesg 无 NAPI budget、TX timeout、USB reset loop、panic。

至少完成一次重插和一次正常重启验证，再考虑刷入。刷入前再次保存当前可启动 boot，并确认 MSM/fastboot 恢复路径。

## r8152 历史目录

`kernel/r8152-experiment` 记录 Android 9/NetHunter 基线、隔离构建与功耗思路。它不是当前 OOS 11 的构建入口，其源 checkout 曾不完整，也没有可靠的最终 patch。保留它是为了说明失败路径，不应对它运行生产刷入流程。

## 许可证

Linux kernel 与本补丁按 GPL-2.0-only。ASIX vendor driver 的获取与再分发条件由 ASIX 上游决定，因此仓库只包含本项目 patch，不复制其完整源码。
