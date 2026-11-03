//go:build !windows

package main

import (
	"fmt"
	"os"
)

// houridle drives the Windows Steam client through steam_api64.dll.
func main() {
	fmt.Fprintln(os.Stderr, "houridle only runs on Windows.")
	os.Exit(1)
}
