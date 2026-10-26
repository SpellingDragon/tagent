package capabilities

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCriticalCallsHoldThroughFormalPath drives the pre-execution ruling through the module root's public entries:
//   - a critical call is parked in the approval wait through the formal path whatever rule matched it
//   - the parked call reports the wait and never a refusal: the task reads as awaiting approval and the ruling keeps its denial nil
//   - a strict violation is refused before any execution and the refusal facts are the native call's own
//   - the paired result, the denial and the governance record each name the call identity and the argument digest
//   - a call the formal approval face granted runs on its retry and settles beside the ruling that admitted it
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestCriticalCallsHoldThroughFormalPath(t *testing.T) {
	t.Run("a critical call parks in the approval wait through the formal path and executes nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		ruleProbe := &policyProbe{name: "critical_tool"}
		plainProbe := &policyProbe{name: "critical_plain"}
		ledger := governance.NewDenialLedger()
		recorder := &rulingRecorder{inner: governance.Open(
			governance.WithStrict(),
			governance.WithRules(governance.RiskRule{
				ID:      "needs-human",
				MinRisk: plugin.RiskCritical,
				Match:   func(in governance.RiskContext) bool { return in.ToolName == "critical_tool" },
				Reason:  "critical_tool always awaits a human ruling",
			}),
			governance.WithApprovals(approval.NewApprovalManager("", time.Hour)),
			governance.WithLedger(ledger),
		)}
		argsA, argsB := `{"x":1}`, `{"x":2}`
		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			if round == 1 {
				return ntToolTurn(
					ntCall("call-crit-1", "critical_tool", argsA),
					ntCall("call-crit-2", "critical_plain", argsB),
				)
			}
			return ntFinalTurn("settled")
		}}
		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {
				Model: provider,
				Tools: []trpctool.CallableTool{ruleProbe, plainProbe},
				Plugins: []plugin.Instance{&criticalGrader{grades: map[string]plugin.RiskLevel{
					"critical_tool":  plugin.RiskCritical,
					"critical_plain": plugin.RiskCritical,
				}}},
			},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithPolicy(recorder))
		if err != nil {
			t.Fatalf("assemble the approval runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-approval"}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-approval",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the approval runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the waits closing the turn completed beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := ruleProbe.calls() + plainProbe.calls(); got != 0 {
			t.Fatalf("critical executions = %d, want the parked calls to execute nothing", got)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("recorded requests = %d, want the ruling round beside the closing round", len(requests))
		}
		matchedPair := pairedOf(t, &requests[1], "call-crit-1")
		checkPair(t, matchedPair, "call-crit-1", "critical_tool", "parked awaiting approval")
		plainPair := pairedOf(t, &requests[1], "call-crit-2")
		checkPair(t, plainPair, "call-crit-2", "critical_plain", "parked awaiting approval")
		for _, pair := range []model.Message{matchedPair, plainPair} {
			if strings.Contains(pair.Content, task.ErrDenied.Error()) {
				t.Errorf("held call answer = %q, want a wait that is never a refusal", pair.Content)
			}
		}
		rulings := recorder.rulings()
		if len(rulings) != 2 {
			t.Fatalf("rulings = %+v, want exactly one pre-execution ruling per parked call", rulings)
		}
		waiting := rulings[0]
		if waiting.callID != "call-crit-1" || waiting.decision.Disposition != plugin.DispositionHold ||
			waiting.decision.Denial != nil || waiting.decision.Refused() {
			t.Fatalf("ruling of the matched critical call = %+v, want the wait carrying no denial", waiting)
		}
		if waiting.decision.Risk != plugin.RiskCritical || waiting.decision.Rule != "needs-human" ||
			!strings.Contains(waiting.decision.Reason, "critical_tool always awaits a human ruling") ||
			!strings.Contains(waiting.decision.Reason, "critical risk awaits approval") {
			t.Fatalf("ruling of the matched critical call = %+v, want the violation named beside the approval wait", waiting.decision)
		}
		unmatched := rulings[1]
		if unmatched.callID != "call-crit-2" || unmatched.decision.Disposition != plugin.DispositionHold ||
			unmatched.decision.Denial != nil || unmatched.decision.Refused() {
			t.Fatalf("ruling of the unmatched critical call = %+v, want the wait carrying no denial whatever rule matched", unmatched)
		}
		if unmatched.decision.Rule != "" || !strings.Contains(unmatched.decision.Reason, "critical risk awaits approval") {
			t.Fatalf("ruling of the unmatched critical call = %+v, want the plain approval wait", unmatched.decision)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}
		reading, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the approval scene: %v", err)
		}
		held := 0
		for _, detail := range reading.Details {
			if detail.Ref.Kind != query.ObjectTask {
				continue
			}
			held++
			if detail.State.TaskStatus != task.StatusAwaitingApproval {
				t.Fatalf("task %s status = %v, want the parked call to read as awaiting approval", detail.Ref.TaskID, detail.State.TaskStatus)
			}
		}
		if held != 2 {
			t.Fatalf("task details = %d, want one waiting task per parked call", held)
		}
		records := ledger.Query(0)
		if len(records) != 2 {
			t.Fatalf("governance records = %+v, want one approval fact per parked call", records)
		}
		byDigest := map[string]governance.DenialRecord{}
		for _, rec := range records {
			if rec.Subtype != governance.SubtypeApproval || rec.Level != plugin.RiskCritical {
				t.Fatalf("governance record = %+v, want the wait recorded as an approval fact at the critical grade", rec)
			}
			byDigest[rec.ArgsDigest] = rec
		}
		matchedRecord, ok := byDigest[approval.ArgsDigest([]byte(argsA))]
		if !ok || matchedRecord.ToolName != "critical_tool" || matchedRecord.RuleID != "needs-human" {
			t.Fatalf("approval fact of the matched call = %+v, want the native tool and rule the call was parked under", matchedRecord)
		}
		plainRecord, ok := byDigest[approval.ArgsDigest([]byte(argsB))]
		if !ok || plainRecord.ToolName != "critical_plain" || plainRecord.RuleID != "" {
			t.Fatalf("approval fact of the unmatched call = %+v, want the native tool beside no rule", plainRecord)
		}
	})

	t.Run("a strict violation is refused before any execution and its refusal facts name the native call", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		probe := &policyProbe{name: "danger_tool"}
		ledger := governance.NewDenialLedger()
		recorder := &rulingRecorder{inner: governance.Open(
			governance.WithStrict(),
			governance.WithRules(governance.RiskRule{
				ID:      "no-danger",
				MinRisk: plugin.RiskHigh,
				Match:   func(in governance.RiskContext) bool { return in.ToolName == "danger_tool" },
				Reason:  "danger_tool is refused at the high grade",
			}),
			governance.WithApprovals(approval.NewApprovalManager("", time.Hour)),
			governance.WithLedger(ledger),
		)}
		args := `{"x":1}`
		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			if round == 1 {
				return ntToolTurn(ntCall("call-danger-1", "danger_tool", args))
			}
			return ntFinalTurn("settled")
		}}
		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {
				Model: provider,
				Tools: []trpctool.CallableTool{probe},
				Plugins: []plugin.Instance{&criticalGrader{grades: map[string]plugin.RiskLevel{
					"danger_tool": plugin.RiskHigh,
				}}},
			},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithPolicy(recorder))
		if err != nil {
			t.Fatalf("assemble the refusal runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-refusal"}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-refusal",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the refusal runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the refusal closing the turn completed beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := probe.calls(); got != 0 {
			t.Fatalf("danger_tool executions = %d, want the strict refusal leaving zero executions", got)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("recorded requests = %d, want the ruling round beside the closing round", len(requests))
		}
		refusalPair := pairedOf(t, &requests[1], "call-danger-1")
		checkPair(t, refusalPair, "call-danger-1", "danger_tool", task.ErrDenied.Error())
		checkPair(t, refusalPair, "call-danger-1", "danger_tool", "rule: no-danger")
		checkPair(t, refusalPair, "call-danger-1", "danger_tool", "danger_tool is refused at the high grade")
		rulings := recorder.rulings()
		if len(rulings) != 1 {
			t.Fatalf("rulings = %+v, want exactly one pre-execution ruling of the refused call", rulings)
		}
		if !rulings[0].decision.Refused() {
			t.Fatalf("strict ruling = %+v, want a terminal refusal carrying its denial", rulings[0].decision)
		}
		denial := rulings[0].decision.Denial
		if denial.CallID != "call-danger-1" || denial.Tool != "danger_tool" || denial.Rule != "no-danger" {
			t.Fatalf("denial = %+v, want the refusal facts naming the native call the model issued", denial)
		}
		if denial.Result.Role != model.RoleTool || denial.Result.ToolID != "call-danger-1" || denial.Result.ToolName != "danger_tool" {
			t.Fatalf("denial result = %+v, want the paired result answering the native call identity", denial.Result)
		}
		records := ledger.Query(0)
		if len(records) != 1 {
			t.Fatalf("governance records = %+v, want one denial fact of the refused call", records)
		}
		refusalRecord := records[0]
		if refusalRecord.Subtype != governance.SubtypeDenial || refusalRecord.ToolName != "danger_tool" || refusalRecord.RuleID != "no-danger" {
			t.Fatalf("refusal fact = %+v, want the denial recorded under the native tool and its rule", refusalRecord)
		}
		if want := approval.ArgsDigest([]byte(args)); refusalRecord.ArgsDigest != want {
			t.Fatalf("refusal digest = %s, want the digest %s of the arguments the model issued", refusalRecord.ArgsDigest, want)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}
		reading, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the refusal scene: %v", err)
		}
		for _, detail := range reading.Details {
			if detail.Ref.Kind == query.ObjectTask {
				t.Fatalf("task detail = %+v, want no task registered for a call refused before execution", detail.Ref)
			}
		}
	})

	t.Run("a call the formal approval face granted runs on its retry", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		probe := &policyProbe{name: "critical_tool"}
		approvals := approval.NewApprovalManager("", time.Hour)
		recorder := &rulingRecorder{inner: governance.Open(
			governance.WithStrict(),
			governance.WithRules(governance.RiskRule{
				ID:      "needs-human",
				MinRisk: plugin.RiskCritical,
				Match:   func(in governance.RiskContext) bool { return in.ToolName == "critical_tool" },
				Reason:  "critical_tool always awaits a human ruling",
			}),
			governance.WithApprovals(approvals),
		)}
		args := `{"x":7}`
		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			if round == 1 {
				return ntToolTurn(ntCall("call-crit-1", "critical_tool", args))
			}
			if round == 2 {
				approvalID := task.NewApprovalID()
				if _, err := approvals.Request(ctx, approvalID, "critical_tool", []byte(args), plugin.RiskCritical, "needs-human", "critical_tool always awaits a human ruling"); err != nil {
					t.Errorf("register the approval request: %v", err)
				}
				if _, err := approvals.Decide(approvalID, approval.ApprovalApproved, "operator"); err != nil {
					t.Errorf("decide the approval request: %v", err)
				}
				return ntToolTurn(ntCall("call-crit-2", "critical_tool", args))
			}
			return ntFinalTurn("settled")
		}}
		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {
				Model: provider,
				Tools: []trpctool.CallableTool{probe},
				Plugins: []plugin.Instance{&criticalGrader{grades: map[string]plugin.RiskLevel{
					"critical_tool": plugin.RiskCritical,
				}}},
			},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithPolicy(recorder))
		if err != nil {
			t.Fatalf("assemble the approved runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-approved"}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-approved",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the approved runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 3 {
			t.Fatalf("turn = (%v, %d decisions), want the wait, the approved retry and the closing answer as three decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := probe.calls(); got != 1 {
			t.Fatalf("critical_tool executions = %d, want the approved retry executed exactly once", got)
		}
		requests := provider.recorded()
		if len(requests) != 3 {
			t.Fatalf("recorded requests = %d, want the wait round, the retry round and the closing round", len(requests))
		}
		heldPair := pairedOf(t, &requests[1], "call-crit-1")
		checkPair(t, heldPair, "call-crit-1", "critical_tool", "parked awaiting approval")
		checkReceipt(t, pairedOf(t, &requests[2], "call-crit-2"), "call-crit-2", "critical_tool", "probe-out:critical_tool")
		checkObservation(t, settledOf(t, &requests[2], "call-crit-2"), "call-crit-2", "critical_tool", "probe-out:critical_tool")
		rulings := recorder.rulings()
		if len(rulings) != 2 {
			t.Fatalf("rulings = %+v, want the wait of the first call beside the ruling of its retry", rulings)
		}
		if rulings[0].decision.Disposition != plugin.DispositionHold || rulings[0].decision.Denial != nil || rulings[0].decision.Refused() {
			t.Fatalf("ruling of the first call = %+v, want the wait carrying no denial", rulings[0].decision)
		}
		approved := rulings[1].decision
		if approved.Disposition != plugin.DispositionRecord || approved.Denial != nil || approved.Refused() {
			t.Fatalf("ruling of the approved retry = %+v, want the approved call admitted for execution carrying no denial", approved)
		}
		if approved.Rule != "needs-human" || !strings.Contains(approved.Reason, "the call was approved for these arguments") {
			t.Fatalf("ruling of the approved retry = %+v, want the approval named beside its rule", approved)
		}
	})
}

// pairedOf returns the paired tool message the recorded request answers the named call with.
func pairedOf(t *testing.T, req *model.Request, callID string) model.Message {
	t.Helper()
	var found []model.Message
	for _, msg := range req.Messages {
		if msg.ToolID == callID && msg.Role == model.RoleTool {
			found = append(found, msg)
		}
	}
	if len(found) != 1 {
		t.Fatalf("paired answers under %s = %d, want exactly one", callID, len(found))
	}
	return found[0]
}

// settledOf returns the settled observation the recorded request carries under the named call identity.
func settledOf(t *testing.T, req *model.Request, callID string) model.Message {
	t.Helper()
	var found []model.Message
	for _, msg := range req.Messages {
		if msg.ToolID == callID && msg.Role != model.RoleTool {
			found = append(found, msg)
		}
	}
	if len(found) != 1 {
		t.Fatalf("settled observations under %s = %d, want exactly one", callID, len(found))
	}
	return found[0]
}

// criticalGrader is the risk face of these sub-cases: it grades each armed tool name at the grade its table names and everything else at the low grade, so a ruling only consumes the risk grade the one Before pass carried.
type criticalGrader struct {
	grades map[string]plugin.RiskLevel
}

// Name returns the stable name of the test plugin.
func (g *criticalGrader) Name() string { return "critical-grader" }

// Port returns the single port the test instance serves.
func (g *criticalGrader) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductRuling}
}

// AssessRisk grades one tool call from the table the grader carries.
func (g *criticalGrader) AssessRisk(_ context.Context, call *model.ToolCall) plugin.RiskLevel {
	if grade, ok := g.grades[call.Function.Name]; ok {
		return grade
	}
	return plugin.RiskLow
}

// recordedRuling keeps one pre-execution ruling beside the call identity it was rendered over.
type recordedRuling struct {
	callID   string
	decision plugin.Decision
}

// rulingRecorder is the ruling face of these sub-cases: it wraps the policy under test and keeps every pre-execution ruling the formal admission drew from it beside the call it ruled over, so the wait-not-refusal reading is checked on the very ruling the admission consumed.
type rulingRecorder struct {
	inner plugin.Governor

	mu    sync.Mutex
	drawn []recordedRuling
}

// Judge keeps the ruling and delegates to the wrapped policy.
func (r *rulingRecorder) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	decision, err := r.inner.Judge(pass)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drawn = append(r.drawn, recordedRuling{callID: pass.Final.ID, decision: decision})
	return decision, err
}

// rulings reports the pre-execution rulings kept so far, in the order the admission drew them.
func (r *rulingRecorder) rulings() []recordedRuling {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedRuling(nil), r.drawn...)
}
