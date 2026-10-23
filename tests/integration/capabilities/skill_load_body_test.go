package capabilities

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/tool/knowledge"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// skillBodyLoadedMarker and skillBodyUnloadedMarker name the bodies of the two
// skills the anchors below write: one the model loads and one nobody loads.
const (
	skillBodyLoadedMarker   = "greet body marker"
	skillBodyUnloadedMarker = "unloaded body marker"
)

// skillBodyInvocation, skillBodySession and skillBodyToolCall name the one
// invocation each anchor drives over the real assembly.
const (
	skillBodyInvocation = "inv-skill-load-body"
	skillBodySession    = "session-skill-load-body"
	skillBodyToolCall   = "call-load"
	skillBodyMissing    = "missing"
	// skillBodyAgent names the agent the load anchors run as. The identity is
	// theirs alone: the sink ledger is per agent, so an anchor that shared an
	// identity with another test's agent would observe that test's bodies.
	skillBodyAgent = "agent-skill-body"
)

// TestSkillLoadBodyEntersNextRequest drives the skill_dirs load face through the public assembly and checks where the loaded body goes.
// - skill_load answers its immediate receipt with the framework "loaded: <name>" text and nothing else
// - the request after the load carries the loaded body as its own system segment
// - the body of a skill nobody loaded never enters the request
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestSkillLoadBodyEntersNextRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writeCapabilitySkill(t, root, "greet", "greet the user", skillBodyLoadedMarker+"\n")
	writeCapabilitySkill(t, root, "unloaded", "the unloaded skill", skillBodyUnloadedMarker+"\n")
	surface := skillBodySurface(t, ctx, root, skillBodyAgent)

	var mu sync.Mutex
	round := 0
	provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
		mu.Lock()
		round++
		current := round
		mu.Unlock()
		if current == 1 {
			return ntToolTurn(ntCall(skillBodyToolCall, knowledge.SkillLoadToolName, `{"skill":"greet"}`))
		}
		return ntFinalTurn("settled")
	}}

	turn := skillBodyTurn(t, ctx, surface, provider, skillBodyAgent)
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside the load and the answer decision", turn.Terminal, len(turn.Decisions))
	}
	if requestCarries(turn.Decisions[0].Request.Messages, skillBodyLoadedMarker) {
		t.Fatalf("the request before the load already carries the skill body: %+v", turn.Decisions[0].Request.Messages)
	}

	next := turn.Decisions[1].Request.Messages
	settled := settledObservationOf(t, next, skillBodyToolCall)
	if settled.Content != "loaded: greet" {
		t.Fatalf("the load settled with %q, want the framework name-level answer \"loaded: greet\"", settled.Content)
	}
	if strings.Contains(settled.Content, skillBodyLoadedMarker) {
		t.Fatalf("the settled load answer %q carries the body; the body enters the request through the system prefix, not the result text", settled.Content)
	}
	if !requestCarries(next, skillBodyLoadedMarker) {
		t.Fatalf("the request after the load carries no skill body, want the loaded body as prefix material: %+v", next)
	}
	if got := systemMessagesCarrying(next, skillBodyLoadedMarker); got != 1 {
		t.Fatalf("the request carries the loaded body in %d system segments, want exactly one: %+v", got, next)
	}
	if requestCarries(next, skillBodyUnloadedMarker) {
		t.Fatalf("the request carries the body of a skill nobody loaded: %+v", next)
	}
}

// TestUnknownSkillStillRefused drives the load of a name the repository does not hold through the public assembly.
// - the load is refused by name in the framework's own words
// - the next request carries no skill body, so a refused load publishes none
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestUnknownSkillStillRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writeCapabilitySkill(t, root, "greet", "greet the user", skillBodyLoadedMarker+"\n")
	surface := skillBodySurface(t, ctx, root, skillBodyAgent)

	var mu sync.Mutex
	round := 0
	provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
		mu.Lock()
		round++
		current := round
		mu.Unlock()
		if current == 1 {
			return ntToolTurn(ntCall(skillBodyToolCall, knowledge.SkillLoadToolName, `{"skill":"`+skillBodyMissing+`"}`))
		}
		return ntFinalTurn("settled")
	}}

	turn := skillBodyTurn(t, ctx, surface, provider, skillBodyAgent)
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("turn = (%v, %d decisions), want the refused load decision beside the answer decision", turn.Terminal, len(turn.Decisions))
	}

	next := turn.Decisions[1].Request.Messages
	settled := settledObservationOf(t, next, skillBodyToolCall)
	if settled.Content != "unknown skill: "+skillBodyMissing {
		t.Fatalf("the refused load settled with %q, want the framework refusal naming %q", settled.Content, skillBodyMissing)
	}
	if requestCarries(next, skillBodyLoadedMarker) {
		t.Fatalf("the request after a refused load carries a skill body, want none: %+v", next)
	}
}

// skillBodySurface assembles the knowledge surface over a temporary skill
// repository, with the sink the host mounts on the agent's request-side ledger
// mounted on it exactly as the production assembly does.
func skillBodySurface(t *testing.T, ctx context.Context, root string, agent event.AgentID) []trpctool.CallableTool {
	t.Helper()
	repository, err := skill.NewFSRepository(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatalf("open the skill repository: %v", err)
	}
	surface, err := knowledge.New(ctx, knowledge.Options{
		Skills: repository,
		Sink:   tagent.NewSkillLoadSink(agent),
	})
	if err != nil {
		t.Fatalf("assemble the knowledge surface: %v", err)
	}
	return surface
}

// skillBodyTurn assembles the runtime over one knowledge surface and drives one
// invocation of the scripted model through the public entry, so both anchors
// observe the request the real request assembly fixed.
func skillBodyTurn(t *testing.T, ctx context.Context, surface []trpctool.CallableTool, provider *ntModel, agent event.AgentID) agent.Turn {
	t.Helper()
	run, err := tagent.Assemble(ctx, &config.Declaration{
		Entry: string(agent),
		Agents: []config.AgentSpec{{
			ID:     string(agent),
			Model:  "model-a",
			Tools:  []config.ToolRef{{Name: knowledge.SkillLoadToolName}},
			Params: map[string]float64{},
		}},
	}, map[event.AgentID]tagent.NativeAgent{
		agent: {Model: provider, Tools: surface},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: skillBodyInvocation,
		Address:      event.Address{AgentID: agent, SessionID: skillBodySession},
		Input:        []model.Message{{Role: model.RoleUser, Content: "load a skill"}},
	})
	if err != nil {
		t.Fatalf("invoke the skill load: %v", err)
	}
	return turn
}

// settledObservationOf reports the settled observation one request carries
// under the given call identity: the settled body enters the next decision as
// input material under that identity, never as a second tool-role response.
func settledObservationOf(t *testing.T, messages []model.Message, callID string) model.Message {
	t.Helper()
	for _, msg := range messages {
		if msg.ToolID == callID && msg.Role != model.RoleTool {
			return msg
		}
	}
	t.Fatalf("no settled observation for %s in the request: %+v", callID, messages)
	return model.Message{}
}

// systemMessagesCarrying reports how many system segments of one request carry
// the given text.
func systemMessagesCarrying(messages []model.Message, want string) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == model.RoleSystem && strings.Contains(msg.Content, want) {
			count++
		}
	}
	return count
}

// requestCarries reports whether any message of one request carries the given
// text.
func requestCarries(messages []model.Message, want string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.Content, want) {
			return true
		}
	}
	return false
}
