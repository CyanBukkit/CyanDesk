package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"time"

	"cyandesk/internal/protocol"
)

var ErrUDP = errors.New("gateway: udp association failed")

// AssociateUDP performs the equal-size cookie exchange and sends a Hello frame
// so the Rust gateway can bind this socket to a redeemed session.
func AssociateUDP(conn *net.UDPConn, gw *net.UDPAddr, role byte, ticket string) ([]byte, error) {
	nonce := make([]byte, 32)
	if _, err := randRead(nonce); err != nil {
		return nil, err
	}
	req := make([]byte, 64)
	req[0] = 1
	copy(req[1:], nonce)
	if _, err := conn.WriteToUDP(req, gw); err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	if n != 64 || buf[0] != 2 || !hmac.Equal(buf[1:33], nonce) {
		return nil, ErrUDP
	}
	cookie := append([]byte(nil), buf[33:64]...)
	frame := protocol.EncodeHello(role, ticket)
	var framed []byte
	framed, err = frameBytes(protocol.TypeHello, frame)
	if err != nil {
		return nil, err
	}
	pkt := append([]byte{3}, cookie...)
	pkt = append(pkt, framed...)
	if _, err := conn.WriteToUDP(pkt, gw); err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Time{})
	return cookie, nil
}

// WriteUDPFrame sends one protocol frame to the gateway. The datagram is not larger
// than 32 bytes plus the frame.
func WriteUDPFrame(conn *net.UDPConn, gw *net.UDPAddr, cookie []byte, typ byte, payload []byte) error {
	if len(cookie) != 31 {
		return ErrUDP
	}
	framed, err := frameBytes(typ, payload)
	if err != nil {
		return err
	}
	pkt := append([]byte{3}, cookie...)
	pkt = append(pkt, framed...)
	_, err = conn.WriteToUDP(pkt, gw)
	return err
}

// ReadUDPFrame reads one forwarded frame. Packets that fail the cookie check are skipped.
func ReadUDPFrame(conn *net.UDPConn, cookie []byte, deadline time.Time) (byte, []byte, error) {
	buf := make([]byte, 2048)
	for {
		if !deadline.IsZero() {
			_ = conn.SetReadDeadline(deadline)
		}
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return 0, nil, err
		}
		if n < 32 || buf[0] != 3 || !hmac.Equal(buf[1:32], cookie) {
			continue
		}
		fr, err := protocol.ReadFrame(bytesReader(buf[32:n]))
		if err != nil {
			continue
		}
		return fr.Type, fr.Payload, nil
	}
}

func frameBytes(typ byte, payload []byte) ([]byte, error) {
	var buf bytesBuf
	if err := protocol.WriteFrame(&buf, protocol.Frame{Type: typ, Payload: payload}); err != nil {
		return nil, err
	}
	return buf.b, nil
}

type bytesBuf struct{ b []byte }

func (w *bytesBuf) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

func bytesReader(p []byte) *sliceReader { return &sliceReader{b: p} }

type sliceReader struct {
	b []byte
	i int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, errors.New("gateway: short udp frame")
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func randRead(b []byte) (int, error) {
	// local import avoided; use crypto/rand via a tiny wrapper in this file
	return cryptoRand(b)
}

func cookieMAC(secret, nonce []byte, addr string) []byte {
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write(nonce)
	_, _ = m.Write([]byte(addr))
	sum := m.Sum(nil)
	return sum[:31]
}

// keep binary import used if tests build tags change
var _ = binary.BigEndian
