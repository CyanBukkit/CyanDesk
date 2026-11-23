package desktop

import (
	"context"
	"strings"

	"encoding/base64"

	"cyandesk/internal/e2ee"
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
