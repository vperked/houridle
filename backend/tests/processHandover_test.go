package tests

import (
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"houridle/backend/handover"
	"houridle/backend/stoptime"
)

// The handover command line must survive Go's flag parsing, which stops at
// the first plain argument.
func TestHandoverArgsParse(t *testing.T) {
	for _, toTray := range []bool{true, false} {
		stopAt := time.Now().Add(3 * time.Hour).Truncate(time.Second)
		args := handover.Args(toTray, `C:\Program Files\houridle\steam_api64.dll`, stopAt, []uint32{252490, 730})
		fs := flag.NewFlagSet("houridle", flag.ContinueOnError)
		tray := fs.Bool("tray", false, "")
		dll := fs.String("dll", "", "")
		stop := fs.String("stop", "", "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		gotStop, err := stoptime.Parse(*stop, time.Now())
		if *tray != toTray || !strings.HasSuffix(*dll, "steam_api64.dll") || fmt.Sprint(fs.Args()) != "[252490 730]" || err != nil || !gotStop.Equal(stopAt) {
			t.Fatalf("args %q parsed to tray=%v dll=%q stop=%v ids=%v", args, *tray, *dll, gotStop, fs.Args())
		}
	}
}

func TestIdleLockIsExclusive(t *testing.T) {
	first, err := handover.TakeLock()
	if err != nil {
		t.Skipf("houridle is already running on this machine: %v", err)
	}
	defer first.Release()

	done := make(chan error, 1)
	go func() {
		second, err := handover.TakeLock() // waits while the first holds it
		if err == nil {
			second.Release()
		}
		done <- err
	}()
	first.Release()
	if err := <-done; err != nil {
		t.Fatalf("a waiting copy should get the lock once it's released: %v", err)
	}
}
