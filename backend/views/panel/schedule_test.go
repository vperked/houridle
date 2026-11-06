package panel

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"houridle/backend/run/config"
)

// nextChange only looks at window edges and midnights; it must agree with
// checking every minute.
func TestNextChangeMatchesAMinuteByMinuteScan(t *testing.T) {
	slow := func(s *config.Schedule, t time.Time) time.Time {
		in := InSchedule(s, t)
		t = t.Truncate(time.Minute)
		for i := 1; i <= 8*24*60; i++ {
			if u := t.Add(time.Duration(i) * time.Minute); InSchedule(s, u) != in {
				return u
			}
		}
		return time.Time{}
	}
	r := rand.New(rand.NewPCG(1, 2))
	base := time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local)
	for i := range 2000 {
		s := &config.Schedule{On: true, Start: fmt.Sprintf("%02d:%02d", r.IntN(24), r.IntN(4)*15), End: fmt.Sprintf("%02d:%02d", r.IntN(24), r.IntN(4)*15)}
		for d := range 7 {
			s.Days[d] = r.IntN(3) > 0
		}
		at := base.Add(time.Duration(r.IntN(7*24*60)) * time.Minute).Add(time.Duration(r.IntN(60)) * time.Second)
		if got, want := nextChange(s, at), slow(s, at); !got.Equal(want) {
			t.Fatalf("#%d %+v at %s: got %s, want %s", i, *s, at.Format("Mon 15:04:05"), got.Format("Mon 15:04"), want.Format("Mon 15:04"))
		}
	}
}
