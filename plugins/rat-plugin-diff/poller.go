package main

// poller.go ticks every interval, captures a fresh snapshot, diffs it
// against the previous one, and appends derived events to the store
// (also persisting them to ratd's plugin config so they survive
// restarts).

import (
	"context"
	"log/slog"
	"time"
)

type poller struct {
	c        *ratdClient
	store    *store
	cfg      *configStore
	selfName string
	interval time.Duration
}

func newPoller(c *ratdClient, st *store, cfg *configStore, selfName string, interval time.Duration) *poller {
	return &poller{c: c, store: st, cfg: cfg, selfName: selfName, interval: interval}
}

func (p *poller) run(ctx context.Context) {
	// First tick: capture-only, no diff. We don't want a flood of fake
	// "registered" events on every plugin restart.
	{
		tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		snap, _ := capture(tickCtx, p.c, p.selfName)
		cancel()
		p.store.setLast(snap)
	}

	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		curr, currRaw := capture(tickCtx, p.c, p.selfName)
		cancel()

		prev := p.store.getLast()
		if prev == nil {
			p.store.setLast(curr)
			continue
		}
		// We don't preserve raw across ticks (only the lean snapshot is
		// persisted to ratd config). Re-fetch the current plugin configs
		// from currRaw for before/after on this tick; the differ falls
		// back to "null" for `before` when no prevRaw is available, which
		// is honest given we only kept the hash.
		evs := diffSnapshots(prev, curr, nil, currRaw)
		if len(evs) > 0 {
			all := p.store.appendEvents(evs)
			if err := p.cfg.persistEvents(context.Background(), all); err != nil {
				slog.Warn("persist events", "error", err)
			}
			slog.Info("emitted events", "count", len(evs))
		}
		p.store.setLast(curr)
	}
}
