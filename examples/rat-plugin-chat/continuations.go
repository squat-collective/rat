package main

// continuationStore lets a long-running chatTurn pause when it hits its
// per-agent max_iterations cap and wait for the user to confirm. The
// orchestrator emits a `continuation_prompt` SSE event and then calls
// wait(); the UI shows a banner with two buttons; clicking "Continue"
// hits POST /conversations/{id}/continue which calls signal() and the
// loop resumes. If nothing happens within `timeout` (typically 60s),
// the wait returns successfully — auto-yes.
//
// Keyed by conversation id. Only one continuation pending per
// conversation at a time — racing concurrent chats on the same
// conversation isn't supported, and the second one would just bypass
// the prompt (existing channel slot, no waiter). That's acceptable.

import (
	"context"
	"sync"
	"time"
)

type continuationStore struct {
	mu    sync.Mutex
	chans map[string]chan struct{} // conv_id → continue signal
}

func newContinuationStore() *continuationStore {
	return &continuationStore{chans: map[string]chan struct{}{}}
}

// wait blocks until the user signals continue, the timeout expires
// (auto-yes), or ctx is canceled. Returns true if we should continue,
// false if the user/client gave up.
func (s *continuationStore) wait(ctx context.Context, convID string, timeout time.Duration) bool {
	if convID == "" {
		// No id to key on — auto-continue immediately rather than
		// blocking forever with no way for the UI to signal.
		return true
	}
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.chans[convID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		// Only delete if it's still the same channel — a duplicate
		// continue could theoretically have swapped it.
		if cur, ok := s.chans[convID]; ok && cur == ch {
			delete(s.chans, convID)
		}
		s.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return false
	case <-ch:
		return true
	case <-time.After(timeout):
		return true
	}
}

// signal poles a waiting goroutine. Returns true if a goroutine was
// actually waiting (so the caller can 200 vs 404).
func (s *continuationStore) signal(convID string) bool {
	s.mu.Lock()
	ch, ok := s.chans[convID]
	s.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- struct{}{}:
		return true
	default:
		// Already signaled — still treat as success.
		return true
	}
}
