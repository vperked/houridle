//go:build windows

package steam

import (
	"os"
	"path/filepath"
	"testing"
)

// The Steam API library is only picked up from game folders when Valve's
// signature checks out; a modified copy is skipped.
func TestOnlyValveSignedLibrariesAreTrusted(t *testing.T) {
	signed, err := filepath.Abs(filepath.Join("..", "..", apiLibraryName))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(signed)
	if err != nil {
		t.Skipf("no %s in the project folder to test with", apiLibraryName)
	}
	if !trusted(signed) {
		t.Fatalf("%s should verify as signed by Valve", signed)
	}

	tampered := filepath.Join(t.TempDir(), apiLibraryName)
	data[len(data)/2]++
	if err := os.WriteFile(tampered, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if trusted(tampered) {
		t.Fatal("a modified library must not verify")
	}

	unsigned := filepath.Join(t.TempDir(), apiLibraryName)
	if err := os.WriteFile(unsigned, []byte("MZ not really a library"), 0o644); err != nil {
		t.Fatal(err)
	}
	if trusted(unsigned) {
		t.Fatal("an unsigned file must not verify")
	}
}
