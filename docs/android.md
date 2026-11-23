# Android 9+ 会话

这是普通 Android 应用。Pico 如果能安装普通 APK，就按 Android 用。没有 OpenXR，没有 6DoF，没有头显运行时。`vr/alvr-bridge` 仍只是 ALVR 帧头的对照，不是 SteamVR 集成。

## 入口

登录、登记、票据、同意和 Noise_IK 都在 `mobile` 包里，由 `android/bind.sh` 绑进 `android/app/libs/cyandesk.aar`。绑定用的是 `golang.org/x/mobile`（BSD-3-Clause）和已经在桌面客户端里的 `github.com/flynn/noise`（BSD-3-Clause）。没有第二套握手。

调试包是 `cyandesk-android-session-debug.apk`。它不是 0.1.5 的替代，也不该覆盖 0.1.5 的服务器包。

## 会话在做什么

- 主控：向现有控制面申请会话，核对设备指纹，拨已经编进服务器的 Rust 网关，用 MediaCodec 把收到的 Annex-B H.264 解到 Surface。
- 被控：你先按指纹同意。控制范围只有在你已经于系统设置打开输入服务时才会授予。然后系统弹出录屏框。只有系统返回同意，采集服务才把 MediaProjection 的画面送进 MediaCodec 编码器，再把 Annex-B 送进同一条加密会话。
- 观看票据不会注入。会话关闭或撤销后，主机侧轮询到状态不再是 active，就停止编码和注入。
- 直连 UDP 仍会尝试，但没有可用的公网地址时会留在已认证的中继上。手机上的直连没有测过。

`TestAndroidEntrypointCarriesH264ThroughBuiltServer` 用这个 Go 入口对真正的控制面和编译出的 `cyandesk-gateway` 做了一次：同意后的控制票据送出一帧 libx264，对端解出红色画面；指针原样到达；关闭会话后主机不再认为它活跃；观看票据不允许输入。这不是把 HTTP 200 当成画面。

## 权限，按系统版本

应用不写 `Settings.Secure`，不替你打开无障碍，也不跳过录屏框。

| 系统 | 采集 | 输入 |
|---|---|---|
| Android 9（API 28） | 用户同意 MediaProjection 后，前台服务保持采集。这个版本还没有 mediaProjection 前台服务类型。 | 只能在系统设置里打开无障碍服务。服务声明了 `canPerformGestures`。 |
| Android 10–13 | 清单里声明 `FOREGROUND_SERVICE` 和 `mediaProjection`。Android 10 起前台服务要带类型。 | 同上。Android 13 可能把辅助功能标成受限制设置，仍要你在系统界面里允许。 |
| Android 13（API 33） | 另外需要你允许通知，否则前台服务不能留下。拒绝通知就不会开始采集。 | 同上。 |
| Android 14（API 34，本包的 targetSdk） | `startForeground` 带 `mediaProjection`。系统把这次录屏同意和这次服务启动绑在一起，不能拿旧的同意复用。 | 同上。 |
| 更新的系统 | 可能还会收紧录屏前台服务和特殊用途声明。本包没有声明 `specialUse`，也没有在那些版本上运行。 | 无障碍仍不能由应用自行打开。 |

## 这台 Android 9 模拟器上实际跑过的

`TestEmulatorShowsRealFramesBothWays`（`-tags androidemu`）在 API 28 模拟器上走完界面，没有用 `appops` 或 `settings put secure` 打开权限。

- 登录后屏幕上的本机指纹被读出来，再由已信任设备批准。指纹不一致就不会连接。
- 会话先停在等待同意，主机点同意后才发票据。
- 桌面用 ffmpeg 连续送出红、绿、蓝三色 H.264。Android 的 MediaCodec 解到 Surface。截图里画面依次是这三色，状态写着只有观看。
- 只观看时点画面，没有指针送到主机。
- 旋转后观看会话没有被拆掉。关闭会话后 Surface 停止，文字变成“会话已停止”。
- 被控端点同意后，系统录屏框由界面点击“START NOW”。MediaProjection 的画面经 MediaCodec 编码，经同一台控制面和 Rust 网关送出。桌面用 ffmpeg 解出了红、绿、蓝。观看票据下的远程点击没有点中“输入目标”。关闭会话后采集服务退出。
- 直连 UDP 在 Android 拨号里关掉了，还没有实现。

没有把这次叫做手机可用。没有 Pico，没有真机。

无障碍开关没有打开。这个模拟器的无障碍设置列表不响应滑动和翻页，CyanDesk 那一行出不来。测试没有改 secure settings。因此控制票据下的手势注入没有执行。

Android 10、13、14 没有启动对应模拟器。`mayStartCapture` 只在 JVM 里区分了 API 28、29、33、34 的决定：33 起没有通知同意就不采集，29 起代码会带 mediaProjection 前台服务类型。API 35 及更新版本的系统行为没有跑过。

调试包按 ABI 分开，未签名。x86_64 给这个模拟器，arm64-v8a 给以后的设备。没有 universal 包，也没有 release 签名包。

## 构建

需要 Android SDK 34、build-tools 34、NDK 26.1.10909125 和 Go 1.26。

```
sh android/bind.sh
cd android && gradle :core:test :app:assembleDebug
```

`bind.sh` 在这台机器上使用已经下载的 Go 1.26.0。系统自带的 Go 1.22 读不懂 `go.mod` 里的 tool 行。
