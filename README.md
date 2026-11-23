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
./bin/cyandesk gateway --listen 127.0.0.1:13110 --internal-url http://127.0.0.1:13112 --internal-token-file ./data/internal.token
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

密码和引导凭据只从文件读取。

管理页是 `https://127.0.0.1:13111/`。浏览器会提示私有证书，指纹必须和启动时打印的一致。管理员不能从这里观看屏幕。

## Docker

```sh
./deploy/install.sh
```

当前这台开发机没有 Docker 守护进程，所以这条命令还没有在这里跑通。`deploy/compose.yaml` 会构建镜像，发布 13111 和 13110/TCP，不发布内部票据端口 13112。卸载默认保留数据卷：

```sh
./deploy/uninstall.sh
./deploy/uninstall.sh --delete-data
```

## 测试

```sh
go test ./...
```

X11 采集测试会临时启动 Xvfb，不改系统显示配置。
