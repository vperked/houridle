// Package childproc starts houridle's helper processes: hidden, and kept out
// of the terminal's Ctrl+C so houridle can stop them cleanly itself.
package childproc

import "os/exec"

// Command is exec.Command for a background helper process.
func Command(exePath string, args ...string) *exec.Cmd {
	cmd := exec.Command(exePath, args...)
	configure(cmd)
	return cmd
}
