// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
//
// This file holds the one sequence a run is judged at: every account the rest
// criterion reads advances it whenever it is written, so an evaluation that
// spans the task chain, the delivery obligations, the mailbox and the session
// chains belongs to one instant of all of them.
//
// The sequence is one counter, never a lock: the accounts it fences are written
// under their own locks and in their own order, so a judgment that held them all
// at once would order two of those locks against each other. A counter moves no
// lock and makes no reader wait for a writer: a read that spans a write sees the
// sequence move and is re-read, and a read that never holds is answered unjudged
// rather than as a snapshot of instants that never stood together.
package task

import "sync"

// Movement is the one sequence the rest criterion is fenced by: the accounts of
// a run advance it whenever they are written, and the criterion reads it before
// and after the sets it unions. Two equal reads name the one instant those sets
// belong to; a sequence that moved under a read names a judgment no set can be
// reported at.
//
// The redline list (3 rules):
// MUST be advanced by every writer of a set the rest criterion reads, at the write point itself.
// MUST NOT be advanced by a reader: a read moves no account and so moves no sequence.
// MUST NOT stand for an account of its own — it names movement, never the content a set answers with.
type Movement struct {
	mu  sync.Mutex
	seq uint64
}

// NewMovement returns the sequence of one run at its first position.
func NewMovement() *Movement { return &Movement{} }

// Advance records one write of an account the rest criterion reads. It is called
// at the write point, beside the account that moved, so a reader that reads the
// sequence before and after the sets sees every write standing between them.
func (m *Movement) Advance() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.seq++
	m.mu.Unlock()
}

// Read reports the position the accounts of this run stand at. A sequence
// attached to no run reads zero, which every read of one instant equals: an
// account that keeps no sequence is judged by its own snapshot alone.
func (m *Movement) Read() uint64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seq
}
