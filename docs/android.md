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

MediaCodec 的编码和解码调用在 APK 里。这台机器后来用 KVM 启动了 Android 9（API 28）模拟器，装上调试包后登录页能显示服务器、指纹、账号和“没有 OpenXR”的说明。没有替模拟器点录屏同意，也没有用 adb 授予 MediaProjection 或无障碍。因此编码器、Surface 和解码循环仍没有在 Android 进程里跑过。没有手机，没有 Pico。

## 构建

需要 Android SDK 34、build-tools 34、NDK 26.1.10909125 和 Go 1.26。

```
sh android/bind.sh
cd android && gradle :core:test :app:assembleDebug
```

`bind.sh` 在这台机器上使用已经下载的 Go 1.26.0。系统自带的 Go 1.22 读不懂 `go.mod` 里的 tool 行。
