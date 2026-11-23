package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cyandesk/internal/clientcore"
	"cyandesk/internal/desktop"
	"cyandesk/internal/hostcap"
	"cyandesk/internal/input"
	"cyandesk/internal/protocol"
)

func runAgent(args []string) int {
	return runAgentContext(context.Background(), args)
}

func runAgentContext(ctx context.Context, args []string) int {
	config, display := "", os.Getenv("DISPLAY")
	allowUpgrade := false
	width, height, fps := 1280, 720, 15
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i < len(args) {
				config = args[i]
			}
		case "--display":
			i++
			if i < len(args) {
				display = args[i]
			}
		case "--width":
			i++
			width, _ = strconv.Atoi(args[i])
		case "--height":
			i++
			height, _ = strconv.Atoi(args[i])
		case "--fps":
			i++
			fps, _ = strconv.Atoi(args[i])
		case "--allow-upgrade":
			allowUpgrade = true
		default:
			fmt.Fprintln(os.Stderr, "未知参数")
			return 2
		}
	}
	if width < 2 || height < 2 || width%2 != 0 || height%2 != 0 || fps < 1 || fps > 60 {
		fmt.Fprintln(os.Stderr, "宽高必须为正偶数，帧率在 1 到 60 之间。这不是性能承诺。")
		return 2
	}
	if err := hostcap.RequireSession(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if config == "" {
		fmt.Fprintln(os.Stderr, "需要 --config")
		return 2
	}
	profile, err := clientcore.Load(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("主机代理已启动。等待已授权的会话。按 Ctrl+C 结束。")
	for {
		select {
		case <-ctx.Done():
			return 0
		default:
		}
		sessions, err := profile.Sessions()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			if !sleepOrDone(ctx, 2*time.Second) {
				return 0
			}
			continue
		}
		for _, sess := range sessions {
			if sess["host_id"] != profile.DeviceID || sess["state"] != "active" {
				continue
			}
			id, _ := sess["id"].(string)
			if err := serveOne(profile, id, display, allowUpgrade, width, height, fps); err != nil && err != desktop.ErrRevoked {
				fmt.Fprintln(os.Stderr, "会话结束")
			}
		}
		if !sleepOrDone(ctx, time.Second) {
			return 0
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func serveOne(profile clientcore.Profile, sessionID, display string, allowUpgrade bool, width, height, fps int) error {
	state, ticket, err := profile.SessionTicket(sessionID)
	if err != nil || state != "active" || ticket == "" {
		return err
	}
	info, err := desktop.ParseTicket(ticket)
	if err != nil {
		return err
	}
	ctrlID := ""
	sessions, err := profile.Sessions()
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess["id"] == sessionID {
			ctrlID, _ = sess["controller_id"].(string)
		}
	}
	pinned := profile.Pins[ctrlID]
	ctrlRec, err := profile.Device(ctrlID)
	if err != nil {
		return err
	}
	fp, _ := ctrlRec["fingerprint"].(string)
	noiseKey, _ := ctrlRec["noise_public_key"].(string)
	self, err := profile.NoiseKey()
	if err != nil {
		return err
	}
	ctx := context.Background()
	link, err := desktop.OpenAuthorized(ctx, gatewayFrom(profile), protocol.RoleHost, ticket, pinned, fp, noiseKey, self)
	if err != nil {
		return err
	}
	defer link.Close()
	gate := &input.Gate{AllowInput: desktop.HasInput(info.Scopes)}
	return desktop.ServeHost(ctx, link, desktop.HostConfig{
		Gate: gate, GrantedInput: desktop.HasInput(info.Scopes), AllowUpgrade: allowUpgrade,
		Grab: func() ([]byte, int, int, error) {
			rgb, err := hostcap.Grab(display, width, height)
			return rgb, width, height, err
		},
		Encode:   hostcap.Encode,
		Inject:   hostcap.Sink{Display: display},
		Alive:    func() bool { st, _, err := profile.Session(sessionID); return err == nil && st == "active" },
		Interval: time.Second / time.Duration(fps),
		Width:    width,
		Height:   height,
		Queue:    desktop.NewVideoQueue(2),
	})
}

func runConnect(args []string) int {
	config, hostID, mode, pin := "", "", "view", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			config = args[i]
		case "--host":
			i++
			hostID = args[i]
		case "--mode":
			i++
			mode = args[i]
		case "--fingerprint":
			i++
			pin = args[i]
		default:
			fmt.Fprintln(os.Stderr, "未知参数")
			return 2
		}
	}
	if config == "" || hostID == "" || pin == "" {
		fmt.Fprintln(os.Stderr, "需要 --config --host --fingerprint")
		return 2
	}
	if err := hostcap.RequireSession(); err != nil {
		fmt.Fprintln(os.Stderr, "本机没有可用的画面窗口。")
		return 2
	}
	profile, err := clientcore.Load(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	scopes := []string{"view"}
	if mode == "control" {
		scopes = []string{"view", "input"}
	}
	sessionID, ticket, _, err := profile.CreateSession(hostID, scopes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rec, err := profile.Device(hostID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fp, _ := rec["fingerprint"].(string)
	noiseKey, _ := rec["noise_public_key"].(string)
	if !strings.EqualFold(fp, pin) {
		fmt.Fprintln(os.Stderr, "指纹与已保存的记录不一致，已停止连接")
		return 1
	}
	ctx := context.Background()
	self, err := profile.NoiseKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	link, err := desktop.OpenAuthorized(ctx, gatewayFrom(profile), protocol.RoleController, ticket, pin, fp, noiseKey, self)
	if err != nil {
		_ = profile.CloseSession(sessionID)
		fmt.Fprintln(os.Stderr, "连接失败")
		return 1
	}
	defer link.Close()
	defer profile.CloseSession(sessionID)
	fmt.Println("已连接。第一帧到达后打开窗口。关闭窗口或按 Ctrl+C 结束。")
	var view *viewer
	defer func() {
		if view != nil && view.close != nil {
			view.close()
		}
	}()
	for {
		kind, payload, err := link.Recv()
		if err != nil {
			fmt.Println("连接已断开")
			return 0
		}
		_, rgb, w, h, ok, err := desktop.DecodeDeliveredFrame(kind, payload)
		if err != nil || !ok {
			continue
		}
		if view == nil {
			view, err = startViewer(w, h)
			if err != nil {
				fmt.Fprintln(os.Stderr, viewerStartError())
				return 1
			}
			go readViewerInput(view, link, mode == "control")
			fmt.Printf("远端画面 %dx%d。尺寸由被控端 --width --height --fps 决定，这里不保证能维持该帧率。\n", w, h)
		}
		view.present(rgb)
	}
}

func readViewerInput(view *viewer, link interface {
	Send(byte, []byte) error
}, allow bool) {
	var seq uint64
	for {
		x, y, ok := view.nextClick()
		if !ok {
			return
		}
		if !allow {
			continue
		}
		seq++
		_ = link.Send(protocol.AppInput, desktop.EncodeInput(input.Event{Epoch: 1, Seq: seq, Kind: 2, X: int32(x), Y: int32(y), Button: 1}))
	}
}

func gatewayFrom(p clientcore.Profile) string {
	return "127.0.0.1:13110"
}
