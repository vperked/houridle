package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houridle/backend/run/eventlog"
)

func TestEventLogAppendsLines(t *testing.T) {
	dir := t.TempDir()
	log, err := eventlog.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("start  idling %d game(s)", 2)
	log.Printf("ok     Rust is idling")
	log.Close()

	// Reopening appends rather than overwriting.
	log, _ = eventlog.Open(dir)
	log.Printf("stop   after 1h")
	log.Close()

	data, err := os.ReadFile(filepath.Join(dir, eventlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[1], "ok     Rust is idling") || !strings.HasSuffix(lines[2], "stop   after 1h") {
		t.Fatalf("log is:\n%s", data)
	}
}

func TestEventLogRotates(t *testing.T) {
	dir := t.TempDir()
	log, err := eventlog.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 1000)
	for range 1500 { // ~1.5 MB, past the 1 MiB limit
		log.Printf("%s", line)
	}
	log.Close()

	current, err1 := os.Stat(filepath.Join(dir, eventlog.FileName))
	old, err2 := os.Stat(filepath.Join(dir, eventlog.FileName+".1"))
	if err1 != nil || err2 != nil {
		t.Fatalf("expected houridle.log and houridle.log.1: %v, %v", err1, err2)
	}
	if current.Size() >= old.Size() || old.Size() > 1<<20 {
		t.Fatalf("sizes after rotating: current %d, old %d", current.Size(), old.Size())
	}
}

func TestEventLogKeepsWritingWhenRotationFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, eventlog.FileName+".1")
	if err := os.MkdirAll(filepath.Join(blocker, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	log, err := eventlog.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 1000)
	const lines = 1500
	for i := range lines {
		log.Printf("%d %s", i, line)
	}
	log.Close()

	data, err := os.ReadFile(filepath.Join(dir, eventlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(got) != lines || !strings.Contains(got[lines-1], fmt.Sprintf(" %d x", lines-1)) {
		t.Fatalf("got %d lines in houridle.log, want all %d", len(got), lines)
	}

	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	log, _ = eventlog.Open(dir)
	for range 100 {
		log.Printf("%s", line)
	}
	log.Close()
	if _, err := os.Stat(blocker); err != nil {
		t.Fatalf("expected houridle.log.1 after the blocker went: %v", err)
	}
}

func TestNilEventLogIsSafe(t *testing.T) {
	var log *eventlog.Log // e.g. the log file couldn't be opened
	log.Printf("ignored")
	log.Close()
	if log.Path() != "" {
		t.Fatal("a nil log has no path")
	}
}
