package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houridle/backend/eventlog"
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

func TestNilEventLogIsSafe(t *testing.T) {
	var log *eventlog.Log // e.g. the log file couldn't be opened
	log.Printf("ignored")
	log.Close()
	if log.Path() != "" {
		t.Fatal("a nil log has no path")
	}
}
