//go:build windows && cyandesk_win7

package hostcap

import "errors"

// Win7Capture is a separate build tag from the Windows 10/11 GDI file.
// Nothing in this file has been run on Windows 7.
func Win7Capture() error {
	return errors.New("hostcap: Windows 7 is a separate route and has not been tested")
}
