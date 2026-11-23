//go:build windows && !cyandesk_win7

package hostcap

import (
	"encoding/binary"
	"errors"
	"unsafe"

	"cyandesk/internal/input"
	"cyandesk/internal/media"
	"golang.org/x/sys/windows"
)

// Name is the Windows 10/11 GDI backend. Compiling this file is not a test on
// Windows, and it is not a Windows 7 build. The cyandesk_win7 tag is a
// separate route that is not part of this binary.
const Name = "windows-gdi"

func RequireSession() error { return nil }

func Encode(rgb []byte, w, h int) ([]byte, error) {
	return media.EncodeJPEG(rgb, w, h)
}

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	gdi32                      = windows.NewLazySystemDLL("gdi32.dll")
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procSendInput              = user32.NewProc("SendInput")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

const (
	smCXScreen = 0
	smCYScreen = 1
	srcCopy    = 0x00CC0020
	biRGB      = 0
	dibRGB     = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// Grab copies the primary screen with GDI BitBlt and GetDIBits.
// display is ignored; Windows does not use an X11 display name.
func Grab(display string, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("hostcap: invalid frame size")
	}
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, errors.New("hostcap: GetDC failed")
	}
	defer procReleaseDC.Call(0, hdc)
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	if mem == 0 {
		return nil, errors.New("hostcap: CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, errors.New("hostcap: CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer procSelectObject.Call(mem, old)
	if r, _, err := procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), hdc, 0, 0, srcCopy); r == 0 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return nil, err
		}
		return nil, errors.New("hostcap: BitBlt failed")
	}
	hdr := bitmapInfoHeader{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 24, Compression: biRGB}
	stride := ((w*3 + 3) / 4) * 4
	raw := make([]byte, stride*h)
	if r, _, err := procGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&hdr)), dibRGB); r == 0 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return nil, err
		}
		return nil, errors.New("hostcap: GetDIBits failed")
	}
	rgb := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := y*stride + x*3
			d := (y*w + x) * 3
			rgb[d] = raw[s+2]
			rgb[d+1] = raw[s+1]
			rgb[d+2] = raw[s]
		}
	}
	return rgb, nil
}

// Sink sends pointer and key events with SendInput. It has not been run on a
// Windows machine in this environment.
type Sink struct{ Display string }

func (s Sink) Inject(ev input.Event) error {
	switch ev.Kind {
	case 2:
		sw, _, _ := procGetSystemMetrics.Call(smCXScreen)
		sh, _, _ := procGetSystemMetrics.Call(smCYScreen)
		if sw == 0 || sh == 0 {
			return errors.New("hostcap: no primary screen")
		}
		dx := int32(int64(ev.X) * 65535 / int64(sw))
		dy := int32(int64(ev.Y) * 65535 / int64(sh))
		if err := sendMouse(dx, dy, 0x8000|0x0001|0x0002); err != nil {
			return err
		}
		return sendMouse(dx, dy, 0x8000|0x0001|0x0004)
	case 4, 5:
		flags := uint32(0)
		if ev.Kind == 5 {
			flags = 0x0002
		}
		return sendKey(uint16(ev.Key), flags)
	default:
		return nil
	}
}

func sendMouse(dx, dy int32, flags uint32) error {
	var b [40]byte
	binary.LittleEndian.PutUint32(b[8:12], uint32(dx))
	binary.LittleEndian.PutUint32(b[12:16], uint32(dy))
	binary.LittleEndian.PutUint32(b[20:24], flags)
	return callSendInput(&b)
}

func sendKey(vk uint16, flags uint32) error {
	var b [40]byte
	binary.LittleEndian.PutUint32(b[0:4], 1)
	binary.LittleEndian.PutUint16(b[8:10], vk)
	binary.LittleEndian.PutUint32(b[12:16], flags)
	return callSendInput(&b)
}

func callSendInput(b *[40]byte) error {
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(b)), uintptr(len(b)))
	if r != 1 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return err
		}
		return errors.New("hostcap: SendInput failed")
	}
	return nil
}
