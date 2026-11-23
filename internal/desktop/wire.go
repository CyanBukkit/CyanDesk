package desktop

import (
	"encoding/binary"
	"errors"

	"cyandesk/internal/input"
	"cyandesk/internal/protocol"
)

const (
	PolicyView    byte = 0
	PolicyControl byte = 1
)

var ErrShort = errors.New("desktop: short message")

func EncodeVideo(seq uint32, w, h int, annex []byte) []byte {
	buf := make([]byte, 12+len(annex))
	binary.BigEndian.PutUint32(buf[0:4], seq)
	binary.BigEndian.PutUint16(buf[4:6], uint16(w))
	binary.BigEndian.PutUint16(buf[6:8], uint16(h))
	binary.BigEndian.PutUint32(buf[8:12], uint32(len(annex)))
	copy(buf[12:], annex)
	return buf
}

func DecodeVideo(p []byte) (seq uint32, w, h int, annex []byte, err error) {
	if len(p) < 12 {
		return 0, 0, 0, nil, ErrShort
	}
	seq = binary.BigEndian.Uint32(p[0:4])
	w = int(binary.BigEndian.Uint16(p[4:6]))
	h = int(binary.BigEndian.Uint16(p[6:8]))
	n := int(binary.BigEndian.Uint32(p[8:12]))
	if n < 0 || len(p) < 12+n {
		return 0, 0, 0, nil, ErrShort
	}
	return seq, w, h, append([]byte(nil), p[12:12+n]...), nil
}

func EncodeInput(e input.Event) []byte {
	buf := make([]byte, 4+8+1+4+4+1+4)
	binary.BigEndian.PutUint32(buf[0:4], e.Epoch)
	binary.BigEndian.PutUint64(buf[4:12], e.Seq)
	buf[12] = e.Kind
	binary.BigEndian.PutUint32(buf[13:17], uint32(e.X))
	binary.BigEndian.PutUint32(buf[17:21], uint32(e.Y))
	buf[21] = e.Button
	binary.BigEndian.PutUint32(buf[22:26], e.Key)
	return buf
}

func DecodeInput(p []byte) (input.Event, error) {
	if len(p) < 26 {
		return input.Event{}, ErrShort
	}
	return input.Event{
		Epoch:  binary.BigEndian.Uint32(p[0:4]),
		Seq:    binary.BigEndian.Uint64(p[4:12]),
		Kind:   p[12],
		X:      int32(binary.BigEndian.Uint32(p[13:17])),
		Y:      int32(binary.BigEndian.Uint32(p[17:21])),
		Button: p[21],
		Key:    binary.BigEndian.Uint32(p[22:26]),
	}, nil
}

// VideoQueue is a bounded latest-frame queue. Push drops the oldest frame when full.
type VideoQueue struct {
	ch      chan []byte
	Dropped int
}

func NewVideoQueue(n int) *VideoQueue {
	if n < 1 {
		n = 1
	}
	return &VideoQueue{ch: make(chan []byte, n)}
}

func (q *VideoQueue) Push(frame []byte) {
	select {
	case q.ch <- frame:
		return
	default:
	}
	select {
	case <-q.ch:
		q.Dropped++
	default:
	}
	select {
	case q.ch <- frame:
	default:
		q.Dropped++
	}
}

func (q *VideoQueue) Pop() ([]byte, bool) {
	select {
	case f := <-q.ch:
		return f, true
	default:
		return nil, false
	}
}

// AppVideo is the protocol type used for encoded frames.
const AppVideo = protocol.AppVideo
