package mobile

import (
	"context"
	"errors"

	"cyandesk/internal/desktop"
	"cyandesk/internal/input"
	"cyandesk/internal/protocol"
)

// Packet is one application message after Noise decryption.
type Packet struct {
	Kind  int
	Seq   int
	W     int
	H     int
	X     int
	Y     int
	Annex []byte
}

// Link is an authenticated session. Media bytes are already inside Noise.
type Link struct {
	res interface {
		Send(byte, []byte) error
		Recv() (byte, []byte, error)
		Close()
		Current() string
	}
	stop    context.CancelFunc
	session string
}

// SendVideo sends one annex-B access unit in the desktop video container.
func (l *Link) SendVideo(seq, w, h int, annex []byte) error {
	if l == nil || l.res == nil {
		return errors.New("连接已关闭")
	}
	if len(annex) == 0 || w < 2 || h < 2 {
		return errors.New("没有可发送的画面")
	}
	return l.res.Send(protocol.AppVideo, desktop.EncodeVideo(uint32(seq), w, h, annex))
}

// SendPointer sends one click in frame coordinates. The host decides whether
// the ticket scope allows injection.
func (l *Link) SendPointer(seq, x, y int) error {
	if l == nil || l.res == nil {
		return errors.New("连接已关闭")
	}
	return l.res.Send(protocol.AppInput, desktop.EncodeInput(input.Event{
		Epoch: 1, Seq: uint64(seq), Kind: 2, X: int32(x), Y: int32(y), Button: 1,
	}))
}

// Recv blocks until the next application packet.
func (l *Link) Recv() (*Packet, error) {
	if l == nil || l.res == nil {
		return nil, errors.New("连接已关闭")
	}
	kind, payload, err := l.res.Recv()
	if err != nil {
		return nil, err
	}
	pkt := &Packet{Kind: int(kind)}
	switch kind {
	case protocol.AppVideo:
		seq, w, h, annex, err := desktop.DecodeVideo(payload)
		if err != nil {
			return nil, err
		}
		pkt.Seq = int(seq)
		pkt.W = w
		pkt.H = h
		pkt.Annex = annex
	case protocol.AppInput:
		ev, err := desktop.DecodeInput(payload)
		if err != nil {
			return nil, err
		}
		pkt.Seq = int(ev.Seq)
		pkt.X = int(ev.X)
		pkt.Y = int(ev.Y)
	default:
		pkt.Annex = append([]byte(nil), payload...)
	}
	return pkt, nil
}

// Path is the selected lane name, such as relay or direct-udp.
func (l *Link) Path() string {
	if l == nil || l.res == nil {
		return ""
	}
	return l.res.Current()
}

// Close stops the session transport.
func (l *Link) Close() {
	if l == nil {
		return
	}
	if l.stop != nil {
		l.stop()
	}
	if l.res != nil {
		l.res.Close()
	}
}
