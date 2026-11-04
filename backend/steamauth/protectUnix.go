//go:build !windows

package steamauth

const permsUnsupported = false

// On Linux the protection is the file mode: the session is written 0600 in a
// 0700 folder, readable only by its owner (the same model as ~/.ssh).
func protect(plain []byte) ([]byte, error)    { return plain, nil }
func unprotect(sealed []byte) ([]byte, error) { return sealed, nil }
