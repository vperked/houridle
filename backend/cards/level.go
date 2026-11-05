package cards

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// Level is the account's Steam level and XP, from the badge page.
type Level struct {
	Level   int
	XP      int // all the XP the account has earned
	LevelXP int // XP the current level started at
	NextXP  int // XP the next level starts at
}

var (
	levelNum = regexp.MustCompile(`friendPlayerLevelNum">(\d+)<`)
	levelXP  = regexp.MustCompile(`profile_xp_block_xp">\s*XP\s*([\d,]+)`)
)

// XPForLevel is the XP a Steam level starts at: each level costs 100 XP,
// plus 100 more for every ten levels already reached.
func XPForLevel(level int) int {
	xp := 0
	for l := 0; l < level; l++ {
		xp += 100 * (l/10 + 1)
	}
	return xp
}

// ParseLevel reads the level and XP from a badge page.
func ParseLevel(page string) (Level, bool) {
	m := levelNum.FindStringSubmatch(page)
	x := levelXP.FindStringSubmatch(page)
	if m == nil || x == nil {
		return Level{}, false
	}
	l, _ := strconv.Atoi(m[1])
	xp, _ := strconv.Atoi(strings.ReplaceAll(x[1], ",", ""))
	return Level{Level: l, XP: xp, LevelXP: XPForLevel(l), NextXP: XPForLevel(l + 1)}, true
}

// Overview reads every badge page once: the badges, the games with card
// drops left, and the account's level and XP.
func Overview(ctx context.Context, steamID uint64, accessToken string) ([]Badge, []Drop, Level, error) {
	var badges []Badge
	var drops []Drop
	var lvl Level
	first := true
	err := eachBadgePage(ctx, steamID, accessToken, func(page string) {
		if first {
			lvl, _ = ParseLevel(page)
			first = false
		}
		badges = append(badges, ParseBadges(page)...)
		drops = append(drops, Parse(page)...)
	})
	return badges, drops, lvl, err
}
