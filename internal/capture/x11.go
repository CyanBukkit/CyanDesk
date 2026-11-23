//go:build linux

package capture

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Screen is one X11 output. It is an existing display, not a virtual monitor created by CyanDesk.
type Screen struct {
	Name   string
	Width  int
	Height int
}

func ListScreens(display string) ([]Screen, error) {
	cmd := exec.Command("xrandr", "--display", display)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var screens []Screen
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(line, " connected ") {
			continue
		}
		dim := ""
		for _, f := range fields[1:] {
			if strings.Contains(f, "x") && strings.Contains(f, "+") {
				dim = f
				break
			}
		}
		if i := strings.IndexByte(dim, '+'); i >= 0 {
			dim = dim[:i]
		}
		w, h, ok := parseSize(dim)
		if !ok {
			continue
		}
		screens = append(screens, Screen{Name: fields[0], Width: w, Height: h})
	}
	if len(screens) == 0 {
		return nil, fmt.Errorf("capture: no screens on %s", display)
	}
	return screens, nil
}

// WithDisplay returns the process environment with DISPLAY set to one server.
func WithDisplay(display string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, "DISPLAY=") {
			continue
		}
		out = append(out, item)
	}
	return append(out, "DISPLAY="+display)
}

func parseSize(s string) (int, int, bool) {
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// GrabRGB reads one frame from an existing X11 display.
func GrabRGB(display string, w, h int) ([]byte, error) {
	cmd := exec.Command("ffmpeg", "-loglevel", "error",
		"-f", "x11grab", "-video_size", fmt.Sprintf("%dx%d", w, h), "-i", display,
		"-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	cmd.Env = WithDisplay(display)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("capture: %w: %s", err, errb.String())
	}
	want := w * h * 3
	if out.Len() < want {
		return nil, fmt.Errorf("capture: short frame %d", out.Len())
	}
	return out.Bytes()[:want], nil
}

// Click asks the X server to deliver a button press. Callers must already have decided the session allows input.
func Click(display string, x, y int) error {
	cmd := exec.Command("xdotool", "mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y), "click", "1")
	cmd.Env = WithDisplay(display)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("capture: click: %w: %s", err, errb.String())
	}
	return nil
}
