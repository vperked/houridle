//go:build !windows

package steamauth

import "os"

const permsUnsupported = false

// syncDir makes a rename in dir durable: on Linux the new directory entry
// is only safe on disk once the directory itself is synced.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// On Linux the protection is the file mode: the session is written 0600 in a
// 0700 folder, readable only by its owner (the same model as ~/.ssh).
func protect(plain []byte) ([]byte, error)    { return plain, nil }
func unprotect(sealed []byte) ([]byte, error) { return sealed, nil }
