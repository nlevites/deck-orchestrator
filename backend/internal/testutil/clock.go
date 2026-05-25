package testutil

import (
	"sync/atomic"
	"time"
)

// FakeClock is a goroutine-safe deterministic clock for tests.
type FakeClock struct {
	nanos atomic.Int64
}

func NewClock(start time.Time) *FakeClock {
	c := &FakeClock{}
	c.nanos.Store(start.UnixNano())
	return c
}

func (c *FakeClock) Now() time.Time {
	return time.Unix(0, c.nanos.Load()).UTC()
}

func (c *FakeClock) Advance(d time.Duration) {
	c.nanos.Add(int64(d))
}

func (c *FakeClock) Set(t time.Time) {
	c.nanos.Store(t.UnixNano())
}
