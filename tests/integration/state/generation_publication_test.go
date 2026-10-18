package state

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"

	orgstate "github.com/SpellingDragon/tagent/state"
)

// policyStand is the deterministic stand-in of the real governance policy the
// mechanism checks inject. Its answers are fixed inputs of the mechanism and
// carry no quality conclusion: the formal quality assessment lives in the
// structural evaluation gate.
type policyStand struct {
	answers map[orgstate.CheckName]orgstate.CheckAnswer
	err     error
}

// Check implements Governance with the fixed answers the stand-in carries.
func (s *policyStand) Check(ctx context.Context, plan orgstate.StatePlan) (map[orgstate.CheckName]orgstate.CheckAnswer, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.answers, nil
}

// recordSink records the publication records committed through it and can be
// set to refuse a commit.
type recordSink struct {
	records []orgstate.PublicationRecord
	err     error
}

// CommitPublication implements PublicationRecorder beside the records it keeps.
func (s *recordSink) CommitPublication(ctx context.Context, record orgstate.PublicationRecord) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, record)
	return nil
}

// releaseLog records the release of each candidate resource by name, in the
// order the releases run.
type releaseLog struct {
	names []string
}

// track returns the release of one named resource that reports itself here.
func (l *releaseLog) track(name string) func(context.Context) error {
	return func(context.Context) error {
		l.names = append(l.names, name)
		return nil
	}
}

// publicationCandidate prepares the candidate of one declared agent beside the
// candidate-exclusive resources the caller registered.
func publicationCandidate(t *testing.T, baseline, fingerprint string, m model.Model, leases *orgstate.Leases) orgstate.Candidate {
	t.Helper()
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}}
	plan := orgstate.OrgPlan{Declaration: decl, Baseline: baseline, Fingerprint: fingerprint, NextPrompt: "prompt-a"}
	built, err := orgstate.NewStatePlan(plan, map[event.AgentID]orgstate.AgentBinding{"agent-a": {Model: m}}, leases.Refs())
	if err != nil {
		t.Fatalf("instantiate the candidate plan: %v", err)
	}
	return orgstate.Candidate{Plan: built, Resources: leases}
}

// passing reports the passing disposition one named check carries.
func passing(name orgstate.CheckName) orgstate.CheckAnswer {
	return orgstate.CheckAnswer{Name: name, Verdict: orgstate.CheckPass, Reason: "evidence collected", Scope: "org"}
}

// TestGenerationPublication checks how the sole publication entry assigns generations and retires superseded resources:
// - a required check left undispositioned refuses the publication beside the named missing evidence
// - an exemption is recorded with its reason and scope and is no passing disposition
// - a candidate whose baseline advanced is discarded, and byte-identical content publishes no new generation and builds no new resource
// - a publication keeps the resources of the generation a request still holds and retires them once the last hold is gone
// - a rollback rebuilds the previous full configuration as one more generation, and a refused candidate frees only the resources it acquired
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestGenerationPublication(t *testing.T) {
	t.Run("a missing required check refuses the publication", func(t *testing.T) {
		ctx := context.Background()
		contract := orgstate.PublishContract{Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"}}

		sink := &recordSink{}
		pub := orgstate.NewPublisher(orgstate.NewBindings(), contract, &policyStand{}, sink)
		log := &releaseLog{}
		leases := orgstate.NewLeases()
		leases.Register("model", "model-a", log.track("model-a"))
		cand := publicationCandidate(t, pub.Baseline(), "fp-1", &stubModel{name: "gen-1"}, leases)
		if _, err := pub.Publish(ctx, cand); !errors.Is(err, orgstate.ErrRequiredCheckMissing) {
			t.Fatalf("publish beside undispositioned checks = %v, want %v beside a refusal that names the missing evidence", err, orgstate.ErrRequiredCheckMissing)
		}
		if got := pub.Current(); got.Generation != 0 {
			t.Fatalf("effective publication = %+v, want the state kept as it was", got)
		}
		if len(sink.records) != 0 {
			t.Fatalf("committed records = %d, want no record beside a refusal that reports no passing disposition", len(sink.records))
		}
		if len(log.names) != 1 || log.names[0] != "model-a" {
			t.Fatalf("released = %v, want the candidate-exclusive resources freed beside the refusal", log.names)
		}

		empty := &recordSink{}
		pubEmpty := orgstate.NewPublisher(orgstate.NewBindings(), orgstate.PublishContract{}, &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"),
		}}, empty)
		logEmpty := &releaseLog{}
		leasesEmpty := orgstate.NewLeases()
		leasesEmpty.Register("model", "model-b", logEmpty.track("model-b"))
		candEmpty := publicationCandidate(t, pubEmpty.Baseline(), "fp-2", &stubModel{name: "gen-2"}, leasesEmpty)
		if _, err := pubEmpty.Publish(ctx, candEmpty); !errors.Is(err, orgstate.ErrPublicationContractEmpty) {
			t.Fatalf("publish beside a contract that requires no check = %v, want %v beside a refusal that names the missing evidence", err, orgstate.ErrPublicationContractEmpty)
		}
		if got := pubEmpty.Current(); got.Generation != 0 {
			t.Fatalf("effective publication = %+v, want nothing published beside an empty contract", got)
		}

		partSink := &recordSink{}
		pubPart := orgstate.NewPublisher(orgstate.NewBindings(), contract, &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"),
		}}, partSink)
		leasesPart := orgstate.NewLeases()
		leasesPart.Register("model", "model-c", func(context.Context) error { return nil })
		candPart := publicationCandidate(t, pubPart.Baseline(), "fp-3", &stubModel{name: "gen-3"}, leasesPart)
		if _, err := pubPart.Publish(ctx, candPart); !errors.Is(err, orgstate.ErrRequiredCheckMissing) {
			t.Fatalf("publish beside one required check left out = %v, want %v beside a refusal that names the check", err, orgstate.ErrRequiredCheckMissing)
		}

		mute := &recordSink{}
		pubMute := orgstate.NewPublisher(orgstate.NewBindings(), contract, &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval":       {},
			"benchmark":  passing("benchmark"),
			"apply-gate": passing("apply-gate"),
		}}, mute)
		leasesMute := orgstate.NewLeases()
		leasesMute.Register("model", "model-d", func(context.Context) error { return nil })
		candMute := publicationCandidate(t, pubMute.Baseline(), "fp-4", &stubModel{name: "gen-4"}, leasesMute)
		if _, err := pubMute.Publish(ctx, candMute); !errors.Is(err, orgstate.ErrRequiredCheckMissing) {
			t.Fatalf("publish beside a check that answers no disposition = %v, want %v beside a refusal that names the check", err, orgstate.ErrRequiredCheckMissing)
		}

		naked := &recordSink{}
		pubNaked := orgstate.NewPublisher(orgstate.NewBindings(), contract, &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval":       {Verdict: orgstate.CheckExempt, Reason: "not applicable to this change"},
			"benchmark":  passing("benchmark"),
			"apply-gate": passing("apply-gate"),
		}}, naked)
		leasesNaked := orgstate.NewLeases()
		leasesNaked.Register("model", "model-e", func(context.Context) error { return nil })
		candNaked := publicationCandidate(t, pubNaked.Baseline(), "fp-5", &stubModel{name: "gen-5"}, leasesNaked)
		if _, err := pubNaked.Publish(ctx, candNaked); !errors.Is(err, orgstate.ErrExemptionUnrecorded) {
			t.Fatalf("publish beside an exemption that records no scope = %v, want %v beside a refusal that names the exemption", err, orgstate.ErrExemptionUnrecorded)
		}

		sinkExempt := &recordSink{}
		pubExempt := orgstate.NewPublisher(orgstate.NewBindings(), contract, &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval":       passing("eval"),
			"benchmark":  {Name: "benchmark", Verdict: orgstate.CheckExempt, Reason: "no performance-relevant field changed", Scope: "benchmark"},
			"apply-gate": passing("apply-gate"),
		}}, sinkExempt)
		leasesExempt := orgstate.NewLeases()
		leasesExempt.Register("model", "model-f", func(context.Context) error { return nil })
		candExempt := publicationCandidate(t, pubExempt.Baseline(), "fp-6", &stubModel{name: "gen-6"}, leasesExempt)
		out, err := pubExempt.Publish(ctx, candExempt)
		if err != nil {
			t.Fatalf("publish beside an explicit exemption = %v, want the publication to rest on the dispositions it recorded", err)
		}
		if out.Result != orgstate.PublishApplied || out.Publication.Generation != 1 {
			t.Fatalf("publication = (%+v, %v), want one applied generation", out.Publication, out.Result)
		}
		if len(sinkExempt.records) != 1 {
			t.Fatalf("committed records = %d, want exactly one record for the publication", len(sinkExempt.records))
		}
		record := sinkExempt.records[0]
		if record.Generation != 1 || record.Fingerprint != "fp-6" || record.Result != orgstate.PublishApplied || record.RollbackSource != 0 {
			t.Fatalf("committed record = %+v, want the generation, the fingerprint, the result and the rollback source it published", record)
		}
		if len(record.Checks) != 3 {
			t.Fatalf("recorded checks = %+v, want one disposition per check", record.Checks)
		}
		var exempt *orgstate.CheckAnswer
		for i := range record.Checks {
			if record.Checks[i].Name == "benchmark" {
				exempt = &record.Checks[i]
			}
		}
		if exempt == nil {
			t.Fatalf("recorded checks = %+v, want the exemption named beside the others", record.Checks)
		}
		if exempt.Verdict != orgstate.CheckExempt || exempt.Reason != "no performance-relevant field changed" || exempt.Scope != "benchmark" {
			t.Fatalf("recorded exemption = %+v, want the reason and the scope recorded as no passing disposition", *exempt)
		}
	})

	t.Run("a candidate whose baseline advanced is discarded", func(t *testing.T) {
		ctx := context.Background()
		contract := orgstate.PublishContract{Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"}}
		policy := &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"), "benchmark": passing("benchmark"), "apply-gate": passing("apply-gate"),
		}}
		sink := &recordSink{}
		pub := orgstate.NewPublisher(orgstate.NewBindings(), contract, policy, sink)

		logFirst := &releaseLog{}
		leasesFirst := orgstate.NewLeases()
		leasesFirst.Register("model", "model-a", logFirst.track("model-a"))
		first := publicationCandidate(t, pub.Baseline(), "fp-1", &stubModel{name: "gen-1"}, leasesFirst)
		outFirst, err := pub.Publish(ctx, first)
		if err != nil || outFirst.Publication.Generation != 1 {
			t.Fatalf("publish the startup generation = (%+v, %v), want the first generation applied", outFirst, err)
		}

		logStale := &releaseLog{}
		leasesStale := orgstate.NewLeases()
		leasesStale.Register("model", "model-stale", logStale.track("model-stale"))
		stale := publicationCandidate(t, pub.Baseline(), "fp-stale", &stubModel{name: "stale"}, leasesStale)

		logSecond := &releaseLog{}
		leasesSecond := orgstate.NewLeases()
		leasesSecond.Register("model", "model-b", logSecond.track("model-b"))
		second := publicationCandidate(t, pub.Baseline(), "fp-2", &stubModel{name: "gen-2"}, leasesSecond)
		outSecond, err := pub.Publish(ctx, second)
		if err != nil || outSecond.Publication.Generation != 2 {
			t.Fatalf("publish the second generation = (%+v, %v), want one more generation applied", outSecond, err)
		}
		if len(logFirst.names) != 1 || logFirst.names[0] != "model-a" {
			t.Fatalf("released = %v, want the superseded generation retired once it held no request", logFirst.names)
		}

		if _, err := pub.Publish(ctx, stale); !errors.Is(err, orgstate.ErrBaselineAdvanced) {
			t.Fatalf("publish a stale candidate = %v, want %v beside the discard that names the advanced baseline", err, orgstate.ErrBaselineAdvanced)
		}
		if got := pub.Current(); got.Generation != 2 || got.Fingerprint != "fp-2" {
			t.Fatalf("effective publication = %+v, want the newer publication kept and never overwritten", got)
		}
		if len(sink.records) != 2 {
			t.Fatalf("committed records = %d, want no record for the discarded candidate", len(sink.records))
		}
		if len(logStale.names) != 1 || logStale.names[0] != "model-stale" {
			t.Fatalf("released = %v, want the discarded candidate's own resources freed", logStale.names)
		}
		if len(logSecond.names) != 0 || len(logFirst.names) != 1 {
			t.Fatalf("released beside the discard = (%v, %v), want the resources in service left as they were", logSecond.names, logFirst.names)
		}
	})

	t.Run("reloading identical content publishes no new generation", func(t *testing.T) {
		ctx := context.Background()
		contract := orgstate.PublishContract{Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"}}
		policy := &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"), "benchmark": passing("benchmark"), "apply-gate": passing("apply-gate"),
		}}
		sink := &recordSink{}
		pub := orgstate.NewPublisher(orgstate.NewBindings(), contract, policy, sink)

		live := &stubModel{name: "gen-1"}
		logFirst := &releaseLog{}
		leasesFirst := orgstate.NewLeases()
		leasesFirst.Register("model", "model-a", logFirst.track("model-a"))
		first := publicationCandidate(t, pub.Baseline(), "fp-1", live, leasesFirst)
		outFirst, err := pub.Publish(ctx, first)
		if err != nil || outFirst.Publication.Generation != 1 {
			t.Fatalf("publish the first generation = (%+v, %v), want the generation applied", outFirst, err)
		}

		logReload := &releaseLog{}
		leasesReload := orgstate.NewLeases()
		leasesReload.Register("model", "model-a", logReload.track("model-a"))
		reload := publicationCandidate(t, pub.Baseline(), "fp-1", &stubModel{name: "gen-1-rebuilt"}, leasesReload)
		outReload, err := pub.Publish(ctx, reload)
		if err != nil {
			t.Fatalf("reload byte-identical content = %v, want no publication beside it", err)
		}
		if outReload.Result != orgstate.PublishUnchanged || outReload.Publication.Generation != 1 {
			t.Fatalf("reload outcome = (%+v, %v), want the generation in effect and no new one", outReload.Publication, outReload.Result)
		}
		if got := pub.Current(); got.Generation != 1 || got.Fingerprint != "fp-1" {
			t.Fatalf("effective publication = %+v, want the first generation kept", got)
		}
		if len(sink.records) != 1 {
			t.Fatalf("committed records = %d, want no record beside a reload that published nothing", len(sink.records))
		}
		if len(logReload.names) != 1 || logReload.names[0] != "model-a" {
			t.Fatalf("released = %v, want the rebuilt candidate to leave and build no new resource", logReload.names)
		}
		if len(logFirst.names) != 0 {
			t.Fatalf("released = %v, want the resources in service kept", logFirst.names)
		}
		handle, err := pub.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as the request begins executing: %v", err)
		}
		if handle.Binding.Model != live {
			t.Fatalf("borrowed model = %v, want the binding in service beside the reload", handle.Binding.Model)
		}
		if handle.Ref.Generation != 1 {
			t.Fatalf("borrowed generation = %d, want the generation the publication assigned", handle.Ref.Generation)
		}
	})

	t.Run("a publication keeps the in-flight old resources alive", func(t *testing.T) {
		ctx := context.Background()
		contract := orgstate.PublishContract{Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"}}
		policy := &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"), "benchmark": passing("benchmark"), "apply-gate": passing("apply-gate"),
		}}
		sink := &recordSink{}
		pub := orgstate.NewPublisher(orgstate.NewBindings(), contract, policy, sink)

		old := &stubModel{name: "gen-1"}
		logOld := &releaseLog{}
		leasesOld := orgstate.NewLeases()
		leasesOld.Register("model", "model-a", logOld.track("model-a"))
		outFirst, err := pub.Publish(ctx, publicationCandidate(t, pub.Baseline(), "fp-1", old, leasesOld))
		if err != nil || outFirst.Publication.Generation != 1 {
			t.Fatalf("publish the first generation = (%+v, %v), want the generation applied", outFirst, err)
		}

		handle, err := pub.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as the request begins executing: %v", err)
		}
		if handle.Ref.Generation != outFirst.Publication.Generation {
			t.Fatalf("borrowed generation = %d, want the generation the publication assigned", handle.Ref.Generation)
		}
		if handle.Binding.Model != old {
			t.Fatalf("borrowed model = %v, want the generation the request took", handle.Binding.Model)
		}

		fresh := &stubModel{name: "gen-2"}
		logFresh := &releaseLog{}
		leasesFresh := orgstate.NewLeases()
		leasesFresh.Register("model", "model-b", logFresh.track("model-b"))
		outSecond, err := pub.Publish(ctx, publicationCandidate(t, pub.Baseline(), "fp-2", fresh, leasesFresh))
		if err != nil || outSecond.Publication.Generation != 2 {
			t.Fatalf("publish the second generation = (%+v, %v), want one more generation applied", outSecond, err)
		}
		if len(logOld.names) != 0 {
			t.Fatalf("released = %v, want the resources of the generation still in flight kept in service", logOld.names)
		}
		if handle.Binding.Model != old {
			t.Fatalf("in-flight model = %v, want the generation the request took kept until it finishes", handle.Binding.Model)
		}

		queued, err := pub.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as the queued request begins executing: %v", err)
		}
		if queued.Binding.Model != fresh || queued.Ref.Generation != 2 {
			t.Fatalf("queued borrow = (%v, %d), want the generation effective at execution start", queued.Binding.Model, queued.Ref.Generation)
		}

		if err := handle.Release(ctx); err != nil {
			t.Fatalf("end the use of the borrowed generation: %v", err)
		}
		if len(logOld.names) != 1 || logOld.names[0] != "model-a" {
			t.Fatalf("released = %v, want the old generation retired once its last hold is gone", logOld.names)
		}
		if err := handle.Release(ctx); !errors.Is(err, orgstate.ErrHandleReleased) {
			t.Fatalf("second release of one handle = %v, want %v beside a use that ends exactly once", err, orgstate.ErrHandleReleased)
		}
		if len(logOld.names) != 1 {
			t.Fatalf("released = %v, want the retired generation freed exactly once", logOld.names)
		}

		if err := queued.Release(ctx); err != nil {
			t.Fatalf("end the use of the borrowed generation: %v", err)
		}
		if len(logFresh.names) != 0 {
			t.Fatalf("released = %v, want the resources of the generation in service kept", logFresh.names)
		}
	})

	t.Run("a rollback publishes a new generation", func(t *testing.T) {
		ctx := context.Background()
		contract := orgstate.PublishContract{Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"}}
		policy := &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"), "benchmark": passing("benchmark"), "apply-gate": passing("apply-gate"),
		}}
		sink := &recordSink{}
		pub := orgstate.NewPublisher(orgstate.NewBindings(), contract, policy, sink)

		logPre := &releaseLog{}
		leasesPre := orgstate.NewLeases()
		leasesPre.Register("model", "model-pre", logPre.track("model-pre"))
		if _, err := pub.Rollback(ctx, publicationCandidate(t, pub.Baseline(), "fp-pre", &stubModel{name: "pre"}, leasesPre)); !errors.Is(err, orgstate.ErrRollbackUnavailable) {
			t.Fatalf("rollback before any superseded publication = %v, want %v beside a refusal that names the missing source", err, orgstate.ErrRollbackUnavailable)
		}
		if len(logPre.names) != 1 {
			t.Fatalf("released = %v, want the refused rollback candidate freed", logPre.names)
		}

		first := &stubModel{name: "gen-1"}
		outFirst, err := pub.Publish(ctx, publicationCandidate(t, pub.Baseline(), "fp-a", first, resourceLeases(t, "model-a")))
		if err != nil || outFirst.Publication.Generation != 1 {
			t.Fatalf("publish the startup generation = (%+v, %v), want the generation applied", outFirst, err)
		}
		outSecond, err := pub.Publish(ctx, publicationCandidate(t, pub.Baseline(), "fp-b", &stubModel{name: "gen-2"}, resourceLeases(t, "model-b")))
		if err != nil || outSecond.Publication.Generation != 2 {
			t.Fatalf("publish the second generation = (%+v, %v), want one more generation applied", outSecond, err)
		}

		source, ok := pub.RollbackConfig()
		if !ok || source.Fingerprint != "fp-a" {
			t.Fatalf("rollback source = (%+v, %v), want the previous full configuration kept", source, ok)
		}

		rebuilt := &stubModel{name: "gen-3-rebuilt"}
		outBack, err := pub.Rollback(ctx, publicationCandidate(t, pub.Baseline(), source.Fingerprint, rebuilt, resourceLeases(t, "model-c")))
		if err != nil {
			t.Fatalf("roll the previous full configuration back: %v", err)
		}
		if outBack.Result != orgstate.PublishRolledBack || outBack.Publication.Generation != 3 {
			t.Fatalf("rollback outcome = (%+v, %v), want one more generation applied from the previous configuration", outBack.Publication, outBack.Result)
		}
		last := sink.records[len(sink.records)-1]
		if last.Generation != 3 || last.Result != orgstate.PublishRolledBack || last.RollbackSource != 1 {
			t.Fatalf("committed record = %+v, want the new generation beside the generation its content came back from", last)
		}
		handle, err := pub.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow after the rollback: %v", err)
		}
		if handle.Binding.Model != rebuilt || handle.Binding.Model == first {
			t.Fatalf("borrowed model = %v, want the freshly rebuilt binding and no execution object restored", handle.Binding.Model)
		}
		if handle.Ref.Generation != 3 {
			t.Fatalf("borrowed generation = %d, want the generation the rollback assigned", handle.Ref.Generation)
		}

		outAgain, err := pub.Rollback(ctx, publicationCandidate(t, pub.Baseline(), source.Fingerprint, &stubModel{name: "gen-4-rebuilt"}, resourceLeases(t, "model-d")))
		if err != nil || outAgain.Publication.Generation != 4 {
			t.Fatalf("roll back again = (%+v, %v), want one more generation beside the same rollback source", outAgain, outAgain.Publication.Generation)
		}
		if last = sink.records[len(sink.records)-1]; last.RollbackSource != 1 {
			t.Fatalf("committed record = %+v, want the rollback source kept across consecutive rollbacks", last)
		}

		logBad := &releaseLog{}
		leasesBad := orgstate.NewLeases()
		leasesBad.Register("model", "model-bad", logBad.track("model-bad"))
		if _, err := pub.Rollback(ctx, publicationCandidate(t, pub.Baseline(), "fp-x", &stubModel{name: "bad"}, leasesBad)); !errors.Is(err, orgstate.ErrRollbackSourceMismatch) {
			t.Fatalf("rollback beside other content = %v, want %v beside a refusal that names the source", err, orgstate.ErrRollbackSourceMismatch)
		}
		if len(logBad.names) != 1 {
			t.Fatalf("released = %v, want the refused rollback candidate freed", logBad.names)
		}
		if got := pub.Current(); got.Generation != 4 {
			t.Fatalf("effective publication = %+v, want the rolled back generation kept", got)
		}
	})

	t.Run("a refused candidate releases only its own resources", func(t *testing.T) {
		ctx := context.Background()
		contract := orgstate.PublishContract{Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"}}
		sink := &recordSink{}
		policy := &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"), "benchmark": passing("benchmark"), "apply-gate": passing("apply-gate"),
		}}
		pub := orgstate.NewPublisher(orgstate.NewBindings(), contract, policy, sink)

		logLive := &releaseLog{}
		leasesLive := orgstate.NewLeases()
		leasesLive.Register("model", "model-live", logLive.track("model-live"))
		live := &stubModel{name: "gen-1"}
		out, err := pub.Publish(ctx, publicationCandidate(t, pub.Baseline(), "fp-1", live, leasesLive))
		if err != nil || out.Publication.Generation != 1 {
			t.Fatalf("publish the generation in service = (%+v, %v), want the generation applied", out, err)
		}

		logCand := &releaseLog{}
		leasesCand := orgstate.NewLeases()
		leasesCand.Register("model", "model-a", logCand.track("model-a"))
		leasesCand.Register("toolset", "agent-a", logCand.track("toolset-agent-a"))
		leasesCand.Register("model", "model-b", logCand.track("model-b"))
		policy.answers = map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": {Name: "eval", Verdict: orgstate.CheckReject, Reason: "evidence contradicts the change", Scope: "org"},
		}
		refused := publicationCandidate(t, pub.Baseline(), "fp-2", &stubModel{name: "gen-2"}, leasesCand)
		if _, err := pub.Publish(ctx, refused); !errors.Is(err, orgstate.ErrCheckRefused) {
			t.Fatalf("publish beside a refusing check = %v, want %v beside the refusal that names the check", err, orgstate.ErrCheckRefused)
		}
		if len(logCand.names) != 3 || logCand.names[0] != "model-b" || logCand.names[1] != "toolset-agent-a" || logCand.names[2] != "model-a" {
			t.Fatalf("released = %v, want the candidate's own resources freed in the reverse of the construction order and once each", logCand.names)
		}
		if len(logLive.names) != 0 {
			t.Fatalf("released = %v, want the resources already in service left untouched", logLive.names)
		}
		if err := leasesCand.ReleaseAll(ctx); err != nil {
			t.Fatalf("second release pass = %v, want nothing left to free", err)
		}
		if len(logCand.names) != 3 {
			t.Fatalf("released = %v, want each resource freed exactly once across every release pass", logCand.names)
		}
		if got := pub.Current(); got.Generation != 1 || got.Fingerprint != "fp-1" {
			t.Fatalf("effective publication = %+v, want the state kept as it was beside the refusal", got)
		}
		if len(sink.records) != 1 {
			t.Fatalf("committed records = %d, want no record beside a refusal", len(sink.records))
		}
		handle, err := pub.Borrow(ctx, "agent-a")
		if err != nil || handle.Binding.Model != live {
			t.Fatalf("borrow beside a refused candidate = (%v, %v), want the binding in service kept", handle, err)
		}
	})

	t.Run("a record that fails to commit refuses the publication", func(t *testing.T) {
		ctx := context.Background()
		contract := orgstate.PublishContract{Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"}}
		policy := &policyStand{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": passing("eval"), "benchmark": passing("benchmark"), "apply-gate": passing("apply-gate"),
		}}
		sinkErr := errors.New("the recorder refused the commit")
		sink := &recordSink{err: sinkErr}
		pub := orgstate.NewPublisher(orgstate.NewBindings(), contract, policy, sink)

		log := &releaseLog{}
		leases := orgstate.NewLeases()
		leases.Register("model", "model-a", log.track("model-a"))
		if _, err := pub.Publish(ctx, publicationCandidate(t, pub.Baseline(), "fp-1", &stubModel{name: "gen-1"}, leases)); !errors.Is(err, sinkErr) {
			t.Fatalf("publish beside a failing record commit = %v, want the recorder failure kept beside the refusal", err)
		}
		if got := pub.Current(); got.Generation != 0 {
			t.Fatalf("effective publication = %+v, want nothing switched beside the record that could not be committed", got)
		}
		if len(log.names) != 1 || log.names[0] != "model-a" {
			t.Fatalf("released = %v, want the candidate-exclusive resources freed beside the refusal", log.names)
		}
		if len(sink.records) != 0 {
			t.Fatalf("committed records = %d, want no record beside the failed commit", len(sink.records))
		}

		sinkOK := &recordSink{}
		pubOK := orgstate.NewPublisher(orgstate.NewBindings(), contract, policy, sinkOK)
		outFirst, err := pubOK.Publish(ctx, publicationCandidate(t, pubOK.Baseline(), "fp-1", &stubModel{name: "gen-1"}, resourceLeases(t, "model-a")))
		if err != nil || outFirst.Publication.Generation != 1 {
			t.Fatalf("publish the first generation = (%+v, %v), want the generation applied", outFirst, err)
		}
		outSecond, err := pubOK.Publish(ctx, publicationCandidate(t, pubOK.Baseline(), "fp-2", &stubModel{name: "gen-2"}, resourceLeases(t, "model-b")))
		if err != nil || outSecond.Publication.Generation != 2 {
			t.Fatalf("publish the second generation = (%+v, %v), want one more generation applied", outSecond, err)
		}
		if len(sinkOK.records) != 2 {
			t.Fatalf("committed records = %d, want one self-contained record per publication", len(sinkOK.records))
		}
		for i, want := range []orgstate.PublicationRecord{
			{Generation: 1, Fingerprint: "fp-1", Result: orgstate.PublishApplied},
			{Generation: 2, Fingerprint: "fp-2", Result: orgstate.PublishApplied},
		} {
			got := sinkOK.records[i]
			if got.Generation != want.Generation || got.Fingerprint != want.Fingerprint || got.Result != want.Result || got.RollbackSource != 0 {
				t.Fatalf("committed record %d = %+v, want the generation, the fingerprint, the result and the rollback source it published", i, got)
			}
			if len(got.Checks) != 3 {
				t.Fatalf("committed record %d = %+v, want the check dispositions recorded beside the publication", i, got)
			}
		}
	})
}

// resourceLeases registers one candidate-exclusive resource beside the release
// that frees it.
func resourceLeases(t *testing.T, name string) *orgstate.Leases {
	t.Helper()
	leases := orgstate.NewLeases()
	leases.Register("model", name, func(context.Context) error { return nil })
	return leases
}
