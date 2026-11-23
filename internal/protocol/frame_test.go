package protocol

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("ticket-body")
	if err := WriteFrame(&buf, Frame{Type: TypeHello, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypeHello || string(f.Payload) != string(payload) {
		t.Fatalf("frame = %+v", f)
	}
}

func TestRejectsBadVersion(t *testing.T) {
	buf := []byte{'C', 'D', 9, TypeClose, 0, 0, 0, 0, 0, 0}
	if _, err := ReadFrame(bytes.NewReader(buf)); err != ErrVersion {
		t.Fatalf("err = %v", err)
	}
}

func TestHelloCodec(t *testing.T) {
	p := EncodeHello(RoleHost, "abc.def")
	role, ticket, err := DecodeHello(p)
	if err != nil || role != RoleHost || ticket != "abc.def" {
		t.Fatalf("role=%d ticket=%s err=%v", role, ticket, err)
	}
}
