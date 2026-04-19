//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func terminalWidth() int {
	if col := os.Getenv("COLUMNS"); col != "" {
		if w, err := strconv.Atoi(col); err == nil && w > 0 {
			return w
		}
	}
	if os.Getenv("TMUX") != "" {
		out, err := exec.Command("tmux", "display-message", "-p", "#{pane_width}").Output()
		if err == nil {
			if w, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && w > 0 {
				return w
			}
		}
	}
	return 80
}
