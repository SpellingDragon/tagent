package host

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestConfigChangeReachesRequestBoundary drives the three anchors of the metadata eventization leaf over the formal configuration entry.
//   - prompt change: the request under way keeps the view it fixed, and the next request carries the newly published prompt source
//   - model change: the decision under way keeps the model it began on, and the next decision runs the installed model its capture records
//   - same request fixed view: further changes while one request is in flight leave that request on its boundary view
//   - the next request reads only the latest published generation
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChangeReachesRequestBoundary(t *testing.T) {
	t.Run("prompt change reaches the next request while the in-flight one keeps its view", runPromptViewAnchor)
	t.Run("model change reaches the next decision while the in-flight one keeps its model", runModelViewAnchor)
	t.Run("further changes leave one in-flight request on its boundary view", runFixedViewAnchor)
}

// viewAnchorEnv prepares the provider credential and loopback face the booted
// hosts of these checks run under.
func viewAnchorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
}

// bootViewAnchorHost boots one host over the configuration source and hands the
// test its cleanup.
func bootViewAnchorHost(t *testing.T, configPath string) *assembly.Host {
	t.Helper()
	host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	return host
}

// viewAnchorSnapshots reports the capture's fixed request views in fix order.
func viewAnchorSnapshots(host *assembly.Host) []assembly.RequestSnapshotRecord {
	return host.Run().RequestSnapshots()
}

// runPromptViewAnchor submits one prompt-source change through the formal
// configuration entry while a request is in flight:
//   - the in-flight request keeps the prompt body its boundary fixed and never
//     carries the newly published body
//   - the next request carries the published body and no stale one
func runPromptViewAnchor(t *testing.T) {
	viewAnchorEnv(t)
	const (
		inFlightInput = "anchor prompt in flight"
		nextInput     = "anchor prompt next"
		fileFlight    = "anchor prompt file in flight"
		fileNext      = "anchor prompt file next"
		bootBody      = "ANCHOR-PROMPT-BOOT-BODY"
		publishedBody = "ANCHOR-PROMPT-PUBLISHED-BODY"
		rewrittenBody = "ANCHOR-PROMPT-REWRITTEN-BODY"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	inFlight := script.expect("model-a", "the request under way", requestTail(inFlightInput)).
		answers(scriptedAnswerOf("the request under way settled")).
		park()
	script.expect("model-a", "the next request", requestTail(nextInput)).
		answers(scriptedAnswerOf("the next request settled"))
	fileFlightStep := script.expect("model-a", "the request reading the file body under way", requestTail(fileFlight)).
		answers(scriptedAnswerOf("the file body under way settled")).
		park()
	script.expect("model-a", "the request reading the rewritten file body", requestTail(fileNext)).
		answers(scriptedAnswerOf("the rewritten body settled"))
	provider.decide = script.decide

	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", bootBody)
	writeReloadPrompt(t, root, "published.md", publishedBody)
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-view-prompt"}

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, inFlightInput)
		driven <- integrityOutcome{turn: turn, err: err}
	}()
	script.await(t, inFlight)
	before := viewAnchorSnapshots(host)
	if len(before) != 1 || before[0].Snapshot.PromptVersion == "" {
		t.Fatalf("capture views under way = %+v, want the one fixed prompt version", before)
	}

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("published.md")))
	answer := postReload(t, host.FaceAddr())
	if !answer.Reloaded || !answer.Publication.Published {
		t.Fatalf("the prompt change reload = %+v, want the published generation", answer)
	}

	script.release(inFlight)
	got := awaitIntegrityDrive(t, driven, "the request under way")
	if got.err != nil || got.turn.Terminal != model.KindCompleted {
		t.Fatalf("the request under way = (%v, %v), want it completed on its boundary view", got.turn.Terminal, got.err)
	}
	requests := provider.servedRequests()
	if len(requests) != 1 {
		t.Fatalf("served requests = %d, want the one request under way", len(requests))
	}
	if !requestCarriesPrompt(provider, 0, bootBody) {
		t.Fatalf("the request under way = %+v, want the prompt body its boundary fixed", requests[0].Messages)
	}
	if requestCarriesPrompt(provider, 0, publishedBody) {
		t.Fatalf("the request under way = %+v, want no published body under it", requests[0].Messages)
	}

	second, err := driveIntegrityTurn(ctx, host, addr, nextInput)
	if err != nil || second.Terminal != model.KindCompleted {
		t.Fatalf("the next request = (%v, %v), want it completed on the published view", second.Terminal, err)
	}
	requests = provider.servedRequests()
	if len(requests) != 2 {
		t.Fatalf("served requests = %d, want the next request served beside the first", len(requests))
	}
	if !requestCarriesPrompt(provider, 1, publishedBody) {
		t.Fatalf("the next request = %+v, want the published prompt body", requests[1].Messages)
	}
	if requestCarriesPrompt(provider, 1, bootBody) {
		t.Fatalf("the next request = %+v, want no stale body of the generation before", requests[1].Messages)
	}
	after := viewAnchorSnapshots(host)
	if len(after) != 2 || after[1].Snapshot.PromptVersion == "" || after[1].Snapshot.PromptVersion == before[0].Snapshot.PromptVersion {
		t.Fatalf("capture prompt versions = (%+v, %+v), want the next request fixed on a new content version", before, after)
	}

	fileDriven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, fileFlight)
		fileDriven <- integrityOutcome{turn: turn, err: err}
	}()
	script.await(t, fileFlightStep)
	fileBefore := viewAnchorSnapshots(host)
	if len(fileBefore) != 3 {
		t.Fatalf("capture views before the rewrite = %+v, want the three fixed views", fileBefore)
	}
	writeReloadPrompt(t, root, "published.md", rewrittenBody)
	script.release(fileFlightStep)
	got = awaitIntegrityDrive(t, fileDriven, "the request reading the file body under way")
	if got.err != nil || got.turn.Terminal != model.KindCompleted {
		t.Fatalf("the request reading the file body under way = (%v, %v), want it completed on its fixed body", got.turn.Terminal, got.err)
	}
	requests = provider.servedRequests()
	if len(requests) != 3 {
		t.Fatalf("served requests = %d, want the file-body request served beside the earlier ones", len(requests))
	}
	if !requestCarriesPrompt(provider, 2, publishedBody) {
		t.Fatalf("the request reading the file body under way = %+v, want the body its boundary fixed", requests[2].Messages)
	}
	if requestCarriesPrompt(provider, 2, rewrittenBody) {
		t.Fatalf("the request reading the file body under way = %+v, want no rewritten body under it", requests[2].Messages)
	}

	fileNextTurn, err := driveIntegrityTurn(ctx, host, addr, fileNext)
	if err != nil || fileNextTurn.Terminal != model.KindCompleted {
		t.Fatalf("the request reading the rewritten file body = (%v, %v), want it completed on the rewritten body", fileNextTurn.Terminal, err)
	}
	requests = provider.servedRequests()
	if len(requests) != 4 {
		t.Fatalf("served requests = %d, want the rewritten-body request served beside the earlier ones", len(requests))
	}
	if !requestCarriesPrompt(provider, 3, rewrittenBody) {
		t.Fatalf("the request reading the rewritten file body = %+v, want the rewritten body read at its boundary", requests[3].Messages)
	}
	if requestCarriesPrompt(provider, 3, publishedBody) {
		t.Fatalf("the request reading the rewritten file body = %+v, want no stale body of the read before", requests[3].Messages)
	}
	fileAfter := viewAnchorSnapshots(host)
	if len(fileAfter) != 4 || fileAfter[3].Snapshot.PromptVersion == "" || fileAfter[3].Snapshot.PromptVersion == fileBefore[len(fileBefore)-1].Snapshot.PromptVersion {
		t.Fatalf("capture prompt versions = (%+v, %+v), want the rewritten body fixed as a new content version", fileBefore, fileAfter)
	}

	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the declared script was violated: %v", violations)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runModelViewAnchor submits one model-reference change through the formal
// configuration entry while a request is in flight:
//   - the in-flight request and its capture stay on the model it began on
//   - the next decision is served by the installed model, which its capture
//     records under the new model name
func runModelViewAnchor(t *testing.T) {
	viewAnchorEnv(t)
	const (
		inFlightInput = "anchor model in flight"
		nextInput     = "anchor model next"
		promptBody    = "ANCHOR-MODEL-PROMPT-BODY"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	inFlight := script.expect("model-a", "the decision under way", requestTail(inFlightInput)).
		answers(scriptedAnswerOf("the decision under way settled")).
		park()
	script.expect("model-b", "the next decision", requestTail(nextInput)).
		answers(scriptedAnswerOf("the next decision settled"))
	provider.decide = script.decide

	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", promptBody)
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-view-model"}

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, inFlightInput)
		driven <- integrityOutcome{turn: turn, err: err}
	}()
	script.await(t, inFlight)

	snapshots := viewAnchorSnapshots(host)
	if len(snapshots) != 1 || snapshots[0].Snapshot.Model != "model-a" {
		t.Fatalf("capture views under way = %+v, want the one fixed on model-a", snapshots)
	}

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-b", root, reloadPromptBlock("boot.md")))
	answer := postReload(t, host.FaceAddr())
	if !answer.Reloaded || !answer.Publication.Published || len(answer.ModelChanges) != 1 {
		t.Fatalf("the model change reload = %+v, want the published move onto the new version", answer)
	}
	if answer.ModelChanges[0].To != "local/model-b" || answer.ModelChanges[0].Fact == "" {
		t.Fatalf("judged model change = %+v, want the new version beside the committed model configuration change", answer.ModelChanges[0])
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(answer.ModelChanges[0].Fact))
	if err != nil {
		t.Fatalf("read the committed model configuration change: %v", err)
	}
	if fact.Type != event.TypeConfigChange || fact.Owner.AgentID != "user" {
		t.Fatalf("committed fact = (%q, %q), want the model configuration change of the declared agent", fact.Type, fact.Owner.AgentID)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionModel {
		t.Fatalf("committed fact dimension = %q, want the model dimension", got)
	}
	if got := fact.Meta[event.MetaKeyTriggerSource]; got != event.ConfigChangeSourceConfig {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}

	script.release(inFlight)
	got := awaitIntegrityDrive(t, driven, "the decision under way")
	if got.err != nil || got.turn.Terminal != model.KindCompleted {
		t.Fatalf("the decision under way = (%v, %v), want it completed on its start version", got.turn.Terminal, got.err)
	}
	served := provider.servedModels()
	if len(served) != 1 || served[0] != "model-a" {
		t.Fatalf("served models = %v, want the decision under way served by its start version", served)
	}
	snapshots = viewAnchorSnapshots(host)
	if len(snapshots) != 1 || snapshots[0].Snapshot.Model != "model-a" {
		t.Fatalf("capture views after the in-flight decision = %+v, want the view it began on", snapshots)
	}

	second, err := driveIntegrityTurn(ctx, host, addr, nextInput)
	if err != nil || second.Terminal != model.KindCompleted {
		t.Fatalf("the next decision = (%v, %v), want it completed on the installed model", second.Terminal, err)
	}
	served = provider.servedModels()
	if len(served) != 2 || served[1] != "model-b" {
		t.Fatalf("served models = %v, want the next decision served by the installed model", served)
	}
	snapshots = viewAnchorSnapshots(host)
	if len(snapshots) != 2 || snapshots[1].Snapshot.Model != "model-b" {
		t.Fatalf("capture views = %+v, want the next decision recorded under the installed model name", snapshots)
	}

	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the declared script was violated: %v", violations)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runFixedViewAnchor changes the configuration twice while one request is in
// flight:
//   - the request under way keeps its boundary view through both changes, and
//     its stream ends on the model it began on
//   - the next request reads the latest published prompt and model only
func runFixedViewAnchor(t *testing.T) {
	viewAnchorEnv(t)
	const (
		inFlightInput = "anchor fixed view in flight"
		nextInput     = "anchor fixed view next"
		bootBody      = "ANCHOR-FIXED-BOOT-BODY"
		middleBody    = "ANCHOR-FIXED-MIDDLE-BODY"
		latestBody    = "ANCHOR-FIXED-LATEST-BODY"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	inFlight := script.expect("model-a", "the request under way", requestTail(inFlightInput)).
		answers(scriptedAnswerOf("the request under way settled")).
		park()
	script.expect("model-c", "the next request", requestTail(nextInput)).
		answers(scriptedAnswerOf("the next request settled"))
	provider.decide = script.decide

	root := t.TempDir()
	writeReloadPrompt(t, root, "boot.md", bootBody)
	writeReloadPrompt(t, root, "middle.md", middleBody)
	writeReloadPrompt(t, root, "latest.md", latestBody)
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-a", root, reloadPromptBlock("boot.md")))
	host := bootViewAnchorHost(t, configPath)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-view-fixed"}

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, inFlightInput)
		driven <- integrityOutcome{turn: turn, err: err}
	}()
	script.await(t, inFlight)

	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-b", root, reloadPromptBlock("middle.md")))
	first := postReload(t, host.FaceAddr())
	if !first.Reloaded || !first.Publication.Published || len(first.ModelChanges) != 1 {
		t.Fatalf("the first change beside the request under way = %+v, want the published move", first)
	}
	writeHostDocumentFile(t, configPath, reloadTierDocument(root, provider.endpoint(), "model-c", root, reloadPromptBlock("latest.md")))
	second := postReload(t, host.FaceAddr())
	if !second.Reloaded || !second.Publication.Published || len(second.ModelChanges) != 1 {
		t.Fatalf("the second change beside the request under way = %+v, want the published move", second)
	}

	script.release(inFlight)
	got := awaitIntegrityDrive(t, driven, "the request under way")
	if got.err != nil || got.turn.Terminal != model.KindCompleted {
		t.Fatalf("the request under way = (%v, %v), want it completed on its boundary view", got.turn.Terminal, got.err)
	}
	requests := provider.servedRequests()
	if len(requests) != 1 {
		t.Fatalf("served requests = %d, want the one fixed-view request", len(requests))
	}
	if !requestCarriesPrompt(provider, 0, bootBody) {
		t.Fatalf("the request under way = %+v, want the body its boundary fixed", requests[0].Messages)
	}
	if requestCarriesPrompt(provider, 0, middleBody) || requestCarriesPrompt(provider, 0, latestBody) {
		t.Fatalf("the request under way = %+v, want no body of a later generation", requests[0].Messages)
	}
	served := provider.servedModels()
	if len(served) != 1 || served[0] != "model-a" {
		t.Fatalf("served models = %v, want the request under way served by its boundary model", served)
	}
	snapshots := viewAnchorSnapshots(host)
	if len(snapshots) != 1 || snapshots[0].Snapshot.Model != "model-a" {
		t.Fatalf("capture views = %+v, want the one view fixed at the boundary", snapshots)
	}

	third, err := driveIntegrityTurn(ctx, host, addr, nextInput)
	if err != nil || third.Terminal != model.KindCompleted {
		t.Fatalf("the next request = (%v, %v), want it completed on the latest generation", third.Terminal, err)
	}
	requests = provider.servedRequests()
	if len(requests) != 2 {
		t.Fatalf("served requests = %d, want the next request beside the fixed one", len(requests))
	}
	if !requestCarriesPrompt(provider, 1, latestBody) {
		t.Fatalf("the next request = %+v, want the latest published body", requests[1].Messages)
	}
	if requestCarriesPrompt(provider, 1, bootBody) || requestCarriesPrompt(provider, 1, middleBody) {
		t.Fatalf("the next request = %+v, want no stale body of an earlier change", requests[1].Messages)
	}
	served = provider.servedModels()
	if len(served) != 2 || served[1] != "model-c" {
		t.Fatalf("served models = %v, want the next request served by the latest installed model", served)
	}
	snapshots = viewAnchorSnapshots(host)
	if len(snapshots) != 2 || snapshots[1].Snapshot.Model != "model-c" {
		t.Fatalf("capture views = %+v, want the next request recorded under the latest model name", snapshots)
	}

	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the declared script was violated: %v", violations)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}
