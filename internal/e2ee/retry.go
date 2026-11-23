package e2ee

import (
	"bytes"
	"time"

	"github.com/flynn/noise"
)

// HandshakeUDP completes Noise_IK and retries the same handshake message when
// the reply is late. A later data loss does not start another handshake.
func HandshakeUDP(pipe Pipe, initiator bool, static noise.DHKey, peerStatic, prologue []byte) (*Datagram, error) {
	cfg := noise.Config{
		CipherSuite:   Suite(),
		Pattern:       noise.HandshakeIK,
		Initiator:     initiator,
		Prologue:      prologue,
		StaticKeypair: static,
	}
	if initiator {
		cfg.PeerStatic = peerStatic
	}
	hs, err := noise.NewHandshakeState(cfg)
	if err != nil {
		return nil, err
	}
	var send, recv *noise.CipherState
	var hello, reply []byte
	if initiator {
		hello, _, _, err = hs.WriteMessage(nil, nil)
		if err != nil {
			return nil, err
		}
		var got []byte
		for attempt := 0; attempt < 5; attempt++ {
			if err = pipe.WriteMsg(hello); err != nil {
				return nil, err
			}
			got, err = pipe.ReadMsg()
			if err != nil {
				continue
			}
			_, send, recv, err = hs.ReadMessage(nil, got)
			if err == nil {
				reply = got
				break
			}
		}
		if send == nil || recv == nil {
			if err == nil {
				err = errTimeout
			}
			return nil, err
		}
	} else {
		var msg []byte
		for attempt := 0; attempt < 5; attempt++ {
			msg, err = pipe.ReadMsg()
			if err != nil {
				continue
			}
			if hello != nil && bytes.Equal(msg, hello) && reply != nil {
				if err = pipe.WriteMsg(reply); err != nil {
					return nil, err
				}
				continue
			}
			if _, _, _, err = hs.ReadMessage(nil, msg); err != nil {
				continue
			}
			hello = append([]byte(nil), msg...)
			var cs0, cs1 *noise.CipherState
			reply, cs0, cs1, err = hs.WriteMessage(nil, nil)
			if err != nil {
				return nil, err
			}
			send, recv = cs1, cs0
			if err = pipe.WriteMsg(reply); err != nil {
				return nil, err
			}
			break
		}
		if send == nil || recv == nil {
			if err == nil {
				err = errTimeout
			}
			return nil, err
		}
	}
	d := NewDatagram(pipe, send, recv)
	d.hello = hello
	d.reply = reply
	return d, nil
}

var errTimeout = errText("e2ee: handshake timed out")

type errText string

func (e errText) Error() string { return string(e) }

// Keep the timeout value referenced so callers can document the bound.
const HandshakeAttempt = 400 * time.Millisecond
