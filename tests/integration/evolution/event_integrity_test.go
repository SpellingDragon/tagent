package evolution

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// TestEventIntegrityMeditationSchedule drives the reflection rhythm and its execution watermark through the production reflection loop and the formal assembly.
// - It checks the schedule the frozen delta fixes.
//   - material newer than the watermark fires on cadence, while a deferred pass leaves the very material inside the window
//   - a start with no consumed reflection passes through the rhythm floor once and then locks itself
//   - a reflection turn that ends short of a completed terminal is a deferral: the watermark stays and the material is re-evaluated
//   - an observation that arrives while a pass is in flight is retained for the next evaluation and never dropped as busy
//   - the reflection's own product never re-enters the judgment that produced it
//   - the formal assembly drives a real reflection turn, and the material is not recorded as consumed before that turn completes
//   - the anchors that must fire tick outside the min gap of the material they read, so the activity side of the rhythm floor leaves the fired pass its subject
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestEventIntegrityMeditationSchedule(t *testing.T) {
	t.Run("new_material_on_cadence", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := reflectionConfig()
		rig := newReflectionRig(t, cfg, t.TempDir())
		firstAt := time.Unix(0, 0).Add(2 * time.Minute)
		firstKey := rig.ledger.record(externalFact("cadence-first", "the material the deferred pass covered", firstAt), rig.partition)

		rig.input.arm(meditation.ReflectionDeferred)
		deferredAt := firstAt.Add(cfg.Cadence.MinGap + time.Minute)
		deferred := rig.reflection.Tick(ctx, deferredAt)
		if deferred.Decision.Disposition != meditation.TickFired || deferred.Injection == nil {
			t.Fatalf("deferred tick = (%q, %s), want the material firing the reflection", deferred.Decision.Disposition, deferred.Decision.Reason)
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the yield = %s, want the injection leaving it in place", got)
		}
		if got := rig.reflection.DeferredCount(); got != 1 {
			t.Fatalf("deferral count = %d, want the yielded pass tallied", got)
		}

		rig.input.arm(meditation.ReflectionConsumed)
		consumedAt := deferredAt.Add(time.Minute)
		consumed := rig.reflection.Tick(ctx, consumedAt)
		if consumed.Decision.Disposition != meditation.TickFired || consumed.Injection == nil {
			t.Fatalf("re-evaluated tick = (%q, %s), want the same face firing once it is read again", consumed.Decision.Disposition, consumed.Decision.Reason)
		}
		if consumed.Scan == nil || consumed.Scan.Verdict.Recent == nil || consumed.Scan.Verdict.Recent.Identity.Key != firstKey {
			t.Fatalf("re-evaluated face = %+v, want the yielded material still inside the window", consumed.Scan)
		}
		if got := rig.watermark(t); !got.Equal(consumedAt) {
			t.Fatalf("watermark after the consumption = %s, want the injection moment %s", got, consumedAt)
		}

		laterAt := consumedAt.Add(30 * time.Second)
		laterKey := rig.ledger.record(externalFact("cadence-later", "the material newer than the consumed reflection", laterAt), rig.partition)
		held := rig.reflection.Tick(ctx, laterAt)
		if held.Decision.Disposition != meditation.TickRhythm {
			t.Fatalf("held tick = (%q, %s), want the rhythm floor holding the fresh material", held.Decision.Disposition, held.Decision.Reason)
		}
		if held.Scan == nil || held.Scan.Verdict.Recent == nil || held.Scan.Verdict.Recent.Identity.Key != laterKey {
			t.Fatalf("held face = %+v, want the fresh material inside the window", held.Scan)
		}
		if got := rig.input.injectedCount(); got != 2 {
			t.Fatalf("injected inputs = %d, want none beside the two passes before the rhythm floor elapses", got)
		}

		cadenceAt := laterAt.Add(cfg.Cadence.MinGap + time.Minute)
		fired := rig.reflection.Tick(ctx, cadenceAt)
		if fired.Decision.Disposition != meditation.TickFired || fired.Injection == nil {
			t.Fatalf("cadence tick = (%q, %s), want the fresh material firing once the floor elapses", fired.Decision.Disposition, fired.Decision.Reason)
		}
		if fired.Scan == nil || fired.Scan.Verdict.Recent == nil || fired.Scan.Verdict.Recent.Identity.Key != laterKey {
			t.Fatalf("cadence face = %+v, want the material newer than the watermark", fired.Scan)
		}
		if got := rig.watermark(t); !got.Equal(cadenceAt) {
			t.Fatalf("watermark after the cadence fire = %s, want the injection moment %s", got, cadenceAt)
		}
		if got := rig.input.injectedCount(); got != 3 {
			t.Fatalf("injected inputs = %d, want one reflection per due material", got)
		}
	})

	t.Run("cold_start_passes_through", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := reflectionConfig()
		rig := newReflectionRig(t, cfg, t.TempDir())
		firstAt := time.Unix(0, 0).Add(2 * time.Minute)
		firstKey := rig.ledger.record(externalFact("cold-first", "the material a start with no watermark reads", firstAt), rig.partition)

		rig.input.arm(meditation.ReflectionConsumed)
		firstTick := firstAt.Add(cfg.Cadence.MinGap + time.Minute)
		first := rig.reflection.Tick(ctx, firstTick)
		if first.Decision.Disposition != meditation.TickFired || first.Injection == nil {
			t.Fatalf("cold tick = (%q, %s), want the first tick passing through the rhythm floor", first.Decision.Disposition, first.Decision.Reason)
		}
		if first.Scan == nil || first.Scan.Verdict.Recent == nil || first.Scan.Verdict.Recent.Identity.Key != firstKey {
			t.Fatalf("cold face = %+v, want the new material it read", first.Scan)
		}
		if got := rig.watermark(t); !got.Equal(firstTick) {
			t.Fatalf("watermark after the cold pass = %s, want the injection moment %s", got, firstTick)
		}
		if got := rig.input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs = %d, want exactly the one cold pass", got)
		}

		laterAt := firstTick.Add(time.Minute)
		laterKey := rig.ledger.record(externalFact("cold-later", "the material the locked rhythm holds", laterAt), rig.partition)
		locked := rig.reflection.Tick(ctx, laterAt.Add(time.Minute))
		if locked.Decision.Disposition != meditation.TickRhythm {
			t.Fatalf("locked tick = (%q, %s), want the cold pass locking the rhythm behind itself", locked.Decision.Disposition, locked.Decision.Reason)
		}
		if locked.Scan == nil || locked.Scan.Verdict.Recent == nil || locked.Scan.Verdict.Recent.Identity.Key != laterKey {
			t.Fatalf("locked face = %+v, want the fresh material held inside the window", locked.Scan)
		}
		if got := rig.input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs = %d, want the cold pass to be the only one", got)
		}
		if got := rig.watermark(t); !got.Equal(firstTick) {
			t.Fatalf("watermark after the locked tick = %s, want it unmoved at %s", got, firstTick)
		}
	})

	t.Run("non_completed_terminal_defers", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		rig := newFormalMeditationRig(t, nil, errors.New("the meditation endpoint is unreachable"), nil)
		first := "the material the failed reflection must not consume"
		rig.invokeUserTurn(t, ctx, "inv-integrity-failed-1", first)
		firstKey := rig.factKey(t, first)

		failed := rig.awaitFire(t)
		if failed.Decision.Disposition != meditation.TickFired || failed.Injection == nil {
			t.Fatalf("failed tick = (%q, %s), want the reflection turn attempted", failed.Decision.Disposition, failed.Decision.Reason)
		}
		if failed.Scan == nil || failed.Scan.Verdict.Recent == nil || failed.Scan.Verdict.Recent.Identity.Key != firstKey {
			t.Fatalf("failed face = %+v, want the material the attempted turn read", failed.Scan)
		}
		if got := rig.watermark(t, ctx); !got.Moment.IsZero() {
			t.Fatalf("watermark after the failed terminal = %s, want it left at the pre-injection value", got.Moment)
		}
		requests := rig.medModel.recorded()
		if len(requests) != 1 {
			t.Fatalf("meditation requests = %d, want exactly the one real reflection turn that failed", len(requests))
		}
		if last := lastRequestMessage(requests[0]); !strings.HasPrefix(last.Content, "[reflection]") {
			t.Fatalf("reflection input = %q, want the formal assembly driving the reflection turn", last.Content)
		}

		time.Sleep(100 * time.Millisecond)
		second := "the material the next attempt adds"
		rig.invokeUserTurn(t, ctx, "inv-integrity-failed-2", second)
		secondKey := rig.factKey(t, second)

		again := rig.awaitFire(t)
		if again.Scan == nil {
			t.Fatal("the re-evaluated tick carries no scan")
		}
		if !again.Scan.Watermark.IsZero() {
			t.Fatalf("re-evaluated window = %s, want it opened at the unmoved watermark", again.Scan.Watermark)
		}
		if again.Scan.Verdict.Recent == nil || again.Scan.Verdict.Recent.Identity.Key != secondKey {
			t.Fatalf("re-evaluated face = %+v, want the newer material read", again.Scan.Verdict)
		}
		refs, err := rig.run.ObservedEvents(ctx, memory.QueryOptions{
			PartitionIDs: []int{memory.PartitionIDFromName("user-agent")},
			OrderBy:      memory.QueryOrderByDesc,
		})
		if err != nil {
			t.Fatalf("read the observed face: %v", err)
		}
		stillInside := false
		for _, ref := range refs {
			if ref.Key == firstKey {
				stillInside = true
				break
			}
		}
		if !stillInside {
			t.Fatalf("observed face = %+v, want the first material still inside the window opened at the unmoved watermark", refs)
		}
		if got := rig.watermark(t, ctx); !got.Moment.IsZero() {
			t.Fatalf("watermark after the second failed terminal = %s, want the failed turns never counted as consumed", got.Moment)
		}
	})

	t.Run("busy_observation_holds_watermark", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := reflectionConfig()
		cfg.Cadence.MinGap = 0
		rig := newReflectionRig(t, cfg, t.TempDir())
		input := newHeldReflectionInput(rig.ledger, rig.partition)
		reflection, err := meditation.NewReflection(cfg, rig.store, rig.ledger, input)
		if err != nil {
			t.Fatalf("assemble the reflection over the held input: %v", err)
		}
		driver := meditation.NewDriver(reflection, 0, 0, nil, "")
		t.Cleanup(driver.Stop)

		firstKey := rig.ledger.record(externalFact("busy-first", "the material the in-flight pass reads", time.Now().Add(-time.Minute)), rig.partition)
		if err := driver.ObserveCommit(ctx, firstKey); err != nil {
			t.Fatalf("observe the first commit: %v", err)
		}
		var first event.Event
		select {
		case first = <-input.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("the driver started no reflection pass")
		}

		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark while the first pass is in flight = %s, want the injection leaving it in place", got)
		}
		time.Sleep(10 * time.Millisecond)
		busy := "the material that arrived while the pass was in flight"
		busyKey := rig.ledger.record(externalFact("busy-material", busy, time.Now()), rig.partition)
		if err := driver.ObserveCommit(ctx, busyKey); err != nil {
			t.Fatalf("observe the busy commit: %v", err)
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the busy observation = %s, want the busy observation never advancing it", got)
		}
		if got := input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs while busy = %d, want the in-flight pass alone", got)
		}

		close(input.release)

		// The retained observation is the next evaluation: it opens after the
		// watermark the first consumption advanced to, so the material is read.
		var second event.Event
		select {
		case second = <-input.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("the busy observation was dropped: no next tick re-evaluated the material it named")
		}
		if !strings.HasPrefix(second.Message.Content, "[reflection]") {
			t.Fatalf("next reflection input = %q, want a real reflection input", second.Message.Content)
		}
		if !strings.Contains(second.Message.Content, busy) {
			t.Fatalf("next reflection input =\n%s\nwant the material that arrived while the pass was in flight", second.Message.Content)
		}
		if !second.Timestamp.After(first.Timestamp) {
			t.Fatalf("next injection = %s, want it after the first injection %s", second.Timestamp, first.Timestamp)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			current, err := rig.store.Current(ctx)
			if err != nil {
				t.Fatalf("read the watermark: %v", err)
			}
			if current.Moment.Equal(second.Timestamp) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("watermark after the retained evaluation = %s, want the second injection moment %s", current.Moment, second.Timestamp)
			}
			time.Sleep(5 * time.Millisecond)
		}
	})

	t.Run("own_product_no_reentry", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cfg := reflectionConfig()
		rig := newReflectionRig(t, cfg, t.TempDir())
		sourceAt := time.Unix(0, 0).Add(2 * time.Minute)
		rig.ledger.record(externalFact("reentry-source", "the host material the reflection reads", sourceAt), rig.partition)
		rig.input.arm(meditation.ReflectionConsumed)
		consumedAt := sourceAt.Add(cfg.Cadence.MinGap + time.Minute)
		fired := rig.reflection.Tick(ctx, consumedAt)
		if fired.Decision.Disposition != meditation.TickFired || fired.Injection == nil {
			t.Fatalf("tick = (%q, %s), want the host material firing the reflection", fired.Decision.Disposition, fired.Decision.Reason)
		}

		own, err := rig.ledger.CommittedFact(ctx, fired.Injection.Key)
		if err != nil {
			t.Fatalf("read the committed reflection input: %v", err)
		}
		if got := own.Meta[event.MetaKeyTriggerSource]; got != meditation.LineageReflection {
			t.Fatalf("reflection input stamp = %q, want the reflection lineage", got)
		}
		if !meditation.SelfManagedLineage(own.Meta[event.MetaKeyTriggerSource]) {
			t.Fatal("the reflection input reads as host-facing material, want it self-managed")
		}
		var policy meditation.NoveltyPolicy
		if policy.Novel(own) {
			t.Fatal("the reflection's own product reads as new material, want the freshness policy excluding it")
		}

		outcomes := make(chan meditation.TickOutcome, 4)
		driver := meditation.NewDriver(rig.reflection, 0, 0, nil, "")
		driver.SetObserver(func(outcome meditation.TickOutcome) { outcomes <- outcome })
		t.Cleanup(driver.Stop)
		if err := driver.ObserveCommit(ctx, fired.Injection.Key); err != nil {
			t.Fatalf("observe the own product: %v", err)
		}
		select {
		case outcome := <-outcomes:
			t.Fatalf("own product re-entered the driver: %q (%s)", outcome.Decision.Disposition, outcome.Decision.Reason)
		case <-time.After(300 * time.Millisecond):
		}
		if got := rig.input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs after the own product = %d, want no re-entry", got)
		}
		if got := rig.watermark(t); !got.Equal(consumedAt) {
			t.Fatalf("watermark after the own product = %s, want it unmoved at %s", got, consumedAt)
		}

		ownLater := rig.ledger.record(lineageFact("reentry-later", "a later self-managed product", meditation.LineageReflection, consumedAt.Add(time.Minute)), rig.partition)
		settled := rig.reflection.Tick(ctx, consumedAt.Add(2*time.Minute))
		if settled.Decision.Disposition != meditation.TickNoNovelty {
			t.Fatalf("tick = (%q, %s), want the self-managed traffic holding no novelty", settled.Decision.Disposition, settled.Decision.Reason)
		}
		if settled.Scan == nil || settled.Scan.Verdict.Novelty {
			t.Fatalf("verdict = %+v, want the self-managed fact never opening the gate", settled.Scan)
		}
		found := false
		for _, fact := range settled.Scan.Facts {
			if fact.Identity.Key == ownLater {
				found = true
				if !fact.Attribution.SelfManaged {
					t.Fatalf("scanned attribution = %+v, want the self-managed lineage", fact.Attribution)
				}
			}
		}
		if !found {
			t.Fatalf("scanned facts = %+v, want the later self-managed fact inside the window", settled.Scan.Facts)
		}
		if got := settled.Scan.Counts.UnknownLineage; got != 0 {
			t.Fatalf("unknown lineage = %d, want the self-managed stamp recognized", got)
		}
		if got := rig.input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs = %d, want the own product never re-entering", got)
		}
	})

	t.Run("formal_reflection_consumer", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		gate := make(chan struct{})
		rig := newFormalMeditationRig(t, func(*model.Request) *model.Response {
			return meditationFinalTurn("the reflection turn settled")
		}, nil, gate)

		material := "the material the formal reflection turn reads"
		rig.invokeUserTurn(t, ctx, "inv-integrity-consumer", material)
		select {
		case <-rig.medModel.started:
		case <-time.After(20 * time.Second):
			t.Fatal("the formal assembly drove no reflection turn")
		}

		requests := rig.medModel.recorded()
		if len(requests) != 1 {
			t.Fatalf("meditation requests = %d, want exactly one real reflection turn", len(requests))
		}
		reflectionInput := lastRequestMessage(requests[0])
		if !strings.HasPrefix(reflectionInput.Content, "[reflection]") {
			t.Fatalf("reflection input = %q, want the formal assembly driving the reflection turn", reflectionInput.Content)
		}
		if !strings.Contains(reflectionInput.Content, "write down what the observation found") {
			t.Fatalf("reflection input =\n%s\nwant the configured prompt carried into the real turn", reflectionInput.Content)
		}
		if got := rig.watermark(t, ctx); !got.Moment.IsZero() {
			t.Fatalf("watermark while the reflection turn is in flight = %s, want the material not yet recorded as consumed", got.Moment)
		}
		injected := rig.reflectionFacts(t)
		if len(injected) != 1 || injected[0].Meta[event.MetaKeyTriggerSource] != meditation.LineageReflection {
			t.Fatalf("meditator chain facts = %+v, want the one reflection input the assembly committed", injected)
		}

		close(gate)
		fired := rig.awaitFire(t)
		if fired.Decision.Disposition != meditation.TickFired || fired.Injection == nil {
			t.Fatalf("fired tick = (%q, %s), want the consumed reflection turn", fired.Decision.Disposition, fired.Decision.Reason)
		}
		materialKey := rig.factKey(t, material)
		if fired.Scan == nil || fired.Scan.Verdict.Recent == nil || fired.Scan.Verdict.Recent.Identity.Key != materialKey {
			t.Fatalf("fired face = %+v, want the material the formal turn reflected on", fired.Scan)
		}
		current := rig.watermark(t, ctx)
		if !current.Moment.Equal(fired.Injection.At) {
			t.Fatalf("watermark after the consumed turn = %s, want the injection moment %s", current.Moment, fired.Injection.At)
		}
		if !current.Moment.After(fired.Scan.Verdict.Recent.Content.Timestamp) {
			t.Fatalf("watermark = %s, want it advanced past the material it consumed", current.Moment)
		}
	})
}

// meditationPassPolicy is the deterministic stand-in governance of the formal
// assembly these checks run over: the fixed passing dispositions are mechanism
// inputs carrying no quality conclusion.
type meditationPassPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (meditationPassPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       meditationPassingCheck("eval"),
		"benchmark":  meditationPassingCheck("benchmark"),
		"apply-gate": meditationPassingCheck("apply-gate"),
	}, nil
}

// meditationPassingCheck reports the passing disposition of one named mechanism
// check.
func meditationPassingCheck(name state.CheckName) state.CheckAnswer {
	return state.CheckAnswer{Name: name, Verdict: state.CheckPass, Reason: "the stand-in passes the mechanism check", Scope: "assembly", Evidence: []string{"deterministic stand-in"}}
}

// meditationEntryModel is the native model stand-in of the formal assembly: it
// records every native request, optionally signals and holds the reflection
// request, and then answers it with its fixed terminal or its fixed failure.
type meditationEntryModel struct {
	name     string
	answer   func(*model.Request) *model.Response
	failure  error
	gate     <-chan struct{}
	started  chan struct{}
	mu       sync.Mutex
	requests []model.Request
}

// GenerateContent records the native request and answers it with the terminal
// this stand-in carries.
func (m *meditationEntryModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	started, gate, answer, failure := m.started, m.gate, m.answer, m.failure
	m.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if failure != nil {
		return nil, failure
	}
	if answer == nil {
		return nil, errors.New("the meditation stand-in carries no answer")
	}
	stream := make(chan *model.Response, 1)
	stream <- answer(req)
	close(stream)
	return stream, nil
}

// Info reports the stand-in's descriptor.
func (m *meditationEntryModel) Info() model.Info { return model.Info{Name: m.name} }

// recorded reports the native requests the loop assembled.
func (m *meditationEntryModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// meditationFinalTurn is the completed final answer of the stand-in.
func meditationFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

// lastRequestMessage reports the last message the request carries, which is the
// material the decision runs on.
func lastRequestMessage(req model.Request) model.Message {
	if len(req.Messages) == 0 {
		return model.Message{}
	}
	return req.Messages[len(req.Messages)-1]
}

// formalMeditationRig is one formally assembled runtime whose reflection driver
// observes the user agent's partition through the assembly's public faces and
// injects into the meditator's own loop session: the same construction the host
// wires, driven through Run.Invoke and the real loop.
type formalMeditationRig struct {
	run       *assembly.Run
	userModel *meditationEntryModel
	medModel  *meditationEntryModel
	userAddr  event.Address
	medAddr   event.Address
	fired     chan meditation.TickOutcome
}

// newFormalMeditationRig assembles the rig over a local storage tier, the
// run's anchor record and the run-level plugin builder, so the reflection reads
// the chain the run commits and its turn runs through Run.ReflectionInput.
func newFormalMeditationRig(t *testing.T, medAnswer func(*model.Request) *model.Response, medFailure error, medGate <-chan struct{}) *formalMeditationRig {
	t.Helper()
	root := t.TempDir()
	userModel := &meditationEntryModel{name: "model-user", answer: func(*model.Request) *model.Response {
		return meditationFinalTurn("the user turn settled")
	}}
	medModel := &meditationEntryModel{name: "model-meditator", answer: medAnswer, failure: medFailure, gate: medGate, started: make(chan struct{}, 1)}
	fired := make(chan meditation.TickOutcome, 8)

	decl := &config.Declaration{
		Entry: "user-agent",
		Agents: []config.AgentSpec{
			{ID: "user-agent", Model: "model-user", MaxTokens: 100},
			{ID: "meditator", Model: "model-meditator"},
		},
	}
	run, err := assembly.Assemble(context.Background(), decl, map[event.AgentID]assembly.NativeAgent{
		"user-agent": {Model: userModel},
		"meditator":  {Model: medModel},
	},
		assembly.WithGovernance(meditationPassPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}),
		assembly.WithReflectionAnchors(filepath.Join(root, "anchors.json")),
		assembly.WithPlugins(func(assembled *assembly.Run) ([]plugin.Instance, error) {
			reflection, err := meditation.NewReflection(meditation.ReflectionConfig{
				Loop: meditation.ReflectionLoop{Agent: "meditator", Session: "meditator-loop"},
				Face: meditation.ReflectionFace{
					ObservedNamespaces: []string{"user-agent"},
					ReadNamespaces:     []string{"user-agent", "meditator"},
				},
				Cadence: meditation.ReflectionCadence{Interval: 0},
				Prompt:  "write down what the observation found",
			}, assembled.ReflectionGate(), assembled.ObservationFacts(), assembled.ReflectionInput())
			if err != nil {
				return nil, err
			}
			driver := meditation.NewDriver(reflection, 0, 0, nil, "")
			driver.SetObserver(func(outcome meditation.TickOutcome) {
				if outcome.Decision.Disposition != meditation.TickFired {
					return
				}
				select {
				case fired <- outcome:
				default:
				}
			})
			t.Cleanup(driver.Stop)
			return []plugin.Instance{driver}, nil
		}),
	)
	if err != nil {
		t.Fatalf("assemble the formal meditation rig: %v", err)
	}
	return &formalMeditationRig{
		run:       run,
		userModel: userModel,
		medModel:  medModel,
		userAddr:  event.Address{AgentID: "user-agent", SessionID: "session-user"},
		medAddr:   event.Address{AgentID: "meditator", SessionID: "meditator-loop"},
		fired:     fired,
	}
}

// invokeUserTurn drives one real user turn through the assembled run, which is
// where the production commit face offers the material to the reflection driver.
func (r *formalMeditationRig) invokeUserTurn(t *testing.T, ctx context.Context, invocation event.InvocationID, content string) {
	t.Helper()
	turn, err := r.run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID:  invocation,
		Address:       r.userAddr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: content}},
	})
	if err != nil {
		t.Fatalf("invoke the user turn %q: %v", invocation, err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("user turn %q terminal = %v, want the completed turn that committed the material", invocation, turn.Terminal)
	}
}

// awaitFire waits for one fired reflection pass to settle.
func (r *formalMeditationRig) awaitFire(t *testing.T) meditation.TickOutcome {
	t.Helper()
	select {
	case outcome := <-r.fired:
		return outcome
	case <-time.After(20 * time.Second):
		t.Fatal("the formal reflection driver fired no pass")
		return meditation.TickOutcome{}
	}
}

// watermark reads the execution watermark the assembly's gate holds.
func (r *formalMeditationRig) watermark(t *testing.T, ctx context.Context) meditation.Watermark {
	t.Helper()
	gate := r.run.ReflectionGate()
	if gate == nil {
		t.Fatal("the formal assembly carries no reflection gate")
	}
	current, err := gate.Current(ctx)
	if err != nil {
		t.Fatalf("read the watermark: %v", err)
	}
	return current
}

// factKey reports the identity the material committed under on the user chain.
func (r *formalMeditationRig) factKey(t *testing.T, content string) event.EventID {
	t.Helper()
	for _, fact := range r.run.SessionFacts(r.userAddr) {
		if fact.Message.Content == content && fact.Meta[event.MetaKeyTriggerSource] == "user" {
			return fact.Key
		}
	}
	t.Fatalf("no committed material on the user chain reads as %q", content)
	return ""
}

// reflectionFacts reports the reflection inputs the assembly committed into the
// meditator's own chain.
func (r *formalMeditationRig) reflectionFacts(t *testing.T) []event.Event {
	t.Helper()
	out := make([]event.Event, 0, 1)
	for _, fact := range r.run.SessionFacts(r.medAddr) {
		if fact.Meta[event.MetaKeyTriggerSource] == meditation.LineageReflection {
			out = append(out, fact)
		}
	}
	return out
}

// heldReflectionInput is the reflection injection face of the busy sub-case: it
// commits each input into the observed chain, reports it to the test and holds
// the turn until the test releases it, so a second observation arrives while
// the first pass is still in flight.
type heldReflectionInput struct {
	ledger    *reflectionLedger
	partition int
	entered   chan event.Event
	release   chan struct{}

	mu       sync.Mutex
	seq      int
	injected []event.Event
}

// newHeldReflectionInput returns the held injection face over the observed chain.
func newHeldReflectionInput(ledger *reflectionLedger, partition int) *heldReflectionInput {
	return &heldReflectionInput{
		ledger:    ledger,
		partition: partition,
		entered:   make(chan event.Event, 8),
		release:   make(chan struct{}),
	}
}

// InjectReflection commits the input, reports it and waits for the release of
// the turn, then reports the consumed outcome of that turn.
func (i *heldReflectionInput) InjectReflection(ctx context.Context, evt event.Event) (meditation.ReflectionReceipt, error) {
	i.mu.Lock()
	i.seq++
	key := event.EventID(fmt.Sprintf("held-reflection-%d", i.seq))
	i.mu.Unlock()
	evt.Key = key
	i.ledger.record(evt, i.partition)
	i.mu.Lock()
	i.injected = append(i.injected, evt)
	i.mu.Unlock()
	select {
	case i.entered <- evt:
	case <-ctx.Done():
		return meditation.ReflectionReceipt{}, ctx.Err()
	}
	select {
	case <-i.release:
	case <-ctx.Done():
		return meditation.ReflectionReceipt{}, ctx.Err()
	}
	return meditation.ReflectionReceipt{Key: key, Outcome: meditation.ReflectionConsumed, At: evt.Timestamp}, nil
}

// injectedCount reports how many reflection inputs were handed to the face.
func (i *heldReflectionInput) injectedCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.injected)
}

var _ meditation.ReflectionInput = (*heldReflectionInput)(nil)
