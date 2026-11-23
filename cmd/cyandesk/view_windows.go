//go:build windows

package main

import "cyandesk/internal/hostcap"

type viewer struct {
	win   *hostcap.Viewer
	close func()
}

func viewerStartError() string {
	return "无法创建 Windows 画面窗口。"
}

func startViewer(w, h int) (*viewer, error) {
	win, err := hostcap.OpenViewer(w, h)
	if err != nil {
		return nil, err
	}
	return &viewer{win: win, close: win.Close}, nil
}

func (v *viewer) present(rgb []byte) { v.win.Present(rgb) }

func (v *viewer) nextClick() (x, y int, ok bool) {
	c, open := <-v.win.Clicks()
	if !open {
		return 0, 0, false
	}
	return int(c[0]), int(c[1]), true
}
