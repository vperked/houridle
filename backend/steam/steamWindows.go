//go:build windows

package steam

import (
	"errors"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	apiLibraryName  = "steam_api64.dll"
	steamImage      = "steam.exe"
	stillActiveCode = 259 // STILL_ACTIVE from GetExitCodeProcess
)

// Path returns the Steam install folder from the registry.
func Path() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		return "", errors.New("couldn't find Steam: it doesn't seem to be installed")
	}
	defer k.Close()
	p, _, err := k.GetStringValue("SteamPath")
	if err != nil || p == "" {
		return "", errors.New("couldn't find Steam's install folder in the registry")
	}
	return filepath.Clean(p), nil
}

// activeProcess reads Steam's running PID and signed-in account ID.
func activeProcess() (pid, accountID uint32) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam\ActiveProcess`, registry.QUERY_VALUE)
	if err != nil {
		return 0, 0
	}
	defer k.Close()
	p, _, _ := k.GetIntegerValue("pid")
	u, _, _ := k.GetIntegerValue("ActiveUser")
	return uint32(p), uint32(u)
}

// steamProcess returns pid if it is a running steam.exe, or 0. Checking the
// image name stops a stale PID that Windows has since reused for another
// program from passing as Steam.
func steamProcess(pid uint32) uint32 {
	if pid == 0 {
		return 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != stillActiveCode {
		return 0
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return 0
	}
	if !strings.EqualFold(filepath.Base(windows.UTF16ToString(buf[:n])), steamImage) {
		return 0
	}
	return pid
}

// pathKey makes paths comparable: Windows paths ignore case.
func pathKey(p string) string { return strings.ToLower(p) }

// loadable reports whether houridle can load this library. steam_api64.dll is
// always the 64-bit build.
func loadable(string) bool { return true }

// valveSigner is the start of the signing certificate's name on Valve's
// libraries ("Valve Corp.").
const valveSigner = "valve"

var (
	crypt32              = windows.NewLazySystemDLL("crypt32.dll")
	procCryptMsgGetParam = crypt32.NewProc("CryptMsgGetParam")
	procCryptMsgClose    = crypt32.NewProc("CryptMsgClose")
)

const cmsgSignerCertInfoParam = 7 // CMSG_SIGNER_CERT_INFO_PARAM

// trusted reports whether path has a valid Authenticode signature from
// Valve. Revocation isn't checked, so this never waits on the network.
func trusted(path string) bool {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	data := &windows.WinTrustData{
		Size:             uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:         windows.WTD_UI_NONE,
		RevocationChecks: windows.WTD_REVOKE_NONE,
		UnionChoice:      windows.WTD_CHOICE_FILE,
		StateAction:      windows.WTD_STATEACTION_VERIFY,
		ProvFlags:        windows.WTD_CACHE_ONLY_URL_RETRIEVAL,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&windows.WinTrustFileInfo{
			Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
			FilePath: path16,
		}),
	}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	if verifyErr != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(signerName(path16)), valveSigner)
}

// signerName is the display name of the certificate that signed the file
// (already verified by WinVerifyTrust), or "" if it can't be read.
func signerName(path16 *uint16) string {
	var encoding, contentType, formatType uint32
	var store, msg windows.Handle
	err := windows.CryptQueryObject(windows.CERT_QUERY_OBJECT_FILE, unsafe.Pointer(path16),
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED, windows.CERT_QUERY_FORMAT_FLAG_BINARY, 0,
		&encoding, &contentType, &formatType, &store, &msg, nil)
	if err != nil {
		return ""
	}
	defer windows.CertCloseStore(store, 0)
	defer procCryptMsgClose.Call(uintptr(msg))

	var size uint32
	if ok, _, _ := procCryptMsgGetParam.Call(uintptr(msg), cmsgSignerCertInfoParam, 0, 0, uintptr(unsafe.Pointer(&size))); ok == 0 || size == 0 {
		return ""
	}
	info := make([]byte, size) // a CERT_INFO plus the data it points to
	if ok, _, _ := procCryptMsgGetParam.Call(uintptr(msg), cmsgSignerCertInfoParam, 0, uintptr(unsafe.Pointer(&info[0])), uintptr(unsafe.Pointer(&size))); ok == 0 {
		return ""
	}
	cert, err := windows.CertFindCertificateInStore(store, windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING,
		0, windows.CERT_FIND_SUBJECT_CERT, unsafe.Pointer(&info[0]), nil)
	if err != nil {
		return ""
	}
	defer windows.CertFreeCertificateContext(cert)
	name := make([]uint16, 256)
	n := windows.CertGetNameString(cert, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, &name[0], uint32(len(name)))
	if n <= 1 {
		return ""
	}
	return windows.UTF16ToString(name[:n])
}
