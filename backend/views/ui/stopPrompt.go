package ui

import (
	"fmt"
	"time"

	"houridle/backend/run/stoptime"
)

// AskStopTime asks when houridle should stop idling by itself. Pressing
// Enter (or having no input) means keep going until stopped by hand.
func AskStopTime() time.Time {
	question := "\n  " + accent("◆") + " stop automatically? " +
		muted("hours (3), a time (23:30), or Enter to keep going: ")
	for {
		answer, err := readLine(question)
		if err != nil {
			return stoptime.Never // input closed: nobody to ask
		}
		at, err := stoptime.Parse(answer, time.Now())
		if err != nil {
			fmt.Println("  " + warning(err.Error()))
			continue
		}
		if !at.IsZero() {
			fmt.Println("  " + muted("will stop at "+stoptime.Describe(at, time.Now())))
		}
		return at
	}
}
