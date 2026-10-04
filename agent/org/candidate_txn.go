// 本文件承载候选事务的有序责任表：先记后判错、按获取逆序撤销，顺序由实际获取证据决定。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
package org

import (
	"sync/atomic"

	"github.com/SpellingDragon/tagent/agent"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// Txn is the ordered responsibility table of ONE candidate (S-B/2.3「事务」):
// every resource acquisition, owner registration and agent construction is
// recorded IMMEDIATELY — before the error is checked, so a failed parent is
// still unwindable — and earlier than the next fallible action. Discard unwinds
// in REVERSE acquisition order (partial registration revoked → built agent
// Closed → fingerprint reset), and that order comes from the recorded evidence
// rather than any difference over owned names or map iteration order. Reload and
// rollback share this structure.
type Txn struct {
	dropFP          func(name string)
	unregisterOwner func(name string)
	order           []string
	agents          map[string]*agent.TagentAgent
}

// NewTxn builds an empty responsibility table. dropFP resets a name's memory
// fingerprint and unregisterOwner revokes its store-owner registration; both are
// supplied by the composition root, which owns the books they touch.
func NewTxn(dropFP func(name string), unregisterOwner func(name string)) *Txn {
	return &Txn{dropFP: dropFP, unregisterOwner: unregisterOwner, agents: map[string]*agent.TagentAgent{}}
}

// Acquire records a responsibility the candidate has taken on: a fully built
// agent (a != nil) or a partial owner registration whose build failed midway
// (a == nil — its store lease was already released by the builder's own cleanup,
// so discard only revokes the registration). Idempotent per name: a dependency
// acquired by an earlier top is not re-recorded.
func (tx *Txn) Acquire(name string, a *agent.TagentAgent) {
	if _, ok := tx.agents[name]; ok {
		return
	}
	tx.agents[name] = a
	tx.order = append(tx.order, name)
}

// Discard unwinds every responsibility in REVERSE acquisition order and returns
// that order (also recorded into the discard-order witness). Per item: the
// store-owner registration is revoked, the memory fingerprint is reset, and a
// built agent is Closed (its own Close releases the store lease; a partial
// registration has no lease left to release). Each entry revokes its
// registration whether or not an agent was built — a leftover entry could later
// refuse an unrelated agent that recycled the same heap address, reported as a
// partition collision. The published online face is never touched: discard only
// ever runs before the single commit point.
func (tx *Txn) Discard() []string {
	if tx == nil {
		return nil
	}
	order := make([]string, 0, len(tx.order))
	for i := len(tx.order) - 1; i >= 0; i-- {
		n := tx.order[i]
		order = append(order, n)
		a := tx.agents[n]
		if tx.dropFP != nil {
			tx.dropFP(n)
		}
		if tx.unregisterOwner != nil {
			tx.unregisterOwner(n)
		}
		if a != nil {
			if cerr := a.Close(); cerr != nil {
				log.Warnf("[org-hotreload] refused-candidate cleanup: close hot-added %q: %v", n, cerr)
			}
		}
		delete(tx.agents, n)
	}
	recordDiscardOrder(order)
	return order
}

// lastDiscardOrder is the TEST-only witness for the reverse-acquisition-order
// contract (same introspection family as agent.TagentAgentsConstructed): no
// production path reads it. Map-iteration cleanup order was UNOBSERVABLE —
// which is exactly why it survived review — so the contract needs a witness.
var lastDiscardOrder atomic.Value

func recordDiscardOrder(order []string) {
	if len(order) == 0 {
		return
	}
	lastDiscardOrder.Store(order)
}

// LastDiscardOrder returns the order a previous Discard unwound, or false when
// nothing has been discarded yet. It exists because the cleanup contract is
// otherwise unobservable; no production path reads it.
func LastDiscardOrder() ([]string, bool) {
	v, ok := lastDiscardOrder.Load().([]string)
	return v, ok
}
