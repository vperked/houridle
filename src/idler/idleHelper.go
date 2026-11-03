package idler

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"houridle/src/steamworks"
)

// HelperFlag is the command-line flag that makes houridle run as an idle helper.
const HelperFlag = "child"

// The helper prints one status line: "READY", or "FAIL <code> <message>".
const (
	helperReady      = "READY"
	helperFailPrefix = "FAIL "
)

// Failure codes added by the supervisor (the helper uses steamworks codes).
const (
	failTimeout = "timeout"
	failNoReply = "noReply"
	failStart   = "start"
)

// RunHelper is the helper process for one game. It tells the running Steam
// client that this process is the game, then waits until the parent closes
// our stdin (which also happens if the parent dies). Returns the exit code.
func RunHelper(appID uint32, dllPath string) int {
	api, err := steamworks.Init(dllPath, appID)
	if err != nil {
		code := steamworks.CodeRefused
		var swErr *steamworks.Error
		if errors.As(err, &swErr) {
			code = swErr.Code
		}
		fmt.Println(helperFailPrefix + code + " " + err.Error())
		return 1
	}
	defer api.Shutdown()
	fmt.Println(helperReady)

	stdinClosed := make(chan struct{})
	go func() {
		io.Copy(io.Discard, os.Stdin)
		close(stdinClosed)
	}()
	stopSignal := make(chan os.Signal, 1)
	signal.Notify(stopSignal, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stdinClosed:
			return 0
		case <-stopSignal:
			return 0
		case <-ticker.C:
			api.RunCallbacks()
		}
	}
}
