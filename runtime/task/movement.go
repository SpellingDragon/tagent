// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
//
// This file holds the private sequence a run's accounts are judged at: every
// account advances its own sequence whenever it is written, the judgment reads
// the accounts' sequences together, and the sequence belongs to the account
// that advances it, never to a mechanism wired beside it. The registry and the
// mailbox each build their own; the close only reads them.
//
// The sequence is one counter, never a lock: the accounts it fences are written
// under their own locks in their own order, so a counter moves no lock and makes
// no reader wait. A read spanning a write sees a sequence move and is re-read;
package task

import "sync/atomic"

// movement is the private sequence one account of a run advances at every write.
// It embeds the counter itself, so the account that owns it exposes only the
// counter's own Add and Load and the mechanism stays out of the public surface.
type movement struct {
	atomic.Uint64
}

// newMovement returns the sequence of one account at its first position.
func newMovement() *movement { return &movement{} }
