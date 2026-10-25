package record

import "time"

// SetNow replaces the clock, so a test decides when a survey is.
func SetNow(f func() time.Time) { now = f }
