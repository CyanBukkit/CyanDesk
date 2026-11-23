package capture

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"cyandesk/internal/input"
	"cyandesk/internal/media"
)

func TestX11CaptureInputAndViewOnly(t *testing.T) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Fatal(err)
	}
	display := ":" + strconv.Itoa(150+os.Getpid()%50)
	xvfb := exec.Command("Xvfb", display, "-screen", "0", "320x240x24")
	if err := xvfb.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = xvfb.Process.Kill(); _ = xvfb.Wait() }()
	time.Sleep(300 * time.Millisecond)

	screens, err := ListScreens(display)
	if err != nil || len(screens) == 0 {
		t.Fatal(err, screens)
	}

	bin := filepath.Join(t.TempDir(), "box")
	build := exec.Command("gcc", "-o", bin, "testdata/box.c", "-lX11")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile box: %v %s", err, out)
	}
	box := exec.Command(bin)
	box.Env = append(os.Environ(), "DISPLAY="+display)
	if err := box.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = box.Process.Kill(); _ = box.Wait() }()
	time.Sleep(300 * time.Millisecond)

	before, err := GrabRGB(display, 320, 240)
	if err != nil {
		t.Fatal(err)
	}
	if !mostlyBlue(before) {
		t.Fatal("expected the real X11 window to start blue")
	}
	annex, err := media.EncodeH264(before, 320, 240)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := media.DecodeH264(annex, 320, 240)
	if err != nil {
		t.Fatal(err)
	}
	if !mostlyBlue(decoded) {
		t.Fatal("encoded capture did not stay blue")
	}

	gate := input.Gate{AllowInput: false}
	sink := clickSink{display: display}
	if err := gate.Apply(input.Event{Epoch: 1, Seq: 1, Kind: 2, X: 20, Y: 20, Button: 1}, &sink); err != input.ErrViewOnly {
		t.Fatal(err)
	}
	if sink.n != 0 {
		t.Fatal("view-only reached xdotool")
	}
	still, err := GrabRGB(display, 320, 240)
	if err != nil {
		t.Fatal(err)
	}
	if !mostlyBlue(still) {
		t.Fatal("view-only changed the remote window")
	}

	gate.AllowInput = true
	if err := gate.Apply(input.Event{Epoch: 1, Seq: 2, Kind: 2, X: 20, Y: 20, Button: 1}, &sink); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	after, err := GrabRGB(display, 320, 240)
	if err != nil {
		t.Fatal(err)
	}
	if !hasGreen(after) {
		t.Fatal("control click did not change the X11 window")
	}
}

type clickSink struct {
	display string
	n       int
}

func (c *clickSink) Inject(ev input.Event) error {
	c.n++
	return Click(c.display, int(ev.X), int(ev.Y))
}

func mostlyBlue(rgb []byte) bool {
	var b, other int
	n := len(rgb) / 3
	for i := 0; i < n; i++ {
		if rgb[i*3+2] > 150 && rgb[i*3] < 40 && rgb[i*3+1] < 40 {
			b++
		} else {
			other++
		}
	}
	return b > n/20
}

func hasGreen(rgb []byte) bool {
	n := len(rgb) / 3
	g := 0
	for i := 0; i < n; i++ {
		if rgb[i*3+1] > 150 && rgb[i*3] < 40 && rgb[i*3+2] < 40 {
			g++
		}
	}
	return g > n/50
}
