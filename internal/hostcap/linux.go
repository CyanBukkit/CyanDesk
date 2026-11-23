//go:build linux

package hostcap

import (
	"errors"
	"os"
	"os/exec"
	"strconv"

	"cyandesk/internal/capture"
	"cyandesk/internal/input"
	"cyandesk/internal/media"
)

const Name = "linux-x11"

func RequireSession() error {
	if os.Getenv("DISPLAY") == "" {
		return errors.New("没有图形会话。无头主机不会伪造屏幕。")
	}
	return nil
}

func Grab(display string, w, h int) ([]byte, error) {
	return capture.GrabRGB(display, w, h)
}

func Encode(rgb []byte, w, h int) ([]byte, error) {
	return media.EncodeH264(rgb, w, h)
}

// Sink injects into an existing X11 display.
type Sink struct{ Display string }

func (s Sink) Inject(ev input.Event) error {
	if ev.Kind == 4 || ev.Kind == 5 {
		action := "keydown"
		if ev.Kind == 5 {
			action = "keyup"
		}
		cmd := exec.Command("xdotool", action, strconv.FormatUint(uint64(ev.Key), 10))
		cmd.Env = capture.WithDisplay(s.Display)
		return cmd.Run()
	}
	return capture.Click(s.Display, int(ev.X), int(ev.Y))
}
