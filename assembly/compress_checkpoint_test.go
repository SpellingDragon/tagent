// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// compressAccount is the state a compression consumer derives from the commits it
// observes: the judgments of every session chain it still owes, the identities the
// products of those chains answer, and the batch that is owed.
type compressAccount struct {
	Judgments map[event.SessionID][]event.EventID
	Answered  map[event.EventID]bool
	Owed      map[event.SessionID][]event.EventID
}

// compressAccountOf reads the account one consumer holds beside the answers its
// chain states: the batch still owed is the consumer's own state, and the
// identities a product answers are read from the chain, because the answer
// outlives the account that read it.
func compressAccountOf(t *testing.T, consumer *CompressionConsumer) compressAccount {
	t.Helper()
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	account := compressAccount{
		Judgments: make(map[event.SessionID][]event.EventID, len(consumer.judgments)),
		Answered:  settledJudgments(t, consumer),
		Owed:      map[event.SessionID][]event.EventID{},
	}
	for session, filed := range consumer.judgments {
		keys := make([]event.EventID, 0, len(filed))
		for _, request := range filed {
			keys = append(keys, request.ID)
		}
		account.Judgments[session] = keys
		if len(keys) > 0 {
			account.Owed[session] = keys
		}
	}
	return account
}

// settledJudgments answers the judgment identities the products of one consumer's
// chain answer. A consumer of no session face, and a run beside no storage tier,
// states no answer: the chain it would read is no chain this process holds.
func settledJudgments(t *testing.T, consumer *CompressionConsumer) map[event.EventID]bool {
	t.Helper()
	out := map[event.EventID]bool{}
	sessions, carried := consumer.sessions.(ageFoldSessions)
	if !carried || sessions.run == nil || sessions.run.tier == nil {
		return out
	}
	for _, product := range chainProducts(t, context.Background(), sessions.run) {
		out[product.Answers.Request] = true
	}
	return out
}

// judgedCount reports how many judgments one account holds an answer for, on the
// chain or in the batch still owed.
func (a compressAccount) judgedCount() int {
	count := len(a.Answered)
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
		t.Fatalf("the chain answers %d judgment(s), want the %d products it holds", len(live.Answered), products)
	}
	for request := range live.Answered {
		if slices.Contains(live.Judgments[addr.SessionID], request) {
			t.Fatalf("the live consumer still owes the judgment %s its product answers, want a settled judgment held nowhere beside %d on the chain", request, len(live.Answered))
		}
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

// strippedCompressionCut runs one chain to a checkpoint that states its compression
// account, files that checkpoint again without the account, and answers what a boot
// over the stripped cut opens beside.
//
// - The owed judgment is filed, then the chain commits past it, so the newest survivor the projection states stands beyond it and a boot bounded by that coverage reads no fact of it.
// - Negative: a fixture whose account answers no judgment, or whose owed judgment stands past the projection's boundary, fails here instead of proving nothing below.
func strippedCompressionCut(t *testing.T, ctx context.Context, storage Option) (event.Address, compressAccount, event.CompressRequest, event.EventID) {
	t.Helper()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-compress-stripped"}
	source := windowChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	commitWindowTurns(t, ctx, source, addr, 12)
	if countFoldProducts(t, ctx, source) == 0 {
		t.Fatal("the fixture folded nothing, so the account the cut states proves nothing")
	}
	owed := event.CompressRequest{
		ID:     event.EventID("event-compress-stripped-owed"),
		Reason: "the material over the window no product answers yet",
		At:     time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
	}
	if err := source.fileCompressRequest(ctx, addr.SessionID, owed); err != nil {
		t.Fatalf("file the owed judgment of the chain: %v", err)
	}
	commitWindowTurns(t, ctx, source, addr, 16)
	products := countFoldProducts(t, ctx, source)
	live := compressAccountOf(t, source.compress)
	if len(live.Answered) != products {
		t.Fatalf("the account answers %d judgment(s), want the %d products the chain holds", len(live.Answered), products)
	}
	if held := live.Owed[addr.SessionID]; !slices.Equal(held, []event.EventID{owed.ID}) {
		t.Fatalf("the account owes %v, want the judgment %s beside the %d its products answer", held, owed.ID, products)
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
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	request, held, err := source.tier.Segments.GetEventInPartition(ctx, owed.ID, partition)
	if err != nil || !held {
		t.Fatalf("read the owed judgment %s back by identity = (%t, %v), want the fact the chain holds", owed.ID, held, err)
	}
	if cut, named := checkpoint.Offset[partition]; !named || request.Seq > cut {
		t.Fatalf("the owed judgment %s stands at %d beside the cut %d, want it inside the range the projection cut covers: a boot that read past the cut anyway would prove nothing",
			owed.ID, request.Seq, cut)
	}
	if boundary := statedSurvivorBoundary(t, ctx, source, checkpoint, addr, partition); request.Seq > boundary {
		t.Fatalf("the owed judgment %s stands at %d beside the newest survivor %d the projection states, want it inside the range the projection presents: a boot bounded by that coverage would read no fact of it",
			owed.ID, request.Seq, boundary)
	}
	stripped := commitCheckpointWithoutTheCompressionSlice(t, ctx, source, checkpoint)
	endRun(t, source)
	return addr, live, owed, stripped
}

// statedSurvivorBoundary answers the newest chain position the projection slice of
// one checkpoint names for one chain, or zero beside a checkpoint stating no entry
// of it: the boundary a boot bounded by the projection's coverage replays past.
func statedSurvivorBoundary(t *testing.T, ctx context.Context, run *Run, checkpoint event.Checkpoint, addr event.Address, partition int) int64 {
	t.Helper()
	boundary := int64(0)
	for _, slice := range checkpoint.Slices {
		if slice.Name != checkpointSliceProjection {
			continue
		}
		var wire projectionSliceWire
		if err := json.Unmarshal([]byte(slice.Payload), &wire); err != nil {
			t.Fatalf("the projection slice states no readable payload: %v", err)
		}
		for _, session := range wire.Sessions {
			if session.Session != string(addr.SessionID) {
				continue
			}
			for _, entry := range session.Entries {
				stored, held, err := run.tier.Segments.GetEventInPartition(ctx, entry.Key, partition)
				if err != nil || !held {
					continue
				}
				if stored.Seq > boundary {
					boundary = stored.Seq
				}
			}
		}
	}
	if boundary == 0 {
		t.Fatalf("the checkpoint states no survivor of %s, so the boundary a boot replays past proves nothing", addr.SessionID)
	}
	return boundary
}

// TestProjectionCoverageDoesNotStandForOtherConsumers pins the account a cut that states no compression slice leaves unstated.
//   - the owed judgment stands inside the range the projection cut covers, so a boot bounded by that cut reads no fact of it
//   - the compression account reads the facts its own slice does not state, so the boot owes the batch the stopped process owed
//   - the boot restores the stripped cut and reads no compression slice of it, so the account is that read's answer and no slice's
//   - consumer: the real boot restore over a stripped cut, from the checkpoint restore to the coverage read its mount declared
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectionCoverageDoesNotStandForOtherConsumers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr, live, owed, stripped := strippedCompressionCut(t, ctx, storage)

	booted := windowChainRun(t, ctx, storage)
	if restored, _ := booted.CheckpointFiled(); restored != stripped {
		t.Fatalf("the boot restored the checkpoint %s, want the stripped cut %s", restored, stripped)
	}
	booted.mu.Lock()
	statedCompress := booted.checkpointSlices[checkpointSliceCompress]
	booted.mu.Unlock()
	if statedCompress {
		t.Fatal("the boot read a compression slice of the stripped cut, so the account below could be that slice's")
	}
	requireSameAccount(t, "booted", compressAccountOf(t, booted.compress), live)
	if held := pendingIdentities(booted.compress, addr.SessionID); !held[owed.ID] {
		t.Fatalf("the boot owes %v, want the judgment %s the cut states no account of", held, owed.ID)
	}
	t.Logf("the boot restored the cut %s stating no compression slice and owed the judgment %s beside the %d the chain answers",
		stripped, owed.ID, len(live.Answered))
	endRun(t, booted)
}

// TestMissingCompressionSliceDoesNotReplayExecutedJudgments pins that the read a missing slice leaves owed strikes every settled judgment.
//   - the read folds a judgment before the product that answers it, so the answer strikes it instead of leaving it owed
//   - so the boot owes no judgment the chain answers: an execution over that account plans no model call and files no second product
//   - negative: a read that folded the products first would owe every answered judgment, which the count of strikes and the products answer
//   - consumer: the real boot restore and CompressionConsumer.Execute behind it
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestMissingCompressionSliceDoesNotReplayExecutedJudgments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr, live, owed, stripped := strippedCompressionCut(t, ctx, storage)

	booted := windowChainRun(t, ctx, storage)
	if restored, _ := booted.CheckpointFiled(); restored != stripped {
		t.Fatalf("the boot restored the checkpoint %s, want the stripped cut %s", restored, stripped)
	}
	pending := pendingIdentities(booted.compress, addr.SessionID)
	for request := range live.Answered {
		if pending[request] {
			t.Fatalf("the boot owes the answered judgment %s, want the answer the chain states to strike it: an account owing it executes it a second time", request)
		}
	}
	if !pending[owed.ID] {
		t.Fatalf("the boot owes %v, want the judgment %s no product answers", pending, owed.ID)
	}
	if struck := answeredCount(t, booted); struck < len(live.Answered) {
		t.Fatalf("the boot struck %d answered judgment(s), want at least the %d the read folded", struck, len(live.Answered))
	}

	before := productsByRequest(t, ctx, booted)
	if err := booted.compress.Execute(ctx, addr.SessionID); err != nil {
		t.Fatalf("execute the batch the boot owes: %v", err)
	}
	after := productsByRequest(t, ctx, booted)
	if len(after) != len(before) {
		t.Fatalf("the execution left %d answered judgment(s) on the chain beside the %d it found, want no judgment the chain answers executed again", len(after), len(before))
	}
	for request, count := range after {
		if count != before[request] {
			t.Fatalf("the execution filed %d product(s) for the judgment %s beside the %d the chain stated, want one product per judgment", count, request, before[request])
		}
	}
	if held := pendingIdentities(booted.compress, addr.SessionID); !held[owed.ID] {
		t.Fatalf("the execution owes %v, want the judgment %s whose material no product names to stay owed", held, owed.ID)
	}
	t.Logf("the boot struck %d answered judgment(s) while owing the judgment %s alone", answeredCount(t, booted), owed.ID)
	endRun(t, booted)
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
		consumer := NewCompressionConsumer(nil, nil)
		for at := answered; at < count; at++ {
			consumer.judgments[session] = append(consumer.judgments[session], request(at))
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

// TestCompressionCheckpointTracksPendingWork pins what the account costs the checkpoint it states, beside the settled history that grows under it.
//   - the batch still owed is the whole resident state: an account owing two judgments holds two entries
//   - that holds at none, sixty-four and two hundred and fifty-six settled judgments
//   - the slice bytes are the same bytes at every depth of settled history, so no settled judgment is restated and no depth grows the body
//   - the build allocates the same bytes per call at every depth of settled history: the state the slice walks is the batch, never the history it settled
//   - a new judgment grows the batch by one entry and the body by the one entry it states, so the pending work is what both faces track
//   - the settled judgments are struck exactly once each and held nowhere, so the account of a settled history is no key at all
//   - consumer: compressStateSlice.SnapshotState, the slice the framework checkpoint files
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionCheckpointTracksPendingWork(t *testing.T) {
	ctx := context.Background()
	const session = event.SessionID("session-compress-pending-work")
	const owed = 2
	request := func(at int) event.CompressRequest {
		return event.CompressRequest{
			ID:     event.EventID(fmt.Sprintf("event-compress-pending-%03d", at)),
			Reason: strings.Repeat("the reason the band was compressed ", 12),
			At:     time.Date(2026, 11, 16, 13, 0, at, 0, time.UTC),
		}
	}
	accountOf := func(settled int) *Run {
		consumer := NewCompressionConsumer(nil, nil)
		for at := 0; at < settled; at++ {
			consumer.Record(session, request(at))
			consumer.NoteProduct(event.CompressProductFor(request(at).ID, compressionProducer, event.EventID(fmt.Sprintf("key-%03d", at)), nil, ""))
		}
		for at := settled; at < settled+owed; at++ {
			consumer.Record(session, request(at))
		}
		return &Run{compress: consumer}
	}
	bodyBytes := func(run *Run) int {
		payload, err := compressStateSlice{run: run}.SnapshotState(ctx)
		if err != nil {
			t.Fatalf("state the compression slice: %v", err)
		}
		return len(payload)
	}
	buildBytes := func(run *Run) float64 {
		const calls = 128
		slice := compressStateSlice{run: run}
		if _, err := slice.SnapshotState(ctx); err != nil {
			t.Fatalf("state the compression slice: %v", err)
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for at := 0; at < calls; at++ {
			if _, err := slice.SnapshotState(ctx); err != nil {
				t.Fatalf("state the compression slice: %v", err)
			}
		}
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / calls
	}

	none := accountOf(0)
	if held := none.compress.pending(session); len(held) != owed {
		t.Fatalf("the account of no settled judgment owes %d judgment(s), want the %d it filed", len(held), owed)
	}
	shallow, middling, deep := accountOf(64), accountOf(128), accountOf(256)
	for _, probe := range []struct {
		settled int
		run     *Run
	}{{64, shallow}, {128, middling}, {256, deep}} {
		held := probe.run.compress.pending(session)
		if len(held) != owed {
			t.Fatalf("the account beside %d settled judgment(s) holds %d entr(ies), want the %d it still owes: the settled history is no resident state", probe.settled, len(held), owed)
		}
		if got := probe.run.compress.struckNotes(); got != probe.settled {
			t.Fatalf("the account beside %d settled judgment(s) struck %d, want one strike per settled judgment", probe.settled, got)
		}
		if got := bodyBytes(probe.run); got != bodyBytes(none) {
			t.Fatalf("the batch beside %d settled judgment(s) states %d byte(s) beside the %d of the same batch alone, want the settled history stated nowhere", probe.settled, got, bodyBytes(none))
		}
	}
	alone := buildBytes(none)
	for _, probe := range []struct {
		settled int
		run     *Run
	}{{64, shallow}, {256, deep}} {
		got := buildBytes(probe.run)
		if got > alone+allocNoise {
			t.Fatalf("stating the batch beside %d settled judgment(s) allocates %.0f byte(s) a call beside the %.0f of the batch alone, want a build bounded by the batch it owes", probe.settled, got, alone)
		}
	}

	grown := accountOf(256)
	grown.compress.Record(session, request(256+owed))
	if held := grown.compress.pending(session); len(held) != owed+1 {
		t.Fatalf("the account holds %d judgment(s) beside the one filed after the settled history, want the %d it owes", len(held), owed+1)
	}
	if got, want := bodyBytes(grown), bodyBytes(none); got <= want {
		t.Fatalf("the batch grew to %d byte(s) beside the %d it owed, want the one entry the new judgment states", got, want)
	}
}

// allocNoise is the slack one build of the compression slice is measured with:
// the reconciler and the counter keep allocating beside the call, so the face is
// pinned as a bound rather than as an equality.
const allocNoise = 512.0

// TestCompressionCheckpointPreservesOutOfOrderPendingRequest pins the batch an out-of-order answer leaves owed.
//   - an answer that lands second removes its own judgment and neither the ones filed before it nor after it
//   - so the account is never truncated to a prefix or to a watermark
//   - the answers of the newest judgments leave the oldest owed, so the batch is not read as a suffix either
//   - a repeated answer removes nothing more, so the charge is one strike per judgment
//   - the slice states the judgment still owed alone: the answered judgments of the same session are stated nowhere
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionCheckpointPreservesOutOfOrderPendingRequest(t *testing.T) {
	ctx := context.Background()
	const session = event.SessionID("session-compress-out-of-order")
	filed := make([]event.CompressRequest, 0, 3)
	for at := 0; at < 3; at++ {
		filed = append(filed, event.CompressRequest{
			ID:     event.EventID(fmt.Sprintf("event-compress-out-of-order-%d", at)),
			Reason: "the band the walk answered out of order",
			At:     time.Date(2026, 11, 16, 14, 0, at, 0, time.UTC),
		})
	}
	answer := func(request event.CompressRequest) event.CompressProduct {
		return event.CompressProductFor(request.ID, compressionProducer, event.EventID("key-"+request.ID), nil, "")
	}
	consumer := NewCompressionConsumer(nil, nil)
	for _, request := range filed {
		consumer.Record(session, request)
	}
	consumer.NoteProduct(answer(filed[1]))
	if held := consumer.pending(session); !slices.Equal(identitiesOf(held), []event.EventID{filed[0].ID, filed[2].ID}) {
		t.Fatalf("the account owes %v beside the answer of the middle judgment %s, want the ones filed before and after it",
			identitiesOf(held), filed[1].ID)
	}
	consumer.NoteProduct(answer(filed[2]))
	if held := consumer.pending(session); !slices.Equal(identitiesOf(held), []event.EventID{filed[0].ID}) {
		t.Fatalf("the account owes %v beside the answers of the two newest judgments, want the oldest judgment %s alone: neither a prefix nor a suffix of the batch is the batch",
			identitiesOf(held), filed[0].ID)
	}
	consumer.NoteProduct(answer(filed[2]))
	if got := consumer.struckNotes(); got != 2 {
		t.Fatalf("the account struck %d judged judgment(s) beside the repeated answer of %s, want the two it answered", got, filed[2].ID)
	}
	if held := consumer.pending(session); !slices.Equal(identitiesOf(held), []event.EventID{filed[0].ID}) {
		t.Fatalf("the repeated answer left the account owing %v, want the oldest judgment %s alone", identitiesOf(held), filed[0].ID)
	}

	payload, err := compressStateSlice{run: &Run{compress: consumer}}.SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the compression slice: %v", err)
	}
	if !strings.Contains(string(payload), string(filed[0].ID)) {
		t.Fatalf("the compression slice states no judgment beside the one %s the chain still owes, want the pending batch stated", filed[0].ID)
	}
	for _, settled := range filed[1:] {
		if strings.Contains(string(payload), string(settled.ID)) {
			t.Fatalf("the compression slice states the answered judgment %s, want the batch still owed alone", settled.ID)
		}
	}
}

// TestCompressionCheckpointDoesNotRepeatSettledRequest pins the answer a chain already states.
//   - a judgment the chain answers is struck before the material is summarized: the execution files no product and asks no model, and the batch stops owing it
//   - the answer is read at the identity the judgment and the fold product key mint
//   - so the execution reaches the chain rather than an account of the answers it read
//   - negative: the material still stands as foldable, so an execution that judged the projection instead of the answer would file the product again
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionCheckpointDoesNotRepeatSettledRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-compress-settled"}
	run := windowChainRun(t, ctx, storage)
	run.compress.sessions = nil
	run.ageFold.setExecutor(nil)
	commitWindowTurns(t, ctx, run, addr, 12)
	judged := pendingIdentities(run.compress, addr.SessionID)
	if len(judged) == 0 {
		t.Fatal("the fixture filed no compression judgment, so the settled answer proves nothing")
	}

	face := ageFoldSessions{run: run}
	projection, window, carried := face.ProjectionOf(ctx, addr.SessionID)
	if !carried {
		t.Fatal("the chain registered no projection, so the fixture names no product")
	}
	folded := foldProjectionOf(ctx, projection)
	if folded == nil {
		t.Fatal("the projection folds to no accounting-free view, so the fixture names no product")
	}
	plan, err := folded.PlanByAge(ctx, replay.AgeFoldPlan{
		KeepRecent:         window.KeepRecentTasks,
		VolumeThreshold:    window.VolumeThresholdBytes(),
		SummaryBudgetBytes: window.SummaryBudgetBytes,
	})
	if err != nil {
		t.Fatalf("plan the fold of the chain: %v", err)
	}
	settled := replay.FoldProduct{}
	for _, product := range plan.Products {
		if judged[product.Request.ID] {
			settled = product
			break
		}
	}
	if settled.Request.ID == "" {
		t.Fatal("the plan names no product of a judgment the chain filed, so the settled answer proves nothing")
	}
	minted := event.CompressProductFor(settled.Request.ID, compressionProducer, settled.Entry.Key, settled.Covered, settled.Entry.Content)
	run.mu.Lock()
	agent := run.agentOfSessionLocked(addr.SessionID)
	run.mu.Unlock()
	if _, err := run.commitFactChecked(ctx, compressProductEvent(addr.SessionID, agent, minted, time.Now().UTC())); err != nil {
		t.Fatalf("file the product the chain answers the judgment %s with: %v", settled.Request.ID, err)
	}

	counted := &countingCompressSessions{compressProductStore: face}
	run.compress = NewCompressionConsumer(face, counted)
	run.compress.Record(addr.SessionID, event.CompressRequest{ID: settled.Request.ID, Reason: settled.Request.Reason})
	if held := run.compress.pending(addr.SessionID); !slices.Equal(identitiesOf(held), []event.EventID{settled.Request.ID}) {
		t.Fatalf("the account owes %v beside the answer the chain states, want the judgment %s it was handed", identitiesOf(held), settled.Request.ID)
	}
	if err := run.compress.Execute(ctx, addr.SessionID); err != nil {
		t.Fatalf("execute the batch: %v", err)
	}
	if counted.held == 0 {
		t.Fatal("the execution read no answer back at the identity of the judgment, want the chain asked before the material folds")
	}
	if counted.filed != 0 {
		t.Fatalf("the execution filed %d product(s) of a judgment its chain answers, want the answered judgment executed never again", counted.filed)
	}
	if held := run.compress.pending(addr.SessionID); len(held) != 0 {
		t.Fatalf("the account still owes %v beside the answer the chain states, want the answered judgment struck", identitiesOf(held))
	}
	if got := run.compress.struckNotes(); got == 0 {
		t.Fatal("the execution struck no judgment beside the answer it read, want the answered judgment charged once")
	}
	if answers := productsByRequest(t, ctx, run); answers[settled.Request.ID] != 1 {
		t.Fatalf("the chain answers the judgment %s with %d product(s), want the one it states", settled.Request.ID, answers[settled.Request.ID])
	}
	endRun(t, run)
}

// countingCompressSessions counts what one execution asks the product ledger
// face: the answers it reads back at their identity and the products it files.
type countingCompressSessions struct {
	compressProductStore
	held  int
	filed int
}

// HoldsCompressProduct asks the chain whether it answers one judgment.
func (c *countingCompressSessions) HoldsCompressProduct(ctx context.Context, session event.SessionID, request, key event.EventID) (bool, error) {
	c.held++
	return c.compressProductStore.HoldsCompressProduct(ctx, session, request, key)
}

// FileCompressProduct files one execution result on the chain.
func (c *countingCompressSessions) FileCompressProduct(ctx context.Context, session event.SessionID, product event.CompressProduct) error {
	c.filed++
	return c.compressProductStore.FileCompressProduct(ctx, session, product)
}

// identitiesOf reports the identities one batch of judgments is filed under, in
// filing order.
func identitiesOf(filed []event.CompressRequest) []event.EventID {
	out := make([]event.EventID, 0, len(filed))
	for _, request := range filed {
		out = append(out, request.ID)
	}
	return out
}

// locatingChainRun assembles one run of the folding fixture beside the model face
// it answers, so a restore that asked the model again is counted.
func locatingChainRun(t *testing.T, ctx context.Context, opts ...Option) (*Run, *snapshotTestModel) {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	options := append([]Option{
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
	}, opts...)
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}, {ID: "agent-b", Model: "model-a", ReplayWindow: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, options...)
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run, provider
}

// TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel pins that every filed product is reached again through what filed it, asking no model.
//   - the restore states that product's text without asking the model a second time
//   - producer: an executed judgment files one product, and the identity the fact is keyed under is minted from the request, the producer and the key alone
//   - producer: the fact itself states the fields that identity was minted from, so a product is located from the fact rather than from the account a process holds
//   - producer: the locator a producer asks before executing a judgment answers the filed products and answers nothing for one the chain never filed
//   - producer: the fold checkpoint names every entry it leaves beside the text it minted
//   - restorer: the product fact reads back as the product it states, field by field
//   - restorer: a boot over the root states the recorded text of every fold entry, from both checkpoints, and asks no model
//   - negative: a fact missing the key or the request, and a fact stating an identity another fact is keyed under, restore no product instead of inventing one
//   - negative: a checkpoint naming a survivor the chain holds no entry for and carrying no text refuses the restore by name
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-compress-locating"}

	source, asked := locatingChainRun(t, ctx, storage)
	commitWindowTurns(t, ctx, source, addr, 12)
	products := chainProducts(t, ctx, source)
	if len(products) == 0 {
		t.Fatal("the fold filed no product, so no locating below is a claim about one")
	}
	if len(asked.requests) == 0 {
		t.Fatal("the fold asked the model nothing, so a restore that asked it again would prove nothing")
	}
	survivors := latestFoldCheckpointSurvivors(t, ctx, source)
	if len(survivors) == 0 {
		t.Fatal("the fold checkpoint names no survivor, so the text the restore states proves nothing")
	}

	for _, product := range products {
		stored, err := source.CommittedEvent(ctx, product.ID)
		if err != nil {
			t.Fatalf("read the product %s back by its identity: %v", product.ID, err)
		}
		for _, field := range []struct{ name, held string }{
			{name: "request", held: stored.Meta[metaKeyCompressRequest]},
			{name: "producer", held: stored.Meta[metaKeyCompressProducer]},
			{name: "key", held: stored.Meta[metaKeyCompressKey]},
		} {
			if field.held == "" {
				t.Fatalf("the product fact %s states no %s, so nothing a reader holds reaches the identity it is keyed under", stored.Key, field.name)
			}
		}
		read, stated := compressProductOf(stored)
		if !stated {
			t.Fatalf("the product fact %s states no product, want the fields it was filed with", stored.Key)
		}
		if read.ID != stored.Key {
			t.Fatalf("the product fact %s reads back as %s, want the identity it is keyed under", stored.Key, read.ID)
		}
		if read.Answers != product.Answers || read.Key != product.Key || read.Text != product.Text || !slices.Equal(read.Covered, product.Covered) {
			t.Fatalf("the product %s reads back as request %s producer %s key %s text %q, want the request %s producer %s key %s text %q it was filed with",
				stored.Key, read.Answers.Request, read.Answers.Producer, read.Key, read.Text,
				product.Answers.Request, product.Answers.Producer, product.Key, product.Text)
		}
		if reminted := event.CompressProductFor(read.Answers.Request, read.Answers.Producer, read.Key, read.Covered, read.Text); reminted.ID != stored.Key {
			t.Fatalf("the product %s is minted as %s from the fields its fact states, want the identity it is keyed under: the text is no part of the address",
				stored.Key, reminted.ID)
		}
		held, err := source.holdsCompressProduct(ctx, addr.SessionID, product.Answers.Request, product.Key)
		if err != nil {
			t.Fatalf("ask the chain whether it answers the judgment %s: %v", product.Answers.Request, err)
		}
		if !held {
			t.Fatalf("the locator answered no product for the judgment %s the chain answers, so the execution would fold its material again", product.Answers.Request)
		}
	}
	if held, err := source.holdsCompressProduct(ctx, addr.SessionID, event.EventID("event-compress-never-filed"), event.EventID("-hist-never-filed")); err != nil || held {
		t.Fatalf("the locator answered (%t, %v) for a judgment the chain never filed, want no product and no error", held, err)
	}

	carried := 0
	for _, survivor := range survivors {
		switch survivor.Type {
		case replay.ProjectionTypeSegmentFold, replay.ProjectionTypeHistorySummary:
			if survivor.Content == "" {
				t.Fatalf("the checkpoint names the fold product %s carrying no text, want the text a restore states it from: the chain states that text under no key this entry names", survivor.Key)
			}
			carried++
		default:
			if survivor.Content != "" {
				t.Fatalf("the checkpoint names the mapped entry %s carrying %d byte(s), want the text read back from the chain under that key", survivor.Key, len(survivor.Content))
			}
		}
	}
	if carried == 0 {
		t.Fatal("the fold checkpoint carries no text, so the restore below restates nothing a second reading could not")
	}
	t.Logf("the fold asked the model %d time(s) producing %d product(s)", len(asked.requests), len(products))
	t.Logf("located %d product(s) by the identity their facts state, and the locator answered each of them once", len(products))
	t.Logf("the fold checkpoint names %d survivor(s), %d of them carrying the text this anchor restates", len(survivors), carried)
	factTexts := make(map[string]bool, len(products))
	for _, product := range products {
		if product.Text != "" {
			factTexts[product.Text] = true
		}
	}
	for _, survivor := range survivors {
		if survivor.Content == "" {
			continue
		}
		if !factTexts[survivor.Content] {
			t.Fatalf("the checkpoint carries the text %q of the survivor %s, want the text the product fact states under its own identity: the checkpoint copy restates that text instead of minting a second one", survivor.Content, survivor.Key)
		}
	}

	checkpointID, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the framework checkpoint of the chain = (%t, %v), want a filed fact", filed, err)
	}
	endRun(t, source)
	filedProducts := len(products)

	booted, bootAsked := locatingChainRun(t, ctx, storage)
	if carried, _ := booted.CheckpointFiled(); carried != checkpointID {
		t.Fatalf("the boot restored the checkpoint %s, want the one %s the chain states", carried, checkpointID)
	}
	requireRecordedSurvivorText(t, ctx, booted, addr, survivors, "the boot")
	requireMappedSurvivorText(t, ctx, booted, addr, survivors)
	if len(bootAsked.requests) != 0 {
		t.Fatalf("the boot asked the model %d time(s) restoring %d survivor(s), want the restated text alone: a restore never mints a second summary", len(bootAsked.requests), len(survivors))
	}
	if got := countFoldProducts(t, ctx, booted); got != filedProducts {
		t.Fatalf("the boot left %d product(s) on the chain beside the %d already filed, want the restore to fold nothing again", got, filedProducts)
	}
	endRun(t, booted)

	anchoredSource, _ := locatingChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	commitWindowTurns(t, ctx, anchoredSource, addr, 12)
	anchoredSurvivors := latestFoldCheckpointSurvivors(t, ctx, anchoredSource)
	if len(anchoredSurvivors) == 0 {
		t.Fatal("the fold checkpoint of the anchored root names no survivor, so the anchored restore below restates nothing")
	}
	anchored, anchoredAsked := locatingChainRun(t, ctx)
	seeds, report, err := anchored.seedAnchoredReplay(ctx, anchoredSource.tier)
	if err != nil {
		t.Fatalf("rebuild the chain from the fold checkpoint: %v", err)
	}
	if report.Anchors == 0 {
		t.Fatal("the rebuild used no anchor, so the fold checkpoint restore below is the only path read")
	}
	seed, registered := seeds[addr]
	if !registered {
		t.Fatalf("the anchored rebuild registered no projection for %s", addr.SessionID)
	}
	requireSurvivorText(t, seed.projection.Entries(), anchoredSurvivors, "the anchored rebuild")
	if len(anchoredAsked.requests) != 0 {
		t.Fatalf("the anchored rebuild asked the model %d time(s), want the text the checkpoint carries restated", len(anchoredAsked.requests))
	}
	endRun(t, anchored)
	endRun(t, anchoredSource)

	if err := requireUnstatedProductRefusals(t, products[0]); err != nil {
		t.Fatal(err)
	}
	requireUnstatedSurvivorRefusal(t, ctx)
}

// requireRecordedSurvivorText reports whether the projection one boot registered
// states, for every survivor that carries text, the text the checkpoint recorded.
func requireRecordedSurvivorText(t *testing.T, ctx context.Context, run *Run, addr event.Address, survivors []anchorEntry, name string) {
	t.Helper()
	projection, _, carried := run.compress.sessions.ProjectionOf(ctx, addr.SessionID)
	if !carried {
		t.Fatalf("%s registered no projection for %s", name, addr.SessionID)
	}
	requireSurvivorText(t, projection.Entries(), survivors, name)
}

// requireSurvivorText reports whether one restored projection states the text the
// fold checkpoint recorded for every survivor that carries one.
func requireSurvivorText(t *testing.T, entries []replay.ProjectionEntry, survivors []anchorEntry, name string) {
	t.Helper()
	stated := make(map[event.EventID]replay.ProjectionEntry, len(entries))
	for _, entry := range entries {
		stated[entry.Key] = entry
	}
	for _, survivor := range survivors {
		if survivor.Content == "" {
			continue
		}
		entry, held := stated[survivor.Key]
		if !held {
			t.Fatalf("%s states no entry for the survivor %s the checkpoint names", name, survivor.Key)
		}
		if entry.Content != survivor.Content {
			t.Fatalf("%s states %q for the survivor %s, want the %q the checkpoint recorded: a restore reads the text it was filed with instead of minting a second one",
				name, entry.Content, survivor.Key, survivor.Content)
		}
	}
}

// requireMappedSurvivorText reports whether the projection one boot registered states,
// for every survivor carrying no text of its own, the message the chain states under the
// key the survivor names: the entry's text is the chain's, read back by key.
func requireMappedSurvivorText(t *testing.T, ctx context.Context, run *Run, addr event.Address, survivors []anchorEntry) {
	t.Helper()
	projection, _, carried := run.compress.sessions.ProjectionOf(ctx, addr.SessionID)
	if !carried {
		t.Fatalf("the boot registered no projection for %s", addr.SessionID)
	}
	stated := make(map[event.EventID]replay.ProjectionEntry, len(projection.Entries()))
	for _, entry := range projection.Entries() {
		stated[entry.Key] = entry
	}
	restated := 0
	for _, survivor := range survivors {
		if survivor.Content != "" {
			continue
		}
		entry, held := stated[survivor.Key]
		if !held {
			continue
		}
		stored, err := run.CommittedEvent(ctx, survivor.Key)
		if err != nil {
			t.Fatalf("read the mapped survivor %s back by key: %v", survivor.Key, err)
		}
		mapped, holdsEntry := mappedEntryOf(stored)
		if !holdsEntry {
			continue
		}
		if entry.Content != mapped.Content {
			t.Fatalf("the boot states %q for the mapped survivor %s, want the %q the chain states under that key", entry.Content, survivor.Key, mapped.Content)
		}
		restated++
	}
	if restated == 0 {
		t.Fatal("the boot states no mapped survivor, so the text read back by key proves nothing")
	}
	t.Logf("the boot restated the chain's own text of %d mapped survivor(s)", restated)
}

// requireUnstatedProductRefusals reports whether a product fact states no product
// when the fields its identity was minted from are missing or name another fact:
// a half-stated fact restores nothing rather than inventing material.
func requireUnstatedProductRefusals(t *testing.T, product event.CompressProduct) error {
	t.Helper()
	fact := compressProductEvent("session-compress-locating", "agent-a", product, time.Now().UTC())
	if _, stated := compressProductOf(fact); !stated {
		return fmt.Errorf("the product fact of %s states no product, so the refusals below say nothing", product.ID)
	}
	for _, absent := range []string{metaKeyCompressKey, metaKeyCompressRequest} {
		mutated := fact
		mutated.Meta = make(map[string]string, len(fact.Meta))
		for key, value := range fact.Meta {
			mutated.Meta[key] = value
		}
		delete(mutated.Meta, absent)
		if _, stated := compressProductOf(mutated); stated {
			return fmt.Errorf("a product fact stating no %s restored a product, want it refused: nothing a reader holds reaches the material it names", absent)
		}
		t.Logf("a product fact stating no %s restored nothing", absent)
	}
	conflicting := fact
	conflicting.Meta = make(map[string]string, len(fact.Meta))
	for key, value := range fact.Meta {
		conflicting.Meta[key] = value
	}
	conflicting.Meta[metaKeyCompressProducer] = "assembly.another-producer"
	if _, stated := compressProductOf(conflicting); stated {
		return fmt.Errorf("a product fact stating another identity than the one it is keyed under restored a product, want it refused: one identity, one fact")
	}
	t.Log("a product fact stating another identity than the one it is keyed under restored nothing")
	return nil
}

// requireUnstatedSurvivorRefusal reports whether a checkpoint naming a survivor the
// chain holds no entry for, and carrying no text, refuses the restore by name.
func requireUnstatedSurvivorRefusal(t *testing.T, ctx context.Context) {
	t.Helper()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-compress-unstated"}
	run, _ := locatingChainRun(t, ctx)
	payload, err := json.Marshal([]anchorEntry{{Key: event.EventID("event-compress-unstated"), Type: event.TypeExternalInput}})
	if err != nil {
		t.Fatalf("encode the survivor payload: %v", err)
	}
	request := event.CompressRequestFor("assembly.test", "the unstated survivor", "the fixture names a survivor no fact states")
	product := event.CompressProductFor(request.ID, compressionProducer, event.EventID("-hist-unstated"), nil, "the folded summary").WithCheckpoint(payload)
	if _, err := run.commitFactChecked(ctx, compressProductEvent(addr.SessionID, addr.AgentID, product, time.Now().UTC())); err != nil {
		t.Fatalf("commit the anchor naming an unstated survivor: %v", err)
	}
	if _, _, err := run.seedAnchoredReplay(ctx, run.tier); !errors.Is(err, ErrCheckpointSurvivorUnstated) {
		t.Fatalf("the rebuild of a checkpoint naming an unstated survivor answered %v, want the refusal named %v", err, ErrCheckpointSurvivorUnstated)
	} else {
		t.Logf("a checkpoint naming a survivor the chain states no entry for refused the rebuild with: %v", err)
	}
	endRun(t, run)
}
