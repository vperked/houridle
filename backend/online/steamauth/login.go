package steamauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// GuardType is a way Steam asks you to prove it's you (Steam Guard).
type GuardType int

const (
	GuardNone               GuardType = 1 // nothing to do
	GuardEmailCode          GuardType = 2 // a code emailed to you
	GuardDeviceCode         GuardType = 3 // the code in the Steam mobile app
	GuardDeviceConfirmation GuardType = 4 // approve in the Steam mobile app
	GuardEmailConfirmation  GuardType = 5 // approve from the email link
)

// Confirmation is one way the sign-in can be completed.
type Confirmation struct {
	Type    GuardType
	Message string // e.g. the email domain, from Steam
}

// Pending is a sign-in that has started and is waiting to be completed.
type Pending struct {
	ClientID      uint64
	RequestID     []byte
	Interval      time.Duration
	SteamID       uint64 // known up front for password sign-ins; 0 for QR
	Confirmations []Confirmation
	ChallengeURL  string // QR sign-ins: what the Steam app scans
}

// NeedsCode returns the code-entry guard Steam allows, preferring the
// mobile app code. ok is false if no code is accepted.
func (p *Pending) NeedsCode() (GuardType, string, bool) {
	for _, want := range []GuardType{GuardDeviceCode, GuardEmailCode} {
		for _, c := range p.Confirmations {
			if c.Type == want {
				return want, c.Message, true
			}
		}
	}
	return 0, "", false
}

// CanConfirm reports whether Steam accepts the given approval type.
func (p *Pending) CanConfirm(t GuardType) bool {
	for _, c := range p.Confirmations {
		if c.Type == t {
			return true
		}
	}
	return false
}

// Result is a finished sign-in.
type Result struct {
	AccountName  string
	SteamID      uint64
	RefreshToken string
}

const (
	platformSteamClient = 1
	persistent          = 1
	defaultPoll         = 5 * time.Second
	minPoll             = time.Second
	maxPoll             = 30 * time.Second
)

func deviceDetails(name string) *Encoder {
	osType := int64(-203) // Linux
	if runtime.GOOS == "windows" {
		osType = 16 // Windows 10
	}
	return new(Encoder).
		String(1, name).
		Varint(2, platformSteamClient).
		Varint(3, uint64(osType))
}

func pollInterval(seconds float32) time.Duration {
	s := float64(seconds)
	switch {
	case s != s || s <= 0:
		return defaultPoll
	case s >= maxPoll.Seconds():
		return maxPoll
	}
	return max(time.Duration(s*float64(time.Second)), minPoll)
}

func confirmations(d *Decoded, num protowire.Number) []Confirmation {
	var out []Confirmation
	for _, m := range d.Messages(num) {
		out = append(out, Confirmation{Type: GuardType(m.Uint(1)), Message: m.Str(2)})
	}
	return out
}

// BeginQR starts a QR-code sign-in. The returned ChallengeURL is what the
// Steam mobile app scans; it can change while waiting (see Poll).
func (c *Client) BeginQR(ctx context.Context, deviceName string) (*Pending, error) {
	req := new(Encoder).
		String(1, deviceName).
		Varint(2, platformSteamClient).
		Message(3, deviceDetails(deviceName)).
		String(4, "Client")
	d, err := c.call(ctx, "BeginAuthSessionViaQR", false, req.Out())
	if err != nil {
		return nil, err
	}
	p := &Pending{
		ClientID:      d.Uint(1),
		ChallengeURL:  d.Str(2),
		RequestID:     d.Raw(3),
		Interval:      pollInterval(d.Float(4)),
		Confirmations: confirmations(d, 5),
	}
	if p.ClientID == 0 || p.ChallengeURL == "" || len(p.RequestID) == 0 {
		return nil, errors.New("Steam sent an incomplete QR sign-in")
	}
	return p, nil
}

// BeginPassword starts a username and password sign-in. The password is
// encrypted with the public key Steam hands out for that account, the way
// Steam's own sign-in page does, and is never sent in the clear.
func (c *Client) BeginPassword(ctx context.Context, account, password, deviceName string) (*Pending, error) {
	keyReq := new(Encoder).String(1, account)
	k, err := c.call(ctx, "GetPasswordRSAPublicKey", true, keyReq.Out())
	if err != nil {
		return nil, err
	}
	mod, okMod := new(big.Int).SetString(k.Str(1), 16)
	exp, okExp := new(big.Int).SetString(k.Str(2), 16)
	if !okMod || !okExp || !exp.IsInt64() || k.Uint(3) == 0 {
		return nil, errors.New("Steam sent an unusable password key (is the account name right?)")
	}
	pub := &rsa.PublicKey{N: mod, E: int(exp.Int64())}
	// Steam's sign-in only takes PKCS #1 v1.5 (its own clients and web page
	// send that), so the newer OAEP padding isn't an option here.
	//lint:ignore SA1019 Steam's protocol requires PKCS #1 v1.5
	sealed, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return nil, fmt.Errorf("couldn't protect the password: %w", err)
	}

	req := new(Encoder).
		String(1, deviceName).
		String(2, account).
		String(3, base64.StdEncoding.EncodeToString(sealed)).
		Varint(4, k.Uint(3)).
		Bool(5, true).
		Varint(6, platformSteamClient).
		Varint(7, persistent).
		String(8, "Client").
		Message(9, deviceDetails(deviceName))
	d, err := c.call(ctx, "BeginAuthSessionViaCredentials", false, req.Out())
	if err != nil {
		return nil, err
	}
	p := &Pending{
		ClientID:      d.Uint(1),
		RequestID:     d.Raw(2),
		Interval:      pollInterval(d.Float(3)),
		Confirmations: confirmations(d, 4),
		SteamID:       d.Uint(5),
	}
	if p.ClientID == 0 || len(p.RequestID) == 0 || !ValidSteamID(p.SteamID) {
		return nil, errors.New("Steam sent an incomplete sign-in")
	}
	return p, nil
}

// SubmitCode answers a code-entry Steam Guard challenge.
func (c *Client) SubmitCode(ctx context.Context, p *Pending, kind GuardType, code string) error {
	req := new(Encoder).
		Varint(1, p.ClientID).
		Fixed64(2, p.SteamID).
		String(3, code).
		Varint(4, uint64(kind))
	_, err := c.call(ctx, "UpdateAuthSessionWithSteamGuardCode", false, req.Out())
	return err
}

// Poll asks whether the sign-in has been completed. It returns nil, nil
// while still waiting. For QR sign-ins Steam may rotate the challenge; p is
// updated so the caller can redraw the code.
func (c *Client) Poll(ctx context.Context, p *Pending) (*Result, error) {
	req := new(Encoder).Varint(1, p.ClientID).Bytes(2, p.RequestID)
	d, err := c.call(ctx, "PollAuthSessionStatus", false, req.Out())
	if err != nil {
		return nil, err
	}
	if id := d.Uint(1); id != 0 {
		p.ClientID = id
	}
	if u := d.Str(2); u != "" {
		p.ChallengeURL = u
	}
	refresh := d.Str(3)
	if refresh == "" {
		return nil, nil
	}
	info, err := ParseToken(refresh)
	if err != nil {
		return nil, fmt.Errorf("Steam sent a sign-in token houridle can't read: %w", err)
	}
	if !info.CanLogOn() {
		return nil, errors.New("Steam sent a token that can't sign in to the Steam client network")
	}
	// Whatever way we signed in, the token must belong to the account we
	// asked for (a password sign-in names it up front).
	if p.SteamID != 0 && info.SteamID != p.SteamID {
		return nil, errors.New("Steam sent a token for a different account; nothing was saved")
	}
	return &Result{AccountName: d.Str(6), SteamID: info.SteamID, RefreshToken: refresh}, nil
}
