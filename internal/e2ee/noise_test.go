package e2ee

import (
	"bytes"
	"testing"
)

func TestIKRoundTripAndRejectsWrongPeer(t *testing.T) {
	ini, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	rsp, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pro := Prologue("sess", "nonce", "view,input", "h", "c")
	a, b := NewMemPair()
	done := make(chan error, 1)
	go func() {
		s, err := Handshake(b, false, rsp, nil, pro)
		if err != nil {
			done <- err
			return
		}
		msg, err := s.Recv(nil)
		if err != nil {
			done <- err
			return
		}
		done <- s.Send(nil, append([]byte("ok:"), msg...))
	}()
	s, err := Handshake(a, true, ini, rsp.Public, pro)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(nil, []byte("frame-1")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, err := s.Recv(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("ok:frame-1")) {
		t.Fatalf("got %q", got)
	}

	other, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	c, d := NewMemPair()
	errCh := make(chan error, 1)
	go func() {
		_, err := Handshake(d, false, rsp, nil, pro)
		_ = d.Close()
		errCh <- err
	}()
	_, err = Handshake(c, true, ini, other.Public, pro)
	if err == nil {
		t.Fatal("expected handshake failure with the wrong peer key")
	}
	if err := <-errCh; err == nil {
		t.Fatal("responder accepted a handshake that used a different pinned key")
	}
}
