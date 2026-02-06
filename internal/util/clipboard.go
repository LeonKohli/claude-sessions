package util

import (
	"os/exec"
	"strings"
)

// CopyToClipboard copies text to the system clipboard using pbcopy (macOS).
func CopyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
