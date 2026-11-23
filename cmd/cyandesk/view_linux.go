//go:build linux

package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"cyandesk/internal/capture"
)

type viewer struct {
	in     io.WriteCloser
	clicks <-chan string
	close  func()
}

func viewerStartError() string {
	return "找不到 cyandesk-view。请先编译 packaging/linux/cyandesk-view.c"
}

func startViewer(w, h int) (*viewer, error) {
	cmd := exec.Command("cyandesk-view", strconv.Itoa(w), strconv.Itoa(h))
	cmd.Env = capture.WithDisplay(os.Getenv("DISPLAY"))
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ch := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	return &viewer{in: in, clicks: ch, close: func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}}, nil
}

func (v *viewer) present(rgb []byte) {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(rgb)))
	_, _ = v.in.Write(hdr[:])
	_, _ = v.in.Write(rgb)
}

func (v *viewer) nextClick() (x, y int, ok bool) {
	line, open := <-v.clicks
	if !open {
		return 0, 0, false
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "click" {
		return 0, 0, false
	}
	x, _ = strconv.Atoi(fields[1])
	y, _ = strconv.Atoi(fields[2])
	return x, y, true
}
