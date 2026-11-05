//go:build windows

package childproc

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // start without a console window

// configure hides the helper's window and gives it its own process group,
// so a Ctrl+C in houridle's console doesn't reach it.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
