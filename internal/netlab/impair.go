package netlab

import (
	"io"
	"net"
	"sync"
	"time"
)

// TCPProxy forwards connections, delaying each direction and dropping the first dropFirst
// writes. This is an in-path impairment, not a sleep inside the path selector.
type TCPProxy struct {
	Listen    string
	Target    string
	Delay     time.Duration
	DropFirst int
}

func (p TCPProxy) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			d, err := net.Dial("tcp", p.Target)
			if err != nil {
				return
			}
			defer d.Close()
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); copyImpair(d, c, p.Delay, p.DropFirst) }()
			go func() { defer wg.Done(); copyImpair(c, d, p.Delay, p.DropFirst) }()
			wg.Wait()
		}(c)
	}
}

func copyImpair(dst, src net.Conn, delay time.Duration, dropFirst int) {
	buf := make([]byte, 32*1024)
	dropped := 0
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if dropped < dropFirst {
				dropped++
				continue
			}
			if delay > 0 {
				time.Sleep(delay)
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// UDPProxy is a one-socket-per-client NAT with delay and a deterministic
// number of dropped datagrams. Reply size is not increased.
type UDPProxy struct {
	Listen    string
	Target    string
	Delay     time.Duration
	DropFirst int
}

func (p UDPProxy) Serve(conn *net.UDPConn, target *net.UDPAddr) {
	type flow struct {
		sock *net.UDPConn
		drop int
	}
	var mu sync.Mutex
	flows := map[string]*flow{}
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		payload := append([]byte(nil), buf[:n]...)
		mu.Lock()
		f := flows[from.String()]
		if f == nil {
			s, err := net.ListenUDP("udp", &net.UDPAddr{IP: conn.LocalAddr().(*net.UDPAddr).IP})
			if err != nil {
				mu.Unlock()
				continue
			}
			f = &flow{sock: s}
			flows[from.String()] = f
			go func(s *net.UDPConn, client *net.UDPAddr) {
				b := make([]byte, 2048)
				for {
					n, _, err := s.ReadFromUDP(b)
					if err != nil {
						return
					}
					if p.Delay > 0 {
						time.Sleep(p.Delay)
					}
					_, _ = conn.WriteToUDP(b[:n], client)
				}
			}(s, from)
		}
		f.drop++
		drop := f.drop <= p.DropFirst
		sock := f.sock
		mu.Unlock()
		if drop {
			continue
		}
		go func(payload []byte) {
			if p.Delay > 0 {
				time.Sleep(p.Delay)
			}
			_, _ = sock.WriteToUDP(payload, target)
		}(payload)
	}
}

// Silence unused in case a caller only needs one direction.
var _ = io.Copy
