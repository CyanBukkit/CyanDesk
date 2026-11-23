//go:build !windows

package main

import (
	"fmt"
	"os"
)

func runGUI(args []string) int {
	fmt.Fprintln(os.Stderr, "图形登录窗口在 Windows 构建里。这个系统请用 client、agent 和 connect 子命令。")
	return 2
}
