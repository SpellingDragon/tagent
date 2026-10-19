package execution

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/model"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// sessionInputOf renders the input one external session drives, so the session
// and the round are readable from the request a decision was assembled for.
func sessionInputOf(session, round int) string {
	return fmt.Sprintf("sess-%d round %d", session, round)
}

// sessionOf reports the session and the round one assembled request belongs to:
// the input markers of its own material name the session, and the highest round
// among them names the drive.
func sessionOf(messages []model.Message) (int, int, bool) {
	session, round := -1, 0
	for _, msg := range messages {
		if msg.Role != model.RoleUser {
			continue
		}
		if own, at, ok := parseSessionInput(msg.Content); ok {
			session, round = own, at
		}
	}
	return session, round, session >= 0
}

// parseSessionInput reads one rendered session input back.
func parseSessionInput(content string) (int, int, bool) {
	match := sessionInputPattern.FindStringSubmatch(content)
	if match == nil {
		return 0, 0, false
	}
	session, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	round, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}
	return session, round, true
}

var (
	// sessionInputPattern matches the rendered input of one external session.
	sessionInputPattern = regexp.MustCompile(`sess-(\d+) round (\d+)`)
	// sessionMarkers matches every session marker a request carries, wherever it
	// sits: message content, a tool call argument or a call identity.
	sessionMarkers = regexp.MustCompile(`sess-\d+`)
)

// driveOverlap is how long one decision is held, so the concurrent drives
// actually overlap and a history shared between sessions would be read mid-drive.
const driveOverlap = 2 * time.Millisecond

// TestConcurrentRunsStayScoped checks that concurrent external runs of different sessions keep their histories apart through the formal entry.
// - four sessions drive two rounds each, all of them concurrently on one assembled runtime
// - every request a decision assembled carries its own session's material only, never another session's input, call or settled result
// - each later round reads its own session's earlier round and nothing else
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestConcurrentRunsStayScoped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const sessions = 4
	const rounds = 2
	tool := &scopeTool{}
	provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		time.Sleep(driveOverlap)
		own, round, ok := sessionOf(req.Messages)
		if !ok {
			t.Errorf("request of %d messages carries no session input: %+v", len(req.Messages), req.Messages)
			return convFinalTurn("unscoped")
		}
		if !hasSettledCall(req.Messages) {
			return convToolTurn(convCall(fmt.Sprintf("call-sess-%d-round-%d", own, round), "echo", `{"marker":"sess-`+strconv.Itoa(own)+`"}`))
		}
		return convFinalTurn("answer of sess-" + strconv.Itoa(own))
	}}

	run, err := tagent.Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	start := make(chan struct{})
	failures := make(chan string, sessions)
	var drives sync.WaitGroup
	for session := 0; session < sessions; session++ {
		drives.Add(1)
		go func(session int) {
			defer drives.Done()
			<-start
			for round := 1; round <= rounds; round++ {
				turn, err := run.Invoke(ctx, tagent.InvokeRequest{
					InvocationID: event.InvocationID(fmt.Sprintf("inv-sess-%d-round-%d", session, round)),
					Address:      event.Address{AgentID: "agent-a", SessionID: event.SessionID(fmt.Sprintf("session-%d", session))},
					Input:        []model.Message{{Role: model.RoleUser, Content: sessionInputOf(session, round)}},
				})
				if err != nil {
					failures <- fmt.Sprintf("session %d round %d: %v", session, round, err)
					return
				}
				if turn.Terminal != model.KindCompleted {
					failures <- fmt.Sprintf("session %d round %d terminal = %v", session, round, turn.Terminal)
					return
				}
			}
		}(session)
	}
	close(start)
	drives.Wait()
	close(failures)
	for failure := range failures {
		t.Fatalf("concurrent drive: %s", failure)
	}

	requests := provider.recorded()
	if len(requests) != sessions*(rounds+1) {
		t.Fatalf("recorded requests = %d, want three decisions per drive: the call, its settled answer, and the round that reads both", len(requests))
	}
	perSession := map[int]map[int]int{}
	for _, req := range requests {
		own, round, ok := sessionOf(req.Messages)
		if !ok {
			t.Fatalf("request of %d messages carries no session input", len(req.Messages))
		}
		if perSession[own] == nil {
			perSession[own] = map[int]int{}
		}
		perSession[own][round]++
	}
	for _, req := range requests {
		own, round, ok := sessionOf(req.Messages)
		if !ok {
			t.Fatalf("request of %d messages carries no session input", len(req.Messages))
		}
		markers := map[string]bool{}
		for _, msg := range req.Messages {
			for _, marker := range sessionMarkers.FindAllString(msg.Content, -1) {
				markers[marker] = true
			}
			for _, call := range msg.ToolCalls {
				for _, marker := range sessionMarkers.FindAllString(call.ID, -1) {
					markers[marker] = true
				}
				for _, marker := range sessionMarkers.FindAllString(string(call.Function.Arguments), -1) {
					markers[marker] = true
				}
			}
		}
		if len(markers) != 1 {
			t.Fatalf("request markers = %v, want the markers of one session only", markers)
		}
		var foreign []string
		for marker := range markers {
			if marker != fmt.Sprintf("sess-%d", own) {
				foreign = append(foreign, marker)
			}
		}
		if len(foreign) != 0 {
			t.Fatalf("session %d request carries the markers of %v, want no cross-session material", own, foreign)
		}
		inputs := sessionInputsOf(req.Messages)
		want := make([]string, 0, round)
		for at := 1; at <= round; at++ {
			want = append(want, sessionInputOf(own, at))
		}
		if strings.Join(inputs, "|") != strings.Join(want, "|") {
			t.Fatalf("session %d request inputs = %v, want its own rounds %v", own, inputs, want)
		}
	}
	for session := 0; session < sessions; session++ {
		if got := perSession[session]; got[1] != 2 || got[2] != 1 {
			t.Fatalf("session %d decisions per round = %v, want the call beside its settled answer and then the round that reads both", session, got)
		}
	}
	called := tool.markers()
	if len(called) != sessions {
		t.Fatalf("tool executions = %v, want one call per session carrying its own marker", called)
	}
	seen := map[string]bool{}
	for _, args := range called {
		found := sessionMarkers.FindAllString(args, -1)
		if len(found) != 1 {
			t.Fatalf("tool arguments = %q, want the marker of the session that called it", args)
		}
		if seen[found[0]] {
			t.Fatalf("tool arguments = %q, want a call identity of its own session rather than a repeated one", args)
		}
		seen[found[0]] = true
	}
	if len(seen) != sessions {
		t.Fatalf("sessions that called the tool = %v, want every session's own call", seen)
	}
}

// TestCrossSessionContentIsRefused checks that material of another session never enters a request, and that a chain naming no session is refused by name.
// - a replay chain that names no session is refused by name instead of joining every user of the partition
// - a session without a registered chain assembles none of another session's projected facts
// - the session that registered the chain does assemble it, so the exclusion above is a boundary and not an empty assembly
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCrossSessionContentIsRefused(t *testing.T) {
	ctx := context.Background()
	const alphaContent = "alpha-only fact: the private material of session alpha"
	alpha := replay.ChainKey{PartitionID: 1, SessionID: "session-alpha"}
	chain := replay.NewMemoryEventChain()
	if err := chain.Commit(ctx, replay.ChainFact{
		Key:     event.EventID("a-0001"),
		Chain:   alpha,
		Type:    event.TypeExternalInput,
		Summary: "alpha asks",
		Content: alphaContent,
		Time:    time.Now(),
	}); err != nil {
		t.Fatalf("commit the alpha fact: %v", err)
	}
	projection := replay.NewReplayProjection()
	if _, err := replay.RebuildProjectionFromWAL(ctx, projection, chain, replay.NewMemoryRelations(), alpha, replay.RebuildOptions{}); err != nil {
		t.Fatalf("rebuild the alpha chain: %v", err)
	}

	provider := &convModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return convFinalTurn("answered")
	}}
	run, err := tagent.Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	if err := run.AttachReplay(agent.RequestChain{PartitionID: 1}, 100, projection); !errors.Is(err, agent.ErrRequestChainSessionRequired) {
		t.Fatalf("replay registration without a session = %v, want %v", err, agent.ErrRequestChainSessionRequired)
	}
	if err := run.AttachReplay(agent.RequestChain{PartitionID: 1, SessionID: "session-beta"}, 100, nil); err == nil {
		t.Fatalf("replay registration without a projection = %v, want the named refusal", err)
	}
	if err := run.AttachReplay(agent.RequestChain{PartitionID: 1, SessionID: event.SessionID(alpha.SessionID)}, 100, projection); err != nil {
		t.Fatalf("register the alpha chain: %v", err)
	}

	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-session-beta",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-beta"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "beta asks its own question"}},
	}); err != nil {
		t.Fatalf("invoke session beta: %v", err)
	}
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-session-alpha",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-alpha"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "alpha asks its own question"}},
	}); err != nil {
		t.Fatalf("invoke session alpha: %v", err)
	}

	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want one decision per session", len(requests))
	}
	beta := requests[0].Messages
	if len(beta) != 1 || beta[0].Content != "beta asks its own question" {
		t.Fatalf("session beta request = %+v, want its own input alone", beta)
	}
	for _, msg := range beta {
		if strings.Contains(msg.Content, alphaContent) {
			t.Fatalf("session beta request carries the alpha chain content %q", msg.Content)
		}
	}
	assembled := requests[1].Messages
	alphaAt, inputAt := -1, -1
	for at, msg := range assembled {
		if msg.Content == alphaContent {
			alphaAt = at
		}
		if msg.Content == "alpha asks its own question" {
			inputAt = at
		}
	}
	if alphaAt < 0 || inputAt < 0 || alphaAt > inputAt {
		t.Fatalf("session alpha request = %+v, want its own chain assembled in front of its own input", assembled)
	}
}

// sessionInputsOf reports the rendered session inputs one request carries, in
// the order they stand in it.
func sessionInputsOf(messages []model.Message) []string {
	var inputs []string
	for _, msg := range messages {
		if msg.Role != model.RoleUser {
			continue
		}
		if _, _, ok := parseSessionInput(msg.Content); ok {
			inputs = append(inputs, msg.Content)
		}
	}
	return inputs
}

// hasSettledCall reports whether one request already carries the admission or
// the settled result of a call, so a decision is the later one of its drive.
func hasSettledCall(messages []model.Message) bool {
	for _, msg := range messages {
		if msg.ToolID != "" {
			return true
		}
	}
	return false
}

// scopeTool is the native tool stand-in of the concurrent drives: it records the
// marker it was called with and answers beside it, so a request carrying another
// session's tool result is visible as that marker.
type scopeTool struct {
	mu     sync.Mutex
	called []string
}

// Declaration returns the declaration naming the stand-in tool.
func (t *scopeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "echo"}
}

// Call records the marker of the call and answers beside it.
func (t *scopeTool) Call(_ context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.called = append(t.called, string(jsonArgs))
	return "echo:" + string(jsonArgs), nil
}

// markers reports the arguments of every recorded call, in call order.
func (t *scopeTool) markers() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.called...)
}
