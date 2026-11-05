// Package cards reads your Steam badge pages to find which games still have
// trading card drops left, so houridle can idle exactly those (`houridle
// start -cards`).
//
// Steam has no API for this; its own badge page is the source, read with
// the same sign-in houridle idles with (an access token, as a cookie).
package cards

import (
	"context"
	"errors"
	"fmt"
	"houridle/backend/steam"
	"houridle/backend/steamweb"
	"html"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BaseURL is Steam Community. A variable so tests can use a local fake.
var BaseURL = "https://steamcommunity.com"

const (
	maxPages     = 50      // a safety stop; that's ~7500 badges
	maxPageBytes = 4 << 20 // a badge page is ~100 KB
	fetchTimeout = 30 * time.Second
)

// Drop is a game with card drops left.
type Drop struct {
	AppID     uint32
	Name      string
	Remaining int
}

// ErrNotSignedIn means Steam showed the page without the sign-in (the
// token was refused), so drops can't be read.
var ErrNotSignedIn = errors.New("Steam Community didn't accept the sign-in, so card drops can't be read")

var (
	rowStart  = regexp.MustCompile(`class="badge_row is_link"`)
	appLink   = regexp.MustCompile(`/gamecards/(\d+)/`)
	remaining = regexp.MustCompile(`progress_info_bold">\s*(\d+) card drops? remaining`)
	title     = regexp.MustCompile(`(?s)class="badge_title">\s*(.*?)\s*(?:&nbsp;|<)`)
	signedIn  = regexp.MustCompile(`g_steamID = "(\d+)"`)
	pageLink  = regexp.MustCompile(`class="pagelink"[^>]*href="[^"]*[?&]p=(\d+)`)
)

// Remaining returns every game with card drops left on the account, most
// drops first. accessToken is a Steam access token for steamID (a secret:
// it is only sent to Steam, as the sign-in cookie).
func Remaining(ctx context.Context, steamID uint64, accessToken string) ([]Drop, error) {
	var all []Drop
	err := eachBadgePage(ctx, steamID, accessToken, func(page string) {
		all = append(all, Parse(page)...)
	})
	if err != nil {
		return nil, err
	}
	sortDrops(all)
	return all, nil
}

// Parse reads the games with drops left from one badge page.
func Parse(page string) []Drop {
	var out []Drop
	starts := rowStart.FindAllStringIndex(page, -1)
	for i, s := range starts {
		end := len(page)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		row := page[s[0]:end]
		app := appLink.FindStringSubmatch(row)
		left := remaining.FindStringSubmatch(row)
		if app == nil || left == nil {
			continue // a community badge, or a game with no drops left
		}
		id, err := strconv.ParseUint(app[1], 10, 32)
		n, _ := strconv.Atoi(left[1])
		if err != nil || id == 0 || n <= 0 {
			continue
		}
		name := ""
		if t := title.FindStringSubmatch(row); t != nil {
			name = strings.TrimSpace(html.UnescapeString(t[1]))
		}
		if name == "" {
			name = steam.PlaceholderName(uint32(id))
		}
		out = append(out, Drop{AppID: uint32(id), Name: name, Remaining: n})
	}
	return out
}

// sortDrops puts the most drops first; stable, so equal games keep the
// page's order.
func sortDrops(d []Drop) {
	slices.SortStableFunc(d, func(a, b Drop) int { return b.Remaining - a.Remaining })
}

// newClient is a client signed in to Steam Community. The cookie is set in
// a jar, so it follows the redirect to a custom profile URL and is never
// sent anywhere but Steam Community.
func newClient(steamID uint64, accessToken string) (*steamweb.Client, error) {
	return steamweb.New(steamID, accessToken, fetchTimeout, BaseURL)
}

func fetch(ctx context.Context, client *http.Client, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't reach Steam Community: %w", steamweb.ShortErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Steam Community answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return "", fmt.Errorf("couldn't read the badge page: %w", err)
	}
	return string(data), nil
}
