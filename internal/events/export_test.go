package events

import "time"

// SetNow replaces the clock, so a test decides which day a survey falls on.
func SetNow(f func() time.Time) { now = f }
