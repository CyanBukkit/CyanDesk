//go:build !windows

package winsvc

import (
	"context"
	"errors"
)

func Run(name string, run func(context.Context) error) error {
	return errors.New("winsvc: the service adapter is only built for Windows and has not been installed")
}
