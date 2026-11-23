package desktop

import (
	"context"
	"errors"
	"time"

	"cyandesk/internal/input"
	"cyandesk/internal/media"
	"cyandesk/internal/protocol"
)

var ErrRevoked = errors.New("desktop: session is no longer authorized")

// Link is the encrypted path selected for this session.
type Link interface {
	Send(app byte, payload []byte) error
	Recv() (byte, []byte, error)
	Close()
}

// HostConfig is the controlled machine. AllowInput at start must come from the
// granted scopes, not from the controller. AllowUpgrade stays false unless the
// host operator explicitly permits returning to control after a downgrade.
type HostConfig struct {
	Gate           *input.Gate
	GrantedInput   bool
	AllowUpgrade   bool
	Grab           func() ([]byte, int, int, error)
	Inject         input.Sink
	Alive          func() bool
	Interval       time.Duration
	Width          int
	Height         int
	Queue          *VideoQueue
	FramesSent     *int
	InputsRejected *int
	Encode         func(rgb []byte, w, h int) ([]byte, error)
}

// ServeHost captures, encodes, and sends until the context ends, the path
// fails, or Alive reports the session was revoked or closed.
func ServeHost(ctx context.Context, link Link, cfg HostConfig) error {
	if cfg.Gate == nil {
		return errors.New("desktop: missing input gate")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 200 * time.Millisecond
	}
	if cfg.Queue == nil {
		cfg.Queue = NewVideoQueue(2)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	go func() { errc <- hostRead(ctx, link, cfg) }()
	go func() { errc <- hostSend(ctx, link, cfg) }()
	err := <-errc
	link.Close()
	cancel()
	return err
}

func hostRead(ctx context.Context, link Link, cfg HostConfig) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		kind, payload, err := link.Recv()
		if err != nil {
			return err
		}
		switch kind {
		case protocol.AppInput:
			ev, err := DecodeInput(payload)
			if err != nil {
				continue
			}
			if err := cfg.Gate.Apply(ev, cfg.Inject); err != nil {
				if cfg.InputsRejected != nil {
					*cfg.InputsRejected++
				}
				_ = link.Send(protocol.AppReject, []byte("view-only"))
			}
		case protocol.AppPolicy:
			if len(payload) == 0 {
				continue
			}
			switch payload[0] {
			case PolicyView:
				cfg.Gate.AllowInput = false
				_ = link.Send(protocol.AppInputAck, []byte{PolicyView})
			case PolicyControl:
				if cfg.GrantedInput && cfg.AllowUpgrade {
					cfg.Gate.AllowInput = true
					_ = link.Send(protocol.AppInputAck, []byte{PolicyControl})
				} else {
					if cfg.InputsRejected != nil {
						*cfg.InputsRejected++
					}
					_ = link.Send(protocol.AppReject, []byte("upgrade-denied"))
				}
			}
		case protocol.AppReleaseAll:
			continue
		}
	}
}

func hostSend(ctx context.Context, link Link, cfg HostConfig) error {
	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	var seq uint32 = 1
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if cfg.Alive != nil && !cfg.Alive() {
				return ErrRevoked
			}
			rgb, w, h, err := cfg.Grab()
			if cfg.Width > 0 {
				w = cfg.Width
			}
			if cfg.Height > 0 {
				h = cfg.Height
			}
			if err != nil || w == 0 || h == 0 || len(rgb) < w*h*3 {
				continue
			}
			rgb = rgb[:w*h*3]
			encode := cfg.Encode
			if encode == nil {
				encode = media.EncodeH264
			}
			annex, err := encode(rgb, w, h)
			if err != nil {
				continue
			}
			cfg.Queue.Push(EncodeVideo(seq, w, h, annex))
			seq++
			for {
				frame, ok := cfg.Queue.Pop()
				if !ok {
					break
				}
				if err := link.Send(protocol.AppVideo, frame); err != nil {
					return err
				}
				if cfg.FramesSent != nil {
					*cfg.FramesSent++
				}
			}
		}
	}
}

// DecodeDeliveredFrame turns one received application message into RGB when it is video.
func DecodeDeliveredFrame(kind byte, payload []byte) (seq uint32, rgb []byte, w, h int, ok bool, err error) {
	if kind != protocol.AppVideo {
		return 0, nil, 0, 0, false, nil
	}
	seq, w, h, annex, err := DecodeVideo(payload)
	if err != nil {
		return 0, nil, 0, 0, false, err
	}
	if len(annex) >= 2 && annex[0] == 0xff && annex[1] == 0xd8 {
		rgb, err = media.DecodeJPEG(annex, w, h)
	} else {
		rgb, err = media.DecodeH264(annex, w, h)
	}
	if err != nil {
		return 0, nil, 0, 0, false, err
	}
	return seq, rgb, w, h, true, nil
}
