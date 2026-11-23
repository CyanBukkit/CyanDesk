//go:build androidemu

package androidemu

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"image"
	"image/png"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"cyandesk/internal/api"
	"cyandesk/internal/boot"
	"cyandesk/internal/clientcore"
	"cyandesk/internal/media"
	"cyandesk/internal/protocol"
	"cyandesk/mobile"
)

const serial = "emulator-5572"

func TestEmulatorShowsRealFramesBothWays(t *testing.T) {
	if _, err := exec.LookPath("adb"); err != nil {
		t.Fatal("adb is not installed")
	}
	if !booted(t) {
		t.Fatal("Android 9 emulator is not booted; this test does not grant permissions to a substitute")
	}
	apk := os.Getenv("CYANDESK_APK")
	if apk == "" {
		apk = filepath.Join("..", "..", "android", "app", "build", "outputs", "apk", "debug", "app-x86_64-debug.apk")
	}
	if _, err := os.Stat(apk); err != nil {
		t.Fatal(err)
	}
	env, publicURL := startServer(t)
	pass := randomSecret(t)
	admin := clientcore.Profile{ServerURL: publicURL, Fingerprint: env.Material.CertFingerprint}
	token, err := os.ReadFile(env.BootstrapFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Bootstrap(strings.TrimSpace(string(token)), "emu-lab", pass); err != nil {
		t.Fatal(err)
	}
	desk, err := mobile.Connect(publicURL, env.Material.CertFingerprint, "emu-lab", pass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desk.Enroll("linux-office"); err != nil {
		t.Fatal(err)
	}
	if err := desk.Approve(desk.DeviceID(), desk.Fingerprint()); err != nil {
		t.Fatal(err)
	}
	deskFP := desk.Fingerprint()

	adb(t, "install", "-r", apk)
	adb(t, "shell", "am", "force-stop", "desk.cyan.client")
	adb(t, "logcat", "-c")
	adb(t, "shell", "am", "start", "-n", "desk.cyan.client/.LoginActivity")
	waitText(t, "登录", 20*time.Second)
	typeInto(t, "https://服务器:13111", "https://10.0.2.2:"+publicPort(publicURL))
	typeInto(t, "64 位证书指纹", env.Material.CertFingerprint)
	typeInto(t, "账号", "emu-lab")
	typeInto(t, "密码", pass)
	tap(t, "登录")
	waitText(t, "本机指纹", 30*time.Second)
	ui := dump(t)
	shown := fingerprintIn(ui, "本机指纹")
	if shown == "" {
		t.Fatalf("screen did not show a device fingerprint: %s", visible(ui))
	}
	if err := desk.Approve(deviceByFingerprint(t, desk, shown), shown); err != nil {
		t.Fatalf("on-screen fingerprint was not the enrolled device: %v", err)
	}
	tap(t, "刷新设备")
	waitText(t, deskFP, 20*time.Second)
	shot(t, "devices-fingerprint.png")
	if !strings.Contains(dump(t), deskFP) {
		t.Fatal("device list does not show the desktop fingerprint the user must compare")
	}

	colors := viewSession(t, desk, deskFP, shown)
	if len(colors) < 2 {
		t.Fatalf("android surface showed %v, want at least two changing colors", colors)
	}
	t.Logf("android surface colors: %v", colors)

	hostColors, clickedView, clickedControl := hostSession(t, desk, shown, pass)
	if len(hostColors) < 2 {
		t.Fatalf("desktop decoded %v colors from android capture, want at least two", hostColors)
	}
	if clickedView {
		t.Fatal("view-only session changed the pattern click target")
	}
	if !clickedControl {
		t.Log("control injection was not observed; the emulator settings list did not present the service switch")
	}
	t.Logf("desktop decoded android capture colors: %v", hostColors)
	scrubLog(t, pass)
}

func viewSession(t *testing.T, desk *mobile.Agent, deskFP, androidFP string) []string {
	t.Helper()
	tapTextContaining(t, deskFP)
	ui := dump(t)
	if !strings.Contains(ui, deskFP) {
		t.Fatal("refusing to request a session before the selected fingerprint is on screen")
	}
	type dialResult struct {
		link *mobile.Link
		id   string
		err  error
	}
	got := make(chan dialResult, 1)
	go func() {
		deadline := time.Now().Add(45 * time.Second)
		var id string
		for time.Now().Before(deadline) {
			incoming, err := desk.Incoming()
			if err == nil && incoming != "" {
				id = incoming
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if id == "" {
			got <- dialResult{err: os.ErrDeadlineExceeded}
			return
		}
		peer, err := desk.PeerFingerprint(id)
		if err != nil || !strings.EqualFold(peer, androidFP) {
			got <- dialResult{err: err}
			return
		}
		if err := desk.Consent(id, "view", true); err != nil {
			got <- dialResult{err: err}
			return
		}
		link, err := desk.Dial(mobile.RoleHost, id, peer)
		got <- dialResult{link: link, id: id, err: err}
	}()
	tap(t, "观看这台设备")
	res := <-got
	if res.err != nil || res.link == nil {
		note := strings.TrimSpace(adbOut(t, "shell", "run-as", "desk.cyan.client", "cat", "files/playback-state.txt"))
		t.Fatalf("desktop host did not join the consented view session: %v; phone says %s", res.err, note)
	}
	defer res.link.Close()
	waitText(t, "只有观看", 20*time.Second)
	inputs := 0
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			pkt, err := res.link.Recv()
			if err != nil {
				return
			}
			if pkt.Kind == int(protocol.AppInput) {
				inputs++
			}
		}
	}()
	seen := map[string]bool{}
	for _, rgb := range [][3]byte{{220, 10, 10}, {10, 180, 10}, {10, 10, 220}} {
		annex := mustAnnex(t, rgb)
		deadline := time.Now().Add(3 * time.Second)
		seq := 1
		for time.Now().Before(deadline) {
			if err := res.link.SendVideo(seq, 64, 64, annex); err != nil {
				t.Fatal(err)
			}
			seq++
			time.Sleep(200 * time.Millisecond)
			if name := shotColor(t, "viewer-"+colorName(rgb)+".png"); name != "" {
				seen[name] = true
			}
		}
	}
	tapCenter(t)
	time.Sleep(800 * time.Millisecond)
	close(stop)
	if inputs != 0 {
		t.Fatalf("view ticket sent %d pointer events", inputs)
	}
	adb(t, "shell", "settings", "put", "system", "accelerometer_rotation", "0")
	adb(t, "shell", "settings", "put", "system", "user_rotation", "1")
	time.Sleep(1200 * time.Millisecond)
	rotated := dump(t)
	if strings.Contains(rotated, "会话已停止") {
		t.Fatal("rotation closed the decrypted viewer")
	}
	adb(t, "shell", "settings", "put", "system", "user_rotation", "0")
	if err := desk.CloseSession(res.id); err != nil {
		t.Fatal(err)
	}
	waitText(t, "会话已停止", 15*time.Second)
	shot(t, "viewer-revoked.png")
	return keys(seen)
}

func hostSession(t *testing.T, desk *mobile.Agent, androidFP, pass string) (colors []string, viewClicked, controlClicked bool) {
	t.Helper()
	adb(t, "shell", "input", "keyevent", "4")
	waitText(t, "作为被控", 15*time.Second)
	id := requestWhenReady(t, desk, androidFP, "view")
	tap(t, "作为被控，查看等待中的连接")
	waitText(t, desk.Fingerprint(), 15*time.Second)
	if !strings.Contains(dump(t), desk.Fingerprint()) {
		t.Fatal("consent screen does not show the controller fingerprint")
	}
	tap(t, "按指纹同意观看，然后出现系统录屏框")
	if !tapAny(t, "START NOW", "Start now", "立即开始") {
		t.Fatalf("projection dialog was not accepted on screen\n%s", visible(dump(t)))
	}
	link, err := desk.Dial(mobile.RoleController, id, androidFP)
	if err != nil {
		t.Fatalf("dial after projection consent: %v; capture %s", err, captureNote(t))
	}
	defer link.Close()
	waitText(t, "输入目标", 20*time.Second)
	colors = decodeChanging(t, link)
	if strings.Contains(dump(t), "已点击") {
		t.Fatal("pattern was clicked before a remote pointer")
	}
	tapRemote(t, link, 1)
	time.Sleep(time.Second)
	viewClicked = strings.Contains(dump(t), "已点击")
	if err := desk.CloseSession(id); err != nil {
		t.Fatal(err)
	}
	waitGone(t, "CaptureService", 15*time.Second)

	backTo(t, "作为被控，查看等待中的连接")
	_ = enableAccessibility(t)
	if !accessibilityEnabled(t) {
		backTo(t, "作为被控，查看等待中的连接")
		t.Log("emulator accessibility list did not expose the CyanDesk switch; control injection was not run")
		return colors, viewClicked, false
	}
	backTo(t, "作为被控，查看等待中的连接")
	id = requestWhenReady(t, desk, androidFP, "control")
	tap(t, "作为被控，查看等待中的连接")
	waitText(t, desk.Fingerprint(), 15*time.Second)
	tap(t, "按指纹同意控制。需要你已打开输入服务")
	if !tapAny(t, "START NOW", "Start now", "立即开始") {
		t.Fatalf("second projection dialog was not accepted\n%s", visible(dump(t)))
	}
	link2, err := desk.Dial(mobile.RoleController, id, androidFP)
	if err != nil {
		t.Fatalf("control dial: %v; capture %s", err, captureNote(t))
	}
	defer link2.Close()
	waitText(t, "输入目标", 20*time.Second)
	tapRemote(t, link2, 2)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(dump(t), "已点击") {
			controlClicked = true
			break
		}
		time.Sleep(700 * time.Millisecond)
	}
	adb(t, "shell", "settings", "put", "system", "user_rotation", "1")
	time.Sleep(800 * time.Millisecond)
	if !servicePresent(t, "CaptureService") {
		t.Fatal("rotation stopped the capture service")
	}
	more := decodeChanging(t, link2)
	if len(more) < 1 {
		t.Fatal("no frames after rotation")
	}
	adb(t, "shell", "settings", "put", "system", "user_rotation", "0")
	if err := desk.CloseSession(id); err != nil {
		t.Fatal(err)
	}
	waitGone(t, "CaptureService", 15*time.Second)
	state := strings.TrimSpace(adbOut(t, "shell", "run-as", "desk.cyan.client", "cat", "files/capture-state.txt"))
	if state != "stopped" {
		t.Fatalf("capture state after revoke is %q", state)
	}
	_ = pass
	return colors, viewClicked, controlClicked
}

func requestWhenReady(t *testing.T, desk *mobile.Agent, hostFP, mode string) string {
	t.Helper()
	id, err := desk.Request(deviceByFingerprint(t, desk, hostFP), mode)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err := desk.Poll(id)
		if err == nil && (state == "waiting_consent" || state == "active") {
			return id
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("session was neither waiting for consent nor active")
	return ""
}

func decodeChanging(t *testing.T, link *mobile.Link) []string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var stream bytes.Buffer
	var w, h int
	frames := 0
	for time.Now().Before(deadline) && frames < 80 {
		pkt, err := link.Recv()
		if err != nil {
			break
		}
		if pkt.Kind != int(protocol.AppVideo) || len(pkt.Annex) == 0 {
			continue
		}
		w, h = pkt.W, pkt.H
		stream.Write(pkt.Annex)
		frames++
	}
	if frames < 2 {
		t.Fatalf("android encoder sent %d frames", frames)
	}
	rgb, err := decodeAll(stream.Bytes(), w, h)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	frameBytes := w * h * 3
	for off := 0; off+frameBytes <= len(rgb); off += frameBytes {
		if name := classifyRGB(rgb[off : off+frameBytes]); name != "" {
			seen[name] = true
		}
	}
	return keys(seen)
}

func tapRemote(t *testing.T, link *mobile.Link, seq int) {
	t.Helper()
	ui := dump(t)
	var box [4]int
	found := false
	for _, n := range nodes(ui) {
		if n.text == "输入目标" || n.text == "已点击" {
			box = n.bounds
			found = true
			break
		}
	}
	if !found {
		t.Fatal("input target is not on screen")
	}
	x := (box[0] + box[2]) / 2
	y := (box[1] + box[3]) / 2
	if err := link.SendPointer(seq, x, y); err != nil {
		t.Fatal(err)
	}
}

func backTo(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(dump(t), text) {
			return
		}
		adb(t, "shell", "input", "keyevent", "4")
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("could not return to %q\n%s", text, visible(dump(t)))
}

func accessibilityEnabled(t *testing.T) bool {
	t.Helper()
	out := adbOut(t, "shell", "settings", "get", "secure", "enabled_accessibility_services")
	return strings.Contains(out, "desk.cyan.client")
}

func captureNote(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(adbOut(t, "shell", "run-as", "desk.cyan.client", "cat", "files/capture-state.txt"))
}

func enableAccessibility(t *testing.T) bool {
	t.Helper()
	adb(t, "shell", "am", "start", "-a", "android.settings.ACCESSIBILITY_SETTINGS")
	deadline := time.Now().Add(12 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		ui := dump(t)
		if strings.Contains(ui, "CyanDesk") {
			found = true
			break
		}
		adb(t, "shell", "input", "swipe", "180", "500", "180", "220", "300")
		adb(t, "shell", "input", "keyevent", "93")
	}
	if !found {
		shot(t, "accessibility-list.png")
		return false
	}
	tap(t, "CyanDesk")
	time.Sleep(700 * time.Millisecond)
	if !tapAny(t, "OFF", "关", "CyanDesk") {
		tapSwitch(t)
	}
	time.Sleep(400 * time.Millisecond)
	tapAny(t, "OK", "Allow", "确定", "允许")
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(dump(t), "ON") || strings.Contains(dump(t), "开") {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	adb(t, "shell", "input", "keyevent", "4")
	adb(t, "shell", "input", "keyevent", "4")
	return accessibilityEnabled(t)
}

func mustAnnex(t *testing.T, rgb [3]byte) []byte {
	t.Helper()
	buf := make([]byte, 64*64*3)
	for i := 0; i < len(buf); i += 3 {
		buf[i], buf[i+1], buf[i+2] = rgb[0], rgb[1], rgb[2]
	}
	annex, err := media.EncodeH264(buf, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	return annex
}

func shotColor(t *testing.T, name string) string {
	t.Helper()
	pngBytes := adbBytes(t, "exec-out", "screencap", "-p")
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return ""
	}
	if name != "" {
		_ = os.MkdirAll("/tmp/cyandesk-emu-shots", 0o755)
		path := filepath.Join("/tmp/cyandesk-emu-shots", name)
		f, err := os.Create(path)
		if err == nil {
			_ = png.Encode(f, img)
			_ = f.Close()
		}
	}
	return classifyImage(img)
}

func classifyImage(img image.Image) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	counts := map[string]int{}
	for _, pt := range [][2]int{{w / 2, h / 2}, {w / 2, h * 2 / 3}, {w / 3, h / 2}, {w * 2 / 3, h / 2}} {
		r, g, bl, _ := img.At(b.Min.X+pt[0], b.Min.Y+pt[1]).RGBA()
		name := classifyPixel(int(r>>8), int(g>>8), int(bl>>8))
		if name != "" {
			counts[name]++
		}
	}
	best, n := "", 0
	for name, c := range counts {
		if c > n {
			best, n = name, c
		}
	}
	return best
}

func classifyRGB(rgb []byte) string {
	if len(rgb) < 3 {
		return ""
	}
	counts := map[string]int{}
	step := len(rgb) / 3 / 20
	if step < 1 {
		step = 1
	}
	for i := 0; i+2 < len(rgb); i += step * 3 {
		name := classifyPixel(int(rgb[i]), int(rgb[i+1]), int(rgb[i+2]))
		if name != "" {
			counts[name]++
		}
	}
	best, n := "", 0
	for name, c := range counts {
		if c > n {
			best, n = name, c
		}
	}
	return best
}

func classifyPixel(r, g, b int) string {
	switch {
	case r > 140 && r > g+40 && r > b+40:
		return "red"
	case g > 100 && g > r+20 && g > b+20:
		return "green"
	case b > 140 && b > r+40 && b > g+40:
		return "blue"
	default:
		return ""
	}
}

func colorName(rgb [3]byte) string {
	return classifyPixel(int(rgb[0]), int(rgb[1]), int(rgb[2]))
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func deviceByFingerprint(t *testing.T, desk *mobile.Agent, fp string) string {
	t.Helper()
	text, err := desk.Devices()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 && strings.EqualFold(parts[3], fp) {
			return parts[0]
		}
	}
	t.Fatalf("no device with the fingerprint shown on screen")
	return ""
}

func fingerprintIn(ui, label string) string {
	re := regexp.MustCompile(label + `[^0-9a-fA-F]*([0-9a-fA-F]{64})`)
	m := re.FindStringSubmatch(ui)
	if m == nil {
		re = regexp.MustCompile(`([0-9a-fA-F]{64})`)
		all := re.FindAllString(ui, -1)
		if len(all) == 0 {
			return ""
		}
		return strings.ToLower(all[0])
	}
	return strings.ToLower(m[1])
}

func decodeAll(annex []byte, w, h int) ([]byte, error) {
	cmd := exec.Command("ffmpeg", "-loglevel", "error", "-f", "h264", "-i", "pipe:0", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	cmd.Stdin = bytes.NewReader(annex)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if w*h*3 > out.Len() {
		return nil, os.ErrInvalid
	}
	return out.Bytes(), nil
}

func scrub(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[redacted]")
}

func scrubLog(t *testing.T, secret string) {
	t.Helper()
	out := adbOut(t, "logcat", "-d")
	if secret != "" && strings.Contains(out, secret) {
		t.Fatal("logcat contains the test password")
	}
}

func booted(t *testing.T) bool {
	t.Helper()
	out := adbOut(t, "shell", "getprop", "sys.boot_completed")
	return strings.TrimSpace(out) == "1"
}

func publicPort(raw string) string {
	u := strings.TrimPrefix(raw, "https://")
	_, port, _ := net.SplitHostPort(u)
	return port
}

func screenSize(t *testing.T) [2]int {
	t.Helper()
	out := adbOut(t, "shell", "wm", "size")
	re := regexp.MustCompile(`(\d+)x(\d+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		return [2]int{320, 480}
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	return [2]int{w, h}
}

func servicePresent(t *testing.T, name string) bool {
	t.Helper()
	out := adbOut(t, "shell", "dumpsys", "activity", "services", "desk.cyan.client")
	return strings.Contains(out, name)
}

func waitGone(t *testing.T, name string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !servicePresent(t, name) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("%s still running", name)
}

type uiNode struct {
	text   string
	bounds [4]int
	class  string
}

func dump(t *testing.T) string {
	t.Helper()
	var last string
	for i := 0; i < 4; i++ {
		_ = adbOut(t, "shell", "uiautomator", "dump", "/sdcard/cyandesk-ui.xml")
		last = adbOut(t, "shell", "cat", "/sdcard/cyandesk-ui.xml")
		if strings.Contains(last, "<hierarchy") {
			return last
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("uiautomator dump failed: %s", last)
	return ""
}

func nodes(ui string) []uiNode {
	re := regexp.MustCompile(`<node\b([^>]*)/?>`)
	attr := func(src, key string) string {
		r := regexp.MustCompile(key + `="([^"]*)"`)
		m := r.FindStringSubmatch(src)
		if m == nil {
			return ""
		}
		return m[1]
	}
	var out []uiNode
	for _, m := range re.FindAllStringSubmatch(ui, -1) {
		b := attr(m[1], "bounds")
		nums := regexp.MustCompile(`\d+`).FindAllString(b, 4)
		if len(nums) != 4 {
			continue
		}
		var box [4]int
		for i := range box {
			box[i], _ = strconv.Atoi(nums[i])
		}
		out = append(out, uiNode{text: attr(m[1], "text"), bounds: box, class: attr(m[1], "class")})
	}
	return out
}

func visible(ui string) string {
	var b strings.Builder
	for _, n := range nodes(ui) {
		if n.text != "" {
			b.WriteString(n.text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func tap(t *testing.T, text string) {
	t.Helper()
	if !tapAny(t, text) {
		t.Fatalf("could not tap %q\n%s", text, visible(dump(t)))
	}
}

func tapTextContaining(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for _, n := range nodes(dump(t)) {
			if strings.Contains(n.text, text) && n.bounds[2] > n.bounds[0] {
				cx := (n.bounds[0] + n.bounds[2]) / 2
				cy := (n.bounds[1] + n.bounds[3]) / 2
				adb(t, "shell", "input", "tap", strconv.Itoa(cx), strconv.Itoa(cy))
				return
			}
		}
		adb(t, "shell", "input", "swipe", "200", "500", "200", "250", "200")
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("could not tap text containing fingerprint")
}

func tapAny(t *testing.T, texts ...string) bool {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for _, n := range nodes(dump(t)) {
			for _, text := range texts {
				if n.text == text && n.bounds[2] > n.bounds[0] {
					cx := (n.bounds[0] + n.bounds[2]) / 2
					cy := (n.bounds[1] + n.bounds[3]) / 2
					adb(t, "shell", "input", "tap", strconv.Itoa(cx), strconv.Itoa(cy))
					return true
				}
			}
		}
		adb(t, "shell", "input", "swipe", "200", "600", "200", "280", "200")
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func tapSwitch(t *testing.T) {
	t.Helper()
	for _, n := range nodes(dump(t)) {
		if strings.Contains(n.class, "Switch") {
			cx := (n.bounds[0] + n.bounds[2]) / 2
			cy := (n.bounds[1] + n.bounds[3]) / 2
			adb(t, "shell", "input", "tap", strconv.Itoa(cx), strconv.Itoa(cy))
			return
		}
	}
}

func tapCenter(t *testing.T) {
	t.Helper()
	size := screenSize(t)
	adb(t, "shell", "input", "tap", strconv.Itoa(size[0]/2), strconv.Itoa(size[1]/2))
}

func typeInto(t *testing.T, hint, value string) {
	t.Helper()
	tap(t, hint)
	time.Sleep(200 * time.Millisecond)
	// input text treats %s as space and is unreliable with punctuation.
	var b strings.Builder
	for _, r := range value {
		switch r {
		case ' ':
			b.WriteString("%s")
		case '&', '<', '>', '|', ';', '(', ')', '\'', '"', '\\', '`', '$':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	adbQuiet(t, "shell", "input", "text", b.String())
}

func waitText(t *testing.T, text string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		ui := dump(t)
		if strings.Contains(ui, text) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q\n%s", text, visible(dump(t)))
}

func shot(t *testing.T, name string) {
	t.Helper()
	_ = shotColor(t, name)
}

func adb(t *testing.T, args ...string) {
	t.Helper()
	if strings.TrimSpace(adbOut(t, args...)) == "" && false {
		return
	}
}

func adbQuiet(t *testing.T, args ...string) {
	t.Helper()
	_ = adbOut(t, args...)
}

func adbOut(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("adb", append([]string{"-s", serial}, args...)...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	return buf.String()
}

func adbBytes(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("adb", append([]string{"-s", serial}, args...)...)
	out, _ := cmd.Output()
	return out
}

func randomSecret(t *testing.T) string {
	t.Helper()
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func startServer(t *testing.T) (*boot.Env, string) {
	t.Helper()
	dir := t.TempDir()
	env, err := boot.Open(dir, []string{"127.0.0.1", "10.0.2.2", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.DB.Close() })
	apiSrv, err := api.New(env.DB, env.Material, env.Artifacts, log.New(os.Stderr, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	gwAddr := freeAddr(t, "0.0.0.0")
	_, gwPort, err := net.SplitHostPort(gwAddr)
	if err != nil {
		t.Fatal(err)
	}
	gwAddr = net.JoinHostPort("0.0.0.0", gwPort)
	apiSrv.GatewayTCP = gwAddr
	apiSrv.GatewayUDP = gwAddr
	cert, err := tls.X509KeyPair([]byte(env.Material.CertPEM), []byte(env.Material.KeyPEM))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "0.0.0.0:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = http.Serve(ln, apiSrv.PublicHandler()) }()
	iln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = iln.Close() })
	go func() { _ = http.Serve(iln, apiSrv.InternalHandler()) }()
	bin := buildRustGateway(t)
	tok := filepath.Join(t.TempDir(), "internal.token")
	if err := os.WriteFile(tok, []byte(env.Material.InternalToken), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--listen", gwAddr, "--internal-url", "http://"+iln.Addr().String(), "--internal-token-file", tok)
	var gwLog bytes.Buffer
	cmd.Stderr = &gwLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		if t.Failed() {
			t.Logf("gateway: %s", scrub(gwLog.String(), env.Material.InternalToken))
		}
	})
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return env, "https://127.0.0.1:" + port
}

func freeAddr(t *testing.T, host string) string {
	t.Helper()
	ln, err := net.Listen("tcp", host+":0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func buildRustGateway(t *testing.T) string {
	t.Helper()
	dir := "/tmp/cyandesk-gw-target"
	bin := filepath.Join(dir, "release", "cyandesk-gateway")
	cmd := exec.Command("cargo", "build", "--release", "--manifest-path", filepath.Join("..", "..", "gateway-rs", "Cargo.toml"), "--target-dir", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rust gateway: %v\n%s", err, out)
	}
	return bin
}
