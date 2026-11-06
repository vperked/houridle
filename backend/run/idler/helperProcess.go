package idler

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"houridle/backend/client/childproc"
)

const (
	readyTimeout     = 30 * time.Second // a helper must report in within this
	stopGracePeriod  = 5 * time.Second  // before a helper is killed on shutdown
	maxHelperLogSize = 64 << 10         // cap on buffered helper output
)

// helperProcess is one running idle helper ("houridle -child <appid>").
type helperProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser // closing it tells the helper to exit
	ready  *readyWatcher
	exited chan struct{} // closed once the process has exited
}

// helperFailure is a helper that reported (or showed) it couldn't start.
type helperFailure struct {
	code string
	msg  string
}

func (f *helperFailure) Error() string { return f.msg }

// startHelper launches a hidden helper for appID.
func startHelper(exePath, dllPath string, appID uint32) (*helperProcess, error) {
	h := &helperProcess{
		cmd:    childproc.Command(exePath, "-"+HelperFlag, strconv.FormatUint(uint64(appID), 10), "-dll", dllPath),
		ready:  &readyWatcher{result: make(chan string, 1)},
		exited: make(chan struct{}),
	}
	h.cmd.Stdout = h.ready

	var err error
	if h.stdin, err = h.cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if err := h.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		h.cmd.Wait()
		close(h.exited)
	}()
	return h, nil
}

// waitReady waits for the helper to say it started. It returns nil when the
// game is running, a *helperFailure if it couldn't start, or ctx's error.
func (h *helperProcess) waitReady(ctx context.Context) error {
	timeout := time.NewTimer(readyTimeout)
	defer timeout.Stop()
	select {
	case line := <-h.ready.result:
		return parseStatus(line)
	case <-h.exited:
		select {
		case line := <-h.ready.result:
			return parseStatus(line)
		default:
			return &helperFailure{code: failNoReply, msg: "helper exited without saying why"}
		}
	case <-timeout.C:
		return &helperFailure{code: failTimeout, msg: "timed out talking to Steam"}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseStatus(line string) error {
	if line == helperReady {
		return nil
	}
	code, msg, _ := strings.Cut(strings.TrimPrefix(line, helperFailPrefix), " ")
	return &helperFailure{code: code, msg: msg}
}

// stop asks the helper to exit cleanly, and kills it if it doesn't in time.
func (h *helperProcess) stop() {
	h.stdin.Close()
	select {
	case <-h.exited:
	case <-time.After(stopGracePeriod):
		h.kill()
	}
}

// kill ends the helper and waits for it to go. Safe to call more than once.
func (h *helperProcess) kill() {
	h.cmd.Process.Kill()
	<-h.exited
}

// readyWatcher is the helper's stdout. It passes on the first READY/FAIL
// line and discards everything else (steam_api64.dll prints its own logs).
type readyWatcher struct {
	mu     sync.Mutex
	buf    []byte
	done   bool
	result chan string
}

func (r *readyWatcher) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return len(p), nil
	}
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(r.buf[:i]))
		r.buf = r.buf[i+1:]
		if line == helperReady || strings.HasPrefix(line, helperFailPrefix) {
			r.done, r.buf = true, nil
			r.result <- line
			return len(p), nil
		}
	}
	if len(r.buf) > maxHelperLogSize {
		r.buf = r.buf[:0]
	}
	return len(p), nil
}
