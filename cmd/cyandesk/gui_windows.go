//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"cyandesk/internal/clientcore"
	"golang.org/x/sys/windows"
)

var (
	guiUser             = windows.NewLazySystemDLL("user32.dll")
	guiCreateWindowEx   = guiUser.NewProc("CreateWindowExW")
	guiDefWindowProc    = guiUser.NewProc("DefWindowProcW")
	guiDispatchMessage  = guiUser.NewProc("DispatchMessageW")
	guiGetMessage       = guiUser.NewProc("GetMessageW")
	guiGetWindowText    = guiUser.NewProc("GetWindowTextW")
	guiPostQuitMessage  = guiUser.NewProc("PostQuitMessage")
	guiRegisterClassEx  = guiUser.NewProc("RegisterClassExW")
	guiSendMessage      = guiUser.NewProc("SendMessageW")
	guiShowWindow       = guiUser.NewProc("ShowWindow")
	guiTranslateMessage = guiUser.NewProc("TranslateMessage")
	guiSetWindowText    = guiUser.NewProc("SetWindowTextW")
	guiKernel           = windows.NewLazySystemDLL("kernel32.dll")
	guiGetModuleHandle  = guiKernel.NewProc("GetModuleHandleW")
)

const (
	wmDestroy   = 0x0002
	wmCommand   = 0x0111
	wsChild     = 0x40000000
	wsVisible   = 0x10000000
	wsBorder    = 0x00800000
	esPassword  = 0x0020
	lbsNotify   = 0x0001
	lbAddString = 0x0180
	lbGetCurSel = 0x0188
	lbGetText   = 0x0189
	lbReset     = 0x0184
	idLogin     = 1
	idView      = 2
	idControl   = 3
)

type guiState struct {
	hwnd                                 windows.HWND
	server, fp, user, pass, list, status windows.HWND
	profile                              clientcore.Profile
	config                               string
	devices                              [][2]string
}

func runGUI(args []string) int {
	config := filepath.Join(os.Getenv("APPDATA"), "CyanDesk")
	st := &guiState{config: config}
	if p, err := clientcore.Load(config); err == nil {
		st.profile = p
	}
	instance, _, _ := guiGetModuleHandle.Call(0)
	class, _ := windows.UTF16PtrFromString("CyanDeskLogin")
	cb := windows.NewCallback(st.wndProc)
	var wc struct {
		size, style            uint32
		proc                   uintptr
		cls, wnd               int32
		inst, icon, cursor, bg uintptr
		menu, name             *uint16
		small                  uintptr
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	wc.proc = cb
	wc.inst = instance
	wc.name = class
	if r, _, err := guiRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	title, _ := windows.UTF16PtrFromString("CyanDesk")
	hwnd, _, err := guiCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0x00CF0000, 80, 80, 560, 460, 0, 0, instance, 0)
	if hwnd == 0 {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	st.hwnd = windows.HWND(hwnd)
	st.server = edit(hwnd, "https://服务器:13111", 20, 20, 500, 24, 0)
	st.fp = edit(hwnd, "证书指纹", 20, 52, 500, 24, 0)
	st.user = edit(hwnd, "账号", 20, 84, 240, 24, 0)
	st.pass = edit(hwnd, "", 270, 84, 250, 24, esPassword)
	button(hwnd, "登录并刷新设备", 20, 118, 160, 28, idLogin)
	st.list = listbox(hwnd, 20, 160, 500, 180)
	button(hwnd, "观看", 20, 356, 100, 28, idView)
	button(hwnd, "控制", 140, 356, 100, 28, idControl)
	st.status = edit(hwnd, "先填写服务器地址和指纹。密码只留在这个窗口里。", 250, 356, 270, 28, 0)
	if st.profile.ServerURL != "" {
		setText(st.server, st.profile.ServerURL)
	}
	if st.profile.Fingerprint != "" {
		setText(st.fp, st.profile.Fingerprint)
	}
	if st.profile.LoginName != "" {
		setText(st.user, st.profile.LoginName)
	}
	guiShowWindow.Call(hwnd, 5)
	var msg [48]byte
	for {
		r, _, _ := guiGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			return 0
		}
		guiTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		guiDispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}

func (st *guiState) wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCommand:
		switch int(wparam & 0xffff) {
		case idLogin:
			st.login()
		case idView:
			go st.connect("view")
		case idControl:
			go st.connect("control")
		}
		return 0
	case wmDestroy:
		guiPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := guiDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}

func (st *guiState) login() {
	st.profile.ServerURL = strings.TrimSpace(textOf(st.server))
	st.profile.Fingerprint = strings.ToLower(strings.TrimSpace(textOf(st.fp)))
	user := strings.TrimSpace(textOf(st.user))
	pass := textOf(st.pass)
	if err := st.profile.Save(st.config); err != nil {
		setText(st.status, err.Error())
		return
	}
	if err := st.profile.Login(user, pass); err != nil {
		setText(st.status, "登录没有完成")
		return
	}
	if err := st.profile.Save(st.config); err != nil {
		setText(st.status, err.Error())
		return
	}
	devs, err := st.profile.Devices()
	if err != nil {
		setText(st.status, "设备列表没有读到")
		return
	}
	guiSendMessage.Call(uintptr(st.list), lbReset, 0, 0)
	st.devices = nil
	for _, d := range devs {
		id, _ := d["id"].(string)
		fp, _ := d["fingerprint"].(string)
		name, _ := d["name"].(string)
		st.devices = append(st.devices, [2]string{id, fp})
		line, _ := syscall.UTF16PtrFromString(name + "  " + id)
		guiSendMessage.Call(uintptr(st.list), lbAddString, 0, uintptr(unsafe.Pointer(line)))
	}
	setText(st.status, fmt.Sprintf("已登录，%d 台设备。选择一台后观看或控制。", len(devs)))
}

func (st *guiState) connect(mode string) {
	sel, _, _ := guiSendMessage.Call(uintptr(st.list), lbGetCurSel, 0, 0)
	if int32(sel) < 0 || int(sel) >= len(st.devices) {
		setText(st.status, "先选择一台设备")
		return
	}
	id := st.devices[sel][0]
	fp := st.devices[sel][1]
	setText(st.status, "正在连接。这个窗口没有在 Windows 上实测。")
	runConnect([]string{"--config", st.config, "--host", id, "--fingerprint", fp, "--mode", mode})
}

func edit(parent uintptr, initial string, x, y, w, h int, style uintptr) windows.HWND {
	cls, _ := windows.UTF16PtrFromString("EDIT")
	text, _ := windows.UTF16PtrFromString(initial)
	hwnd, _, _ := guiCreateWindowEx.Call(0x200, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(text)), wsChild|wsVisible|wsBorder|style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, 0, 0, 0)
	return windows.HWND(hwnd)
}

func button(parent uintptr, label string, x, y, w, h, id int) {
	cls, _ := windows.UTF16PtrFromString("BUTTON")
	text, _ := windows.UTF16PtrFromString(label)
	guiCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(text)), wsChild|wsVisible, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), 0, 0)
}

func listbox(parent uintptr, x, y, w, h int) windows.HWND {
	cls, _ := windows.UTF16PtrFromString("LISTBOX")
	hwnd, _, _ := guiCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, wsChild|wsVisible|wsBorder|lbsNotify, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, 0, 0, 0)
	return windows.HWND(hwnd)
}

func textOf(hwnd windows.HWND) string {
	buf := make([]uint16, 512)
	guiGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func setText(hwnd windows.HWND, s string) {
	p, _ := windows.UTF16PtrFromString(s)
	guiSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(p)))
}
