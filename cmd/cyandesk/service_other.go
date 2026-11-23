//go:build !windows

package main

import (
	"fmt"
	"os"
)

func runService(args []string) int {
	fmt.Fprintln(os.Stderr, "service 只在 Windows 构建里存在，而且尚未在 Windows 上安装。")
	return 2
}
