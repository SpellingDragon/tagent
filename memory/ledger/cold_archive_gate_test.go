// The quiet point of a retirement pass at the engine: the copy, its verification and
// the rewrite of the hot journal run under the one store lock a commit also takes.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
package ledger

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestColdArchiveHoldsTheExclusiveGate pins the quiet point of the relocation pass at the engine.
//   - denominator: the pass settles a key set of its own snapshot and a writer commits beside it
//   - positive: every acknowledged write is held after the pass and after a reopen, byte for byte
//   - positive: the event count states the whole history the fixture and the writer committed
//   - positive: a fact committed before the pass answers its bytes to a reader running beside it
//   - negative: a gate opened between the snapshot and the rewrite erases the writes that landed there
//   - consumer: StoreEvent and GetEvent are the faces the assembly commits through; the reopen is OpenTier
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestColdArchiveHoldsTheExclusiveGate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fixture := commitSettledFacts(t, ctx, root, 4, 40)
	live := map[event.EventID]struct{}{fixture.live: {}}
	watched := fixture.keys[0]
	watchedBody, held, err := fixture.tier.Segments.GetEvent(ctx, watched)
	if err != nil || !held {
		t.Fatalf("read the fact the reader watches before the pass answered (%v, held=%v)", err, held)
	}

	const writes = 200
	written := make([]event.EventID, writes)
	writtenBody := make(map[event.EventID][]byte, writes)
	var writerErr error
	var readerErr error

	passDone := make(chan struct{})
	passReport := make(chan RelocateReport, 1)
	passErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		defer close(passDone)
		report, err := fixture.tier.Segments.RetireFacts(ctx, live)
		passErr <- err
		passReport <- report
	}()

	go func() {
		defer wg.Done()
		for at := 0; at < writes; at++ {
			key := event.EventID(fmt.Sprintf("gate-writer-%d", at))
			body := bytes.Repeat([]byte(fmt.Sprintf("gate-material-%d ", at)), 32)
			record := SegmentRecord{
				PartitionID: 2,
				WindowTS:    int64(at / 8),
				Seq:         int64(at%8) + 1,
				EventKey:    key,
				EventTime:   int64(at),
				Body:        body,
			}.WithChain("fact", "session-gate")
			if err := fixture.tier.Segments.StoreEvent(ctx, record); err != nil {
				writerErr = fmt.Errorf("commit %s beside the pass: %w", key, err)
				return
			}
			written[at] = key
			writtenBody[key] = body
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-passDone:
				return
			default:
			}
			stored, held, err := fixture.tier.Segments.GetEvent(ctx, watched)
			if err != nil {
				readerErr = fmt.Errorf("read the watched fact beside the pass: %w", err)
				return
			}
			if !held {
				readerErr = fmt.Errorf("the watched fact %s answered missing beside the pass, want the bytes it was committed with", watched)
				return
			}
			if !bytes.Equal(stored.Body, watchedBody.Body) {
				readerErr = fmt.Errorf("the watched fact %s answered %q beside the pass, want %q", watched, stored.Body, watchedBody.Body)
				return
			}
		}
	}()

	wg.Wait()
	if err := <-passErr; err != nil {
		t.Fatalf("relocate the settled facts beside a writer: %v", err)
	}
	report := <-passReport
	if writerErr != nil {
		t.Fatalf("%v", writerErr)
	}
	if readerErr != nil {
		t.Fatalf("%v", readerErr)
	}

	if report.Settled == 0 {
		t.Fatalf("the pass settled no key beside %d write(s), want the settled facts this fixture commits", writes)
	}
	wantCount := len(fixture.keys) + writes
	if count := fixture.tier.Segments.EventCount(); count != wantCount {
		t.Fatalf("the store states %d committed event(s) after the pass beside %d write(s), want the %d the fixture and the writer committed: a bounded hot journal must not read as a store that lost its history",
			count, writes, wantCount)
	}
	answers := func(tier *Tier, where string) {
		t.Helper()
		for _, key := range written {
			stored, held, err := tier.Segments.GetEvent(ctx, key)
			if err != nil {
				t.Fatalf("read the write %s %s: %v", key, where, err)
			}
			if !held {
				t.Fatalf("the write %s answers missing %s, want the bytes the store acknowledged: the pass erased a write that landed inside its window", key, where)
			}
			if want := writtenBody[key]; !bytes.Equal(stored.Body, want) {
				t.Fatalf("the write %s answers %q %s, want %q", key, stored.Body, where, want)
			}
		}
		if stored, held, err := tier.Segments.GetEvent(ctx, watched); err != nil || !held || !bytes.Equal(stored.Body, watchedBody.Body) {
			t.Fatalf("the moved fact %s answers (%v, held=%v) %s, want the bytes it was committed with", watched, err, held, where)
		}
	}
	answers(fixture.tier, "in the process that ran the pass")
	if err := fixture.tier.Segments.Close(); err != nil {
		t.Fatalf("close the journal after the pass: %v", err)
	}
	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the journal after the pass: %v", err)
	}
	defer reopened.Segments.Close()
	answers(reopened, "after the reopen")
	if count := reopened.Segments.EventCount(); count != wantCount {
		t.Fatalf("the reopened store states %d committed event(s), want the %d the fixture and the writer committed", count, wantCount)
	}
}
