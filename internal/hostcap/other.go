//go:build !linux && !windows

package hostcap

import (
	"errors"

	"cyandesk/internal/input"
)

const Name = "unimplemented"

func RequireSession() error {
	return errors.New("hostcap: this OS has no capture implementation")
}

func Grab(display string, w, h int) ([]byte, error) {
	return nil, errors.New("hostcap: this OS has no capture implementation")
}

func Encode(rgb []byte, w, h int) ([]byte, error) {
	return nil, errors.New("hostcap: this OS has no encoder")
}

type Sink struct{ Display string }

func (s Sink) Inject(input.Event) error {
	return errors.New("hostcap: this OS has no input implementation")
}
