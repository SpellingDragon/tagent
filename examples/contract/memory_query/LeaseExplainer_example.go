package memoryqueryexample

import (
	"time"

	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// leaseLedgerRead is the canonical execution-resource ledger read: it answers the retained
// state of one registration beside the counts the ledger keeps over its leases.
type leaseLedgerRead struct {
	retained map[state.LeaseRef]query.LeaseRetention
	counts   query.LeaseLedgerCounts
}

// ExplainLease returns the retained state of the resource registered under ref; a key the
// ledger holds nothing under answers as not held.
func (l *leaseLedgerRead) ExplainLease(ref state.LeaseRef) (query.LeaseRetention, error) {
	retained, ok := l.retained[ref]
	if !ok {
		return query.LeaseRetention{Ref: ref, Held: false}, nil
	}
	return retained, nil
}

// LeaseCounts returns the counting face of the ledger.
func (l *leaseLedgerRead) LeaseCounts() query.LeaseLedgerCounts { return l.counts }

var _ query.LeaseExplainer = (*leaseLedgerRead)(nil)

// ExampleLeaseExplainer explains one held lease beside the counts of the ledger, and
// reports a key the ledger holds nothing under as not held.
//
// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
func ExampleLeaseExplainer() {
	ref := state.LeaseRef{Kind: "tmux", Key: "session-1"}
	ledger := &leaseLedgerRead{
		retained: map[state.LeaseRef]query.LeaseRetention{
			ref: {
				Ref:      ref,
				Held:     true,
				Deadline: time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC),
				Renewed:  2,
			},
		},
		counts: query.LeaseLedgerCounts{Held: 1, Acquired: 1},
	}

	held, err := ledger.ExplainLease(ref)
	if err != nil {
		return
	}
	absent, err := ledger.ExplainLease(state.LeaseRef{Kind: "tmux", Key: "session-2"})
	if err != nil {
		return
	}
	if absent.Held {
		return
	}
	_ = held.Renewed
	_ = ledger.LeaseCounts().Held
}
