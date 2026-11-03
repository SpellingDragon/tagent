// Package config exercises the agent declaration end to end: one declaration
// referencing preset tools assembles into a run that serves them.
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	orgconfig "github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	// presetKnowledgeText is the one line the declared knowledge source carries,
	// worded so the search query reaches it and no other text does.
	presetKnowledgeText = "预设知识库标记：发布窗口值守手册"
	// presetRecallText is the content of the seeded memory event the recall
	// query reaches.
	presetRecallText = "预设召回标记：迁移演练纪要"
	// presetRecallPartition is the partition the seeded event is committed
	// under and the recall query is authorized for.
	presetRecallPartition = "preset-recall"
	// presetRecallKey is the canonical identity of the seeded event.
	presetRecallKey = event.EventID("preset-recall-1")
)

// TestPresetToolsAssemble drives the preset references of one declaration through a real assembly.
// - the knowledge, recall and call_subagent references assemble into the three tool instances the model sees
// - the knowledge search answers the declared source through the tRPC knowledge tool
// - the recall answers through the run's own memory retrieval face over the committed store
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestPresetToolsAssemble(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	writePresetFile(t, filepath.Join(root, "prompts", "system.md"), "preset system prompt\n")
	docs := filepath.Join(root, "docs")
	writePresetFile(t, filepath.Join(docs, "runbook.md"), presetKnowledgeText+"\n")
	storeRoot := t.TempDir()
	seedPresetRecall(t, storeRoot)

	decl := &orgconfig.Declaration{
		Entry: "agent-preset",
		Agents: []orgconfig.AgentSpec{{
			ID:    "agent-preset",
			Model: "model-preset",
			Files: []orgconfig.FileRef{{Path: filepath.Join(root, "prompts", "system.md"), Kind: orgconfig.FileKindPrompt}},
			Tools: []orgconfig.ToolRef{
				{Name: "knowledge", Params: map[string]string{"source": docs, "max_results": "3"}},
				{Name: "recall"},
				{Name: "call_subagent"},
			},
		}},
	}

	var roundMu sync.Mutex
	round := 0
	provider := &presetModel{name: "model-preset", answer: func(req *model.Request) *model.Response {
		roundMu.Lock()
		round++
		current := round
		roundMu.Unlock()
		if current == 1 {
			return presetToolTurn(
				presetCall("call-knowledge", "knowledge_search", `{"query":"发布窗口值守"}`),
				presetCall("call-recall", "recall", `{"text":"预设召回标记","strategy":"keyword_only","partitions":["`+presetRecallPartition+`"]}`),
			)
		}
		return presetFinalTurn("preset tools settled")
	}}

	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-preset": {Model: provider},
	},
		assembly.WithGovernance(presetGovernance{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: storeRoot}),
	)
	if err != nil {
		t.Fatalf("assemble the preset declaration: %v", err)
	}
	t.Cleanup(func() {
		if _, err := run.Close(context.Background(), agent.CloseRequest{Timeout: time.Now().Add(200 * time.Millisecond)}); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	turn, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-preset-tools",
		Address:      event.Address{AgentID: "agent-preset", SessionID: "session-preset-tools"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "what do the presets answer"}},
	})
	if err != nil {
		t.Fatalf("invoke the preset tools: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, turn.DecisionCount)
	}

	requests := provider.recorded()
	if len(requests) < 2 {
		t.Fatalf("model requests = %d (%s), want the opening decision beside the one that reads its first settled result",
			len(requests), describePresetRequests(requests))
	}
	checkPresetDeclarations(t, &requests[1])
	if !presetResultReached(requests[1:2], "call-knowledge", "knowledge_search", presetKnowledgeText) &&
		!presetResultReached(requests[1:2], "call-recall", "recall", string(presetRecallKey)) {
		t.Fatalf("the request the first settled result drove reads no settled observation (%s), want the decision it drove to read that result",
			describePresetRequests(requests))
	}

	// The two calls of the opening decision settle independently: the knowledge
	// search answers inside the turn while the memory retrieval may land after the
	// closing decision and drive the next segment of the address, exactly as the
	// resident loop's fast-result law says. Either way the settled body of each
	// call must reach a model request under its own call identity.
	awaitPresetResults(t, provider, 10*time.Second)
}

// checkPresetDeclarations checks the three preset tool instances the first
// decision's request carries.
func checkPresetDeclarations(t *testing.T, req *model.Request) {
	t.Helper()
	cases := []struct {
		name        string
		description string
	}{
		{name: "knowledge_search", description: "knowledge base"},
		{name: "recall", description: "references"},
		{name: "call_subagent", description: "agents_file"},
	}
	for _, want := range cases {
		tool, ok := req.Tools[want.name]
		if !ok || tool == nil {
			t.Errorf("second request tools = %v, want the %q preset instance", namesOfTools(req.Tools), want.name)
			continue
		}
		declaration := tool.Declaration()
		if declaration == nil || declaration.Name != want.name || !strings.Contains(declaration.Description, want.description) {
			t.Errorf("preset %q declaration = %+v, want the assembled instance declaring %q", want.name, declaration, want.description)
		}
	}
}

// awaitPresetResults waits until both settled bodies have reached a model
// request under their own call identities. The knowledge search normally answers
// inside the turn; the memory retrieval may settle after the closing decision and
// drive the next segment of the address, so the wait spans the requests the
// address serves rather than one turn.
func awaitPresetResults(t *testing.T, provider *presetModel, wait time.Duration) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		requests := provider.recorded()
		knowledge := presetResultReached(requests, "call-knowledge", "knowledge_search", presetKnowledgeText)
		recall := presetResultReached(requests, "call-recall", "recall", string(presetRecallKey))
		if knowledge && recall {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("settled observations reached the model = (knowledge %t, recall %t) in %s (%s), want each settled body read under its own call identity",
				knowledge, recall, wait, describePresetRequests(requests))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// presetResultReached reports whether one settled body reached a model request
// under the call identity it was admitted for. The harvest path renders the
// settled result as user material carrying the body while the chain-restored
// history renders it under the tool role, so the check reads the call identity
// and the body instead of one rendering: an admission receipt answers "ok" and
// never carries the body.
func presetResultReached(requests []model.Request, callID, tool, marker string) bool {
	for _, req := range requests {
		for _, msg := range req.Messages {
			if msg.ToolID == callID && msg.ToolName == tool && strings.Contains(msg.Content, marker) {
				return true
			}
		}
	}
	return false
}

// describePresetRequests renders the model requests the address served, so a
// failure names what the model read instead of a count.
func describePresetRequests(requests []model.Request) string {
	lines := make([]string, 0, len(requests))
	for at, req := range requests {
		messages := make([]string, 0, len(req.Messages))
		for _, msg := range req.Messages {
			content := msg.Content
			if len(content) > 60 {
				content = content[:60] + "…"
			}
			messages = append(messages, fmt.Sprintf("%s/%s:%s", msg.Role, msg.ToolID, content))
		}
		lines = append(lines, fmt.Sprintf("#%d [%s]", at, strings.Join(messages, " | ")))
	}
	return strings.Join(lines, "; ")
}

// seedPresetRecall commits one event into a real storage tier, so the recall
// face of a later assembly reads a committed event rather than a fixture.
func seedPresetRecall(t *testing.T, root string) {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("OpenTier(localfile): %v", err)
	}
	fact := event.Event{
		Key:       presetRecallKey,
		Type:      event.TypeExternalInput,
		Timestamp: time.UnixMilli(1000),
		Message:   model.Message{Role: model.RoleUser, Content: presetRecallText},
		Owner:     event.Address{AgentID: "agent-preset", SessionID: "session-preset"},
	}
	body, err := memory.BodyFromEvent(fact, "text/plain", "keep")
	if err != nil {
		t.Fatalf("BodyFromEvent(%s): %v", presetRecallKey, err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("Encode(%s): %v", presetRecallKey, err)
	}
	if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
		PartitionID: memory.PartitionIDFromName(presetRecallPartition),
		WindowTS:    1000,
		Seq:         0,
		EventKey:    presetRecallKey,
		EventTime:   1000,
		Body:        encoded,
	}); err != nil {
		t.Fatalf("StoreEvent(%s): %v", presetRecallKey, err)
	}
	if err := tier.Originals.Put(ctx, body.Ref(), []byte(presetRecallText)); err != nil {
		t.Fatalf("Put(original %s): %v", presetRecallKey, err)
	}
}

// writePresetFile writes one file under the declaration root.
func writePresetFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// namesOfTools reports the declared names of one request's tools, so a failure
// names what the request carried instead of a map address.
func namesOfTools(tools map[string]trpctool.Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	return names
}

// presetModel is the scripted provider of the preset test: it records every
// request and answers the script of the round it is in.
type presetModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the request and streams the scripted response.
func (m *presetModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted provider's descriptor.
func (m *presetModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the scripted provider served.
func (m *presetModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// presetCall builds one native tool call of a scripted assistant message.
func presetCall(id, name, args string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      name,
			Arguments: []byte(args),
		},
	}
}

// presetToolTurn builds the scripted response carrying native tool calls.
func presetToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Done:    true,
	}
}

// presetFinalTurn builds the scripted terminal response carrying the final
// answer.
func presetFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

// presetGovernance is the governance stand-in of the preset test: every
// required mechanism check answers with one passing disposition.
type presetGovernance struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (presetGovernance) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	answer := func(name state.CheckName) state.CheckAnswer {
		return state.CheckAnswer{
			Name:     name,
			Verdict:  state.CheckPass,
			Reason:   "the preset stand-in passes the mechanism check",
			Scope:    "assembly",
			Evidence: []string{"deterministic stand-in"},
		}
	}
	return map[state.CheckName]state.CheckAnswer{
		"eval":       answer("eval"),
		"benchmark":  answer("benchmark"),
		"apply-gate": answer("apply-gate"),
	}, nil
}
