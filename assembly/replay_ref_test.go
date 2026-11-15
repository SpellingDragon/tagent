// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
package assembly

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestReplayRefNamesAMissingBody pins the answer of the anchored rebuild's
// per-slot walk when the store it walks cannot answer one body.
//   - a fact the walk names but the store answers no body for is refused with
//     ErrSegmentBodyMissing carrying that key, never returned as an empty product
//     beside a nil error
//   - the refusal is the same named error the sibling recovery reads answer
//     (eachCommittedEvent, eachFactPastCut, EachEvent), so the two paths of one
//     store cannot disagree about one missing body
//
// The branch the walk takes is the one a derived index publishes fewer entries
// than the commit order names: today the load publishes every entry, so the walk
// reaches it only through a store that answers no body for the named fact, which
// is what this test drives directly.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestReplayRefNamesAMissingBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	missing := event.EventID("session-replay-ref-missing-fact")
	report := seedReport{}
	seed := &sessionSeed{}
	product, stated, agent, err := run.replayRef(ctx, run.tier, missing, seed, &report)
	if err == nil {
		t.Fatalf("the walk answered the missing fact %s as (%+v, %t, %s, nil), want the missing body refused by name",
			missing, product, stated, agent)
	}
	if !errors.Is(err, ledger.ErrSegmentBodyMissing) {
		t.Fatalf("the walk refused the missing fact with %v, want ErrSegmentBodyMissing", err)
	}
	if !strings.Contains(err.Error(), string(missing)) {
		t.Fatalf("the refusal = %v, want the key %s named", err, missing)
	}
	if report.Decoded != 0 {
		t.Fatalf("the walk decoded %d fact(s) of a body it never read, want none counted", report.Decoded)
	}
}
