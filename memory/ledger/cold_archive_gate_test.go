// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestColdArchiveHoldsTheExclusiveGate pins the quiet point of the relocation
// pass at the engine (runtime-recovery-ideal 08-fact-retirement 6.2): the pass
// copies, verifies and rewrites the hot journal under the one store lock a commit
// also takes, so no write lands inside the rewrite and no reader observes a fact
// that is half moved.
//   - denominator: the pass settles a key set of its own snapshot — the report
//     states more than zero settled keys — and a writer commits beside it, so a
//     green below is a claim about a pass that ran beside writes and not about an
//     idle store
//   - positive: every write the store acknowledged is held after the pass and
//     after a reopen, byte for byte, and the event count states the whole history
//     the fixture and the writer committed
//   - positive (reader): a fact committed before the pass answers its bytes to a
//     reader running beside the pass, so the moved fact is never observed missing
//     while it is being moved
//   - negative: a pass that opens the gate between reading its snapshot and
//     rewriting the hot journal erases the writes that landed in that window — the
//     rewrite keeps its snapshot's keys alone — so the acknowledged write reads
//     back missing, which is the red this anchor turns on when the gate is removed
//   - consumer: the writer and the reader are the store's own StoreEvent and
//     GetEvent — the two faces the assembly commits and reads through — and the
//     reopen is the boot's own OpenTier over the same root
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

	// The pass: the store's own relocation, over the key set the live fold names.
	go func() {
		defer wg.Done()
		defer close(passDone)
		report, err := fixture.tier.Segments.RetireFacts(ctx, live)
		passErr <- err
		passReport <- report
	}()

	// The writer: commits of its own partition beside the pass, each one acknowledged
	// before the next, so every key it holds is a write the store said it kept.
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

	// The reader: a fact committed before the pass is never observed missing while
	// the pass moves it.
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

	// The denominator: the pass settled a key set of its own, and every write beside
	// it was acknowledged, so the assertions below judge a pass that ran beside work.
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
