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
	case "version":
		fmt.Println("cyandesk 0.1.3 protocol cyandesk/1")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `CyanDesk 0.1.2

  cyandesk control --data 目录 --listen :13111 --internal 127.0.0.1:13112
  cyandesk gateway --listen :13110 --internal-url http://127.0.0.1:13112 --internal-token-file 文件
  cyandesk edge --listen :13111 --upstream http://127.0.0.1:13113 --admin 目录 --cert 证书 --key 私钥
  cyandesk client --config 目录 server set https://主机:13111
  cyandesk client --config 目录 server trust 指纹
  cyandesk client --config 目录 bootstrap --user 账号 --password-file 文件 --token-file 文件
  cyandesk client --config 目录 login --user 账号 --password-file 文件
  cyandesk client --config 目录 enroll --name 名称
  cyandesk client --config 目录 devices
  cyandesk client --config 目录 approve 设备ID --fingerprint 指纹
  cyandesk agent --config 目录 --display :0
  cyandesk connect --config 目录 --host 设备ID --fingerprint 指纹 --mode view|control
  cyandesk version

密码只从文件读取，不接受命令行参数。13111 是管理入口，不提供执行命令的接口。
`)
}

func runControl(args []string) int {
	fs := flag.NewFlagSet("control", flag.ExitOnError)
	data := fs.String("data", "data", "数据目录")
	listen := fs.String("listen", "127.0.0.1:13111", "对外 HTTPS")
	internal := fs.String("internal", "127.0.0.1:13112", "仅本机的票据接口")
	admin := fs.String("admin", "", "管理页目录，可空")
	_ = fs.Parse(args)
	host := hostOf(*listen)
	env, err := boot.Open(*data, []string{host, "localhost", "127.0.0.1"})
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
	srv.GatewayTCP = host + ":13110"
	srv.GatewayUDP = host + ":13110"
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
	cert, err := tls.X509KeyPair([]byte(env.Material.CertPEM), []byte(env.Material.KeyPEM))
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
	fmt.Printf("cert fingerprint: %s\n", env.Material.CertFingerprint)
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
