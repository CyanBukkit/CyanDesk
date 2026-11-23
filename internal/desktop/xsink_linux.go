//go:build linux

package desktop

import (
	"os/exec"
	"strconv"

	"cyandesk/internal/capture"
	"cyandesk/internal/input"
)

// XSink injects pointer and key events into one X11 display.
type XSink struct{ Display string }

func (s XSink) Inject(ev input.Event) error {
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
