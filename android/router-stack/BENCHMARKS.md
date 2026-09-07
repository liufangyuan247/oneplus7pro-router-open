# OnePlus 7 Pro 软路由基准测试

测试日期：2026-08-22

## 当前实测结果

| 测试 | 实测 | 结论 |
|---|---:|---|
| 虚拟 LAN → WAN，iptables NAT，4 TCP 流 | 25.4 Gbit/s | CPU/内核不是千兆普通 NAT 的瓶颈 |
| 虚拟 WAN → LAN，iptables NAT，4 TCP 流 | 24.1 Gbit/s | CPU/内核不是千兆普通 NAT 的瓶颈 |
| 虚拟 LAN → Mihomo TPROXY → WAN，1 TCP 流 | 12.7 Gbit/s | 本地 TPROXY/Mihomo DIRECT 软件路径高于千兆需求 |
| 虚拟 LAN → Mihomo TPROXY → WAN，4 TCP 流 | 23.7 Gbit/s | 多流软件路径余量充足 |
| 虚拟 WAN → Mihomo TPROXY → LAN，4 TCP 流 | 22.8 Gbit/s | 反向软件路径余量充足 |
| 三星 870 EVO 500GB，4 GiB 顺序写入并落盘 | 75.2 MB/s（71.7 MiB/s） | 当前 NTFS/FUSE 写入路径较慢且有长尾抖动 |
| 同一 SSD，4 GiB 顺序读取 | 232 MB/s（222 MiB/s） | 足以喂满单个千兆 LAN 下载 |
| SSD → 当前电脑，HTTP/1.1，经现有 Wi-Fi | 32.66 MB/s，约 261 Mbit/s | 当前无线链路是该场景瓶颈 |

SSD 为 `/dev/block/sdg`，主数据分区当前挂载在
`/mnt/usb_disk`，文件系统由 Android 以 `fuseblk`
方式读写。测试只创建专用临时文件，测试后已删除。

## 如何理解虚拟 WAN/LAN 结果

`bench-veth-router.sh` 创建两个隔离的 Linux 网络命名空间和两对 veth：

```text
模拟 LAN 客户端 ─ veth ─ Android 路由/NAT ─ veth ─ 模拟 WAN 服务器
```

数据包真实经过 Android 的 IPv4 转发和 iptables MASQUERADE，但不经过 USB
控制器、Hub、PHY 和真实网卡。因此 24–25 Gbit/s 是手机 CPU、内存和内核数据
路径的上限验证，不是最终网口速度预测。

`tools/android/bench-mihomo-tproxy-ceiling.sh` 在同样的双命名空间拓扑中
启动一个临时的同版本 Mihomo，数据真实经过 TPROXY 接收、Mihomo
DIRECT 中继和反向回包。它不热加载或重启生产 Mihomo，退出后删除临时
进程、命名空间、策略路由和 iptables 规则。该结果证明透明代理软件数据
路径不是千兆瓶颈，但仍未包含 USB 网卡、PPPoE 封装、远端代理节点加密
和公网质量。

脚本临时添加两条只匹配 `10.203.0.0/24` 和 `10.204.0.0/24` 的 Android
策略路由规则；退出时会删除命名空间、iptables 规则和策略路由规则。

## 最终硬件下的合理预期

- 普通 IPv4 NAT：两块千兆网卡的实际上限通常约 930–950 Mbit/s，即约
  110–118 MB/s。当前虚拟测试表明手机算力有充足余量。
- PPPoE：虚拟测试没有覆盖 PPPoE 封装；必须等网线和第二网卡到位后实测。
- mihomo/Clash 透明代理：虚拟测试没有覆盖规则匹配、加密和远端节点性能，
  不能用 24 Gbit/s 结果推算代理速度。
- SSD 向有线千兆终端发送：当前 232 MB/s 读取速度高于千兆网络上限，预计
  最终由网口限制在约 105–115 MB/s。
- 有线终端向 SSD 写入：当前 NTFS/FUSE 路径会先限制在约 75 MB/s；服务数据
  若不需要把 SSD 直接插回 Windows，建议最终改用 ext4 后重新测试。
- WAN 满速转发与 SSD 满速传输同时发生时，两块网卡和 SSD 会共享 CM418 的
  USB 3.x 上行链路；虚拟测试无法复现 Hub、USB 中断和总线竞争，必须在第二
  网卡到货后做并发压力测试。

## 复测脚本

- `bench-veth-router.sh`：虚拟双网口 NAT 上限。
- `tools/android/bench-mihomo-tproxy-ceiling.sh`：虚拟双网口 Mihomo TPROXY
  DIRECT 软件路径上限。
- `probe-storage.sh`：识别 USB 设备、外置块设备和挂载点。
- `bench-storage.sh`：在当前 SSD 主分区创建 4 GiB 临时文件，测顺序读写。
- `bench-storage-http.sh`：创建 1 GiB 临时文件并在 TCP 18080 端口临时提供
  HTTP 下载，用另一台终端测端到端吞吐量。

存储脚本当前固定匹配本次 SSD 的挂载 UUID。重新分区或格式化后 UUID 会变化，
运行前必须同步修改 `TARGET_DIR`，否则脚本会安全退出。
