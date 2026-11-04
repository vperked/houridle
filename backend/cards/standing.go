package cards

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Standing is what Steam says about an account's record: bans, trade
// restrictions, and whether it's limited.
type Standing struct {
	VACBans      int    // VAC bans on record
	GameBans     int    // game (developer) bans on record
	DaysSinceBan int    // days since the last ban; -1 if there's none
	TradeBan     string // "None", "Probation" or "Banned"
	Limited      bool   // a limited account: hasn't spent $5 on Steam, so some features are off
}

// Banned reports whether the account has any ban or trade ban.
func (s Standing) Banned() bool {
	return s.VACBans > 0 || s.GameBans > 0 || s.TradeBan != "" && s.TradeBan != "None"
}

var (
	xmlField   = func(name string) *regexp.Regexp { return regexp.MustCompile(`<` + name + `>(?:<!\[CDATA\[)?([^<\]]*)`) }
	xmlVAC     = xmlField("vacBanned")
	xmlTrade   = xmlField("tradeBanState")
	xmlLimited = xmlField("isLimitedAccount")
	tagsRe     = regexp.MustCompile(`<[^>]+>`)
	vacBans    = regexp.MustCompile(`(\d+|Multiple) VAC bans? on record`)
	gameBans   = regexp.MustCompile(`(\d+|Multiple) game bans? on record`)
	sinceBan   = regexp.MustCompile(`(\d+) day\(s\) since last ban`)
)

// AccountStanding reads the account's record from its Steam Community
// profile (the same as anyone visiting it sees).
func AccountStanding(ctx context.Context, steamID uint64, accessToken string) (Standing, error) {
	client, err := newClient(steamID, accessToken)
	if err != nil {
		return Standing{}, err
	}
	profile := fmt.Sprintf("%s/profiles/%d/", BaseURL, steamID)
	xml, err := fetch(ctx, client.HTTP, profile+"?xml=1")
	if err != nil {
		return Standing{}, err
	}
	m := xmlTrade.FindStringSubmatch(xml)
	if m == nil {
		return Standing{}, fmt.Errorf("couldn't read the account's profile")
	}
	s := Standing{TradeBan: strings.TrimSpace(m[1]), DaysSinceBan: -1}
	if m := xmlLimited.FindStringSubmatch(xml); m != nil {
		s.Limited = strings.TrimSpace(m[1]) == "1"
	}
	vacFlag := false
	if m := xmlVAC.FindStringSubmatch(xml); m != nil {
		vacFlag = strings.TrimSpace(m[1]) == "1"
	}
	page, err := fetch(ctx, client.HTTP, profile+"?l=english")
	if err != nil {
		return Standing{}, err
	}
	parseBans(page, &s)
	if vacFlag && s.VACBans == 0 {
		s.VACBans = 1 // the profile flag says so even if the page didn't
	}
	return s, nil
}

// parseBans reads the ban counts from a profile page's ban notice.
func parseBans(page string, s *Standing) {
	i := strings.Index(page, `class="profile_ban_status"`)
	if i < 0 {
		return // no bans on record
	}
	block := page[i:min(len(page), i+1500)] // the notice and the lines after it
	text := html.UnescapeString(tagsRe.ReplaceAllString(block, " "))
	count := func(re *regexp.Regexp) int {
		m := re.FindStringSubmatch(text)
		if m == nil {
			return 0
		}
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
		return 2 // "Multiple"
	}
	s.VACBans, s.GameBans = count(vacBans), count(gameBans)
	if m := sinceBan.FindStringSubmatch(text); m != nil {
		s.DaysSinceBan, _ = strconv.Atoi(m[1])
	}
}
