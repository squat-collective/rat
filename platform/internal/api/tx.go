package api

import "context"

// TxStores bundles tx-scoped stores that a multi-step handler operates on
// inside a single database transaction. Methods on these stores commit or
// roll back together via the surrounding TxRunner.InTx call.
//
// Add a field here when a new store needs to participate in a tx. Stores
// without a field cannot be used inside the callback — call them outside
// the tx instead.
type TxStores struct {
	Runs      RunStore
	Triggers  PipelineTriggerStore
	Schedules ScheduleStore
}

// TxRunner executes a callback inside a single database transaction.
//
// Contract for fn:
//   - MUST only perform DB work via the supplied TxStores.
//   - MUST NOT perform HTTP, gRPC, file IO, sleeps, or any other blocking
//     remote operation. The tx holds a pooled DB connection for its whole
//     duration; mixing IO inside starves the pool and can deadlock under
//     load. Do the IO outside the tx (before or after).
//   - Any error returned (or a panic) rolls back the tx. Returning nil
//     commits.
//
// Implementations live in the postgres package (real pgx tx) and in tests
// (in-memory pass-through; sequential execution with no real isolation).
type TxRunner interface {
	InTx(ctx context.Context, fn func(TxStores) error) error
}

// runFireTx runs a "fire a trigger" multi-step DB mutation atomically when
// the Server has a TxRunner wired (production); falls back to sequential
// calls against the Server's own stores when not (unit tests).
//
// The fallback is intentional — the in-memory stores used in unit tests do
// not implement true transactions, and the test surface area is large
// enough that synthesising a TxRunner for every test fixture is more
// disruptive than the lack of atomicity in tests. Integration tests against
// a real Postgres exercise the InTx path explicitly.
func (s *Server) runFireTx(ctx context.Context, fn func(TxStores) error) error {
	if s.TxRunner != nil {
		return s.TxRunner.InTx(ctx, fn)
	}
	return fn(TxStores{Runs: s.Runs, Triggers: s.Triggers, Schedules: s.Schedules})
}
