package assembly

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestRequestSnapshotsSingleSource pins the one home of the safe request snapshots: the capture fixes each request once and every read answers from it.
//   - the read face answers exactly the snapshots the capture holds, in fix order, each beside its decision
//   - the content read back is the captured snapshot's own content, byte for byte
//   - call arguments come from the same captured transcript; a call it never carried answers the empty object
//   - a second read answers the same snapshots, and a run beside no capture answers none
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestRequestSnapshotsSingleSource(t *testing.T) {
	t.Run("the read face answers the capture's own snapshots", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		capture := snapshotTestCapture(t)
		run := snapshotTestRun(t, ctx, WithCapture(capture))
		turn := snapshotTestInvoke(t, ctx, run)
		if turn.DecisionCount != 2 {
			t.Fatalf("decisions = %d, want the tool call and the closing decision of one turn", turn.DecisionCount)
		}
		records := run.RequestSnapshots()
		refs := capture.SnapshotRefs()
		if len(records) != len(refs) {
			t.Fatalf("read face = %d snapshots, want the %d the capture holds", len(records), len(refs))
		}
		if len(records) != turn.DecisionCount {
			t.Fatalf("read face = %d snapshots, want one per decision beside the %d decisions", len(records), turn.DecisionCount)
		}
		fixed := capture.Snapshots()
		if len(fixed) != len(records) {
			t.Fatalf("capture account = %d snapshots, want the %d the read face answers", len(fixed), len(records))
		}
		for i := range records {
			if records[i].Decision != fixed[i].Decision {
				t.Fatalf("snapshot %d decision = %s, want the decision whose request it was fixed for %s",
					i, records[i].Decision, fixed[i].Decision)
			}
			if fixed[i].Decision != records[i].Decision {
				t.Fatalf("capture snapshot %d decision = %s, want the %s the read face answers",
					i, fixed[i].Decision, records[i].Decision)
			}
			held, err := capture.SnapshotOf(refs[i])
			if err != nil {
				t.Fatalf("read capture snapshot %d back: %v", i, err)
			}
			want, err := rl.MarshalSnapshot(held)
			if err != nil {
				t.Fatalf("marshal capture snapshot %d: %v", i, err)
			}
			got, err := rl.MarshalSnapshot(records[i].Snapshot)
			if err != nil {
				t.Fatalf("marshal read-back snapshot %d: %v", i, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("read-back snapshot %d = %s, want the capture's own content %s", i, got, want)
			}
		}
		if again := run.RequestSnapshots(); len(again) != len(records) {
			t.Fatalf("second read = %d snapshots, want the %d the first read answered", len(again), len(records))
		}
	})

	t.Run("call arguments are answered from the captured transcript", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		capture := snapshotTestCapture(t)
		run := snapshotTestRun(t, ctx, WithCapture(capture))
		snapshotTestInvoke(t, ctx, run)
		const issued = `{"path":"greet"}`
		if got := run.callArgumentsOf("call-1"); got != issued {
			t.Fatalf("call arguments = %q, want the %q the request was issued with", got, issued)
		}
		if got := run.callArgumentsOf("call-absent"); got != "{}" {
			t.Fatalf("arguments of a call no snapshot carried = %q, want the empty argument object", got)
		}
		carried := ""
		for _, one := range capture.Snapshots() {
			for _, message := range one.Snapshot.Messages {
				for _, call := range message.ToolCalls {
					if call.ID == "call-1" {
						carried = string(call.Function.Arguments)
					}
				}
			}
		}
		if carried != issued {
			t.Fatalf("captured transcript arguments = %q, want the %q the read answered", carried, issued)
		}
	})

	t.Run("a run beside no capture answers an empty read face", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run := snapshotTestRun(t, ctx)
		snapshotTestInvoke(t, ctx, run)
		if records := run.RequestSnapshots(); len(records) != 0 {
			t.Fatalf("read face = %d snapshots beside no capture, want none fixed", len(records))
		}
		if got := run.callArgumentsOf("call-1"); got != "{}" {
			t.Fatalf("call arguments beside no capture = %q, want the empty argument object", got)
		}
	})
}

// TestProjectionRendersTheStatedMessageRole pins the one message-role rendering rule both assembly paths share.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestProjectionRendersTheStatedMessageRole(t *testing.T) {
	projection := replay.NewReplayProjection()
	projection.Append(context.Background(), replay.WithMessageRole(replay.ProjectionEntry{
		Key:     "1-instruction",
		Type:    event.TypeExternalInput,
		Content: "review the change",
	}, string(model.RoleSystem)))
	projection.Append(context.Background(), replay.ProjectionEntry{
		Key:     "2-task",
		Type:    event.TypeExternalInput,
		Content: "the change under review",
	})
	messages, sources, err := projectedSegment(context.Background(), nil, projection, FoldWindow{}, 2, 0)
	if err != nil {
		t.Fatalf("project the entries: %v", err)
	}
	if len(sources) != len(messages) {
		t.Fatalf("projected sources = %d, want one per message (%d)", len(sources), len(messages))
	}
	if len(messages) != 2 {
		t.Fatalf("projected messages = %d, want the two entries", len(messages))
	}
	if messages[0].Role != model.RoleSystem {
		t.Fatalf("instruction role = %q, want the role its own message stated", messages[0].Role)
	}
	if messages[1].Role != model.RoleUser {
		t.Fatalf("task role = %q, want the frozen type's role beside a message stating none", messages[1].Role)
	}
}

// TestDelegatedAssemblyReadsItsInstructionThroughTheFileFact pins the preset read of a delegated assembly through the file change fact.
//
// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
func TestDelegatedAssemblyReadsItsInstructionThroughTheFileFact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const (
		bootBody = "PRESET-FACT-BOOT-INSTRUCTION"
		nextBody = "PRESET-FACT-NEXT-INSTRUCTION"
	)
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "system.md")
	writePresetPrompt(t, promptPath, bootBody+"\n")

	spec := config.AgentSpec{
		ID:    "agent-a",
		Model: "model-a",
		Files: []config.FileRef{{Path: promptPath, Kind: config.FileKindPrompt}},
		Tools: []config.ToolRef{{Name: subagent.CallSubagentRef}},
	}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{spec}}
	run, err := Assemble(ctx, decl, map[event.AgentID]NativeAgent{
		"agent-a": {Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
				Done:    true,
			}
		}}},
	}, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	filed := presetFileFact(t, ctx, run, promptPath, bootBody+"\n")
	if filed.Type != event.TypeConfigChange {
		t.Fatalf("preset file fact type = %q, want the configuration change family", filed.Type)
	}
	if got := filed.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionFile {
		t.Fatalf("preset file fact dimension = %q, want the file dimension", got)
	}
	if filed.Owner.AgentID != "agent-a" {
		t.Fatalf("preset file fact owner = %q, want the agent whose prompt files carry the instruction", filed.Owner.AgentID)
	}

	assembly, err := run.presetAssembly(ctx, "agent-a", spec, nil)
	if err != nil {
		t.Fatalf("resolve the delegated assembly: %v", err)
	}
	if assembly.Instruction != bootBody {
		t.Fatalf("delegated instruction = %q, want the %q the file fact carries", assembly.Instruction, bootBody)
	}
	if after := run.CommittedFacts(); after != 1 {
		t.Fatalf("committed facts = %d after re-reading the same content, want the one fact the content names", after)
	}

	writePresetPrompt(t, promptPath, nextBody+"\n")
	assembly, err = run.presetAssembly(ctx, "agent-a", spec, nil)
	if err != nil {
		t.Fatalf("resolve the delegated assembly of the rewritten prompt: %v", err)
	}
	if assembly.Instruction != nextBody {
		t.Fatalf("delegated instruction beside the rewrite = %q, want the rewritten body %q", assembly.Instruction, nextBody)
	}
	if after := run.CommittedFacts(); after != 2 {
		t.Fatalf("committed facts = %d after the rewrite, want one fact per content version the assembly read", after)
	}
	rewritten := presetFileFact(t, ctx, run, promptPath, nextBody+"\n")
	if body := rewritten.Message.Content; !strings.Contains(body, nextBody) {
		t.Fatalf("rewritten preset file fact body = %q, want the rewritten body", body)
	}
	if earlier, err := run.CommittedEvent(ctx, presetFileFactID(promptPath, bootBody+"\n")); err != nil {
		t.Fatalf("read the earlier preset file fact back: %v", err)
	} else if earlier.Message.Content != filed.Message.Content {
		t.Fatalf("earlier preset file fact body = %q, want the content it filed, %q", earlier.Message.Content, filed.Message.Content)
	}
}

// writePresetPrompt writes one prompt body under the given path.
func writePresetPrompt(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the prompt directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the prompt file %s: %v", path, err)
	}
}

// presetFileFact reports the file change fact one preset read of the prompt files
// files: the identity is the one the boundary derives for the content, so a check
// reads the committed fact by it instead of scanning the chain.
func presetFileFact(t *testing.T, ctx context.Context, run *Run, path, content string) event.Event {
	t.Helper()
	fact, err := run.CommittedEvent(ctx, presetFileFactID(path, content))
	if err != nil {
		t.Fatalf("read the preset file fact of %s: %v", path, err)
	}
	return fact
}

// presetFileFactID derives the identity one preset read files for the content:
// the file reference the boundary reads the content under beside the content
// itself, exactly as the request boundary derives the file dimension identity.
func presetFileFactID(path, content string) event.EventID {
	ref := config.FileRef{Path: filepath.Base(filepath.FromSlash(path)), Kind: config.FileKindPrompt}
	return event.ConfigChangeFor("agent-a", event.ConfigChangeDimensionFile, fileFactGeneration,
		fileFactHeader(ref, content)+"\n"+content).ID
}

// snapshotTestCapture builds the trajectory capture one assembled run fixes its
// request views through: a real file writer under a temporary directory and one
// bounded sink over it, so the read holds what a production capture would.
func snapshotTestCapture(t *testing.T) *rl.TrajectoryCapture {
	t.Helper()
	writer, err := rl.NewFileCaptureWriter(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("run-request-views", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCapture("run-request-views", sink, rl.SnapshotPolicy{})
	if err != nil {
		t.Fatalf("open the trajectory capture: %v", err)
	}
	return capture
}

// snapshotTestRun assembles the runtime over one scripted provider whose first
// decision calls a tool and whose second decision settles the turn, with the
// options the caller attaches: the extra option is how a test attaches the
// capture whose account the read face answers from, or leaves it beside none.
func snapshotTestRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	return snapshotTestRunWithTrim(t, ctx, true, opts...)
}

// snapshotTestRunWithTrim assembles the run and controls its post-seed trim.
func snapshotTestRunWithTrim(t *testing.T, ctx context.Context, trim bool, opts ...Option) *Run {
	t.Helper()
	round := 0
	var mu sync.Mutex
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		mu.Lock()
		defer mu.Unlock()
		round++
		if round == 1 {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{{
						ID:   "call-1",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name:      "echo-a",
							Arguments: []byte(`{"path":"greet"}`),
						},
					}},
				}}},
				Done: true,
			}
		}
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
			Done:    true,
		}
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	natives := map[event.AgentID]NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&snapshotTestTool{name: "echo-a", output: "out-a"}}},
	}
	options := append([]Option{WithGovernance(snapshotTestGovernance{})}, opts...)
	run, err := assemble(ctx, decl, natives, trim, options...)
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// snapshotTestInvoke drives one invocation through the formal entry of the
// assembled run and returns the turn it served.
func snapshotTestInvoke(t *testing.T, ctx context.Context, run *Run) agent.Turn {
	t.Helper()
	turn, err := run.Invoke(ctx, InvokeRequest{
		InvocationID: "inv-request-views",
		Episode:      "ep-request-views",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-request-views"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "drive the tool"}},
	})
	if err != nil {
		t.Fatalf("invoke the assembled run: %v", err)
	}
	return turn
}

// snapshotTestModel is the scripted provider stand-in: it records the native
// request it was driven with and streams the scripted answer.
type snapshotTestModel struct {
	mu       sync.Mutex
	answer   func(*model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the native request and streams the scripted answer.
func (m *snapshotTestModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
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
func (m *snapshotTestModel) Info() model.Info {
	return model.Info{Name: "model-a"}
}

// snapshotTestTool is the native tool stand-in: it executes once per call and
// answers with its declared output.
type snapshotTestTool struct {
	name   string
	output string
}

// Declaration returns the declaration naming the stand-in tool.
func (t *snapshotTestTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the stand-in tool once and reports its declared output.
func (t *snapshotTestTool) Call(context.Context, []byte) (any, error) {
	return t.output, nil
}

// snapshotTestGovernance is the passing governance stand-in: the publication
// checks of this assembly answer pass at every required name, so the test reads
// the snapshot account rather than a governance ruling.
type snapshotTestGovernance struct{}

// Check answers every required check of one candidate plan with a pass.
func (snapshotTestGovernance) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       {Name: "eval", Verdict: state.CheckPass},
		"benchmark":  {Name: "benchmark", Verdict: state.CheckPass},
		"apply-gate": {Name: "apply-gate", Verdict: state.CheckPass},
	}, nil
}

// budgetTestAgent is the one agent every request-budget test assembles.
const budgetTestAgent = event.AgentID("agent-budget")

// budgetTestDrive is one driven turn beside the provider that served it.
type budgetTestDrive struct {
	run      *Run
	provider *snapshotTestModel
	turn     agent.Turn
	err      error
}

// budgetTestRun assembles one agent over the scripted provider, writes the
// declared prompt body into a prompt file when one is named, and drives one
// invocation at the test session.
func budgetTestRun(t *testing.T, ctx context.Context, spec config.AgentSpec, promptBody, input, session string, opts ...Option) *budgetTestDrive {
	t.Helper()
	if promptBody != "" {
		spec.Files = append(spec.Files, budgetTestFileRef(t, "system.md", promptBody, config.FileKindPrompt))
	}
	return budgetTestDriveSpec(t, ctx, spec, input, session, nil, opts...)
}

// budgetTestFileRef writes one declared file and answers its reference.
func budgetTestFileRef(t *testing.T, name, body string, kind config.FileKind) config.FileRef {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the budget file %s: %v", name, err)
	}
	return config.FileRef{Path: path, Kind: kind}
}

// budgetTestSkillFile writes one skill body as the skill framework indexes it:
// a folder named for the skill beside its SKILL.md, so the request boundary's
// skill repository resolves the declared reference by name.
func budgetTestSkillFile(t *testing.T, name, body string) config.FileRef {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the budget skill %s: %v", name, err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the budget skill %s: %v", name, err)
	}
	return config.FileRef{Path: path, Kind: config.FileKindSkill}
}

// budgetTestDriveSpec assembles one declared agent and drives one invocation,
// running the preparation over the assembled run before the drive starts. The
// preparation is how a test attaches a projection the boundary must present.
func budgetTestDriveSpec(t *testing.T, ctx context.Context, spec config.AgentSpec, input, session string, prepare func(*Run), opts ...Option) *budgetTestDrive {
	t.Helper()
	if spec.ID == "" {
		spec.ID = string(budgetTestAgent)
	}
	if spec.Model == "" {
		spec.Model = "model-budget"
	}
	provider := &snapshotTestModel{answer: boundaryAnchorAnswer("the budget test answer")}
	decl := &config.Declaration{Entry: spec.ID, Agents: []config.AgentSpec{spec}}
	options := append([]Option{WithGovernance(snapshotTestGovernance{})}, opts...)
	run, err := Assemble(ctx, decl, map[event.AgentID]NativeAgent{
		event.AgentID(spec.ID): {Model: provider},
	}, options...)
	if err != nil {
		t.Fatalf("assemble the budget runtime: %v", err)
	}
	if prepare != nil {
		prepare(run)
	}
	drive := &budgetTestDrive{run: run, provider: provider}
	drive.turn, drive.err = run.Invoke(ctx, InvokeRequest{
		InvocationID: event.InvocationID("inv-" + session),
		Address:      event.Address{AgentID: event.AgentID(spec.ID), SessionID: event.SessionID(session)},
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
	return drive
}

// budgetTestAttach registers one projection under the drive's session chain.
func budgetTestAttach(t *testing.T, run *Run, session string, window FoldWindow, projection *replay.ReplayProjection) {
	t.Helper()
	if err := run.AttachReplay(agent.RequestChain{SessionID: event.SessionID(session)}, window, projection); err != nil {
		t.Fatalf("attach the budget projection: %v", err)
	}
}

// budgetTestProjection answers a projection holding the given entries in order.
func budgetTestProjection(ctx context.Context, contents ...string) *replay.ReplayProjection {
	projection := replay.NewReplayProjection()
	for i, content := range contents {
		projection.Append(ctx, replay.ProjectionEntry{
			Key:     event.EventID(fmt.Sprintf("budget-entry-%d", i+1)),
			Type:    event.TypeExternalInput,
			Content: content,
		})
	}
	return projection
}

// requests reports the requests the provider served, in order.
func (d *budgetTestDrive) requests() []model.Request {
	d.provider.mu.Lock()
	defer d.provider.mu.Unlock()
	return append([]model.Request(nil), d.provider.requests...)
}

// oneRequest answers the one request the provider served.
func (d *budgetTestDrive) oneRequest(t *testing.T, label string) model.Request {
	t.Helper()
	requests := d.requests()
	if len(requests) != 1 {
		t.Fatalf("%s: provider requests = %d, want exactly one (failure=%q)", label, len(requests), d.failureText())
	}
	return requests[0]
}

// failureText joins the invocation error and the turn failure of a drive.
func (d *budgetTestDrive) failureText() string {
	return strings.TrimSpace(strings.Join(nonEmptyStrings(errText(d.err), d.turn.Failure), " "))
}

// budgetTestTotal measures one wire request the way the final assembly does.
func budgetTestTotal(request model.Request) int {
	_, _, total := agent.AccountRequestBudget(request.Messages, nil, request.Tools, "")
	return total
}

// budgetTestRefusal asserts one drive issued no request and reported a named
// budget refusal, and answers the cause it reported.
func budgetTestRefusal(t *testing.T, drive *budgetTestDrive, label string) string {
	t.Helper()
	if requests := drive.requests(); len(requests) != 0 {
		t.Fatalf("%s: provider requests = %d, want 0 before a refusal", label, len(requests))
	}
	if drive.turn.Terminal != model.KindFailed {
		t.Fatalf("%s: terminal = %v, want failed", label, drive.turn.Terminal)
	}
	cause := drive.failureText()
	if !boundaryAnchorNamesBudgetRefusal(cause) {
		t.Fatalf("%s: refusal = %q, want a cause naming the budget boundary", label, cause)
	}
	return cause
}

// budgetTestPromptBody renders one prompt body of the named size behind a
// unique marker the request assertions search for.
func budgetTestPromptBody(marker string, size int) string {
	line := marker + " "
	return strings.Repeat(line, size/len(line)+1)[:size]
}

// budgetTestTool is a declared tool stand-in with no execution reach: the
// request budget reads its declaration, never its call face.
type budgetTestTool struct {
	declaration *trpctool.Declaration
}

// Declaration returns the declaration the meter reads.
func (t budgetTestTool) Declaration() *trpctool.Declaration { return t.declaration }

// TestRequestBudgetAccountsAllFaces pins the unified nine-face meter.
//
// - each of the nine input faces is injected once and the total equals the sum of the faces
// - a large schema and a long tool-call argument land in the tool-declarations face
// - an unknown media part is named and counted nowhere, never read as zero
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestRequestBudgetAccountsAllFaces(t *testing.T) {
	text := "a text content part"
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "fixed prompt material"},
		{Role: model.RoleSystem, Content: "skill body material"},
		{Role: model.RoleSystem, Content: "environment material"},
		{Role: model.RoleUser, Content: "history material"},
		{Role: model.RoleUser, Content: "recovery hint material"},
		{Role: model.RoleAssistant, Content: "answer", ReasoningContent: "reasoning material"},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{{Type: "text", Text: &text}}},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
			ID: "call-1",
			Function: model.FunctionDefinitionParam{
				Name:      "echo",
				Arguments: []byte(`{"query":"a long argument that the tool-call face must carry"}`),
			},
		}}},
	}
	sources := []string{
		agent.BudgetFaceFixedPrompt,
		agent.BudgetFaceSkillBodies,
		agent.BudgetFaceEnvironment,
		agent.BudgetFaceHistory,
		agent.BudgetFaceRecoveryHint,
		agent.BudgetFaceHistory,
		agent.BudgetFaceHistory,
		agent.BudgetFaceHistory,
	}
	tools := map[string]trpctool.Tool{
		"echo": budgetTestTool{declaration: &trpctool.Declaration{
			Name:        "echo",
			Description: "echo one query",
			InputSchema: &trpctool.Schema{
				Type: "object",
				Properties: map[string]*trpctool.Schema{
					"query": {Type: "string", Description: "the query to echo"},
				},
			},
		}},
	}
	taskBoard := "task board material"

	measure := func(msgs []model.Message, srcs []string, tl map[string]trpctool.Tool, board string) (map[string]int, []string, int) {
		return agent.AccountRequestBudget(msgs, srcs, tl, board)
	}
	sum := func(faces map[string]int) int {
		total := 0
		for _, tokens := range faces {
			total += tokens
		}
		return total
	}
	faces, unknown, total := measure(messages, sources, tools, taskBoard)
	if len(faces) != 9 {
		t.Fatalf("faces = %d, want the nine input faces", len(faces))
	}
	if total != sum(faces) {
		t.Fatalf("total = %d, want the sum of the faces (%d)", total, sum(faces))
	}
	for _, face := range []string{
		agent.BudgetFaceFixedPrompt, agent.BudgetFaceSkillBodies, agent.BudgetFaceEnvironment,
		agent.BudgetFaceHistory, agent.BudgetFaceToolDeclarations, agent.BudgetFaceReasoning,
		agent.BudgetFaceContentParts, agent.BudgetFaceTaskBoard, agent.BudgetFaceRecoveryHint,
	} {
		if faces[face] <= 0 {
			t.Fatalf("face %q = %d, want a positive bucket (faces=%v)", face, faces[face], faces)
		}
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none for the measured base", unknown)
	}

	for _, face := range []string{
		agent.BudgetFaceFixedPrompt, agent.BudgetFaceSkillBodies, agent.BudgetFaceEnvironment,
		agent.BudgetFaceHistory, agent.BudgetFaceRecoveryHint,
	} {
		face := face
		msgs := append(append([]model.Message(nil), messages...), model.Message{Role: model.RoleSystem, Content: "increment of " + face})
		srcs := append(append([]string(nil), sources...), face)
		nextFaces, _, nextTotal := measure(msgs, srcs, tools, taskBoard)
		if nextFaces[face] <= faces[face] || nextTotal <= total {
			t.Fatalf("face %q increment: face %d -> %d, total %d -> %d, want both to grow", face, faces[face], nextFaces[face], total, nextTotal)
		}
		if nextTotal-total != nextFaces[face]-faces[face] {
			t.Fatalf("face %q increment: total delta %d, face delta %d, want the plain message's whole delta in its face", face, nextTotal-total, nextFaces[face]-faces[face])
		}
		if sum(nextFaces) != nextTotal {
			t.Fatalf("face %q increment: total %d != sum %d", face, nextTotal, sum(nextFaces))
		}
	}

	reasoning := append(append([]model.Message(nil), messages...), model.Message{
		Role: model.RoleUser, Content: "plain", ReasoningContent: "reasoning increment",
	})
	reasoningSources := append(append([]string(nil), sources...), agent.BudgetFaceHistory)
	reasoningFaces, _, reasoningTotal := measure(reasoning, reasoningSources, tools, taskBoard)
	if reasoningFaces[agent.BudgetFaceReasoning] <= faces[agent.BudgetFaceReasoning] {
		t.Fatalf("reasoning face = %d, want more than %d", reasoningFaces[agent.BudgetFaceReasoning], faces[agent.BudgetFaceReasoning])
	}
	if reasoningTotal-total != (reasoningFaces[agent.BudgetFaceReasoning]-faces[agent.BudgetFaceReasoning])+(reasoningFaces[agent.BudgetFaceHistory]-faces[agent.BudgetFaceHistory]) {
		t.Fatalf("reasoning increment: total delta %d, reasoning delta %d, history delta %d, want the sum", reasoningTotal-total, reasoningFaces[agent.BudgetFaceReasoning]-faces[agent.BudgetFaceReasoning], reasoningFaces[agent.BudgetFaceHistory]-faces[agent.BudgetFaceHistory])
	}

	partsText := "a content part increment"
	parts := append(append([]model.Message(nil), messages...), model.Message{
		Role: model.RoleUser, ContentParts: []model.ContentPart{{Type: "text", Text: &partsText}},
	})
	partsSources := append(append([]string(nil), sources...), agent.BudgetFaceHistory)
	partsFaces, _, partsTotal := measure(parts, partsSources, tools, taskBoard)
	if partsFaces[agent.BudgetFaceContentParts] <= faces[agent.BudgetFaceContentParts] {
		t.Fatalf("content-parts face = %d, want more than %d", partsFaces[agent.BudgetFaceContentParts], faces[agent.BudgetFaceContentParts])
	}
	if partsTotal-total != (partsFaces[agent.BudgetFaceContentParts]-faces[agent.BudgetFaceContentParts])+(partsFaces[agent.BudgetFaceHistory]-faces[agent.BudgetFaceHistory]) {
		t.Fatalf("content-parts increment: total delta %d, parts delta %d, history delta %d, want the sum", partsTotal-total, partsFaces[agent.BudgetFaceContentParts]-faces[agent.BudgetFaceContentParts], partsFaces[agent.BudgetFaceHistory]-faces[agent.BudgetFaceHistory])
	}

	call := append(append([]model.Message(nil), messages...), model.Message{
		Role: model.RoleAssistant,
		ToolCalls: []model.ToolCall{{
			ID: "call-2",
			Function: model.FunctionDefinitionParam{
				Name:      "echo",
				Arguments: []byte(strings.Repeat("x", 512)),
			},
		}},
	})
	callSources := append(append([]string(nil), sources...), agent.BudgetFaceHistory)
	callFaces, _, callTotal := measure(call, callSources, tools, taskBoard)
	if callFaces[agent.BudgetFaceToolDeclarations] <= faces[agent.BudgetFaceToolDeclarations] {
		t.Fatalf("tool-declarations face = %d, want the long call argument to grow it beyond %d", callFaces[agent.BudgetFaceToolDeclarations], faces[agent.BudgetFaceToolDeclarations])
	}
	if callTotal-total != (callFaces[agent.BudgetFaceToolDeclarations]-faces[agent.BudgetFaceToolDeclarations])+(callFaces[agent.BudgetFaceHistory]-faces[agent.BudgetFaceHistory]) {
		t.Fatalf("tool-call increment: total delta %d, declarations delta %d, history delta %d, want the sum", callTotal-total, callFaces[agent.BudgetFaceToolDeclarations]-faces[agent.BudgetFaceToolDeclarations], callFaces[agent.BudgetFaceHistory]-faces[agent.BudgetFaceHistory])
	}

	moreTools := map[string]trpctool.Tool{
		"echo": tools["echo"],
		"read": budgetTestTool{declaration: &trpctool.Declaration{
			Name:        "read",
			Description: "read one path",
			InputSchema: &trpctool.Schema{Type: "object", Properties: map[string]*trpctool.Schema{"path": {Type: "string"}}},
		}},
	}
	toolFaces, _, toolTotal := measure(messages, sources, moreTools, taskBoard)
	if toolFaces[agent.BudgetFaceToolDeclarations] <= faces[agent.BudgetFaceToolDeclarations] || toolTotal-total != toolFaces[agent.BudgetFaceToolDeclarations]-faces[agent.BudgetFaceToolDeclarations] {
		t.Fatalf("tool declaration increment: face %d -> %d, total %d -> %d, want the whole delta in tool_declarations", faces[agent.BudgetFaceToolDeclarations], toolFaces[agent.BudgetFaceToolDeclarations], total, toolTotal)
	}
	boardFaces, _, boardTotal := measure(messages, sources, tools, taskBoard+" and more")
	if boardFaces[agent.BudgetFaceTaskBoard] <= faces[agent.BudgetFaceTaskBoard] || boardTotal-total != boardFaces[agent.BudgetFaceTaskBoard]-faces[agent.BudgetFaceTaskBoard] {
		t.Fatalf("task-board increment: face %d -> %d, total %d -> %d, want the whole delta in task_board", faces[agent.BudgetFaceTaskBoard], boardFaces[agent.BudgetFaceTaskBoard], total, boardTotal)
	}

	image := append(append([]model.Message(nil), messages...), model.Message{
		Role: model.RoleUser, ContentParts: []model.ContentPart{{Type: "image"}},
	})
	imageSources := append(append([]string(nil), sources...), agent.BudgetFaceHistory)
	imageFaces, imageUnknown, imageTotal := measure(image, imageSources, tools, taskBoard)
	if len(imageUnknown) != 1 || !strings.Contains(imageUnknown[0], "image") {
		t.Fatalf("unknown = %v, want the one image part named", imageUnknown)
	}
	if imageFaces[agent.BudgetFaceContentParts] != faces[agent.BudgetFaceContentParts] {
		t.Fatalf("unknown image moved the content-parts face %d -> %d, want it uncounted", faces[agent.BudgetFaceContentParts], imageFaces[agent.BudgetFaceContentParts])
	}
	if imageTotal <= total {
		t.Fatalf("unknown image total = %d, want the message framing still counted over %d", imageTotal, total)
	}
}

// TestProviderWindowGateSemantics pins the one gate predicate and the declarations that decide it.
//
// - no context window: the gate is closed and the whole request is sent
// - an explicit context window: the gate is open and the oversized request is refused
// - a replay window alone opens no gate: it drives folding, not the provider's capacity
// - the context window alone is the provider window; the replay window never stands in for it
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestProviderWindowGateSemantics(t *testing.T) {
	ctx := context.Background()
	big := budgetTestPromptBody("the fixed prompt line of the provider-window gate", 10<<10)
	small := budgetTestPromptBody("the short prompt line of the provider-window gate", 1<<10)

	closed := budgetTestRun(t, ctx, config.AgentSpec{}, big, "gate closed input", "session-budget-gate-closed")
	closedRequest := closed.oneRequest(t, "gate closed")
	if len(closedRequest.Messages) == 0 {
		t.Fatal("gate-closed request carries no message, want the whole fixed prompt")
	}
	if closedRequest.Messages[0].Content != big {
		t.Fatalf("gate-closed first message = %q, want the whole fixed prompt", boundaryAnchorPreview(closedRequest.Messages[0].Content, 120))
	}
	if closed.turn.Terminal != model.KindCompleted {
		t.Fatalf("gate-closed turn terminal = %v, want completed", closed.turn.Terminal)
	}

	explicit := budgetTestRun(t, ctx, config.AgentSpec{ContextWindow: 64}, big, "explicit window input", "session-budget-gate-explicit")
	budgetTestRefusal(t, explicit, "explicit window")

	replayAlone := budgetTestRun(t, ctx, config.AgentSpec{ReplayWindow: 64}, big, "replay-window-only input", "session-budget-gate-replay-alone")
	replayAloneRequest := replayAlone.oneRequest(t, "replay window alone")
	if len(replayAloneRequest.Messages) == 0 {
		t.Fatal("replay-window-only request carries no message, want the whole prompt served with no gate")
	}
	if replayAloneRequest.Messages[0].Content != big {
		t.Fatalf("replay-window-only first message = %q, want the whole fixed prompt", boundaryAnchorPreview(replayAloneRequest.Messages[0].Content, 120))
	}

	replayOnly := budgetTestRun(t, ctx, config.AgentSpec{ReplayWindow: 32, ContextWindow: 4096, MaxTokens: 1}, small, "replay window input", "session-budget-gate-replay")
	replayRequest := replayOnly.oneRequest(t, "replay window")
	if len(replayRequest.Messages) == 0 {
		t.Fatal("replay-window request carries no message, want the whole fixed prompt")
	}
	if replayRequest.Messages[0].Content != small {
		t.Fatalf("replay-window first message = %q, want the whole fixed prompt", boundaryAnchorPreview(replayRequest.Messages[0].Content, 120))
	}

	narrow := budgetTestRun(t, ctx, config.AgentSpec{ReplayWindow: 4096, ContextWindow: 32, MaxTokens: 1}, small, "explicit small window input", "session-budget-gate-narrow")
	budgetTestRefusal(t, narrow, "explicit small window")
}

// TestSymptomAnchorPhantomReserveRefusesFittingRequest pins the request a window without a declared output limit serves.
//   - a request that fits the declared physical window is served, whatever the provider window
//   - the output reserve is the declared output limit, or zero when none is declared, so no reserve stands in for an undeclared one
//   - the fixed prompt is sized at 385600 bytes, which is 96400 tokens at the four-chars-per-token ratio
//   - that ratio puts the request over the 96000 a quarter-window reserve subtracts and under the 128000 physical window
//   - the served total is calibrated with the gate closed, so the band assertion pins the numbers rather than trusting the byte size alone
//   - the declaration names a physical window and a replay window and no output reserve, and the fitting request must be served rather than refused
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestSymptomAnchorPhantomReserveRefusesFittingRequest(t *testing.T) {
	ctx := context.Background()
	prompt := budgetTestPromptBody("the c75 fixed prompt line that fits the physical window", 385600)
	const physicalWindow = 128000

	base := budgetTestRun(t, ctx, config.AgentSpec{}, prompt, "c75 symptom input", "session-c75-calibrate")
	total := budgetTestTotal(base.oneRequest(t, "c75 calibration"))
	if total <= 96000 || total >= physicalWindow {
		t.Fatalf("calibrated total = %d tokens, want the c75 band (96000, %d): the anchor must reproduce a request that fits the window but exceeds window - window/4", total, physicalWindow)
	}

	drive := budgetTestRun(t, ctx, config.AgentSpec{
		ReplayWindow:  physicalWindow,
		ContextWindow: physicalWindow,
	}, prompt, "c75 symptom input", "session-c75-symptom")
	request := drive.oneRequest(t, "c75 fitting request")
	if len(request.Messages) == 0 {
		t.Fatal("c75 request carries no message, want the whole fixed prompt served")
	}
	if drive.turn.Terminal != model.KindCompleted {
		t.Fatalf("c75 turn terminal = %v, want completed (failure=%q)", drive.turn.Terminal, drive.failureText())
	}
}

// TestSymptomAnchorReplayWindowAloneOpensNoGate pins the gate a declaration opens.
//   - a declaration naming only the replay window opens no budget gate, because the replay window drives the fold cadence, not the provider's physical capacity
//   - the gate opens only on ContextWindow, so the request runs whole
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestSymptomAnchorReplayWindowAloneOpensNoGate(t *testing.T) {
	ctx := context.Background()
	big := budgetTestPromptBody("the fixed prompt line of the replay-window-only gate", 10<<10)
	drive := budgetTestRun(t, ctx, config.AgentSpec{ReplayWindow: 64}, big, "replay-window-only input", "session-replay-only-gate")
	request := drive.oneRequest(t, "replay window alone")
	if len(request.Messages) == 0 {
		t.Fatal("replay-window-only request carries no message, want the whole prompt served with no gate")
	}
	if request.Messages[0].Content != big {
		t.Fatalf("replay-window-only first message = %q, want the whole fixed prompt", boundaryAnchorPreview(request.Messages[0].Content, 120))
	}
	if drive.turn.Terminal != model.KindCompleted {
		t.Fatalf("replay-window-only turn terminal = %v, want completed (failure=%q)", drive.turn.Terminal, drive.failureText())
	}
}

// TestSymptomAnchorOutputLimitServed pins the output limit a declaration serves.
//   - a declared max_tokens reaches the provider as the output limit
//   - a declaration that names none leaves the served limit absent, so no undeclared reserve stands in for it
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestSymptomAnchorOutputLimitServed(t *testing.T) {
	ctx := context.Background()
	t.Run("a declared output limit reaches the provider", func(t *testing.T) {
		const outputLimit = 8000
		drive := budgetTestRun(t, ctx, config.AgentSpec{ReplayWindow: 128000, ContextWindow: 128000, MaxTokens: outputLimit}, "", "the output limit input", "session-output-limit-served")
		request := drive.oneRequest(t, "output limit served")
		if request.GenerationConfig.MaxTokens == nil || *request.GenerationConfig.MaxTokens != outputLimit {
			t.Fatalf("served output limit = %v, want the declared %d", request.GenerationConfig.MaxTokens, outputLimit)
		}
	})
	t.Run("no declaration leaves the output limit absent", func(t *testing.T) {
		drive := budgetTestRun(t, ctx, config.AgentSpec{ReplayWindow: 128000, ContextWindow: 128000}, "", "the output limit baseline input", "session-output-limit-baseline")
		request := drive.oneRequest(t, "output limit baseline")
		if request.GenerationConfig.MaxTokens != nil {
			t.Fatalf("served output limit = %d, want nil when none is declared", *request.GenerationConfig.MaxTokens)
		}
	})
}

// attachEmptyBudgetProjection answers the preparation that registers an empty
// projection under one session, so every calibration run measures the same
// request shape.
func attachEmptyBudgetProjection(t *testing.T, session string) func(*Run) {
	t.Helper()
	return func(run *Run) {
		budgetTestAttach(t, run, session, FoldWindow{}, replay.NewReplayProjection())
	}
}

// TestBudgetReservesOutputAllowance pins the output-limit declaration as the allowance the budget reserves.
// - the limit enters the canonical fingerprint
// - its declared value is the allowance the input budget leaves
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestBudgetReservesOutputAllowance(t *testing.T) {
	ctx := context.Background()
	prompt := budgetTestPromptBody("the explicit reserve prompt", 512)
	session := "session-budget-allowance-base"
	base := budgetTestDriveSpec(t, ctx, config.AgentSpec{Files: []config.FileRef{
		budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt),
	}}, "allowance input", session, attachEmptyBudgetProjection(t, session))
	total := budgetTestTotal(base.oneRequest(t, "allowance base"))
	if total <= 8 {
		t.Fatalf("calibration total = %d tokens, want enough material to exercise the allowance", total)
	}
	window := total + 1

	one := budgetTestDriveSpec(t, ctx, config.AgentSpec{
		ContextWindow: window,
		MaxTokens:     1,
		Files:         []config.FileRef{budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt)},
	}, "allowance input", "session-budget-allowance-one", attachEmptyBudgetProjection(t, "session-budget-allowance-one"))
	one.oneRequest(t, "reserve one")

	whole := budgetTestDriveSpec(t, ctx, config.AgentSpec{
		ContextWindow: window,
		MaxTokens:     window,
		Files:         []config.FileRef{budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt)},
	}, "allowance input", "session-budget-allowance-whole", attachEmptyBudgetProjection(t, "session-budget-allowance-whole"))
	budgetTestRefusal(t, whole, "reserve consumes the whole window")

	fingerprint := func(decl config.Declaration) string {
		t.Helper()
		got, err := config.Fingerprint(decl)
		if err != nil {
			t.Fatalf("fingerprint the declaration: %v", err)
		}
		return got
	}
	bare := fingerprint(config.Declaration{Entry: "a", Agents: []config.AgentSpec{{ID: "a"}}})
	withWindow := fingerprint(config.Declaration{Entry: "a", Agents: []config.AgentSpec{{ID: "a", ContextWindow: 128000}}})
	withOutputLimit := fingerprint(config.Declaration{Entry: "a", Agents: []config.AgentSpec{{ID: "a", MaxTokens: 4096}}})
	if bare == withWindow || bare == withOutputLimit || withWindow == withOutputLimit {
		t.Fatalf("fingerprints bare=%s window=%s output_limit=%s, want each budget declaration to change the canonical core", bare, withWindow, withOutputLimit)
	}
}

// TestFoldWindowSemanticsUnchanged pins the replay/fold window under the request budget.
// - MaxTokens still presents the projection under its own budget
// - the fold defaults still derive from it
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestFoldWindowSemanticsUnchanged(t *testing.T) {
	ctx := context.Background()
	window := normalizeFoldWindow(FoldWindow{MaxTokens: 100})
	if window.CompressThreshold != config.DefaultCompressThreshold {
		t.Fatalf("compression threshold = %v, want the declaration default %v", window.CompressThreshold, config.DefaultCompressThreshold)
	}
	if window.KeepRecentTasks != config.DefaultKeepRecentTasks {
		t.Fatalf("kept recent tasks = %d, want the declaration default %d", window.KeepRecentTasks, config.DefaultKeepRecentTasks)
	}
	if got, want := (FoldWindow{MaxTokens: 100}).VolumeThresholdBytes(), int(float64(100*replay.BudgetCharsPerToken)*config.DefaultCompressThreshold); got != want {
		t.Fatalf("volume threshold = %d bytes, want %d", got, want)
	}

	third := strings.Repeat("the third projected line is long enough that the 100-token replay window cannot keep it whole. ", 8)
	projection := budgetTestProjection(ctx,
		"the first projected line",
		"the second projected line",
		third,
	)
	expected, err := projection.AssembleBudget(ctx, 100)
	if err != nil {
		t.Fatalf("assemble the replay window: %v", err)
	}
	if len(expected) < 2 {
		t.Fatalf("the fold window presented %d entries, want the first two at least", len(expected))
	}

	session := "session-budget-fold"
	drive := budgetTestDriveSpec(t, ctx, config.AgentSpec{
		ReplayWindow:  100,
		ContextWindow: 10000,
		MaxTokens:     1,
		Files: []config.FileRef{
			budgetTestFileRef(t, "system.md", "you are terse.", config.FileKindPrompt),
		},
	}, "fold input", session, func(run *Run) {
		budgetTestAttach(t, run, session, FoldWindow{MaxTokens: 100}, projection)
	})
	request := drive.oneRequest(t, "fold window")
	if want := len(expected) + 2; len(request.Messages) != want {
		t.Fatalf("the request carries %d messages, want the prompt beside the %d replay-window entries and the input (%d)", len(request.Messages), len(expected), want)
	}
	for i, entry := range expected {
		want := replay.TicketOf(entry.Key, entry.Type) + " " + entry.Content
		if request.Messages[i+1].Content != want {
			t.Fatalf("projected entry %d = %q, want %q: the replay window must present the projection it always did, under the ticket injection", i, boundaryAnchorPreview(request.Messages[i+1].Content, 120), boundaryAnchorPreview(want, 120))
		}
	}
	for _, message := range request.Messages {
		if strings.Contains(message.Content, third) {
			t.Fatalf("the request carries the third entry whole, but the 100-token replay window could not: %q", boundaryAnchorPreview(message.Content, 120))
		}
	}
}

// budgetTestCarries reports whether one request holds a marker in any message.
func budgetTestCarries(request model.Request, marker string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, marker) {
			return true
		}
	}
	return false
}

// attachBudgetProjection answers the preparation that registers one projection
// under the drive's session chain.
func attachBudgetProjection(t *testing.T, session string, projection *replay.ReplayProjection, window FoldWindow) func(*Run) {
	t.Helper()
	return func(run *Run) {
		budgetTestAttach(t, run, session, window, projection)
	}
}

// TestBudgetDegradeLadder pins the one degrade order at the boundary reassembly path.
// - the environment block yields first, the skill bodies next, then the projection folds
// - each step is measured on the real assembled request, never estimated
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestBudgetDegradeLadder(t *testing.T) {
	ctx := context.Background()
	prompt := budgetTestPromptBody("ladder-prompt-marker", 2048)
	skillBody := "ladder-skill-marker " + strings.Repeat("declared skill detail. ", 40)
	input := "ladder input"
	entryOne := "projection-entry-one-marker " + strings.Repeat("one ", 80)
	entryTwo := "projection-entry-two-marker " + strings.Repeat("two ", 300)
	foldWindow := FoldWindow{MaxTokens: 2000}
	attached := func(run *Run, session string) {
		budgetTestAttach(t, run, session, foldWindow, budgetTestProjection(ctx, entryOne, entryTwo))
	}

	spec := func(environment, skill bool, window int) config.AgentSpec {
		decl := config.AgentSpec{ContextWindow: window, MaxTokens: 1}
		if environment {
			decl.EnvironmentBlock = true
		}
		decl.Files = append(decl.Files, budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt))
		if skill {
			decl.Files = append(decl.Files, budgetTestSkillFile(t, "ladder-skill", skillBody))
		}
		return decl
	}
	const wide = 1 << 20
	fullSession := "session-ladder-full"
	full := budgetTestDriveSpec(t, ctx, spec(true, true, wide), input, fullSession, func(run *Run) { attached(run, fullSession) })
	fullTotal := budgetTestTotal(full.oneRequest(t, "ladder full"))

	noEnvSession := "session-ladder-no-environment"
	noEnv := budgetTestDriveSpec(t, ctx, spec(false, true, wide), input, noEnvSession, func(run *Run) { attached(run, noEnvSession) })
	noEnvTotal := budgetTestTotal(noEnv.oneRequest(t, "ladder without environment"))

	noSkillSession := "session-ladder-no-skill"
	noSkill := budgetTestDriveSpec(t, ctx, spec(false, false, wide), input, noSkillSession, func(run *Run) { attached(run, noSkillSession) })
	noSkillTotal := budgetTestTotal(noSkill.oneRequest(t, "ladder without skill bodies"))

	noHistorySession := "session-ladder-no-history"
	noHistory := budgetTestDriveSpec(t, ctx, spec(false, false, wide), input, noHistorySession, attachEmptyBudgetProjection(t, noHistorySession))
	noHistoryTotal := budgetTestTotal(noHistory.oneRequest(t, "ladder without projection"))

	if !(noHistoryTotal < noSkillTotal && noSkillTotal < noEnvTotal && noEnvTotal < fullTotal) {
		t.Fatalf("calibration totals no-history=%d no-skill=%d no-environment=%d full=%d, want each material to add its own tokens (scene never formed)",
			noHistoryTotal, noSkillTotal, noEnvTotal, fullTotal)
	}

	envWindow := (noEnvTotal+fullTotal)/2 + 1
	if envWindow-1 <= noEnvTotal || envWindow-1 >= fullTotal {
		t.Fatalf("environment step budget %d is outside (%d, %d)", envWindow-1, noEnvTotal, fullTotal)
	}
	envSession := "session-ladder-step-environment"
	envDrive := budgetTestDriveSpec(t, ctx, spec(true, true, envWindow), input, envSession, func(run *Run) { attached(run, envSession) })
	envRequest := envDrive.oneRequest(t, "ladder environment step")
	if budgetTestCarries(envRequest, "## Environment") {
		t.Fatal("environment step: the request still carries the environment block, want it yielded")
	}
	if !budgetTestCarries(envRequest, "ladder-skill-marker") {
		t.Fatalf("environment step: the request dropped the skill bodies (input=%d no-env=%d full=%d request=%d env=%t), want only the environment block to yield",
			envWindow-1, noEnvTotal, fullTotal, budgetTestTotal(envRequest), budgetTestCarries(envRequest, "## Environment"))
	}
	if !budgetTestCarries(envRequest, "projection-entry-one-marker") {
		t.Fatal("environment step: the request dropped the projection, want only the environment block to yield")
	}
	if got := budgetTestTotal(envRequest); got > envWindow-1 {
		t.Fatalf("environment step: request measures %d tokens over input budget %d", got, envWindow-1)
	}

	skillWindow := (noSkillTotal+noEnvTotal)/2 + 1
	if skillWindow-1 <= noSkillTotal || skillWindow-1 >= noEnvTotal {
		t.Fatalf("skill step budget %d is outside (%d, %d)", skillWindow-1, noSkillTotal, noEnvTotal)
	}
	skillSession := "session-ladder-step-skill"
	skillDrive := budgetTestDriveSpec(t, ctx, spec(true, true, skillWindow), input, skillSession, func(run *Run) { attached(run, skillSession) })
	skillRequest := skillDrive.oneRequest(t, "ladder skill step")
	if budgetTestCarries(skillRequest, "## Environment") || budgetTestCarries(skillRequest, "ladder-skill-marker") {
		t.Fatal("skill step: the request still carries the environment block or the skill bodies, want both yielded")
	}
	if !budgetTestCarries(skillRequest, "projection-entry-one-marker") {
		t.Fatal("skill step: the request dropped the projection, want it kept while the skill bodies yield")
	}
	if got := budgetTestTotal(skillRequest); got > skillWindow-1 {
		t.Fatalf("skill step: request measures %d tokens over input budget %d", got, skillWindow-1)
	}

	foldScenarioWindow := (noHistoryTotal+noSkillTotal)/2 + 1
	if foldScenarioWindow-1 <= noHistoryTotal || foldScenarioWindow-1 >= noSkillTotal {
		t.Fatalf("projection step budget %d is outside (%d, %d)", foldScenarioWindow-1, noHistoryTotal, noSkillTotal)
	}
	foldSession := "session-ladder-step-projection"
	foldDrive := budgetTestDriveSpec(t, ctx, spec(true, true, foldScenarioWindow), input, foldSession, func(run *Run) { attached(run, foldSession) })
	foldRequest := foldDrive.oneRequest(t, "ladder projection step")
	if budgetTestCarries(foldRequest, "## Environment") || budgetTestCarries(foldRequest, "ladder-skill-marker") {
		t.Fatal("projection step: the request still carries a dynamic face, want both yielded before the projection folds")
	}
	if !budgetTestCarries(foldRequest, "ladder-prompt-marker") {
		t.Fatal("projection step: the request dropped the declared prompt, want it kept longest")
	}
	if budgetTestCarries(foldRequest, "projection-entry-two-marker") {
		t.Fatal("projection step: the long projected entry still fits after the fold, want the projection to have folded further")
	}
	if got := budgetTestTotal(foldRequest); got > foldScenarioWindow-1 {
		t.Fatalf("projection step: request measures %d tokens over input budget %d", got, foldScenarioWindow-1)
	}
}

// TestBudgetRefusalWhenUndegradable pins the refusal terminal and the existing failure-notice channel.
//
// - a fixed prompt no degrade can carry issues no model request
// - the turn ends failed and the failure names the budget boundary
// - the host's existing failure notice reaches the recipient with that cause
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestBudgetRefusalWhenUndegradable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompt := budgetTestPromptBody("the refusal prompt line", 10<<10)
	spec := boundaryAnchorSpec()
	spec.ReplayWindow = boundaryAnchorWindow
	spec.ContextWindow = boundaryAnchorWindow
	spec.Files = append(spec.Files, budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt))
	run, provider := boundaryAnchorAssemble(t, ctx, spec, boundaryAnchorAnswer("the refusal must never be served"))
	addr := boundaryAnchorAddress("session-budget-refusal")
	sender := newBoundaryAnchorSender()
	host := boundaryAnchorHost(ctx, run, sender)

	turn, invokeErr := host.Invoke(ctx, InvokeRequest{
		InvocationID: "inv-budget-refusal",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "refuse this request"}},
	})
	provider.mu.Lock()
	requests := len(provider.requests)
	provider.mu.Unlock()
	if requests != 0 {
		t.Fatalf("provider requests = %d, want 0 before a refusal", requests)
	}
	if turn.Terminal != model.KindFailed {
		t.Fatalf("terminal = %v (invoke err=%v), want failed", turn.Terminal, invokeErr)
	}
	cause := strings.TrimSpace(turn.Failure)
	if cause == "" {
		cause = strings.TrimSpace(errText(invokeErr))
	}
	if !boundaryAnchorNamesBudgetRefusal(cause) {
		t.Fatalf("failure = %q, want a cause naming the budget boundary", cause)
	}
	delivered := sender.deliveredContents()
	if len(delivered) != 1 {
		t.Fatalf("delivered = %q, want the one existing failure notice", delivered)
	}
	if want := failureNoticeText(cause); delivered[0] != want {
		t.Fatalf("delivered notice = %q, want the existing failure-notice text %q (no new channel)", delivered[0], want)
	}
	if !strings.Contains(strings.ToLower(delivered[0]), "budget") {
		t.Fatalf("delivered notice = %q, want it to name the request budget", delivered[0])
	}
}

// TestBudgetConsumerPathEvidence pins the budget on the formal consumer path.
// - a real AgentSpec drives the loop and the final request is under the budget
// - the request snapshot reads the decision back
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestBudgetConsumerPathEvidence(t *testing.T) {
	ctx := context.Background()
	prompt := budgetTestPromptBody("consumer-path-prompt-marker", 1024)
	input := "consumer path input"
	wide := 1 << 20

	withEnvSession := "session-budget-consumer-with-environment"
	withEnv := budgetTestDriveSpec(t, ctx, config.AgentSpec{
		ContextWindow: wide, MaxTokens: 1, EnvironmentBlock: true,
		Files: []config.FileRef{budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt)},
	}, input, withEnvSession, attachEmptyBudgetProjection(t, withEnvSession))
	withEnvTotal := budgetTestTotal(withEnv.oneRequest(t, "consumer path with environment"))

	withoutEnvSession := "session-budget-consumer-without-environment"
	withoutEnv := budgetTestDriveSpec(t, ctx, config.AgentSpec{
		ContextWindow: wide, MaxTokens: 1, EnvironmentBlock: false,
		Files: []config.FileRef{budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt)},
	}, input, withoutEnvSession, attachEmptyBudgetProjection(t, withoutEnvSession))
	withoutEnvTotal := budgetTestTotal(withoutEnv.oneRequest(t, "consumer path without environment"))
	if withEnvTotal <= withoutEnvTotal {
		t.Fatalf("environment material added %d tokens, want a positive difference", withEnvTotal-withoutEnvTotal)
	}
	inputBudget := (withoutEnvTotal + withEnvTotal) / 2
	if inputBudget <= withoutEnvTotal || inputBudget >= withEnvTotal {
		t.Fatalf("consumer path budget %d is outside (%d, %d)", inputBudget, withoutEnvTotal, withEnvTotal)
	}

	capture := snapshotTestCapture(t)
	evidenceSession := "session-budget-consumer-evidence"
	evidence := budgetTestDriveSpec(t, ctx, config.AgentSpec{
		ContextWindow: inputBudget + 1, MaxTokens: 1, EnvironmentBlock: true,
		Files: []config.FileRef{budgetTestFileRef(t, "system.md", prompt, config.FileKindPrompt)},
	}, input, evidenceSession, attachEmptyBudgetProjection(t, evidenceSession), WithCapture(capture))
	request := evidence.oneRequest(t, "consumer path evidence")
	if budgetTestCarries(request, "## Environment") {
		t.Fatal("consumer path: the final request still carries the environment block, want the budget to have yielded it")
	}
	if !budgetTestCarries(request, "consumer-path-prompt-marker") {
		t.Fatal("consumer path: the final request dropped the declared prompt")
	}
	if got := budgetTestTotal(request); got > inputBudget {
		t.Fatalf("consumer path: request measures %d tokens over input budget %d", got, inputBudget)
	}

	snapshots := evidence.run.RequestSnapshots()
	if len(snapshots) != 1 {
		t.Fatalf("request snapshots = %d, want the one decision's snapshot", len(snapshots))
	}
	snapshot := snapshots[0].Snapshot
	if len(snapshot.Messages) != len(request.Messages) {
		t.Fatalf("snapshot messages = %d, request messages = %d, want the same final request", len(snapshot.Messages), len(request.Messages))
	}
	for i := range request.Messages {
		if snapshot.Messages[i].Role != request.Messages[i].Role || snapshot.Messages[i].Content != request.Messages[i].Content {
			t.Fatalf("snapshot message %d = (%s,%q), request message = (%s,%q)", i,
				snapshot.Messages[i].Role, boundaryAnchorPreview(snapshot.Messages[i].Content, 80),
				request.Messages[i].Role, boundaryAnchorPreview(request.Messages[i].Content, 80))
		}
	}
	for _, message := range snapshot.Messages {
		if strings.Contains(message.Content, "## Environment") {
			t.Fatal("snapshot still carries the environment block the budget yielded, want the decision readable from the snapshot")
		}
	}
}

// snapshotTestAgent is the agent the generation-snapshot checks assemble.
const snapshotTestAgent = event.AgentID("agent-snapshot")

// snapshotTestGeneration assembles one published generation of the snapshot
// checks and answers the run beside the boundary a request of that generation
// is assembled over. The two generations differ in exactly the two faces the
// assembly reads from the declaration: the environment block and the window the
// request budget is measured against.
type snapshotTestGeneration struct {
	run      *Run
	boundary agent.RequestBoundary
	natives  map[event.AgentID]NativeAgent
	files    []config.FileRef
}

// snapshotTestGenerations assembles the run on its first generation: an agent
// declaring the environment block under a narrow window, at one session.
func snapshotTestGenerations(t *testing.T, ctx context.Context) snapshotTestGeneration {
	t.Helper()
	first := config.AgentSpec{
		ID:               string(snapshotTestAgent),
		Model:            "model-snapshot",
		EnvironmentBlock: true,
		ContextWindow:    4096,
		MaxTokens:        1,
		Files:            []config.FileRef{budgetTestFileRef(t, "system.md", "snapshot generation one prompt", config.FileKindPrompt)},
	}
	natives := map[event.AgentID]NativeAgent{snapshotTestAgent: {Model: &snapshotTestModel{answer: boundaryAnchorAnswer("the snapshot answer")}}}
	run, err := Assemble(ctx, &config.Declaration{Entry: string(snapshotTestAgent), Agents: []config.AgentSpec{first}}, natives,
		WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the snapshot runtime: %v", err)
	}
	run.mu.Lock()
	binding, published := run.current.Agents[snapshotTestAgent]
	run.mu.Unlock()
	if !published {
		t.Fatalf("no published binding of agent %s, want the assembled generation's binding", snapshotTestAgent)
	}
	return snapshotTestGeneration{
		run: run,
		boundary: agent.RequestBoundary{
			Agent:   snapshotTestAgent,
			Chain:   agent.RequestChain{SessionID: "session-snapshot-generation"},
			Binding: binding,
		},
		natives: natives,
		files:   first.Files,
	}
}

// next publishes the second generation of the same agent: it declares no
// environment block and a window of a different size, so every face the
// assembly reads from the declaration changes under it.
func (g snapshotTestGeneration) next(t *testing.T, ctx context.Context) {
	t.Helper()
	second := config.AgentSpec{
		ID:               string(snapshotTestAgent),
		Model:            "model-snapshot",
		EnvironmentBlock: false,
		ContextWindow:    65536,
		MaxTokens:        1,
		Files:            g.files,
	}
	if _, err := g.run.Publish(ctx, &config.Declaration{Entry: string(snapshotTestAgent), Agents: []config.AgentSpec{second}}, g.natives); err != nil {
		t.Fatalf("publish the next generation: %v", err)
	}
	if spec := g.run.agentSpecOf(snapshotTestAgent); spec.EnvironmentBlock || spec.ContextWindow != 65536 {
		t.Fatalf("spec in effect = %+v, want the later generation published", spec)
	}
}

// TestRequestSingleGenerationSnapshot pins one generation binding per request assembly.
//   - the assembly began on a generation that declares the environment block
//   - the generation published while it assembled declares none
//   - the block the request carries is still the one the assembly began on
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestRequestSingleGenerationSnapshot(t *testing.T) {
	ctx := context.Background()
	generation := snapshotTestGenerations(t, ctx)
	assembly := generation.run.beginRequestAssembly(generation.boundary)

	generation.next(t, ctx)

	view, err := assembly.assemble(ctx)
	if err != nil {
		t.Fatalf("assemble the request that began before the publication: %v", err)
	}
	if !strings.Contains(view.DynamicSystem, "## Environment") {
		t.Fatalf("dynamic section = %q, want the environment block of the generation the assembly began on", view.DynamicSystem)
	}
	if !strings.Contains(view.DynamicSystem, "- agent: "+string(snapshotTestAgent)) {
		t.Fatalf("dynamic section = %q, want the block naming the agent the assembly began on", view.DynamicSystem)
	}
}

// TestRequestToolAndSkillFacesStayOnTheStartingGeneration pins the tool face and the skill face of one assembly to the generation the assembly began on.
//   - the two faces are exactly those a publication could otherwise mix into one request
//   - the assembly began on a generation declaring the skill "alpha" beside the tool "tool-a"
//   - the generation published while it assembled declares "beta" beside "tool-b"
//   - the block the request carries still names alpha and tool-a, and the visible skill face it answers is the starting generation's alone
//   - the capture of a driven generation reports the tool face the block names, so a captured request reads the generation that served it
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestRequestToolAndSkillFacesStayOnTheStartingGeneration(t *testing.T) {
	facesAgent := event.AgentID("agent-snapshot-faces")
	prompt := budgetTestFileRef(t, "system.md", "snapshot face prompt", config.FileKindPrompt)
	alpha := budgetTestSkillFile(t, "alpha", "alpha body")
	beta := budgetTestSkillFile(t, "beta", "beta body")
	nativeOf := func(tool string) map[event.AgentID]NativeAgent {
		return map[event.AgentID]NativeAgent{facesAgent: {
			Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
				return &model.Response{
					Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
					Done:    true,
				}
			}},
			Tools: []trpctool.CallableTool{&snapshotTestTool{name: tool, output: tool + "-out"}},
		}}
	}
	declarationOf := func(skill config.FileRef) *config.Declaration {
		return &config.Declaration{Entry: string(facesAgent), Agents: []config.AgentSpec{{
			ID:               string(facesAgent),
			Model:            "model-snapshot",
			EnvironmentBlock: true,
			ContextWindow:    4096,
			MaxTokens:        1,
			Files:            []config.FileRef{prompt, skill},
		}}}
	}

	t.Run("the block and the visible skill face of one assembly stay on its generation", func(t *testing.T) {
		ctx := context.Background()
		run, err := Assemble(ctx, declarationOf(alpha), nativeOf("tool-a"), WithGovernance(snapshotTestGovernance{}))
		if err != nil {
			t.Fatalf("assemble the snapshot runtime: %v", err)
		}
		run.mu.Lock()
		binding, published := run.current.Agents[facesAgent]
		run.mu.Unlock()
		if !published {
			t.Fatalf("no published binding of agent %s, want the assembled generation's binding", facesAgent)
		}
		assembly := run.beginRequestAssembly(agent.RequestBoundary{
			Agent:   facesAgent,
			Chain:   agent.RequestChain{SessionID: "session-snapshot-faces"},
			Binding: binding,
		})

		if _, err := run.Publish(ctx, declarationOf(beta), nativeOf("tool-b")); err != nil {
			t.Fatalf("publish the next generation: %v", err)
		}

		view, err := assembly.assemble(ctx)
		if err != nil {
			t.Fatalf("assemble the request that began before the publication: %v", err)
		}
		if !strings.Contains(view.DynamicSystem, "- tools: skill_load, tool-a") {
			t.Fatalf("dynamic section = %q, want the tool face of the generation the assembly began on", view.DynamicSystem)
		}
		if strings.Contains(view.DynamicSystem, "tool-b") {
			t.Fatalf("dynamic section = %q, want no tool of the generation published while it assembled", view.DynamicSystem)
		}
		if !strings.Contains(view.DynamicSystem, "- skills: alpha") {
			t.Fatalf("dynamic section = %q, want the skill face of the generation the assembly began on", view.DynamicSystem)
		}
		if strings.Contains(view.DynamicSystem, "beta") {
			t.Fatalf("dynamic section = %q, want no skill of the generation published while it assembled", view.DynamicSystem)
		}
		if len(view.VisibleSkills) != 1 || view.VisibleSkills[0] != "alpha" {
			t.Fatalf("visible skills = %v, want the starting generation's own alpha alone", view.VisibleSkills)
		}
	})

	t.Run("the capture of a driven generation reports the tool face it served", func(t *testing.T) {
		ctx := context.Background()
		run, err := Assemble(ctx, declarationOf(alpha), nativeOf("tool-a"),
			WithGovernance(snapshotTestGovernance{}), WithCapture(snapshotTestCapture(t)))
		if err != nil {
			t.Fatalf("assemble the snapshot runtime: %v", err)
		}
		if _, err := run.Invoke(ctx, InvokeRequest{
			InvocationID: "inv-snapshot-faces",
			Episode:      "ep-snapshot-faces",
			Address:      event.Address{AgentID: facesAgent, SessionID: "session-snapshot-faces"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "settle without a call"}},
		}); err != nil {
			t.Fatalf("invoke the assembled run: %v", err)
		}
		if _, err := run.Publish(ctx, declarationOf(beta), nativeOf("tool-b")); err != nil {
			t.Fatalf("publish the next generation: %v", err)
		}
		records := run.RequestSnapshots()
		if len(records) == 0 {
			t.Fatal("capture holds no request snapshot, want the snapshot of the served decision")
		}
		for i, record := range records {
			tools := make([]string, 0, len(record.Snapshot.VisibleTools))
			for _, tool := range record.Snapshot.VisibleTools {
				tools = append(tools, tool.Name)
			}
			if !slices.Contains(tools, "tool-a") {
				t.Fatalf("snapshot %d visible tools = %v, want the tool face of the generation that served it", i, tools)
			}
			if slices.Contains(tools, "tool-b") {
				t.Fatalf("snapshot %d visible tools = %v, want no tool of the generation published after it served", i, tools)
			}
			if len(record.Snapshot.VisibleSkills) != 1 || record.Snapshot.VisibleSkills[0].Name != "alpha" {
				t.Fatalf("snapshot %d visible skills = %v, want the skill face of the generation that served it", i, record.Snapshot.VisibleSkills)
			}
		}
	})
}

// TestRequestSnapshotNotCachedAcrossRequests pins the snapshot as per-assembly only.
//   - the first request carries the environment block its generation declares
//   - after a publication the same boundary assembles a second request
//   - the second carries no block, because the new generation declares none
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestRequestSnapshotNotCachedAcrossRequests(t *testing.T) {
	ctx := context.Background()
	generation := snapshotTestGenerations(t, ctx)
	first, err := generation.run.requestBoundary(ctx, generation.boundary)
	if err != nil {
		t.Fatalf("assemble the first request: %v", err)
	}
	if !strings.Contains(first.DynamicSystem, "## Environment") {
		t.Fatalf("first dynamic section = %q, want the environment block the generation declares", first.DynamicSystem)
	}
	generation.next(t, ctx)

	second, err := generation.run.requestBoundary(ctx, generation.boundary)
	if err != nil {
		t.Fatalf("assemble the request that begins after the publication: %v", err)
	}
	if strings.Contains(second.DynamicSystem, "## Environment") {
		t.Fatalf("second dynamic section = %q, want no environment block: the later generation declares none", second.DynamicSystem)
	}
}

// TestPresentedEntriesCarryTheirEventKeyTicket pins the injection of the fact address.
//   - an entry carrying a committed fact key presents its material behind [evt_<key>|<type>]
//   - the key and the type are the entry's own, so the ticket addresses the fact the material came from
//   - the material behind the ticket is the presentation the entry already had, unchanged
//   - an entry presenting no material states no ticket: the address is owed to material the request carries
//
// 契约: docs/wiki/agent/agent-architecture.md#context-management
func TestPresentedEntriesCarryTheirEventKeyTicket(t *testing.T) {
	input := replay.ProjectionEntry{Key: "e-1", Type: event.TypeExternalInput, Content: "the committed input"}
	if got, want := presentedProjectionContent(input), "[evt_e-1|external_input] the committed input"; got != want {
		t.Fatalf("the presented input = %q, want %q: the material travels with the address of the fact it came from", got, want)
	}
	command := replay.ProjectionEntry{Key: "e-2-call", Type: event.TypeActionCommand, Content: "ok"}
	if got, want := presentedProjectionContent(command), "[evt_e-2-call|action_command] ok"; got != want {
		t.Fatalf("the presented call = %q, want %q: the frozen type of the entry, not a type of the presentation's own", got, want)
	}
	empty := replay.ProjectionEntry{Key: "e-3", Type: event.TypeAgentOutput, Content: ""}
	if got := presentedProjectionContent(empty); got != "" {
		t.Fatalf("the presented empty entry = %q, want the empty presentation: a ticket alone would promise a read of material the model never saw", got)
	}
	hint := replay.ProjectionEntry{Key: "-recovery", Type: replay.ProjectionTypeRecoveryHint, Content: "recovery: replayed 3 entries"}
	if got := presentedProjectionContent(hint); got != hint.Content {
		t.Fatalf("the recovery hint = %q, want it as the assembly wrote it %q: the hint names no fact", got, hint.Content)
	}
}

// TestTheKeyInjectionAddsNoSecondTicket pins the injection's idempotence.
//   - content already opening with a ticket is presented as it stands
//   - no second ticket is appended, so a ticket the model imitated and fed back is never doubled
func TestTheKeyInjectionAddsNoSecondTicket(t *testing.T) {
	kept := replay.ProjectionEntry{Key: "e-4", Type: event.TypeExternalInput, Content: "[evt_e-4|external_input] the imitated line"}
	if got := presentedProjectionContent(kept); got != kept.Content {
		t.Fatalf("the entry opening with a ticket = %q, want it unchanged %q", got, kept.Content)
	}
	if got := strings.Count(presentedProjectionContent(kept), "[evt_"); got != 1 {
		t.Fatalf("the presented entry carries %d ticket(s), want the one it already had", got)
	}
}

// TestAProductEntryGetsNoSyntheticTicket pins what the injection leaves alone.
//   - a segment fold and a history summary carry a synthetic key, so neither is given a ticket
//   - the product text presents itself: its lines name the facts it folded, or it is the summary
//   - no [evt_-seg-…] or [evt_-hist-…] address appears, so the model is never handed a ticket that answers missing
func TestAProductEntryGetsNoSyntheticTicket(t *testing.T) {
	fold := replay.ProjectionEntry{
		Key:     "-seg-abc123",
		Type:    replay.ProjectionTypeSegmentFold,
		Summary: "segment fold",
		Content: "[10-01 10:00|gen:2] [evt_e-1-plan|thinking_plan] thinking_plan: plan",
	}
	if got := presentedProjectionContent(fold); got != fold.Content {
		t.Fatalf("the presented segment fold = %q, want its own lines %q", got, fold.Content)
	}
	summary := replay.ProjectionEntry{Key: "-hist-abc123", Type: replay.ProjectionTypeHistorySummary, Summary: "the store", Content: "the earlier turns"}
	if got, want := presentedProjectionContent(summary), "the store: the earlier turns"; got != want {
		t.Fatalf("the presented history summary = %q, want the summary read %q", got, want)
	}
	for _, product := range []replay.ProjectionEntry{fold, summary} {
		presented := presentedProjectionContent(product)
		for _, form := range []string{"[evt_-seg-", "[evt_-hist-"} {
			if strings.Contains(presented, form) {
				t.Fatalf("the presented product %s carries the synthetic-key ticket %q: that address answers missing", product.Key, form)
			}
		}
	}
}

// TestFoldWindowCarriesTheSummaryBudget pins the summary budget the fold window carries.
//   - a declaration naming only its model window takes the declaration default
//   - a declared budget passes through as it stands, so the fold reads the policy value instead of a constant
//
// 契约: docs/wiki/agent/event-flow.md#unified-compression
func TestFoldWindowCarriesTheSummaryBudget(t *testing.T) {
	undeclared := foldWindowOf(config.AgentSpec{ReplayWindow: 100})
	if undeclared.SummaryBudgetBytes != config.DefaultSummaryBudgetBytes {
		t.Fatalf("summary budget = %d, want the declaration default %d", undeclared.SummaryBudgetBytes, config.DefaultSummaryBudgetBytes)
	}
	declared := foldWindowOf(config.AgentSpec{ReplayWindow: 100, SummaryBudgetBytes: 512})
	if declared.SummaryBudgetBytes != 512 {
		t.Fatalf("summary budget = %d, want the declared 512", declared.SummaryBudgetBytes)
	}
}
