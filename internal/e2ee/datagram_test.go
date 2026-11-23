package e2ee

import "testing"

func TestDatagramSurvivesLossAndRejectsReplay(t *testing.T) {
	a, b := newDropPair()
	iKey, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	rKey, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pro := Prologue("s", "n", "view,input", "h", "c")
	errc := make(chan error, 2)
	var left, right *Datagram
	go func() {
		s, err := Handshake(a, true, iKey, rKey.Public, pro)
		if err != nil {
			errc <- err
			return
		}
		left = NewDatagram(a, s.send, s.recv)
		errc <- nil
	}()
	go func() {
		s, err := Handshake(b, false, rKey, nil, pro)
		if err != nil {
			errc <- err
			return
		}
		right = NewDatagram(b, s.send, s.recv)
		errc <- nil
	}()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if err := left.Send([]byte("one")); err != nil {
		t.Fatal(err)
	}
	got, err := right.Recv()
	if err != nil || string(got) != "one" {
		t.Fatalf("first %q %v", got, err)
	}
	a.dropNext = 1
	if err := left.Send([]byte("lost")); err != nil {
		t.Fatal(err)
	}
	if err := left.Send([]byte("kept")); err != nil {
		t.Fatal(err)
	}
	got, err = right.Recv()
	if err != nil || string(got) != "kept" {
		t.Fatalf("after loss %q %v", got, err)
	}
	// Reorder: hold one packet, deliver the later one first.
	a.hold = 1
	if err := left.Send([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := left.Send([]byte("b")); err != nil {
		t.Fatal(err)
	}
	a.release()
	first, err := right.Recv()
	if err != nil {
		t.Fatal(err)
	}
	second, err := right.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "b" || string(second) != "a" {
		t.Fatalf("reorder delivered %q then %q", first, second)
	}
	if err := left.Send([]byte("c")); err != nil {
		t.Fatal(err)
	}
	if err := b.inject(a.last); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Recv(); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Recv(); err != ErrReplay {
		t.Fatalf("replay err %v", err)
	}
}

type dropPipe struct {
	r        chan []byte
	w        chan []byte
	dropNext int
	hold     int
	held     []byte
	last     []byte
}

func newDropPair() (*dropPipe, *dropPipe) {
	a := make(chan []byte, 8)
	b := make(chan []byte, 8)
	return &dropPipe{r: b, w: a}, &dropPipe{r: a, w: b}
}

func (p *dropPipe) WriteMsg(b []byte) error {
	cp := append([]byte(nil), b...)
	if p.dropNext > 0 {
		p.dropNext--
		return nil
	}
	if p.hold > 0 {
		p.hold--
		p.held = cp
		return nil
	}
	p.last = cp
	p.w <- cp
	return nil
}

func (p *dropPipe) release() {
	if p.held != nil {
		p.w <- p.held
		p.held = nil
	}
}

func (p *dropPipe) inject(b []byte) error {
	p.r <- append([]byte(nil), b...)
	return nil
}

func (p *dropPipe) ReadMsg() ([]byte, error) {
	return <-p.r, nil
}

func (p *dropPipe) Close() error { return nil }
