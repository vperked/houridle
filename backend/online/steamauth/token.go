package steamauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// TokenInfo is what a Steam token (a JWT) says about itself. It is only
// read, never trusted: Steam checks the signature when the token is used.
// houridle uses it to know whose token it is and when it runs out.
type TokenInfo struct {
	SteamID  uint64
	Expires  time.Time
	Audience []string
}

// ParseToken reads the claims of a Steam access or refresh token.
func ParseToken(token string) (TokenInfo, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return TokenInfo{}, errors.New("not a Steam token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return TokenInfo{}, errors.New("not a Steam token")
	}
	var claims struct {
		Sub string          `json:"sub"`
		Exp int64           `json:"exp"`
		Aud json.RawMessage `json:"aud"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return TokenInfo{}, errors.New("not a Steam token")
	}
	id, err := strconv.ParseUint(claims.Sub, 10, 64)
	if err != nil || !ValidSteamID(id) {
		return TokenInfo{}, errors.New("Steam token has no valid SteamID")
	}
	info := TokenInfo{SteamID: id}
	if claims.Exp > 0 {
		info.Expires = time.Unix(claims.Exp, 0)
	}
	var many []string
	if json.Unmarshal(claims.Aud, &many) != nil {
		var one string
		if json.Unmarshal(claims.Aud, &one) == nil {
			many = []string{one}
		}
	}
	info.Audience = many
	return info, nil
}

// CanLogOn reports whether the token is for the Steam client network, which
// is what idling needs (web or mobile tokens can't sign in there).
func (t TokenInfo) CanLogOn() bool { return slices.Contains(t.Audience, "client") }

// Expired reports whether the token has run out at now.
func (t TokenInfo) Expired(now time.Time) bool {
	return !t.Expires.IsZero() && !now.Before(t.Expires)
}

// SteamID64 layout: universe (8 bits) | type (4) | instance (20) | account (32).
const (
	universePublic  = 1
	typeIndividual  = 1
	instanceDesktop = 1
)

// ValidSteamID reports whether id is an ordinary user account in the public
// universe, which is the only kind houridle signs in as.
func ValidSteamID(id uint64) bool {
	return id>>56 == universePublic &&
		(id>>52)&0xF == typeIndividual &&
		(id>>32)&0xFFFFF == instanceDesktop &&
		uint32(id) != 0
}

// FormatSteamID is the SteamID64 as shown to people.
func FormatSteamID(id uint64) string { return fmt.Sprint(id) }
