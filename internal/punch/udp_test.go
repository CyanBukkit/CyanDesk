package punch

import (
	"net"
	"testing"
	"time"
)

func TestUDPCookieIsEqualSizeAndAuthenticates(t *testing.T) {
	a, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	secret := []byte("test-cookie-secret-32-bytes-long!")
	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 128)
		_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, from, err := b.ReadFromUDP(buf)
		if err != nil {
			errCh <- err
			return
		}
		if n != packetLen || buf[0] != 1 {
			errCh <- ErrCookie
			return
		}
		cookie := MakeCookie(secret, buf[1:33], from)
		if !CheckCookie(secret, buf[1:33], cookie, from) {
			errCh <- ErrCookie
			return
		}
		_, err = b.WriteToUDP(CookieReply(buf[1:33], cookie), from)
		errCh <- err
	}()
	rtt, err := Exchange(a, b.LocalAddr().(*net.UDPAddr), secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if rtt <= 0 || rtt > time.Second {
		t.Fatalf("rtt %s", rtt)
	}
	req := CookieRequest(make([]byte, 32))
	rep := CookieReply(make([]byte, 32), make([]byte, 31))
	if len(req) != len(rep) {
		t.Fatalf("amplification: request %d reply %d", len(req), len(rep))
	}
}
