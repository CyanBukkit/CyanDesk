package e2ee

import (
	"encoding/binary"
	"sync"
	"testing"
)

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
	a := make(chan []byte, 64)
	b := make(chan []byte, 64)
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

func TestHeaderTamperDoesNotRekey(t *testing.T) {
	left, right := handshakePair(t)
	in := inbox(right)
	if err := left.Send([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	pkt := <-in.r
	pkt[4] = 1
	in.r <- pkt
	if _, err := right.Recv(); err == nil {
		t.Fatal("tampered header was accepted")
	}
	if err := left.Send([]byte("still")); err != nil {
		t.Fatal(err)
	}
	got, err := right.Recv()
	if err != nil || string(got) != "still" {
		t.Fatalf("session desynchronized after tamper: %q %v", got, err)
	}
}

func TestReplayAndCrossSession(t *testing.T) {
	a, b := handshakePair(t)
	c, d := handshakePair(t)
	if err := a.Send([]byte("probe")); err != nil {
		t.Fatal(err)
	}
	bin := inbox(b)
	pkt := <-bin.r
	bin.r <- append([]byte(nil), pkt...)
	got, err := b.Recv()
	if err != nil || string(got) != "probe" {
		t.Fatal(err)
	}
	bin.r <- append([]byte(nil), pkt...)
	if _, err := b.Recv(); err != ErrReplay {
		t.Fatalf("replay: %v", err)
	}
	inbox(d).r <- append([]byte(nil), pkt...)
	if _, err := d.Recv(); err == nil {
		t.Fatal("cross-session packet was accepted")
	}
	if err := c.Send([]byte("own")); err != nil {
		t.Fatal(err)
	}
	got, err = d.Recv()
	if err != nil || string(got) != "own" {
		t.Fatalf("cross-session desync %q %v", got, err)
	}
}

func TestSequentialNonces(t *testing.T) {
	send, recv := handshakePair(t)
	in := inbox(recv)
	for i := 0; i < 4; i++ {
		if err := send.Send([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
		pkt := <-in.r
		t.Logf("seq %d gen %d n %d", i, binary.BigEndian.Uint32(pkt[1:5]), binary.BigEndian.Uint64(pkt[5:13]))
	}
}

func TestRekeyReorderAndConcurrentNonce(t *testing.T) {
	left, right := handshakePair(t)
	left.rekeyAt = 3
	in := inbox(right)
	var pkts [][]byte
	for i := 0; i < 5; i++ {
		if err := left.Send([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
		pkts = append(pkts, <-in.r)
	}
	in.r <- pkts[3]
	in.r <- pkts[2]
	in.r <- pkts[4]
	got, err := right.Recv()
	if err != nil || got[0] != 3 {
		t.Fatalf("new generation %v %v", got, err)
	}
	got, err = right.Recv()
	if err != nil || got[0] != 2 {
		t.Fatalf("previous generation after rekey %v %v", got, err)
	}
	got, err = right.Recv()
	if err != nil || got[0] != 4 {
		t.Fatalf("following packet %v %v", got, err)
	}
	in.r <- append([]byte(nil), pkts[3]...)
	if _, err := right.Recv(); err != ErrReplay {
		t.Fatalf("rekey replay %v", err)
	}

	send, recv := handshakePair(t)
	if n := len(inbox(recv).r); n != 0 {
		t.Fatalf("channel not empty before concurrent send: %d", n)
	}
	send.trace = nil
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := send.Send([]byte{byte(i)}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	seenN := map[uint64]int{}
	for _, n := range send.trace {
		seenN[n]++
		if seenN[n] > 1 {
			t.Fatalf("counter reused %d", n)
		}
	}
	if len(send.trace) != 32 {
		t.Fatalf("sent %d", len(send.trace))
	}
	for n := uint64(0); n < 32; n++ {
		if seenN[n] != 1 {
			t.Fatalf("missing nonce %d", n)
		}
	}
}

func inbox(d *Datagram) *dropPipe { return d.pipe.(*dropPipe) }

func handshakePair(t *testing.T) (*Datagram, *Datagram) {
	t.Helper()
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
	return left, right
}
