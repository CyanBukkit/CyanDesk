package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	Version    byte = 1
	MaxPayload      = 8 << 20
)

const (
	TypeHello byte = iota + 1
	TypeHelloAck
	TypePeerReady
	TypeRelay
	TypeSignal
	TypeClose
	TypeHandshake
	TypeSecure
)

const (
	RoleHost       byte = 1
	RoleController byte = 2
)

const (
	AppProbe byte = iota + 1
	AppProbeAck
	AppVideo
	AppAudio
	AppInput
	AppInputAck
	AppKeyframe
	AppReleaseAll
	AppFileList
	AppFileListResult
	AppFileGet
	AppFileData
	AppReject
	AppHello
)

var (
	ErrBadMagic = errors.New("protocol: bad magic")
	ErrVersion  = errors.New("protocol: version mismatch")
	ErrTooLarge = errors.New("protocol: payload too large")
	ErrShort    = errors.New("protocol: short buffer")
)

type Frame struct {
	Type    byte
	Flags   uint16
	Payload []byte
}

func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxPayload {
		return ErrTooLarge
	}
	var hdr [10]byte
	hdr[0] = 'C'
	hdr[1] = 'D'
	hdr[2] = Version
	hdr[3] = f.Type
	binary.BigEndian.PutUint16(hdr[4:6], f.Flags)
	binary.BigEndian.PutUint32(hdr[6:10], uint32(len(f.Payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(f.Payload) == 0 {
		return nil
	}
	_, err := w.Write(f.Payload)
	return err
}

func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [10]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	if hdr[0] != 'C' || hdr[1] != 'D' {
		return Frame{}, ErrBadMagic
	}
	if hdr[2] != Version {
		return Frame{}, ErrVersion
	}
	n := binary.BigEndian.Uint32(hdr[6:10])
	if n > MaxPayload {
		return Frame{}, ErrTooLarge
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, err
	}
	return Frame{Type: hdr[3], Flags: binary.BigEndian.Uint16(hdr[4:6]), Payload: payload}, nil
}

func EncodeHello(role byte, ticket string) []byte {
	tb := []byte(ticket)
	buf := make([]byte, 1+4+len(tb))
	buf[0] = role
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(tb)))
	copy(buf[5:], tb)
	return buf
}

func DecodeHello(p []byte) (role byte, ticket string, err error) {
	if len(p) < 5 {
		return 0, "", ErrShort
	}
	n := binary.BigEndian.Uint32(p[1:5])
	if int(n) != len(p)-5 {
		return 0, "", ErrShort
	}
	return p[0], string(p[5:]), nil
}

func EncodeAck(ok bool, code uint16, message string) []byte {
	mb := []byte(message)
	if len(mb) > 400 {
		mb = mb[:400]
	}
	buf := make([]byte, 1+2+2+len(mb))
	if ok {
		buf[0] = 1
	}
	binary.BigEndian.PutUint16(buf[1:3], code)
	binary.BigEndian.PutUint16(buf[3:5], uint16(len(mb)))
	copy(buf[5:], mb)
	return buf
}

func DecodeAck(p []byte) (ok bool, code uint16, message string, err error) {
	if len(p) < 5 {
		return false, 0, "", ErrShort
	}
	n := binary.BigEndian.Uint16(p[3:5])
	if int(n) != len(p)-5 {
		return false, 0, "", ErrShort
	}
	return p[0] == 1, binary.BigEndian.Uint16(p[1:3]), string(p[5:]), nil
}

func PutU16String(dst []byte, s string) []byte {
	b := []byte(s)
	var n [2]byte
	binary.BigEndian.PutUint16(n[:], uint16(len(b)))
	dst = append(dst, n[:]...)
	return append(dst, b...)
}

func TakeU16String(p []byte) (string, []byte, error) {
	if len(p) < 2 {
		return "", nil, ErrShort
	}
	n := int(binary.BigEndian.Uint16(p[:2]))
	if len(p) < 2+n {
		return "", nil, ErrShort
	}
	return string(p[2 : 2+n]), p[2+n:], nil
}
