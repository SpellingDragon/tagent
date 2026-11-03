// The organization the declaration names is configuration: a reload that moves
// its entry or adds an agent publishes the move as one generation and judges it
// as one configuration change per dimension. The entry the generation declares
// speaks for the next turn while a turn already running keeps the entry it was
// admitted under, and an address the generation added serves its first turn
// through the same construction Boot ran.
package host

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// TestReloadMovesTheEntryAgent moves the declared entry while one user turn runs:
//   - the reload publishes the entry move as a new generation and files the one
//     organization entry change, owned by the entry the document now names
//   - the turn that began under the entry before the move still delivers through
//     the outbound face: the in-flight turn keeps the view it was admitted under
//   - the next turn at the entry the generation now declares is the user turn and
//     delivers through the outbound face
//   - a turn at the entry before the move is headless: it runs and delivers
//     nothing, so the user port follows the entry the generation declares
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestReloadMovesTheEntryAgent(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	var started sync.Once
	first := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	// A failed check must unblock the parked provider handler too: the scripted
	// server waits for its requests when it closes, and a turn left in flight
	// would hold the cleanup open instead of reporting the failure.
	defer unblock()
	provider.answer = func(index int) string {
		if index == 0 {
			started.Do(func() { close(first) })
			<-release
		}
		return scriptedAnswerOf("the turn settled")
	}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), "user",
		orgRosterAgent{ID: "user", Model: "model-entry"},
		orgRosterAgent{ID: "reviewer", Model: "model-reviewer"},
	))
	sender := newRecordingSender()
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	inFlight := make(chan turnResult, 1)
	go func() {
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address: event.Address{AgentID: "user", SessionID: "session-entry-in-flight"},
			Input:   []model.Message{{Role: model.RoleUser, Content: "the turn in flight"}},
		})
		inFlight <- turnResult{turn: turn, err: err}
	}()
	select {
	case <-first:
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the in-flight turn to reach the provider")
	}

	before := host.Run().CommittedFacts()
	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), "reviewer",
		orgRosterAgent{ID: "user", Model: "model-entry"},
		orgRosterAgent{ID: "reviewer", Model: "model-reviewer"},
	))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published || answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the entry move published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %s, want none beside the entry move", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one organization entry dimension", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	if change.Agent != "reviewer" || change.Dimension != "org.entry" {
		t.Fatalf("filed config change = %+v, want the organization entry dimension of the entry now declared", change)
	}
	if change.Fact == "" {
		t.Fatal("the filed entry change names no committed fact")
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed entry change: %v", err)
	}
	if fact.Type != "config_change" {
		t.Fatalf("committed fact type = %q, want the configuration change family", fact.Type)
	}
	if fact.Owner.AgentID != "reviewer" {
		t.Fatalf("committed fact owner = %q, want the entry the document now names", fact.Owner.AgentID)
	}
	if got := fact.Meta["config_change_dimension"]; got != "org.entry" {
		t.Fatalf("committed fact dimension = %q, want the organization entry dimension", got)
	}
	if got := fact.Meta["trigger_source"]; got != "config" {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta["config_change_generation"]; got != formatGeneration(answer.Publication.Generation) {
		t.Fatalf("committed fact generation = %q, want the publication generation %d", got, answer.Publication.Generation)
	}
	for _, want := range []string{"entry moved", "user", "reviewer"} {
		if !strings.Contains(fact.Message.Content, want) {
			t.Fatalf("committed fact body = %q, want the entry move naming %q", fact.Message.Content, want)
		}
	}
	if after := host.Run().CommittedFacts(); after != before+1 {
		t.Fatalf("committed facts = %d after the entry move, want the one config change beside the %d before", after, before)
	}

	unblock()
	result := <-inFlight
	if result.err != nil {
		t.Fatalf("invoke the in-flight turn: %v", result.err)
	}
	if result.turn.Terminal != model.KindCompleted {
		t.Fatalf("in-flight turn terminal = %v, want the completed turn of the entry before the move", result.turn.Terminal)
	}
	waitForSettle(t, "the in-flight turn of the entry before the move to reach the outbound face", func() bool {
		return len(sender.bodies()) == 1
	})
	if actions := sender.actions(); actions[0].target.AgentID != "user" {
		t.Fatalf("in-flight delivery target = %q, want the entry the turn was admitted under", actions[0].target.AgentID)
	}

	next := invokeAgentTurn(t, ctx, host, "reviewer", "session-entry-next")
	if next.Terminal != model.KindCompleted {
		t.Fatalf("turn at the moved entry terminal = %v, want the completed turn", next.Terminal)
	}
	waitForSettle(t, "the turn at the moved entry to reach the outbound face", func() bool {
		return len(sender.bodies()) == 2
	})
	if actions := sender.actions(); actions[1].target.AgentID != "reviewer" {
		t.Fatalf("delivery target = %q, want the entry the generation now declares", actions[1].target.AgentID)
	}

	headless := invokeAgentTurn(t, ctx, host, "user", "session-entry-headless")
	if headless.Terminal != model.KindCompleted {
		t.Fatalf("turn at the entry before the move terminal = %v, want the completed headless turn", headless.Terminal)
	}
	if got := len(sender.bodies()); got != 2 {
		t.Fatalf("delivered bodies = %d after a turn at the entry before the move, want the %d of the user port", got, 2)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestReloadAddsAnAgentThatServesItsFirstTurn adds a declared agent between two
// turns:
//   - the reload publishes the added agent as a new generation and files the one
//     organization agents change, owned by the added agent
//   - the address the generation added answers a real turn on the model the
//     document names for it, so the consumer mounts it on first use
//   - the lease the publication mounted for the added agent serves a later model
//     move: the next turn at that address runs on the installed version
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestReloadAddsAnAgentThatServesItsFirstTurn(t *testing.T) {
	viewAnchorEnv(t)
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf("the turn settled") }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), "user",
		orgRosterAgent{ID: "user", Model: "model-entry"},
	))
	host := bootViewAnchorHost(t, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if turn := invokeScriptedTurn(t, ctx, host, "session-added-agent"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot generation", turn.Terminal)
	}
	if served := provider.servedModels(); len(served) != 1 || served[0] != "model-entry" {
		t.Fatalf("served models = %v, want the boot turn on the declared model", served)
	}
	before := host.Run().CommittedFacts()

	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), "user",
		orgRosterAgent{ID: "user", Model: "model-entry"},
		orgRosterAgent{ID: "other", Model: "model-other", Block: orgRosterSendToolBlock},
	))
	status, body := postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("reload status = %d, want 200: %s", status, body)
	}
	answer := decodeConfigChangeAnswer(t, body)
	if !answer.Publication.Published || answer.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the added agent published as a new generation", answer.Publication)
	}
	if len(answer.ModelChanges) != 0 {
		t.Fatalf("judged model changes = %s, want none beside the added agent", answer.ModelChanges)
	}
	if len(answer.ConfigChanges) != 1 {
		t.Fatalf("filed config changes = %+v, want the one organization agents dimension", answer.ConfigChanges)
	}
	change := answer.ConfigChanges[0]
	if change.Agent != "other" || change.Dimension != "org.agents" {
		t.Fatalf("filed config change = %+v, want the organization agents dimension of the added agent", change)
	}
	if change.Fact == "" {
		t.Fatal("the filed agents change names no committed fact")
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed agents change: %v", err)
	}
	if fact.Type != "config_change" {
		t.Fatalf("committed fact type = %q, want the configuration change family", fact.Type)
	}
	if fact.Owner.AgentID != "other" {
		t.Fatalf("committed fact owner = %q, want the added agent", fact.Owner.AgentID)
	}
	if got := fact.Meta["config_change_dimension"]; got != "org.agents" {
		t.Fatalf("committed fact dimension = %q, want the organization agents dimension", got)
	}
	if got := fact.Meta["trigger_source"]; got != "config" {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta["config_change_generation"]; got != formatGeneration(answer.Publication.Generation) {
		t.Fatalf("committed fact generation = %q, want the publication generation %d", got, answer.Publication.Generation)
	}
	for _, want := range []string{"other", "added"} {
		if !strings.Contains(fact.Message.Content, want) {
			t.Fatalf("committed fact body = %q, want the added agent naming %q", fact.Message.Content, want)
		}
	}
	if after := host.Run().CommittedFacts(); after != before+1 {
		t.Fatalf("committed facts = %d after adding the agent, want the one config change beside the %d before", after, before)
	}
	if publications := host.Run().LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none beside an added agent", publications)
	}

	added := invokeAgentTurn(t, ctx, host, "other", "session-added-agent-first")
	if added.Terminal != model.KindCompleted {
		t.Fatalf("turn of the added agent terminal = %v, want the completed turn", added.Terminal)
	}
	if served := provider.servedModels(); len(served) != 2 || served[1] != "model-other" {
		t.Fatalf("served models = %v, want the added agent served by the model its declaration names", served)
	}

	writeHostDocumentFile(t, configPath, orgRosterDocument(root, provider.endpoint(), "user",
		orgRosterAgent{ID: "user", Model: "model-entry"},
		orgRosterAgent{ID: "other", Model: "model-other-b", Block: orgRosterSendToolBlock},
	))
	status, body = postReloadAttempt(t, host.FaceAddr())
	if status != 200 {
		t.Fatalf("model reload status = %d, want 200: %s", status, body)
	}
	moved := decodeReloadAnswer(t, body)
	if !moved.Publication.Published {
		t.Fatalf("publication = %+v, want the model move of the added agent published as a new generation", moved.Publication)
	}
	if len(moved.ModelChanges) != 1 {
		t.Fatalf("judged model changes = %+v, want the one move of the added agent", moved.ModelChanges)
	}
	move := moved.ModelChanges[0]
	if move.Agent != "other" || move.From != "local/model-other" || move.To != "local/model-other-b" {
		t.Fatalf("judged change = %+v, want the added agent moving between the declared versions", move)
	}
	if configChanges := decodeConfigChangeAnswer(t, body); len(configChanges.ConfigChanges) != 0 {
		t.Fatalf("filed config changes = %+v, want none beside the model move", configChanges.ConfigChanges)
	}

	second := invokeAgentTurn(t, ctx, host, "other", "session-added-agent-second")
	if second.Terminal != model.KindCompleted {
		t.Fatalf("second turn of the added agent terminal = %v, want the completed turn", second.Terminal)
	}
	if served := provider.servedModels(); len(served) != 3 || served[2] != "model-other-b" {
		t.Fatalf("served models = %v, want the added agent served by the installed version", served)
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// turnResult is one invocation the check runs beside the test goroutine: the turn
// it answered beside the failure that stopped it.
type turnResult struct {
	turn agent.Turn
	err  error
}

// invokeAgentTurn drives one turn at an explicit agent session address and
// answers its turn.
func invokeAgentTurn(t *testing.T, ctx context.Context, host *assembly.Host, agentID, sessionID string) agent.Turn {
	t.Helper()
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: event.Address{AgentID: event.AgentID(agentID), SessionID: event.SessionID(sessionID)},
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the host at agent %q: %v", agentID, err)
	}
	return turn
}

// formatGeneration renders one publication generation as the metadata value a
// committed configuration change carries.
func formatGeneration(generation uint64) string {
	return strconv.FormatUint(generation, 10)
}

// orgRosterSendToolBlock declares the host send tool for one agent, so a check
// adds or removes an agent whose section is not bare: the tool section of an
// added agent is built with its first binding and a removed agent's section
// leaves with the generation that dropped it.
const orgRosterSendToolBlock = "    tools:\n      - { kind: tool, id: send }\n"

// orgRosterAgent is one declared agent of a roster document: the identity and the
// model reference the agent names, and an empty model beside the document default.
// Block lists further agent section lines already indented, so a check declares a
// tool section beside the model of the agent.
type orgRosterAgent struct {
	ID    string
	Model string
	Block string
}

// orgRosterDocument renders one host document over the scripted provider: the
// given entry agent and the declared agents in order, over one local provider
// registry entry and the storage root the document names.
func orgRosterDocument(root, endpoint, entry string, agents ...orgRosterAgent) string {
	var b strings.Builder
	b.WriteString("entry: " + entry + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: model-entry\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	for _, agent := range agents {
		if strings.TrimSpace(agent.Model) == "" && strings.TrimSpace(agent.Block) == "" {
			b.WriteString("  " + agent.ID + ": {}\n")
			continue
		}
		b.WriteString("  " + agent.ID + ":\n")
		if strings.TrimSpace(agent.Model) != "" {
			b.WriteString("    model: " + agent.Model + "\n")
		}
		b.WriteString(agent.Block)
	}
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}
