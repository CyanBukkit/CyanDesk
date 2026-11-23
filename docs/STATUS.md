# 实现状态

日期：2026-11-23（北京时间）。对照的是已批准的设计 v0.1。下面只记录代码现状，不改设计原文。

## 测试基线

正在给用户测试的包仍是 0.1.5。校验写在 `docs/release/0.1.5-baseline.md`。不要用后面的构建替换那两个文件，也不要把内部改动说成用户必须马上更换的版本。0.1.5 的 UDP 头问题按复现结果是直连路径的可用性影响，不是解密或注入。这不表示 0.1.5 已经过审计。

## 当前

Android 不再只是登录壳。`mobile` 包走现有控制面的登记、票据和 Noise，并被 gomobile 绑进调试 APK。被控端在系统录屏同意之后用 MediaCodec 编码，主控端把 Annex-B 解到 Surface。输入只在票据含 `input` 且用户打开了无障碍服务时注入。细节和 Android 9 与更新版本的权限差别在 `docs/android.md`。

`TestEmulatorShowsRealFramesBothWays` 在 Android 9 模拟器上通过。桌面 ffmpeg 的红绿蓝经 Rust 网关显示在 Surface 上；同意系统录屏后，MediaCodec 的红绿蓝被桌面解出。只观看不会产生点击。关闭会话后采集服务退出，观看端停止解码。模拟器的无障碍列表滑不动，控制手势没有执行，也没有改 secure settings。这不是手机验收。直连 UDP 仍未做。Android 10/13/14 只有 JVM 里的权限判断，没有对应模拟器。macOS、Windows 7、Apple 系统和虚拟显示器仍然没有实现。Windows 10/11 仍只是交叉编译。ALVR 包仍只解析帧头，不是 SteamVR。

## 0.1.5

`agent` 和 `connect` 会同时打开已认证的中继和直连 UDP。`TestExecutableAgentMigratesThroughRustGateway` 用编译出的程序、Xvfb、颜色窗口和 Rust 网关测过：先走上直连，打断直连后画面和点击改走中继，恢复后再迁回，关闭会话后主机打印授权撤销。直连 UDP 使用显式 nonce 的 Noise 数据报，丢包不重新握手。

服务器包是 `cyandesk-server-0.1.6-linux-amd64.tar.gz`。在这台机器上安装了 Docker 29.1.3 和 Compose 2.40.3，手动启动 dockerd 后 `docker compose build` 和 `up -d` 成功。`https://127.0.0.1:13111/health/live` 返回 live，证书 SAN 含启动时给定的名字，bootstrap 文件权限是 0600，重启后证书指纹不变，13112 没有映射到宿主机。第一次把 dockerd 设成 `--bridge=none` 时构建失败，错误是 `network bridge not found`。Windows 的 `cyandesk gui` 已交叉编译，没有在 Windows 上运行。公网 NAT 没有测。0.1.4 与 0.1.5 的数据目录结构没有被删表迁移，升级保留原卷。

这是可编译、可测试的桌面会话原型，不是 12 月 1 日前的完整软件，也不是各平台稳定版。

## 这轮写了什么

- 控制面：SQLite、Argon2id、短时令牌、刷新令牌轮换、设备登记、指纹确认、授权、撤销、审计、制品签名。
- 13111：HTTPS、管理 API、中文管理页。内部票据接口不在公开端口上。没有执行命令的接口。
- 13110：Rust 网关同时听 TCP 和 UDP。没有票据不能中继。UDP 先交换 64 字节 cookie，错误 cookie 的包被丢掉。媒体仍是端到端 Noise 密文。
- 持续桌面会话：Linux 主机按间隔采集 X11，用 libx264 编码，主控解码并画到本地窗口。队列满时丢掉旧画面。只观看和未授权的控制升级由主机拒绝。
- 活动会话迁移：两条已经握手的 UDP 路径同时开着。直连被丢包打死后，JPEG 画面和输入改走中继。直连要重新握手后才恢复。恢复后先保持在中继上，过了迟滞才迁回。序号 6 先到、5 后到时，先注入 5 再注入 6。重复序号不注入。换路径不重置序号。指纹不一致或 prologue 不一致时握手不建立。
- 命令：`agent` 和 `connect` 走同一套入口，采集和窗口按系统分开。Linux 仍用 X11。Windows 10/11 构建使用 GDI BitBlt、SendInput、JPEG 和 Win32 窗口，另有一个服务适配器。
- Windows 7 用单独的 `cyandesk_win7` 构建标签，不在这次的 Windows 二进制里，也没有实现。

最终网关是 Rust 程序 `cyandesk-gateway`。仓库里的 Go 网关只留作对照。说明见 `docs/ADR-gateway-go.md`。`agent` 和 `connect` 保持中继和直连两条已认证路径，见上面的 0.1.5 节。

## 已测试

`go test ./...` 在这台 Linux 上通过。

- 持续会话仍是本机 TCP：多帧变色、授权点击、只观看拒绝、断线去重。
- 私有命名空间 10.1.0.2 与 10.2.0.2 的路由实验仍然只是私有实验网，不是 NAT，也不拿探测耗时当性能结果。
- `TestLiveUDPMigration` 在本机 UDP 上完成了上面的迁移。记录在 `migration.json`：`network=localhost-udp`，`nat=not-nat`，`performance_claim=false`。探测耗时只决定是否切换。
- `TestEmulatedUserspaceNAT` 在新的网络命名空间里用用户态转发改写了源端口，回程可达。内核 nat 表不可用，没有安装规则。这不是公网 NAT，也不是 10.1/10.2 那次路由。
- 账号、指纹、坏票据、坏 cookie 测试仍然通过。

## 平台

| 系统 | 主控 | 被控 | 这次的证据 |
|---|---|---|---|
| Linux | `connect` + X11 窗口 | `agent` + X11/ffmpeg | 在这台机器上测试过 |
| Windows 10/11 | Win32 窗口代码 | GDI BitBlt、SendInput、服务适配器 | 交叉编译出了 PE32+ 二进制。没有在 Windows 上运行 |
| Windows 7 | 不在上述二进制里 | 同左 | 未实现，未测试 |
| macOS Intel / Apple Silicon | 未实现 | 未实现 | 无 |
| Android 9+ / Pico | Android 9 模拟器上 Surface 解出了桌面的红绿蓝 | 同一模拟器在系统录屏同意后用 MediaCodec 送出红绿蓝。观看不注入。控制手势没跑到 | 不是手机或 Pico。无障碍开关在这个模拟器里点不到。直连 UDP 未做。Android 10/13/14 未开机 |
| iOS / iPadOS / visionOS | 未实现 | 设计上只做主控 | 无 |
| 虚拟显示器 L3、SteamVR / ALVR | 未实现 | 未实现 | 无 |

交叉编译成功不是 Windows 可用的证据。

## 没有做

- 公网 NAT 和真实防火墙没有测。缺测试环境是阻塞项，不是把模拟结果写成通过。
- 没有性能承诺。`--width --height --fps` 只是采集参数。
- RustDesk 没有链进来。ALVR 只有帧头对照，没有 SteamVR 运行时。
- 没有操作系统虚拟显示器。
- Docker 曾在这台隔离机器上构建并做过本机冒烟，不是用户的服务器，也没有改宿主机防火墙。
- 没有公网部署，没有购买，没有使用真实用户的服务器、设备或账号，没有签名证书。
