// Package childproc starts houridle's helper processes: hidden, and kept out
// of the terminal's Ctrl+C so houridle can stop them cleanly itself.
package childproc

import (
	"os/exec"
	"time"
)

const waitDelay = 2 * time.Second

// Command is exec.Command for a background helper process.
func Command(exePath string, args ...string) *exec.Cmd {
	cmd := exec.Command(exePath, args...)
	cmd.WaitDelay = waitDelay
	configure(cmd)
	return cmd
}
