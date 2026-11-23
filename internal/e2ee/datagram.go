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
	ErrReplay    = errors.New("e2ee: replayed or expired datagram")
	ErrDatagram  = errors.New("e2ee: malformed datagram")
	ErrExhausted = errors.New("e2ee: transport key nonce space is exhausted")
)

type Datagram struct {
	pipe       Pipe
	mu         sync.Mutex
	send       *noise.CipherState
	recv       *noise.CipherState
	sendN      uint64
	sendGen    uint32
	recvGen    uint32
	rekeyAt    uint64
	replay     replay
	prev       *noise.CipherState
	prevGen    uint32
	prevReplay replay
	hasPrev    bool
	hello      []byte
	reply      []byte
	trace      []uint64
}

func NewDatagram(pipe Pipe, send, recv *noise.CipherState) *Datagram {
	return &Datagram{pipe: pipe, send: send, recv: recv}
}

func (d *Datagram) Send(plaintext []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	limit := d.rekeyAt
	if limit == 0 {
		limit = rekeyEvery
	}
	if d.sendN >= limit {
		if d.sendGen == ^uint32(0) {
			return ErrExhausted
		}
		d.send.Rekey()
		d.send.SetNonce(0)
		d.sendGen++
		d.sendN = 0
	}
	n := d.sendN
	d.trace = append(d.trace, n)
	hdr := packetHeader(d.sendGen, n)
	d.send.SetNonce(n)
	ct, err := d.send.Encrypt(nil, hdr, plaintext)
	if err != nil {
		return err
	}
	d.sendN++
	out := make([]byte, len(hdr)+len(ct))
	copy(out, hdr)
	copy(out[len(hdr):], ct)
	return d.pipe.WriteMsg(out)
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
	if len(pkt) < 13+16 || pkt[0] != datagramVersion {
		return nil, ErrDatagram
	}
	hdr := pkt[:13]
	ct := pkt[13:]
	gen := binary.BigEndian.Uint32(hdr[1:5])
	n := binary.BigEndian.Uint64(hdr[5:13])
	switch {
	case gen == d.recvGen:
		return d.openCommit(d.recv, &d.replay, n, hdr, ct)
	case d.hasPrev && gen == d.prevGen:
		return d.openCommit(d.prev, &d.prevReplay, n, hdr, ct)
	case gen == d.recvGen+1:
		next := rekeyedCopy(d.recv)
		plain, err := openAt(next, n, hdr, ct)
		if err != nil {
			return nil, err
		}
		var fresh replay
		if !fresh.accept(n) {
			return nil, ErrReplay
		}
		d.prev = d.recv
		d.prevGen = d.recvGen
		d.prevReplay = d.replay
		d.hasPrev = true
		d.recv = next
		d.recvGen = gen
		d.replay = fresh
		return plain, nil
	default:
		return nil, ErrDatagram
	}
}

func (d *Datagram) openCommit(cs *noise.CipherState, rp *replay, n uint64, hdr, ct []byte) ([]byte, error) {
	plain, err := openAt(cs, n, hdr, ct)
	if err != nil {
		return nil, err
	}
	if !rp.accept(n) {
		return nil, ErrReplay
	}
	return plain, nil
}

func openAt(cs *noise.CipherState, n uint64, hdr, ct []byte) ([]byte, error) {
	cs.SetNonce(n)
	return cs.Decrypt(nil, hdr, ct)
}

func rekeyedCopy(cs *noise.CipherState) *noise.CipherState {
	next := noise.UnsafeNewCipherState(Suite(), cs.UnsafeKey(), 0)
	next.Rekey()
	next.SetNonce(0)
	return next
}

func packetHeader(gen uint32, n uint64) []byte {
	hdr := make([]byte, 13)
	hdr[0] = datagramVersion
	binary.BigEndian.PutUint32(hdr[1:5], gen)
	binary.BigEndian.PutUint64(hdr[5:13], n)
	return hdr
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
