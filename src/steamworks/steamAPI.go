// Package steamworks calls Valve's Steamworks API library (steam_api64.dll on
// Windows, libsteam_api.so on Linux) as a given game, inside this process.
//
// Loading the library differs per OS (libraryWindows.go, libraryLinux.go);
// everything else here is shared.
package steamworks

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

// Error codes, so callers can show a short label.
const (
	CodeNotOwned = "notOwned"
	CodeNoSteam  = "noSteam"
	CodeOutdated = "outdated"
	CodeLoad     = "load"
	CodeRefused  = "refused"
)

// ESteamAPIInitResult values from SteamAPI_InitFlat.
const (
	initOK              = 0
	initNoSteamClient   = 2
	initVersionMismatch = 3
)

// Error is a failure talking to Steam, with a code for the UI.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// library is Steam's API library, loaded into this process.
type library interface {
	// lookup finds an exported function, or reports false if it's missing.
	lookup(name string) (function, bool)
	release()
}

// function calls one exported C function with pointer-sized arguments and
// returns its result register.
type function func(args ...uintptr) uintptr

// API is an initialized Steamworks session for one app.
type API struct {
	lib          library
	runCallbacks function
	shutdown     function
}

// Init loads the library and starts Steamworks as appID. A process can only
// be one app, and only once, so callers run this in a dedicated helper.
func Init(libPath string, appID uint32) (*API, error) {
	id := strconv.FormatUint(uint64(appID), 10)
	os.Setenv("SteamAppId", id)
	os.Setenv("SteamGameId", id)

	lib, err := openLibrary(libPath)
	if err != nil {
		return nil, &Error{CodeLoad, fmt.Sprintf("couldn't load %s: %v", libPath, err)}
	}
	if err := initSteam(lib); err != nil {
		lib.release()
		return nil, err
	}
	api := &API{lib: lib}
	api.runCallbacks, _ = lib.lookup("SteamAPI_RunCallbacks")
	api.shutdown, _ = lib.lookup("SteamAPI_Shutdown")
	return api, nil
}

// RunCallbacks lets Steam deliver pending results. Call it regularly.
func (a *API) RunCallbacks() {
	if a.runCallbacks != nil {
		a.runCallbacks()
	}
}

// Shutdown ends the Steamworks session.
func (a *API) Shutdown() {
	if a.shutdown != nil {
		a.shutdown()
	}
}

// function finds an export, or reports that this library is too old for it.
func (a *API) function(name string) (function, error) {
	fn, ok := a.lib.lookup(name)
	if !ok {
		return nil, &Error{CodeLoad, "Steam's API library doesn't have " + name + "; try a newer one with -dll"}
	}
	return fn, nil
}

// initSteam calls whichever init function this library version has.
func initSteam(lib library) error {
	// Steamworks SDK 1.58+: ESteamAPIInitResult SteamAPI_InitFlat(SteamErrMsg *err)
	if initFlat, ok := lib.lookup("SteamAPI_InitFlat"); ok {
		msg := new([1024]byte)
		r := initFlat(uintptr(unsafe.Pointer(&msg[0])))
		runtime.KeepAlive(msg)
		text := string(bytes.TrimRight(msg[:], "\x00"))
		switch int32(r) {
		case initOK:
			return nil
		case initNoSteamClient:
			return &Error{CodeNoSteam, "Steam isn't running or isn't signed in"}
		case initVersionMismatch:
			return &Error{CodeOutdated, "Steam client is too old for this Steam API library; update Steam"}
		}
		if strings.Contains(text, "ConnectToGlobalUser") {
			return &Error{CodeNotOwned, "Steam won't run this app on your account (not owned, or family-shared)"}
		}
		return &Error{CodeRefused, "Steam refused to start this app (" + text + ")"}
	}

	// Older SDKs: bool SteamAPI_Init() / SteamAPI_InitSafe()
	for _, name := range []string{"SteamAPI_Init", "SteamAPI_InitSafe"} {
		if init, ok := lib.lookup(name); ok {
			if boolResult(init()) {
				return nil
			}
			return &Error{CodeRefused, "Steam refused to start this app; is Steam running and do you own it?"}
		}
	}
	return &Error{CodeLoad, "this Steam API library has no init function houridle recognizes"}
}

// cString copies a NUL-terminated UTF-8 string owned by the library. The
// pointer arrives as a uintptr; reinterpreting the variable's bytes (rather
// than converting the value) is the vet-clean way to read it.
func cString(p uintptr) string {
	if p == 0 {
		return ""
	}
	start := *(**byte)(unsafe.Pointer(&p))
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(start), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(start, n))
}

// cBytes makes a NUL-terminated heap copy of s to pass to the library.
// Callers keep it alive with runtime.KeepAlive until the call returns.
func cBytes(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// boolResult reads a C bool return value (only the low byte is set).
func boolResult(r uintptr) bool { return r&0xff != 0 }
