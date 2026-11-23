//go:build windows

package winsvc

import (
	"context"

	"golang.org/x/sys/windows/svc"
)

// Handler runs the desktop agent when the Windows service control manager
// starts the process. This file compiles on a Windows target. It has not been
// installed or started on Windows.
type Handler struct {
	Run func(context.Context) error
}

func (h Handler) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	done := make(chan struct{})
	go func() {
		_ = h.Run(ctx)
		close(done)
	}()
	for {
		select {
		case req := <-requests:
			switch req.Cmd {
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		case <-done:
			return false, 0
		}
	}
}

func Run(name string, run func(context.Context) error) error {
	if run == nil {
		run = func(context.Context) error { return nil }
	}
	return svc.Run(name, Handler{Run: run})
}
