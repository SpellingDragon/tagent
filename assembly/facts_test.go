// The hot window checks of the live fact ledger: a chain that runs long keeps the
// material a drive may still read, and the delivery of a turn reads that material
// by the identity of the invocation rather than by a position of the chain.
package assembly

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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

// factWindowAnswer is the assistant body the scripted provider answers every turn
// of these checks with.
const factWindowAnswer = "the answer the hot window keeps"

// factWindowAddr is the session the hot window checks commit their facts to.
var factWindowAddr = event.Address{AgentID: "agent-a", SessionID: "session-fact-window"}

// TestFactsStayInTheirSessionWindow pins the bound of the live fact ledger.
// - a chain whose last agent_output closes its segment keeps the most recent window of entries
// - a segment in flight is kept whole beside the turn-end fact that opened it
// - a chain beside no turn-end fact is one segment in flight and is kept whole
// - a chain replayed from the durable log converges to the same window
// - each session keeps its own window, so one chain never drops a neighbour's material
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestFactsStayInTheirSessionWindow(t *testing.T) {
	ctx := context.Background()
	t.Run("a closed chain keeps the most recent window", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		const seeded = noWindowResidentFacts + 32
		for at := 0; at < seeded; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("closed-%05d", at), "inv-window-closed", "the material of the closed chain", event.TypeExternalInput))
		}
		commitWindowFact(t, ctx, run, addr, windowEvent(addr,
			"closed-closing", "inv-window-closed", "the answer that closed the chain", event.TypeAgentOutput))

		facts := run.SessionFacts(addr)
		if len(facts) != noWindowResidentFacts {
			t.Fatalf("the closed chain = %d facts, want the window %d", len(facts), noWindowResidentFacts)
		}
		if want := event.EventID("closed-00033"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want %s", facts[0].Key, want)
		}
		if _, held := run.factOf(event.EventID("closed-00032")); held {
			t.Fatalf("the fact outside the window is still held by the live ledger")
		}
		if got := run.CommittedFacts(); got != noWindowResidentFacts {
			t.Fatalf("the live ledger holds %d facts, want the window %d", got, noWindowResidentFacts)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("a segment in flight is kept whole", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		commitWindowFact(t, ctx, run, addr, windowEvent(addr,
			"flight-opening", "inv-window-flight", "the answer that closed the previous segment", event.TypeAgentOutput))
		for at := 0; at < 2*noWindowResidentFacts; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("flight-%05d", at), "inv-window-flight", "the material of the segment in flight", event.TypeExternalInput))
		}

		facts := run.SessionFacts(addr)
		if want := 2*noWindowResidentFacts + 1; len(facts) != want {
			t.Fatalf("the chain in flight = %d facts, want the opening fact beside the whole segment %d", len(facts), want)
		}
		if want := event.EventID("flight-opening"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want the opening fact %s", facts[0].Key, want)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("a chain beside no turn-end is kept whole", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		for at := 0; at < noWindowResidentFacts+64; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("open-%05d", at), "inv-window-open", "the material of the only segment", event.TypeExternalInput))
		}

		facts := run.SessionFacts(addr)
		if want := noWindowResidentFacts + 64; len(facts) != want {
			t.Fatalf("the chain beside no turn-end = %d facts, want the whole segment %d", len(facts), want)
		}
		if want := event.EventID("open-00000"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want the head of the segment %s", facts[0].Key, want)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("a replayed chain converges to the window", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		for at := 0; at < noWindowResidentFacts+64; at++ {
			if err := run.seedFact(ctx, windowEvent(addr, fmt.Sprintf("replay-%05d", at), "inv-window-replay", "the replayed material", event.TypeExternalInput)); err != nil {
				t.Fatalf("seed the replayed fact: %v", err)
			}
		}
		if err := run.seedFact(ctx, windowEvent(addr, "replay-closing", "inv-window-replay", "the answer the earlier process committed", event.TypeAgentOutput)); err != nil {
			t.Fatalf("seed the closing fact: %v", err)
		}
		run.trimSeededFacts()

		facts := run.SessionFacts(addr)
		if len(facts) != noWindowResidentFacts {
			t.Fatalf("the replayed chain = %d facts, want the window %d", len(facts), noWindowResidentFacts)
		}
		if want := event.EventID("replay-00065"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want %s", facts[0].Key, want)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("each session keeps its own window", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		other := event.Address{AgentID: "agent-a", SessionID: "session-fact-window-neighbour"}
		for at := 0; at < noWindowResidentFacts+16; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("long-%05d", at), "inv-window-long", "the material of the long chain", event.TypeExternalInput))
		}
		commitWindowFact(t, ctx, run, addr, windowEvent(addr,
			"long-closing", "inv-window-long", "the answer that closed the long chain", event.TypeAgentOutput))
		for at := 0; at < 6; at++ {
			commitWindowFact(t, ctx, run, other, windowEvent(other,
				fmt.Sprintf("neighbour-%02d", at), "inv-window-neighbour", "the material of the short chain", event.TypeExternalInput))
		}
		if got := len(run.SessionFacts(other)); got != 6 {
			t.Fatalf("the short chain = %d facts, want its six facts kept whole", got)
		}
		if got := len(run.SessionFacts(addr)); got != noWindowResidentFacts {
			t.Fatalf("the long chain = %d facts, want its window %d kept", got, noWindowResidentFacts)
		}
	})
}

// TestDeliveryAfterRestartKeepsTheTurnAnswer pins the delivery of the first turn a restarted host drives over a trimmed chain.
// - the answer of that turn reaches the outbound face although the chain is trimmed as it commits
// - the body the sender takes is the fact the turn committed under its own invocation
// - the chain stays within the window beside the segment the turn flew
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveryAfterRestartKeepsTheTurnAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := factWindowTestRun(t, ctx)
	addr := factWindowAddr
	for at := 0; at < noWindowResidentFacts+64; at++ {
		if err := run.seedFact(ctx, windowEvent(addr, fmt.Sprintf("restart-%05d", at), "inv-window-earlier", "the material the restart replays", event.TypeExternalInput)); err != nil {
			t.Fatalf("seed the replayed fact: %v", err)
		}
	}
	if err := run.seedFact(ctx, windowEvent(addr, "restart-closing", "inv-window-earlier",
		"the answer the earlier process delivered", event.TypeAgentOutput)); err != nil {
		t.Fatalf("seed the closing fact: %v", err)
	}
	run.trimSeededFacts()
	if got := len(run.SessionFacts(addr)); got != noWindowResidentFacts {
		t.Fatalf("the replayed chain = %d facts, want the window %d", got, noWindowResidentFacts)
	}

	channel := &windowTestChannel{}
	host := &Host{
		run:           run,
		entry:         "agent-a",
		sender:        channel,
		userAddresses: map[event.SessionID]event.Address{},
	}
	turn, err := host.Invoke(ctx, InvokeRequest{
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "the first turn after the restart"}},
	})
	if err != nil {
		t.Fatalf("invoke the first turn after the restart: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.InvocationID == "" {
		t.Fatalf("the turn = (%v, %q), want a completed turn beside its invocation", turn.Terminal, turn.InvocationID)
	}
	if bodies := channel.bodies(); len(bodies) != 1 || bodies[0] != factWindowAnswer {
		t.Fatalf("delivered bodies = %q, want the one answer of the turn %q", bodies, factWindowAnswer)
	}
	bodies := generatedBodiesOf(run.SessionFacts(addr), turn.InvocationID)
	if len(bodies) != 1 || bodies[0].content != factWindowAnswer {
		t.Fatalf("the bodies the turn invocation names = %+v, want the one delivered answer", bodies)
	}
	if got := len(run.SessionFacts(addr)); got > noWindowResidentFacts {
		t.Fatalf("the chain beside the turn = %d facts, want no more than the window %d", got, noWindowResidentFacts)
	}
	requireFactOrderMatchesLedger(t, run)
}

// TestConfigChangeMapsNoProjectionEntry pins the explicit type skip of a configuration change at the projection boundary.
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestConfigChangeMapsNoProjectionEntry(t *testing.T) {
	at := time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)
	change := event.ConfigChangeFor("user", event.ConfigChangeDimensionPrompt, 3, "agent user prompt source changed: [boot.md] -> [next.md]")
	committed := change.Event(at)
	if committed.Message.Content == "" {
		t.Fatal("the committed config change carries no message body, so the empty-payload skip would explain the mapping alone")
	}
	if entry, mapped := mappedEntryOf(committed); mapped {
		t.Fatalf("config change mapped as %+v, want no projection entry: the fact is control-plane material", entry)
	}
	stamped := committed
	stamped.Owner = event.Address{AgentID: "user", SessionID: "session-stamped"}
	if entry, mapped := mappedEntryOf(stamped); mapped {
		t.Fatalf("session-stamped config change mapped as %+v, want no projection entry: the skip is by type", entry)
	}

	ordinary := event.Event{
		Key:       "ordinary-output",
		Type:      event.TypeAgentOutput,
		Timestamp: at,
		Message:   model.Message{Role: model.RoleAssistant, Content: "the answer a request reads"},
	}
	if _, mapped := mappedEntryOf(ordinary); !mapped {
		t.Fatal("an ordinary transcript fact mapped onto no entry, want the mapping intact beside the type skip")
	}
}

// TestMappedEntryCarriesInstanceID pins the recorder generation the fact→projection mapping copies onto every mapped entry.
//   - the entry a committed fact maps onto carries the exact InstanceID of that fact's envelope, whole and never truncated
//   - a restarted process replays the earlier generation's facts beside its own commits
//   - two recorder generations therefore stand in one mapped sequence and stay distinguishable
//   - the sequence is read through the run's public replay read, so the check drives the assembly path
//   - the check never calls the mapping itself
//   - the mapping is checked fact by fact: every public entry read back through the replay read face names exactly the generation its fact's envelope carried
//   - both recorder generations are present in the one mapped sequence
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestMappedEntryCarriesInstanceID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-generation"}
	invoke := func(run *Run, invocation, body string) {
		t.Helper()
		if _, err := run.Invoke(ctx, InvokeRequest{
			InvocationID:  event.InvocationID(invocation),
			Address:       addr,
			TriggerSource: "user",
			Input:         []model.Message{{Role: model.RoleUser, Content: body}},
		}); err != nil {
			t.Fatalf("invoke %q: %v", invocation, err)
		}
	}
	generationOf := func(run *Run, invocation string) event.InstanceID {
		t.Helper()
		generation := event.InstanceID("")
		for _, fact := range run.SessionFacts(addr) {
			if fact.InvocationID == event.InvocationID(invocation) && fact.InstanceID != "" {
				generation = fact.InstanceID
			}
		}
		if generation == "" {
			t.Fatalf("the facts of invocation %q carry no recorder generation", invocation)
		}
		return generation
	}

	first := snapshotTestRun(t, ctx, storage())
	invoke(first, "inv-generation-before", "the turn before the restart")
	before := generationOf(first, "inv-generation-before")
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the first process: %v", err)
	}

	restarted := snapshotTestRun(t, ctx, storage())
	invoke(restarted, "inv-generation-after", "the turn after the restart")
	after := generationOf(restarted, "inv-generation-after")
	if before == after {
		t.Fatalf("the restarted process reused the earlier recorder generation %q, want its own", before)
	}

	facts := map[event.EventID]event.InstanceID{}
	for _, fact := range restarted.SessionFacts(addr) {
		facts[fact.Key] = fact.InstanceID
	}
	entries, carried := restarted.ReplayEntries(addr.SessionID)
	if !carried || len(entries) == 0 {
		t.Fatalf("the restarted run registered no mapped sequence: carried=%v entries=%d", carried, len(entries))
	}
	seen := map[event.InstanceID]bool{}
	for _, entry := range entries {
		generation := entry.Generation()
		if generation == "" {
			t.Fatalf("mapped entry %q carries no recorder generation, want the generation of the fact it was minted from", entry.Key)
		}
		fromFact, held := facts[entry.Key]
		if !held {
			t.Fatalf("mapped entry %q names no fact of the chain", entry.Key)
		}
		if generation != fromFact {
			t.Fatalf("mapped entry %q generation = %q, want the fact's exact InstanceID %q", entry.Key, generation, fromFact)
		}
		seen[generation] = true
	}
	if !seen[before] || !seen[after] {
		t.Fatalf("mapped generations = %v, want the replayed %q beside the restarted process's own %q", seen, before, after)
	}
}

// factWindowTestRun assembles one runtime whose provider answers a turn with one
// plain assistant body and no tool call, so a drive commits its input fact beside
// its turn-end fact alone.
func factWindowTestRun(t *testing.T, ctx context.Context) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}},
			Done:    true,
		}
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	natives := map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}
	run, err := Assemble(ctx, decl, natives, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// windowEvent builds one fact of these checks under the invocation it belongs to.
func windowEvent(addr event.Address, key, invocation, body, kind string) event.Event {
	role := model.RoleUser
	if kind == event.TypeAgentOutput {
		role = model.RoleAssistant
	}
	return event.Event{
		Key:          event.EventID(key),
		Type:         kind,
		Timestamp:    time.Now(),
		Message:      model.Message{Role: role, Content: body},
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: event.InvocationID(invocation),
	}
}

// commitWindowFact commits one fact through the live commit entry of the run.
func commitWindowFact(t *testing.T, ctx context.Context, run *Run, addr event.Address, evt event.Event) {
	t.Helper()
	if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
		t.Fatalf("commit the fact %s: %v", evt.Key, err)
	}
}

// requireFactOrderMatchesLedger requires the commit order of the run to name only
// facts the live ledger still holds, and every one of them once.
func requireFactOrderMatchesLedger(t *testing.T, run *Run) {
	t.Helper()
	run.mu.Lock()
	defer run.mu.Unlock()
	if len(run.factOrder) != len(run.facts) {
		t.Fatalf("the commit order names %d facts beside the %d the ledger holds", len(run.factOrder), len(run.facts))
	}
	for _, key := range run.factOrder {
		if _, held := run.facts[key]; !held {
			t.Fatalf("the commit order names the dropped fact %s", key)
		}
	}
}

// windowTestChannel is the outbound face these checks hand the host: it keeps
// every body it accepted and accepts all of them.
type windowTestChannel struct {
	mu   sync.Mutex
	sent []string
}

// Send records one accepted body and reports the delivery as landed.
func (c *windowTestChannel) Send(_ context.Context, _ event.Address, content string) error {
	c.mu.Lock()
	c.sent = append(c.sent, content)
	c.mu.Unlock()
	return nil
}

// bodies reports the bodies the channel accepted, in the order it took them.
func (c *windowTestChannel) bodies() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}

// deliveryFoldDialogue builds the conversational material of one session: four
// complete turns, each an input closed by an answer.
func deliveryFoldDialogue(at time.Time) []replay.ProjectionEntry {
	entries := make([]replay.ProjectionEntry, 0, 8)
	for turn := 0; turn < 4; turn++ {
		entries = append(entries,
			replay.ProjectionEntry{
				Key:     event.EventID("input-" + string(rune('a'+turn))),
				Type:    event.TypeExternalInput,
				Summary: "the turn input",
				Content: "the input of the turn",
				Time:    at.Add(time.Duration(turn) * time.Minute),
			},
			replay.ProjectionEntry{
				Key:     event.EventID("answer-" + string(rune('a'+turn))),
				Type:    event.TypeAgentOutput,
				Summary: "the turn answer",
				Content: "the answer of the turn",
				Time:    at.Add(time.Duration(turn)*time.Minute + time.Second),
			},
		)
	}
	return entries
}

// deliveryFoldProjection builds one projection over the given entries.
func deliveryFoldProjection(ctx context.Context, entries []replay.ProjectionEntry) *replay.ReplayProjection {
	projection := replay.NewReplayProjection()
	for _, entry := range entries {
		projection.Append(ctx, entry)
	}
	return projection
}

// TestPinnedDeliveryFactsSpendTheWindowNotTheFoldProducts pins what a presented receipt spends.
//
// - A pinned (held or owed) settlement fact is presented by the session but never enters the segments or the products.
// - The same dialogue mints the same products with and without an unsettled obligation.
// - The obligation stands after the second turn, where a live held fact lands: in the tail of the material, never as the input that opens a turn.
func TestPinnedDeliveryFactsSpendTheWindowNotTheFoldProducts(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)
	dialogue := deliveryFoldDialogue(at)
	held := replay.ProjectionEntry{
		Key:     event.EventID("delivery.delivery-fresh-8-abc.held.approval"),
		Type:    deliverySettlementType,
		Summary: deliverySettlementSummary,
		Content: "delivery held approval: delivery-fresh-8-abc -> user@session (35 bytes) reason=approval",
		Time:    at.Add(90 * time.Second),
	}
	withDelivery := append(append(append([]replay.ProjectionEntry{}, dialogue[:4]...), held), dialogue[4:]...)

	accounted := deliveryFoldProjection(ctx, dialogue)
	presented := deliveryFoldProjection(ctx, withDelivery)
	if got, want := len(presented.Entries()), len(accounted.Entries())+1; got != want {
		t.Fatalf("the presented projection carries %d entries, want the %d of the dialogue plus the held fact", got, want)
	}

	plan := replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 300}
	plain, err := foldProjectionOf(ctx, accounted).PlanByAge(ctx, plan)
	if err != nil {
		t.Fatalf("plan the dialogue: %v", err)
	}
	guarded, err := foldProjectionOf(ctx, presented).PlanByAge(ctx, plan)
	if err != nil {
		t.Fatalf("plan the presented projection: %v", err)
	}

	if !plain.Triggered || !guarded.Triggered {
		t.Fatalf("the four-turn dialogue over the 300-byte threshold did not fold: plain=%+v guarded=%+v", plain, guarded)
	}
	if guarded.Volume != plain.Volume {
		t.Fatalf("the pinned settlement fact entered the fold view: plain=%d guarded=%d", plain.Volume, guarded.Volume)
	}
	if len(guarded.Products) != len(plain.Products) {
		t.Fatalf("the pinned settlement fact moved the fold products: plain=%d guarded=%d", len(plain.Products), len(guarded.Products))
	}
	for i := range plain.Products {
		if plain.Products[i].Entry.Key != guarded.Products[i].Entry.Key ||
			len(plain.Products[i].Covered) != len(guarded.Products[i].Covered) {
			t.Fatalf("product %d differs beside the pinned fact: plain=%+v guarded=%+v", i, plain.Products[i], guarded.Products[i])
		}
	}
	if got := len(foldProjectionOf(ctx, presented).Entries()); got != len(accounted.Entries()) {
		t.Fatalf("the fold view carries %d entries, want the %d of the dialogue alone", got, len(accounted.Entries()))
	}
	for _, entry := range presented.Entries() {
		if entry.Type == deliverySettlementType {
			return
		}
	}
	t.Fatalf("the presented projection dropped the held settlement fact: %+v", presented.Entries())
}

// receiptRenderedBytes reports the rendered volume the delivery settlement facts
// of one projection carry, measured entry by entry the way the projection renders
// them: the checks below read what a receipt costs the projection beside the
// conversation without the window carrying a measure of its own.
func receiptRenderedBytes(entries []replay.ProjectionEntry) int {
	volume := 0
	for _, entry := range entries {
		if entry.Type != deliverySettlementType {
			continue
		}
		volume += len(replay.RenderProjection([]replay.ProjectionEntry{entry}))
	}
	return volume
}

// TestPinnedDeliveryFactsNeverMoveTheFoldThreshold pins what a receipt leaves alone.
//
// - The threshold the fold judges under is the window's own budget, with or without receipts beside the conversation.
// - The fold view is the dialogue alone, byte for byte, however many receipts the session presents.
// - The presented projection renders the dialogue plus its receipts, so the receipts are material the request carries.
func TestPinnedDeliveryFactsNeverMoveTheFoldThreshold(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)
	dialogue := deliveryFoldDialogue(at)
	withDelivery := append([]replay.ProjectionEntry{}, dialogue...)
	for i := 0; i < 3; i++ {
		withDelivery = append(withDelivery, replay.ProjectionEntry{
			Key:     event.EventID("delivery.delivery-fresh-" + string(rune('a'+i)) + ".sent"),
			Type:    deliverySettlementType,
			Summary: deliverySettlementSummary,
			Content: "delivery sent: delivery-fresh-" + string(rune('a'+i)) + " -> user@session (35 bytes)",
			Time:    at.Add(time.Duration(i) * time.Second),
		})
	}
	window := FoldWindow{MaxTokens: 1000}
	budget := window.VolumeThresholdBytes()
	if want := int(float64(1000*replay.BudgetCharsPerToken) * config.DefaultCompressThreshold); budget != want {
		t.Fatalf("the threshold = %d bytes, want the window's own %d-byte budget", budget, want)
	}
	if accounted := (FoldWindow{MaxTokens: 1000, KeepRecentTasks: 1}).VolumeThresholdBytes(); accounted != budget {
		t.Fatalf("the threshold = %d bytes beside one recent task kept whole, want the same %d-byte budget", accounted, budget)
	}

	plain := deliveryFoldProjection(ctx, dialogue)
	presented := deliveryFoldProjection(ctx, withDelivery)
	if got, want := len(foldProjectionOf(ctx, presented).Entries()), len(plain.Entries()); got != want {
		t.Fatalf("the fold view carries %d entries, want the %d of the dialogue alone", got, want)
	}
	if settled, presented := len(replay.RenderProjection(foldProjectionOf(ctx, presented).Entries())), len(replay.RenderProjection(presented.Entries())); settled != len(replay.RenderProjection(plain.Entries())) || presented <= settled {
		t.Fatalf("the presented projection renders %d bytes beside the %d of the fold view, want the receipts rendering no less than them", presented, settled)
	}
	if spent := receiptRenderedBytes(withDelivery); spent == 0 {
		t.Fatalf("the three settlement facts render no volume: %+v", withDelivery)
	}
}

// TestDeliverySettlementFactsStayOutOfTheFoldView pins both halves of the split directly.
//
// - The rendered volume of the fold view is the rendered volume of the dialogue, byte for byte, however many settlement facts stand beside it.
// - The rendered projection the model reads is that volume plus every receipt, and the two never share a byte.
func TestDeliverySettlementFactsStayOutOfTheFoldView(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)
	dialogue := deliveryFoldDialogue(at)
	withDelivery := append([]replay.ProjectionEntry{}, dialogue...)
	for i := 0; i < 3; i++ {
		withDelivery = append(withDelivery, replay.ProjectionEntry{
			Key:     event.EventID("delivery.delivery-fresh-" + string(rune('a'+i)) + ".sent"),
			Type:    deliverySettlementType,
			Summary: deliverySettlementSummary,
			Content: "delivery sent: delivery-fresh-" + string(rune('a'+i)) + " -> user@session (35 bytes)",
			Time:    at.Add(time.Duration(i) * time.Second),
		})
	}
	presented := deliveryFoldProjection(ctx, withDelivery)
	plain := foldProjectionOf(ctx, deliveryFoldProjection(ctx, dialogue))
	guarded := foldProjectionOf(ctx, presented)
	report, err := plain.PlanByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1})
	if err != nil {
		t.Fatalf("plan the dialogue: %v", err)
	}
	other, err := guarded.PlanByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1})
	if err != nil {
		t.Fatalf("plan the settlement-beside dialogue: %v", err)
	}
	if report.Volume != other.Volume {
		t.Fatalf("the fold view carried the settlement facts: plain=%d guarded=%d", report.Volume, other.Volume)
	}
	for i := range plain.Entries() {
		if plain.Entries()[i].Key != guarded.Entries()[i].Key {
			t.Fatalf("the fold view entry %d is %s, want %s", i, guarded.Entries()[i].Key, plain.Entries()[i].Key)
		}
	}
	rendered := len(replay.RenderProjection(presented.Entries()))
	spent := receiptRenderedBytes(presented.Entries())
	if spent == 0 {
		t.Fatalf("the three settlement facts render no volume: %+v", presented.Entries())
	}
	if rendered != other.Volume+spent {
		t.Fatalf("the projection the model reads renders %d bytes, want the %d of the dialogue plus the %d of its receipts", rendered, other.Volume, spent)
	}
}

// TestSettlementRoleMappedBesideTheFoldView keeps the two halves of the contract together.
//
// - The settlement type maps onto a model role (the fact is presented), even though the fold mints its products on a view that excludes it.
func TestSettlementRoleMappedBesideTheFoldView(t *testing.T) {
	role, err := agent.RoleOf(deliverySettlementType)
	if err != nil {
		t.Fatalf("the settlement type maps onto no role: %v", err)
	}
	if role != model.RoleUser {
		t.Fatalf("the settlement type maps onto %q, want the user role", role)
	}
}

// foldJudgeFace is the session face one age fold plugin judges through here: the
// projection and window it answers, and the judgments it recorded. It reports the
// window the way the run's own face does, so the criterion under check is the
// criterion the plugin runs in production.
type foldJudgeFace struct {
	projection *replay.ReplayProjection
	window     FoldWindow
	filed      []event.CompressRequest
}

// SessionOf answers the one chain this face holds.
func (f *foldJudgeFace) SessionOf(context.Context, event.EventID) (event.SessionID, bool) {
	return "session-fold-judge", true
}

// ProjectionOf answers the projection beside the window it was registered under.
func (f *foldJudgeFace) ProjectionOf(context.Context, event.SessionID) (*replay.ReplayProjection, FoldWindow, bool) {
	return f.projection, f.window, true
}

// FileCompressRequest records the judgment the plugin filed.
func (f *foldJudgeFace) FileCompressRequest(_ context.Context, _ event.SessionID, request event.CompressRequest) error {
	f.filed = append(f.filed, request)
	return nil
}

// foldWindowOfBytes reports the window whose budget covers the given rendered
// volume, so a fixture states the conversation it puts under judgement and the
// bound it is judged against in the same unit the fold measures. The share of a
// window is rounded down to whole tokens, so the declaration rounds the window up.
func foldWindowOfBytes(bytes int) FoldWindow {
	budgetPerToken := float64(replay.BudgetCharsPerToken) * config.DefaultCompressThreshold
	return FoldWindow{MaxTokens: int(float64(bytes)/budgetPerToken) + 1}
}

// TestFoldCriterionJudgesTheConversationNotTheReceipts pins what trips a fold.
//
// - A conversation inside the window's budget is not judged due however many receipts stand beside it.
// - The same conversation under a window it crosses is judged due, so the check never passes vacuously.
func TestFoldCriterionJudgesTheConversationNotTheReceipts(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)
	receipt := replay.ProjectionEntry{
		Key:     event.EventID("delivery.delivery-fresh-8-abc.held.approval"),
		Type:    deliverySettlementType,
		Summary: deliverySettlementSummary,
		Content: "delivery held approval: delivery-fresh-8-abc -> user@session (35 bytes) reason=approval",
		Time:    at.Add(90 * time.Second),
	}
	presented := deliveryFoldProjection(ctx, append(deliveryFoldDialogue(at), receipt))
	conversation := len(replay.RenderProjection(foldProjectionOf(ctx, presented).Entries()))
	rendered := len(replay.RenderProjection(presented.Entries()))
	if conversation == 0 || rendered <= conversation {
		t.Fatalf("the fixture renders conversation=%d beside the whole %d, want receipts the projection carries", conversation, rendered)
	}

	roomy := foldWindowOfBytes(conversation)
	if budget := roomy.VolumeThresholdBytes(); budget < conversation {
		t.Fatalf("the roomy window leaves %d bytes for a %d-byte conversation, want it inside", budget, conversation)
	}
	if budget := roomy.VolumeThresholdBytes(); budget >= rendered {
		t.Fatalf("the roomy window leaves %d bytes for the %d-byte projection, want the receipts rendering over it", budget, rendered)
	}
	quiet := &foldJudgeFace{projection: presented, window: roomy}
	if err := NewAgeFoldPlugin(quiet).ObserveCommit(ctx, event.EventID("delivery.delivery-fresh-8-abc.held.approval")); err != nil {
		t.Fatalf("judge the presented projection: %v", err)
	}
	if len(quiet.filed) != 0 {
		t.Fatalf("the %d-byte conversation inside its %d-byte budget was judged due by %d product(s): the receipts beside it tripped the fold",
			conversation, roomy.VolumeThresholdBytes(), len(quiet.filed))
	}

	tight := foldWindowOfBytes(conversation / 2)
	if budget := tight.VolumeThresholdBytes(); budget >= conversation {
		t.Fatalf("the tight window leaves %d bytes for a %d-byte conversation, want it over", budget, conversation)
	}
	due := &foldJudgeFace{projection: presented, window: tight}
	if err := NewAgeFoldPlugin(due).ObserveCommit(ctx, event.EventID("delivery.delivery-fresh-8-abc.held.approval")); err != nil {
		t.Fatalf("judge the over-budget conversation: %v", err)
	}
	if len(due.filed) == 0 {
		t.Fatalf("the %d-byte conversation over its %d-byte budget was judged due by nothing", conversation, tight.VolumeThresholdBytes())
	}
}

// TestFoldCriterionBoundsTheConversationAsTheChainGrows pins the bound over a live chain.
//
// - The conversation the fold judges never renders more than the window's budget as the chain grows.
// - The ceiling does not move with the history: a 48-turn chain is held at the same budget as a 12-turn one.
// - The material a long chain carries is the folded form, not the whole chain.
// - The fold converges: once the bands stop promoting, the conversation stands inside the budget.
// - The receipts the projection carries stay the set the fold has not overtaken, not one per turn.
func TestFoldCriterionBoundsTheConversationAsTheChainGrows(t *testing.T) {
	ctx := context.Background()
	window := FoldWindow{MaxTokens: 4000}
	budget := window.VolumeThresholdBytes()
	short := commitDeliveryGrowthChain(t, ctx, window, 12)
	long := commitDeliveryGrowthChain(t, ctx, window, 48)
	t.Logf("[fold-bound] budget=%d bytes, one turn renders %d: 12 turns peak=%d settled=%d receipts=%d; 48 turns peak=%d settled=%d receipts=%d",
		budget, short.perTurn, short.peak, short.settled, short.spent, long.peak, long.settled, long.spent)
	if 48*long.perTurn <= budget {
		t.Fatalf("the 48-turn chain renders %d bytes unfolded, want a chain that crosses the %d-byte budget", 48*long.perTurn, budget)
	}
	if short.peak > budget || long.peak > budget {
		t.Fatalf("the conversation peaked at %d (12 turns) and %d (48 turns) bytes, want both inside the %d-byte budget",
			short.peak, long.peak, budget)
	}
	if short.settled > budget || long.settled > budget {
		t.Fatalf("the conversation settled at %d (12 turns) and %d (48 turns) bytes, want both inside the %d-byte budget", short.settled, long.settled, budget)
	}
	if short.spent == 0 || long.spent == 0 {
		t.Fatalf("the chain presented no receipt beside the conversation: %d bytes at 12 turns, %d at 48", short.spent, long.spent)
	}
	if long.spent > short.spent+2*deliveryGrowthReceiptBytes {
		t.Fatalf("the receipts the projection carries grew with the chain: %d bytes at 12 turns, %d at 48", short.spent, long.spent)
	}
}

// deliveryGrowthReceiptBytes bounds the rendered volume one growth receipt adds;
// the chain asserts that the receipts a long projection carries stay the handful
// the fold has not overtaken rather than one per turn of the history.
const deliveryGrowthReceiptBytes = 400

// deliveryGrowthVolume is what one growth chain reads: the ceiling the
// conversation reached over the whole chain, its volume once the fold converged,
// the volume the receipts the projection still presents render, and the volume
// one turn of the chain renders.
type deliveryGrowthVolume struct {
	peak    int
	settled int
	spent   int
	perTurn int
}

// commitDeliveryGrowthChain commits closed turns of the growth chain beside one
// delivery settlement receipt per turn, and reports what the projection the model
// reads carried beside it.
func commitDeliveryGrowthChain(t *testing.T, ctx context.Context, window FoldWindow, turns int) deliveryGrowthVolume {
	t.Helper()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: event.SessionID(fmt.Sprintf("session-fold-bound-%d", turns))}
	if err := run.AttachReplay(agent.RequestChain{SessionID: addr.SessionID}, window, replay.NewReplayProjection()); err != nil {
		t.Fatalf("register the window of the growth chain: %v", err)
	}
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	var volume deliveryGrowthVolume
	for turn := 0; turn < turns; turn++ {
		for position, leg := range deliveryGrowthLegs {
			commitWindowFact(t, ctx, run, addr, deliveryGrowthTurnFact(addr, base, turn, position, leg.typ, leg.summary))
		}
		commitWindowFact(t, ctx, run, addr, deliveryGrowthReceipt(addr, base, turn))
		projection, _, carried := ageFoldSessions{run}.ProjectionOf(ctx, addr.SessionID)
		if !carried {
			t.Fatalf("turn %d: the growth chain registered no projection", turn)
		}
		if rendered := len(replay.RenderProjection(foldProjectionOf(ctx, projection).Entries())); rendered > volume.peak {
			volume.peak = rendered
		}
	}
	projection, windowed, carried := ageFoldSessions{run}.ProjectionOf(ctx, addr.SessionID)
	if !carried {
		t.Fatalf("the growth chain registered no projection")
	}
	volume.spent = receiptRenderedBytes(projection.Entries())
	volume.settled = settleDeliveryGrowth(ctx, t, projection, windowed)
	volume.perTurn = len(replay.RenderProjection(deliveryGrowthTurnEntries(addr, base, 0)))
	return volume
}

// deliveryGrowthLegs names the four legs one closed growth turn commits, in the
// order the turn commits them.
var deliveryGrowthLegs = []struct {
	typ     string
	summary string
}{
	{event.TypeExternalInput, "growth ask"},
	{event.TypeThinkingPlan, "growth plan"},
	{event.TypeActionCommand, "growth call"},
	{event.TypeAgentOutput, "growth answer"},
}

// deliveryGrowthTurnEntries reports the projection entries one growth turn adds:
// its four legs beside the receipt it is answered with, so the ceiling the chain
// asserts is read from the material the chain itself commits.
func deliveryGrowthTurnEntries(addr event.Address, base time.Time, turn int) []replay.ProjectionEntry {
	entries := make([]replay.ProjectionEntry, 0, len(deliveryGrowthLegs)+1)
	for position, leg := range deliveryGrowthLegs {
		if entry, held := mappedEntryOf(deliveryGrowthTurnFact(addr, base, turn, position, leg.typ, leg.summary)); held {
			entries = append(entries, entry)
		}
	}
	if entry, held := mappedEntryOf(deliveryGrowthReceipt(addr, base, turn)); held {
		entries = append(entries, entry)
	}
	return entries
}

// deliveryGrowthTurnFact names one leg of one growth turn.
func deliveryGrowthTurnFact(addr event.Address, base time.Time, turn, position int, typ, summary string) event.Event {
	return event.Event{
		Key:       event.EventID(fmt.Sprintf("growth-%03d-%d", turn, position)),
		Type:      typ,
		Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
		Meta:      map[string]string{event.MetaKeyEventSummary: summary},
		Message:   model.Message{Role: model.RoleUser, Content: "the material of growth turn ................"},
		Owner:     addr,
		Source:    addr,
		Target:    addr,
	}
}

// deliveryGrowthReceipt names the settlement receipt one growth turn is answered
// with: the obligation the model reads beside the turn.
func deliveryGrowthReceipt(addr event.Address, base time.Time, turn int) event.Event {
	return event.Event{
		Key:       event.EventID(fmt.Sprintf("delivery.delivery-growth-%03d.held.approval", turn)),
		Type:      deliverySettlementType,
		Timestamp: base.Add(time.Duration(turn*10+4) * time.Minute),
		Meta:      map[string]string{event.MetaKeyEventSummary: deliverySettlementSummary},
		Message: model.Message{Role: model.RoleUser,
			Content: fmt.Sprintf("delivery held approval: delivery-growth-%03d -> agent-a@session (32 bytes) reason=approval", turn)},
		Owner:  addr,
		Source: addr,
		Target: addr,
	}
}

// settleDeliveryGrowth drives the fold of one projection to its fixed point: the
// plan is judged exactly as the consumer judges it, on the conversation material,
// and each product it mints is applied with the settlement facts of its band
// covered, so a round that mints nothing is the round the fold has converged. It
// reports the rendered volume of the conversation at that point.
func settleDeliveryGrowth(ctx context.Context, t *testing.T, projection *replay.ReplayProjection, window FoldWindow) int {
	t.Helper()
	settled := replay.NewReplayProjection()
	for _, entry := range projection.Entries() {
		settled.Append(ctx, entry)
	}
	for round := 0; round < 8; round++ {
		report, err := foldProjectionOf(ctx, settled).PlanByAge(ctx, replay.AgeFoldPlan{
			KeepRecent:      window.KeepRecentTasks,
			VolumeThreshold: window.VolumeThresholdBytes(),
		})
		if err != nil {
			t.Fatalf("judge the growth projection: %v", err)
		}
		if len(report.Products) == 0 {
			break
		}
		for _, product := range report.Products {
			settled.ApplyProducts([]replay.FoldProduct{coverSettlementFacts(product, settled)})
		}
	}
	return len(replay.RenderProjection(foldProjectionOf(ctx, settled).Entries()))
}

// residencyTestRun assembles one run over the in-process storage tier, so the
// fact window checks read the durable chain without touching a directory.
func residencyTestRun(t *testing.T, ctx context.Context) *Run {
	t.Helper()
	return snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
}

// commitResidencyChain commits a chain long enough to leave the fact window and
// closes it with the turn-end fact the trim reads.
func commitResidencyChain(t *testing.T, ctx context.Context, run *Run, addr event.Address, prefix string, seeded int) {
	t.Helper()
	for at := 0; at < seeded; at++ {
		commitWindowFact(t, ctx, run, addr, windowEvent(addr,
			fmt.Sprintf("%s-%05d", prefix, at), "inv-residency", "the material of the long chain", event.TypeExternalInput))
	}
	commitWindowFact(t, ctx, run, addr, windowEvent(addr,
		prefix+"-closing", "inv-residency", "the answer that closed the chain", event.TypeAgentOutput))
}

// TestAssemblyReplayFactsBounded pins live bounds beside complete durable reads.
// - the live fact ledger and replay sequence stay bounded while the durable query returns every committed fact
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAssemblyReplayFactsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := residencyTestRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-residency-bounded"}
	const seeded = noWindowResidentFacts + 256
	commitResidencyChain(t, ctx, run, addr, "bounded", seeded)

	if got := run.CommittedFacts(); got != noWindowResidentFacts {
		t.Fatalf("the live fact ledger holds %d facts, want the %d-fact window", got, noWindowResidentFacts)
	}
	refs, err := run.ObservedEvents(ctx, memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName(string(addr.AgentID))}})
	if err != nil {
		t.Fatalf("read the durable history: %v", err)
	}
	chainFacts := 0
	for _, ref := range refs {
		if ref.Type != event.TypeCheckpoint {
			chainFacts++
		}
	}
	if chainFacts != seeded+1 {
		t.Fatalf("the durable read answered %d chain facts beside the %d checkpoints, want all %d committed facts plus the closing output",
			chainFacts, len(refs)-chainFacts, seeded+1)
	}
	entries, carried := run.ReplayEntries(addr.SessionID)
	if !carried {
		t.Fatal("the long chain registered no replay sequence")
	}
	if len(entries) > 2*noWindowResidentFacts {
		t.Fatalf("the registered sequence holds %d entries, want at most twice the fallback count %d", len(entries), 2*noWindowResidentFacts)
	}
}

// TestAssemblyColdWindowReadback pins the read-back equivalence of a fact that left the window.
// - the durable query still answers it
// - the identity read returns the committed fact byte for byte
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestAssemblyColdWindowReadback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := residencyTestRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-residency-cold"}
	const seeded = noWindowResidentFacts + 32
	commitResidencyChain(t, ctx, run, addr, "cold", seeded)

	oldKey := event.EventID("cold-00000")
	if _, held := run.factOf(oldKey); held {
		t.Fatalf("the fact %s is still in the live ledger, so the cold read proves nothing", oldKey)
	}
	refs, err := run.ObservedEvents(ctx, memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName(string(addr.AgentID))}})
	if err != nil {
		t.Fatalf("read the durable history: %v", err)
	}
	found := false
	for _, ref := range refs {
		if ref.Key == oldKey {
			found = true
		}
	}
	if !found {
		t.Fatalf("the durable read lost the cold fact %s", oldKey)
	}
	restored, err := run.CommittedEvent(ctx, oldKey)
	if err != nil {
		t.Fatalf("read the fact %s back from the durable chain: %v", oldKey, err)
	}
	if restored.Key != oldKey || restored.Type != event.TypeExternalInput || restored.Message.Content != "the material of the long chain" {
		t.Fatalf("the cold read restored %+v, want the committed fact %s", restored, oldKey)
	}
	hotKey := event.EventID(fmt.Sprintf("cold-%05d", seeded-1))
	hot, err := run.CommittedEvent(ctx, hotKey)
	if err != nil || hot.Key != hotKey {
		t.Fatalf("read the in-window fact %s = (%+v, %v)", hotKey, hot, err)
	}
}

// TestDeliveryPendingPredicate pins the predicate the consumer registry reads before reclamation.
// - a pinned settlement fact of an unsettled obligation names the address its delivery still owes
// - releasing the pin clears the answer
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveryPendingPredicate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := residencyTestRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-residency-pending"}
	other := event.Address{AgentID: "agent-b", SessionID: "session-residency-pending"}
	heldKey := event.EventID(deliveryFactPrefix + "obligation-1.held.retrying")
	held := windowEvent(addr, string(heldKey), "inv-delivery", "the delivery is waiting on a retry", deliverySettlementType)
	if _, err := run.CommitToSession(ctx, addr, held); err != nil {
		t.Fatalf("commit the held settlement fact: %v", err)
	}
	if run.deliveryPendingFor(addr) {
		t.Fatal("an unpinned settlement fact reads as an unsettled delivery")
	}
	run.pinFact(heldKey)
	if !run.deliveryPendingFor(addr) {
		t.Fatal("the pinned held(retrying) obligation reads as settled, want pending")
	}
	if run.deliveryPendingFor(other) {
		t.Fatal("the obligation of one address reads as pending for another")
	}
	run.unpinFact(heldKey)
	if run.deliveryPendingFor(addr) {
		t.Fatal("the released obligation still reads as pending")
	}
}

// TestWindowedFactsFollowTheProjection pins the derived residency of one session chain.
// - every fact before the oldest original the projection still presents is released
// - the facts from that entry onward stay resident beside the pinned ones
// - a chain carrying no window keeps the fallback count beside its segment in flight
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestWindowedFactsFollowTheProjection(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-windowed-residency"}
	build := func(t *testing.T, facts int) *Run {
		t.Helper()
		run := &Run{
			facts:            map[event.EventID]event.Event{},
			sessionFactCount: map[event.SessionID]int{},
			pinnedFacts:      map[event.EventID]struct{}{},
			replays:          map[event.Address]replayEntry{},
		}
		for at := 1; at <= facts; at++ {
			evt := windowEvent(addr, fmt.Sprintf("resident-%04d", at), "inv-windowed", "material", event.TypeExternalInput)
			run.facts[evt.Key] = evt
			run.factOrder = append(run.factOrder, evt.Key)
			run.sessionFactCount[addr.SessionID]++
		}
		return run
	}

	t.Run("the projection boundary bounds the resident facts", func(t *testing.T) {
		run := build(t, 8)
		projection := replay.NewReplayProjection()
		for _, key := range []string{"resident-0005", "resident-0006"} {
			projection.Append(ctx, replay.ProjectionEntry{
				Key: event.EventID(key), Type: event.TypeExternalInput, Content: "presented", Time: time.Now(),
			})
		}
		run.replays[addr] = replayEntry{projection: projection, window: FoldWindow{MaxTokens: 128}}
		run.pinnedFacts[event.EventID("resident-0003")] = struct{}{}

		run.trimFacts(addr.SessionID)

		for _, key := range []string{"resident-0001", "resident-0002", "resident-0004"} {
			if _, held := run.facts[event.EventID(key)]; held {
				t.Fatalf("the fact %s before the presented boundary is still resident", key)
			}
		}
		for _, key := range []string{"resident-0003", "resident-0005", "resident-0006", "resident-0007", "resident-0008"} {
			if _, held := run.facts[event.EventID(key)]; !held {
				t.Fatalf("the fact %s at or after the presented boundary is released", key)
			}
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("a chain beside no window keeps the fallback count", func(t *testing.T) {
		run := build(t, noWindowResidentFacts+53)
		closing := windowEvent(addr, "resident-closing", "inv-windowed", "the answer that closed the chain", event.TypeAgentOutput)
		run.facts[closing.Key] = closing
		run.factOrder = append(run.factOrder, closing.Key)
		run.sessionFactCount[addr.SessionID]++

		run.trimFacts(addr.SessionID)

		if got := len(run.SessionFacts(addr)); got != noWindowResidentFacts {
			t.Fatalf("the chain beside no window = %d facts, want the fallback count %d", got, noWindowResidentFacts)
		}
		requireFactOrderMatchesLedger(t, run)
	})
}

// TestRestartSameSegmentAfterReplay pins the segment division a restart rebuilds from the chain alone.
//   - the live sequence cuts at the input relation its outputs recorded
//   - the input an output did not name rolls into the next segment
//   - a process restarted over the same durable root rebuilds the same division, segment by segment
//   - the rebuilt division spans every entry of the chain, so the restart neither loses nor mints a segment
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestRestartSameSegmentAfterReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-restart-segment"}
	base := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	commit := func(run *Run, at int, key, typ string, role model.Role, content string, meta map[string]string) {
		t.Helper()
		commitWindowFact(t, ctx, run, addr, event.Event{
			Key:       event.EventID(key),
			Type:      typ,
			Timestamp: base.Add(time.Duration(at) * time.Minute),
			Meta:      meta,
			Message:   model.Message{Role: role, Content: content},
		})
	}
	seedChain := func(run *Run) {
		t.Helper()
		commit(run, 1, "1-in", event.TypeExternalInput, model.RoleUser, "the first input", nil)
		commit(run, 2, "2-in", event.TypeExternalInput, model.RoleUser, "the input the next turn consumes", nil)
		commit(run, 3, "3-out", event.TypeAgentOutput, model.RoleAssistant, "the answer to the first input alone", map[string]string{
			event.MetaKeyInputEvents: `["1-in"]`,
		})
		commit(run, 4, "4-in", event.TypeExternalInput, model.RoleUser, "the third input", nil)
		commit(run, 5, "5-out", event.TypeAgentOutput, model.RoleAssistant, "the answer to both late inputs", map[string]string{
			event.MetaKeyInputEvents: `["2-in","4-in"]`,
		})
	}
	segmentsOf := func(run *Run) []replay.Segment {
		t.Helper()
		entries, carried := run.ReplayEntries(addr.SessionID)
		if !carried {
			t.Fatalf("session %q registered no mapped sequence", addr.SessionID)
		}
		segments := replay.SegmentsOf(entries, replay.DefaultAgeKeepRecent)
		spanned := 0
		for _, segment := range segments {
			spanned += len(segment.Entries)
		}
		if spanned != len(entries) {
			t.Fatalf("the segments span %d entries beside the %d the sequence carries", spanned, len(entries))
		}
		return segments
	}
	divisionOf := func(segments []replay.Segment) [][]event.EventID {
		division := make([][]event.EventID, 0, len(segments))
		for _, segment := range segments {
			keys := make([]event.EventID, 0, len(segment.Entries))
			for _, entry := range segment.Entries {
				keys = append(keys, entry.Key)
			}
			division = append(division, keys)
		}
		return division
	}
	requireDivision := func(what string, got, want [][]event.EventID) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s cuts %v, want %v", what, got, want)
		}
		for at := range want {
			if len(got[at]) != len(want[at]) {
				t.Fatalf("%s cuts %v, want %v", what, got, want)
			}
			for entry := range want[at] {
				if got[at][entry] != want[at][entry] {
					t.Fatalf("%s cuts %v, want %v", what, got, want)
				}
			}
		}
	}
	requireRelationCut := func(what string, segments []replay.Segment) {
		t.Helper()
		for at, segment := range segments {
			if !segment.Complete {
				t.Fatalf("%s segment %d = %+v, want an output closing it", what, at, segment)
			}
			if segment.Legacy() {
				t.Fatalf("%s segment %d = %+v, want the relation the closing output recorded, not the positional cut", what, at, segment)
			}
		}
	}
	want := [][]event.EventID{
		{"1-in", "3-out"},
		{"2-in", "4-in", "5-out"},
	}

	first := snapshotTestRun(t, ctx, storage())
	seedChain(first)
	live := segmentsOf(first)
	requireDivision("the live sequence", divisionOf(live), want)
	requireRelationCut("the live sequence", live)
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the first process: %v", err)
	}

	restarted := snapshotTestRun(t, ctx, storage())
	rebuilt := segmentsOf(restarted)
	requireDivision("the restarted sequence", divisionOf(rebuilt), want)
	requireRelationCut("the restarted sequence", rebuilt)
}

// TestRestartColdStartRecomputesTheWindow pins the deliberate cold start.
// - the registered window follows the declaration this boot published, not the one the chain was written under
// - a chain the previous process left beside an unclosed input presents that input as ended material
// - the restart resumes no range, so nothing carries an ended boundary across processes
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestRestartColdStartRecomputesTheWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-cold-start"}
	boot := func(t *testing.T, maxTokens int) *Run {
		t.Helper()
		provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
			return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
		}}
		run, err := assemble(ctx, &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: maxTokens}},
		}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}, true,
			WithGovernance(snapshotTestGovernance{}),
			WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
		if err != nil {
			t.Fatalf("assemble the run: %v", err)
		}
		return run
	}

	first := boot(t, 60)
	commitWindowFact(t, ctx, first, addr, windowEvent(addr, "1-in", "inv-cold", "the first input", event.TypeExternalInput))
	commitWindowFact(t, ctx, first, addr, windowEvent(addr, "2-out", "inv-cold", "the answer that closed the range", event.TypeAgentOutput))
	commitWindowFact(t, ctx, first, addr, windowEvent(addr, "3-in", "inv-cold", "the input the restart finds unclosed", event.TypeExternalInput))
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the first process: %v", err)
	}

	restarted := boot(t, 4096)
	entry, carried := restarted.replays[addr]
	if !carried {
		t.Fatalf("the restarted run registered no sequence for %v", addr)
	}
	want := foldWindowOf(config.AgentSpec{ID: "agent-a", Model: "model-a", ReplayWindow: 4096})
	if entry.window != want {
		t.Fatalf("the restarted window = %+v, want the %+v this declaration publishes", entry.window, want)
	}
	if carried := foldWindowOf(config.AgentSpec{ID: "agent-a", Model: "model-a", ReplayWindow: 60}); entry.window == carried {
		t.Fatalf("the restarted window = %+v, want the window this declaration publishes rather than the one the chain was written under", entry.window)
	}
	if entry.projection.Len() == 0 {
		t.Fatal("the restarted run replayed no entry, so the cold-start boundary proves nothing")
	}
	if entry.endedAt != "" || entry.rangeOpen {
		t.Fatalf("the restarted registration resumed a range: endedAt=%q rangeOpen=%v, want the deliberate cold start", entry.endedAt, entry.rangeOpen)
	}
	if got, held := entry.endedPrefix(), entry.projection.Len(); got != held {
		t.Fatalf("the restarted prefix ends at %d of %d entries, want every entry presented as ended material", got, held)
	}
}

// TestTrimKeepsTheControlCountConsistent pins the control-plane scan cursor across a trim.
// - dropping facts shrinks the commit order; the cursor and count are recomputed over the kept prefix so a later count reads a valid slice
// - revert the cursor recompute in compactFactOrderLocked and the second count panics on factOrder[controlScanned:] past the shorter order
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestTrimKeepsTheControlCountConsistent(t *testing.T) {
	run := &Run{facts: map[event.EventID]event.Event{}}
	for i := 0; i < 10; i++ {
		key := event.EventID(fmt.Sprintf("s-%02d", i))
		run.facts[key] = event.Event{Key: key}
		run.factOrder = append(run.factOrder, key)
	}
	for i := 0; i < 2; i++ {
		key := event.EventID(fmt.Sprintf("gen-retire:%02d", i))
		run.facts[key] = event.Event{Key: key}
		run.factOrder = append(run.factOrder, key)
	}
	run.countControlFactsLocked()
	if run.controlFacts != 2 || run.controlScanned != 12 {
		t.Fatalf("before trim control=%d scanned=%d, want the two retirement records over twelve facts", run.controlFacts, run.controlScanned)
	}
	dropped := map[event.EventID]struct{}{}
	for i := 0; i < 10; i++ {
		key := event.EventID(fmt.Sprintf("s-%02d", i))
		dropped[key] = struct{}{}
		delete(run.facts, key)
	}
	dropped["gen-retire:00"] = struct{}{}
	delete(run.facts, "gen-retire:00")
	run.compactFactOrderLocked(dropped)
	if run.controlScanned > len(run.factOrder) {
		t.Fatalf("the trim left controlScanned=%d past the order length %d", run.controlScanned, len(run.factOrder))
	}
	run.countControlFactsLocked()
	if run.controlFacts != 1 {
		t.Fatalf("after trim control=%d, want the one surviving retirement record", run.controlFacts)
	}
	if got := run.CommittedFacts(); got != 0 {
		t.Fatalf("committed material facts = %d, want no session fact beside the control record", got)
	}
}

// factCommitEngine is the storage engine seam a fixture wraps: it records the key
// set of every batch and refuses the ones the fixture names before the engine sees
// them.
type factCommitEngine struct {
	ledger.AtomicStore
	mu      sync.Mutex
	batches [][]string
	// refuse answers a batch before the engine commits it.
	refuse func(ledger.Batch) error
	// report answers a batch the engine already committed, so a fixture reads what a
	// commit landed beside an outcome the engine names: an outcome the caller cannot
	// place leaves its keys durable all the same.
	report func(ledger.Batch) error
}

// Commit records the batch it is handed and answers what the fixture named.
func (e *factCommitEngine) Commit(ctx context.Context, batch ledger.Batch) error {
	keys := make([]string, 0, len(batch.Puts))
	for _, put := range batch.Puts {
		keys = append(keys, put.Key)
	}
	e.mu.Lock()
	e.batches = append(e.batches, keys)
	refuse := e.refuse
	report := e.report
	e.mu.Unlock()
	if refuse != nil && len(batch.Puts) > 0 {
		if err := refuse(batch); err != nil {
			return err
		}
	}
	if err := e.AtomicStore.Commit(ctx, batch); err != nil {
		return err
	}
	if report != nil && len(batch.Puts) > 0 {
		return report(batch)
	}
	return nil
}

// Close releases the engine the wrapper holds, so the storage root's write rights
// leave with the view that holds them.
func (e *factCommitEngine) Close() error {
	if closer, held := e.AtomicStore.(interface{ Close() error }); held {
		return closer.Close()
	}
	return nil
}

// written answers the recorded batches that carry key.
func (e *factCommitEngine) written(key string) [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out [][]string
	for _, batch := range e.batches {
		if slices.Contains(batch, key) {
			out = append(out, batch)
		}
	}
	return out
}

// factCommitRun assembles one run whose facts commit through the probe over root,
// so a test reads the commit a fact lands in beside the entry that committed it.
func factCommitRun(t *testing.T, ctx context.Context, root string, probe *factCommitEngine) *Run {
	t.Helper()
	engine, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the engine of the root: %v", err)
	}
	probe.AtomicStore = engine
	return windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, Engine: probe}))
}

// factCommitEvent names one input fact of the fixture chain.
func factCommitEvent(key string, addr event.Address, at time.Time, content string) event.Event {
	return event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeExternalInput,
		Timestamp: at,
		Owner:     addr,
		Message:   model.Message{Role: model.RoleUser, Content: content},
	}
}

// factCommitKeys answers the keys one fact of partition, window and slot commits
// under, beside the seat the store states its count in. The original family lands
// as the reference that names the text its event carries, so no body key travels
// beside the event that already holds those bytes.
func factCommitKeys(partition int, window int64, seq int, key event.EventID) []string {
	return []string{
		fmt.Sprintf("%d:evt:%d:%d", partition, window, seq),
		fmt.Sprintf("%d:idx:%s", partition, key),
		fmt.Sprintf("%d:meta:%d", partition, window),
		"ref:" + string(key),
		"store:seat",
	}
}

// factFamilyKeys answers every key the five families of one fact commit under, so
// a fixture states that a refused fact published none of them beside the keys a
// boot of the same root writes.
func factFamilyKeys(t *testing.T, ctx context.Context, store ledger.AtomicStore, partition int) []string {
	t.Helper()
	var out []string
	for _, prefix := range []string{"blob:", "ref:", fmt.Sprintf("%d:evt:", partition), fmt.Sprintf("%d:idx:", partition), fmt.Sprintf("%d:meta:", partition)} {
		keys, err := store.Keys(ctx, prefix)
		if err != nil {
			t.Fatalf("read the keys under %q: %v", prefix, err)
		}
		out = append(out, keys...)
	}
	return out
}

// persistOneFact lands one fact through the write step the commit entry runs: the
// identity is read first, as a commit reads it before it states a position, and the
// write then runs under the landing fence the entry holds across the registration and
// the fold that follow it in a commit of this run.
func persistOneFact(t *testing.T, ctx context.Context, run *Run, evt event.Event) (ledger.FactCommitOutcome, error) {
	t.Helper()
	stored, chainHolds, err := run.chainRecord(ctx, evt)
	if err != nil {
		return ledger.FactRefused, err
	}
	run.landing.Lock()
	defer run.landing.Unlock()
	return run.persistFactOrdered(ctx, evt, stored, chainHolds)
}

// TestFactCommitLandsOriginalAndRecordInOneCommit pins the commit unit of one fact through the entry that commits a run's facts.
//   - the one commit carries the event body, its index entry, the window meta, the original body and its reference
//   - the reopened root answers the record by identity and the original by reference, and the commit took one sequence slot
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestFactCommitLandsOriginalAndRecordInOneCommit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	probe := &factCommitEngine{}
	run := factCommitRun(t, ctx, root, probe)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-whole-fact"}
	content := "the material one fact commits whole"
	evt := factCommitEvent("whole-fact", addr, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC), content)
	if _, err := persistOneFact(t, ctx, run, evt); err != nil {
		t.Fatalf("commit one fact: %v", err)
	}
	body, err := memory.BodyFromEvent(evt, factCommitFormat, factCommitRetention)
	if err != nil {
		t.Fatalf("derive the body of the fact: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("encode the body of the fact: %v", err)
	}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	window := evt.Timestamp.UnixMilli()
	want := factCommitKeys(partition, window, 1, evt.Key)
	batches := probe.written(want[0])
	if len(batches) != 1 {
		t.Fatalf("the fact landed in %d commit(s), want one", len(batches))
	}
	if len(batches[0]) != len(want) {
		t.Fatalf("the one commit carries %v, want the %d key families %v", batches[0], len(want), want)
	}
	for _, key := range want {
		if !slices.Contains(batches[0], key) {
			t.Fatalf("the one commit carries %v, want %s", batches[0], key)
		}
	}
	if got := run.tier.Segments.EventCount(); got != 1 {
		t.Fatalf("the committing view counts %d event(s), want the one it landed", got)
	}
	endRun(t, run)
	reopened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the root: %v", err)
	}
	defer reopened.Close()
	stored, held, err := reopened.Segments.GetEvent(ctx, evt.Key)
	if err != nil || !held {
		t.Fatalf("the reopened record = held %t, %v, want the fact it committed", held, err)
	}
	if !bytes.Equal(stored.Body, encoded) {
		t.Fatalf("the reopened record carries %d byte(s), want the %d it committed", len(stored.Body), len(encoded))
	}
	if stored.PartitionID != partition || stored.WindowTS != window || stored.Seq != 1 {
		t.Fatalf("the reopened slot = {%d %d %d}, want the one the commit took", stored.PartitionID, stored.WindowTS, stored.Seq)
	}
	if got := reopened.Segments.EventCount(); got != 1 {
		t.Fatalf("the reopened view counts %d event(s), want the one the commit landed", got)
	}
	original := reopened.Originals.Get(ctx, evt.Key)
	if original.Outcome != ledger.RecallFound {
		t.Fatalf("the reopened original = %v, want the body the commit carried", original.Outcome)
	}
	if string(original.Body) != content {
		t.Fatalf("the reopened original = %q, want the %q the commit carried", string(original.Body), content)
	}
}

// TestRefusedFactCommitPublishesNoKey pins the refusal of one fact the engine cannot carry whole: no key family of it stays published.
//   - a commit refused on its record family leaves no original body behind, so the fact never stands half committed
//   - the refusal reaches the caller by the name the engine answered it with
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRefusedFactCommitPublishesNoKey(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	refused := errors.New("tagent: the engine refused the record family")
	probe := &factCommitEngine{refuse: func(batch ledger.Batch) error {
		for _, put := range batch.Puts {
			if strings.Contains(put.Key, ":evt:") {
				return refused
			}
		}
		return nil
	}}
	run := factCommitRun(t, ctx, root, probe)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-refused-fact"}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	evt := factCommitEvent("refused-fact", addr, time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC), "the material a refused fact carries")
	if _, err := persistOneFact(t, ctx, run, evt); err == nil {
		t.Fatal("the refused commit answered success, want the refusal the engine named")
	} else if !errors.Is(err, refused) {
		t.Fatalf("the refused commit = %v, want the %v it answered", err, refused)
	}
	if keys := factFamilyKeys(t, ctx, run.tier.Store, partition); len(keys) != 0 {
		t.Fatalf("the committing view holds %v, want no key of a refused fact", keys)
	}
	if _, held, err := run.tier.Segments.GetEvent(ctx, evt.Key); err != nil || held {
		t.Fatalf("the committing view answers held %t, %v for the refused fact, want no record", held, err)
	}
	if got := run.tier.Originals.Get(ctx, evt.Key); got.Outcome == ledger.RecallFound {
		t.Fatalf("the committing view answers the original of the refused fact as %v, want no body", got.Outcome)
	}
	endRun(t, run)
	reopened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the root: %v", err)
	}
	defer reopened.Close()
	if keys := factFamilyKeys(t, ctx, reopened.Store, partition); len(keys) != 0 {
		t.Fatalf("the reopened root holds %v, want no key of a refused fact", keys)
	}
	if _, held, err := reopened.Segments.GetEvent(ctx, evt.Key); err != nil || held {
		t.Fatalf("the reopened root answers held %t, %v for the refused fact, want no record", held, err)
	}
	if got := reopened.Originals.Get(ctx, evt.Key); got.Outcome == ledger.RecallFound {
		t.Fatalf("the reopened root answers the original of the refused fact as %v, want no body", got.Outcome)
	}
}

// TestFactCommitJudgesTheDomainBeforeTheEngineBytes pins which layer decides a repeat of one fact.
//   - two bodies naming one fact by this layer's judgment encode different bytes, so the fixture exercises two different judgments
//   - the engine refuses the second body under the one identity while the fact entry answers the duplicate it is
//   - the committed bytes do not move, in the committing view and in the reopened root
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestFactCommitJudgesTheDomainBeforeTheEngineBytes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}))
	addr := event.Address{AgentID: "agent-a", SessionID: "session-domain-equivalence"}
	at := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	first := factCommitEvent("domain-equivalent-fact", addr, at, "the material one identity names")
	if _, err := persistOneFact(t, ctx, run, first); err != nil {
		t.Fatalf("commit the fact the identity names: %v", err)
	}
	second := first
	second.Source = event.Address{AgentID: "another-agent", SessionID: "another-session"}
	left, err := memory.BodyFromEvent(first, factCommitFormat, factCommitRetention)
	if err != nil {
		t.Fatalf("derive the body of the first commit: %v", err)
	}
	right, err := memory.BodyFromEvent(second, factCommitFormat, factCommitRetention)
	if err != nil {
		t.Fatalf("derive the body of the repeat: %v", err)
	}
	if !left.SameFact(right) {
		t.Fatal("the fixture bodies name different facts, so it exercises no domain equivalence")
	}
	leftBytes, err := left.Encode()
	if err != nil {
		t.Fatalf("encode the first body: %v", err)
	}
	rightBytes, err := right.Encode()
	if err != nil {
		t.Fatalf("encode the second body: %v", err)
	}
	if bytes.Equal(leftBytes, rightBytes) {
		t.Fatal("the fixture bodies encode equal bytes, so it exercises no difference between the two judgments")
	}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	window := at.UnixMilli()
	record := ledger.SegmentRecord{
		PartitionID: partition,
		WindowTS:    window,
		Seq:         1,
		EventKey:    first.Key,
		EventTime:   window,
		Body:        rightBytes,
	}.WithChain(first.Type, sessionOf(first))
	if err := run.tier.Segments.StoreEvent(ctx, record); !errors.Is(err, ledger.ErrContentConflict) {
		t.Fatalf("the engine under the one identity = %v, want the byte conflict it judges", err)
	}
	if _, err := persistOneFact(t, ctx, run, second); err != nil {
		t.Fatalf("the fact entry under the one identity = %v, want the duplicate this layer judged", err)
	}
	if got := run.tier.Segments.EventCount(); got != 1 {
		t.Fatalf("the repeat left the count at %d, want the one sequence slot", got)
	}
	stored, held, err := run.tier.Segments.GetEvent(ctx, first.Key)
	if err != nil || !held {
		t.Fatalf("the committed record = held %t, %v, want the fact the identity names", held, err)
	}
	if !bytes.Equal(stored.Body, leftBytes) {
		t.Fatalf("the committed record moved to %d byte(s), want the %d the first commit landed", len(stored.Body), len(leftBytes))
	}
	endRun(t, run)
	reopened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the root: %v", err)
	}
	defer reopened.Close()
	restored, held, err := reopened.Segments.GetEvent(ctx, first.Key)
	if err != nil || !held {
		t.Fatalf("the reopened record = held %t, %v, want the fact the identity names", held, err)
	}
	if !bytes.Equal(restored.Body, leftBytes) {
		t.Fatalf("the reopened record carries %d byte(s), want the %d the first commit landed", len(restored.Body), len(leftBytes))
	}
	original := reopened.Originals.Get(ctx, first.Key)
	if original.Outcome != ledger.RecallFound || string(original.Body) != first.Message.Content {
		t.Fatalf("the reopened original = %v over %q, want the %q the first commit landed", original.Outcome, string(original.Body), first.Message.Content)
	}
}

// TestFactCommitRefusesOneIdentityInTwoPartitions pins that one identity stands for one fact of the whole store, whichever partition names it.
//   - the same key committed under an owner of another partition is refused with the tier's content conflict, whatever the bytes it carries
//   - the two shapes are the other bytes and the very bytes the first commit filed
//   - no second record lands: the count, the committed bytes and the original stay those of the first commit, in the reopened root too
//   - negative: a pre-check reading across the partitions answers the same bytes as a repeat and the other ones as the layer's key conflict
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestFactCommitRefusesOneIdentityInTwoPartitions(t *testing.T) {
	ctx := context.Background()
	mine := event.Address{AgentID: "agent-a", SessionID: "session-two-partitions-mine"}
	other := event.Address{AgentID: "agent-b", SessionID: "session-two-partitions-other"}
	minePartition := memory.PartitionIDFromName(string(mine.AgentID))
	otherPartition := memory.PartitionIDFromName(string(other.AgentID))
	if minePartition == otherPartition {
		t.Fatalf("both owners fold into partition %d, so the fixture proves no identity of another partition", minePartition)
	}
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	window := at.UnixMilli()
	filed := event.Event{Key: event.EventID("two-partition-identity"), Type: event.TypeCheckpoint, Timestamp: at, Owner: mine}
	body, err := memory.BodyFromEvent(filed, factCommitFormat, factCommitRetention)
	if err != nil {
		t.Fatalf("derive the body of the first commit: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("encode the body of the first commit: %v", err)
	}
	cases := []struct {
		name   string
		commit func(t *testing.T, ctx context.Context, run *Run) error
	}{
		{name: "another owner", commit: func(t *testing.T, ctx context.Context, run *Run) error {
			t.Helper()
			second := filed
			second.Owner = other
			_, err := run.commitFactChecked(ctx, second)
			return err
		}},
		{name: "the same bytes", commit: func(t *testing.T, ctx context.Context, run *Run) error {
			t.Helper()
			outcome, err := ledger.CommitFact(ctx, run.tier, ledger.FactCommit{
				Record: ledger.SegmentRecord{
					PartitionID: otherPartition,
					WindowTS:    window,
					Seq:         1,
					EventKey:    filed.Key,
					EventTime:   window,
					Body:        encoded,
				}.WithChain(filed.Type, sessionOf(filed)),
				Ref:  body.Ref(),
				Body: []byte(body.Message.Content),
			})
			if outcome != ledger.FactConflict {
				t.Fatalf("the commit of the same bytes in partition %d answered %v, want the conflict the tier judges", otherPartition, outcome)
			}
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}))
			if _, err := run.commitFactChecked(ctx, filed); err != nil {
				t.Fatalf("commit the fact of its own partition: %v", err)
			}
			stored, held, err := run.tier.Segments.GetEventInPartition(ctx, filed.Key, minePartition)
			if err != nil || !held {
				t.Fatalf("the first commit = held %t, %v, want the record in partition %d", held, err, minePartition)
			}
			if !bytes.Equal(stored.Body, encoded) {
				t.Fatalf("the first commit landed %d byte(s), want the %d the fixture derived", len(stored.Body), len(encoded))
			}
			refusal := tc.commit(t, ctx, run)
			if !errors.Is(refusal, ledger.ErrContentConflict) {
				t.Fatalf("the identity committed in partition %d answered %v, want %v", otherPartition, refusal, ledger.ErrContentConflict)
			}
			if errors.Is(refusal, memory.ErrFactKeyConflict) {
				t.Fatalf("the refusal = %v, want the tier's content conflict rather than the layer's key conflict", refusal)
			}
			if count := run.tier.Segments.EventCount(); count != 1 {
				t.Fatalf("the store holds %d event(s) after the refusal, want the one the first commit landed", count)
			}
			if _, held, err := run.tier.Segments.GetEventInPartition(ctx, filed.Key, otherPartition); err != nil || held {
				t.Fatalf("partition %d answers held %t, %v, want no record under the refused identity", otherPartition, held, err)
			}
			endRun(t, run)
			reopened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
			if err != nil {
				t.Fatalf("reopen the root: %v", err)
			}
			defer reopened.Close()
			restored, held, err := reopened.Segments.GetEventInPartition(ctx, filed.Key, minePartition)
			if err != nil || !held {
				t.Fatalf("the reopened record = held %t, %v, want the fact the identity names", held, err)
			}
			if !bytes.Equal(restored.Body, encoded) {
				t.Fatalf("the reopened record carries %d byte(s), want the %d the first commit landed", len(restored.Body), len(encoded))
			}
			if _, held, err := reopened.Segments.GetEventInPartition(ctx, filed.Key, otherPartition); err != nil || held {
				t.Fatalf("the reopened partition %d answers held %t, %v, want no record under the refused identity", otherPartition, held, err)
			}
			if original := reopened.Originals.Get(ctx, filed.Key); original.Outcome != ledger.RecallFound {
				t.Fatalf("the reopened original = %v, want the body the first commit landed", original.Outcome)
			}
		})
	}
}

// replayRefFixturePartition commits one mapped fact of a chain and answers the partition the chain is filed in.
func replayRefFixturePartition(t *testing.T, ctx context.Context, run *Run, addr event.Address) int {
	t.Helper()
	key, err := run.commitFactChecked(ctx, factCommitEvent("session-replay-ref-present-fact", addr, time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), "the fact the walk reads"))
	if err != nil {
		t.Fatalf("commit the fact the walk reads: %v", err)
	}
	partition, _ := seqOfFact(t, run, key)
	if derived := memory.PartitionIDFromName(string(addr.AgentID)); partition != derived {
		t.Fatalf("the fixture fact landed in partition %d, want the partition %d its owner names", partition, derived)
	}
	return partition
}

// releaseLiveFact releases one fact from the live ledger of a run the way a trim and
// a restart both do, so the durable chain answers a repeat of that fact while this
// process holds no copy beside it.
func releaseLiveFact(t *testing.T, run *Run, key event.EventID) {
	t.Helper()
	run.mu.Lock()
	defer run.mu.Unlock()
	evt, held := run.facts[key]
	if !held {
		t.Fatalf("the run holds no fact under %s, so the fixture releases nothing", key)
	}
	delete(run.facts, key)
	if session := sessionOf(evt); session != "" {
		run.sessionFactCount[session]--
	}
	run.compactFactOrderLocked(map[event.EventID]struct{}{key: {}})
}

// landedWatermark answers the sequence slot one partition landed last, read through
// the watermark a cut reads.
func landedWatermark(run *Run, partition int) int64 {
	return run.checkpointWatermark()[partition]
}

// TestRepeatedFactDoesNotAdvanceLandedSequence pins the sequence a repeat of one fact leaves untouched through the entry that commits a run's facts.
// - the chain answers the repeat as the fact it already holds: no key of it lands a second time and the landed sequence stays where the first commit left it
// - the next new fact takes the slot after that sequence, so a repeat never spends one
// - the identity keeps the bytes and the slot the first commit gave it, in this view and in a root reopened after it
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRepeatedFactDoesNotAdvanceLandedSequence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	probe := &factCommitEngine{}
	run := factCommitRun(t, ctx, root, probe)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-repeated-fact"}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	first := factCommitEvent("repeated-fact", addr, time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), "the material one identity names")
	if _, err := run.commitFactChecked(ctx, first); err != nil {
		t.Fatalf("commit the fact the identity names: %v", err)
	}
	if got := landedWatermark(run, partition); got != 1 {
		t.Fatalf("the first commit left the landed sequence at %d, want the one slot it took", got)
	}
	body, err := memory.BodyFromEvent(first, factCommitFormat, factCommitRetention)
	if err != nil {
		t.Fatalf("derive the body of the fact: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("encode the body of the fact: %v", err)
	}
	original := "ref:" + string(first.Key)
	if got := len(probe.written(original)); got != 1 {
		t.Fatalf("the first commit landed %d original commit(s), want one", got)
	}

	releaseLiveFact(t, run, first.Key)
	if key, err := run.commitFactChecked(ctx, first); err != nil || key != first.Key {
		t.Fatalf("the repeated fact = %s, %v, want the identity the chain already holds", key, err)
	}
	if got := landedWatermark(run, partition); got != 1 {
		t.Fatalf("the repeat left the landed sequence at %d, want the one slot the first commit took", got)
	}
	if got := len(probe.written(original)); got != 1 {
		t.Fatalf("the repeat landed %d original commit(s), want the one the first commit made", got)
	}
	if gotPartition, gotSeq := seqOfFact(t, run, first.Key); gotPartition != partition || gotSeq != 1 {
		t.Fatalf("the repeat left the identity at {%d %d}, want the {%d 1} the first commit filed", gotPartition, gotSeq, partition)
	}

	second := factCommitEvent("later-fact", addr, first.Timestamp.Add(time.Second), "the material the next fact carries")
	if _, err := run.commitFactChecked(ctx, second); err != nil {
		t.Fatalf("commit the fact after the repeat: %v", err)
	}
	if got := landedWatermark(run, partition); got != 2 {
		t.Fatalf("the fact after the repeat left the landed sequence at %d, want 2: a repeat of one fact must not spend a sequence slot", got)
	}
	if _, gotSeq := seqOfFact(t, run, second.Key); gotSeq != 2 {
		t.Fatalf("the fact after the repeat landed at slot %d, want 2: a repeat of one fact must not spend a sequence slot", gotSeq)
	}
	stored, held, err := run.tier.Segments.GetEventInPartition(ctx, first.Key, partition)
	if err != nil || !held {
		t.Fatalf("the repeated identity = held %t, %v, want the record the first commit landed", held, err)
	}
	if stored.Seq != 1 || !bytes.Equal(stored.Body, encoded) {
		t.Fatalf("the repeated identity = slot %d over %d byte(s), want the slot 1 over the %d bytes the first commit landed", stored.Seq, len(stored.Body), len(encoded))
	}
	recalled := run.tier.Originals.Get(ctx, first.Key)
	if recalled.Outcome != ledger.RecallFound || string(recalled.Body) != first.Message.Content {
		t.Fatalf("the repeated identity answers the original as %v over %q, want the %q the first commit carried", recalled.Outcome, string(recalled.Body), first.Message.Content)
	}

	endRun(t, run)
	reopened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the root: %v", err)
	}
	defer reopened.Close()
	repeated, held, err := reopened.Segments.GetEventInPartition(ctx, first.Key, partition)
	if err != nil || !held {
		t.Fatalf("the reopened repeated identity = held %t, %v, want the record the first commit landed", held, err)
	}
	if repeated.Seq != 1 || !bytes.Equal(repeated.Body, encoded) {
		t.Fatalf("the reopened repeated identity = slot %d over %d byte(s), want the slot 1 over the %d bytes the first commit landed", repeated.Seq, len(repeated.Body), len(encoded))
	}
	next, held, err := reopened.Segments.GetEventInPartition(ctx, second.Key, partition)
	if err != nil || !held {
		t.Fatalf("the reopened fact after the repeat = held %t, %v, want the record it landed", held, err)
	}
	if next.Seq != 2 {
		t.Fatalf("the reopened fact after the repeat landed at slot %d, want 2: a repeat of one fact must not spend a sequence slot", next.Seq)
	}
	if got := reopened.Segments.EventCount(); got != 2 {
		t.Fatalf("the reopened root counts %d event(s), want the two facts it landed", got)
	}
}

// factApplyRecorder is one state holder that states the order the facts it is
// folded with reached the state seam, so a check reads the order a commit applied
// its facts in beside the order the storage committed them in. A recorder built
// with one fact named parks that fact's fold until the check releases it, so the
// interleaving is reached by the fold the product runs and not by timing.
type factApplyRecorder struct {
	mu      sync.Mutex
	order   []event.EventID
	gated   event.EventID
	gate    chan struct{}
	arrived chan struct{}
	reached bool
	once    sync.Once
}

// newFactApplyRecorder returns one recorder, parking the fold of gated when it is
// not empty.
func newFactApplyRecorder(gated event.EventID) *factApplyRecorder {
	recorder := &factApplyRecorder{gated: gated, gate: make(chan struct{})}
	if gated != "" {
		recorder.arrived = make(chan struct{})
	}
	return recorder
}

// StateName names the slice this recorder states.
func (r *factApplyRecorder) StateName() string { return "assembly.fact-apply-order" }

// SnapshotState states nothing: the order is this check's own observation and no
// cut carries it.
func (r *factApplyRecorder) SnapshotState(context.Context) ([]byte, error) {
	return []byte("null"), nil
}

// RestoreState accepts the empty state the cut states for this slice.
func (r *factApplyRecorder) RestoreState(context.Context, []byte) error { return nil }

// Reduce folds one fact by appending its identity to the order this recorder
// states. The gated fact parks here until the check releases it, so the commit of
// that fact stands between its landing and the application the seam derives.
func (r *factApplyRecorder) Reduce(_ context.Context, evt event.Event) error {
	r.mu.Lock()
	park := evt.Key == r.gated && !r.reached
	if park {
		r.reached = true
	}
	r.mu.Unlock()
	if park {
		r.once.Do(func() { close(r.arrived) })
		<-r.gate
	}
	r.mu.Lock()
	r.order = append(r.order, evt.Key)
	r.mu.Unlock()
	return nil
}

// awaitArrival waits until the gated fact reaches the seam, failing the check
// when it never does.
func (r *factApplyRecorder) awaitArrival(t *testing.T) {
	t.Helper()
	if r.arrived == nil {
		t.Fatal("the recorder names no fact to park, so the check would wait for a fold it never asked for")
	}
	select {
	case <-r.arrived:
	case <-time.After(60 * time.Second):
		t.Fatal("the gated fact never reached the state seam, so the interleaving of this check is unreachable")
	}
}

// release lets the parked fold proceed.
func (r *factApplyRecorder) release() {
	r.once.Do(func() {})
	select {
	case <-r.gate:
	default:
		close(r.gate)
	}
}

// appliedOrder reports the order the facts reached the seam in.
func (r *factApplyRecorder) appliedOrder() []event.EventID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.EventID(nil), r.order...)
}

// countOf reports how many times one identity was folded into this recorder.
func (r *factApplyRecorder) countOf(key event.EventID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	folded := 0
	for _, held := range r.order {
		if held == key {
			folded++
		}
	}
	return folded
}

// factReadCounter wraps one engine and counts the reads of one index key, so a
// check knows both commits of one identity have looked at a chain holding nothing
// before either of them lands. The count is the engine's own call order, so the
// interleaving is deterministic and names no sleep.
type factReadCounter struct {
	ledger.AtomicStore
	mu      sync.Mutex
	watch   string
	want    int
	reads   int
	ready   chan struct{}
	relayed sync.Once
}

// newFactReadCounter returns one counter that answers ready once the wanted reads
// of watch happened.
func newFactReadCounter(store ledger.AtomicStore, watch string, want int) *factReadCounter {
	return &factReadCounter{AtomicStore: store, watch: watch, want: want, ready: make(chan struct{})}
}

// Read answers one fetch and counts the reads of the watched key.
func (c *factReadCounter) Read(ctx context.Context, fetch ledger.Fetch) ([]ledger.Pair, error) {
	pairs, err := c.AtomicStore.Read(ctx, fetch)
	if fetch.Key != "" && fetch.Key == c.watch {
		c.mu.Lock()
		c.reads++
		reached := c.reads >= c.want
		reads := c.reads
		c.mu.Unlock()
		if reached && reads >= c.want {
			c.relayed.Do(func() { close(c.ready) })
		}
	}
	return pairs, err
}

// await waits until both commits have read the identity.
func (c *factReadCounter) await(t *testing.T) {
	t.Helper()
	select {
	case <-c.ready:
	case <-time.After(60 * time.Second):
		c.mu.Lock()
		reads := c.reads
		c.mu.Unlock()
		t.Fatalf("the %d reads of %s this check needs never happened: the engine answered %d, so the two commits never stood together before the landing",
			c.want, c.watch, reads)
	}
}

// count reports the reads of the watched key answered so far.
func (c *factReadCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// factLandedSeq answers the sequence slot one fact landed at, read back from the
// storage root the run committed it to.
func factLandedSeq(t *testing.T, ctx context.Context, run *Run, key event.EventID, partition int) int64 {
	t.Helper()
	stored, held, err := run.tier.Segments.GetEventInPartition(ctx, key, partition)
	if err != nil || !held {
		t.Fatalf("the storage root answers the fact %s = held %t, %v, want the record the commit landed", key, held, err)
	}
	return stored.Seq
}

// TestFactLandedBesideAnUncertainBarrierIsStatedByItsRetry pins the retry of a commit whose barrier named no outcome.
// - the first commit lands the batch and reports the outcome the engine cannot name, so the process states neither the registration nor a fold
// - the retry meets a fact the chain holds whole and a process that never registered it, so it states that landing: one registration, one fold
// - the control is the state of the seam: a retry answered as a plain repeat of a fact this process never registered folds nothing
// - a seam state so left is the state no replay of the same chain derives
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestFactLandedBesideAnUncertainBarrierIsStatedByItsRetry(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-uncertain-landing"}
	key := event.EventID("uncertain-landing")
	fact := factCommitEvent(string(key), addr, time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), "the fact the unsettled barrier left durable")
	engine, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the engine of the root: %v", err)
	}
	probe := &factCommitEngine{AtomicStore: engine}
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, Engine: probe}))
	run.stopCheckpointFiler()
	defer endRun(t, run)
	recorder := newFactApplyRecorder("")
	if _, err := run.RegisterStateSlice(recorder); err != nil {
		t.Fatalf("mount the state holder this check reads the folds from: %v", err)
	}
	reported := false
	probe.report = func(ledger.Batch) error {
		if reported {
			return nil
		}
		reported = true
		return fmt.Errorf("tagent: the barrier reported beside the write: %w", ledger.ErrCommitUncertain)
	}

	if _, err := run.CommitToSession(ctx, addr, fact); !errors.Is(err, ledger.ErrCommitUncertain) {
		t.Fatalf("the commit whose barrier named no outcome answered %v, want the uncertain outcome the engine named", err)
	}
	if folded := recorder.countOf(key); folded != 0 {
		t.Fatalf("the state seam folded the identity %d time(s) beside the commit the process could not name, want none", folded)
	}

	if _, err := run.CommitToSession(ctx, addr, fact); err != nil {
		t.Fatalf("the retry of the fact the chain already holds answered %v, want the commitment it is", err)
	}
	if folded := recorder.countOf(key); folded != 1 {
		t.Fatalf("the state seam folded the identity %d time(s) after the retry, want one: the landing the unsettled barrier left is the state a replay of the same chain derives", folded)
	}
	stated := 0
	for _, held := range run.SessionFacts(addr) {
		if held.Key == key {
			stated++
		}
	}
	if stated != 1 {
		t.Fatalf("the live ledger states the identity %d time(s) after the retry, want one registration", stated)
	}
}

// TestConcurrentCommitPreservesApplicationOrder pins the order two concurrent commits reach the state seam in against the durable order.
//   - the first fact lands and its fold parks before the seam states it, so the ordered unit the commit holds stands inside that application
//   - the landing fence is held there, so a second commit cannot state a position beside it: the fence it must take is the one the parked fold holds
//   - a fold outside the ordered unit leaves the fence free here, which is the control this check reads
//   - the second commit is in flight — its two reads of the identity are done — and has applied nothing, because it waits on that fence
//   - the count of those reads is what tells this check the second commit is in flight and waiting on the fence
//   - once the first fold is released both commits land and the seam states them in the order the storage committed them
//   - the same facts stated in another order are another state, and a replay of this chain folds them by the durable order
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestConcurrentCommitPreservesApplicationOrder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-commit-order"}
	partition := memory.PartitionIDFromName(string(addr.AgentID))
	first := factCommitEvent("order-first", addr, time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC), "the fact the storage committed first")
	second := factCommitEvent("order-second", addr, time.Date(2026, 11, 16, 10, 0, 1, 0, time.UTC), "the fact that reaches the seam first")
	engine, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the engine of the root: %v", err)
	}
	counter := newFactReadCounter(engine, fmt.Sprintf("%d:idx:%s", partition, second.Key), 2)
	run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, Engine: counter}))
	run.stopCheckpointFiler()
	defer endRun(t, run)
	recorder := newFactApplyRecorder(first.Key)
	if _, err := run.RegisterStateSlice(recorder); err != nil {
		t.Fatalf("mount the state holder this check reads the order from: %v", err)
	}

	landed := make(chan error, 1)
	go func() {
		_, err := run.CommitToSession(ctx, addr, first)
		landed <- err
	}()
	recorder.awaitArrival(t)
	if run.landing.TryLock() {
		run.landing.Unlock()
		t.Fatal("the landing fence stood free beside the parked fold of the first fact, want it held: the registration and the fold of a commit run inside the unit that lands it")
	}
	landing := make(chan error, 1)
	go func() {
		_, err := run.CommitToSession(ctx, addr, second)
		landing <- err
	}()
	counter.await(t)
	if applied := recorder.countOf(second.Key); applied != 0 {
		t.Fatalf("the second fact reached the state seam %d time(s) beside the parked fold of the first, want nothing applied: the fence the first commit holds is the one the second states its position under",
			applied)
	}
	recorder.release()
	if err := <-landed; err != nil {
		t.Fatalf("commit the first fact: %v", err)
	}
	if err := <-landing; err != nil {
		t.Fatalf("commit the second fact beside the parked fold of the first: %v", err)
	}

	firstSeq := factLandedSeq(t, ctx, run, first.Key, partition)
	secondSeq := factLandedSeq(t, ctx, run, second.Key, partition)
	if firstSeq >= secondSeq {
		t.Fatalf("the storage committed the two facts at slots %d and %d, want the first before the second: this check needs that order to read a divergence",
			firstSeq, secondSeq)
	}
	durable := []event.EventID{first.Key, second.Key}
	applied := recorder.appliedOrder()
	if !slices.Equal(applied, durable) {
		t.Fatalf("the state seam applied %v while the storage committed %v: the derived state of this chain is not the state a replay of the same durable order derives",
			applied, durable)
	}
}

// TestConcurrentSameFactKeepsOneApplication pins one identity committed twice at once, and the storage answers the repeat as the commitment it already is.
//   - the equivalent case keeps one record, one registration and one fold: the repeat is not a second fact and the seam is asked to state it once
//   - the conflicting case refuses the second commit by name and folds nothing under the identity, leaving the first content as the original
//   - the probe parks the first commit before the engine sees it and reads the chain until the second commit has looked: both lookups answer absence
//   - each commit reads the identity twice before it takes a landing slot, once as its lookup of the chain and once as the read its own persist repeats
//   - the landing lock the check holds keeps both commits between those reads and the commit that lands
//   - the released landing slot wakes the two commits in no stated order, so the outcomes are read as a set
//   - one identity landed, and the content it never named is refused beside the key conflict
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestConcurrentSameFactKeepsOneApplication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	for _, held := range []struct {
		name    string
		content string
	}{
		{name: "the same fact twice", content: "the material one identity carries"},
		{name: "another content under one identity", content: "another material the identity never named"},
	} {
		t.Run(held.name, func(t *testing.T) {
			root := t.TempDir()
			addr := event.Address{AgentID: "agent-a", SessionID: "session-same-fact"}
			key := event.EventID("same-fact-once")
			partition := memory.PartitionIDFromName(string(addr.AgentID))
			indexKey := fmt.Sprintf("%d:idx:%s", partition, key)
			engine, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
			if err != nil {
				t.Fatalf("open the engine of the root: %v", err)
			}
			counter := newFactReadCounter(engine, indexKey, 4)
			run := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, Engine: counter}))
			run.stopCheckpointFiler()
			defer endRun(t, run)
			recorder := newFactApplyRecorder("")
			if _, err := run.RegisterStateSlice(recorder); err != nil {
				t.Fatalf("mount the state holder this check reads the folds from: %v", err)
			}

			first := factCommitEvent(string(key), addr, time.Date(2026, 11, 16, 11, 0, 0, 0, time.UTC), "the material one identity carries")
			second := factCommitEvent(string(key), addr, time.Date(2026, 11, 16, 11, 0, 1, 0, time.UTC), held.content)
			type outcome struct {
				key event.EventID
				err error
			}
			run.landing.Lock()
			firstDone := make(chan outcome, 1)
			secondDone := make(chan outcome, 1)
			go func() {
				id, err := run.CommitToSession(ctx, addr, first)
				firstDone <- outcome{key: id, err: err}
			}()
			go func() {
				id, err := run.CommitToSession(ctx, addr, second)
				secondDone <- outcome{key: id, err: err}
			}()
			counter.await(t)
			run.landing.Unlock()
			firstOutcome := <-firstDone
			secondOutcome := <-secondDone
			equivalent := held.content == "the material one identity carries"
			committed, refused := 0, 0
			for _, outcome := range []outcome{firstOutcome, secondOutcome} {
				switch {
				case outcome.err == nil:
					committed++
				case errors.Is(outcome.err, memory.ErrFactKeyConflict):
					refused++
				default:
					t.Fatalf("a commit of one identity answered %v, want the commitment or the key conflict the storage answers", outcome.err)
				}
			}
			if equivalent {
				if committed != 2 || refused != 0 {
					t.Fatalf("the two commits of one fact answered %d commitment(s) and %d refusal(s), want both answered as the commitment the storage already holds",
						committed, refused)
				}
			} else if committed != 1 || refused != 1 {
				t.Fatalf("the two commits of two contents under one identity answered %d commitment(s) and %d refusal(s), want one of each",
					committed, refused)
			}
			if folded := recorder.countOf(key); folded != 1 {
				t.Fatalf("the state seam folded the identity %d time(s), want one: the order %v names it that many times",
					folded, recorder.appliedOrder())
			}
			run.mu.Lock()
			named := 0
			for _, held := range run.factOrder {
				if held == key {
					named++
				}
			}
			facts := len(run.factOrder)
			run.mu.Unlock()
			if named != 1 {
				t.Fatalf("the commit order names the identity %s %d time(s), want one: a second registration states a fact the storage holds once", key, named)
			}
			if facts != 1 {
				t.Fatalf("the commit order names %d fact(s) beside the one identity this check committed, want one", facts)
			}
			stored, found, err := run.tier.Segments.GetEventInPartition(ctx, key, partition)
			if err != nil || !found {
				t.Fatalf("the root answers the identity = held %t, %v, want the one record", found, err)
			}
			if stored.Seq != 1 {
				t.Fatalf("the identity landed at slot %d, want the one slot the commitment takes", stored.Seq)
			}
		})
	}
}

// noTierChainRun assembles one run beside no storage tier at all: no option names
// a tier and the declaration names no memory plan, so the run keeps its facts in
// memory alone and claims no durability for any of them.
func noTierChainRun(t *testing.T, ctx context.Context) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	run, err := assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}, false, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the run beside no storage tier: %v", err)
	}
	return run
}

// TestNoTierCommitPreservesApplicationOrder pins the ordered unit of a run opened over no storage tier at all.
//   - the run opened no tier, so no durable account states its commits and none of them promises a record that outlives the process
//   - the fold of one commit parks inside the ordered unit, so the landing fence is held beside it: a fold outside that unit leaves the fence free
//   - the seam derives the two facts in the order the run filed their positions, which is the order the commits reached that unit
//   - negative: an application outside the unit folds the second fact beside the parked one, which the order assertion reads as a divergence
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestNoTierCommitPreservesApplicationOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := noTierChainRun(t, ctx)
	run.stopCheckpointFiler()
	if run.tier != nil {
		t.Fatalf("the run opened the tier %q, want the run beside no storage tier this case pins", run.tier.Capabilities.Tier)
	}
	if durable := run.durableAccount(); durable != nil {
		t.Fatal("the run holds a durable account, want the in-memory authority alone: a run beside no tier states no account to seat")
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-no-tier-order"}
	first := factCommitEvent("no-tier-first", addr, time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC), "the fact the run files first")
	second := factCommitEvent("no-tier-second", addr, time.Date(2026, 11, 22, 10, 0, 1, 0, time.UTC), "the fact the run files second")
	recorder := newFactApplyRecorder(first.Key)
	if _, err := run.RegisterStateSlice(recorder); err != nil {
		t.Fatalf("mount the state holder this check reads the order from: %v", err)
	}

	landed := make(chan error, 1)
	go func() {
		_, err := run.CommitToSession(ctx, addr, first)
		landed <- err
	}()
	recorder.awaitArrival(t)
	if run.landing.TryLock() {
		run.landing.Unlock()
		t.Fatal("the landing fence stood free beside the parked fold of the no-tier commit, want it held: the position and the fold of one commit run inside the unit that lands it")
	}
	beside := make(chan error, 1)
	go func() {
		_, err := run.CommitToSession(ctx, addr, second)
		beside <- err
	}()
	recorder.release()
	if err := <-landed; err != nil {
		t.Fatalf("commit the first fact beside no tier: %v", err)
	}
	if err := <-beside; err != nil {
		t.Fatalf("commit the second fact beside no tier: %v", err)
	}

	want := []event.EventID{first.Key, second.Key}
	if applied := recorder.appliedOrder(); !slices.Equal(applied, want) {
		t.Fatalf("the state seam applied %v, want the %v the commits filed: the derived order of a run beside no tier is the order its own commit order states",
			applied, want)
	}
	if facts := run.SessionFacts(addr); len(facts) != len(want) {
		t.Fatalf("the run holds %d fact(s) of %v, want the %d it committed: the run states no record beside the two", len(facts), addr, len(want))
	}
	if entries, carried := run.ReplayEntries(addr.SessionID); !carried || len(entries) != len(want) {
		t.Fatalf("the run presents %d entry(ies) (carried %t), want the %d facts it folded in memory", len(entries), carried, len(want))
	}
}
