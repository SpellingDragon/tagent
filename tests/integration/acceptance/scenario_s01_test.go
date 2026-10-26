package acceptance

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// TestScenario01OrganizationPublication drives the sole publication entry over one two-agent declaration.
// - a byte-identical reload publishes no second generation
// - a candidate whose baseline advanced is discarded beside the resources it freed
// - a candidate the governance refuses frees exactly what it acquired
// - a request under way keeps the generation it borrowed while the newer one takes effect
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestScenario01OrganizationPublication(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S01")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	governance := &scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}
	sink := &scenarioPublicationSink{}
	publisher := state.NewPublisher(state.NewBindings(), scenarioPublishContract(), governance, sink)
	decl := scenarioDeclaration()
	live := scenarioBindings(&scenarioModel{name: "model-a"}, &scenarioModel{name: "model-b"})
	firstLeases := state.NewLeases()
	firstLeases.Register("model", "model-a", func(context.Context) error { return nil })
	firstLeases.Register("model", "model-b", func(context.Context) error { return nil })
	first, err := publisher.Publish(ctx, scenarioCandidate(t, publisher, decl, live, "fp-org", firstLeases))
	if err != nil {
		t.Fatalf("publish the organization: %v", err)
	}
	reloadLeases := state.NewLeases()
	reloadLeases.Register("model", "model-a", func(context.Context) error { return nil })
	reloadLeases.Register("model", "model-b", func(context.Context) error { return nil })
	reload, err := publisher.Publish(ctx, scenarioCandidate(t, publisher, decl, live, "fp-org", reloadLeases))
	if err != nil {
		t.Fatalf("reload the byte-identical organization: %v", err)
	}
	published := sink.published()
	observations["published_generation_count"] = int(publisher.Current().Generation)
	observations["same_content_no_republish"] = reload.Result == state.PublishUnchanged &&
		reload.Publication.Generation == first.Publication.Generation && len(published) == 1
	refs = append(refs, scenarioPublicationRefs(published)...)

	stalePublisher := state.NewPublisher(state.NewBindings(), scenarioPublishContract(), governance, &scenarioPublicationSink{})
	staleLive := scenarioBindings(&scenarioModel{name: "model-a"}, &scenarioModel{name: "model-b"})
	staleLiveLeases := state.NewLeases()
	staleLiveLeases.Register("model", "model-a", func(context.Context) error { return nil })
	staleLiveLeases.Register("model", "model-b", func(context.Context) error { return nil })
	staleBaseline := stalePublisher.Baseline()
	if _, err := stalePublisher.Publish(ctx, scenarioCandidate(t, stalePublisher, decl, staleLive, "fp-org-1", staleLiveLeases)); err != nil {
		t.Fatalf("publish the generation the stale candidate was prepared beside: %v", err)
	}
	nextLeases := state.NewLeases()
	nextLeases.Register("model", "model-a", func(context.Context) error { return nil })
	nextLeases.Register("model", "model-b", func(context.Context) error { return nil })
	if _, err := stalePublisher.Publish(ctx, scenarioCandidate(t, stalePublisher, decl, staleLive, "fp-org-2", nextLeases)); err != nil {
		t.Fatalf("publish the newer organization: %v", err)
	}
	rebuilt := scenarioCandidateAt(t, decl, staleLive, staleBaseline, "fp-stale")
	_, staleErr := stalePublisher.Publish(ctx, rebuilt)
	observations["stale_candidate_rejected"] = errors.Is(staleErr, state.ErrBaselineAdvanced)

	refusing := &scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark")}
	failingPublisher := state.NewPublisher(state.NewBindings(), scenarioPublishContract(), refusing, &scenarioPublicationSink{})
	releases := &scenarioReleases{}
	failingLeases := state.NewLeases()
	failingLeases.Register("model", "model-a", releases.track("model-a"))
	failingLeases.Register("model", "model-b", releases.track("model-b"))
	_, failingErr := failingPublisher.Publish(ctx, scenarioCandidate(t, failingPublisher, decl, live, "fp-org-failing", failingLeases))
	released := releases.released()
	observations["candidate_failure_released_resources"] = errors.Is(failingErr, state.ErrRequiredCheckMissing) &&
		len(released) == 2 && released[0] == "model-b" && released[1] == "model-a" &&
		failingPublisher.Current().Generation == 0

	inflightPublisher := state.NewPublisher(state.NewBindings(), scenarioPublishContract(), governance, &scenarioPublicationSink{})
	inflightRelease := &scenarioReleases{}
	inflightLeases := state.NewLeases()
	inflightLeases.Register("model", "model-a", inflightRelease.track("model-a"))
	inflightLeases.Register("model", "model-b", inflightRelease.track("model-b"))
	if _, err := inflightPublisher.Publish(ctx, scenarioCandidate(t, inflightPublisher, decl, live, "fp-org-old", inflightLeases)); err != nil {
		t.Fatalf("publish the generation the in-flight request borrows: %v", err)
	}
	handle, err := inflightPublisher.Borrow(ctx, "agent-a")
	if err != nil {
		t.Fatalf("borrow the in-flight generation: %v", err)
	}
	newer := scenarioBindings(&scenarioModel{name: "model-a-next"}, &scenarioModel{name: "model-b-next"})
	newerLeases := state.NewLeases()
	newerLeases.Register("model", "model-a", inflightRelease.track("model-a-next"))
	newerLeases.Register("model", "model-b", inflightRelease.track("model-b-next"))
	if _, err := inflightPublisher.Publish(ctx, scenarioCandidate(t, inflightPublisher, decl, newer, "fp-org-new", newerLeases)); err != nil {
		t.Fatalf("publish the newer generation beside the in-flight request: %v", err)
	}
	held := handle.Binding.Model == live["agent-a"].Model
	retiredWhileHeld := len(inflightRelease.released()) == 0
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("release the in-flight generation: %v", err)
	}
	retiredAfterHold := inflightRelease.released()
	observations["inflight_request_generation"] = scenarioGenerationOf(held, retiredWhileHeld, len(retiredAfterHold))
	refs = append(refs, "borrow:"+fmt.Sprintf("%d", handle.Ref.Generation))

	EmitScenarioRecord(t, scenario, refs, observations)
}

// scenarioCandidateAt prepares one publication candidate under an explicit
// baseline, so a stale publication attempt carries the baseline it captured.
func scenarioCandidateAt(t *testing.T, decl *config.Declaration, agents map[event.AgentID]state.AgentBinding, baseline, fingerprint string) state.Candidate {
	t.Helper()
	built, err := state.NewStatePlan(state.OrgPlan{Declaration: decl, Baseline: baseline, Fingerprint: fingerprint}, agents, nil)
	if err != nil {
		t.Fatalf("prepare the candidate plan %s: %v", fingerprint, err)
	}
	return state.Candidate{Plan: built, Resources: state.NewLeases()}
}

// scenarioGenerationOf reports the generation one in-flight request keeps: the
// generation it borrowed stays in effect while the newer publication lands, and
// the superseded resources retire only after the hold ends.
func scenarioGenerationOf(held, retiredWhileHeld bool, releasedAfter int) string {
	if held && retiredWhileHeld && releasedAfter > 0 {
		return "previous"
	}
	return "current"
}

// scenarioPublicationRefs reports the generation and fingerprint identities the
// publication records carry.
func scenarioPublicationRefs(records []state.PublicationRecord) []string {
	refs := make([]string, 0, len(records))
	for _, record := range records {
		refs = append(refs, "publication:"+itoa(uint64(record.Generation))+":"+record.Fingerprint)
	}
	return refs
}

// itoa reports the decimal form of one generation counter.
func itoa(value uint64) string {
	return fmt.Sprintf("%d", value)
}
