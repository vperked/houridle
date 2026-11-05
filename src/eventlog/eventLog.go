// Package eventlog appends houridle's activity to houridle.log, so you can
// see what happened while you weren't watching, e.g. on a server.
package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// FileName is the log's name inside the folder given to Open.
	FileName = "houridle.log"
	// maxSize is when the log moves aside to houridle.log.1 and starts over,
	// so it never grows past about twice this.
	maxSize = 1 << 20
	// rotateRetry is how much more is written after a failed rotation before
	// trying again, so a file that can't be moved isn't retried every line.
	rotateRetry = maxSize / 16
)

// Log is an append-only activity log. Methods on a nil *Log do nothing, so
// callers can carry on if the log couldn't be opened.
type Log struct {
	mu      sync.Mutex
	path    string
	file    *os.File // nil after Close, or while it can't be reopened
	size    int64
	retryAt int64 // after a failed rotation: don't try again below this size
	closed  bool
}

// Open opens (or creates) houridle.log in dir.
func Open(dir string) (*Log, error) {
	l := &Log{path: filepath.Join(dir, FileName)}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

// Path is where the log is written.
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Printf writes one timestamped line.
func (l *Log) Printf(format string, a ...any) {
	if l == nil {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05") + "  " + fmt.Sprintf(format, a...) + "\n"

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	if l.file == nil && l.open() != nil {
		return // still can't open it; try again with the next line
	}
	if l.size+int64(len(line)) > maxSize && l.size >= l.retryAt {
		l.rotate()
	}
	if l.file != nil {
		n, _ := l.file.WriteString(line)
		l.size += int64(n)
	}
}

// Close flushes and closes the log.
func (l *Log) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

func (l *Log) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.file, l.size = f, info.Size()
	return nil
}

// rotate moves the full log to houridle.log.1 (replacing the old one) and
// starts a new file. The file is closed first because Windows can't rename
// an open file. If the rename fails (e.g. another houridle still has the
// file open during a handover), it carries on appending to the same file and
// tries again later; if reopening fails, Printf retries it with each line.
func (l *Log) rotate() {
	l.file.Close()
	l.file = nil
	renameErr := os.Rename(l.path, l.path+".1")
	if l.open() != nil {
		return
	}
	if renameErr != nil {
		l.retryAt = l.size + rotateRetry
	} else {
		l.retryAt = 0
	}
}
