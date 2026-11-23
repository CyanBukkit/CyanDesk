# 验收跟踪

日期：2026-11-23。状态只用：已实现、已测试、未实现、阻塞。已测试只表示这台隔离 Linux 上的自动化结果。

| 组 | 要求 | 状态 | 证据 |
|---|---|---|---|
| A 账号 | 初始化、登录、设备待批准、撤销、刷新重用、日志无秘密 | 已测试 | `go test ./internal/api ./internal/clientcore`，以及本机 CLI 日志 |
| B 连接 | 中继与直连并行，按探测选路，无票据拒绝 | 已测试（仅本机 TCP） | `go test ./internal/gateway ./internal/remote` |
| B UDP | 等长 cookie | 已测试（仅 127.0.0.1） | `go test ./internal/punch` |
| C 迁移与输入去重 | 迟滞策略与重复序号 | 策略已测试；真实网络迁移未实现 | `go test ./internal/link ./internal/input` |
| D 画质 | 预设与弱网矩阵 | 未实现测量。H.264 单帧往返已测试 | `go test ./internal/media` |
| E 只观看 | 主机拒绝注入 | 已测试（X11） | `go test ./internal/capture` |
| E 管理员不能观看 | 管理接口无画面 | 已实现。管理列表不含票据 | API 测试与管理页 |
| F L1 | 裁剪坐标 | 仅数学函数已测试 | `go test ./internal/display` |
| F L2 | 真实多屏 | 只在 Xvfb 上看到一块屏 | `ListScreens` |
| F L3 | 操作系统虚拟屏 | 未实现 | 无驱动 |
| G 平台 | Win7/10/11、macOS、Android、Pico、Apple 仅主控、Linux 服务 | 未实现。Linux 命令行登录已测试 | 无对应设备 |
| G 无头 Linux | 不伪造画面；文件路径不能逃出根目录 | 路径约束已测试。远程命令执行未实现 | `go test ./internal/files` |
| H VR | SteamVR / ALVR | 未实现 | 许可证已归档，没有构建 |
| I 安装 | 13110/13111、签名制品、卸载保留数据 | 13111 与 13110/TCP 已在本机监听测试。Docker 阻塞：没有守护进程。制品签名函数已测试 | 命令行日志；`deploy/compose.yaml` 未实际运行 |

安全上还没有做的：公网 NAT 类型矩阵、票据重放以外的完整攻击演练、更新安装器、平台签名。
