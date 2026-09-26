package util

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// CopyToClipboard uses the clipboard command for the current desktop session.
func CopyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = exec.Command("pbcopy")
	case runtime.GOOS == "linux" && os.Getenv("WAYLAND_DISPLAY") != "":
		cmd = exec.Command("wl-copy")
	case runtime.GOOS == "linux" && os.Getenv("DISPLAY") != "":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	default:
		return fmt.Errorf("clipboard unavailable: no supported desktop session")
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard via %s: %w", cmd.Args[0], err)
	}
	return nil
}
