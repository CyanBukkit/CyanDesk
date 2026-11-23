package api

import "sync"

type Event struct {
	Seq       int64  `json:"seq"`
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	State     string `json:"state,omitempty"`
}

type Hub struct {
	mu   sync.Mutex
	seq  int64
	subs map[string]map[chan Event]struct{}
}

func newHub() *Hub {
	return &Hub{subs: map[string]map[chan Event]struct{}{}}
}

func (h *Hub) Subscribe(userID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan Event]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[userID], ch)
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(userID string, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	ev.Seq = h.seq
	for ch := range h.subs[userID] {
		select {
		case ch <- ev:
		default:
		}
	}
}
