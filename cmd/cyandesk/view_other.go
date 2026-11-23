//go:build !linux && !windows

package main

import "errors"

type viewer struct{ close func() }

func viewerStartError() string {
	return "这个系统还没有画面窗口。"
}

func startViewer(w, h int) (*viewer, error) {
	return nil, errors.New("这个系统还没有画面窗口")
}

func (v *viewer) present(rgb []byte) {}

func (v *viewer) nextClick() (int, int, bool) { return 0, 0, false }
