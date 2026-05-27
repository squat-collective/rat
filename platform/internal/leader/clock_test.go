package leader

import (
	"sync"
	"time"
)

// fakeClock is a deterministic Clock for tests. It does not advance on its
// own — call Advance to move time forward and fire any tickers whose
// interval has elapsed.
//
// fakeClock is safe for concurrent use: the leader's heartbeat goroutine
// reads ticks from the fake's channel while the test calls Advance from
// the main goroutine.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

// newFakeClock returns a fakeClock pinned to a fixed reference time.
func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// Now returns the current fake time.
func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// NewTicker registers a fakeTicker that fires when Advance moves the clock
// past its next scheduled tick.
func (f *fakeClock) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("fakeClock.NewTicker: non-positive interval")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTicker{
		ch:       make(chan time.Time, 1), // buffer 1 mirrors time.NewTicker
		interval: d,
		next:     f.now.Add(d),
	}
	f.tickers = append(f.tickers, t)
	return t
}

// Sleep is a no-op for the fake. Real sleeps in tests would defeat the
// determinism the fake exists to provide.
func (f *fakeClock) Sleep(_ time.Duration) {}

// Advance moves the fake clock forward by d. For each registered (live)
// ticker whose next-tick time falls inside the new range, the ticker's
// channel is fired once for the most recent scheduled tick. (Like
// time.Ticker, the fake drops ticks if the consumer is slow — we don't
// flood the channel.)
//
// Advance blocks briefly to give the goroutine on the other end of the
// ticker channel a chance to observe the tick; this is the only place
// real time intrudes, and it's bounded.
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	now := f.now
	tickers := append([]*fakeTicker(nil), f.tickers...)
	f.mu.Unlock()

	for _, t := range tickers {
		t.maybeFire(now)
	}

	// Tiny yield so the heartbeat goroutine can pick up the tick before
	// the test's next assertion runs. This is the determinism trade-off:
	// we don't sleep waiting for time to pass, only for scheduler turnover.
	time.Sleep(1 * time.Millisecond)
}

// fakeTicker is a Ticker driven by fakeClock.Advance.
type fakeTicker struct {
	mu       sync.Mutex
	ch       chan time.Time
	interval time.Duration
	next     time.Time
	stopped  bool
}

// C returns the tick channel.
func (t *fakeTicker) C() <-chan time.Time { return t.ch }

// Stop marks the ticker as stopped; subsequent Advance calls won't fire it.
func (t *fakeTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
}

// maybeFire delivers a tick if now >= next and the ticker is live.
// If multiple intervals have elapsed, we collapse them into one tick
// (matching time.Ticker semantics when the consumer can't keep up).
func (t *fakeTicker) maybeFire(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return
	}
	if now.Before(t.next) {
		return
	}
	// Advance next past now so we don't refire on the same Advance.
	for !now.Before(t.next) {
		t.next = t.next.Add(t.interval)
	}
	select {
	case t.ch <- now:
	default:
		// Buffer full — consumer hasn't drained the previous tick.
		// Mirror time.Ticker behaviour: drop the new tick.
	}
}
