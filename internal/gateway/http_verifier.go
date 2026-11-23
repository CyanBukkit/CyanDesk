package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPVerifier asks the control process to redeem tickets. It does not see media keys.
type HTTPVerifier struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (h HTTPVerifier) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return http.DefaultClient
}

func (h HTTPVerifier) Redeem(ctx context.Context, ticket string) (Claims, error) {
	body, _ := json.Marshal(map[string]string{"ticket": ticket})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.BaseURL, "/")+"/api/v1/internal/tickets/redeem", bytes.NewReader(body))
	if err != nil {
		return Claims{}, err
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := h.client().Do(req)
	if err != nil {
		return Claims{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return Claims{}, fmt.Errorf("%w: %s", ErrDenied, res.Status)
	}
	var payload struct {
		SessionID       string   `json:"session_id"`
		HostID          string   `json:"host_id"`
		ControllerID    string   `json:"controller_id"`
		Scopes          []string `json:"scopes"`
		BytesLimit      int64    `json:"bytes_limit"`
		HostNoise       string   `json:"host_noise"`
		ControllerNoise string   `json:"controller_noise"`
		Nonce           string   `json:"nonce"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Claims{}, err
	}
	return Claims{
		SessionID: payload.SessionID, HostID: payload.HostID, ControllerID: payload.ControllerID,
		Scopes: payload.Scopes, BytesLimit: payload.BytesLimit, HostNoise: payload.HostNoise,
		ControllerNoise: payload.ControllerNoise, Nonce: payload.Nonce,
	}, nil
}

func (h HTTPVerifier) SessionOpen(ctx context.Context, sessionID string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.BaseURL, "/")+"/api/v1/internal/sessions/"+sessionID, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	res, err := h.client().Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	var payload struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return false, err
	}
	return payload.State == "active", nil
}
