// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package assembly

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// compressAccount is the state a compression consumer derives from the commits it
// observes: the judgments of every session chain in filing order, the identities
// a product answers, and the judgments neither answers — the batch still owed.
type compressAccount struct {
	Judgments map[event.SessionID][]event.EventID
	Answered  map[event.EventID]bool
	Owed      map[event.SessionID][]event.EventID
}

// compressAccountOf reads the account one consumer holds.
func compressAccountOf(t *testing.T, consumer *CompressionConsumer) compressAccount {
	t.Helper()
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	account := compressAccount{
		Judgments: make(map[event.SessionID][]event.EventID, len(consumer.judgments)),
		Answered:  make(map[event.EventID]bool, len(consumer.answered)),
		Owed:      map[event.SessionID][]event.EventID{},
	}
	for session, filed := range consumer.judgments {
		keys := make([]event.EventID, 0, len(filed))
		for _, request := range filed {
			keys = append(keys, request.ID)
		}
		account.Judgments[session] = keys
	}
	for request := range consumer.answered {
		account.Answered[request] = true
	}
	for session, filed := range account.Judgments {
		owed := make([]event.EventID, 0, len(filed))
		for _, request := range filed {
			if !account.Answered[request] {
				owed = append(owed, request)
			}
		}
		if len(owed) > 0 {
			account.Owed[session] = owed
		}
	}
	return account
}

// judgedCount reports how many judgments one account holds.
func (a compressAccount) judgedCount() int {
	count := 0
	for _, keys := range a.Judgments {
		count += len(keys)
	}
	return count
}

// requireSameAccount reports whether two rebuilds answer one account: the same
// judgments session by session, the same identities answered and the same batch
// still owed. The comparison is the parity a restart owes, never a count alone: a
// rebuild answering the right number of other judgments is no rebuild.
func requireSameAccount(t *testing.T, name string, got, want compressAccount) {
	t.Helper()
	if len(got.Judgments) != len(want.Judgments) {
		t.Fatalf("the %s rebuild holds the judgments of %d chain(s), want the %d the live process holds",
			name, len(got.Judgments), len(want.Judgments))
	}
	for session, filed := range want.Judgments {
		if !slices.Equal(got.Judgments[session], filed) {
			t.Fatalf("the %s rebuild holds %d judgment(s) of session %s, want the %d the live process holds",
				name, len(got.Judgments[session]), session, len(filed))
		}
		if !slices.Equal(got.Owed[session], want.Owed[session]) {
			t.Fatalf("the %s rebuild owes %d judgment(s) of session %s, want the %d the live process owes",
				name, len(got.Owed[session]), session, len(want.Owed[session]))
		}
	}
	if len(got.Answered) != len(want.Answered) {
		t.Fatalf("the %s rebuild answers %d judgment(s), want the %d the live process answers",
			name, len(got.Answered), len(want.Answered))
	}
	for request := range want.Answered {
		if !got.Answered[request] {
			t.Fatalf("the %s rebuild leaves the judgment %s unanswered beside the product that answers it", name, request)
		}
	}
}

// statedCompressAccount reads the compression account one checkpoint states back.
func statedCompressAccount(t *testing.T, checkpoint event.Checkpoint) compressAccount {
	t.Helper()
	for _, slice := range checkpoint.Slices {
		if slice.Name != checkpointSliceCompress {
			continue
		}
		var wire compressSliceWire
		if err := json.Unmarshal([]byte(slice.Payload), &wire); err != nil {
			t.Fatalf("the compression slice states no readable payload: %v", err)
		}
		account := compressAccount{
			Judgments: make(map[event.SessionID][]event.EventID, len(wire.Sessions)),
			Answered:  make(map[event.EventID]bool, len(wire.Answered)),
			Owed:      map[event.SessionID][]event.EventID{},
		}
		for _, session := range wire.Sessions {
			keys := make([]event.EventID, 0, len(session.Judgments))
			for _, judgment := range session.Judgments {
				keys = append(keys, judgment.ID)
			}
			account.Judgments[event.SessionID(session.Session)] = keys
		}
		for i := range wire.Answered {
			account.Answered[wire.Answered[i]] = true
		}
		for session, filed := range account.Judgments {
			owed := make([]event.EventID, 0, len(filed))
			for _, request := range filed {
				if !account.Answered[request] {
					owed = append(owed, request)
				}
			}
			if len(owed) > 0 {
				account.Owed[session] = owed
			}
		}
		return account
	}
	t.Fatal("the checkpoint states no compression slice")
	return compressAccount{}
}

// TestCompressionJudgmentsSurviveTheBootCut pins the compression state slice.
//   - the checkpoint states the judgments the live consumer holds and the identities its products answer
//   - the boot answers those judgments without reading the covered facts back, so the account survives the cut
//   - the booted, the anchored and the full walk answer one account
//   - a boot beside a checkpoint stating no compression account answers the batch past the cut alone (negative: the parity is the checkpoint's, not a replay's)
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionJudgmentsSurviveTheBootCut(t *testing.T) {
	ctx := context.Background()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-compress-cut"}
	source := windowChainRun(t, ctx, storage)
	commitWindowTurns(t, ctx, source, addr, 12)
	products := countFoldProducts(t, ctx, source)
	if products == 0 {
		t.Fatal("the fixture folded nothing, so the judgment parity proves nothing")
	}
	live := compressAccountOf(t, source.compress)
	if live.judgedCount() != products || len(live.Answered) != products {
		t.Fatalf("the live consumer judged %d and answered %d judgment(s), want the %d products the chain holds",
			live.judgedCount(), len(live.Answered), products)
	}

	checkpointID, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the checkpoint of the chain = (%t, %v), want a filed fact", filed, err)
	}
	stored, err := source.CommittedEvent(ctx, checkpointID)
	if err != nil {
		t.Fatalf("read the checkpoint back: %v", err)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the checkpoint %s states no readable checkpoint", checkpointID)
	}
	if names := checkpoint.CheckpointSliceNames(); !slices.Contains(names, checkpointSliceCompress) {
		t.Fatalf("the checkpoint states the slices %v, want the compression account beside them", names)
	}
	requireSameAccount(t, "checkpoint", statedCompressAccount(t, checkpoint), live)
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	booted := windowChainRun(t, ctx, storage)
	requireSameAccount(t, "booted", compressAccountOf(t, booted.compress), live)
	covered := 0
	for request := range live.Answered {
		booted.mu.Lock()
		_, read := booted.facts[request]
		booted.mu.Unlock()
		if !read {
			covered++
		}
	}
	if covered == 0 {
		t.Fatal("the boot read every judgment it answers, so the checkpoint carried none of them")
	}
	t.Logf("the boot answers %d judgment(s), %d of them beside no judgment fact in its ledger", len(live.Answered), covered)

	full := windowChainRun(t, ctx, storage)
	resetCompression(full)
	if _, _, _, err := full.seedFullReplay(ctx, full.tier); err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	requireSameAccount(t, "full walk", compressAccountOf(t, full.compress), live)

	stripped := commitCheckpointWithoutTheCompressionSlice(t, ctx, booted, checkpoint)
	bare := windowChainRun(t, ctx, storage)
	if restored, _ := bare.CheckpointFiled(); restored != stripped {
		t.Fatalf("the boot restored the checkpoint %s, want the stripped checkpoint %s", restored, stripped)
	}
	bareAccount := compressAccountOf(t, bare.compress)
	if want := len(live.Answered) - covered; len(bareAccount.Answered) != want {
		t.Fatalf("the boot beside a checkpoint stating no compression account answers %d judgment(s), want the %d past the cut beside the %d the live process answers",
			len(bareAccount.Answered), want, len(live.Answered))
	}
}

// commitCheckpointWithoutTheCompressionSlice files the checkpoint the given one states
// beside the projection alone: the state of every other slice is the one the
// checkpoint states, and the compression account is the one it never wrote.
func commitCheckpointWithoutTheCompressionSlice(t *testing.T, ctx context.Context, run *Run, checkpoint event.Checkpoint) event.EventID {
	t.Helper()
	kept := make([]event.CheckpointSlice, 0, len(checkpoint.Slices))
	for _, slice := range checkpoint.Slices {
		if slice.Name != checkpointSliceCompress {
			kept = append(kept, slice)
		}
	}
	if len(kept) == len(checkpoint.Slices) {
		t.Fatal("the checkpoint states no compression slice, so filing it stripped proves nothing")
	}
	stripped, err := event.CheckpointFor(checkpoint.Offset, kept)
	if err != nil {
		t.Fatalf("state the stripped checkpoint: %v", err)
	}
	payload, err := event.CheckpointPayloadOf(stripped)
	if err != nil {
		t.Fatalf("render the stripped checkpoint: %v", err)
	}
	run.mu.Lock()
	owner := run.checkpointOwnerLocked()
	run.mu.Unlock()
	partition := memory.PartitionIDFromName(string(owner.AgentID))
	run.landing.Lock()
	key := event.EventID(event.CheckpointKey(run.landedSeq[partition] + 1))
	run.landing.Unlock()
	if _, err := run.commitFactChecked(ctx, event.Event{
		Key:       key,
		Type:      event.TypeCheckpoint,
		Timestamp: time.Now().UTC(),
		Owner:     event.Address{AgentID: owner.AgentID},
		Meta:      stripped.MetaEntries(),
		Message:   model.Message{Content: string(payload)},
	}); err != nil {
		t.Fatalf("file the stripped checkpoint %s: %v", key, err)
	}
	return key
}
