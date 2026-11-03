//go:build linux

package childproc

import (
	"os/exec"
	"syscall"
)

// configure puts the helper in its own process group, so a Ctrl+C in the
// terminal reaches only houridle, which then stops its helpers cleanly.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
