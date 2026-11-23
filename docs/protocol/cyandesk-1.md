# cyandesk/1

这是本仓库网关和客户端实际使用的线协议。主版本不同直接拒绝。

## 端口

- `13111/TCP`：HTTPS。管理页、`/api/v1`、下载。内部票据接口不在这个端口上。
- `13112/TCP`：仅控制面内部 HTTP。Compose 不发布该端口。网关用它核销票据。
- `13110/TCP`：信令与加密中继。
- `13110/UDP`：带 cookie 的直连候选，以及发往网关的 UDP 中继。QUIC 未实现。

客户端还会使用临时端口做直连。不能说全网只需要两个端口。

## 帧

```
0     'C'
1     'D'
2     version = 1
3     type
4..5  flags，大端，目前为 0
6..9  payload 长度，大端
10..  payload，最大 8 MiB
```

类型：

| 值 | 名称 | 含义 |
|---|---|---|
| 1 | Hello | 角色字节 + 票据。角色 1 是主机，2 是主控 |
| 2 | HelloAck | 网关接受或拒绝 |
| 3 | PeerReady | 对端已经持同一会话票据上线 |
| 4 | Relay | 网关原样转发给对端的字节，网关不解密 |
| 5 | Signal | 候选地址，网关原样转发 |
| 6 | Close | 结束 |
| 7 | Handshake | 直连上的 Noise 握手消息 |
| 8 | Secure | 直连上的 Noise 密文 |

UDP 数据报在帧之前还有 1 字节：`1` 表示等长 cookie 请求，`2` 表示等长 cookie 响应，`3` 表示已经带 cookie 的协议帧。cookie 响应不比请求大。

## 票据

控制面用服务器 Ed25519 密钥对票据正文签名。票据包含会话、双方设备、双方 Noise 公钥、scopes、nonce、到期时间和一次性 `jti`。网关核销后转发的是密文。服务器签发票据不能代替设备私钥，握手仍然要核对 Noise 静态公钥。

票据默认 2 分钟有效。会话可以更长；网关之后只复查会话是否仍为 active。

## 端到端

Noise 模式：`Noise_IK_25519_ChaChaPoly_BLAKE2s`（`github.com/flynn/noise`）。

主控是 initiator，主机是 responder。initiator 使用票据里的主机 Noise 公钥作为 `PeerStatic`。prologue 为：

```
cyandesk/1\n
session_id\n
nonce\n
逗号分隔的 scopes\n
host_noise_b64\n
controller_noise_b64\n
```

传输密钥：握手完成后，返回的第一个 CipherState 用于 initiator 到 responder，第二个用于反方向。这与 flynn/noise 的测试向量用法一致。

探测在握手成功之后进行：加密的 16 字节随机数往返。可用路径指握手成功并且探测成功，不是 TCP 三次握手完成。

## 选路

同一时刻启动中继和直连。先采用最早完成探测的路径出图。另一条路径若连续达到配置的改善幅度并过了冷却时间，才迁移。生产默认是改善 20%、保持 3 秒、冷却 10 秒。测试可以缩短保持时间。

迁移后旧路径停止发送媒体。输入带 `epoch` 和单调序号；主机对已处理序号不重复注入。

## 应用消息

Secure 明文的第一个字节是应用类型，后面是载荷。

| 值 | 名称 |
|---|---|
| 1 | Probe |
| 2 | ProbeAck |
| 3 | Video（H.264 bytestream 片段） |
| 4 | Audio（Opus/Ogg 字节） |
| 5 | Input |
| 6 | InputAck |
| 7 | KeyframeRequest |
| 8 | ReleaseAll |
| 9 | FileList |
| 10 | FileListResult |
| 11 | FileGet |
| 12 | FileData |
| 13 | Reject |
| 14 | Hello |

只观看会话里，主机收到 Input 时返回 Reject，并且不调用注入器。

## 不在本协议里的东西

没有开放的任意地址转发。中继只把已经核销的双方互转。管理 API 没有执行壳命令或构建命令的端点。
