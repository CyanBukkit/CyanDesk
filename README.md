# CyanDesk

个人设备用的自建远控草稿。这一版能在一台 Linux 上完成：初始化管理员、登录、登记设备、批准、13111 管理页，以及经过认证的 13110 中继和直连竞速。它还不是各平台都能安装的完整产品。

设计原文在 `docs/design/CyanDesk-design-v0.1-approved.canvas.tsx`。做到哪一步写在 `docs/STATUS.md` 和 `docs/ACCEPTANCE.md`。

## 本机运行

需要 Go 1.26、Node 22 和 ffmpeg。

```sh
make admin
CGO_ENABLED=0 go build -trimpath -o bin/cyandesk ./cmd/cyandesk
./bin/cyandesk control --data ./data --listen 127.0.0.1:13111 --internal 127.0.0.1:13112 --admin admin-web/dist
```

启动时会打印证书指纹，以及 `bootstrap.token` 和 `internal.token` 的路径。文件里的内容不会出现在日志里。没有默认密码。

另一个终端：

```sh
./gateway-rs/target/release/cyandesk-gateway --listen 127.0.0.1:13110 --internal-url http://127.0.0.1:13112 --internal-token-file ./data/internal.token
```

客户端第一次要显式信任指纹：

```sh
./bin/cyandesk client --config ./client-data server set https://127.0.0.1:13111
./bin/cyandesk client --config ./client-data server trust <指纹>
./bin/cyandesk client --config ./client-data bootstrap --user admin --password-file ./admin.password --token-file ./data/bootstrap.token
./bin/cyandesk client --config ./client-data login --user admin --password-file ./admin.password
./bin/cyandesk client --config ./client-data enroll --name office
./bin/cyandesk client --config ./client-data devices
./bin/cyandesk client --config ./client-data approve <设备ID> --fingerprint <指纹>
```

被控端需要真实显示器。没有 `DISPLAY` 时，`agent` 会退出，不会假装有画面。

```sh
gcc -o bin/cyandesk-view packaging/linux/cyandesk-view.c -lX11
./bin/cyandesk agent --config ./host-data --display "$DISPLAY"
./bin/cyandesk connect --config ./view-data --host <被控设备ID> --fingerprint <指纹> --mode control
```

`--mode view` 只申请观看。主机不因为主控本地改了开关就接受输入。画面大小用 `--width --height --fps` 指定，这不是帧率保证。

`agent` 和 `connect` 仍在拨号时选定一条路径。活动会话在丢包后改走另一条加密 UDP、再按迟滞迁回，是 `TestLiveUDPMigration` 覆盖的本机路径，还没有接到这两条命令上。丢包会破坏那条 Noise 传输，恢复时要重新握手。

Windows 10/11 的二进制可以用下面的命令交叉编译。编出来不代表在 Windows 上运行过。Windows 7 不在这个二进制里。

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o cyandesk.exe ./cmd/cyandesk
```

密码和引导凭据只从文件读取。

管理页是 `https://127.0.0.1:13111/`。浏览器会提示私有证书，指纹必须和启动时打印的一致。管理员不能从这里观看屏幕。

## Docker

```sh
./deploy/install.sh
```

这台开发机没有 Docker、podman、rootlesskit、slirp4netns，也没有 `/var/run/docker.sock`。没有为了安装它们去改宿主机防火墙。安装脚本在这里不能跑通，不能记成通过。Compose 里的网关是 Rust 二进制，同时映射 13110 的 TCP 和 UDP，不发布 13112。卸载默认保留数据卷：

```sh
./deploy/uninstall.sh
./deploy/uninstall.sh --delete-data
```

## 测试

```sh
go test ./...
```

X11 采集测试会临时启动 Xvfb，不改系统显示配置。
