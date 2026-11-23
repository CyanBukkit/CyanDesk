package desktop

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	"encoding/base64"

	"cyandesk/internal/capture"
	"cyandesk/internal/e2ee"
	"cyandesk/internal/input"
	"cyandesk/internal/protocol"
	"cyandesk/internal/remote"
	"github.com/flynn/noise"
)

// OpenAuthorized checks the pinned fingerprint and the ticket's noise key, then
// opens the encrypted path. A demo flag cannot skip this.
func OpenAuthorized(ctx context.Context, gatewayAddr string, role byte, ticket, pinnedFingerprint, recordFingerprint, recordNoise string, static noise.DHKey) (*remote.Result, error) {
	info, err := ParseTicket(ticket)
	if err != nil {
		return nil, err
	}
	ticketNoise := info.ControllerNoise
	if role == protocol.RoleController {
		ticketNoise = info.HostNoise
	}
	if err := CheckPeer(pinnedFingerprint, recordFingerprint, ticketNoise, recordNoise); err != nil {
		return nil, err
	}
	var peerStatic []byte
	if role == protocol.RoleController {
		peerStatic, err = decodeNoise(recordNoise)
		if err != nil {
			return nil, err
		}
	}
	return remote.Open(ctx, remote.Side{
		Gateway: gatewayAddr, Role: role, Ticket: ticket, Static: static, PeerStatic: peerStatic,
		Prologue: e2ee.Prologue(info.SessionID, info.Nonce, strings.Join(info.Scopes, ","), info.HostNoise, info.ControllerNoise),
	})
}

func decodeNoise(s string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(s)
}

func HasInput(scopes []string) bool {
	for _, s := range scopes {
		if s == "input" {
			return true
		}
	}
	return false
}

// XSink injects pointer and key events into one X display.
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
