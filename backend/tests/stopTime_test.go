package tests

import (
	"context"
	"testing"
	"time"

	"houridle/backend/run/stoptime"
)

func TestStopTimeParse(t *testing.T) {
	now := time.Date(2026, 11, 3, 17, 0, 0, 0, time.Local)
	cases := []struct {
		input string
		want  time.Time
	}{
		{"", stoptime.Never},
		{"never", stoptime.Never},
		{"3", now.Add(3 * time.Hour)},
		{"1.5", now.Add(90 * time.Minute)},
		{"90m", now.Add(90 * time.Minute)},
		{" 2h30m ", now.Add(150 * time.Minute)},
		{"23:30", time.Date(2026, 11, 3, 23, 30, 0, 0, time.Local)},
		{"09:00", time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)},  // already past today: tomorrow
		{"17:00", time.Date(2026, 11, 4, 17, 0, 0, 0, time.Local)}, // right now counts as past
	}
	for _, c := range cases {
		got, err := stoptime.Parse(c.input, now)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("Parse(%q) = %v, %v; want %v", c.input, got, err, c.want)
		}
	}
}

func TestStopTimeRejectsNonsense(t *testing.T) {
	now := time.Now()
	for _, bad := range []string{"soon", "-2", "-30m", "25:00", "7pm"} {
		if _, err := stoptime.Parse(bad, now); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

// Format and Parse must round-trip exactly: it's how a stop time survives
// moving between the window and the tray.
func TestStopTimeRoundTrip(t *testing.T) {
	now := time.Now()
	for _, at := range []time.Time{stoptime.Never, now.Add(2 * time.Hour).Truncate(time.Second)} {
		got, err := stoptime.Parse(stoptime.Format(at), now)
		if err != nil || !got.Equal(at) {
			t.Errorf("round trip of %v gave %v, %v", at, got, err)
		}
	}
}

func TestStopTimeDescribe(t *testing.T) {
	now := time.Date(2026, 11, 3, 17, 0, 0, 0, time.Local)
	if got := stoptime.Describe(now.Add(time.Hour), now); got != "18:00" {
		t.Errorf("later today = %q", got)
	}
	if got := stoptime.Describe(now.Add(20*time.Hour), now); got != "Wed 13:00" {
		t.Errorf("tomorrow = %q", got)
	}
}

func TestStopContextEndsAtTheStopTime(t *testing.T) {
	ctx, cancel := stoptime.WithStop(context.Background(), time.Now().Add(50*time.Millisecond))
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the stop time passed but the context didn't end")
	}
	if !stoptime.Reached(ctx) {
		t.Fatalf("ended with %v, not the stop time", context.Cause(ctx))
	}
}

func TestStopContextCancelledIsNotTheStopTime(t *testing.T) {
	ctx, cancel := stoptime.WithStop(context.Background(), time.Now().Add(time.Hour))
	cancel()
	<-ctx.Done()
	if stoptime.Reached(ctx) {
		t.Fatal("stopping by hand was taken for the stop time")
	}

	ctx, cancel = stoptime.WithStop(context.Background(), stoptime.Never)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("no stop time should mean the context never ends by itself")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStopContextPastTimeEndsAtOnce(t *testing.T) {
	ctx, cancel := stoptime.WithStop(context.Background(), time.Now().Add(-time.Minute))
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a past stop time should end the context at once")
	}
	if !stoptime.Reached(ctx) {
		t.Fatal("should count as the stop time")
	}
}
