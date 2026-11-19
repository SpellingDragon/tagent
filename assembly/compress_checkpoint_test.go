// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

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
