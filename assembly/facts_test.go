// The hot window checks of the live fact ledger: a chain that runs long keeps the
// material a drive may still read, and the delivery of a turn reads that material
// by the identity of the invocation rather than by a position of the chain.
package assembly

import (
	"context"
	"fmt"
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
			run.seedFact(windowEvent(addr, fmt.Sprintf("replay-%05d", at), "inv-window-replay", "the replayed material", event.TypeExternalInput))
		}
		run.seedFact(windowEvent(addr, "replay-closing", "inv-window-replay", "the answer the earlier process committed", event.TypeAgentOutput))
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
		run.seedFact(windowEvent(addr, fmt.Sprintf("restart-%05d", at), "inv-window-earlier", "the material the restart replays", event.TypeExternalInput))
	}
	run.seedFact(windowEvent(addr, "restart-closing", "inv-window-earlier",
		"the answer the earlier process delivered", event.TypeAgentOutput))
	run.trimSeededFacts()
	if got := len(run.SessionFacts(addr)); got != noWindowResidentFacts {
		t.Fatalf("the replayed chain = %d facts, want the window %d", got, noWindowResidentFacts)
	}

	channel := &windowTestChannel{}
	host := &Host{
		run:               run,
		entry:             "agent-a",
		sender:            channel,
		userAddresses:     map[event.SessionID]event.Address{},
		pendingDeliveries: map[string]*heldDelivery{},
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

// TestPinnedDeliveryFactsDoNotAffectFoldVolume pins the fold's accounting view.
//
// - A pinned (held or owed) settlement fact is presented by the session but never enters the volume, the segments or the products of the age fold.
// - The same dialogue folds at the same moment with and without an unsettled obligation.
// - The obligation stands after the second turn, where a live held fact lands: in the tail of the material, never as the input that opens a turn.
func TestPinnedDeliveryFactsDoNotAffectFoldVolume(t *testing.T) {
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
		t.Fatalf("the pinned settlement fact moved the fold volume: plain=%d guarded=%d", plain.Volume, guarded.Volume)
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

// TestDeliverySettlementFactsCarryNoFoldVolume pins the volume half of the same invariant directly.
//
// - The rendered volume of the fold view is the rendered volume of the dialogue, byte for byte, however many settlement facts stand beside it.
func TestDeliverySettlementFactsCarryNoFoldVolume(t *testing.T) {
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
	plain := foldProjectionOf(ctx, deliveryFoldProjection(ctx, dialogue))
	guarded := foldProjectionOf(ctx, deliveryFoldProjection(ctx, withDelivery))
	report, err := plain.PlanByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1})
	if err != nil {
		t.Fatalf("plan the dialogue: %v", err)
	}
	other, err := guarded.PlanByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 1})
	if err != nil {
		t.Fatalf("plan the settlement-beside dialogue: %v", err)
	}
	if report.Volume != other.Volume {
		t.Fatalf("settlement facts carried fold volume: plain=%d guarded=%d", report.Volume, other.Volume)
	}
	for i := range plain.Entries() {
		if plain.Entries()[i].Key != guarded.Entries()[i].Key {
			t.Fatalf("the fold view entry %d is %s, want %s", i, guarded.Entries()[i].Key, plain.Entries()[i].Key)
		}
	}
}

// TestSettlementRoleMappedBesideTheFoldView keeps the two halves of the contract together.
//
// - The settlement type maps onto a model role (the fact is presentable), even though the fold view excludes it from accounting.
func TestSettlementRoleMappedBesideTheFoldView(t *testing.T) {
	role, err := agent.RoleOf(deliverySettlementType)
	if err != nil {
		t.Fatalf("the settlement type maps onto no role: %v", err)
	}
	if role != model.RoleUser {
		t.Fatalf("the settlement type maps onto %q, want the user role", role)
	}
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
	if len(refs) != seeded+1 {
		t.Fatalf("the durable read answered %d facts, want all %d committed facts plus the closing output", len(refs), seeded+1)
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
