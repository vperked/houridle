//go:build windows

package steamworks

import "syscall"

// dllLibrary is steam_api64.dll loaded with LoadLibrary.
type dllLibrary struct{ dll *syscall.DLL }

func openLibrary(path string) (library, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, err
	}
	return dllLibrary{dll}, nil
}

func (l dllLibrary) lookup(name string) (function, bool) {
	proc, err := l.dll.FindProc(name)
	if err != nil {
		return nil, false
	}
	return func(args ...uintptr) uintptr {
		r, _, _ := proc.Call(args...)
		return r
	}, true
}

func (l dllLibrary) release() { l.dll.Release() }
