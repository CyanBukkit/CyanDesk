package remote

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/gateway"
	"cyandesk/internal/protocol"
)

func TestOpenLiveMovesWhenDirectIsSlowerThenReturns(t *testing.T) {
	gw := gateway.New(&allowAll{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = gw.Serve(ln) }()
	delay, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer delay.Close()
	go serveDelay(delay, ln.Addr().String(), 20*time.Millisecond)
	proxy, stop, drop := listenDrop(t)
	defer stop()

	hostKey, _ := e2ee.GenerateKey()
	ctrlKey, _ := e2ee.GenerateKey()
	pro := e2ee.Prologue("s", "n", "view,input", "h", "c")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := OpenLive(ctx, Side{
			Gateway: delay.Addr().String(), Role: protocol.RoleHost, Ticket: "good",
			Static: hostKey, Prologue: pro,
		}, Direct{Advertise: proxy})
		errc <- err
	}()
	res, err := OpenLive(ctx, Side{
		Gateway: delay.Addr().String(), Role: protocol.RoleController, Ticket: "good",
		Static: ctrlKey, PeerStatic: hostKey.Public, Prologue: pro,
	}, Direct{})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(18 * time.Second)
	for time.Now().Before(deadline) && res.Current() != "direct-udp" {
		time.Sleep(100 * time.Millisecond)
	}
	if res.Current() != "direct-udp" {
		t.Fatalf("did not select direct-udp, current=%s", res.Current())
	}
	drop.Store(true)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && res.Current() != "relay" {
		time.Sleep(50 * time.Millisecond)
	}
	if res.Current() != "relay" {
		t.Fatalf("path %s", res.Current())
	}
	if err := res.Send(protocol.AppVideo, []byte{9}); err != nil {
		t.Fatal(err)
	}
}

type allowAll struct{}

func (allowAll) Redeem(context.Context, string) (gateway.Claims, error) {
	return gateway.Claims{SessionID: "s1", HostID: "h", ControllerID: "c", BytesLimit: 8 << 20, Nonce: "n", HostNoise: "hn", ControllerNoise: "cn"}, nil
}

func (allowAll) SessionOpen(context.Context, string) (bool, error) { return true, nil }

func serveDelay(ln net.Listener, target string, delay time.Duration) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			d, err := net.Dial("tcp", target)
			if err != nil {
				return
			}
			defer d.Close()
			buf := make([]byte, 32*1024)
			done := make(chan struct{}, 2)
			copyOne := func(dst, src net.Conn) {
				defer func() { done <- struct{}{} }()
				for {
					n, err := src.Read(buf)
					if n > 0 {
						time.Sleep(delay)
						if _, werr := dst.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}
			go copyOne(d, c)
			copyOne(c, d)
			<-done
		}(c)
	}
}

func listenDrop(t *testing.T) (string, func(), *atomic.Bool) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	drop := &atomic.Bool{}
	go func() {
		buf := make([]byte, 65535)
		var host, ctrl *net.UDPAddr
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n == 1 && buf[0] == 0 {
				cp := *from
				host = &cp
				continue
			}
			var dest *net.UDPAddr
			if host != nil && from.String() == host.String() {
				dest = ctrl
			} else if host != nil {
				cp := *from
				ctrl = &cp
				dest = host
			}
			if drop.Load() || dest == nil {
				continue
			}
			_, _ = conn.WriteToUDP(append([]byte(nil), buf[:n]...), dest)
		}
	}()
	return conn.LocalAddr().String(), func() { _ = conn.Close() }, drop
}
