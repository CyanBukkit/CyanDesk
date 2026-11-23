package desktop

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

var ErrIdentity = errors.New("desktop: peer identity does not match the pinned device")

// CheckPeer refuses a session when the server's current device fingerprint
// differs from the fingerprint the user already confirmed, or when the ticket
// names a different noise key than the device record.
func CheckPeer(pinnedFingerprint, recordFingerprint, ticketNoise, recordNoise string) error {
	if pinnedFingerprint == "" || recordFingerprint == "" {
		return ErrIdentity
	}
	if !strings.EqualFold(pinnedFingerprint, recordFingerprint) {
		return ErrIdentity
	}
	if ticketNoise == "" || recordNoise == "" || ticketNoise != recordNoise {
		return ErrIdentity
	}
	return nil
}

type ticketBody struct {
	HostNoise       string   `json:"host_noise"`
	ControllerNoise string   `json:"controller_noise"`
	Nonce           string   `json:"nonce"`
	Scopes          []string `json:"scopes"`
	SID             string   `json:"sid"`
}

// TicketInfo is the public portion of a relay ticket.
type TicketInfo struct {
	SessionID       string
	HostNoise       string
	ControllerNoise string
	Nonce           string
	Scopes          []string
}

// ParseTicket reads public fields from a ticket. It does not log the ticket.
func ParseTicket(raw string) (TicketInfo, error) {
	i := strings.IndexByte(raw, '.')
	if i <= 0 {
		return TicketInfo{}, errors.New("desktop: malformed ticket")
	}
	body, err := base64.RawURLEncoding.DecodeString(raw[:i])
	if err != nil {
		return TicketInfo{}, err
	}
	var t ticketBody
	if err := json.Unmarshal(body, &t); err != nil {
		return TicketInfo{}, err
	}
	return TicketInfo{SessionID: t.SID, HostNoise: t.HostNoise, ControllerNoise: t.ControllerNoise, Nonce: t.Nonce, Scopes: t.Scopes}, nil
}

// TicketNoise reads the public noise keys from a ticket payload.
func TicketNoise(raw string) (hostNoise, controllerNoise string, err error) {
	info, err := ParseTicket(raw)
	if err != nil {
		return "", "", err
	}
	return info.HostNoise, info.ControllerNoise, nil
}
