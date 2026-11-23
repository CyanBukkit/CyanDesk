package e2ee

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/flynn/noise"
)

// Datagram carries Noise_IK transport messages with an explicit nonce.
// The AEAD is ChaChaPoly from flynn/noise. A lost or reordered datagram does
// not advance the peer past a gap, and a repeated nonce inside the replay
// window is rejected. Rekey uses CipherState.Rekey from the Noise spec and
// then sets the nonce back to zero, so a key is never reused with an old nonce.
const (
	datagramVersion = 1
	rekeyEvery      = 1 << 16
	replayWindow    = 2048
)

var (
	ErrReplay   = errors.New("e2ee: replayed or expired datagram")
	ErrDatagram = errors.New("e2ee: malformed datagram")
)

type Datagram struct {
	pipe    Pipe
	mu      sync.Mutex
	send    *noise.CipherState
	recv    *noise.CipherState
	sendN   uint64
	sendGen uint32
	recvGen uint32
	replay  replay
	hello   []byte
	reply   []byte
}

func NewDatagram(pipe Pipe, send, recv *noise.CipherState) *Datagram {
	return &Datagram{pipe: pipe, send: send, recv: recv}
}

func (d *Datagram) Send(plaintext []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sendN == rekeyEvery {
		d.send.Rekey()
		d.send.SetNonce(0)
		d.sendGen++
		d.sendN = 0
	}
	n := d.sendN
	d.send.SetNonce(n)
	ct, err := d.send.Encrypt(nil, nil, plaintext)
	if err != nil {
		return err
	}
	d.sendN++
	buf := make([]byte, 13+len(ct))
	buf[0] = datagramVersion
	binary.BigEndian.PutUint32(buf[1:5], d.sendGen)
	binary.BigEndian.PutUint64(buf[5:13], n)
	copy(buf[13:], ct)
	return d.pipe.WriteMsg(buf)
}

func (d *Datagram) Recv() ([]byte, error) {
	pkt, err := d.pipe.ReadMsg()
	if err != nil {
		return nil, err
	}
	if d.hello != nil && bytes.Equal(pkt, d.hello) && d.reply != nil {
		_ = d.pipe.WriteMsg(d.reply)
		return d.Recv()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(pkt) < 13 || pkt[0] != datagramVersion {
		return nil, ErrDatagram
	}
	gen := binary.BigEndian.Uint32(pkt[1:5])
	n := binary.BigEndian.Uint64(pkt[5:13])
	if gen == d.recvGen+1 && d.recvGen != ^uint32(0) {
		d.recv.Rekey()
		d.recv.SetNonce(0)
		d.recvGen = gen
		d.replay = replay{}
	}
	if gen != d.recvGen {
		return nil, ErrDatagram
	}
	if !d.replay.accept(n) {
		return nil, ErrReplay
	}
	d.recv.SetNonce(n)
	return d.recv.Decrypt(nil, nil, pkt[13:])
}

func (d *Datagram) Close() error {
	if d.pipe == nil {
		return nil
	}
	return d.pipe.Close()
}

// replay is a 2048-bit sliding window. Bit 0 is the newest accepted nonce.
// It is an anti-replay bitmap, not a cipher.
type replay struct {
	last uint64
	set  bool
	bits [replayWindow / 64]uint64
}

func (r *replay) accept(n uint64) bool {
	if !r.set {
		r.last = n
		r.set = true
		r.bits[0] = 1
		return true
	}
	if n > r.last {
		r.shift(n - r.last)
		r.bits[0] |= 1
		r.last = n
		return true
	}
	delta := r.last - n
	if delta >= replayWindow {
		return false
	}
	word := delta / 64
	bit := delta % 64
	mask := uint64(1) << bit
	if r.bits[word]&mask != 0 {
		return false
	}
	r.bits[word] |= mask
	return true
}

func (r *replay) shift(by uint64) {
	if by >= replayWindow {
		for i := range r.bits {
			r.bits[i] = 0
		}
		return
	}
	var next [replayWindow / 64]uint64
	for i := uint64(0); i < replayWindow; i++ {
		if r.bits[i/64]&(uint64(1)<<uint(i%64)) == 0 {
			continue
		}
		j := i + by
		if j < replayWindow {
			next[j/64] |= uint64(1) << uint(j%64)
		}
	}
	r.bits = next
}
