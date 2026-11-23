//go:build windows && !cyandesk_win7

package hostcap

import (
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procShowWindow        = user32.NewProc("ShowWindow")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procBeginPaint        = user32.NewProc("BeginPaint")
	procEndPaint          = user32.NewProc("EndPaint")
	procInvalidateRect    = user32.NewProc("InvalidateRect")
	procSetDIBitsToDevice = gdi32.NewProc("SetDIBitsToDevice")
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmLButtonDown = 0x0201
	csHRedraw     = 0x0002
	csVRedraw     = 0x0001
	wsOverlapped  = 0x00CF0000
	swShow        = 5
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	RcLeft    int32
	RcTop     int32
	RcRight   int32
	RcBottom  int32
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

// Viewer is a Win32 window that shows the latest RGB frame.
// It is the Windows controller surface. It has not been opened on Windows here.
type Viewer struct {
	hwnd   uintptr
	w, h   int
	mu     sync.Mutex
	frame  []byte
	clicks chan [2]int32
	ready  chan error
}

func OpenViewer(w, h int) (*Viewer, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("hostcap: invalid viewer size")
	}
	v := &Viewer{w: w, h: h, clicks: make(chan [2]int32, 8), ready: make(chan error, 1)}
	go v.loop()
	if err := <-v.ready; err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Viewer) Clicks() <-chan [2]int32 { return v.clicks }

func (v *Viewer) Present(rgb []byte) {
	v.mu.Lock()
	v.frame = append([]byte(nil), rgb...)
	hwnd := v.hwnd
	v.mu.Unlock()
	if hwnd != 0 {
		procInvalidateRect.Call(hwnd, 0, 1)
	}
}

func (v *Viewer) Close() {
	if v.hwnd != 0 {
		procPostQuitMessage.Call(0)
	}
}

func (v *Viewer) loop() {
	instance, _, _ := procGetModuleHandleW.Call(0)
	name, _ := windows.UTF16PtrFromString("CyanDeskView")
	cls := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		Style:     csHRedraw | csVRedraw,
		WndProc:   windows.NewCallback(v.wndProc),
		Instance:  instance,
		ClassName: name,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&cls))); r == 0 {
		v.ready <- err
		return
	}
	title, _ := windows.UTF16PtrFromString("CyanDesk")
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(title)), wsOverlapped, 40, 40, uintptr(v.w+16), uintptr(v.h+40), 0, 0, instance, 0)
	if hwnd == 0 {
		v.ready <- err
		return
	}
	v.hwnd = hwnd
	procShowWindow.Call(hwnd, swShow)
	v.ready <- nil
	var msg [48]byte
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}

func (v *Viewer) wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		v.paint(hdc)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmLButtonDown:
		x := int32(lparam & 0xffff)
		y := int32((lparam >> 16) & 0xffff)
		select {
		case v.clicks <- [2]int32{x, y}:
		default:
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (v *Viewer) paint(hdc uintptr) {
	v.mu.Lock()
	frame := v.frame
	w, h := v.w, v.h
	v.mu.Unlock()
	if hdc == 0 || len(frame) < w*h*3 {
		return
	}
	stride := ((w*3 + 3) / 4) * 4
	raw := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := (y*w + x) * 3
			d := y*stride + x*3
			raw[d] = frame[s+2]
			raw[d+1] = frame[s+1]
			raw[d+2] = frame[s]
		}
	}
	hdr := bitmapInfoHeader{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 24}
	procSetDIBitsToDevice.Call(hdc, 0, 0, uintptr(w), uintptr(h), 0, 0, 0, uintptr(h), uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&hdr)), 0)
}
