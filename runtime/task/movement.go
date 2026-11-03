// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
//
// This file holds the private sequence a run's accounts are judged at: every
// account the rest criterion reads advances its own sequence whenever it is
// written, and the judgment reads the accounts' sequences together.
//
// The sequence is one counter, never a lock: the accounts it fences are written
// under their own locks and in their own order, so a judgment that held them all
// at once would order two of those locks against each other. A counter moves no
// lock and makes no reader wait for a writer: a read that spans a write sees a
// sequence move and is re-read, and a read that never holds is answered unjudged
// rather than as a snapshot of instants that never stood together.
//
// The sequence belongs to the account that advances it, never to a mechanism
// wired beside it: the task registry and the mailbox each build their own, and
// the close only reads them.
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
