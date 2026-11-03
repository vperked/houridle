//go:build windows

// Package steamworks calls Valve's steam_api64.dll (the Steamworks flat API)
// as a given app, inside the current process.
package steamworks

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
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

// API is an initialized Steamworks session for one app.
type API struct {
	dll          *syscall.DLL
	runCallbacks *syscall.Proc
	shutdown     *syscall.Proc
}

// Init loads the DLL and starts Steamworks as appID. A process can only be
// one app, and only once, so callers run this in a dedicated helper process.
func Init(dllPath string, appID uint32) (*API, error) {
	id := strconv.FormatUint(uint64(appID), 10)
	os.Setenv("SteamAppId", id)
	os.Setenv("SteamGameId", id)

	dll, err := syscall.LoadDLL(dllPath)
	if err != nil {
		return nil, &Error{CodeLoad, fmt.Sprintf("couldn't load %s: %v", dllPath, err)}
	}
	if err := initSteam(dll); err != nil {
		dll.Release()
		return nil, err
	}
	api := &API{dll: dll}
	api.runCallbacks, _ = dll.FindProc("SteamAPI_RunCallbacks")
	api.shutdown, _ = dll.FindProc("SteamAPI_Shutdown")
	return api, nil
}

// RunCallbacks lets Steam deliver pending results. Call it regularly.
func (a *API) RunCallbacks() {
	if a.runCallbacks != nil {
		a.runCallbacks.Call()
	}
}

// Shutdown ends the Steamworks session.
func (a *API) Shutdown() {
	if a.shutdown != nil {
		a.shutdown.Call()
	}
}

// proc finds an export, or reports that this DLL is too old for the feature.
func (a *API) proc(name string) (*syscall.Proc, error) {
	p, err := a.dll.FindProc(name)
	if err != nil {
		return nil, &Error{CodeLoad, "this steam_api64.dll doesn't have " + name + "; try a newer one with -dll"}
	}
	return p, nil
}

// initSteam calls whichever init entry point this steam_api64.dll exports.
func initSteam(dll *syscall.DLL) error {
	// Steamworks SDK 1.58+: ESteamAPIInitResult SteamAPI_InitFlat(SteamErrMsg *err)
	if proc, err := dll.FindProc("SteamAPI_InitFlat"); err == nil {
		var msg [1024]byte
		r, _, _ := proc.Call(uintptr(unsafe.Pointer(&msg[0])))
		text := string(bytes.TrimRight(msg[:], "\x00"))
		switch int32(r) {
		case initOK:
			return nil
		case initNoSteamClient:
			return &Error{CodeNoSteam, "Steam isn't running or isn't signed in"}
		case initVersionMismatch:
			return &Error{CodeOutdated, "Steam client is too old for this steam_api64.dll; update Steam"}
		}
		if strings.Contains(text, "ConnectToGlobalUser") {
			return &Error{CodeNotOwned, "Steam won't run this app on your account (not owned, or family-shared)"}
		}
		return &Error{CodeRefused, "Steam refused to start this app (" + text + ")"}
	}

	// Older SDKs: bool SteamAPI_Init() / SteamAPI_InitSafe()
	for _, name := range []string{"SteamAPI_Init", "SteamAPI_InitSafe"} {
		if proc, err := dll.FindProc(name); err == nil {
			if r, _, _ := proc.Call(); r&0xff != 0 {
				return nil
			}
			return &Error{CodeRefused, "Steam refused to start this app; is Steam running and do you own it?"}
		}
	}
	return &Error{CodeLoad, "this steam_api64.dll has no init function houridle recognizes"}
}

// cString copies a NUL-terminated UTF-8 string owned by the DLL. The pointer
// arrives as a uintptr from Proc.Call; reinterpreting the variable's bytes
// (rather than converting the value) is the vet-clean way to read it.
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

// cBytes makes a NUL-terminated heap copy of s to pass to the DLL. Callers
// keep it alive with runtime.KeepAlive until the call returns.
func cBytes(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

func boolResult(r uintptr) bool { return r&0xff != 0 }
