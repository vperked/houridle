//go:build linux

package steamworks

import "github.com/ebitengine/purego"

// soLibrary is libsteam_api.so loaded with dlopen (via purego, so houridle
// doesn't need a C compiler).
type soLibrary struct{ handle uintptr }

func openLibrary(path string) (library, error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	return soLibrary{handle}, nil
}

func (l soLibrary) lookup(name string) (function, bool) {
	sym, err := purego.Dlsym(l.handle, name)
	if err != nil || sym == 0 {
		return nil, false
	}
	return func(args ...uintptr) uintptr {
		r, _, _ := purego.SyscallN(sym, args...)
		return r
	}, true
}

func (l soLibrary) release() { purego.Dlclose(l.handle) }
