# 上游可行性（2026-11-23）

本文件是开工时的核实记录，不是功能完成声明。

## 已锁定的提交

这些是 2026-11-23 从 GitHub 默认分支读取到的提交，用来固定讨论对象。它们不是已经合并进 CyanDesk 的引擎版本。

- RustDesk 客户端 `66fabdf305eff91385e4944c0565fa8ef5a7b730`（2026-11-23T01:34:55Z）
- RustDesk Server OSS `680b60395ecf99a76f74a1d934e453bb6e5e0aa2`（2026-11-08T07:33:32Z）
- ALVR `2945d90e6285c4794a7ce89b07d85ca8e5d52185`（2026-11-08T14:29:23Z）

## 本轮没有把 RustDesk 编进产品的原因

RustDesk 客户端是 Flutter 加 Rust 的完整桌面产品，服务端 OSS 是 rendezvous/relay，没有设计所要求的账号、设备批准、管理 API 和自定义客户端生成。官方把这些放在 Pro。设计要求的是独立控制面，加上对引擎的桥接，而不是换品牌。

本轮没有把该仓库拉下来改，也没有假装已经完成 fork。桥接接口按设计里的命令名留在 `docs/protocol/bridge.md`。当前 Linux 图形路径使用本机 ffmpeg/x11grab/xdotool，只证明这条机器上的采集、编码、加密传输和输入门闩，不代表 RustDesk 的编码器、权限模型或跨平台能力。

后续把 RustDesk 接进来时，必须单独记录：实际链接了哪些包、是否构成 AGPL 衍生作品、以及仍缺的账号无关能力。不能用本轮的 ffmpeg 路径代替那个结论。

## ALVR

本轮没有编译 ALVR，没有 SteamVR 驱动，也没有头显。VR 仍是独立工作流。桌面视频会话不能记成 VR 通过。

## 网关语言

发布网关是 Rust 程序 `cyandesk-gateway`。Go 网关留在仓库里作对照，不作为发布入口。线协议以 `docs/protocol/cyandesk-1.md` 为准。

## 本机工具链

已在当前隔离环境确认：Go、Rust、Node、ffmpeg（libx264、libopus、x11grab）、Xvfb、xdotool、xrandr、gcc、libX11。没有 Docker、podman、rootlesskit 或 slirp4netns。没有 macOS、Windows、Android 或头显。Windows 二进制只在这台 Linux 上交叉编译，没有在 Windows 上运行。
