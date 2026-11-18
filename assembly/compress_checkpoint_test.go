// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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

// requireSameAccount reports whether two rebuilds owe one batch: the same owed judgments session by session, and no judgment a product answers among them.
//   - The batch is compared instead of the whole judged set: a judgment a product answers stays on the chain and is owed by no boot, so agreeing over the answered identities would be restating settled work.
//   - The comparison is the parity a restart owes, never a count alone: a rebuild owing the right number of other judgments is no rebuild.
func requireSameAccount(t *testing.T, name string, got, want compressAccount) {
	t.Helper()
	if len(got.Owed) != len(want.Owed) {
		t.Fatalf("the %s rebuild owes the judgments of %d chain(s), want the %d the live process owes",
			name, len(got.Owed), len(want.Owed))
	}
	for session, filed := range want.Owed {
		if !slices.Equal(got.Owed[session], filed) {
			t.Fatalf("the %s rebuild owes %d judgment(s) of session %s, want the %d the live process owes",
				name, len(got.Owed[session]), session, len(filed))
		}
		for _, request := range filed {
			if want.Answered[request] {
				t.Fatalf("the %s rebuild owes the judgment %s of session %s beside the product that answers it, want an answered judgment owed by no rebuild", name, request, session)
			}
		}
	}
}

// statedCompressAccount reads the compression account one checkpoint states back:
// the batch every chain it names still owes.
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
			Answered:  map[event.EventID]bool{},
			Owed:      map[event.SessionID][]event.EventID{},
		}
		for _, session := range wire.Sessions {
			keys := make([]event.EventID, 0, len(session.Judgments))
			for _, judgment := range session.Judgments {
				keys = append(keys, judgment.ID)
			}
			account.Judgments[event.SessionID(session.Session)] = keys
			account.Owed[event.SessionID(session.Session)] = keys
		}
		return account
	}
	t.Fatal("the checkpoint states no compression slice")
	return compressAccount{}
}

// TestCompressionJudgmentsSurviveTheBootCut pins the compression state slice.
//   - the checkpoint states the batch the live consumer still owes, and names no identity a product answers
//   - the boot owes that batch, so the account survives the cut
//   - the booted, the anchored and the full walk owe one batch
//   - negative: a boot beside a checkpoint stating no compression account restores no slice, so the batch is the checkpoint's
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
	owed := event.CompressRequest{
		ID:     event.EventID("event-compress-cut-owed"),
		Reason: "material over the window no product answers yet",
		At:     time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC),
	}
	if err := source.fileCompressRequest(ctx, addr.SessionID, owed); err != nil {
		t.Fatalf("file the owed judgment of the chain: %v", err)
	}
	live := compressAccountOf(t, source.compress)
	if len(live.Answered) != products {
		t.Fatalf("the live consumer answers %d judgment(s), want the %d products the chain holds", len(live.Answered), products)
	}
	if held := live.Owed[addr.SessionID]; !slices.Equal(held, []event.EventID{owed.ID}) {
		t.Fatalf("the live consumer owes %v, want the judgment %s it filed beside the %d its products answer", held, owed.ID, products)
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
		t.Fatalf("the checkpoint states the slices %v, want the batch the chain owes beside them", names)
	}
	requireSameAccount(t, "checkpoint", statedCompressAccount(t, checkpoint), live)
	for _, slice := range checkpoint.Slices {
		if slice.Name != checkpointSliceCompress {
			continue
		}
		for request := range live.Answered {
			if strings.Contains(string(slice.Payload), string(request)) {
				t.Fatalf("the compression slice names the answered judgment %s, want the batch still owed alone: the request and the product that answers it stay on the chain", request)
			}
		}
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	full := windowChainRun(t, ctx, storage)
	resetCompression(full)
	if _, _, _, err := full.seedFullReplay(ctx, full.tier); err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	requireSameAccount(t, "full walk", compressAccountOf(t, full.compress), live)
	endRun(t, full)

	booted := windowChainRun(t, ctx, storage)
	requireSameAccount(t, "booted", compressAccountOf(t, booted.compress), live)
	if bootedOwed := compressAccountOf(t, booted.compress).Owed[addr.SessionID]; len(bootedOwed) != 1 || bootedOwed[0] != owed.ID {
		t.Fatalf("the boot owes %v, want the judgment %s the checkpoint states beside the %d an answered judgment restates", bootedOwed, owed.ID, len(live.Answered))
	}

	booted.stopCheckpointFiler()
	stripped := commitCheckpointWithoutTheCompressionSlice(t, ctx, booted, checkpoint)
	endRun(t, booted)
	bare := windowChainRun(t, ctx, storage)
	if restored, _ := bare.CheckpointFiled(); restored != stripped {
		t.Fatalf("the boot restored the checkpoint %s, want the stripped checkpoint %s", restored, stripped)
	}
	bare.mu.Lock()
	statedSlices := bare.checkpointSlices[checkpointSliceCompress]
	bare.mu.Unlock()
	if statedSlices {
		t.Fatal("the boot beside a checkpoint stating no compression account read a compression slice, want the account of the stripped cut alone")
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
	key := event.EventID(event.CheckpointKey(partition, run.landedSeq[partition]+1))
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

// TestCompressionSliceBodyIsBoundedByTheBatchItOwes pins what the compression slice costs the checkpoint body.
//   - an all-answered account states nothing at four, eight and sixteen answered judgments, so no depth grows the body and no settled judgment is restated
//   - one more owed judgment costs the same bytes at four, eight and sixteen of them, so no depth grows an entry either
//   - negative: an owed judgment costs the reason and the moment it was filed with beside the nothing an answered one costs, so the owed batch is what the bound is
//   - consumer: compressStateSlice.SnapshotState, the slice the framework checkpoint files
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionSliceBodyIsBoundedByTheBatchItOwes(t *testing.T) {
	ctx := context.Background()
	const session = event.SessionID("session-compress-body")
	request := func(at int) event.CompressRequest {
		return event.CompressRequest{
			ID:     event.EventID(fmt.Sprintf("event-compress-body-%02d", at)),
			Reason: strings.Repeat("the reason the band was compressed ", 12),
			At:     time.Date(2026, 11, 16, 12, 0, at, 0, time.UTC),
		}
	}
	accountBytes := func(count, answered int) int {
		consumer := NewCompressionConsumer(nil)
		for at := 0; at < count; at++ {
			consumer.judgments[session] = append(consumer.judgments[session], request(at))
			if at < answered {
				consumer.answered[request(at).ID] = struct{}{}
			}
		}
		run := &Run{compress: consumer}
		payload, err := compressStateSlice{run: run}.SnapshotState(ctx)
		if err != nil {
			t.Fatalf("state the compression slice of %d judgment(s), %d answered: %v", count, answered, err)
		}
		if payload == nil {
			return 0
		}
		if err := json.Unmarshal(payload, &compressSliceWire{}); err != nil {
			t.Fatalf("the slice of %d judgment(s) states no readable payload", count)
		}
		return len(payload)
	}

	for _, count := range []int{4, 8, 16} {
		if settled := accountBytes(count, count); settled != 0 {
			t.Fatalf("the %d answered judgment(s) state the body %d byte(s), want nothing: a judgment a product answers stays on the chain", count, settled)
		}
	}

	shallow, middling, deep := accountBytes(4, 0), accountBytes(8, 0), accountBytes(16, 0)
	first := (middling - shallow) / 4
	second := (deep - middling) / 8
	if first <= 0 || second <= 0 {
		t.Fatalf("one owed judgment costs the body %d and %d byte(s) at the two depths, want the identity, the reason and the moment it was filed with", first, second)
	}
	if first != second {
		t.Fatalf("one owed judgment costs the body %d byte(s) at 4 beside %d byte(s) at 8, want the same entry at every depth", first, second)
	}
	if got, want := deep-shallow, first*12; got != want {
		t.Fatalf("the body grew %d byte(s) from 4 to 16 owed judgment(s), want the %d the stated judgments take", got, want)
	}
	identity := 2 * (len(string(request(0).ID)) + 8)
	if first <= identity {
		t.Fatalf("one owed judgment costs the body %d byte(s), want more than the %d two bare identities of one request take: an owed judgment is stated with the reason and the moment it was filed with", first, identity)
	}
	if settled := accountBytes(8, 8); settled >= middling {
		t.Fatalf("the batch of 8 answered judgments states %d byte(s) beside the %d of the 8 owed ones, want the answered batch stated nowhere",
			settled, middling)
	}
}
