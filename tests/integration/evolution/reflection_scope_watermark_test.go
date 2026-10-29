// Package evolution holds the integration tests of the evolution cluster over
// the publisher and the structural gate surfaces.
package evolution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	"github.com/SpellingDragon/tagent/features/evolution/meditation"
)

// TestReflectionScopeAndWatermark drives the reflection loop over its authorized face, its persisted watermark and its loop-session injection face.
// - an observation face outside the authorized read scope is refused by name, and an undeclared face is the agent's own partition
// - the freshness gate stays closed, by name, when the watermark cannot be established
// - injection only writes the explicit input: consumption advances the watermark to the injection moment, a yield leaves it in place
// - the digest renders deterministically and bounded out of the one pass the tick read, spending no model call
// - the fired input is a source=meditation external input consumed by the agent's own loop session and observable on the fact chain
// - every anchor here ticks outside the min gap of the fact it reads, so the activity gate leaves the scope and watermark faces their subject
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestReflectionScopeAndWatermark(t *testing.T) {
	t.Run("an observation face outside the read scope is refused by name", func(t *testing.T) {
		scope, err := meditation.ResolveObservationScope("agent-a", []string{"agent-b", "agent-z", "agent-y"}, []string{"agent-a", "agent-b"})
		if !errors.Is(err, meditation.ErrObservationScopeOutOfBounds) {
			t.Fatalf("resolve error = %v, want the out-of-bounds refusal", err)
		}
		if !strings.Contains(err.Error(), "agent-y, agent-z") {
			t.Fatalf("refusal = %q, want every offending namespace named in one report", err)
		}
		if strings.Contains(err.Error(), "agent-b") {
			t.Fatalf("refusal = %q, want the authorized namespace not reported as offending", err)
		}
		if len(scope.Names) != 0 || len(scope.Partitions) != 0 {
			t.Fatalf("scope = %+v, want no face assembled from a refused declaration", scope)
		}

		if _, err := meditation.NewReflection(meditation.ReflectionConfig{
			Loop: meditation.ReflectionLoop{Agent: "agent-a", Session: "session-meditate"},
			Face: meditation.ReflectionFace{ObservedNamespaces: []string{"agent-z"}},
		}, nil, nil, nil); !errors.Is(err, meditation.ErrObservationScopeOutOfBounds) {
			t.Fatalf("assembly error = %v, want the refused face refusing the assembly", err)
		}

		inside, err := meditation.ResolveObservationScope("agent-a", []string{"agent-b", "agent-b", "agent-a"}, []string{"agent-b"})
		if err != nil {
			t.Fatalf("resolve an authorized face: %v", err)
		}
		if got := strings.Join(inside.Names, ","); got != "agent-b,agent-a" {
			t.Fatalf("authorized face = %q, want the declared order deduplicated", got)
		}
		if len(inside.Partitions) != 2 || inside.Partitions[0] != memory.PartitionIDFromName("agent-b") {
			t.Fatalf("partitions = %v, want one partition per authorized name in the same order", inside.Partitions)
		}
	})

	t.Run("an undeclared face is the agent's own partition and not the read scope", func(t *testing.T) {
		scope, err := meditation.ResolveObservationScope("agent-a", nil, []string{"agent-a", "agent-pool"})
		if err != nil {
			t.Fatalf("resolve an undeclared face: %v", err)
		}
		if got := strings.Join(scope.Names, ","); got != "agent-a" {
			t.Fatalf("undeclared face = %q, want the agent's own partition only", got)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := reflectionConfig()
		rig := newReflectionRig(t, cfg, t.TempDir())
		rig.ledger.record(externalFact("pool-note", "a note from the read scope", time.Unix(0, 0).Add(2*time.Minute)), memory.PartitionIDFromName("agent-pool"))
		ownAt := time.Unix(0, 0).Add(3 * time.Minute)
		own := rig.ledger.record(externalFact("own-note", "a note on the agent's own partition", ownAt), rig.partition)

		outcome := rig.reflection.Tick(ctx, ownAt.Add(cfg.Cadence.MinGap+time.Minute))
		if outcome.Decision.Disposition != meditation.TickFired {
			t.Fatalf("tick disposition = %q (%s), want the own-partition fact to fire the reflection", outcome.Decision.Disposition, outcome.Decision.Reason)
		}
		if len(outcome.Scan.Partitions) != 1 || outcome.Scan.Partitions[0].Name != "agent-a" {
			t.Fatalf("observed partitions = %+v, want the own partition alone", outcome.Scan.Partitions)
		}
		if outcome.Scan.Verdict.Recent == nil || outcome.Scan.Verdict.Recent.Identity.Key != own {
			t.Fatalf("novelty fact = %+v, want the fact on the own partition", outcome.Scan.Verdict.Recent)
		}
		if outcome.Scan.Counts.ReferenceTotal != 1 {
			t.Fatalf("references = %d, want the read-scope note left off the undeclared face", outcome.Scan.Counts.ReferenceTotal)
		}

		quiet := newReflectionRig(t, reflectionConfig(), t.TempDir())
		quiet.ledger.record(externalFact("pool-note", "a note from the read scope", time.Unix(0, 0).Add(2*time.Minute)), memory.PartitionIDFromName("agent-pool"))
		if settled := quiet.reflection.Tick(ctx, time.Unix(0, 0).Add(3*time.Minute)); settled.Decision.Disposition != meditation.TickNoNovelty {
			t.Fatalf("tick = %q (%s), want the own partition alone deciding the judgment", settled.Decision.Disposition, settled.Decision.Reason)
		}
		if got := quiet.input.injectedCount(); got != 0 {
			t.Fatalf("injected inputs = %d, want a read-scope note never opening the own partition's gate", got)
		}
	})

	t.Run("the gate stays closed when the watermark cannot be established", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		dir := t.TempDir()
		cfg := reflectionConfig()
		rig := newReflectionRig(t, cfg, dir)
		noteAt := time.Unix(0, 0).Add(2 * time.Minute)
		rig.ledger.record(externalFact("fresh-note", "a note newer than the watermark", noteAt), rig.partition)
		record := filepath.Join(dir, "anchors.json")
		if err := os.WriteFile(record, []byte(`{"last_meditation":"yesterday"}`), 0o644); err != nil {
			t.Fatalf("write the damaged anchor record: %v", err)
		}

		outcome := rig.reflection.Tick(ctx, time.Unix(0, 0).Add(3*time.Minute))
		if outcome.Decision.Disposition != meditation.TickClosed || !errors.Is(outcome.Decision.Cause, meditation.ErrAnchorUnreadable) {
			t.Fatalf("tick = (%q, %v), want the gate closed on the unparsable watermark", outcome.Decision.Disposition, outcome.Decision.Cause)
		}
		if outcome.Decision.Reason == "" {
			t.Fatal("a closed gate must report the fact it could not establish")
		}
		if got := rig.input.injectedCount(); got != 0 {
			t.Fatalf("injected inputs = %d, want nothing injected past a closed gate", got)
		}
		if _, pending := rig.reflection.Pending(); pending {
			t.Fatal("a closed gate leaves no reflection input pending")
		}

		if err := os.WriteFile(record, []byte(`{}`), 0o644); err != nil {
			t.Fatalf("repair the anchor record: %v", err)
		}
		if repaired := rig.reflection.Tick(ctx, noteAt.Add(cfg.Cadence.MinGap+time.Minute)); repaired.Decision.Disposition != meditation.TickFired {
			t.Fatalf("repaired tick = %q (%s), want the same face firing once the watermark is readable", repaired.Decision.Disposition, repaired.Decision.Reason)
		}

		unreadable, err := meditation.NewAnchorStore(dir)
		if err != nil {
			t.Fatalf("build the store over a directory path: %v", err)
		}
		closed, err := meditation.NewReflection(reflectionConfig(), unreadable, rig.ledger, rig.input)
		if err != nil {
			t.Fatalf("assemble over the unreadable store: %v", err)
		}
		if got := closed.Tick(ctx, time.Unix(0, 0).Add(4*time.Minute)); !errors.Is(got.Decision.Cause, meditation.ErrAnchorUnreadable) {
			t.Fatalf("unreadable tick = (%q, %v), want the gate closed on an unreadable record", got.Decision.Disposition, got.Decision.Cause)
		}

		unwired, err := meditation.NewReflection(reflectionConfig(), nil, rig.ledger, rig.input)
		if err != nil {
			t.Fatalf("assemble without a watermark source: %v", err)
		}
		if got := unwired.Tick(ctx, time.Unix(0, 0).Add(5*time.Minute)); !errors.Is(got.Decision.Cause, meditation.ErrWatermarkSourceUnwired) {
			t.Fatalf("unwired tick = (%q, %v), want the gate closed on a missing watermark source", got.Decision.Disposition, got.Decision.Cause)
		}

		broken := newReflectionRig(t, reflectionConfig(), t.TempDir())
		brokenAt := time.Unix(0, 0).Add(2 * time.Minute)
		broken.ledger.record(externalFact("chain-note", "a note behind an unreachable chain", brokenAt), broken.partition)
		broken.ledger.fail(errors.New("the fact chain is unreachable"))
		if _, err := broken.reflection.FreshSince(ctx, time.Unix(0, 0)); err == nil {
			t.Fatal("a failed read must be reported as an error, never as nothing fresh")
		}
		if got := broken.reflection.Tick(ctx, time.Unix(0, 0).Add(3*time.Minute)); got.Decision.Disposition != meditation.TickClosed || got.Decision.Cause == nil {
			t.Fatalf("tick = (%q, %v), want the gate closed on the unreadable face", got.Decision.Disposition, got.Decision.Cause)
		}
		if got := broken.input.injectedCount(); got != 0 {
			t.Fatalf("injected inputs = %d, want no fallback criterion firing past an unreadable face", got)
		}
		broken.ledger.fail(nil)
		if got := broken.reflection.Tick(ctx, brokenAt.Add(cfg.Cadence.MinGap+time.Minute)); got.Decision.Disposition != meditation.TickFired {
			t.Fatalf("readable tick = %q (%s), want the same face firing once the chain answers", got.Decision.Disposition, got.Decision.Reason)
		}
	})

	t.Run("injection writes the input and consumption advances the watermark to it", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		dir := t.TempDir()
		cfg := reflectionConfig()
		rig := newReflectionRig(t, cfg, dir)
		rig.ledger.record(externalFact("first-note", "the fact the first reflection covers", time.Unix(0, 0).Add(2*time.Minute)), rig.partition)
		injected := time.Unix(0, 0).Add(2 * time.Minute).Add(cfg.Cadence.MinGap + time.Minute)
		rig.input.arm(meditation.ReflectionInFlight)

		fired := rig.reflection.Tick(ctx, injected)
		if fired.Decision.Disposition != meditation.TickFired || fired.Injection == nil {
			t.Fatalf("tick = (%q, %s), want one injected reflection input", fired.Decision.Disposition, fired.Decision.Reason)
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the fire = %s, want the injection to leave it in place", got)
		}
		if _, pending := rig.reflection.Pending(); !pending {
			t.Fatal("an in-flight reflection input awaits its outcome")
		}
		if err := rig.reflection.NoteOutcome(ctx, meditation.ReflectionReceipt{Key: fired.Injection.Key, Outcome: meditation.ReflectionConsumed, At: injected}); err != nil {
			t.Fatalf("settle the consumed turn: %v", err)
		}
		if _, pending := rig.reflection.Pending(); pending {
			t.Fatal("a consumed reflection input leaves nothing pending")
		}
		if got := rig.watermark(t); !got.Equal(injected) {
			t.Fatalf("watermark after the consumption = %s, want the injection moment %s", got, injected)
		}
		if got := rig.reflection.DeferredCount(); got != 0 {
			t.Fatalf("deferral count = %d, want none beside a consumed reflection", got)
		}

		replay := meditation.ReflectionReceipt{Key: fired.Injection.Key, Outcome: meditation.ReflectionConsumed, At: injected}
		if err := rig.reflection.NoteOutcome(ctx, replay); !errors.Is(err, meditation.ErrReflectionNotPending) {
			t.Fatalf("replayed outcome = %v, want the repeated report refused", err)
		}
		if got := rig.watermark(t); !got.Equal(injected) {
			t.Fatalf("watermark after the replay = %s, want it unmoved at %s", got, injected)
		}

		restarted := newReflectionRig(t, cfg, dir)
		if got := restarted.watermark(t); !got.Equal(injected) {
			t.Fatalf("watermark after a restart = %s, want the persisted %s", got, injected)
		}
		restarted.ledger.restore(rig.ledger)
		if got := restarted.reflection.Tick(ctx, injected.Add(10*time.Minute)); got.Decision.Disposition != meditation.TickNoNovelty {
			t.Fatalf("restart tick = %q (%s), want the covered fact no longer firing a reflection", got.Decision.Disposition, got.Decision.Reason)
		}
		restarted.ledger.record(externalFact("later-note", "a fact newer than the consumed reflection", injected.Add(20*time.Minute)), restarted.partition)
		if got := restarted.reflection.Tick(ctx, injected.Add(30*time.Minute)); got.Decision.Disposition != meditation.TickRhythm {
			t.Fatalf("rhythm tick = %q (%s), want the rhythm floor holding a fresh fact", got.Decision.Disposition, got.Decision.Reason)
		}
		if got := restarted.input.injectedCount(); got != 0 {
			t.Fatalf("injected inputs = %d, want none before the rhythm floor elapses", got)
		}

		restarted.input.arm(meditation.ReflectionInFlight)
		second := restarted.reflection.Tick(ctx, injected.Add(2*time.Hour))
		if second.Decision.Disposition != meditation.TickFired || second.Injection == nil {
			t.Fatalf("tick = (%q, %s), want the fresh fact firing past the rhythm floor", second.Decision.Disposition, second.Decision.Reason)
		}
		if _, pending := restarted.reflection.Pending(); !pending {
			t.Fatal("an in-flight reflection input stays pending")
		}
		if err := restarted.reflection.NoteOutcome(ctx, meditation.ReflectionReceipt{Key: "reflection-elsewhere", Outcome: meditation.ReflectionConsumed, At: injected.Add(2 * time.Hour)}); !errors.Is(err, meditation.ErrReflectionKeyMismatch) {
			t.Fatalf("mismatched outcome = %v, want the report about another input refused", err)
		}
		if got := restarted.watermark(t); !got.Equal(injected) {
			t.Fatalf("watermark after the mismatched report = %s, want it unmoved at %s", got, injected)
		}
		if _, pending := restarted.reflection.Pending(); !pending {
			t.Fatal("a refused report leaves the pending input awaiting its own outcome")
		}
		if err := restarted.reflection.NoteOutcome(ctx, meditation.ReflectionReceipt{Key: second.Injection.Key, Outcome: meditation.ReflectionConsumed, At: injected.Add(2 * time.Hour)}); err != nil {
			t.Fatalf("settle the in-flight input: %v", err)
		}
		if got := restarted.watermark(t); !got.Equal(injected.Add(2 * time.Hour)) {
			t.Fatalf("watermark after the settlement = %s, want the second injection moment", got)
		}
	})

	t.Run("a deferred reflection keeps the watermark and re-evaluates the same face", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := reflectionConfig()
		rig := newReflectionRig(t, cfg, t.TempDir())
		covered := time.Unix(0, 0).Add(2 * time.Minute)
		key := rig.ledger.record(externalFact("covered-note", "the fact a yielded reflection covered", covered), rig.partition)
		rig.input.arm(meditation.ReflectionDeferred)

		tickAt := covered.Add(cfg.Cadence.MinGap + time.Minute)
		first := rig.reflection.Tick(ctx, tickAt)
		if first.Decision.Disposition != meditation.TickFired {
			t.Fatalf("tick = (%q, %s), want the reflection firing on the covered fact", first.Decision.Disposition, first.Decision.Reason)
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the yield = %s, want it left in place", got)
		}
		if _, pending := rig.reflection.Pending(); pending {
			t.Fatal("a yielded reflection clears its pending input")
		}
		if got := rig.reflection.DeferredCount(); got != 1 {
			t.Fatalf("deferral count = %d, want the yield tallied", got)
		}
		if got := rig.reflection.LastDeferredAt(); !got.Equal(tickAt) {
			t.Fatalf("last deferral = %s, want the moment the yield was reported", got)
		}

		second := rig.reflection.Tick(ctx, tickAt.Add(time.Minute))
		if second.Decision.Disposition != meditation.TickFired {
			t.Fatalf("next tick = (%q, %s), want the same face re-evaluated and firing", second.Decision.Disposition, second.Decision.Reason)
		}
		if second.Scan == nil || second.Scan.Verdict.Recent == nil || second.Scan.Verdict.Recent.Identity.Key != key {
			t.Fatalf("re-evaluated face = %+v, want the covered fact still inside the window", second.Scan)
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the second fire = %s, want the yields leaving it in place", got)
		}
	})

	t.Run("the digest renders deterministically and bounded from the one pass the tick read", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := reflectionConfig()
		cfg.Face.ObservedNamespaces = nil
		cfg.Face.ReadNamespaces = nil
		cfg.Face.FactLimit = 3
		for i := 1; i <= 9; i++ {
			cfg.Face.ObservedNamespaces = append(cfg.Face.ObservedNamespaces, fmt.Sprintf("pool-%d", i))
			cfg.Face.ReadNamespaces = append(cfg.Face.ReadNamespaces, fmt.Sprintf("pool-%d", i))
		}
		dir := t.TempDir()
		rig := newReflectionRig(t, cfg, dir)
		long := strings.Repeat("d", 200)
		for i := 1; i <= 9; i++ {
			name := fmt.Sprintf("pool-%d", i)
			rig.ledger.record(lineageFact(fmt.Sprintf("note-%d", i), long, meditation.LineageReflection, time.Unix(0, 0).Add(time.Duration(i)*time.Minute)), memory.PartitionIDFromName(name))
		}

		outcome := rig.reflection.Tick(ctx, time.Unix(0, 0).Add(time.Hour))
		if outcome.Decision.Disposition != meditation.TickNoNovelty {
			t.Fatalf("tick = (%q, %s), want the self-managed face holding no novelty", outcome.Decision.Disposition, outcome.Decision.Reason)
		}
		if outcome.Scan == nil || outcome.Scan.Counts.HydratedTotal != cfg.Face.FactLimit {
			t.Fatalf("hydrated facts = %+v, want the pass bounded by its fact limit", outcome.Scan)
		}
		if got := rig.ledger.reads(); got != 1 {
			t.Fatalf("fact-chain reads = %d, want exactly the one pass the tick ran", got)
		}
		if got := len(rig.model.recorded()); got != 0 {
			t.Fatalf("model requests = %d, want the digest and the gate spending no model call", got)
		}

		body := outcome.Digest.Body
		if strings.Count(body, " reference(s), ") != 9 {
			t.Fatalf("digest =\n%s\nwant the pass header beside eight partition lines", body)
		}
		if !strings.Contains(body, "- pool-8:") || strings.Contains(body, "- pool-9:") {
			t.Fatalf("digest =\n%s\nwant the partition listing bounded at eight lines", body)
		}
		if got := strings.Count(body, " (self-managed, "); got != cfg.Face.FactLimit {
			t.Fatalf("digest fact lines = %d, want the listing bounded by the fact limit", got)
		}
		if outcome.Digest.Omitted != 6 {
			t.Fatalf("omitted = %d, want the six references the bound left out", outcome.Digest.Omitted)
		}
		if !strings.Contains(body, "omitted: 6 fact(s)") {
			t.Fatalf("digest =\n%s\nwant the omitted count reported", body)
		}
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, " (self-managed, ") {
				continue
			}
			start := strings.Index(line, ": ") + 2
			rendered := strings.Split(line[start:], " (self-managed, ")[0]
			if runes := utf8.RuneCountInString(rendered); runes != 121 {
				t.Fatalf("rendered summary = %d runes, want the 120-rune bound beside its ellipsis", runes)
			}
		}

		again, err := meditation.NewScanDigest(outcome.Scan, cfg.Face.FactLimit).Render(ctx, meditation.Watermark{Moment: outcome.Scan.Watermark})
		if err != nil {
			t.Fatalf("render the same pass: %v", err)
		}
		if again.Body != body || again.Omitted != outcome.Digest.Omitted {
			t.Fatalf("second render =\n%s\nwant the same pass rendering the same summary", again.Body)
		}
		if _, err := meditation.NewScanDigest(outcome.Scan, cfg.Face.FactLimit).Render(ctx, meditation.Watermark{Moment: time.Unix(0, 0)}); !errors.Is(err, meditation.ErrDigestScanMismatch) {
			t.Fatalf("mismatched render = %v, want the pass of another window refused", err)
		}
		if got := rig.ledger.reads(); got != 1 {
			t.Fatalf("fact-chain reads = %d, want the rendering reading no fact chain", got)
		}
		if _, err := meditation.NewScanDigest(nil, 0).Render(ctx, meditation.Watermark{}); !errors.Is(err, meditation.ErrDigestFaceUnavailable) {
			t.Fatalf("empty render = %v, want a digest without a pass refused by name", err)
		}
	})

	t.Run("the fired input is consumed by the agent's own loop session and observable on the chain", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := reflectionConfig()
		cfg.Prompt = "keep the ledger honest"
		rig := newReflectionRig(t, cfg, t.TempDir())
		source := externalFact("source-note", "the fact the reflection reads", time.Unix(0, 0).Add(2*time.Minute))
		sourceKey := rig.ledger.record(source, rig.partition)
		before := rig.ledger.snapshot()

		injected := time.Unix(0, 0).Add(2 * time.Minute).Add(cfg.Cadence.MinGap + time.Minute)
		outcome := rig.reflection.Tick(ctx, injected)
		if outcome.Decision.Disposition != meditation.TickFired || outcome.Injection == nil {
			t.Fatalf("tick = (%q, %s), want one injected reflection input", outcome.Decision.Disposition, outcome.Decision.Reason)
		}
		input := outcome.Injection.Event
		if input.Type != event.TypeExternalInput {
			t.Fatalf("input type = %q, want an explicit input event", input.Type)
		}
		if got := input.Meta[event.MetaKeyTriggerSource]; got != meditation.LineageReflection {
			t.Fatalf("input trigger source = %q, want the reflection lineage", got)
		}
		if !meditation.SelfManagedLineage(input.Meta[event.MetaKeyTriggerSource]) || !meditation.DeliverableLineage(input.Meta[event.MetaKeyTriggerSource]) {
			t.Fatalf("reflection lineage = %q, want a host-visible lineage the freshness judgment reads as self-managed", input.Meta[event.MetaKeyTriggerSource])
		}
		if input.Owner != rig.address || input.Source != rig.address || input.Target != rig.address {
			t.Fatalf("input address = (%+v, %+v, %+v), want the agent's own loop session", input.Owner, input.Source, input.Target)
		}
		if input.Message.Role != model.RoleUser || !strings.Contains(input.Message.Content, "[reflection]") ||
			!strings.Contains(input.Message.Content, "keep the ledger honest") || !strings.Contains(input.Message.Content, "- agent-a: ") {
			t.Fatalf("input body =\n%s\nwant the reflection prompt led by the digest of the pass that fired it", input.Message.Content)
		}

		requests := rig.model.recorded()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want the one decision of the injected turn", len(requests))
		}
		carried := false
		for _, msg := range requests[0].Messages {
			if strings.Contains(msg.Content, "[reflection]") && strings.Contains(msg.Content, "keep the ledger honest") {
				carried = true
			}
		}
		if !carried {
			t.Fatalf("loop request =\n%+v\nwant the reflection input carried into the agent's own decision", requests[0].Messages)
		}

		observed, err := rig.ledger.CommittedFact(ctx, outcome.Injection.Key)
		if err != nil {
			t.Fatalf("read the committed reflection fact: %v", err)
		}
		if observed.Meta[event.MetaKeyTriggerSource] != meditation.LineageReflection || observed.Type != event.TypeExternalInput {
			t.Fatalf("committed reflection fact = %+v, want the source=meditation input on the chain", observed)
		}
		refs, err := rig.ledger.QueryEvents(ctx, memory.QueryOptions{
			PartitionIDs: []int{rig.partition},
			OrderBy:      memory.QueryOrderByDesc,
		})
		if err != nil {
			t.Fatalf("query the agent's own partition: %v", err)
		}
		if len(refs) == 0 || refs[0].Key != outcome.Injection.Key || refs[0].Type != event.TypeExternalInput {
			t.Fatalf("chain references = %+v, want the reflection input observable by consumers of the same face", refs)
		}
		if !rig.ledger.unchanged(before, sourceKey) {
			t.Fatal("the reflection must not rewrite a fact it read")
		}

		next := rig.reflection.Tick(ctx, injected.Add(time.Minute))
		if next.Decision.Disposition != meditation.TickNoNovelty {
			t.Fatalf("next tick = %q (%s), want the reflection input never re-arming the judgment that produced it", next.Decision.Disposition, next.Decision.Reason)
		}
		if got := rig.input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs = %d, want no second input beside the self-managed one", got)
		}
		if got := rig.watermark(t); !got.Equal(injected) {
			t.Fatalf("watermark = %s, want the injection moment the consumed turn advanced it to", got)
		}
	})
}

// reflectionConfig is the assembly of one reflecting agent these tests run over:
// a one-agent loop session, a declared face of its own partition and a cadence
// wide enough that only the sub-case that names it meets the rhythm floor.
func reflectionConfig() meditation.ReflectionConfig {
	return meditation.ReflectionConfig{
		Loop:    meditation.ReflectionLoop{Agent: "agent-a", Session: "session-meditate"},
		Cadence: meditation.ReflectionCadence{Interval: time.Minute, MinGap: time.Hour},
		Prompt:  "reflect on the observed face",
	}
}

// externalFact builds an input fact of host lineage: the freshness evidence one
// reflection reads.
func externalFact(key, content string, at time.Time) event.Event {
	return event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeExternalInput,
		Timestamp: at,
		Meta:      map[string]string{event.MetaKeyTriggerSource: "user", event.MetaKeyEventSummary: content},
		Message:   model.Message{Role: model.RoleUser, Content: content},
	}
}

// lineageFact builds a fact carrying the named trigger source, so a sub-case can
// file an explicitly self-managed fact on the observed face.
func lineageFact(key, summary, lineage string, at time.Time) event.Event {
	return event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeExternalInput,
		Timestamp: at,
		Meta:      map[string]string{event.MetaKeyTriggerSource: lineage, event.MetaKeyEventSummary: summary},
		Message:   model.Message{Role: model.RoleUser, Content: summary},
	}
}

// reflectionLedger is the fact-chain face of these tests: it stores every recorded
// fact into the in-process query source, so the reflection reads through the
// production query semantics, and answers the committed-fact hydration out of the
// very records it stored.
type reflectionLedger struct {
	mu      sync.Mutex
	query   *memory.MemoryEventQuery
	facts   map[event.EventID]event.Event
	parts   map[event.EventID]int
	failErr error
	seeded  int
	read    int
}

// newReflectionLedger returns an empty fact chain.
func newReflectionLedger() *reflectionLedger {
	return &reflectionLedger{
		query: memory.NewMemoryEventQuery(),
		facts: map[event.EventID]event.Event{},
		parts: map[event.EventID]int{},
	}
}

// record files one fact under the named partition and reports the identity it
// committed under.
func (l *reflectionLedger) record(evt event.Event, partition int) event.EventID {
	l.mu.Lock()
	defer l.mu.Unlock()
	if evt.Key == "" {
		l.seeded++
		evt.Key = event.EventID(fmt.Sprintf("seeded-%d", l.seeded))
	}
	l.facts[evt.Key] = evt
	l.parts[evt.Key] = partition
	_ = l.query.Store(context.Background(), memory.EventRecord{
		Key:         evt.Key,
		PartitionID: partition,
		Type:        evt.Type,
		Summary:     evt.Meta[event.MetaKeyEventSummary],
		Content:     evt.Message.Content,
		Timestamp:   evt.Timestamp.UnixMilli(),
		WindowTS:    evt.Timestamp.UnixMilli(),
	})
	return evt.Key
}

// restore files every fact another ledger holds into this one, so a sub-case can
// read one face from a restarted assembly.
func (l *reflectionLedger) restore(other *reflectionLedger) {
	other.mu.Lock()
	snapshot := make(map[event.EventID]event.Event, len(other.facts))
	partitions := make(map[event.EventID]int, len(other.parts))
	for id, evt := range other.facts {
		snapshot[id] = evt
		partitions[id] = other.parts[id]
	}
	other.mu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, evt := range snapshot {
		if _, held := l.facts[id]; held {
			continue
		}
		l.facts[id] = evt
		l.parts[id] = partitions[id]
		_ = l.query.Store(context.Background(), memory.EventRecord{
			Key:         id,
			PartitionID: partitions[id],
			Type:        evt.Type,
			Summary:     evt.Meta[event.MetaKeyEventSummary],
			Content:     evt.Message.Content,
			Timestamp:   evt.Timestamp.UnixMilli(),
			WindowTS:    evt.Timestamp.UnixMilli(),
		})
	}
}

// snapshot copies the facts of the chain, so a sub-case can hold one fact's record
// across a tick.
func (l *reflectionLedger) snapshot() map[event.EventID]event.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[event.EventID]event.Event, len(l.facts))
	for id, evt := range l.facts {
		out[id] = evt
	}
	return out
}

// unchanged reports whether the named fact still reads as the snapshot recorded it.
func (l *reflectionLedger) unchanged(before map[event.EventID]event.Event, id event.EventID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	was, held := before[id]
	now, still := l.facts[id]
	if !held || !still {
		return false
	}
	return was.Key == now.Key && was.Type == now.Type && was.Timestamp.Equal(now.Timestamp) &&
		was.Message.Content == now.Message.Content && was.Meta[event.MetaKeyEventSummary] == now.Meta[event.MetaKeyEventSummary]
}

// fail arms the read face to report err on every query, so a sub-case can drive
// the gate over an unreachable chain.
func (l *reflectionLedger) fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failErr = err
}

// QueryEvents answers the conditional retrieval through the in-process query
// source and counts the pass.
func (l *reflectionLedger) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	l.mu.Lock()
	l.read++
	fail := l.failErr
	l.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	return l.query.QueryEvents(ctx, opts)
}

// CommittedFact answers the hydration of one reference, refusing an identity that
// never committed.
func (l *reflectionLedger) CommittedFact(_ context.Context, id event.EventID) (event.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	evt, held := l.facts[id]
	if !held {
		return event.Event{}, fmt.Errorf("reflection ledger: fact %s did not commit", id)
	}
	return evt, nil
}

// reads reports how many fact-chain passes ran.
func (l *reflectionLedger) reads() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.read
}

var _ meditation.ObservedFacts = (*reflectionLedger)(nil)

// The reflection loop satisfies the declared observation-source face of the plugin
// layer, which is the face a consumer of the loop holds it by.
var _ plugin.ObservationSource = (*meditation.Reflection)(nil)

// reflectionModel is the native model stand-in of the rig: it records every
// request the loop assembles and answers it with a completed turn, so a decision
// that carries the reflection input ran to its terminal without a real endpoint.
type reflectionModel struct {
	mu       sync.Mutex
	name     string
	requests []model.Request
}

// GenerateContent records the native request and streams the completed answer.
func (m *reflectionModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reflection settled"}}},
		Done:    true,
	}
	close(ch)
	return ch, nil
}

// Info reports the stand-in's descriptor.
func (m *reflectionModel) Info() model.Info { return model.Info{Name: m.name} }

// recorded reports the native requests the loop assembled.
func (m *reflectionModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// loopSessionInput is the loop-session face of these tests: it commits the
// reflection input as a fact of the agent's own partition, drives the agent's own
// loop session with it through the real loop entry, and reports what became of the
// turn. A sub-case arms the outcome a drive reports when the loop cannot report one
// itself.
type loopSessionInput struct {
	loop      *agent.Loop
	ledger    *reflectionLedger
	address   event.Address
	instance  event.InstanceID
	partition int

	mu       sync.Mutex
	armed    meditation.ReflectionOutcome
	injected []event.Event
	seq      int
}

// arm sets the outcome the next injections report beside the loop's own terminal.
func (i *loopSessionInput) arm(outcome meditation.ReflectionOutcome) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.armed = outcome
}

// InjectReflection commits the input, drives the agent's own loop session with it
// and reports the outcome of that turn.
func (i *loopSessionInput) InjectReflection(ctx context.Context, evt event.Event) (meditation.ReflectionReceipt, error) {
	i.mu.Lock()
	i.seq++
	key := event.EventID(fmt.Sprintf("reflection-%d", i.seq))
	armed := i.armed
	i.mu.Unlock()

	evt.Key = key
	i.ledger.record(evt, i.partition)
	i.mu.Lock()
	i.injected = append(i.injected, evt)
	i.mu.Unlock()

	turn, err := i.loop.Drive(ctx, agent.Invocation{
		InvocationID: event.InvocationID("invocation-" + string(key)),
		Address:      i.address,
		Instance:     i.instance,
		Ready:        []event.Event{evt},
	})
	if err != nil {
		return meditation.ReflectionReceipt{}, err
	}
	if armed != "" {
		return meditation.ReflectionReceipt{Key: key, Outcome: armed, At: evt.Timestamp, Reason: "the sub-case armed this outcome"}, nil
	}
	if turn.Terminal != model.KindCompleted {
		return meditation.ReflectionReceipt{Key: key, Outcome: meditation.ReflectionDeferred, At: evt.Timestamp, Reason: "the reflection turn did not reach a completed terminal"}, nil
	}
	return meditation.ReflectionReceipt{Key: key, Outcome: meditation.ReflectionConsumed, At: evt.Timestamp}, nil
}

// injectedCount reports how many reflection inputs were handed to the loop.
func (i *loopSessionInput) injectedCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.injected)
}

var _ meditation.ReflectionInput = (*loopSessionInput)(nil)

// reflectionRig is one reflecting agent assembled over a temp anchor directory, a
// fact chain and the agent's own loop session: the tick, the watermark record and
// the injected inputs a sub-case checks all belong to it.
type reflectionRig struct {
	ledger     *reflectionLedger
	model      *reflectionModel
	store      *meditation.AnchorStore
	input      *loopSessionInput
	reflection *meditation.Reflection

	address   event.Address
	partition int
}

// newReflectionRig assembles one reflecting agent over the anchor directory and
// the loop session its config names.
func newReflectionRig(t *testing.T, cfg meditation.ReflectionConfig, dir string) *reflectionRig {
	t.Helper()
	instance := event.InstanceID("instance-reflect")
	address := event.Address{AgentID: event.AgentID(cfg.Loop.Agent), SessionID: event.SessionID(cfg.Loop.Session)}
	ledger := newReflectionLedger()
	provider := &reflectionModel{name: "model-a"}

	decl := &config.Declaration{
		Entry:  cfg.Loop.Agent,
		Agents: []config.AgentSpec{{ID: cfg.Loop.Agent, Model: provider.name}},
	}
	binding := state.AgentBinding{
		Model:        provider,
		PromptSource: state.PromptSource{Version: "prompt-reflect-v1"},
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: decl}, map[event.AgentID]state.AgentBinding{event.AgentID(cfg.Loop.Agent): binding}, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
	loop := agent.NewLoop(bindings, state.NewOccupancy(), instance, nil)

	store, err := meditation.NewAnchorStore(filepath.Join(dir, "anchors.json"))
	if err != nil {
		t.Fatalf("build the anchor store: %v", err)
	}
	input := &loopSessionInput{
		loop:      loop,
		ledger:    ledger,
		address:   address,
		instance:  instance,
		partition: memory.PartitionIDFromName(cfg.Loop.Agent),
	}
	reflection, err := meditation.NewReflection(cfg, store, ledger, input)
	if err != nil {
		t.Fatalf("assemble the reflection: %v", err)
	}
	return &reflectionRig{
		ledger:     ledger,
		model:      provider,
		store:      store,
		input:      input,
		reflection: reflection,
		address:    address,
		partition:  memory.PartitionIDFromName(cfg.Loop.Agent),
	}
}

// watermark reads the persisted execution watermark of the rig.
func (r *reflectionRig) watermark(t *testing.T) time.Time {
	t.Helper()
	current, err := r.store.Current(context.Background())
	if err != nil {
		t.Fatalf("read the watermark: %v", err)
	}
	return current.Moment
}
