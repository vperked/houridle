//go:build linux

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houridle/src/handover"
)

// Every copy must agree on the lock file whatever its environment, or two
// could idle at once; and it must never be in /tmp, where another user could
// create it first.
func TestLockPathIgnoresEnvironment(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	first, err := handover.LockPath()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	second, err := handover.LockPath()
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
