# 引擎桥接契约

来源：设计第 4 节。这里只固定名字和错误，方便以后替换采集实现。当前 Linux 路径实现了其中一部分，没有 FFI，也没有把 RustDesk 嵌进来。

命令：

- `Initialize(ServerProfile)`
- `Enroll(DeviceIdentity)`
- `ListCapabilities`
- `StartSession(SessionOffer)`
- `ApplyPolicy`
- `SetQuality`
- `SelectDisplay`
- `SetViewLayout`
- `SendInput`
- `CloseSession`

事件：

- `PermissionRequired`
- `StateChanged`
- `FrameStats`
- `PathChanged`
- `FatalError`

错误码：

- `NOT_SUPPORTED`
- `PERMISSION_REQUIRED`
- `AUTH_REVOKED`
- `VERSION_MISMATCH`
- `NETWORK_TIMEOUT`

当前代码里的对应关系：

| 契约 | 现状 |
|---|---|
| Initialize / Enroll | 客户端配置与 `POST /api/v1/devices/enroll` |
| ApplyPolicy | 会话 scopes；只观看时主机不注入输入 |
| StartSession / CloseSession | `internal/remote` |
| SendInput | 经门闩后才调用注入器 |
| SelectDisplay / SetViewLayout | 只有坐标映射和 X11 屏幕枚举，没有虚拟显示驱动 |
| SetQuality | 预设常量已定义；采集进程仍使用固定的低延迟 H.264 参数 |

能力字段至少包括：`os`、`arch`、`engine_version`、`capture`、`input`、`audio`、`codec_encode`、`codec_decode`、`displays`、`virtual_display`、`unattended`、`vr_send`、`vr_receive`。`virtual_display` 与 `vr_*` 在本轮报告里必须是 false，除非另有测试证据。
