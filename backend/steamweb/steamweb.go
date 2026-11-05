// Package steamweb is what every package that talks to Steam's websites
// (Community, the store, its checkout) shares: an HTTP client signed in as
// one account, and short error messages.
package steamweb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

// Client is an HTTP client signed in to Steam's websites as one account.
type Client struct {
	HTTP *http.Client
	// Session is the sessionid cookie's value. Steam checks that a form's
	// sessionid matches it; any random value works.
	Session string
}

// New is a client signed in with an access token on each site (base URLs
// such as "https://steamcommunity.com"). The token is a secret: it's only
// sent to those sites, as the steamLoginSecure cookie, which the jar keeps
// from being sent anywhere else (redirects included).
func New(steamID uint64, accessToken string, timeout time.Duration, sites ...string) (*Client, error) {
	b := make([]byte, 12)
	rand.Read(b)
	session := hex.EncodeToString(b)
	login := url.QueryEscape(fmt.Sprintf("%d||%s", steamID, accessToken))
	jar, _ := cookiejar.New(nil)
	for _, site := range sites {
		u, err := url.Parse(site)
		if err != nil {
			return nil, err
		}
		secure := u.Scheme == "https"
		jar.SetCookies(u, []*http.Cookie{
			{Name: "steamLoginSecure", Value: login, Path: "/", Secure: secure, HttpOnly: true},
			{Name: "sessionid", Value: session, Path: "/", Secure: secure},
		})
	}
	return &Client{HTTP: &http.Client{Jar: jar, Timeout: timeout}, Session: session}, nil
}

// ShortErr drops the method and address Go adds to a failed request's
// error, which only make messages longer.
func ShortErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// GetJSON fetches u and decodes its JSON (at most limit bytes) into into.
func GetJSON(ctx context.Context, client *http.Client, u string, into any, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ShortErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(into)
}
