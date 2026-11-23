//go:build windows

package main

import (
	"context"
	"fmt"
	"os"

	"cyandesk/internal/winsvc"
)

func runService(args []string) int {
	err := winsvc.Run("CyanDesk", func(ctx context.Context) error {
		runAgentContext(ctx, args)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Windows 服务入口没有启动。这个适配器还没有在 Windows 上安装或运行。")
		return 1
	}
	return 0
}
