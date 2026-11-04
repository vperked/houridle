package stoptime

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseHours reads a length of time: "100" (hours), "100h", "90m" or
// "1h30m". It's how -until and -rotate are given.
func ParseHours(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if h, err := strconv.ParseFloat(s, 64); err == nil {
		if h <= 0 || h > 1e6 {
			return 0, fmt.Errorf("%q should be a number of hours above 0", s)
		}
		return time.Duration(h * float64(time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%q isn't a length of time: use hours (100), or 100h, 90m", s)
	}
	return d, nil
}
