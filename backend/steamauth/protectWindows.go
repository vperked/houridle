//go:build windows

package steamauth

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows file modes don't carry permissions, so there is nothing to set.
const permsUnsupported = true

// entropy is extra input DPAPI mixes into the encryption. It is not a
// secret (it is in this program for anyone to read), so it only stops
// tools that decrypt DPAPI blobs blindly; any program running as the same
// Windows user can still decrypt the file. The real protection is DPAPI's:
// other users, and copies of the file taken to another machine or account,
// can't read it.
var entropy = []byte("houridle session v1")

// syncDir does nothing on Windows: a directory can't be synced there, and
// NTFS journals the rename itself (the file's contents were synced first).
func syncDir(string) error { return nil }

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

// protect encrypts for the current Windows user with DPAPI (Windows' own
// data-protection service; houridle does no cryptography itself).
func protect(plain []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob(plain), nil, blob(entropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func unprotect(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, errors.New("empty")
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(sealed), nil, blob(entropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
