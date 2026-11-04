//go:build !windows && !linux

package main

import (
	"fmt"
	"os"
)

// houridle needs the Steam client and its API library, which it supports on
// Windows and Linux.
func main() {
	fmt.Fprintln(os.Stderr, "houridle runs on Windows and Linux.")
	os.Exit(1)
}
