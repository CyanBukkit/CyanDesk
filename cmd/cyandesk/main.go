package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"cyandesk/internal/api"
	"cyandesk/internal/boot"
	"cyandesk/internal/clientcore"
	"cyandesk/internal/edge"
	"cyandesk/internal/gateway"
	"cyandesk/internal/secret"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "control":
		os.Exit(runControl(os.Args[2:]))
	case "gateway":
		os.Exit(runGateway(os.Args[2:]))
	case "edge":
		os.Exit(runEdge(os.Args[2:]))
	case "client":
		os.Exit(runClient(os.Args[2:]))
	case "agent":
		os.Exit(runAgent(os.Args[2:]))
	case "service":
		os.Exit(runService(os.Args[2:]))
	case "connect":
		os.Exit(runConnect(os.Args[2:]))
	case "gui":
		os.Exit(runGUI(os.Args[2:]))
	case "version":
		fmt.Println("cyandesk 0.1.6 protocol cyandesk/1")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `CyanDesk 0.1.6

  cyandesk control --data 目录 --listen 0.0.0.0:13111 --internal 127.0.0.1:13112 --admin 目录 [--cert-host 名称] [--gateway-advertise 主机:13110] [--tls-cert 证书 --tls-key 私钥]
  cyandesk-gateway --listen 0.0.0.0:13110 --internal-url http://127.0.0.1:13112 --internal-token-file 文件
  cyandesk gateway --listen :13110 --internal-url http://127.0.0.1:13112 --internal-token-file 文件
  cyandesk client --config 目录 server set https://主机:13111
  cyandesk client --config 目录 server trust 指纹
  cyandesk client --config 目录 bootstrap --user 账号 --password-file 文件 --token-file 文件
  cyandesk client --config 目录 login --user 账号 --password-file 文件
  cyandesk client --config 目录 enroll --name 名称
  cyandesk client --config 目录 devices
  cyandesk client --config 目录 approve 设备ID --fingerprint 指纹
  cyandesk client --config 目录 grant --host 主机ID --controller 主控ID --scope view,input [--unattended]
  cyandesk client --config 目录 consent 会话ID --scope view,input [--deny]
  cyandesk agent --config 目录 --display 显示 [--width 1280 --height 720 --fps 15]
  cyandesk connect --config 目录 --host 设备ID --fingerprint 指纹 --mode view|control [--direct-advertise 地址:端口]
  cyandesk gui
  cyandesk version

不带参数会打印这段说明。密码只从文件读取。13111 是管理入口。13112 只给本机网关核销票据，不要公开。
发布网关是 cyandesk-gateway。上面的 cyandesk gateway 只是对照程序。
`)
}

func runControl(args []string) int {
	fs := flag.NewFlagSet("control", flag.ExitOnError)
	data := fs.String("data", "data", "数据目录")
	listen := fs.String("listen", "127.0.0.1:13111", "对外 HTTPS")
	internal := fs.String("internal", "127.0.0.1:13112", "仅本机的票据接口")
	admin := fs.String("admin", "", "管理页目录，可空")
	tlsCert := fs.String("tls-cert", "", "替换监听证书 PEM")
	tlsKey := fs.String("tls-key", "", "替换监听私钥 PEM")
	gatewayAdvertise := fs.String("gateway-advertise", "", "向客户端公布的网关 host:port")
	var certHosts []string
	fs.Func("cert-host", "首次生成证书时额外写入的 IP 或域名，可重复", func(v string) error {
		certHosts = append(certHosts, v)
		return nil
	})
	_ = fs.Parse(args)
	if (*tlsCert == "") != (*tlsKey == "") {
		fmt.Fprintln(os.Stderr, "tls-cert 和 tls-key 要一起给出")
		return 2
	}
	host := hostOf(*listen)
	hosts := []string{host, "localhost", "127.0.0.1"}
	hosts = append(hosts, certHosts...)
	env, err := boot.Open(*data, hosts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer env.DB.Close()
	logger := log.New(os.Stderr, "", log.LstdFlags)
	srv, err := api.New(env.DB, env.Material, env.Artifacts, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	gw := net.JoinHostPort(host, "13110")
	if *gatewayAdvertise != "" {
		gw = *gatewayAdvertise
	} else if len(certHosts) > 0 {
		gw = net.JoinHostPort(certHosts[0], "13110")
	}
	srv.GatewayTCP = gw
	srv.GatewayUDP = gw
	var handler http.Handler = srv.PublicHandler()
	if *admin != "" {
		h, err := edge.Handler("http://"+*internal, *admin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		_ = h
		// The public handler already serves the API. Static files are mounted beside it.
		handler = withStatic(srv.PublicHandler(), *admin)
	}
	certPEM := []byte(env.Material.CertPEM)
	keyPEM := []byte(env.Material.KeyPEM)
	fingerprint := env.Material.CertFingerprint
	if *tlsCert != "" {
		certPEM, err = os.ReadFile(*tlsCert)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		keyPEM, err = os.ReadFile(*tlsKey)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fingerprint, err = secret.FingerprintFromPEM(certPEM)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ln, err := tls.Listen("tcp", *listen, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	go func() {
		_ = http.ListenAndServe(*internal, srv.InternalHandler())
	}()
	fmt.Printf("cert fingerprint: %s\n", fingerprint)
	fmt.Printf("bootstrap token file: %s\n", env.BootstrapFile)
	fmt.Printf("internal token file: %s\n", env.InternalFile)
	fmt.Printf("listening https %s\n", ln.Addr())
	if err := http.Serve(ln, handler); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func withStatic(apiHandler http.Handler, dir string) http.Handler {
	static, err := edge.Handler("http://127.0.0.1:0", dir)
	if err != nil {
		return apiHandler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/downloads/") || strings.HasPrefix(r.URL.Path, "/health/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
}

func runGateway(args []string) int {
	fs := flag.NewFlagSet("gateway", flag.ExitOnError)
	listen := fs.String("listen", ":13110", "信令与中继")
	internalURL := fs.String("internal-url", "http://127.0.0.1:13112", "控制面内部地址")
	tokenFile := fs.String("internal-token-file", "", "内部令牌文件")
	_ = fs.Parse(args)
	raw, err := os.ReadFile(*tokenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "这是 Go 参考网关，不作为最终网关。请使用 cyandesk-gateway。\n")
	fmt.Printf("reference gateway listening %s\n", ln.Addr())
	srv := gateway.New(gateway.HTTPVerifier{BaseURL: *internalURL, Token: strings.TrimSpace(string(raw))})
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runEdge(args []string) int {
	fs := flag.NewFlagSet("edge", flag.ExitOnError)
	listen := fs.String("listen", ":13111", "对外 HTTPS")
	upstream := fs.String("upstream", "http://127.0.0.1:13113", "控制面公开 API")
	admin := fs.String("admin", "admin-web/dist", "管理页目录")
	certFile := fs.String("cert", "", "证书")
	keyFile := fs.String("key", "", "私钥")
	_ = fs.Parse(args)
	h, err := edge.Handler(*upstream, *admin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ln, err := tls.Listen("tcp", *listen, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("edge listening %s\n", ln.Addr())
	if err := http.Serve(ln, h); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runClient(args []string) int {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	config := fs.String("config", "", "配置目录")
	_ = fs.Parse(args)
	rest := fs.Args()
	if *config == "" || len(rest) == 0 {
		usage()
		return 2
	}
	p, err := clientcore.Load(*config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch rest[0] {
	case "server":
		if len(rest) < 3 {
			usage()
			return 2
		}
		switch rest[1] {
		case "set":
			p.ServerURL = rest[2]
		case "trust":
			p.Fingerprint = strings.ToLower(rest[2])
		default:
			usage()
			return 2
		}
		if err := p.Save(*config); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("已保存服务器配置")
	case "bootstrap":
		user, passFile, tokenFile := "", "", ""
		for i := 1; i < len(rest); i++ {
			switch rest[i] {
			case "--user":
				i++
				user = rest[i]
			case "--password-file":
				i++
				passFile = rest[i]
			case "--token-file":
				i++
				tokenFile = rest[i]
			default:
				fmt.Fprintln(os.Stderr, "引导凭据只能从文件读取")
				return 2
			}
		}
		pw, err := os.ReadFile(passFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		tok, err := os.ReadFile(tokenFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := p.Bootstrap(strings.TrimSpace(string(tok)), user, strings.TrimSpace(string(pw))); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("管理员已创建")
	case "login":
		user, passFile := "", ""
		for i := 1; i < len(rest); i++ {
			switch rest[i] {
			case "--user":
				i++
				user = rest[i]
			case "--password-file":
				i++
				passFile = rest[i]
			default:
				fmt.Fprintln(os.Stderr, "不接受把密码写在命令里")
				return 2
			}
		}
		pw, err := os.ReadFile(passFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := p.Login(user, strings.TrimSpace(string(pw))); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := p.Save(*config); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("已登录 %s\n", user)
	case "enroll":
		name := "未命名设备"
		for i := 1; i < len(rest); i++ {
			if rest[i] == "--name" && i+1 < len(rest) {
				name = rest[i+1]
			}
		}
		if err := p.Enroll(name, goos(), goarch()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := p.Save(*config); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("设备 %s 待批准\n指纹 %s\n", p.DeviceID, p.DeviceFP)
	case "devices":
		devs, err := p.Devices()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(devs) == 0 {
			fmt.Println("没有设备")
			return 0
		}
		for _, d := range devs {
			fmt.Printf("%s  %s  %s  %s  在线=%v\n", d["id"], d["name"], d["trust_state"], d["fingerprint"], d["online"])
		}
	case "approve":
		if len(rest) < 2 {
			usage()
			return 2
		}
		fp := ""
		id := rest[1]
		for i := 2; i < len(rest); i++ {
			if rest[i] == "--fingerprint" && i+1 < len(rest) {
				fp = rest[i+1]
			}
		}
		if fp == "" {
			fmt.Fprintln(os.Stderr, "需要 --fingerprint，不能跳过核对")
			return 2
		}
		if err := p.Approve(id, fp); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := p.Save(*config); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("已信任这台设备")
	case "grant":
		hostID, ctrlID, scope := "", "", "view"
		unattended := false
		for i := 1; i < len(rest); i++ {
			switch rest[i] {
			case "--host":
				i++
				if i < len(rest) {
					hostID = rest[i]
				}
			case "--controller":
				i++
				if i < len(rest) {
					ctrlID = rest[i]
				}
			case "--scope":
				i++
				if i < len(rest) {
					scope = rest[i]
				}
			case "--unattended":
				unattended = true
			default:
				fmt.Fprintln(os.Stderr, "未知参数")
				return 2
			}
		}
		if hostID == "" || ctrlID == "" {
			fmt.Fprintln(os.Stderr, "需要 --host 和 --controller")
			return 2
		}
		if err := p.Grant(hostID, ctrlID, strings.Split(scope, ","), unattended); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("已写入授权")
	case "consent":
		if len(rest) < 2 {
			usage()
			return 2
		}
		scope := "view"
		accept := true
		for i := 2; i < len(rest); i++ {
			switch rest[i] {
			case "--scope":
				i++
				if i < len(rest) {
					scope = rest[i]
				}
			case "--deny":
				accept = false
			default:
				fmt.Fprintln(os.Stderr, "未知参数")
				return 2
			}
		}
		if err := p.Consent(rest[1], accept, strings.Split(scope, ",")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if accept {
			fmt.Println("主机已同意这次连接")
		} else {
			fmt.Println("主机已拒绝这次连接")
		}
	case "status":
		fmt.Printf("服务器 %s\n指纹 %s\n账号 %s\n设备 %s\n", p.ServerURL, p.Fingerprint, p.LoginName, p.DeviceID)
	default:
		usage()
		return 2
	}
	return 0
}

func hostOf(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "127.0.0.1"
	}
	if host == "" || host == "0.0.0.0" {
		return "127.0.0.1"
	}
	return host
}
