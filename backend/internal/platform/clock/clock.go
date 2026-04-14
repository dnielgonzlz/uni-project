package clock

import "time"

// Clock is an interface for getting the current time.
// It allows service-layer code to be tested with a fixed time.
type Clock interface {
	Now() time.Time
}

// Real implements Clock using the system clock.
type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Fixed implements Clock with a fixed time, for use in tests.
type Fixed struct {
	T time.Time
}

func (f Fixed) Now() time.Time { return f.T }
