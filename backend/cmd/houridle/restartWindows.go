//go:build windows

package main

import (
	"os"
	"os/exec"
)

// restartSelf starts houridle again as the program now at exe; this one
// then exits. (Windows can't replace a running process.)
func restartSelf(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Start()
}
