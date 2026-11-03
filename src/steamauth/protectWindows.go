//go:build windows

package steamauth

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows file modes don't carry permissions, so there is nothing to set.
const permsUnsupported = true

// entropy ties the encrypted file to houridle: another program running as
// the same user can't just ask Windows to decrypt it without knowing this.
var entropy = []byte("houridle session v1")

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
