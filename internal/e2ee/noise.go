package e2ee

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"

	"github.com/flynn/noise"
)

const SuiteName = "Noise_IK_25519_ChaChaPoly_BLAKE2s"

func Suite() noise.CipherSuite {
	return noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)
}

func GenerateKey() (noise.DHKey, error) {
	return Suite().GenerateKeypair(rand.Reader)
}

func Prologue(sessionID, nonce, scopes, hostNoise, controllerNoise string) []byte {
	return []byte("cyandesk/1\n" + sessionID + "\n" + nonce + "\n" + scopes + "\n" + hostNoise + "\n" + controllerNoise + "\n")
}

// Pipe is a message-oriented transport. Each WriteMsg/ReadMsg is one handshake
// or transport message, not a byte stream.
type Pipe interface {
	WriteMsg([]byte) error
	ReadMsg() ([]byte, error)
	Close() error
}

type Session struct {
	send *noise.CipherState
	recv *noise.CipherState
	pipe Pipe
}

func Handshake(pipe Pipe, initiator bool, static noise.DHKey, peerStatic []byte, prologue []byte) (*Session, error) {
	cfg := noise.Config{
		CipherSuite:   Suite(),
		Pattern:       noise.HandshakeIK,
		Initiator:     initiator,
		Prologue:      prologue,
		StaticKeypair: static,
	}
	if initiator {
		if len(peerStatic) != 32 {
			return nil, errors.New("e2ee: peer static key required")
		}
		cfg.PeerStatic = peerStatic
	}
	hs, err := noise.NewHandshakeState(cfg)
	if err != nil {
		return nil, err
	}
	var send, recv *noise.CipherState
	if initiator {
		msg, _, _, err := hs.WriteMessage(nil, nil)
		if err != nil {
			return nil, err
		}
		if err := pipe.WriteMsg(msg); err != nil {
			return nil, err
		}
		reply, err := pipe.ReadMsg()
		if err != nil {
			return nil, err
		}
		_, cs0, cs1, err := hs.ReadMessage(nil, reply)
		if err != nil {
			return nil, err
		}
		send, recv = cs0, cs1
	} else {
		msg, err := pipe.ReadMsg()
		if err != nil {
			return nil, err
		}
		_, _, _, err = hs.ReadMessage(nil, msg)
		if err != nil {
			return nil, err
		}
		reply, cs0, cs1, err := hs.WriteMessage(nil, nil)
		if err != nil {
			return nil, err
		}
		if err := pipe.WriteMsg(reply); err != nil {
			return nil, err
		}
		send, recv = cs1, cs0
	}
	if send == nil || recv == nil {
		return nil, errors.New("e2ee: handshake did not produce transport keys")
	}
	return &Session{send: send, recv: recv, pipe: pipe}, nil
}

func (s *Session) Send(ad, plaintext []byte) error {
	ct, err := s.send.Encrypt(nil, ad, plaintext)
	if err != nil {
		return err
	}
	return s.pipe.WriteMsg(ct)
}

func (s *Session) Recv(ad []byte) ([]byte, error) {
	ct, err := s.pipe.ReadMsg()
	if err != nil {
		return nil, err
	}
	return s.recv.Decrypt(nil, ad, ct)
}

func (s *Session) Close() error {
	if s.pipe == nil {
		return nil
	}
	return s.pipe.Close()
}

// MemoryPipe is an in-memory pair used by tests and by gluing a mux.
type memPipe struct {
	r    <-chan []byte
	w    chan []byte
	once sync.Once
}

func NewMemPair() (Pipe, Pipe) {
	a := make(chan []byte, 8)
	b := make(chan []byte, 8)
	return &memPipe{r: b, w: a}, &memPipe{r: a, w: b}
}

func (m *memPipe) WriteMsg(p []byte) error {
	cp := append([]byte(nil), p...)
	m.w <- cp
	return nil
}

func (m *memPipe) ReadMsg() ([]byte, error) {
	p, ok := <-m.r
	if !ok {
		return nil, io.EOF
	}
	return p, nil
}

func (m *memPipe) Close() error {
	m.once.Do(func() { close(m.w) })
	return nil
}
