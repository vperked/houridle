//go:build linux

package main

import (
	"os"
	"syscall"
)

// restartSelf runs houridle again in this same process (same PID, so a
// service manager or screen session carries on), as the program now at exe.
func restartSelf(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}
