package secret

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Ticket struct {
	V               int      `json:"v"`
	JTI             string   `json:"jti"`
	SID             string   `json:"sid"`
	HostID          string   `json:"host_id"`
	ControllerID    string   `json:"controller_id"`
	HostNoise       string   `json:"host_noise"`
	ControllerNoise string   `json:"controller_noise"`
	Scopes          []string `json:"scopes"`
	Nonce           string   `json:"nonce"`
	Expires         int64    `json:"exp"`
	ServerID        string   `json:"server_id"`
}

func SignTicket(priv ed25519.PrivateKey, t Ticket) (string, error) {
	body, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func VerifyTicket(pub ed25519.PublicKey, raw string, now time.Time) (Ticket, error) {
	var zero Ticket
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return zero, errors.New("ticket: malformed")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return zero, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, err
	}
	if !ed25519.Verify(pub, body, sig) {
		return zero, errors.New("ticket: bad signature")
	}
	var t Ticket
	if err := json.Unmarshal(body, &t); err != nil {
		return zero, err
	}
	if now.Unix() >= t.Expires {
		return zero, errors.New("ticket: expired")
	}
	return t, nil
}

func JTIHash(jti string) string {
	sum := sha256.Sum256([]byte(jti))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
