# 验收跟踪

日期：2026-11-23。状态只用：已实现、已测试、未实现、阻塞。已测试只表示这台隔离 Linux 上的自动化结果。

| 组 | 要求 | 状态 | 证据 |
|---|---|---|---|
| A 账号 | 初始化、登录、设备待批准、撤销、刷新重用、日志无秘密 | 已测试 | `go test ./internal/api ./internal/clientcore`，以及本机 CLI 日志 |
| B 连接 | Rust 网关转发密文，坏票据拒绝，坏 cookie 不转发 | 已测试，对端是 Go 客户端 | `TestRustGatewayRelaysCiphertextAndUDP` |
| B 私有网络候选 | 两个命名空间，延迟中继与直连 UDP 同时测量 | 已测试。这是私有实验网，不是公网 NAT，也不作性能结果 | `TestPrivateNetworkCandidates` |
| B 模拟 NAT | 命名空间内用户态改写源地址 | 已测试。不是内核 NAT，不是公网 NAT | `TestEmulatedUserspaceNAT` |
| B 公网 NAT | 真实运营商 NAT | 未测试，缺公网环境 | 无 |
| C 迁移与输入去重 | 活动会话从直连 UDP 迁到中继再迁回；乱序只注入一次 | 已测试，网络是本机 UDP。丢包后的直连是重新握手。`agent` 命令还没有接上 | `TestLiveUDPMigration` |
| D 画质 | 预设与弱网矩阵 | 未实现测量。H.264 单帧往返已测试 | `go test ./internal/media` |
| E 只观看 / 升级 | 主机拒绝注入，拒绝未授权的控制升级 | 已测试（持续会话） | `TestContinuousRemoteDesktop` |
| E 管理员不能观看 | 管理接口无画面 | 已实现。管理列表不含票据 | API 测试与管理页 |
| F L1 | 裁剪坐标 | 仅数学函数已测试 | `go test ./internal/display` |
| F L2 | 真实多屏 | 只在 Xvfb 上看到一块屏 | `ListScreens` |
| F L3 | 操作系统虚拟屏 | 未实现 | 无驱动 |
| G 平台 | Win7/10/11、macOS、Android、Pico、Apple 仅主控、Linux 服务 | Linux 命令行已测试。Windows 10/11 的 GDI、SendInput、窗口和服务适配器已编进二进制，未在 Windows 上运行。Win7 是单独标签，未实现。其余平台未实现 | 见 `docs/STATUS.md` |
| G 无头 Linux | 不伪造画面；文件路径不能逃出根目录 | 路径约束已测试。远程命令执行未实现 | `go test ./internal/files` |
| H VR | SteamVR / ALVR | 未实现 | 许可证已归档，没有构建 |
| I 安装 | 13110/13111、签名制品、卸载保留数据 | 二进制服务器包已在本机启动，健康检查通过，13112 不在公网端口上。Docker 未测试，没有 compose up。制品签名函数已测试 | `deploy/binary-health.sh`；`TestDockerfileAndCompose` |

安全上还没有做的：公网 NAT 类型矩阵、票据重放以外的完整攻击演练、更新安装器、平台签名。
