# 网关语言

批准的设计使用 Rust 网关。发布路径是 `gateway-rs` 编出的 `cyandesk-gateway`。

`cmd/cyandesk` 里的 `gateway` 子命令是 Go 参考实现，只给对照测试使用。它会在标准错误打出这一点。Compose 启动的是 Rust 二进制。

Rust 网关不解密媒体。它用控制面的内部票据接口核销会话，然后原样转发。UDP 先做 64 字节的 cookie 交换，cookie 对不上就丢弃，避免把中继变成开放代理。

没有 Go 与 Rust 的性能对照数据。不要把功能测试里的延迟数字写成容量承诺。
