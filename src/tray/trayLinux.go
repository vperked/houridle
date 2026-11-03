//go:build linux

package tray

import "time"

// Run has nothing to show on Linux, where there's no tray. houridle refuses
// -tray before getting here (see handover.TraySupported).
func Run(ViewSource, time.Time, time.Time, <-chan struct{}) Choice { return ChoiceStop }
