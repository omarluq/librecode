//go:build windows

package terminal

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func writeSystemClipboard(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "clip.exe")
	cmd.Stdin = strings.NewReader(text)

	return cmd.Run()
}
