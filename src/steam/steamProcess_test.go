package steam

import (
	"os"
	"path/filepath"
	"testing"
)

// A PID that isn't Steam (here, this test) must not pass for it, however
// alive it is: Steam's recorded PID can be stale and reused.
func TestOtherProcessIsNotSteam(t *testing.T) {
	if pid := steamProcess(uint32(os.Getpid())); pid != 0 && pid == uint32(os.Getpid()) {
		t.Fatal("this test process was taken for Steam")
	}
	if steamProcess(0) != 0 {
		t.Fatal("PID 0 is never Steam")
	}
}

func TestCopyFileReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("library"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "library" {
		t.Fatalf("copy = %q, %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
