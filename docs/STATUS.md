# 实现状态

日期：2026-11-23（北京时间）。对照的是已批准的设计 v0.1。下面只记录代码现状，不改设计原文。

## 服务器包 0.1.4

Linux amd64 服务器包是 `cyandesk-server-0.1.4-linux-amd64.tar.gz`。里面有编译好的控制面、Rust 网关和已构建的管理页，以及 Dockerfile 和 Compose。二进制模式在本机用隔离端口做过健康检查。Docker 构建和 `compose up` 没有跑，不能写成安装成功。镜像构建需要从 Docker Hub 拉取基础镜像，并下载 Go 模块和 npm 包，不是离线包。

`agent` / `connect` 仍然在拨号时只留一条路径。活动会话迁移还在库测试里，没有接到这两条命令。

这是可编译、可测试的桌面会话原型，不是 12 月 1 日前的完整软件，也不是各平台稳定版。

## 这轮写了什么

- 控制面：SQLite、Argon2id、短时令牌、刷新令牌轮换、设备登记、指纹确认、授权、撤销、审计、制品签名。
- 13111：HTTPS、管理 API、中文管理页。内部票据接口不在公开端口上。没有执行命令的接口。
- 13110：Rust 网关同时听 TCP 和 UDP。没有票据不能中继。UDP 先交换 64 字节 cookie，错误 cookie 的包被丢掉。媒体仍是端到端 Noise 密文。
- 持续桌面会话：Linux 主机按间隔采集 X11，用 libx264 编码，主控解码并画到本地窗口。队列满时丢掉旧画面。只观看和未授权的控制升级由主机拒绝。
- 活动会话迁移：两条已经握手的 UDP 路径同时开着。直连被丢包打死后，JPEG 画面和输入改走中继。直连要重新握手后才恢复。恢复后先保持在中继上，过了迟滞才迁回。序号 6 先到、5 后到时，先注入 5 再注入 6。重复序号不注入。换路径不重置序号。指纹不一致或 prologue 不一致时握手不建立。
- 命令：`agent` 和 `connect` 走同一套入口，采集和窗口按系统分开。Linux 仍用 X11。Windows 10/11 构建使用 GDI BitBlt、SendInput、JPEG 和 Win32 窗口，另有一个服务适配器。
- Windows 7 用单独的 `cyandesk_win7` 构建标签，不在这次的 Windows 二进制里，也没有实现。

最终网关是 Rust 程序 `cyandesk-gateway`。仓库里的 Go 网关只留作对照。说明见 `docs/ADR-gateway-go.md`。

`agent` / `connect` 目前仍在拨号时选定一条路径并关掉另一条。能在丢包后迁回的是 `desktop.Live`，由 `TestLiveUDPMigration` 驱动，还没有接到这两条命令的拨号函数上。

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
| Android 9+ / Pico | 未实现 | 未实现 | 无 |
| iOS / iPadOS / visionOS | 未实现 | 设计上只做主控 | 无 |
| 虚拟显示器 L3、SteamVR / ALVR | 未实现 | 未实现 | 无 |

交叉编译成功不是 Windows 可用的证据。

## 没有做

- 公网 NAT 和真实防火墙没有测。缺测试环境是阻塞项，不是把模拟结果写成通过。
- `agent` / `connect` 还不会在会话中途迁移。
- 没有性能承诺。`--width --height --fps` 只是采集参数。
- RustDesk 没有链进来。ALVR 没有构建。
- 没有操作系统虚拟显示器。
- 这台机器没有 Docker、podman、rootlesskit、slirp4netns，也没有 `/var/run/docker.sock`。没有改宿主机防火墙。Dockerfile 和 Compose 只做了文本核对：网关是 Rust 二进制，13110 同时有 TCP 和 UDP，13112 不映射出来，入口脚本不打印引导凭据。Compose 没有跑过。
- 没有公网部署，没有购买，没有使用真实用户的服务器、设备或账号，没有签名证书。
