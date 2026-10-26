// The agent scoping of the projection index (G4): the mapped sequence of one
// session chain registers under the full address — the agent beside its session
// — so two agents that share a session id never read each other's projected
// history. Both agents of the scenario declare a model window — the declaration
// that registers a mapped sequence — and drive their turns over one shared
// session id under distinct model identities, so every request the provider
// served is attributed to the agent that asked it, and the markers of one
// agent's committed history are read back from the requests themselves.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// isolationAlphaOne, isolationAlphaTwo, isolationAlphaAnswer, isolationBetaOne,
// isolationBetaTwo and isolationBetaAnswer are the history markers the
// scenario's turns commit and the answer markers its decisions answer with.
const (
	isolationAlphaOne    = "alpha-history-marker-A11"
	isolationAlphaTwo    = "alpha-history-marker-A22"
	isolationAlphaAnswer = "alpha-answer-marker"
	isolationBetaOne     = "beta-history-marker-B11"
	isolationBetaTwo     = "beta-history-marker-B22"
	isolationBetaAnswer  = "beta-answer-marker"
)

// isolationAgentSections renders the two agent sections of one isolation host:
// each agent declares its own model identity beside the model window that
// registers its mapped sequence, so the requests of the shared session carry
// projection material at all and are attributed by the model they name.
func isolationAgentSections() string {
	return "  user:\n" +
		"    model: model-alpha\n" +
		"    max_tokens: 32000\n" +
		"  other:\n" +
		"    model: model-beta\n" +
		"    max_tokens: 32000\n"
}

// isolationRequestText renders every model-visible character one request
// carries: the message contents beside the ids, names and arguments of its tool
// calls, so a marker anywhere in the request is seen by the check.
func isolationRequestText(request scriptedRequest) string {
	parts := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		parts = append(parts, message.Content, message.ToolCallID)
		for _, call := range message.ToolCalls {
			parts = append(parts, call.ID, call.Name, call.Arguments)
		}
	}
	return strings.Join(parts, "\n")
}

// isolationMarkersOf reports the own and foreign marker sets of one model
// identity: the markers of the agent that owns the model beside every marker of
// the other agent's history and answers.
func isolationMarkersOf(modelName string) (own, foreign []string) {
	switch modelName {
	case "model-alpha":
		return []string{isolationAlphaOne, isolationAlphaTwo, isolationAlphaAnswer},
			[]string{isolationBetaOne, isolationBetaTwo, isolationBetaAnswer}
	case "model-beta":
		return []string{isolationBetaOne, isolationBetaTwo, isolationBetaAnswer},
			[]string{isolationAlphaOne, isolationAlphaTwo, isolationAlphaAnswer}
	}
	return nil, nil
}

// isolationForeignMarkers reports the foreign markers one request carries. The
// attribution reads the model the request named, never the declaration alone,
// so a request is judged exactly as the provider saw it.
func isolationForeignMarkers(request scriptedRequest) []string {
	_, foreign := isolationMarkersOf(request.Model)
	text := isolationRequestText(request)
	var carried []string
	for _, marker := range foreign {
		if strings.Contains(text, marker) {
			carried = append(carried, marker)
		}
	}
	return carried
}

// TestAgentSessionIsolation requires the projected history of one session chain to stay scoped to the agent that owns it.
//   - positive: every request of an agent carries its own chain's history markers and none of the other agent's
//   - negative: TestAgentSessionIsolation/detector_flags_foreign_marker refuses a request carrying the other agent's marker by name
//   - consumer: the scenario boots the formal assembly and drives real Host.Invoke turns over an OpenAI-compatible endpoint
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAgentSessionIsolation(t *testing.T) {
	reproductionEnv(t)
	t.Run("requests_stay_scoped", runAgentSessionIsolation)
	t.Run("detector_flags_foreign_marker", checkIsolationDetectorFlagsForeignMarker)
}

// runAgentSessionIsolation drives two turns of each agent over one shared session id and requires every served request to carry its own chain's history markers alone.
func runAgentSessionIsolation(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.decide = func(request scriptedRequest) (string, error) {
		answer := isolationBetaAnswer
		if request.Model == "model-alpha" {
			answer = isolationAlphaAnswer
		}
		return scriptedAnswerOf(answer), nil
	}
	sender := newRecordingSender()
	host := bootIntegrityHostWithAgents(t, provider, sender, isolationAgentSections())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const shared = event.SessionID("session-agent-isolation")
	alpha := event.Address{AgentID: "user", SessionID: shared}
	beta := event.Address{AgentID: "other", SessionID: shared}

	for _, step := range []struct {
		addr  event.Address
		input string
	}{
		{alpha, isolationAlphaOne},
		{beta, isolationBetaOne},
		{alpha, isolationAlphaTwo},
		{beta, isolationBetaTwo},
	} {
		turn, err := driveIntegrityTurn(ctx, host, step.addr, step.input)
		if err != nil {
			t.Fatalf("the drive of %q ended by error: %v", step.input, err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 1 {
			t.Fatalf("the drive of %q = (%v, %d decisions), want the completed turn of its one decision",
				step.input, turn.Terminal, len(turn.Decisions))
		}
	}

	requests := provider.servedRequests()
	if len(requests) != 4 {
		t.Fatalf("model requests = %d, want one decision per driven turn", len(requests))
	}
	wants := []struct {
		model   string
		carries []string
	}{
		{model: "model-alpha", carries: []string{isolationAlphaOne}},
		{model: "model-beta", carries: []string{isolationBetaOne}},
		{model: "model-alpha", carries: []string{isolationAlphaOne, isolationAlphaTwo, isolationAlphaAnswer}},
		{model: "model-beta", carries: []string{isolationBetaOne, isolationBetaTwo, isolationBetaAnswer}},
	}
	for index, request := range requests {
		want := wants[index]
		if request.Model != want.model {
			t.Fatalf("request %d named model %q, want the identity %q of its driven turn", index+1, request.Model, want.model)
		}
		text := isolationRequestText(request)
		for _, marker := range want.carries {
			if !strings.Contains(text, marker) {
				t.Fatalf("request %d (model %s) = %+v, want its own chain's marker %q carried", index+1, request.Model, request.Messages, marker)
			}
		}
		if carried := isolationForeignMarkers(request); len(carried) != 0 {
			t.Fatalf("request %d (model %s) carries the foreign markers %v of the other agent's history; the projection index must scope one session chain to its own agent (messages = %+v)",
				index+1, request.Model, carried, request.Messages)
		}
	}
	t.Logf("[isolation] positive: the four requests over one shared session id each carry their own chain's markers beside none of the other agent's (%s vs %s)",
		isolationAlphaOne, isolationBetaOne)
	closeIntegrityHost(t, ctx, host)
}

// checkIsolationDetectorFlagsForeignMarker requires the isolation check itself to name the foreign marker of a crafted request, so the scenario's green is never a vacuous pass.
func checkIsolationDetectorFlagsForeignMarker(t *testing.T) {
	request := scriptedRequest{
		Index: 0,
		Model: "model-beta",
		Messages: []scriptedMessage{
			{Role: string(model.RoleUser), Content: isolationBetaOne},
			{Role: string(model.RoleAssistant), Content: isolationAlphaOne},
		},
	}
	carried := isolationForeignMarkers(request)
	if len(carried) == 0 || !strings.Contains(strings.Join(carried, "\n"), isolationAlphaOne) {
		t.Fatalf("the isolation detector read a request carrying the foreign marker %q as clean: %v", isolationAlphaOne, carried)
	}
	t.Logf("[isolation] negative: the detector names the foreign marker %q of a request that carries it", isolationAlphaOne)
}
