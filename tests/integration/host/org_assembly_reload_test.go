// The configuration-declared delegation assembly: the agent references a
// declaration wires are the organization's own assembly, so their move is a
// configuration change of the org.assembly dimension and the next call reaches the
// assembly the new generation declares. A refused reload judges nothing.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
)

// TestReloadJudgesTheDeclaredDelegationAssembly rewrites the agent reference the entry declaration wires and posts the reload entry.
//   - the boot request carries the tool of the declared target, described by the file the reference names
//   - the reload publishes the moved reference as a new generation and files one configuration change
//   - the change belongs to the organization assembly dimension and is owned by the declaring agent, with no binding entry beside it
//   - the committed fact names the assembly move, the publication generation and the configuration source
//   - the next request carries the tool of the new target, described by the new description file, never the target of the generation before
//   - the call the new request issues runs on the new target, so the delegated turn is served by the target the declaration now names
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestReloadJudgesTheDeclaredDelegationAssembly(t *testing.T) {
	viewAnchorEnv(t)
	const (
		workerDescription   = "ORG-ASSEMBLY-WORKER-DESCRIPTION"
		reviewerDescription = "ORG-ASSEMBLY-REVIEWER-DESCRIPTION"
		entrySettled        = "the entry settled"
	)
	workerBlock := "    tools:\n" +
		"      - { kind: agent, agent: worker, description_file: worker.md }\n"
	reviewerBlock := "    tools:\n" +
		"      - { kind: agent, agent: reviewer, description_file: reviewer-b.md }\n"
	provider := newScriptedProvider(t)
	provider.answer = func(index int) string {
		requests := provider.servedRequests()
		if index < 0 || index >= len(requests) {
			return scriptedAnswerOf(entrySettled)
		}
		request := requests[index]
		switch request.Model {
		case "model-worker":
			return scriptedAnswerOf("the worker answered")
		case "model-reviewer":
			return scriptedAnswerOf("the reviewer answered")
		}
		if scriptedAnsweredCall(request, "call-reviewer-1") {
			return scriptedAnswerOf(entrySettled)
		}
		if scriptedCarriesAgentTool(request, "reviewer") {
			return settleToolCallAnswer("reviewer", "call-reviewer-1", "{\"input\":\"review this\"}")
		}
		return scriptedAnswerOf("the entry answered without delegating")
	}

	root := t.TempDir()
	writeReloadPrompt(t, root, "worker.md", workerDescription)
	writeReloadPrompt(t, root, "reviewer-b.md", reviewerDescription)
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, orgAssemblyHostDocument(root, provider.endpoint(), root, workerBlock))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if booted := host.Run().CommittedFacts(); booted != 0 {
		t.Fatalf("committed facts = %d after boot, want none: boot judges no configuration change", booted)
	}

	if turn := invokeScriptedTurn(t, ctx, host, "session-org-assembly"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot assembly", turn.Terminal)
	}
	requests := provider.servedRequests()
	if len(requests) != 1 {
		t.Fatalf("served requests = %d, want the one request of the boot turn", len(requests))
	}
	if !scriptedCarriesAgentTool(requests[0], "worker") || scriptedCarriesAgentTool(requests[0], "reviewer") {
		t.Fatalf("boot request tools = %v, want the declared target of the boot reference", requests[0].Tools)
	}
	if got := scriptedToolDescription(t, requests[0].Body, "worker"); !strings.Contains(got, workerDescription) {
		t.Fatalf("boot target description = %q, want the file the boot reference names", got)
	}
	beforeReload := host.Run().CommittedFacts()

	writeHostDocumentFile(t, configPath, orgAssemblyHostDocument(root, provider.endpoint(), root, reviewerBlock))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published || answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the moved reference published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %s, want none beside the assembly move", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one organization assembly dimension", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	if change.Agent != "user" || change.Dimension != "org.assembly" {
		t.Fatalf("filed config change = %+v, want the organization assembly dimension of the declaring agent", change)
	}
	if change.Fact == "" {
		t.Fatal("the filed organization assembly change names no committed fact")
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed organization assembly change: %v", err)
	}
	if fact.Type != "config_change" {
		t.Fatalf("committed fact type = %q, want the configuration change family", fact.Type)
	}
	if fact.Owner.AgentID != "user" {
		t.Fatalf("committed fact owner = %q, want the declaring agent", fact.Owner.AgentID)
	}
	if got := fact.Meta["trigger_source"]; got != "config" {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta["config_change_dimension"]; got != "org.assembly" {
		t.Fatalf("committed fact dimension = %q, want the organization assembly dimension", got)
	}
	if got := fact.Meta["config_change_generation"]; got != strconv.FormatUint(answer.Publication.Generation, 10) {
		t.Fatalf("committed fact generation = %q, want the publication generation %d", got, answer.Publication.Generation)
	}
	for _, want := range []string{"delegation assembly changed", "worker", "reviewer", "worker.md", "reviewer-b.md"} {
		if !strings.Contains(fact.Message.Content, want) {
			t.Fatalf("committed fact body = %q, want the assembly move naming %q", fact.Message.Content, want)
		}
	}
	if after := host.Run().CommittedFacts(); after != beforeReload+1 {
		t.Fatalf("committed facts = %d after the assembly move, want the one config change beside the %d before", after, beforeReload)
	}
	observed, err := host.Run().ObservedEvents(ctx, memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName("user")},
		EventTypes:   []string{event.TypeConfigChange},
	})
	if err != nil {
		t.Fatalf("observe the configuration change facts: %v", err)
	}
	landed := false
	for _, ref := range observed {
		if ref.Key == event.EventID(change.Fact) {
			landed = true
		}
	}
	if !landed {
		t.Fatalf("observed configuration change facts = %+v, want the organization assembly change %s on the chain", observed, change.Fact)
	}

	if turn := invokeScriptedTurn(t, ctx, host, "session-org-assembly"); turn.Terminal != model.KindCompleted {
		t.Fatalf("second turn terminal = %v, want the completed turn of the published assembly", turn.Terminal)
	}
	requests = provider.servedRequests()
	var assembled *scriptedRequest
	for index := range requests {
		if requests[index].Model == "model-entry" && scriptedCarriesAgentTool(requests[index], "reviewer") {
			assembled = &requests[index]
			break
		}
	}
	if assembled == nil {
		t.Fatalf("served requests = %+v, want a request carrying the tool of the published reference", requests)
	}
	if scriptedCarriesAgentTool(*assembled, "worker") {
		t.Fatalf("published request tools = %v, want the target of the generation before replaced", assembled.Tools)
	}
	if got := scriptedToolDescription(t, assembled.Body, "reviewer"); !strings.Contains(got, reviewerDescription) {
		t.Fatalf("published target description = %q, want the file the reloaded reference names", got)
	}
	if got := scriptedToolDescription(t, assembled.Body, "reviewer"); strings.Contains(got, workerDescription) {
		t.Fatalf("published target description = %q, want the description of the generation before replaced", got)
	}

	waitForSettle(t, "the delegated turn of the published assembly to reach its target", func() bool {
		for _, served := range provider.servedModels() {
			if served == "model-reviewer" {
				return true
			}
		}
		return false
	})
	for _, served := range provider.servedModels() {
		if served == "model-worker" {
			t.Fatalf("served models = %v, want the boot target never called: the published assembly reaches the new target", provider.servedModels())
		}
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestReloadJudgesTheRemovedAgent removes a declared agent between two turns.
//   - the reload publishes the removal as a new generation and files the one organization agents change, owned by the agent the document removed
//   - the facts the removed agent already committed stay on the chain and no cancellation is invented for the address
//   - the reload judges the declaration move alone
//   - new input at the removed address is refused by name, while the entry still serves its turns
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestReloadJudgesTheRemovedAgent(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf("ok") }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), "user",
		orgRosterAgent{ID: "user", Model: "model-entry"},
		orgRosterAgent{ID: "worker", Model: "model-worker", Block: orgRosterSendToolBlock},
	))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-org-removed"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot assembly", turn.Terminal)
	}
	workerAddr := event.Address{AgentID: "worker", SessionID: "session-org-removed-worker"}
	if turn := invokeAgentTurn(t, ctx, host, "worker", string(workerAddr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("worker turn terminal = %v, want the completed turn of the declared worker", turn.Terminal)
	}
	workerFacts := host.Run().SessionFacts(workerAddr)
	if len(workerFacts) == 0 {
		t.Fatal("the worker committed no fact to keep beside the removal")
	}
	before := host.Run().CommittedFacts()

	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), "user",
		orgRosterAgent{ID: "user", Model: "model-entry"},
	))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("removal reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published || answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the removal published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %s, want none beside the removal", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one organization agents change", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	if change.Agent != "worker" || change.Dimension != "org.agents" {
		t.Fatalf("filed config change = %+v, want the organization agents dimension of the removed agent", change)
	}
	if change.Fact == "" {
		t.Fatal("the filed agents change names no committed fact")
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed removal change: %v", err)
	}
	if fact.Type != "config_change" {
		t.Fatalf("committed fact type = %q, want the configuration change family", fact.Type)
	}
	if fact.Owner.AgentID != "worker" {
		t.Fatalf("committed fact owner = %q, want the removed agent", fact.Owner.AgentID)
	}
	if got := fact.Meta["config_change_dimension"]; got != "org.agents" {
		t.Fatalf("committed fact dimension = %q, want the organization agents dimension", got)
	}
	if got := fact.Meta["config_change_generation"]; got != strconv.FormatUint(answer.Publication.Generation, 10) {
		t.Fatalf("committed fact generation = %q, want the publication generation %d", got, answer.Publication.Generation)
	}
	for _, want := range []string{"worker", "removed"} {
		if !strings.Contains(fact.Message.Content, want) {
			t.Fatalf("committed fact body = %q, want the removed agent naming %q", fact.Message.Content, want)
		}
	}
	after := host.Run().CommittedFacts()
	if after != before+1 {
		t.Fatalf("committed facts = %d after the removal, want the one config change beside the %d before: no cancellation is invented", after, before)
	}
	for _, held := range workerFacts {
		kept, err := host.Run().CommittedEvent(ctx, held.Key)
		if err != nil {
			t.Fatalf("read the worker fact %s after the removal: %v", held.Key, err)
		}
		if kept.Key != held.Key {
			t.Fatalf("kept worker fact = %s, want the fact the worker committed", kept.Key)
		}
	}

	_, err = host.Invoke(ctx, assembly.InvokeRequest{
		Address: workerAddr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello again"}},
	})
	if !errors.Is(err, assembly.ErrHostAgentUndeclared) {
		t.Fatalf("input at the removed address error = %v, want the undeclared agent refusal", err)
	}
	if !strings.Contains(err.Error(), "worker") {
		t.Fatalf("refusal = %v, want the removed agent named", err)
	}
	if later := host.Run().CommittedFacts(); later != after {
		t.Fatalf("committed facts = %d after the refusal, want the %d before: a refused input commits nothing", later, after)
	}
	if turn := invokeScriptedTurn(t, ctx, host, "session-org-removed-entry"); turn.Terminal != model.KindCompleted {
		t.Fatalf("entry turn after the removal terminal = %v, want the completed turn", turn.Terminal)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// orgAssemblyHostDocument renders one host document whose entry agent declares
// the given tool references beside the declared target agents, over the prompt
// directory the agent references resolve their description files against.
func orgAssemblyHostDocument(root, endpoint, promptDir, entryBlock string, targetSections ...string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("prompt_dir: \"" + promptDir + "\"\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: model-entry\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    model: model-entry\n")
	b.WriteString(entryBlock)
	b.WriteString("  worker:\n    model: model-worker\n")
	b.WriteString("  reviewer:\n    model: model-reviewer\n")
	for _, section := range targetSections {
		b.WriteString(section)
	}
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// scriptedToolDescription reads the description one recorded request declared for
// the named tool, and the empty string when the request carried no such tool.
func scriptedToolDescription(t *testing.T, body map[string]json.RawMessage, name string) string {
	t.Helper()
	raw, ok := body["tools"]
	if !ok {
		return ""
	}
	var tools []struct {
		Function struct {
			Name        string
			Description string
		}
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("decode the recorded tools: %v", err)
	}
	for _, tool := range tools {
		if tool.Function.Name == name {
			return tool.Function.Description
		}
	}
	return ""
}

// scriptedAnsweredCall reports whether the transcript of one request already
// carries the tool result of the named call, so a decision that follows the
// delegation settles instead of calling the target a second time.
func scriptedAnsweredCall(request scriptedRequest, callID string) bool {
	for _, message := range request.Messages {
		if message.ToolCallID == callID {
			return true
		}
	}
	return false
}
