package jobs

import "time"

// clockFunc adapts a function to the Clock port.
type clockFunc func() time.Time

// Now implements Clock.
func (f clockFunc) Now() time.Time { return f() }

// NewClockFunc builds a Clock from a function.
func NewClockFunc(now func() time.Time) Clock { return clockFunc(now) }
