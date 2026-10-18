// Package host exercises the diagnostic read of the assembled runtime through
// the public entries the host drives: the query face must carry the account the
// sole publication entry admitted and every real state dimension of it.
package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// phaseGate is one test-observed phase of the drive: entering the gated phase
// signals the test and waits for its release, so the reading is taken at
// exactly that phase instead of somewhere around it.
type phaseGate struct {
	entered chan struct{}
	release chan struct{}
}

// newPhaseGate returns one openable phase gate.
func newPhaseGate() *phaseGate {
	return &phaseGate{entered: make(chan struct{}, 4), release: make(chan struct{}, 4)}
}

// waitEntered blocks until the gated phase is entered once and fails the test
// when the drive never reaches it.
func (g *phaseGate) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("the gated phase was never entered")
	}
}

// open releases the phase entered once.
func (g *phaseGate) open() {
	g.release <- struct{}{}
}

// enter signals the test and waits for its release.
func (g *phaseGate) enter() {
	g.entered <- struct{}{}
	<-g.release
}

// parkedModel is the scripted native model stand-in of these checks: it records
// every request and answers with the scripted response, holding the requests
// whose tail content its gate names until the test releases them. It never
// reaches a real endpoint.
type parkedModel struct {
	gate     *phaseGate
	name     string
	parkTail string
}

// GenerateContent records the native request, holds the gated decision and
// streams the scripted answer: one tool call per driven input beside one final
// answer for every settled observation.
func (m *parkedModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	last := req.Messages[len(req.Messages)-1]
	if last.Content == m.parkTail {
		m.gate.enter()
	}
	ch := make(chan *model.Response, 1)
	switch last.Content {
	case "hello":
		ch <- entryToolTurn(entryCall("call-1", "echo", `{"x":1}`))
	case "hello again":
		ch <- entryToolTurn(entryCall("call-2", "echo", `{"fail":true}`))
	default:
		ch <- entryFinalTurn("settled")
	}
	close(ch)
	return ch, nil
}

// Info reports the scripted model's descriptor.
func (m *parkedModel) Info() model.Info { return model.Info{Name: m.name} }

// parkedTool is the native echo tool stand-in of these checks: every execution
// passes the gate the test observes first and then answers its output — or the
// scripted failure, so one admitted call settles and one fails on purpose.
type parkedTool struct {
	gate *phaseGate
	name string
}

// Declaration returns the declaration naming the echo tool.
func (t *parkedTool) Declaration() *trpctool.Declaration { return &trpctool.Declaration{Name: t.name} }

// Call passes the gate and reports the scripted outcome of this execution.
func (t *parkedTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	fail := strings.Contains(string(jsonArgs), "fail")
	t.gate.enter()
	if fail {
		return nil, errors.New("the scripted tool failure")
	}
	return "out-a", nil
}

// driveResult carries the outcome of one invocation driven in the background.
type driveResult struct {
	turn agent.Turn
	err  error
}

// agentReading reports the agent reading of the named agent in one reading,
// located by the agent identity its kind names.
func agentReading(t *testing.T, snap query.Snapshot, id event.AgentID) query.AgentState {
	t.Helper()
	for _, d := range snap.Details {
		if d.Ref.Kind == query.ObjectAgent && d.Ref.Address.AgentID == id {
			return d.State.Agent
		}
	}
	t.Fatalf("agent %s 缺席读代: %+v", id, snap.Details)
	return query.AgentState{}
}

// hasAgent reports whether one reading carries the named agent at all.
func hasAgent(snap query.Snapshot, id event.AgentID) bool {
	for _, d := range snap.Details {
		if d.Ref.Kind == query.ObjectAgent && d.Ref.Address.AgentID == id {
			return true
		}
	}
	return false
}

// invocationReading reports the state reading of the named invocation in one
// reading. A host reader never learns the runtime instance identity the
// assembly minted, so the entry is located by the invocation identity its kind
// names.
func invocationReading(t *testing.T, snap query.Snapshot, id event.InvocationID) query.DetailState {
	t.Helper()
	for _, d := range snap.Details {
		if d.Ref.Kind == query.ObjectInvocation && d.Ref.InvocationID == id {
			return d.State
		}
	}
	t.Fatalf("invocation %s 缺席读代: %+v", id, snap.Details)
	return query.DetailState{}
}

// taskReadings counts the native task statuses of one reading off the task
// entries alone.
func taskReadings(snap query.Snapshot) map[task.Status]int {
	out := map[task.Status]int{}
	for _, d := range snap.Details {
		if d.Ref.Kind == query.ObjectTask {
			out[d.State.TaskStatus]++
		}
	}
	return out
}

// noticeReadings counts the delivery phases of one reading off the notice
// entries alone.
func noticeReadings(snap query.Snapshot) map[query.NoticePhase]int {
	out := map[query.NoticePhase]int{}
	for _, d := range snap.Details {
		if d.Ref.Kind == query.ObjectNotice {
			out[d.State.NoticePhase]++
		}
	}
	return out
}

// leaseNames names the resource registrations of one reading by the resource
// they hold, the way a host page lists the account in effect.
func leaseNames(snap query.Snapshot) map[string]bool {
	out := map[string]bool{}
	for _, d := range snap.Details {
		if d.Ref.Kind == query.ObjectLease {
			out[d.Ref.Lease.Kind+"/"+d.Ref.Lease.Key] = true
		}
	}
	return out
}

// equalSet reports whether two name sets hold the same members.
func equalSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if !b[name] {
			return false
		}
	}
	return true
}

// TestQueryShowsPublishedAgentState proves the diagnostic read carries the published account and every real state dimension.
// - ①经正式 Assemble→Publish 发布新 agent 后，run.Query 可见其 configured 态（不被压成 active 单值）
// - ②Detail 保留真实状态维度: agent configured/resident/busy、task 状态、notice 阶段各自可辨
// - ③统计仍从明细派生: every number a host page reports is counted off the detail entries it received
// - D-M1 核验: the configuration and lease dimensions read the account the publication just admitted
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestQueryShowsPublishedAgentState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	modelGate := newPhaseGate()
	toolGate := newPhaseGate()
	provider := &parkedModel{gate: modelGate, name: "model-a", parkTail: "hello"}
	tool := &parkedTool{gate: toolGate, name: "echo"}
	declA := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	agentsA := map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}
	run, err := tagent.Assemble(ctx, declA, agentsA, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the formal run: %v", err)
	}
	scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a", "agent-b"}}
	read := func(t *testing.T) query.Snapshot {
		t.Helper()
		snap, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the live scene: %v", err)
		}
		return snap
	}

	t.Run("①发布后新 agent 的 configured 态可见: the read carries the account the publication admitted", func(t *testing.T) {
		before := read(t)
		if hasAgent(before, "agent-b") {
			t.Fatalf("发布前读到未配置的 agent-b: %+v", before.Details)
		}
		if got := agentReading(t, before, "agent-a"); got != (query.AgentState{Configured: true}) {
			t.Fatalf("agent-a 读数 = %+v, want 仅 configured 三读数", got)
		}
		wantBefore := map[string]bool{
			"model/model-a": true, "toolset/agent-a": true, "filesource/agent-a": true, "plugins/agent-a": true,
		}
		if got := leaseNames(before); !equalSet(got, wantBefore) {
			t.Fatalf("初代租约账 = %v, want %v", got, wantBefore)
		}

		providerB := &entryModel{name: "model-b", answer: func(*model.Request) *model.Response {
			return entryFinalTurn("settled")
		}}
		declB := &config.Declaration{
			Entry: "agent-a",
			Agents: []config.AgentSpec{
				{ID: "agent-a", Model: "model-a"},
				{ID: "agent-b", Model: "model-b"},
			},
		}
		agentsB := map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
			"agent-b": {Model: providerB},
		}
		outcome, err := run.Publish(ctx, declB, agentsB)
		if err != nil {
			t.Fatalf("publish the second generation: %v", err)
		}
		if outcome.Result != state.PublishApplied {
			t.Fatalf("publish = %+v, want the changed declaration applied as the next generation", outcome)
		}

		after := read(t)
		if got := agentReading(t, after, "agent-b"); got != (query.AgentState{Configured: true}) {
			t.Fatalf("agent-b 读数 = %+v, want 配置态三读数 {configured}（不被压成 active 单值）", got)
		}
		wantAfter := map[string]bool{
			"model/model-a": true, "toolset/agent-a": true, "filesource/agent-a": true, "plugins/agent-a": true,
			"model/model-b": true, "toolset/agent-b": true, "filesource/agent-b": true, "plugins/agent-b": true,
		}
		if got := leaseNames(after); !equalSet(got, wantAfter) {
			t.Fatalf("当代租约账 = %v, want %v", got, wantAfter)
		}
		derived := query.AgentCounts{}
		for _, d := range after.Details {
			if d.State.Agent.Configured {
				derived.Configured++
			}
			if d.State.Agent.Resident {
				derived.Resident++
			}
			if d.State.Agent.Busy {
				derived.Busy++
			}
		}
		if derived != (query.AgentCounts{Configured: 2, Resident: 0, Busy: 0}) {
			t.Fatalf("明细派生 agent 统计 = %+v, want configured 2, resident 0, busy 0", derived)
		}
	})

	addr1 := event.Address{AgentID: "agent-a", SessionID: "s-1"}
	addr2 := event.Address{AgentID: "agent-a", SessionID: "s-2"}

	t.Run("②决策在途 busy 读数: the busy reading marks the model decision in flight", func(t *testing.T) {
		driven := make(chan driveResult, 1)
		go func() {
			turn, err := run.Invoke(ctx, tagent.InvokeRequest{
				InvocationID: "inv-1",
				Address:      addr1,
				Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
			})
			driven <- driveResult{turn, err}
		}()
		modelGate.waitEntered(t)

		snap := read(t)
		if got := agentReading(t, snap, "agent-a"); got != (query.AgentState{Configured: true, Resident: true, Busy: true}) {
			t.Fatalf("模型在途 agent-a 读数 = %+v, want configured/resident/busy 三读数并存", got)
		}
		if got := agentReading(t, snap, "agent-b"); got != (query.AgentState{Configured: true}) {
			t.Fatalf("未驻留 agent-b 读数 = %+v, want 仅 configured", got)
		}
		if got := invocationReading(t, snap, "inv-1"); got.Decision == "" {
			t.Fatalf("inv-1 活动决策 = %+v, want 模型在途时持有决策", got)
		}
		modelGate.open()

		toolGate.waitEntered(t)
		snap = read(t)
		if got := agentReading(t, snap, "agent-a"); got != (query.AgentState{Configured: true, Resident: true}) {
			t.Fatalf("工具在途 agent-a 读数 = %+v, want busy 已释放", got)
		}
		if readings := taskReadings(snap); len(readings) != 1 || readings[task.StatusAccepted] != 1 {
			t.Fatalf("工具在途 task 读数 = %v, want 仅原生 accepted（在途未落定）", readings)
		}
		toolGate.open()

		res := <-driven
		if res.err != nil {
			t.Fatalf("invoke inv-1: %v", res.err)
		}
		if res.turn.Terminal != model.KindCompleted {
			t.Fatalf("inv-1 terminal = %v, want completed", res.turn.Terminal)
		}

		snap = read(t)
		if readings := taskReadings(snap); len(readings) != 1 || readings[task.StatusSettled] != 1 {
			t.Fatalf("落定后 task 读数 = %v, want 原生 settled", readings)
		}
		phases := noticeReadings(snap)
		if len(phases) != 1 || phases[query.NoticePhaseConsumed] != 1 {
			t.Fatalf("notice 读数 = %v, want 仅 consumed 相", phases)
		}
		for _, d := range snap.Details {
			switch d.Ref.Kind {
			case query.ObjectTask:
				if d.State == query.DetailRetired {
					t.Fatalf("终态任务被压成 consumed 通知同值: %+v", d.State)
				}
			case query.ObjectNotice:
				if d.State != query.DetailRetired || d.State.NoticePhase != query.NoticePhaseConsumed {
					t.Fatalf("通知读数 = %+v, want consumed 相锚 %+v", d.State, query.DetailRetired)
				}
			}
		}
		if got := agentReading(t, snap, "agent-a"); got != (query.AgentState{Configured: true, Resident: true}) {
			t.Fatalf("落定后 agent-a 读数 = %+v, want configured/resident", got)
		}
		if got := invocationReading(t, snap, "inv-1"); got.Decision != "" {
			t.Fatalf("落定后 inv-1 活动决策 = %+v, want 槽位已释放", got)
		}
	})

	t.Run("②任务状态与通知阶段同场可辨: the native task statuses and the notice phases read apart", func(t *testing.T) {
		driven := make(chan driveResult, 1)
		go func() {
			turn, err := run.Invoke(ctx, tagent.InvokeRequest{
				InvocationID: "inv-2",
				Address:      addr2,
				Input:        []model.Message{{Role: model.RoleUser, Content: "hello again"}},
			})
			driven <- driveResult{turn, err}
		}()
		toolGate.waitEntered(t)

		snap := read(t)
		if readings := taskReadings(snap); len(readings) != 2 || readings[task.StatusSettled] != 1 || readings[task.StatusAccepted] != 1 {
			t.Fatalf("同场 task 读数 = %v, want settled 与 accepted 各自可辨", readings)
		}
		toolGate.open()

		res := <-driven
		if res.err != nil {
			t.Fatalf("invoke inv-2: %v", res.err)
		}
		if res.turn.Terminal != model.KindCompleted {
			t.Fatalf("inv-2 terminal = %v, want completed", res.turn.Terminal)
		}

		snap = read(t)
		if readings := taskReadings(snap); len(readings) != 2 || readings[task.StatusSettled] != 1 || readings[task.StatusFailed] != 1 {
			t.Fatalf("终局 task 读数 = %v, want 原生 settled 与 failed 各自可辨", readings)
		}
		phases := noticeReadings(snap)
		if len(phases) != 1 || phases[query.NoticePhaseConsumed] != 2 {
			t.Fatalf("notice 读数 = %v, want 两条通知各自处于 consumed 相", phases)
		}

		derived := query.AgentCounts{}
		kinds := map[query.ObjectKind]int{}
		for _, d := range snap.Details {
			kinds[d.Ref.Kind]++
			if d.State.Agent.Configured {
				derived.Configured++
			}
			if d.State.Agent.Resident {
				derived.Resident++
			}
			if d.State.Agent.Busy {
				derived.Busy++
			}
		}
		if derived != (query.AgentCounts{Configured: 2, Resident: 1, Busy: 0}) {
			t.Fatalf("明细派生 agent 统计 = %+v, want configured 2, resident 1, busy 0", derived)
		}
		total := 0
		for _, n := range kinds {
			total += n
		}
		if total != len(snap.Details) {
			t.Fatalf("派生合计 = %d, want 明细条目数 %d", total, len(snap.Details))
		}
		if got := kinds[query.ObjectTask]; got != 2 {
			t.Fatalf("派生 task 计数 = %d, want 2 条 task 明细", got)
		}
	})
}
