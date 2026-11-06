//go:build windows

package ui

import (
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var exitSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

// enableColors turns on ANSI escape handling in the Windows console, and
// reports whether it worked (it doesn't when output is redirected).
func enableColors() bool {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return false
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

func stdinIsTerminal() bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil
}

// termSize returns the visible console size in cells.
func termSize() (width, height int) {
	var info windows.ConsoleScreenBufferInfo
	if windows.GetConsoleScreenBufferInfo(windows.Handle(os.Stdout.Fd()), &info) != nil {
		return 80, 24
	}
	return int(info.Window.Right-info.Window.Left) + 1, int(info.Window.Bottom-info.Window.Top) + 1
}

// enableRawInput switches the console to one-key-at-a-time input, with
// escape sequences for arrow keys. It returns a func that restores it.
func enableRawInput() (restore func(), err error) {
	h := windows.Handle(os.Stdin.Fd())
	var old uint32
	if err := windows.GetConsoleMode(h, &old); err != nil {
		return nil, err
	}
	raw := old&^(windows.ENABLE_ECHO_INPUT|windows.ENABLE_LINE_INPUT|windows.ENABLE_PROCESSED_INPUT) | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(h, raw); err != nil {
		return nil, err
	}
	return func() { windows.SetConsoleMode(h, old) }, nil
}

func inputPending(time.Duration) bool { return false }

// launchedByDoubleClick is true when houridle owns its console window (opened
// from Explorer), so the window would vanish the moment we exit.
func launchedByDoubleClick() bool {
	var pids [4]uint32
	n, _, _ := kernel32.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// HasConsoleWindow reports whether this process has a console at all (the
// tray process runs detached, without one).
func HasConsoleWindow() bool {
	hwnd, _, _ := kernel32.NewProc("GetConsoleWindow").Call()
	return hwnd != 0
}

func showErrorBox(msg string) {
	text, _ := windows.UTF16PtrFromString(msg)
	title, _ := windows.UTF16PtrFromString("houridle")
	windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
}
