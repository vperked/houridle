// Package stoptime reads when houridle should stop idling by itself:
//
//	3, 1.5      after that many hours
//	90m, 2h30m  after that long
//	23:30       at that time of day (tomorrow, if it's already past)
//	never, ""   keep going until stopped by hand
package stoptime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Never is the zero time: no automatic stop.
var Never time.Time

const never = "never"

// Parse turns what the user typed into the time to stop, relative to now.
// It also accepts the exact form Format writes (used between houridle copies).
func Parse(input string, now time.Time) (time.Time, error) {
	if exact, err := time.Parse(time.RFC3339, strings.TrimSpace(input)); err == nil {
		return exact, nil // from Format
	}
	s := strings.ToLower(strings.TrimSpace(input))
	switch {
	case s == "" || s == never || s == "no" || s == "0":
		return Never, nil
	case strings.Contains(s, ":"):
		return atClockTime(s, now)
	}

	if hours, err := strconv.ParseFloat(s, 64); err == nil {
		return after(time.Duration(hours*float64(time.Hour)), now, input)
	}
	if d, err := time.ParseDuration(s); err == nil {
		return after(d, now, input)
	}
	return Never, fmt.Errorf("couldn't read %q: use hours (3), a duration (90m), or a time of day (23:30)", input)
}

// Format writes a stop time so another houridle copy can read it back exactly.
func Format(t time.Time) string {
	if t.IsZero() {
		return never
	}
	return t.Format(time.RFC3339)
}

// Describe says when t is, for people: "23:30", or "Tue 09:00" if not today.
func Describe(t, now time.Time) string {
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

func after(d time.Duration, now time.Time, input string) (time.Time, error) {
	if d <= 0 {
		return Never, fmt.Errorf("%q isn't in the future", input)
	}
	return now.Add(d).Truncate(time.Second), nil
}

// atClockTime is the next time the clock shows hh:mm.
func atClockTime(s string, now time.Time) (time.Time, error) {
	clock, err := time.Parse("15:04", s)
	if err != nil {
		return Never, errors.New("couldn't read the time " + strconv.Quote(s) + ": use 24-hour time like 23:30")
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at, nil
}

// ErrReached is the cause of a context ended by WithStop at the stop time.
var ErrReached = errors.New("reached the stop time")

// wallCheckEvery is how often WithStop also compares the wall clock with the
// stop time.
const wallCheckEvery = time.Minute

// WithStop returns a context that ends at the wall-clock time at, with cause
// ErrReached (see Reached). A plain deadline is a monotonic timer, which on
// Linux stands still while the computer sleeps, so after a suspend it would
// fire late by however long the sleep was; this also checks the wall clock
// every minute. A zero at never ends it.
func WithStop(parent context.Context, at time.Time) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	if !at.IsZero() {
		go watch(ctx, at, wallCheckEvery, func() { cancel(ErrReached) })
	}
	return ctx, func() { cancel(context.Canceled) }
}

// Reached reports whether ctx (from WithStop) ended because the stop time came.
func Reached(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), ErrReached)
}

// watch calls reached once the wall clock gets to at, checking when a timer
// set for at fires and every checkEvery in between.
func watch(ctx context.Context, at time.Time, checkEvery time.Duration, reached func()) {
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()
	for {
		left := time.Until(at)
		if left <= 0 {
			reached()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(time.Until(at)) // in case it fired early, or the clock was set back
		case <-ticker.C:
		}
	}
}
