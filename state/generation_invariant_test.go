package state

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// TestGenerationBookkeepingInvariants pins the generation bookkeeping shared by the binding holder and the sole publication entry.
// - every hold and every actual publication moves the number by exactly one, and a byte-identical candidate moves it not at all
// - a handle keeps the generation it borrowed after a newer one is held, and its hold ends exactly once
// - a released generation is refused by name through its own reference, never served the generation in effect
// - a candidate whose recorded baseline was advanced is discarded, and the effective generation stays where it was
// - a publication record that cannot be committed leaves the effective binding untouched
// - a superseded generation's resources are freed exactly once, and only after its last hold ends
// - a rollback re-issues the previous full configuration as one more generation
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestGenerationBookkeepingInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("each hold moves the number by one and a borrowed handle keeps its generation", func(t *testing.T) {
		bindings := NewBindings()
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", "")
		if err := bindings.Hold(first); err != nil {
			t.Fatalf("hold the first plan: %v", err)
		}
		early, err := bindings.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow the effective generation: %v", err)
		}
		second := invariantGenerationPlan(t, "agent-a", "model-b", "fp-b", "")
		if err := bindings.Hold(second); err != nil {
			t.Fatalf("hold the second plan: %v", err)
		}
		late, err := bindings.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow the newer generation: %v", err)
		}
		if got := late.Ref.Generation; got != early.Ref.Generation+1 {
			t.Fatalf("generation after one hold = %d, want exactly one past %d", got, early.Ref.Generation)
		}
		if early.Binding.Model != first.Agents["agent-a"].Model {
			t.Fatalf("the earlier handle's binding = %v, want the generation it took", early.Binding.Model)
		}
		if late.Binding.Model != second.Agents["agent-a"].Model {
			t.Fatalf("the later handle's binding = %v, want the generation in effect when it started", late.Binding.Model)
		}
		if got := bindings.Inflight(); got != 2 {
			t.Fatalf("in-flight holds = %d, want both borrowed generations observable", got)
		}
		for _, handle := range []*BindingHandle{early, late} {
			if err := handle.Release(ctx); err != nil {
				t.Fatalf("release a borrowed handle: %v", err)
			}
		}
		if got := bindings.Inflight(); got != 0 {
			t.Fatalf("in-flight holds after release = %d, want none", got)
		}
	})

	t.Run("a released generation is refused through its reference instead of being lent again", func(t *testing.T) {
		bindings := NewBindings()
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", "")
		if err := bindings.Hold(first); err != nil {
			t.Fatalf("hold the first plan: %v", err)
		}
		handle, err := bindings.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow the effective generation: %v", err)
		}
		ref := handle.Ref
		second := invariantGenerationPlan(t, "agent-a", "model-b", "fp-b", "")
		if err := bindings.Hold(second); err != nil {
			t.Fatalf("hold the second plan: %v", err)
		}
		derived, err := bindings.BorrowDerived(ctx, ref)
		if err != nil {
			t.Fatalf("derive a borrow of the initiating generation: %v", err)
		}
		if derived.Binding.Model != first.Agents["agent-a"].Model {
			t.Fatalf("derived borrow = %v, want the generation its initiating reference names", derived.Binding.Model)
		}
		if err := derived.Release(ctx); err != nil {
			t.Fatalf("release the derived borrow: %v", err)
		}
		if err := handle.Release(ctx); err != nil {
			t.Fatalf("release the initiating handle: %v", err)
		}
		if err := handle.Release(ctx); !errors.Is(err, ErrHandleReleased) {
			t.Fatalf("second release = %v, want %v", err, ErrHandleReleased)
		}
		if _, err := bindings.BorrowDerived(ctx, ref); !errors.Is(err, ErrBindingGenerationReleased) {
			t.Fatalf("derived borrow through a released generation = %v, want %v", err, ErrBindingGenerationReleased)
		}
		if !errors.Is(ErrBindingGenerationReleased, ErrBindingRefUnknown) {
			t.Fatalf("the released-generation refusal is not a named case of %v", ErrBindingRefUnknown)
		}
		foreign := BindingRef{AgentID: "agent-a", InstanceID: "another-instance", Generation: ref.Generation}
		if _, err := bindings.BorrowDerived(ctx, foreign); !errors.Is(err, ErrBindingRefUnknown) {
			t.Fatalf("derived borrow of a foreign reference = %v, want %v", err, ErrBindingRefUnknown)
		}
		if _, err := bindings.BorrowDerived(ctx, BindingRef{AgentID: "agent-a"}); !errors.Is(err, ErrBindingRefUnknown) {
			t.Fatalf("derived borrow of the zero reference = %v, want %v", err, ErrBindingRefUnknown)
		}
		unnumbered := BindingRef{AgentID: "agent-a", InstanceID: ref.InstanceID, Generation: ref.Generation + 42}
		if _, err := bindings.BorrowDerived(ctx, unnumbered); !errors.Is(err, ErrBindingRefUnknown) {
			t.Fatalf("derived borrow of an unnumbered generation = %v, want %v", err, ErrBindingRefUnknown)
		}
		if got := bindings.Inflight(); got != 0 {
			t.Fatalf("in-flight holds after the refusals = %d, want none", got)
		}
	})

	t.Run("an identical candidate assigns no generation and frees only its own resources", func(t *testing.T) {
		bindings, publisher, records := invariantPublisher(t)
		releases := &invariantReleases{}
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", publisher.Baseline())
		applied, err := publisher.Publish(ctx, Candidate{Plan: first, Resources: releases.lease(t, "a")})
		if err != nil {
			t.Fatalf("publish the first candidate: %v", err)
		}
		if applied.Result != PublishApplied || applied.Publication.Generation != 1 {
			t.Fatalf("first publication = (%v, generation %d), want applied as generation 1", applied.Result, applied.Publication.Generation)
		}
		if got := records.count(); got != 1 {
			t.Fatalf("records after one publication = %d, want one", got)
		}
		identical := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", publisher.Baseline())
		again, err := publisher.Publish(ctx, Candidate{Plan: identical, Resources: releases.lease(t, "b")})
		if err != nil {
			t.Fatalf("publish a byte-identical candidate: %v", err)
		}
		if again.Result != PublishUnchanged || again.Publication.Generation != 1 {
			t.Fatalf("byte-identical publication = (%v, generation %d), want unchanged at generation 1", again.Result, again.Publication.Generation)
		}
		if got := records.count(); got != 1 {
			t.Fatalf("records after a byte-identical publication = %d, want still one", got)
		}
		if got := releases.count(); got != 1 {
			t.Fatalf("released candidates = %d, want only the byte-identical candidate's own resources", got)
		}
		if bindings == nil {
			t.Fatal("the publisher carries no binding holder")
		}
	})

	t.Run("a candidate whose baseline was advanced is discarded and moves no generation", func(t *testing.T) {
		_, publisher, records := invariantPublisher(t)
		releases := &invariantReleases{}
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: first}); err != nil {
			t.Fatalf("publish the first candidate: %v", err)
		}
		staleBaseline := publisher.Baseline()
		second := invariantGenerationPlan(t, "agent-a", "model-b", "fp-b", staleBaseline)
		if _, err := publisher.Publish(ctx, Candidate{Plan: second}); err != nil {
			t.Fatalf("publish the second candidate: %v", err)
		}
		stale := invariantGenerationPlan(t, "agent-a", "model-c", "fp-c", staleBaseline)
		outcome, err := publisher.Publish(ctx, Candidate{Plan: stale, Resources: releases.lease(t, "stale")})
		if !errors.Is(err, ErrBaselineAdvanced) {
			t.Fatalf("stale candidate = %v, want %v", err, ErrBaselineAdvanced)
		}
		if outcome.Publication.Generation != 0 || outcome.Result != PublishUnchanged {
			t.Fatalf("stale publication outcome = (%v, generation %d), want the zero outcome", outcome.Result, outcome.Publication.Generation)
		}
		if current := publisher.Current(); current.Generation != 2 || current.Fingerprint != "fp-b" {
			t.Fatalf("effective publication after the stale candidate = (%d, %q), want the newer publication untouched", current.Generation, current.Fingerprint)
		}
		if got := records.count(); got != 2 {
			t.Fatalf("records after the stale candidate = %d, want the two stored publications", got)
		}
		if got := releases.count(); got != 1 {
			t.Fatalf("released candidates = %d, want the stale candidate freed once", got)
		}
	})

	t.Run("a record the recorder refused leaves the effective binding untouched", func(t *testing.T) {
		_, publisher, records := invariantPublisher(t)
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: first}); err != nil {
			t.Fatalf("publish the first candidate: %v", err)
		}
		records.refuse(true)
		next := invariantGenerationPlan(t, "agent-a", "model-b", "fp-b", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: next}); err == nil {
			t.Fatal("publication beside a refused record = nil, want the named refusal")
		}
		records.refuse(false)
		if got := publisher.Current().Generation; got != 1 {
			t.Fatalf("effective generation after a refused record = %d, want 1", got)
		}
		if got := publisher.Current().Fingerprint; got != "fp-a" {
			t.Fatalf("effective fingerprint after a refused record = %q, want the published one", got)
		}
		if got := records.count(); got != 1 {
			t.Fatalf("stored records = %d, want only the one that was committed", got)
		}
	})

	t.Run("a superseded generation's resources are freed once after its last hold ends", func(t *testing.T) {
		_, publisher, _ := invariantPublisher(t)
		releases := &invariantReleases{}
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: first, Resources: releases.lease(t, "a")}); err != nil {
			t.Fatalf("publish the first candidate: %v", err)
		}
		held, err := publisher.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow the generation in effect: %v", err)
		}
		second := invariantGenerationPlan(t, "agent-a", "model-b", "fp-b", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: second}); err != nil {
			t.Fatalf("publish the second candidate: %v", err)
		}
		if got := releases.count(); got != 0 {
			t.Fatalf("released generations = %d, want none while a request still holds the generation", got)
		}
		if err := held.Release(ctx); err != nil {
			t.Fatalf("release the last hold of the superseded generation: %v", err)
		}
		if got := releases.count(); got != 1 {
			t.Fatalf("released generations after the last hold = %d, want one", got)
		}
		third := invariantGenerationPlan(t, "agent-a", "model-c", "fp-c", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: third}); err != nil {
			t.Fatalf("publish the third candidate: %v", err)
		}
		if got := releases.count(); got != 1 {
			t.Fatalf("released generations after a later publication = %d, want the release to happen once", got)
		}
	})

	t.Run("a rollback re-issues the previous full configuration as one more generation", func(t *testing.T) {
		_, publisher, records := invariantPublisher(t)
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: first}); err != nil {
			t.Fatalf("publish the first candidate: %v", err)
		}
		second := invariantGenerationPlan(t, "agent-a", "model-b", "fp-b", publisher.Baseline())
		if _, err := publisher.Publish(ctx, Candidate{Plan: second}); err != nil {
			t.Fatalf("publish the second candidate: %v", err)
		}
		source, ok := publisher.RollbackConfig()
		if !ok || source.Fingerprint != "fp-a" {
			t.Fatalf("rollback source = (%q, %v), want the superseded configuration", source.Fingerprint, ok)
		}
		rebuilt := invariantGenerationPlan(t, "agent-a", "model-a", source.Fingerprint, publisher.Baseline())
		outcome, err := publisher.Rollback(ctx, Candidate{Plan: rebuilt})
		if err != nil {
			t.Fatalf("roll the previous configuration back: %v", err)
		}
		if outcome.Result != PublishRolledBack {
			t.Fatalf("rollback result = %v, want %v", outcome.Result, PublishRolledBack)
		}
		if got := outcome.Publication.Generation; got != 3 {
			t.Fatalf("generation after a rollback = %d, want one more than the superseded publication", got)
		}
		stored := records.last(t)
		if stored.Generation != 3 || stored.Result != PublishRolledBack || stored.RollbackSource != 1 {
			t.Fatalf("rollback record = %+v, want generation 3 rolled back from generation 1", stored)
		}
		other := invariantGenerationPlan(t, "agent-a", "model-c", "fp-c", publisher.Baseline())
		if _, err := publisher.Rollback(ctx, Candidate{Plan: other}); !errors.Is(err, ErrRollbackSourceMismatch) {
			t.Fatalf("rollback of another content = %v, want %v", err, ErrRollbackSourceMismatch)
		}
	})

	t.Run("a rollback before any supersession is refused by name", func(t *testing.T) {
		_, publisher, _ := invariantPublisher(t)
		releases := &invariantReleases{}
		first := invariantGenerationPlan(t, "agent-a", "model-a", "fp-a", publisher.Baseline())
		_, err := publisher.Rollback(ctx, Candidate{Plan: first, Resources: releases.lease(t, "a")})
		if !errors.Is(err, ErrRollbackUnavailable) {
			t.Fatalf("rollback beside no superseded configuration = %v, want %v", err, ErrRollbackUnavailable)
		}
		if got := releases.count(); got != 1 {
			t.Fatalf("released candidates = %d, want the refused candidate freed once", got)
		}
	})
}

// invariantGenerationPlan builds one candidate plan of a single declared agent.
func invariantGenerationPlan(t *testing.T, agent event.AgentID, modelName, fingerprint, baseline string) StatePlan {
	t.Helper()
	declaration := &config.Declaration{Entry: string(agent), Agents: []config.AgentSpec{{ID: string(agent)}}}
	plan, err := NewStatePlan(
		OrgPlan{Declaration: declaration, Baseline: baseline, Fingerprint: fingerprint},
		map[event.AgentID]AgentBinding{agent: {Model: &invariantGenerationModel{name: modelName}}},
		nil,
	)
	if err != nil {
		t.Fatalf("build the candidate plan of %s: %v", agent, err)
	}
	return plan
}

// invariantGenerationModel is the model descriptor of a candidate binding.
type invariantGenerationModel struct {
	name string
}

// GenerateContent answers one empty stream; the publication path never runs a request.
func (m *invariantGenerationModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true}
	close(ch)
	return ch, nil
}

// Info reports the model descriptor.
func (m *invariantGenerationModel) Info() model.Info { return model.Info{Name: m.name} }

// invariantGovernance answers one fixed passing disposition per required check.
type invariantGovernance struct {
	answers map[CheckName]CheckAnswer
}

// Check reports the fixed dispositions.
func (g invariantGovernance) Check(context.Context, StatePlan) (map[CheckName]CheckAnswer, error) {
	return g.answers, nil
}

// invariantRecords stores publication records and can refuse the next one.
type invariantRecords struct {
	mu      sync.Mutex
	records []PublicationRecord
	refused bool
}

// CommitPublication stores one record unless the recorder was made to refuse.
func (r *invariantRecords) CommitPublication(_ context.Context, record PublicationRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refused {
		return errors.New("state: the publication recorder refused the record")
	}
	r.records = append(r.records, record)
	return nil
}

// refuse makes the recorder refuse or accept the next record.
func (r *invariantRecords) refuse(refuse bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refused = refuse
}

// count reports how many records were stored.
func (r *invariantRecords) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// last reports the record stored last.
func (r *invariantRecords) last(t *testing.T) PublicationRecord {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.records) == 0 {
		t.Fatal("no publication record was stored")
	}
	return r.records[len(r.records)-1]
}

// invariantReleases counts the candidate resources one publication path released.
type invariantReleases struct {
	mu       sync.Mutex
	released int
}

// lease registers one candidate-exclusive resource beside its release.
func (r *invariantReleases) lease(t *testing.T, key string) *Leases {
	t.Helper()
	leases := NewLeases()
	leases.Register("candidate", key, func(context.Context) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.released++
		return nil
	})
	return leases
}

// count reports how many resources were released.
func (r *invariantReleases) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.released
}

// invariantPublisher returns a publication entry over one fresh binding holder.
func invariantPublisher(t *testing.T) (*Bindings, *Publisher, *invariantRecords) {
	t.Helper()
	records := &invariantRecords{}
	governance := invariantGovernance{answers: map[CheckName]CheckAnswer{
		"evidence": {Name: "evidence", Verdict: CheckPass, Reason: "the check passed", Scope: "candidate"},
	}}
	bindings := NewBindings()
	publisher := NewPublisher(bindings, PublishContract{Required: []CheckName{"evidence"}}, governance, records)
	return bindings, publisher, records
}
