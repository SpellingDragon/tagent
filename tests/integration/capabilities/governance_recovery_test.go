package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/runtime/task"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestGovernanceRecovery drives the governance recovery surface over the real policy ruling and the real memory tier:
// - a declared goal activates the goal gate and a refusal permeates the goal context into its record
// - the goal state words stay verbatim — active, achieved, abandoned, expired — and expiry is derived from validity
// - the warn grade records a missing goal without denying, and only strict refuses a goal-less critical call
// - an exhausted budget window refuses the call whatever the grade says, and a restart resumes the spent window
// - a restart replays the governance events back and keeps the denial records with their goal permeation
// - concurrent declarations and records keep every counter exact
// - a failing store and a bad snapshot degrade instead of failing the gate
// 契约: docs/wiki/agent/governance-enforcement.md#goal-registry
func TestGovernanceRecovery(t *testing.T) {
	t.Run("a declared goal activates the goal gate and a refusal permeates the goal context", func(t *testing.T) {
		logFace := recOpenLog(t, "")
		goals := governance.NewGoalRegistry()
		ledger := governance.NewDenialLedger()
		ledger.BindStore(logFace)
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskHigh, tool: "shell_exec", rules: []governance.RiskRule{recDeployRule()}, goals: goals, ledger: ledger, goalRequired: []string{"model"}})

		if _, err := rig.call("call-goal-missing", "shell_exec", `{"cmd":"deploy"}`); !errors.Is(err, task.ErrDenied) {
			t.Fatalf("call error = %v, want the goal gate refusing the goal-less autonomous call", err)
		}
		if rig.inner.executions() != 0 {
			t.Fatalf("tool executions = %d, want the refused call to execute nothing", rig.inner.executions())
		}
		if got := ledger.Count(); got != 1 {
			t.Fatalf("ledger records = %d, want the missing-goal ruling recorded", got)
		}
		missing := ledger.Query(0)[0]
		if missing.Subtype != governance.SubtypeDenial || missing.GoalID != "" {
			t.Fatalf("missing-goal record = %+v, want a denial record carrying no goal context", missing)
		}
		if !strings.Contains(missing.Reason, "declared goal") {
			t.Fatalf("missing-goal reason = %q, want the missing goal named for the model to correct", missing.Reason)
		}

		g1 := goals.Declare("ship the release", "entry", 0)
		if g1 != "g-1" {
			t.Fatalf("declared goal id = %q, want the declaration-order identity", g1)
		}
		if _, err := rig.call("call-permeated", "shell_exec", `{"cmd":"deploy"}`); !errors.Is(err, task.ErrDenied) {
			t.Fatalf("call error = %v, want the strict rule refusing the violating call", err)
		}
		if got := ledger.Count(); got != 2 {
			t.Fatalf("ledger records = %d, want the rule refusal recorded beside the goal ruling", got)
		}
		refusal := ledger.Query(0)[0]
		if refusal.Subtype != governance.SubtypeDenial || refusal.GoalID != g1 {
			t.Fatalf("refusal record = %+v, want the denial permeating goal %s", refusal, g1)
		}
		if !strings.Contains(refusal.Reason, recDeployRuleReason) {
			t.Fatalf("refusal reason = %q, want the rule reason recorded", refusal.Reason)
		}
		if d := rig.decision(); !d.Refused() || d.Denial.CallID != "call-permeated" || d.Denial.Tool != "shell_exec" {
			t.Fatalf("ruling = %+v, want the terminal refusal of the called identity", d)
		}

		stored, err := logFace.Scan(context.Background(), "tagent/governance/v1/denial:")
		if err != nil {
			t.Fatalf("scan the governance events: %v", err)
		}
		if len(stored) != 2 {
			t.Fatalf("governance events = %d, want one event beside each ledger record", len(stored))
		}
		var permeated bool
		for _, raw := range stored {
			var evt event.Event
			if err := json.Unmarshal(raw, &evt); err != nil {
				t.Fatalf("parse stored governance event: %v", err)
			}
			if evt.Meta[governance.MetaKeySubtype] != governance.SubtypeDenial {
				t.Fatalf("event subtype = %q, want the denial word", evt.Meta[governance.MetaKeySubtype])
			}
			if evt.Meta["goal_id"] == g1 {
				permeated = true
			}
		}
		if !permeated {
			t.Fatalf("governance events = %+v, want one event permeating goal %s", stored, g1)
		}

		wGoals := governance.NewGoalRegistry()
		wLedger := governance.NewDenialLedger()
		warnRig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskHigh, tool: "shell_exec", goals: wGoals, ledger: wLedger, goalRequired: []string{"model"}})
		out, err := warnRig.call("call-warn-goal", "shell_exec", `{"cmd":"status"}`)
		if err != nil {
			t.Fatalf("the warn grade records a missing goal and never denies: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call answer = %T, want the admission receipt of a call that runs", out)
		}
		warnRig.awaitRecorded()
		if warnRig.inner.executions() != 1 {
			t.Fatalf("tool executions = %d, want the warn-graded call to run", warnRig.inner.executions())
		}
		if warnRig.decision().Refused() {
			t.Fatalf("warn ruling = %+v, want the missing goal recorded without a refusal", warnRig.decision())
		}
		warnRec := recFind(wLedger, governance.SubtypeDenial)
		if warnRec == nil || !strings.Contains(warnRec.Reason, "declared goal") {
			t.Fatalf("warn records = %+v, want the missing goal recorded beside the running call", wLedger.Query(0))
		}
	})

	t.Run("the goal state words are kept verbatim and expired is derived from validity", func(t *testing.T) {
		if string(governance.GoalActive) != "active" || string(governance.GoalAchieved) != "achieved" ||
			string(governance.GoalAbandoned) != "abandoned" || string(governance.GoalExpired) != "expired" {
			t.Fatalf("goal state words = %q/%q/%q/%q, want active/achieved/abandoned/expired verbatim",
				governance.GoalActive, governance.GoalAchieved, governance.GoalAbandoned, governance.GoalExpired)
		}
		goals := governance.NewGoalRegistry()
		g1 := goals.Declare("ship the release", "entry", 0)
		if recGoal(t, goals, g1).Status != governance.GoalActive {
			t.Fatalf("declared goal = %+v, want the goal in flight under its declared word", recGoal(t, goals, g1))
		}
		if !goals.Resolve(g1, governance.GoalAchieved) || recGoal(t, goals, g1).Status != governance.GoalAchieved {
			t.Fatalf("resolved goal = %+v, want the reached goal under its achieved word", recGoal(t, goals, g1))
		}
		g2 := goals.Declare("watch the rollout", "entry", 0)
		if !goals.Resolve(g2, governance.GoalAbandoned) || recGoal(t, goals, g2).Status != governance.GoalAbandoned {
			t.Fatalf("resolved goal = %+v, want the given-up goal under its abandoned word", recGoal(t, goals, g2))
		}
		if !goals.Resolve(g2, governance.GoalAchieved) {
			t.Fatalf("a repeated resolution must be applied as it arrives — the registry guards no terminal state")
		}
		g3 := goals.Declare("collect the metrics", "entry", time.Now().UnixMilli()-1000)
		if goals.HasActive() || goals.Current() != nil {
			t.Fatalf("a goal past its validity must stop counting as active")
		}
		if got := recGoal(t, goals, g3).Status; got != governance.GoalActive {
			t.Fatalf("expired goal status = %q, want the recorded word never flipped — expired is derived", got)
		}
		if len(goals.Active()) != 0 {
			t.Fatalf("active goals = %+v, want only unexpired goals in flight", goals.Active())
		}
		g4 := goals.Declare("keep watching", "entry", 0)
		if !goals.HasActive() || goals.Current() == nil || goals.Current().ID != g4 {
			t.Fatalf("current goal = %+v, want the newest goal in flight as the goal context", goals.Current())
		}
	})

	t.Run("an exhausted window refuses whatever the grade says and a restart never refreshes it", func(t *testing.T) {
		clock := &govClock{now: time.Now()}
		dir := t.TempDir()
		cfg := governance.BudgetConfig{MaxHighRisk: 2}
		ledger := governance.NewDenialLedger()
		policy := governance.Open(
			governance.WithBudget(governance.NewBudgetManager(cfg, dir, governance.WithBudgetClock(clock.current))),
			governance.WithLedger(ledger),
		)
		if d := recJudge(t, policy, "call-budget-1", `{"cmd":"ls"}`); d.Refused() {
			t.Fatalf("first call = %+v, want the window admitting its first operations", d)
		}
		if d := recJudge(t, policy, "call-budget-2", `{"cmd":"ls"}`); d.Refused() {
			t.Fatalf("second call = %+v, want the window admitting within its limit", d)
		}
		spent := recJudge(t, policy, "call-budget-3", `{"cmd":"ls"}`)
		if !spent.Refused() || spent.Denial == nil {
			t.Fatalf("third call = %+v, want the spent window refusing the call even under the warn grade", spent)
		}
		if !strings.Contains(spent.Reason, "budget") {
			t.Fatalf("budget refusal reason = %q, want the spent window named", spent.Reason)
		}
		if rec := recFind(ledger, governance.SubtypeDenial); rec == nil {
			t.Fatalf("ledger records = %+v, want the budget refusal recorded", ledger.Query(0))
		}

		resumed := governance.NewBudgetManager(cfg, dir, governance.WithBudgetClock(clock.current))
		if !errors.Is(resumed.Admit(plugin.RiskHigh), governance.ErrBudgetExhausted) {
			t.Fatalf("a restarted budget manager must resume the spent window, not refresh it")
		}
		if got := resumed.Usage(plugin.RiskHigh); got != 2 {
			t.Fatalf("resumed window usage = %d, want the persisted consumption", got)
		}
		clock.advance(2 * time.Hour)
		if err := resumed.Admit(plugin.RiskHigh); err != nil {
			t.Fatalf("a rolled-out window must admit again: %v", err)
		}
	})

	t.Run("a restart replays the governance events back and keeps the records it made", func(t *testing.T) {
		root := t.TempDir()
		firstLog := recOpenLog(t, root)
		goals1 := governance.NewGoalRegistry()
		goals1.BindStore(firstLog)
		ledger1 := governance.NewDenialLedger()
		ledger1.BindStore(firstLog)

		g1 := goals1.Declare("ship the release", "entry", 0)
		g2 := goals1.Declare("watch the rollout", "entry", 0)
		if g1 != "g-1" || g2 != "g-2" {
			t.Fatalf("declared identities = (%q, %q), want the declaration-order identities", g1, g2)
		}
		if !goals1.Resolve(g1, governance.GoalAchieved) {
			t.Fatalf("resolve goal %s: the registry must accept the resolution", g1)
		}
		ledger1.Record(governance.DenialRecord{
			Subtype:  governance.SubtypeDenial,
			ToolName: "delete_file",
			Level:    plugin.RiskHigh,
			RuleID:   "no-delete",
			Reason:   govDeleteRuleReason,
			GoalID:   g1,
		})

		goalEvents, err := firstLog.Scan(context.Background(), "tagent/governance/v1/goal:")
		if err != nil {
			t.Fatalf("scan the goal events: %v", err)
		}
		var declared, resolved int
		for _, raw := range goalEvents {
			var evt event.Event
			if err := json.Unmarshal(raw, &evt); err != nil {
				t.Fatalf("parse stored goal event: %v", err)
			}
			switch evt.Meta[governance.MetaKeySubtype] {
			case governance.SubtypeGoalDeclared:
				declared++
			case governance.SubtypeGoalResolved:
				resolved++
			default:
				t.Fatalf("goal event subtype = %q, want goal_declared or goal_resolved", evt.Meta[governance.MetaKeySubtype])
			}
		}
		if declared != 2 || resolved != 1 {
			t.Fatalf("goal events = %d declared / %d resolved, want the two declarations and the one resolution on the wire", declared, resolved)
		}

		secondLog := recOpenLog(t, root)
		goals2 := governance.NewGoalRegistry()
		goals2.BindStore(secondLog)
		ledger2 := governance.NewDenialLedger()
		ledger2.BindStore(secondLog)

		if got := recGoal(t, goals2, g1).Status; got != governance.GoalAchieved {
			t.Fatalf("replayed goal %s status = %q, want achieved replayed in operation order", g1, got)
		}
		if got := recGoal(t, goals2, g2).Status; got != governance.GoalActive {
			t.Fatalf("replayed goal %s status = %q, want the still-in-flight goal rebuilt as active", g2, got)
		}
		if !goals2.HasActive() {
			t.Fatalf("the goal gate context must survive the restart")
		}
		if g3 := goals2.Declare("follow up", "entry", 0); g3 != "g-3" {
			t.Fatalf("post-restart declaration = %q, want the declaration order realigned past the replayed goals", g3)
		}
		if ledger2.Count() != 1 {
			t.Fatalf("replayed records = %d, want the refusal kept across the restart", ledger2.Count())
		}
		replayed := ledger2.Query(0)[0]
		if replayed.GoalID != g1 || replayed.Subtype != governance.SubtypeDenial || replayed.ToolName != "delete_file" {
			t.Fatalf("replayed record = %+v, want the refusal still permeating goal %s", replayed, g1)
		}
	})

	t.Run("concurrent declarations and records keep every counter exact", func(t *testing.T) {
		logFace := recOpenLog(t, "")
		goals := governance.NewGoalRegistry()
		goals.BindStore(logFace)
		ledger := governance.NewDenialLedger()
		ledger.BindStore(logFace)
		budget := governance.NewBudgetManager(governance.BudgetConfig{}, "")

		const workers, perWorker = 8, 10
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < perWorker; i++ {
					id := goals.Declare(fmt.Sprintf("goal %d/%d", w, i), "entry", 0)
					if id == "" {
						t.Errorf("declaration %d/%d returned no identity", w, i)
					}
					ledger.Record(governance.DenialRecord{
						Subtype:  governance.SubtypeDenial,
						ToolName: "shell_exec",
						Level:    plugin.RiskMedium,
						Reason:   "concurrent probe",
						GoalID:   id,
					})
					if err := budget.Admit(plugin.RiskMedium); err != nil {
						t.Errorf("admit under concurrency: %v", err)
					}
				}
			}(w)
		}
		wg.Wait()

		if got := len(goals.List()); got != workers*perWorker {
			t.Fatalf("declared goals = %d, want %d under concurrency", got, workers*perWorker)
		}
		seen := map[string]bool{}
		for _, goal := range goals.List() {
			if seen[goal.ID] {
				t.Fatalf("goal identity %s minted twice", goal.ID)
			}
			seen[goal.ID] = true
		}
		if got := ledger.Count(); got != workers*perWorker {
			t.Fatalf("ledger records = %d, want %d under concurrency", got, workers*perWorker)
		}
		if got := budget.Usage(plugin.RiskMedium); got != workers*perWorker {
			t.Fatalf("budget usage = %d, want %d counted under concurrency", got, workers*perWorker)
		}
		goalEvents, err := logFace.Scan(context.Background(), "tagent/governance/v1/goal:")
		if err != nil {
			t.Fatalf("scan the goal events: %v", err)
		}
		if len(goalEvents) != workers*perWorker {
			t.Fatalf("goal events = %d, want one ordered key per declaration with no collision", len(goalEvents))
		}
	})

	t.Run("a failing store and a bad snapshot degrade instead of failing the gate", func(t *testing.T) {
		failing := recFailingLog{}
		goals := governance.NewGoalRegistry()
		goals.BindStore(failing)
		ledger := governance.NewDenialLedger()
		ledger.BindStore(failing)
		if id := goals.Declare("keep going", "entry", 0); id == "" {
			t.Fatalf("a failing store must not swallow the declaration")
		}
		ledger.Record(governance.DenialRecord{Subtype: governance.SubtypeDenial, ToolName: "shell_exec", Level: plugin.RiskMedium, Reason: "store down"})
		if ledger.Count() != 1 || len(goals.List()) != 1 {
			t.Fatalf("a failing store must never block the operation: goals=%d records=%d", len(goals.List()), ledger.Count())
		}
		memGoals := governance.NewGoalRegistry()
		memGoals.BindStore(nil)
		memLedger := governance.NewDenialLedger()
		memLedger.BindStore(nil)
		if memGoals.Declare("memory only", "entry", 0) == "" {
			t.Fatalf("a registry without a store must degrade to memory state")
		}
		memLedger.Record(governance.DenialRecord{Subtype: governance.SubtypeDenial, ToolName: "shell_exec", Level: plugin.RiskMedium, Reason: "memory only"})
		if memLedger.Count() != 1 {
			t.Fatalf("a ledger without a store must degrade to memory state, records = %d", memLedger.Count())
		}

		cfg := governance.BudgetConfig{MaxHighRisk: 2}
		corrupt := t.TempDir()
		if err := os.WriteFile(filepath.Join(corrupt, "budget.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write the corrupted snapshot: %v", err)
		}
		if err := governance.NewBudgetManager(cfg, corrupt).Admit(plugin.RiskHigh); err != nil {
			t.Fatalf("a corrupted snapshot must degrade to a fresh window instead of failing: %v", err)
		}
		forged := t.TempDir()
		if err := os.WriteFile(filepath.Join(forged, "budget.json"), []byte(`{"epoch":0,"buckets":{"high":[9,9,9,9,9,9]}}`), 0o644); err != nil {
			t.Fatalf("write the forged snapshot: %v", err)
		}
		if got := governance.NewBudgetManager(cfg, forged).Usage(plugin.RiskHigh); got != 0 {
			t.Fatalf("an epoch-less snapshot must be wholly untrusted: usage = %d, want the forged consumption never adopted", got)
		}
	})
}

// recJudge rules over one plain high-grade call the way the dispatch admits it:
// the trigger source the goal gate matches on and the grade the one Before pass
// carries.
func recJudge(t *testing.T, policy *governance.Policy, callID, args string) plugin.Decision {
	t.Helper()
	call := model.ToolCall{ID: callID, Type: "function", Function: model.FunctionDefinitionParam{Name: "shell_exec", Arguments: []byte(args)}}
	pass := plugin.CallPass{Final: call, Risk: plugin.RiskHigh, TriggerSource: "model"}
	decision, err := policy.Judge(pass)
	if err != nil {
		t.Fatalf("rule over call %s: %v", callID, err)
	}
	return decision
}

// recOpenLog opens the durable governance event face over the real memory
// tier: the in-memory tier when root is empty, the localfile tier otherwise.
// Reopening the same root is the restart these sub-cases recover across.
func recOpenLog(t *testing.T, root string) governance.GovernanceEventLog {
	t.Helper()
	cfg := ledger.StorageConfig{Type: ledger.StorageMemory}
	if root != "" {
		cfg = ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}
	}
	tier, err := ledger.OpenTier(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open the memory tier under %q: %v", root, err)
	}
	return ledger.TierFactLog(tier)
}

// recFind reports the newest ledger record filed under the subtype, or nil
// when the ledger holds none.
func recFind(ledger *governance.DenialLedger, subtype string) *governance.DenialRecord {
	for _, rec := range ledger.Query(0) {
		if rec.Subtype == subtype {
			rec := rec
			return &rec
		}
	}
	return nil
}

// recGoal reports the goal the registry holds under the identity, failing the
// sub-case when none is found.
func recGoal(t *testing.T, goals *governance.GoalRegistry, id string) *governance.Goal {
	t.Helper()
	for _, goal := range goals.List() {
		if goal.ID == id {
			return goal
		}
	}
	t.Fatalf("goal %s is not in the registry", id)
	return nil
}

// recDeployRuleReason is the correction material the deploy rule records.
const recDeployRuleReason = "deploying unattended is restricted and must not run"

// recDeployRule returns the violation rule these sub-cases rule over: an
// autonomous deploy at the medium grade or above is a violation of the gate.
func recDeployRule() governance.RiskRule {
	return governance.RiskRule{
		ID:      "no-deploy",
		MinRisk: plugin.RiskMedium,
		Match: func(rc governance.RiskContext) bool {
			return rc.ToolName == "shell_exec" && strings.Contains(string(rc.ArgsJSON), "deploy")
		},
		Reason: recDeployRuleReason,
	}
}

// recFailingLog is a governance event face whose writes always fail: the
// durable half degrades beside it instead of failing the operation.
type recFailingLog struct{}

// Put fails, as the store is down.
func (recFailingLog) Put(ctx context.Context, key string, value []byte) error {
	return errors.New("rec failing log: the store is down")
}

// Scan fails, as the store is down.
func (recFailingLog) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	return nil, errors.New("rec failing log: the store is down")
}

// Sync fails, as the store is down.
func (recFailingLog) Sync(ctx context.Context) error {
	return errors.New("rec failing log: the store is down")
}
