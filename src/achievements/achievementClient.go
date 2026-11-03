//go:build windows

package achievements

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"houridle/src/steamworks"
)

const (
	loadTimeout     = 30 * time.Second
	applyTimeout    = 30 * time.Second
	stopGracePeriod = 5 * time.Second
	createNoWindow  = 0x08000000
	maxBufferedLine = 1 << 20
)

// Session is a running achievements helper for one game.
type Session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	exited chan struct{}
}

// Open starts a helper for appID and returns the game's achievements.
// exePath is houridle itself, re-run in helper mode.
func Open(exePath, dllPath string, appID uint32) (*Session, []steamworks.Achievement, error) {
	sink := &lineSink{lines: make(chan string, 4)}
	cmd := exec.Command(exePath, "-"+HelperFlag, strconv.FormatUint(uint64(appID), 10), "-dll", dllPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Stdout = sink
	stdin, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return nil, nil, errors.New("couldn't start the achievements helper: " + err.Error())
	}
	s := &Session{cmd: cmd, stdin: stdin, lines: sink.lines, exited: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(s.exited)
	}()

	var reply listReply
	if err := s.receive(&reply, loadTimeout); err != nil {
		s.Close()
		return nil, nil, err
	}
	if reply.Error != "" {
		s.Close()
		return nil, nil, &steamworks.Error{Code: reply.Code, Msg: reply.Error}
	}
	return s, reply.Achievements, nil
}

// Apply unlocks and locks achievements and saves them to Steam. The session
// is finished afterwards; call Close.
func (s *Session) Apply(unlock, lock []string) (Result, error) {
	data, _ := json.Marshal(changeRequest{Unlock: unlock, Lock: lock})
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		return Result{}, errors.New("lost the achievements helper: " + err.Error())
	}
	var res Result
	if err := s.receive(&res, applyTimeout); err != nil {
		return Result{}, err
	}
	if res.Error != "" {
		return res, errors.New(res.Error)
	}
	return res, nil
}

// Close stops the helper, discarding anything not applied.
func (s *Session) Close() {
	s.stdin.Close()
	select {
	case <-s.exited:
	case <-time.After(stopGracePeriod):
		s.cmd.Process.Kill()
		<-s.exited
	}
}

func (s *Session) receive(v any, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line := <-s.lines:
		return json.Unmarshal([]byte(line), v)
	case <-s.exited:
		select { // the reply may have landed just before exit
		case line := <-s.lines:
			return json.Unmarshal([]byte(line), v)
		default:
			return errors.New("the achievements helper exited unexpectedly")
		}
	case <-timer.C:
		return errors.New("timed out waiting for Steam")
	}
}

// lineSink is the helper's stdout: it forwards our protocol lines and drops
// everything else (steam_api64.dll prints its own log lines).
type lineSink struct {
	mu    sync.Mutex
	buf   []byte
	lines chan string
}

func (l *lineSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(l.buf[:i]))
		l.buf = l.buf[i+1:]
		if payload, ok := strings.CutPrefix(line, linePrefix); ok {
			select {
			case l.lines <- payload:
			default: // nobody is waiting for more than a few replies
			}
		}
	}
	if len(l.buf) > maxBufferedLine {
		l.buf = l.buf[:0]
	}
	return len(p), nil
}
