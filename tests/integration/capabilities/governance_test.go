package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestGovernanceBeforeExecution drives the pre-execution governance surface over the real plugin pipeline and task wrapper:
// - warn records and exposes a rule violation without blocking the execution
// - a gate that is not enabled lets a call through with nothing ruled on it
// - strict refuses a violating call before execution and pairs the refusal with a named native result
// - a critical call is held under both grades: a hold is a task that executed nothing, never a denial
// - the approval request is written to its own file under the approval identity the hold carries
// - an approval response binds the held identity and argument digest; a repeated response is idempotent
// - an approved hold proceeds on retry; a changed-argument or expired approval is held again
// - a denied approval refuses the retried call with a named paired result and zero execution
// - a failed channel delivery never blocks the gate: the request stays answerable through the other entry
// - the ruling runs exactly once per call and consumes the risk grade without a second risk face
// - the refusal facts are the native call's own: same CallID, same tool, same arguments
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
func TestGovernanceBeforeExecution(t *testing.T) {
	t.Run("warn records and exposes a rule violation without blocking the execution", func(t *testing.T) {
		rig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskMedium, tool: "delete_file", rules: []governance.RiskRule{govDeleteRule()}})
		out, err := rig.call("call-warn", "delete_file", `{"path":"probe.txt"}`)
		if err != nil {
			t.Fatalf("the warn grade must not refuse the call: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call answer = %T, want the admission receipt of a call that runs", out)
		}
		res := rig.awaitRecorded()
		if res.Denied != nil || res.Output == nil || res.Tool != "delete_file" {
			t.Fatalf("recorded result = %+v, want the execution recorded under the native tool name with no refusal", res)
		}
		if rig.inner.executions() != 1 {
			t.Fatalf("tool executions = %d, want the violating call still executed exactly once", rig.inner.executions())
		}
		d := rig.decision()
		if d.Disposition != plugin.DispositionRecord || d.Rule != "no-delete" || d.Reason != govDeleteRuleReason {
			t.Fatalf("warn ruling = %+v, want the violation recorded and exposed beside the rule it came from", d)
		}
		if d.Refused() {
			t.Fatalf("warn ruling = %+v, want a recorded violation that is never a refusal", d)
		}
	})

	t.Run("a gate that is not enabled lets every call through", func(t *testing.T) {
		rig := newGovRig(t, govConfig{disabled: true, grade: plugin.RiskCritical, tool: "delete_file", rules: []governance.RiskRule{govDeleteRule()}})
		out, err := rig.call("call-off", "delete_file", `{"path":"probe.txt"}`)
		if err != nil {
			t.Fatalf("a gate that is not enabled must not refuse the call: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call answer = %T, want the admission receipt of a call that runs", out)
		}
		if res := rig.awaitRecorded(); res.Denied != nil {
			t.Fatalf("recorded result = %+v, want no refusal beside a gate that is not enabled", res)
		}
		d := rig.decision()
		if d.Disposition != plugin.DispositionAllow || d.Rule != "" {
			t.Fatalf("ungated ruling = %+v, want the call allowed with no rule duty", d)
		}
	})

	t.Run("strict refuses a violating call before execution and pairs the refusal with a named native result", func(t *testing.T) {
		args := `{"path":"probe.txt"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskMedium, tool: "delete_file", rules: []governance.RiskRule{govDeleteRule()}})
		out, err := rig.call("call-strict", "delete_file", args)
		if !errors.Is(err, task.ErrDenied) {
			t.Fatalf("call error = %v, want the refusal reported beside %v", err, task.ErrDenied)
		}
		if out != nil {
			t.Fatalf("call answer = %+v, want no answer beside a refusal", out)
		}
		if rig.inner.executions() != 0 {
			t.Fatalf("tool executions = %d, want the refused call to execute nothing", rig.inner.executions())
		}
		res := rig.awaitRecorded()
		if res.CallID != "call-strict" || res.Tool != "delete_file" || res.Denied == nil || res.Denied.Tool != "delete_file" {
			t.Fatalf("recorded result = %+v, want the named paired result of the refused call", res)
		}
		if !strings.Contains(res.Denied.Reason, govDeleteRuleReason) {
			t.Fatalf("refusal reason = %q, want the rule reason the model reads to correct itself", res.Denied.Reason)
		}
		d := rig.decision()
		if !d.Refused() {
			t.Fatalf("strict ruling = %+v, want a terminal refusal carrying its denial", d)
		}
		den := d.Denial
		if den.CallID != "call-strict" || den.Tool != "delete_file" || den.Rule != "no-delete" {
			t.Fatalf("denial = %+v, want the refusal facts naming the native call the model issued", den)
		}
		if den.Result.Role != model.RoleTool || den.Result.ToolID != "call-strict" || den.Result.ToolName != "delete_file" {
			t.Fatalf("denial result = %+v, want the native paired result over the very call identity and tool", den.Result)
		}
		if !strings.Contains(den.Result.Content, govDeleteRuleReason) || !strings.Contains(den.Result.Content, "no-delete") {
			t.Fatalf("denial content = %q, want the reason and the rule for the model to read", den.Result.Content)
		}
		if approval.ArgsDigest([]byte(args)) != task.DigestArgs([]byte(args)) {
			t.Fatalf("the policy digest and the task digest disagree over the same native arguments")
		}
	})

	t.Run("a critical call is held awaiting approval under both enforcement grades and the hold is never a denial", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		for _, mode := range []governance.Enforcement{governance.EnforcementWarn, governance.EnforcementStrict} {
			rig := newGovRig(t, govConfig{mode: mode, grade: plugin.RiskCritical})
			out, err := rig.call("call-hold", "shell_exec", args)
			if err != nil {
				t.Fatalf("%s: a held call answers with its wait and no error: %v", mode, err)
			}
			receipt, ok := out.(task.HoldReceipt)
			if !ok {
				t.Fatalf("%s: call answer = %T, want the waiting receipt of a held call", mode, out)
			}
			if receipt.CallID != "call-hold" || receipt.Hold.ApprovalID == "" {
				t.Fatalf("%s: hold receipt = %+v, want the wait beside its approval identity", mode, receipt)
			}
			if receipt.Hold.ExpectedDigest != task.DigestArgs([]byte(args)) {
				t.Fatalf("%s: hold digest = %q, want the digest of the arguments that were held", mode, receipt.Hold.ExpectedDigest)
			}
			if parked, ok := rig.wrapper.Holds().Waiting(receipt.Hold.TaskID); !ok || parked.ApprovalID != receipt.Hold.ApprovalID {
				t.Fatalf("%s: waiting table = (%+v, %v), want the hold registered as a waiting task", mode, parked, ok)
			}
			if rig.inner.executions() != 0 {
				t.Fatalf("%s: tool executions = %d, want the held call to have executed nothing", mode, rig.inner.executions())
			}
			if recorded := rig.recordedSnapshot(); len(recorded) != 0 {
				t.Fatalf("%s: recorded results = %+v, want no refusal result beside a wait", mode, recorded)
			}
			d := rig.decision()
			if d.Disposition != plugin.DispositionHold || d.Refused() {
				t.Fatalf("%s: ruling = %+v, want a hold that is a wait and never a denial", mode, d)
			}
			req := rig.registerApproval(receipt.Hold, "shell_exec", args)
			if req.Status != approval.ApprovalPending {
				t.Fatalf("%s: approval request = %+v, want the request waiting for a ruling", mode, req)
			}
			if waiting := rig.approvals.Pending(); len(waiting) != 1 || waiting[0].ID != req.ID {
				t.Fatalf("%s: pending requests = %+v, want the held call visible on the waiting face", mode, waiting)
			}
		}
	})

	t.Run("an approval response is bound to the held identity and digest and a repeated response is idempotent", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskCritical})
		out, err := rig.call("call-bind", "shell_exec", args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		receipt := out.(task.HoldReceipt)
		req := rig.registerApproval(receipt.Hold, "shell_exec", args)

		raw, err := os.ReadFile(filepath.Join(rig.approvalDir(), req.ID+".json"))
		if err != nil {
			t.Fatalf("read the approval request file: %v", err)
		}
		var onDisk approval.ApprovalRequest
		if err := json.Unmarshal(raw, &onDisk); err != nil {
			t.Fatalf("parse the approval request file: %v", err)
		}
		if onDisk.ArgsDigest != receipt.Hold.ExpectedDigest || onDisk.ArgsDigest != approval.ArgsDigest([]byte(args)) {
			t.Fatalf("request on disk = %+v, want the digest binding over the held arguments beside the policy-minted identity", onDisk)
		}

		first, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "cli:alice")
		if err != nil || first.Status != approval.ApprovalApproved || first.DecidedBy != "cli:alice" {
			t.Fatalf("first ruling = (%+v, %v), want the approval recorded", first, err)
		}
		second, err := rig.approvals.Decide(req.ID, approval.ApprovalDenied, "cli:bob")
		if err != nil || second.Status != approval.ApprovalApproved || second.DecidedBy != "cli:alice" {
			t.Fatalf("repeated ruling = (%+v, %v), want the repeat idempotent beside the first ruling that took effect", second, err)
		}

		answer := task.ApprovalAnswer{ApprovalID: receipt.Hold.ApprovalID, ExpectedDigest: receipt.Hold.ExpectedDigest, Granted: true, Reason: "cli:alice"}
		resolved, err := rig.wrapper.Holds().Resolve(receipt.Hold.TaskID, answer)
		if err != nil || !resolved.Granted {
			t.Fatalf("hold resolution = (%+v, %v), want the granting response to take effect", resolved, err)
		}
		repeat, err := rig.wrapper.Holds().Resolve(receipt.Hold.TaskID, task.ApprovalAnswer{ApprovalID: receipt.Hold.ApprovalID, ExpectedDigest: receipt.Hold.ExpectedDigest, Granted: false, Reason: "cli:bob"})
		if err != nil || repeat.Granted != true || repeat.Reason != "cli:alice" {
			t.Fatalf("repeated resolution = (%+v, %v), want the repeat reporting the first response instead of consuming a second", repeat, err)
		}
		if _, err := rig.wrapper.Holds().Resolve(receipt.Hold.TaskID, task.ApprovalAnswer{ApprovalID: receipt.Hold.ApprovalID, ExpectedDigest: task.DigestArgs([]byte(`{"cmd":"other"}`)), Granted: true}); !errors.Is(err, task.ErrApprovalDigestMismatch) {
			t.Fatalf("changed-arguments resolution error = %v, want the digest mismatch refused by name", err)
		}
		if _, err := rig.wrapper.Holds().Resolve(receipt.Hold.TaskID, task.ApprovalAnswer{ApprovalID: "approval-elsewhere", ExpectedDigest: receipt.Hold.ExpectedDigest, Granted: true}); !errors.Is(err, task.ErrApprovalIdentityMismatch) {
			t.Fatalf("foreign-identity resolution error = %v, want the identity mismatch refused by name", err)
		}

		rebuilt := approval.NewApprovalManager(rig.dir, 30*time.Minute, approval.WithApprovalClock(rig.clock.current), approval.WithApprovalRescanInterval(0))
		seen := rebuilt.Lookup("shell_exec", approval.ArgsDigest([]byte(args)))
		if seen == nil || seen.ID != req.ID || seen.Status != approval.ApprovalApproved {
			t.Fatalf("rebuilt lookup = %+v, want the approved request still visible after a rebuild", seen)
		}
	})

	t.Run("an approved hold proceeds on retry while a changed-argument retry is held again", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskCritical})
		out, err := rig.call("call-approved", "shell_exec", args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		receipt := out.(task.HoldReceipt)
		req := rig.registerApproval(receipt.Hold, "shell_exec", args)
		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "cli:alice"); err != nil {
			t.Fatalf("approve the hold: %v", err)
		}

		out, err = rig.call("call-retry", "shell_exec", args)
		if err != nil {
			t.Fatalf("the approved call must run on retry: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("retry answer = %T, want the admission receipt of the call that runs", out)
		}
		res := rig.awaitRecorded()
		if res.Output == nil || res.Denied != nil {
			t.Fatalf("recorded result = %+v, want the execution recorded after the approval", res)
		}
		if rig.inner.executions() != 1 || rig.inner.last() != args {
			t.Fatalf("executions = (%d, %q), want the inner tool run once over the very held arguments", rig.inner.executions(), rig.inner.last())
		}
		if d := rig.decision(); d.Disposition != plugin.DispositionRecord || !strings.Contains(d.Reason, "approved") {
			t.Fatalf("retried ruling = %+v, want the approved call recorded as it runs", d)
		}

		out, err = rig.call("call-changed", "shell_exec", `{"cmd":"rm -rf /"}`)
		if err != nil {
			t.Fatalf("a changed call answers with its wait and no error: %v", err)
		}
		if _, ok := out.(task.HoldReceipt); !ok {
			t.Fatalf("changed-call answer = %T, want the call held again over the changed arguments", out)
		}
		if rig.inner.executions() != 1 {
			t.Fatalf("tool executions = %d, want the changed call to have executed nothing", rig.inner.executions())
		}
		if d := rig.decision(); d.Disposition != plugin.DispositionHold || d.Refused() {
			t.Fatalf("changed-call ruling = %+v, want a fresh wait, never an approval over other arguments", d)
		}
	})

	t.Run("a denied approval refuses the retried call with a named paired result and zero execution", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskCritical})
		out, err := rig.call("call-denied", "shell_exec", args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		receipt := out.(task.HoldReceipt)
		req := rig.registerApproval(receipt.Hold, "shell_exec", args)
		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalDenied, "cli:bob"); err != nil {
			t.Fatalf("deny the hold: %v", err)
		}

		out, err = rig.call("call-after-denial", "shell_exec", args)
		if !errors.Is(err, task.ErrDenied) {
			t.Fatalf("retry error = %v, want the denied call refused by name", err)
		}
		if out != nil {
			t.Fatalf("retry answer = %+v, want no answer beside the refusal", out)
		}
		if rig.inner.executions() != 0 {
			t.Fatalf("tool executions = %d, want the denied call to execute nothing", rig.inner.executions())
		}
		res := rig.awaitRecorded()
		if res.CallID != "call-after-denial" || res.Tool != "shell_exec" || res.Denied == nil {
			t.Fatalf("recorded result = %+v, want the named paired result of the refused call", res)
		}
		if !strings.Contains(res.Denied.Reason, "denied") {
			t.Fatalf("refusal reason = %q, want the denied approval named for the model to read", res.Denied.Reason)
		}
		if d := rig.decision(); !d.Refused() {
			t.Fatalf("denied ruling = %+v, want a terminal refusal carrying its denial", d)
		}
	})

	t.Run("an expired approval is treated as not approved and never proceeds on the old request", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskCritical, ttl: 30 * time.Minute})
		out, err := rig.call("call-expiring", "shell_exec", args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		receipt := out.(task.HoldReceipt)
		req := rig.registerApproval(receipt.Hold, "shell_exec", args)

		rig.clock.advance(31 * time.Minute)
		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "cli:late"); !errors.Is(err, approval.ErrApprovalExpired) {
			t.Fatalf("late ruling error = %v, want the expired request refused beside %v", err, approval.ErrApprovalExpired)
		}
		seen := rig.approvals.Lookup("shell_exec", approval.ArgsDigest([]byte(args)))
		if seen == nil || seen.Status != approval.ApprovalExpired {
			t.Fatalf("lookup after validity = %+v, want the expired state read as not approved", seen)
		}

		out, err = rig.call("call-after-expiry", "shell_exec", args)
		if err != nil {
			t.Fatalf("an expired approval answers with a fresh wait and no error: %v", err)
		}
		if _, ok := out.(task.HoldReceipt); !ok {
			t.Fatalf("retry answer = %T, want the call held again instead of proceeding on the old request", out)
		}
		if rig.inner.executions() != 0 {
			t.Fatalf("tool executions = %d, want nothing to proceed on the expired request", rig.inner.executions())
		}
	})

	t.Run("a failed channel delivery never blocks the gate and the request stays answerable through the other entry", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		failing := &govChannel{err: errors.New("the wechat channel is unreachable")}
		working := &govChannel{}
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskCritical, channels: []approval.ApprovalChannel{failing, working}})
		out, err := rig.call("call-channel", "shell_exec", args)
		if err != nil {
			t.Fatalf("the gate must keep waiting beside a failed delivery: %v", err)
		}
		if _, ok := out.(task.HoldReceipt); !ok {
			t.Fatalf("call answer = %T, want the waiting receipt of the gate that kept waiting", out)
		}
		receipt := out.(task.HoldReceipt)
		req := rig.registerApproval(receipt.Hold, "shell_exec", args)
		if req == nil || req.Status != approval.ApprovalPending {
			t.Fatalf("registration = %+v, want the request registered beside the failed delivery", req)
		}
		gaps := rig.approvals.DeliveryGaps()
		if len(gaps) != 1 || gaps[0].Request != req.ID || gaps[0].Err == nil {
			t.Fatalf("delivery gaps = %+v, want the failed delivery recorded as a visible gap", gaps)
		}
		if working.count() != 1 {
			t.Fatalf("working channel deliveries = %d, want the request offered to every attached channel", working.count())
		}

		path := filepath.Join(rig.approvalDir(), req.ID+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the approval request file: %v", err)
		}
		var external approval.ApprovalRequest
		if err := json.Unmarshal(raw, &external); err != nil {
			t.Fatalf("parse the approval request file: %v", err)
		}
		external.Status = approval.ApprovalApproved
		external.DecidedBy = "file:external"
		written, err := json.Marshal(&external)
		if err != nil {
			t.Fatalf("encode the external ruling: %v", err)
		}
		if err := os.WriteFile(path, written, 0o644); err != nil {
			t.Fatalf("write the external ruling: %v", err)
		}

		out, err = rig.call("call-after-external", "shell_exec", args)
		if err != nil {
			t.Fatalf("the externally approved call must run: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("retry answer = %T, want the admission receipt of the call the external ruling approved", out)
		}
		if res := rig.awaitRecorded(); res.Output == nil || res.Denied != nil {
			t.Fatalf("recorded result = %+v, want the approved execution recorded", res)
		}
		if rig.inner.executions() != 1 {
			t.Fatalf("tool executions = %d, want the approved call executed exactly once", rig.inner.executions())
		}
	})

	t.Run("the pre-execution ruling runs exactly once per call and consumes the risk grade without a second risk face", func(t *testing.T) {
		rules := []governance.RiskRule{govDeleteRule()}

		low := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskLow, tool: "delete_file", rules: rules})
		if _, err := low.call("call-low", "delete_file", `{"path":"probe.txt"}`); err != nil {
			t.Fatalf("the low grade must not refuse the call: %v", err)
		}
		if res := low.awaitRecorded(); res.Denied != nil {
			t.Fatalf("recorded result = %+v, want no refusal below the rule's grade threshold", res)
		}
		if d := low.decision(); d.Rule != "" {
			t.Fatalf("low-grade ruling = %+v, want no rule fired below its grade threshold", d)
		}
		mid := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskMedium, tool: "delete_file", rules: rules})
		if _, err := mid.call("call-mid", "delete_file", `{"path":"probe.txt"}`); err != nil {
			t.Fatalf("the warn grade must not refuse the call: %v", err)
		}
		if res := mid.awaitRecorded(); res.Denied != nil {
			t.Fatalf("recorded result = %+v, want the violation recorded, not refused", res)
		}
		if d := mid.decision(); d.Rule != "no-delete" {
			t.Fatalf("mid-grade ruling = %+v, want the rule fired at its grade threshold", d)
		}

		renders := mid.grader.renders()
		call := model.ToolCall{ID: "call-standalone", Type: "function", Function: model.FunctionDefinitionParam{Name: "delete_file", Arguments: []byte(`{"path":"probe.txt"}`)}}
		standalone, err := mid.policy.Judge(plugin.CallPass{Final: call})
		if err != nil {
			t.Fatalf("judge the standalone call: %v", err)
		}
		if standalone.Rule != "no-delete" || standalone.Risk != plugin.RiskMedium {
			t.Fatalf("standalone ruling = %+v, want the conservative fallback grade consuming the rule", standalone)
		}
		if mid.grader.renders() != renders {
			t.Fatalf("risk-face renders = %d, want the ruling to consume the rendered grade and never render a second one", mid.grader.renders())
		}

		sourceRule := governance.RiskRule{
			ID:      "no-autonomous-delete",
			MinRisk: plugin.RiskLow,
			Match: func(rc governance.RiskContext) bool {
				return rc.ToolName == "delete_file" && rc.TriggerSource == "autonomous"
			},
			Reason: "an autonomous delete must not run unattended",
		}
		sourced := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskLow, rules: []governance.RiskRule{sourceRule}})
		pass := plugin.CallPass{Final: call, Risk: plugin.RiskLow, TriggerSource: "autonomous"}
		underSource, err := sourced.policy.Judge(pass)
		if err != nil {
			t.Fatalf("judge under the autonomous trigger: %v", err)
		}
		if underSource.Rule != "no-autonomous-delete" {
			t.Fatalf("trigger ruling = %+v, want the rule matching the trigger source the call carries", underSource)
		}
		underModel, err := sourced.policy.Judge(plugin.CallPass{Final: call, Risk: plugin.RiskLow, TriggerSource: "model"})
		if err != nil {
			t.Fatalf("judge under the model trigger: %v", err)
		}
		if underModel.Rule != "" {
			t.Fatalf("trigger ruling = %+v, want no rule fired for a trigger source it does not match", underModel)
		}

		if low.judgeCount() != 1 || mid.judgeCount() != 1 {
			t.Fatalf("ruling counts = (%d, %d), want exactly one pre-execution ruling per call", low.judgeCount(), mid.judgeCount())
		}
		if low.grader.renders() != 1 || mid.grader.renders() != renders {
			t.Fatalf("risk-face renders = (%d, %d), want one render per call from the one Before pass and none from a ruling", low.grader.renders(), mid.grader.renders())
		}
	})
}

// govConfig is the configuration one governance rig is built over.
type govConfig struct {
	// disabled builds the rig over a gate that is not enabled.
	disabled bool
	// mode is the enforcement grade the gate runs under.
	mode governance.Enforcement
	// grade is the risk grade the risk face renders.
	grade plugin.RiskLevel
	// rules are the violation rules the gate rules over.
	rules []governance.RiskRule
	// goals is the goal registry the goal gate rules over.
	goals *governance.GoalRegistry
	// ledger is the governance ledger the rulings record onto.
	ledger *governance.DenialLedger
	// goalRequired lists the trigger sources an operation must hang on an
	// active goal for.
	goalRequired []string
	// tool is the name of the inner native tool the rig wraps, kept the same as
	// the tool the model calls so the refusal facts name the native tool the
	// call was made over. Zero means shell_exec.
	tool string
	// channels are the delivery faces the approval manager offers to.
	channels []approval.ApprovalChannel
	// ttl is the validity an approval request is issued with; zero means the
	// default half hour.
	ttl time.Duration
}

// govRig is the governance rig of these sub-cases: the real plugin pipeline,
// the real policy and approval manager, and the real task wrapper over one
// instrumented native tool, wired the way the dispatch drives a call — the one
// Before pass of the pipeline, then the wrapped call under the frame and the
// pass it produced, with the pre-execution ruling running at admission.
type govRig struct {
	t          *testing.T
	dir        string
	policy     *governance.Policy
	approvals  *approval.ApprovalManager
	pipeline   plugin.Pipeline
	grader     *govGrader
	inner      *govTool
	wrapper    *task.Wrapper
	clock      *govClock
	records    chan task.Result
	recordings []task.Result
	mu         sync.Mutex
	judgments  int
	lastRuling plugin.Decision
}

// newGovRig builds one governance rig over the given configuration.
func newGovRig(t *testing.T, cfg govConfig) *govRig {
	t.Helper()
	ttl := cfg.ttl
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if cfg.tool == "" {
		cfg.tool = "shell_exec"
	}
	r := &govRig{
		t:       t,
		dir:     t.TempDir(),
		grader:  &govGrader{name: "risk-grader", grade: cfg.grade},
		inner:   &govTool{name: cfg.tool},
		clock:   &govClock{now: time.Now()},
		records: make(chan task.Result, 16),
	}
	r.approvals = approval.NewApprovalManager(r.dir, ttl,
		approval.WithApprovalClock(r.clock.current),
		approval.WithApprovalRescanInterval(0),
	)
	for _, ch := range cfg.channels {
		r.approvals.AddChannel(ch)
	}
	r.policy = policyOf(cfg, r.approvals)
	pipeline, err := plugin.NewFlow([]plugin.Instance{r.grader})
	if err != nil {
		t.Fatalf("assemble the plugin pipeline: %v", err)
	}
	r.pipeline = pipeline
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
	manager := task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		return task.DeliveryAccepted, nil
	})
	r.wrapper = task.Wrap(manager, r.inner,
		task.WithResultRecorder(r.record),
		task.WithMailbox(func(ctx context.Context, notice task.Notice) error { return nil }),
		task.WithApprover(r.ruling),
	)
	return r
}

// policyOf builds the gate the rig rules under: an unopened gate lets every
// call through, and every other rig opens the gate with the gates its
// configuration names.
func policyOf(cfg govConfig, approvals *approval.ApprovalManager) *governance.Policy {
	if cfg.disabled {
		return &governance.Policy{}
	}
	opts := []governance.PolicyOption{
		governance.WithRules(cfg.rules...),
		governance.WithApprovals(approvals),
		governance.WithGoals(cfg.goals),
		governance.WithLedger(cfg.ledger),
		governance.WithGoalRequired(cfg.goalRequired...),
	}
	if cfg.mode == governance.EnforcementStrict {
		opts = append(opts, governance.WithStrict())
	}
	return governance.Open(opts...)
}

// call drives one native tool call the way the dispatch does: the one Before
// pass of the plugin pipeline, then the wrapped call under its frame and the
// pass the pipeline produced. Every call checks that the pre-execution ruling
// ran exactly once for it.
func (r *govRig) call(callID, tool, args string) (any, error) {
	r.t.Helper()
	before := r.judgeCount()
	ctx := context.Background()
	call := model.ToolCall{
		ID:   callID,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      tool,
			Arguments: []byte(args),
		},
	}
	pass, err := r.pipeline.Before(ctx, call)
	if err != nil {
		r.t.Fatalf("run the one Before pass of call %s: %v", callID, err)
	}
	pass.TriggerSource = "model"
	out, callErr := r.wrapper.Call(task.WithCallFrame(governance.WithCallPass(ctx, pass), govFrame(callID)), pass.Final.Function.Arguments)
	if got := r.judgeCount() - before; got != 1 {
		r.t.Errorf("pre-execution rulings for call %s = %d, want exactly one", callID, got)
	}
	return out, callErr
}

// ruling is the approver face the wrapper admits a call under: it renders the
// single pre-execution ruling over the Before pass the context carries and
// reports the class the wrapper admits the call under. The refusal is read
// first — a refused call is terminal and carries its denial — while a held call
// is a wait and never a denial.
func (r *govRig) ruling(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
	pass, ok := governance.CallPassFrom(ctx)
	if !ok {
		return task.Ruling{}, errors.New("governance rig: the call carries no Before pass to rule over")
	}
	decision, err := r.policy.Judge(pass)
	if err != nil {
		return task.Ruling{}, err
	}
	r.mu.Lock()
	r.judgments++
	r.lastRuling = decision
	r.mu.Unlock()
	switch {
	case decision.Refused():
		return task.Ruling{Kind: task.RulingDeny, Reason: decision.Reason}, nil
	case decision.Disposition == plugin.DispositionHold:
		return task.Ruling{Kind: task.RulingHold, Reason: decision.Reason}, nil
	default:
		return task.Ruling{Kind: task.RulingRun, Reason: decision.Reason}, nil
	}
}

// record is the result recorder of the rig: it keeps every recorded result and
// hands it to the sub-case awaiting it.
func (r *govRig) record(ctx context.Context, res task.Result) (event.EventID, error) {
	r.mu.Lock()
	r.recordings = append(r.recordings, res)
	r.mu.Unlock()
	r.records <- res
	return event.EventID("fact-" + res.CallID), nil
}

// registerApproval plays the wiring beside the park: it registers the approval
// request under the very approval identity the hold was issued with, so a host
// response names one identity across the hold and the request.
func (r *govRig) registerApproval(hold task.Hold, tool, args string) *approval.ApprovalRequest {
	r.t.Helper()
	d := r.decision()
	if req := r.approvals.Lookup(tool, approval.ArgsDigest([]byte(args))); req != nil {
		return req
	}
	req, err := r.approvals.Request(context.Background(), hold.ApprovalID, tool, []byte(args), d.Risk, d.Rule, d.Reason)
	if err != nil {
		r.t.Fatalf("register the approval request: %v", err)
	}
	return req
}

// awaitRecorded reports the next recorded result, failing the sub-case when no
// result arrives.
func (r *govRig) awaitRecorded() task.Result {
	r.t.Helper()
	select {
	case res := <-r.records:
		return res
	case <-time.After(5 * time.Second):
		r.t.Fatal("the recorded result did not arrive")
		return task.Result{}
	}
}

// decision reports the last pre-execution ruling the rig rendered.
func (r *govRig) decision() plugin.Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRuling
}

// judgeCount reports how many pre-execution rulings the rig rendered.
func (r *govRig) judgeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.judgments
}

// recordedSnapshot reports the results recorded so far.
func (r *govRig) recordedSnapshot() []task.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]task.Result(nil), r.recordings...)
}

// approvalDir reports the directory the approval requests are written under.
func (r *govRig) approvalDir() string {
	return filepath.Join(r.dir, "approvals")
}

// govFrame returns the admission identity one call of these sub-cases is
// admitted under.
func govFrame(callID string) task.CallFrame {
	return task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-gov",
			InvocationID: "invocation-gov",
			DecisionID:   "decision-gov",
			CallID:       callID,
		},
		Address:   event.Address{AgentID: "agent-gov", SessionID: "session-gov"},
		Execution: "serial",
	}
}

// govDeleteRule returns the violation rule these sub-cases rule over: a delete
// at the medium grade or above is a violation of the gate.
func govDeleteRule() governance.RiskRule {
	return governance.RiskRule{
		ID:      "no-delete",
		MinRisk: plugin.RiskMedium,
		Match: func(rc governance.RiskContext) bool {
			return rc.ToolName == "delete_file"
		},
		Reason: govDeleteRuleReason,
	}
}

// govDeleteRuleReason is the correction material the model reads beside the
// rule these sub-cases rule over.
const govDeleteRuleReason = "the delete_file tool is restricted and must not run unattended"

// govTool is the inner native tool the wrapped call delegates to. It records
// every execution and the arguments it ran over, so a sub-case can tell a call
// that ran from one that never did.
type govTool struct {
	name string
	mu   sync.Mutex
	runs int
	args string
}

// Declaration returns the declaration of the inner native tool.
func (g *govTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: g.name}
}

// Call runs the inner native tool over the given arguments.
func (g *govTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	g.mu.Lock()
	g.runs++
	g.args = string(jsonArgs)
	g.mu.Unlock()
	return "ran:" + string(jsonArgs), nil
}

// executions reports how often the inner tool ran.
func (g *govTool) executions() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runs
}

// last reports the arguments the inner tool last ran over.
func (g *govTool) last() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.args
}

// govGrader is the risk face of the pipeline: it renders one fixed grade per
// call and counts its renders, so a sub-case can prove the risk face ran once
// per call and never inside a ruling.
type govGrader struct {
	name  string
	grade plugin.RiskLevel
	mu    sync.Mutex
	runs  int
}

// Name reports the stable name of the risk face.
func (g *govGrader) Name() string { return g.name }

// Port reports the call->ruling port the risk face serves.
func (g *govGrader) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductRuling}
}

// AssessRisk grades one tool call before its side effect runs.
func (g *govGrader) AssessRisk(ctx context.Context, call *model.ToolCall) plugin.RiskLevel {
	g.mu.Lock()
	g.runs++
	g.mu.Unlock()
	return g.grade
}

// renders reports how often the risk face rendered a grade.
func (g *govGrader) renders() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runs
}

// govChannel is one delivery face of the approval channel: it records what it
// was offered, or fails with the configured error.
type govChannel struct {
	err       error
	mu        sync.Mutex
	delivered []approval.ApprovalRequest
}

// Deliver offers one approval request to this channel.
func (c *govChannel) Deliver(ctx context.Context, req approval.ApprovalRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.delivered = append(c.delivered, req)
	return nil
}

// count reports how many requests this channel was offered.
func (c *govChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.delivered)
}

// govClock is the injectable clock the approval manager reads its time from,
// so a sub-case drives validity windows deterministically instead of sleeping.
type govClock struct {
	mu  sync.Mutex
	now time.Time
}

// current reports the current time of the injected clock.
func (c *govClock) current() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// advance moves the injected clock forward.
func (c *govClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
