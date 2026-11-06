//go:build windows

package steam

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyValveSignedLibrariesAreTrusted(t *testing.T) {
	var signed string
	var data []byte
	for _, dir := range []string{filepath.Join("..", "..", "..", "bin"), filepath.Join("..", "..", "..")} {
		if p, err := filepath.Abs(filepath.Join(dir, apiLibraryName)); err == nil {
			if b, err := os.ReadFile(p); err == nil {
				signed, data = p, b
				break
			}
		}
	}
	if data == nil {
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
