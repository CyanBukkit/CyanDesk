# 许可证与版权

本仓库目前是未公开发布的私人草稿。所有者尚未选定公开发布方式。在所有者明确选择之前，不要把本仓库公开，也不要把它说成已经完成许可证审查。

## 本仓库里的代码

`control-plane`、网关、管理后台、安装脚本和本轮 Linux 会话库是 CyanDesk 为这份设计新写的代码，不是把 RustDesk 或 ALVR 换个名字。这些文件里没有复制上述上游的源代码。

推荐但尚未由所有者采纳的兼容方向：若以后把 RustDesk 源码链接或合并进发行物，发行物需要按 AGPL-3.0 履行对应义务。本文件不代替该决定。

## 计划复用、本轮未纳入源码的上游

核实日期：2026-11-23。

| 上游 | 本轮锁定的提交 | 许可证 | 本仓库中的处理 |
|---|---|---|---|
| rustdesk/rustdesk | `66fabdf305eff91385e4944c0565fa8ef5a7b730` | AGPL-3.0 | 只保存官方许可证文本，未放入客户端源码 |
| rustdesk/rustdesk-server | `680b60395ecf99a76f74a1d934e453bb6e5e0aa2` | AGPL-3.0 | 只保存官方许可证文本，未放入 hbbs/hbbr |
| alvr-org/ALVR | `2945d90e6285c4794a7ce89b07d85ca8e5d52185` | MIT | 只保存官方许可证文本，未放入驱动或头显工程 |

许可证副本：

- `docs/upstream/licenses/rustdesk-LICENCE.txt`
- `docs/upstream/licenses/rustdesk-server-LICENSE.txt`
- `docs/upstream/licenses/ALVR-LICENSE.txt`

官方说明：RustDesk 开源服务端是 hbbs/hbbr。集中账号、Web 控制台、API 和自定义客户端生成器在官方 Pro 功能范围内。CyanDesk 自己实现账号、后台和下载登记，不调用 Pro 接口，不使用 Pro 许可证服务。

ALVR 官方 README 写明 PC 端支持 Windows 10/11 与 Linux，不支持 Windows 7/8 和 macOS 主机。头显清单包括 Pico Neo 3/4/4 Ultra 与 Apple Vision Pro。这不是 CyanDesk 已经完成 VR 转发的证据。

## 依赖

Go 模块清单在实现稳定后由 `go list -m all` 生成到 `docs/upstream/go-modules.txt`。管理后台的 npm 依赖以 `admin-web/package-lock.json` 为准。构建时不要把密钥写进镜像。

Android 绑定使用 `golang.org/x/mobile` `v0.0.0-20261117202452-47699113848a`，许可证是 BSD-3-Clause（Go Authors）。会话加密仍是桌面端已经使用的 `github.com/flynn/noise` `v1.1.0`，BSD-3-Clause。没有为手机另写一套 Noise。
