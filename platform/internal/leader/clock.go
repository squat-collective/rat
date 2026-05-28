package leader

import "time"

// Clock abstracts time for testability. Production code uses systemClock
// (real time); tests inject fakeClock (manually advanced via Advance).
//
// All time-sensitive code paths in the leader package go through this
// interface so that tests can drive the heartbeat and election tickers
// deterministically without relying on time.Sleep margins.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// NewTicker returns a Ticker that fires its channel at the given interval.
	// Like time.NewTicker, panics if d <= 0.
	NewTicker(d time.Duration) Ticker

	// Sleep blocks for the given duration.
	Sleep(d time.Duration)
}

// Ticker mirrors time.Ticker but exposes C() as a method so fakes can
// implement it (channel fields can't satisfy an interface).
type Ticker interface {
	// C returns the channel on which ticks are delivered.
	C() <-chan time.Time

	// Stop halts the ticker. After Stop, no more ticks will be sent.
	Stop()
}

// systemClock is the production Clock implementation backed by stdlib time.
type systemClock struct{}

// Now returns time.Now().
func (systemClock) Now() time.Time { return time.Now() }

// NewTicker wraps time.NewTicker.
func (systemClock) NewTicker(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

// Sleep wraps time.Sleep.
func (systemClock) Sleep(d time.Duration) { time.Sleep(d) }

// realTicker adapts time.Ticker to the Ticker interface.
type realTicker struct{ t *time.Ticker }

func (r *realTicker) C() <-chan time.Time { return r.t.C }
func (r *realTicker) Stop()               { r.t.Stop() }
