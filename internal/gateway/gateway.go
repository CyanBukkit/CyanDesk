package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"cyandesk/internal/protocol"
)

var ErrDenied = errors.New("gateway: ticket rejected")

type Claims struct {
	SessionID       string
	HostID          string
	ControllerID    string
	Scopes          []string
	BytesLimit      int64
	HostNoise       string
	ControllerNoise string
	Nonce           string
}

type Verifier interface {
	Redeem(ctx context.Context, ticket string) (Claims, error)
	SessionOpen(ctx context.Context, sessionID string) (bool, error)
}

type Server struct {
	Verify       Verifier
	ForwardDelay time.Duration
	RecheckEvery time.Duration
	mu           sync.Mutex
	pairs        map[string]*pair
	waiters      map[string][]chan joinResult
}

type joinResult struct {
	p   *pair
	err error
}

type pair struct {
	claims    Claims
	ticket    string
	host      *link
	ctrl      *link
	bytes     int64
	readySent bool
	cancel    context.CancelFunc
}

type link struct {
	conn net.Conn
	mu   sync.Mutex
}

func New(v Verifier) *Server {
	return &Server{Verify: v, RecheckEvery: 2 * time.Second, pairs: map[string]*pair{}, waiters: map[string][]chan joinResult{}}
}

func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	fr, err := protocol.ReadFrame(conn)
	if err != nil || fr.Type != protocol.TypeHello {
		conn.Close()
		return
	}
	role, ticket, err := protocol.DecodeHello(fr.Payload)
	if err != nil || (role != protocol.RoleHost && role != protocol.RoleController) || ticket == "" {
		_ = protocol.WriteFrame(conn, protocol.Frame{Type: protocol.TypeHelloAck, Payload: protocol.EncodeAck(false, 400, "bad hello")})
		conn.Close()
		return
	}
	p, err := s.join(ticket)
	if err != nil {
		_ = protocol.WriteFrame(conn, protocol.Frame{Type: protocol.TypeHelloAck, Payload: protocol.EncodeAck(false, 403, "denied")})
		conn.Close()
		return
	}
	lk := &link{conn: conn}
	if !s.reserve(p, role, lk) {
		_ = protocol.WriteFrame(conn, protocol.Frame{Type: protocol.TypeHelloAck, Payload: protocol.EncodeAck(false, 409, "duplicate role")})
		conn.Close()
		return
	}
	if err := protocol.WriteFrame(conn, protocol.Frame{Type: protocol.TypeHelloAck, Payload: protocol.EncodeAck(true, 0, "")}); err != nil {
		conn.Close()
		s.detach(p, role)
		return
	}
	_ = conn.SetDeadline(time.Time{})
	go s.readLoop(p, role, lk)
	s.maybeReady(p)
}

func (s *Server) join(ticket string) (*pair, error) {
	s.mu.Lock()
	for _, existing := range s.pairs {
		if existing.ticket == ticket {
			s.mu.Unlock()
			return existing, nil
		}
	}
	if _, busy := s.waiters[ticket]; busy {
		ch := make(chan joinResult, 1)
		s.waiters[ticket] = append(s.waiters[ticket], ch)
		s.mu.Unlock()
		res := <-ch
		return res.p, res.err
	}
	s.waiters[ticket] = []chan joinResult{}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	claims, err := s.Verify.Redeem(ctx, ticket)
	cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	waiters := s.waiters[ticket]
	delete(s.waiters, ticket)
	if err != nil {
		for _, ch := range waiters {
			ch <- joinResult{err: ErrDenied}
		}
		return nil, ErrDenied
	}
	if existing := s.pairs[claims.SessionID]; existing != nil {
		for _, ch := range waiters {
			ch <- joinResult{p: existing}
		}
		return existing, nil
	}
	cctx, stop := context.WithCancel(context.Background())
	p := &pair{claims: claims, ticket: ticket, cancel: stop}
	s.pairs[claims.SessionID] = p
	go s.recheck(cctx, p)
	for _, ch := range waiters {
		ch <- joinResult{p: p}
	}
	return p, nil
}

func (s *Server) reserve(p *pair, role byte, lk *link) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role == protocol.RoleHost {
		if p.host != nil {
			return false
		}
		p.host = lk
		return true
	}
	if p.ctrl != nil {
		return false
	}
	p.ctrl = lk
	return true
}

func (s *Server) maybeReady(p *pair) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.host == nil || p.ctrl == nil || p.readySent {
		return
	}
	p.readySent = true
	fr := protocol.Frame{Type: protocol.TypePeerReady}
	for _, lk := range []*link{p.host, p.ctrl} {
		lk.mu.Lock()
		_ = protocol.WriteFrame(lk.conn, fr)
		lk.mu.Unlock()
	}
}

func (s *Server) readLoop(p *pair, role byte, lk *link) {
	defer lk.conn.Close()
	defer s.detach(p, role)
	for {
		fr, err := protocol.ReadFrame(lk.conn)
		if err != nil {
			return
		}
		switch fr.Type {
		case protocol.TypeRelay, protocol.TypeSignal:
			if s.ForwardDelay > 0 {
				time.Sleep(s.ForwardDelay)
			}
			if !s.account(p, len(fr.Payload)) {
				s.drop(p, "quota")
				return
			}
			other := s.other(p, role)
			if other == nil {
				continue
			}
			other.mu.Lock()
			err = protocol.WriteFrame(other.conn, fr)
			other.mu.Unlock()
			if err != nil {
				return
			}
		case protocol.TypeClose:
			s.drop(p, "close")
			return
		default:
			return
		}
	}
}

func (s *Server) other(p *pair, role byte) *link {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role == protocol.RoleHost {
		return p.ctrl
	}
	return p.host
}

func (s *Server) account(p *pair, n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.bytes += int64(n)
	return p.claims.BytesLimit <= 0 || p.bytes <= p.claims.BytesLimit
}

func (s *Server) detach(p *pair, role byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role == protocol.RoleHost {
		p.host = nil
	} else {
		p.ctrl = nil
	}
	if p.host == nil && p.ctrl == nil {
		delete(s.pairs, p.claims.SessionID)
		if p.cancel != nil {
			p.cancel()
		}
	}
}

func (s *Server) drop(p *pair, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fr := protocol.Frame{Type: protocol.TypeClose, Payload: protocol.EncodeAck(false, 1, reason)}
	for _, lk := range []*link{p.host, p.ctrl} {
		if lk == nil {
			continue
		}
		lk.mu.Lock()
		_ = protocol.WriteFrame(lk.conn, fr)
		_ = lk.conn.Close()
		lk.mu.Unlock()
	}
	p.host, p.ctrl = nil, nil
	delete(s.pairs, p.claims.SessionID)
	if p.cancel != nil {
		p.cancel()
	}
}

func (s *Server) recheck(ctx context.Context, p *pair) {
	every := s.RecheckEvery
	if every <= 0 {
		every = 2 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ok, err := s.Verify.SessionOpen(context.Background(), p.claims.SessionID)
			if err != nil || !ok {
				s.drop(p, "session closed")
				return
			}
		}
	}
}

// Client is one authenticated gateway connection.
type Client struct {
	conn   net.Conn
	relay  chan []byte
	signal chan []byte
	ready  chan struct{}
	errc   chan error
	wmu    sync.Mutex
}

func Dial(addr string, role byte, ticket string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:   conn,
		relay:  make(chan []byte, 32),
		signal: make(chan []byte, 8),
		ready:  make(chan struct{}, 1),
		errc:   make(chan error, 1),
	}
	if err := protocol.WriteFrame(conn, protocol.Frame{Type: protocol.TypeHello, Payload: protocol.EncodeHello(role, ticket)}); err != nil {
		conn.Close()
		return nil, err
	}
	fr, err := protocol.ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if fr.Type != protocol.TypeHelloAck {
		conn.Close()
		return nil, errors.New("gateway: expected hello ack")
	}
	ok, _, _, err := protocol.DecodeAck(fr.Payload)
	if err != nil || !ok {
		conn.Close()
		return nil, ErrDenied
	}
	go c.read()
	return c, nil
}

func (c *Client) WaitReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.errc:
		return err
	case <-c.ready:
		return nil
	}
}

func (c *Client) read() {
	for {
		fr, err := protocol.ReadFrame(c.conn)
		if err != nil {
			c.fail(err)
			return
		}
		switch fr.Type {
		case protocol.TypePeerReady:
			select {
			case c.ready <- struct{}{}:
			default:
			}
		case protocol.TypeRelay:
			c.relay <- append([]byte(nil), fr.Payload...)
		case protocol.TypeSignal:
			c.signal <- append([]byte(nil), fr.Payload...)
		case protocol.TypeClose:
			c.fail(io.EOF)
			return
		default:
			c.fail(errors.New("gateway: unexpected frame"))
			return
		}
	}
}

func (c *Client) fail(err error) {
	select {
	case c.errc <- err:
	default:
	}
}

func (c *Client) WriteMsg(p []byte) error { return c.write(protocol.TypeRelay, p) }
func (c *Client) ReadMsg() ([]byte, error) {
	select {
	case p := <-c.relay:
		return p, nil
	case err := <-c.errc:
		return nil, err
	}
}
func (c *Client) WriteSignal(p []byte) error { return c.write(protocol.TypeSignal, p) }
func (c *Client) ReadSignal(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p := <-c.signal:
		return p, nil
	case err := <-c.errc:
		return nil, err
	}
}
func (c *Client) write(typ byte, p []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return protocol.WriteFrame(c.conn, protocol.Frame{Type: typ, Payload: p})
}
func (c *Client) Close() error { return c.conn.Close() }

// ReadAck remains for tests that speak frames directly.
func ReadAck(r io.Reader) (bool, error) {
	fr, err := protocol.ReadFrame(r)
	if err != nil {
		return false, err
	}
	if fr.Type != protocol.TypeHelloAck {
		return false, errors.New("gateway: expected hello ack")
	}
	ok, _, _, err := protocol.DecodeAck(fr.Payload)
	return ok, err
}
