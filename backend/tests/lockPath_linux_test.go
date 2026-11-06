//go:build linux

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houridle/backend/run/handover"
)

func TestLockPathIgnoresEnvironment(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	first, err := handover.LockPath("")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	second, err := handover.LockPath("")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("lock path depends on XDG_RUNTIME_DIR: %q vs %q", first, second)
	}
	if tmp := filepath.Clean(os.TempDir()); strings.HasPrefix(first, tmp+string(os.PathSeparator)) {
		t.Fatalf("lock file %q is in the shared temp folder", first)
	}
}
