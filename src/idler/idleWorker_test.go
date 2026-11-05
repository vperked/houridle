package idler

// These tests live in the package (unlike src/tests) because they drive the
// worker's retry loop with a fake Steam and fake helper processes.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"houridle/src/steam"
	"houridle/src/steamworks"
)

// Fake helper behaviours, chosen through the -dll argument.
const (
	fakeFail  = "fake-fail"  // prints FAIL notOwned and exits
	fakeCrash = "fake-crash" // prints READY and exits at once
	fakeIdle  = "fake-idle"  // prints READY and waits for stdin to close
)

// TestMain doubles as the fake helper: the supervisor re-runs this test
// binary as "<exe> -child <appid> -dll <behaviour>".
func TestMain(m *testing.M) {
	if len(os.Args) == 5 && os.Args[1] == "-"+HelperFlag {
		switch os.Args[4] {
		case fakeFail:
			fmt.Println(helperFailPrefix + steamworks.CodeNotOwned + " Steam won't run this app")
			os.Exit(1)
		case fakeCrash:
			fmt.Println(helperReady)
			os.Exit(0)
		case fakeIdle:
			fmt.Println(helperReady)
			io.Copy(io.Discard, os.Stdin)
			os.Exit(0)
		}
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func testExe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

// A helper that prints FAIL and exits must be reported with its own reason,
// even when its exit is noticed at the same moment as the FAIL line.
func TestHelperFailLineBeatsItsExit(t *testing.T) {
	exe := testExe(t)
	for range 20 {
		h, err := startHelper(exe, fakeFail, 730)
		if err != nil {
			t.Fatal(err)
		}
		<-h.exited // both the FAIL line and the exit are now ready
		err = h.waitReady(context.Background())
		var failure *helperFailure
		if !errors.As(err, &failure) || failure.code != steamworks.CodeNotOwned {
			t.Fatalf("got %v, want the helper's notOwned failure", err)
		}
	}
}

func TestCrashBackoffDoublesUpToTheCap(t *testing.T) {
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 5 * time.Minute, 5 * time.Minute}
	for i, w := range want {
		if got := crashBackoff(i + 1); got != w {
			t.Errorf("crashBackoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	if got := crashBackoff(1000); got != crashBackoffMax {
		t.Errorf("crashBackoff(1000) = %v, want the cap", got)
	}
}

// A helper that keeps dying must back off and stop announcing itself; one
// that stayed up a long time before dying starts over.
func TestHelperCrashLoopBacksOffQuietly(t *testing.T) {
	var events []Event
	sup := New([]*steam.Game{{AppID: 730, Name: "CS"}}, "", "", func(e Event) { events = append(events, e) })
	w := sup.workers[0]

	var delays []time.Duration
	for range 6 {
		delays = append(delays, w.helperCrashed(time.Second))
		w.markIdling(1)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] && delays[i] != crashBackoffMax {
			t.Fatalf("restart delays don't grow: %v", delays)
		}
	}
	// Announced: crash 1, its "is idling", crash 2. Then nothing.
	if len(events) != 3 {
		t.Fatalf("a crash loop produced %d events, want 3: %v", len(events), events)
	}
	if d := w.helperCrashed(stableAfter); d != crashBackoffMin {
		t.Fatalf("after a long stable run the backoff should reset, got %v", d)
	}
}

// fakeSteam is a Steam PID that tests can change.
type fakeSteam struct{ pid atomic.Uint32 }

func (f *fakeSteam) install(t *testing.T, pid uint32) {
	f.pid.Store(pid)
	oldPID, oldPoll := steamPID, steamPollEvery
	steamPID = f.pid.Load
	steamPollEvery = 20 * time.Millisecond
	t.Cleanup(func() { steamPID, steamPollEvery = oldPID, oldPoll })
}

// eventLog collects events safely from the workers' goroutines.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e.Text)
}

func (l *eventLog) count(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if strings.Contains(e, substr) {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func allIn(sup *Supervisor, state State) func() bool {
	return func() bool {
		for _, v := range sup.Views() {
			if v.State != state {
				return false
			}
		}
		return true
	}
}

// Steam closing while idling stops the helpers and is announced once; Steam
// restarting (a new PID) moves every game over to it, also announced once.
func TestSteamClosingAndRestartingIsNoticed(t *testing.T) {
	var steamProc fakeSteam
	steamProc.install(t, 100)
	games := []*steam.Game{{AppID: 1, Name: "A"}, {AppID: 2, Name: "B"}, {AppID: 3, Name: "C"}}
	var log eventLog
	sup := New(games, testExe(t), fakeIdle, log.add)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		sup.Wait()
	}()
	sup.Start(ctx)

	waitFor(t, "games to idle", allIn(sup, StateIdling))
	steamProc.pid.Store(0)
	waitFor(t, "games to wait for Steam", allIn(sup, StateWaiting))
	if n := log.count("Steam closed"); n != 1 {
		t.Fatalf(`"Steam closed" announced %d times, want 1`, n)
	}

	steamProc.pid.Store(200)
	waitFor(t, "games to idle again", allIn(sup, StateIdling))
	before := log.count("is idling")
	steamProc.pid.Store(300)
	waitFor(t, "the restart to be noticed", func() bool { return log.count("Steam restarted") > 0 })
	waitFor(t, "games to idle under the new Steam", func() bool {
		return allIn(sup, StateIdling)() && log.count("is idling") >= before+len(games)
	})
	if n := log.count("Steam restarted"); n != 1 {
		t.Fatalf(`"Steam restarted" announced %d times, want 1`, n)
	}
}
