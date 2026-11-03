//go:build linux

package autostart

// Enable isn't available on Linux; use the systemd service in the README.
func Enable(string, string) error { return ErrUnsupported }

// Disable isn't available on Linux; use the systemd service in the README.
func Disable() error { return ErrUnsupported }

// Supported reports whether houridle can set up autostart on this OS.
const Supported = false
