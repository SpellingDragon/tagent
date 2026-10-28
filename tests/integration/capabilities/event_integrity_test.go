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
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestEventIntegrityApproval drives the approval channel of the event-integrity contract through the formal governance policy and the registration wrapper:
//   - a call approved before its retry runs exactly once, whatever the number of duplicate responses
//   - an unapproved call waits as a held task and executes nothing
//   - a rejected call is refused before execution beside its named paired result
//   - an expired request refuses a late ruling and is never made runnable again
//   - changed arguments never reuse the old approval: the new digest is registered and held again
//   - a repeated response under one approval identity is idempotent and never executes the action twice
//   - a response naming another hold, another digest or no hold is refused and never resolves another call
//   - a failed channel delivery is recorded as a visible gap and never blocks the gate or the task side
//   - the formal assembly (governance policy -> registration wrapper -> execution) runs an approved call once
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestEventIntegrityApproval(t *testing.T) {
	const tool = "shell_exec"

	t.Run("approved_then_executes_once", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		ledger := governance.NewDenialLedger()
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskCritical, ledger: ledger})
		out, err := rig.call("call-approved", tool, args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		hold := approvalHoldOf(t, out)
		req := rig.registerApproval(hold.Hold, tool, args)
		if req == nil || req.Status != approval.ApprovalPending {
			t.Fatalf("approval request = %+v, want the held call registered as pending", req)
		}
		if want := task.DigestArgs([]byte(args)); req.ArgsDigest != want || req.ArgsDigest != approval.ArgsDigest([]byte(args)) {
			t.Fatalf("request digest = %q, want the digest %q of the held arguments", req.ArgsDigest, want)
		}

		// The same approve response reaches the book repeatedly and concurrently:
		// the first ruling takes effect and every duplicate reports it again.
		const responders = 8
		var wg sync.WaitGroup
		answers := make([]*approval.ApprovalRequest, responders)
		errs := make([]error, responders)
		for i := 0; i < responders; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				answers[i], errs[i] = rig.approvals.Decide(req.ID, approval.ApprovalApproved, fmt.Sprintf("cli:concurrent-%d", i))
			}(i)
		}
		wg.Wait()
		for i := range answers {
			if errs[i] != nil {
				t.Fatalf("concurrent response %d = %v, want the repeated ruling idempotent", i, errs[i])
			}
			if answers[i] == nil || answers[i].Status != approval.ApprovalApproved {
				t.Fatalf("concurrent response %d = %+v, want the approved state that took effect", i, answers[i])
			}
		}
		if got := rig.inner.executions(); got != 0 {
			t.Fatalf("tool executions before the approved retry = %d, want the duplicate responses to execute nothing", got)
		}

		out, err = rig.call("call-approved-retry", tool, args)
		if err != nil {
			t.Fatalf("the approved call must run on its retry: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("retry answer = %T, want the admission receipt of the approved call", out)
		}
		res := rig.awaitRecorded()
		if res.CallID != "call-approved-retry" || res.Tool != tool || res.Output == nil || res.Denied != nil {
			t.Fatalf("recorded result = %+v, want the approved execution recorded once under the native call", res)
		}
		if got := rig.inner.executions(); got != 1 || rig.inner.last() != args {
			t.Fatalf("executions = (%d, %q), want the approved action to run exactly once over the held arguments", got, rig.inner.last())
		}
		if d := rig.decision(); d.Disposition != plugin.DispositionRecord || d.Refused() || !strings.Contains(d.Reason, "approved") {
			t.Fatalf("retried ruling = %+v, want the approved call recorded as it runs", d)
		}
		records := ledger.Query(0)
		if len(records) != 2 {
			t.Fatalf("governance records = %d, want the wait beside the approved execution", len(records))
		}
		for _, rec := range records {
			if rec.ArgsDigest != req.ArgsDigest {
				t.Fatalf("governance record digest = %q, want the digest %q the approval response was bound to", rec.ArgsDigest, req.ArgsDigest)
			}
		}

		if _, err := rig.call("call-approved-retry", tool, args); err != nil {
			t.Fatalf("repeat the approved call: %v", err)
		}
		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "cli:late"); err != nil {
			t.Fatalf("repeat the approval response: %v", err)
		}
		if got := rig.inner.executions(); got != 1 {
			t.Fatalf("tool executions = %d, want the approved action to execute exactly once", got)
		}
		if got := len(rig.recordedSnapshot()); got != 1 {
			t.Fatalf("recorded results = %d, want exactly one execution record", got)
		}
	})

	t.Run("unapproved_never_executes", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskCritical})
		out, err := rig.call("call-unapproved", tool, args)
		if err != nil {
			t.Fatalf("a held call answers with its wait and no error: %v", err)
		}
		hold := approvalHoldOf(t, out)
		if hold.Hold.ApprovalID == "" || hold.Hold.ExpectedDigest != task.DigestArgs([]byte(args)) {
			t.Fatalf("hold receipt = %+v, want the wait beside its approval identity and the held digest", hold)
		}
		parked, ok := rig.wrapper.Holds().Waiting(hold.Hold.TaskID)
		if !ok || parked.ApprovalID != hold.Hold.ApprovalID || parked.ExpectedDigest != hold.Hold.ExpectedDigest {
			t.Fatalf("waiting table = (%+v, %v), want the call registered as a waiting task", parked, ok)
		}
		if got := rig.inner.executions(); got != 0 {
			t.Fatalf("tool executions = %d, want the unapproved call to execute nothing", got)
		}
		if recorded := rig.recordedSnapshot(); len(recorded) != 0 {
			t.Fatalf("recorded results = %+v, want no refusal result beside a wait", recorded)
		}
		d := rig.decision()
		if d.Disposition != plugin.DispositionHold || d.Refused() || d.Denial != nil {
			t.Fatalf("ruling = %+v, want a hold that is a wait and never a denial", d)
		}
		req := rig.registerApproval(hold.Hold, tool, args)
		if req.Status != approval.ApprovalPending {
			t.Fatalf("approval request = %+v, want the request waiting for a ruling", req)
		}
		if waiting := rig.approvals.Pending(); len(waiting) != 1 || waiting[0].ID != req.ID {
			t.Fatalf("pending requests = %+v, want the held call visible on the waiting face", waiting)
		}
	})

	t.Run("rejected_never_executes", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskCritical})
		out, err := rig.call("call-rejected", tool, args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		hold := approvalHoldOf(t, out)
		req := rig.registerApproval(hold.Hold, tool, args)
		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalDenied, "operator"); err != nil {
			t.Fatalf("deny the hold: %v", err)
		}

		out, err = rig.call("call-rejected-retry", tool, args)
		if !errors.Is(err, task.ErrDenied) {
			t.Fatalf("retry error = %v, want the denied call refused by name", err)
		}
		if out != nil {
			t.Fatalf("retry answer = %+v, want no answer beside the refusal", out)
		}
		if got := rig.inner.executions(); got != 0 {
			t.Fatalf("tool executions = %d, want the denied call to execute nothing", got)
		}
		res := rig.awaitRecorded()
		if res.CallID != "call-rejected-retry" || res.Tool != tool || res.Denied == nil || res.Denied.Tool != tool {
			t.Fatalf("recorded result = %+v, want the named paired result of the refused call", res)
		}
		if !strings.Contains(res.Denied.Reason, "denied") {
			t.Fatalf("refusal reason = %q, want the denied approval named for the model to read", res.Denied.Reason)
		}
		if d := rig.decision(); !d.Refused() || d.Denial == nil {
			t.Fatalf("denied ruling = %+v, want a terminal refusal carrying its denial", d)
		}
	})

	t.Run("expired_response_refused", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskCritical, ttl: 30 * time.Minute})
		out, err := rig.call("call-expiring", tool, args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		hold := approvalHoldOf(t, out)
		req := rig.registerApproval(hold.Hold, tool, args)

		rig.clock.advance(31 * time.Minute)
		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "cli:late"); !errors.Is(err, approval.ErrApprovalExpired) {
			t.Fatalf("late ruling error = %v, want the expired request refused beside %v", err, approval.ErrApprovalExpired)
		}
		seen := rig.approvals.Lookup(tool, approval.ArgsDigest([]byte(args)))
		if seen == nil || seen.Status != approval.ApprovalExpired || seen.ID != req.ID {
			t.Fatalf("lookup after validity = %+v, want the request read under its expired effective state", seen)
		}
		if waiting := rig.approvals.Pending(); len(waiting) != 0 {
			t.Fatalf("pending requests = %+v, want the expired request off the waiting face", waiting)
		}

		out, err = rig.call("call-expired-retry", tool, args)
		if err != nil {
			t.Fatalf("an expired approval answers with a fresh wait and no error: %v", err)
		}
		if _, ok := out.(task.HoldReceipt); !ok {
			t.Fatalf("retry answer = %T, want the call held again instead of proceeding on the expired request", out)
		}
		if got := rig.inner.executions(); got != 0 {
			t.Fatalf("tool executions = %d, want nothing to proceed on the expired request", got)
		}
		if recorded := rig.recordedSnapshot(); len(recorded) != 0 {
			t.Fatalf("recorded results = %+v, want the expired request restored as a wait, never as an executed obligation", recorded)
		}
		if d := rig.decision(); d.Disposition != plugin.DispositionHold || d.Refused() {
			t.Fatalf("ruling after expiry = %+v, want a fresh wait, never a release of the old request", d)
		}
	})

	t.Run("changed_args_requires_new_approval", func(t *testing.T) {
		argsA := `{"cmd":"deploy"}`
		argsB := `{"cmd":"deploy","force":true}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskCritical})
		out, err := rig.call("call-changed-1", tool, argsA)
		if err != nil {
			t.Fatalf("park the call over the first arguments: %v", err)
		}
		holdA := approvalHoldOf(t, out)
		reqA := rig.registerApproval(holdA.Hold, tool, argsA)
		if _, err := rig.approvals.Decide(reqA.ID, approval.ApprovalApproved, "operator"); err != nil {
			t.Fatalf("approve the first arguments: %v", err)
		}

		out, err = rig.call("call-changed-2", tool, argsB)
		if err != nil {
			t.Fatalf("the changed call answers with its wait and no error: %v", err)
		}
		if _, ok := out.(task.HoldReceipt); !ok {
			t.Fatalf("changed-call answer = %T, want the call held again over the changed arguments", out)
		}
		if got := rig.inner.executions(); got != 0 {
			t.Fatalf("tool executions = %d, want the changed call to execute nothing", got)
		}
		digestB := approval.ArgsDigest([]byte(argsB))
		reqB := rig.approvals.Lookup(tool, digestB)
		if reqB == nil || reqB.Status != approval.ApprovalPending || reqB.ID == reqA.ID {
			t.Fatalf("changed-arguments lookup = %+v, want a fresh pending request under the new digest", reqB)
		}
		raw, err := os.ReadFile(filepath.Join(rig.approvalDir(), reqB.ID+".json"))
		if err != nil {
			t.Fatalf("read the changed-arguments request file: %v", err)
		}
		var onDisk approval.ApprovalRequest
		if err := json.Unmarshal(raw, &onDisk); err != nil {
			t.Fatalf("parse the changed-arguments request file: %v", err)
		}
		if onDisk.ArgsDigest != digestB || onDisk.Status != approval.ApprovalPending {
			t.Fatalf("changed-arguments request on disk = %+v, want the new digest written beside its pending state", onDisk)
		}
		if d := rig.decision(); d.Disposition != plugin.DispositionHold || d.Refused() {
			t.Fatalf("changed-call ruling = %+v, want a fresh wait, never an approval over other arguments", d)
		}

		if _, err := rig.approvals.Decide(reqB.ID, approval.ApprovalApproved, "operator"); err != nil {
			t.Fatalf("approve the changed arguments: %v", err)
		}
		out, err = rig.call("call-changed-3", tool, argsB)
		if err != nil {
			t.Fatalf("the newly approved changed call must run: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("retry answer = %T, want the admission receipt of the newly approved call", out)
		}
		if res := rig.awaitRecorded(); res.Denied != nil || res.Output == nil {
			t.Fatalf("recorded result = %+v, want the changed execution recorded after the fresh approval", res)
		}
		if got := rig.inner.executions(); got != 1 || rig.inner.last() != argsB {
			t.Fatalf("executions = (%d, %q), want only the changed arguments executed once", got, rig.inner.last())
		}
	})

	t.Run("duplicate_response_no_second_execution", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementStrict, grade: plugin.RiskCritical})
		out, err := rig.call("call-duplicate", tool, args)
		if err != nil {
			t.Fatalf("park the call: %v", err)
		}
		hold := approvalHoldOf(t, out)
		req := rig.registerApproval(hold.Hold, tool, args)
		first, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "operator")
		if err != nil || first.Status != approval.ApprovalApproved {
			t.Fatalf("first ruling = (%+v, %v), want the approval recorded", first, err)
		}
		repeat, err := rig.approvals.Decide(req.ID, approval.ApprovalDenied, "cli:late")
		if err != nil || repeat.Status != approval.ApprovalApproved || repeat.DecidedBy != "operator" {
			t.Fatalf("repeated ruling = (%+v, %v), want the repeat reporting the first ruling without consuming a second", repeat, err)
		}

		resolved, err := rig.wrapper.Holds().Resolve(hold.Hold.TaskID, task.ApprovalAnswer{
			ApprovalID: hold.Hold.ApprovalID, ExpectedDigest: hold.Hold.ExpectedDigest, Granted: true, Reason: "operator",
		})
		if err != nil || !resolved.Granted {
			t.Fatalf("hold resolution = (%+v, %v), want the granting response to take effect", resolved, err)
		}
		again, err := rig.wrapper.Holds().Resolve(hold.Hold.TaskID, task.ApprovalAnswer{
			ApprovalID: hold.Hold.ApprovalID, ExpectedDigest: hold.Hold.ExpectedDigest, Granted: false, Reason: "cli:late",
		})
		if err != nil || !again.Granted || again.Reason != "operator" {
			t.Fatalf("repeated resolution = (%+v, %v), want the repeat reporting the first response instead of consuming a second", again, err)
		}

		out, err = rig.call("call-duplicate-retry", tool, args)
		if err != nil {
			t.Fatalf("the approved call must run: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("retry answer = %T, want the admission receipt of the approved call", out)
		}
		if res := rig.awaitRecorded(); res.Denied != nil || res.Output == nil {
			t.Fatalf("recorded result = %+v, want the approved execution recorded", res)
		}
		if got := rig.inner.executions(); got != 1 {
			t.Fatalf("tool executions = %d, want the approved action executed once", got)
		}

		if _, err := rig.call("call-duplicate-retry", tool, args); err != nil {
			t.Fatalf("repeat the approved call identity: %v", err)
		}
		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "cli:again"); err != nil {
			t.Fatalf("repeat the approval response: %v", err)
		}
		if _, err := rig.wrapper.Holds().Resolve(hold.Hold.TaskID, task.ApprovalAnswer{
			ApprovalID: hold.Hold.ApprovalID, ExpectedDigest: hold.Hold.ExpectedDigest, Granted: true, Reason: "cli:again",
		}); err != nil {
			t.Fatalf("repeat the hold response: %v", err)
		}
		if got := rig.inner.executions(); got != 1 {
			t.Fatalf("tool executions = %d, want the duplicate responses to execute nothing a second time", got)
		}
		if got := len(rig.recordedSnapshot()); got != 1 {
			t.Fatalf("recorded results = %d, want exactly one execution record", got)
		}
	})

	t.Run("wrong_target_no_overreach", func(t *testing.T) {
		argsA := `{"cmd":"deploy"}`
		argsB := `{"cmd":"rollback"}`
		rig := newGovRig(t, govConfig{mode: governance.EnforcementWarn, grade: plugin.RiskCritical})
		out, err := rig.call("call-target-1", tool, argsA)
		if err != nil {
			t.Fatalf("park the first call: %v", err)
		}
		holdA := approvalHoldOf(t, out)
		out, err = rig.call("call-target-2", tool, argsB)
		if err != nil {
			t.Fatalf("park the second call: %v", err)
		}
		holdB := approvalHoldOf(t, out)
		if holdA.Hold.TaskID == holdB.Hold.TaskID || holdA.Hold.ApprovalID == holdB.Hold.ApprovalID {
			t.Fatalf("holds = (%+v, %+v), want two distinct waiting calls", holdA.Hold, holdB.Hold)
		}

		if _, err := rig.wrapper.Holds().Resolve(holdA.Hold.TaskID, task.ApprovalAnswer{
			ApprovalID: holdB.Hold.ApprovalID, ExpectedDigest: holdA.Hold.ExpectedDigest, Granted: true, Reason: "operator",
		}); !errors.Is(err, task.ErrApprovalIdentityMismatch) {
			t.Fatalf("foreign-identity resolution error = %v, want the response refused beside %v", err, task.ErrApprovalIdentityMismatch)
		}
		if _, err := rig.wrapper.Holds().Resolve(holdA.Hold.TaskID, task.ApprovalAnswer{
			ApprovalID: holdA.Hold.ApprovalID, ExpectedDigest: holdB.Hold.ExpectedDigest, Granted: true, Reason: "operator",
		}); !errors.Is(err, task.ErrApprovalDigestMismatch) {
			t.Fatalf("foreign-digest resolution error = %v, want the response refused beside %v", err, task.ErrApprovalDigestMismatch)
		}
		if _, err := rig.wrapper.Holds().Resolve(event.TaskID("task-without-hold"), task.ApprovalAnswer{
			ApprovalID: holdA.Hold.ApprovalID, ExpectedDigest: holdA.Hold.ExpectedDigest, Granted: true, Reason: "operator",
		}); !errors.Is(err, task.ErrApprovalIdentityMismatch) {
			t.Fatalf("unknown-hold resolution error = %v, want the response refused beside %v", err, task.ErrApprovalIdentityMismatch)
		}

		reqA := rig.approvals.Lookup(tool, approval.ArgsDigest([]byte(argsA)))
		reqB := rig.approvals.Lookup(tool, approval.ArgsDigest([]byte(argsB)))
		if reqA == nil || reqB == nil || reqA.ID == reqB.ID {
			t.Fatalf("approval requests = (%+v, %+v), want one request per held target", reqA, reqB)
		}
		if _, err := rig.approvals.Decide(reqB.ID, approval.ApprovalApproved, "operator"); err != nil {
			t.Fatalf("approve the second target: %v", err)
		}
		if seen := rig.approvals.Lookup(tool, approval.ArgsDigest([]byte(argsA))); seen == nil || seen.Status != approval.ApprovalPending {
			t.Fatalf("first target after the other ruling = %+v, want the ruling over another target to leave it pending", seen)
		}

		if parked, ok := rig.wrapper.Holds().Waiting(holdA.Hold.TaskID); !ok || parked.ApprovalID != holdA.Hold.ApprovalID {
			t.Fatalf("first hold after the refused responses = (%+v, %v), want the wait untouched", parked, ok)
		}
		valid, err := rig.wrapper.Holds().Resolve(holdA.Hold.TaskID, task.ApprovalAnswer{
			ApprovalID: holdA.Hold.ApprovalID, ExpectedDigest: holdA.Hold.ExpectedDigest, Granted: true, Reason: "operator",
		})
		if err != nil || !valid.Granted {
			t.Fatalf("bound resolution = (%+v, %v), want the response naming the hold to take effect", valid, err)
		}
		if got := rig.inner.executions(); got != 0 {
			t.Fatalf("tool executions = %d, want no response to execute a call on its own", got)
		}
	})

	t.Run("channel_delivery_failure_does_not_block", func(t *testing.T) {
		args := `{"cmd":"deploy"}`
		failing := &govChannel{err: errors.New("the approval channel is unreachable")}
		working := &govChannel{}
		rig := newGovRig(t, govConfig{
			mode:     governance.EnforcementStrict,
			grade:    plugin.RiskCritical,
			channels: []approval.ApprovalChannel{failing, working},
		})
		out, err := rig.call("call-channel", tool, args)
		if err != nil {
			t.Fatalf("the gate must keep waiting beside a failed delivery: %v", err)
		}
		hold := approvalHoldOf(t, out)
		req := rig.registerApproval(hold.Hold, tool, args)
		if req == nil || req.Status != approval.ApprovalPending {
			t.Fatalf("registration = %+v, want the request registered beside the failed delivery", req)
		}
		gaps := rig.approvals.DeliveryGaps()
		if len(gaps) != 1 || gaps[0].Request != req.ID || gaps[0].Err == nil {
			t.Fatalf("delivery gaps = %+v, want the failed delivery recorded as a visible gap", gaps)
		}
		if got := working.count(); got != 1 {
			t.Fatalf("working channel deliveries = %d, want the request offered to every attached channel", got)
		}
		if raw, err := os.ReadFile(filepath.Join(rig.approvalDir(), req.ID+".json")); err != nil || len(raw) == 0 {
			t.Fatalf("request file = (%d bytes, %v), want the request persisted despite the failed delivery", len(raw), err)
		}
		if got := rig.inner.executions(); got != 0 {
			t.Fatalf("tool executions = %d, want the gate to keep waiting", got)
		}

		if _, err := rig.approvals.Decide(req.ID, approval.ApprovalApproved, "cli:alice"); err != nil {
			t.Fatalf("answer the request through the other entry: %v", err)
		}
		out, err = rig.call("call-channel-retry", tool, args)
		if err != nil {
			t.Fatalf("the call approved through the other entry must run: %v", err)
		}
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("retry answer = %T, want the admission receipt of the approved call", out)
		}
		if res := rig.awaitRecorded(); res.Denied != nil || res.Output == nil {
			t.Fatalf("recorded result = %+v, want the approved execution recorded", res)
		}
		if got := rig.inner.executions(); got != 1 {
			t.Fatalf("tool executions = %d, want the approved call executed exactly once", got)
		}
	})

	t.Run("formal_governance_task_wrapper", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		const toolName = "approval_tool"
		args := `{"cmd":"deploy"}`
		probe := &policyProbe{name: toolName}
		approvals := approval.NewApprovalManager("", time.Hour)
		recorder := &rulingRecorder{inner: governance.Open(
			governance.WithRiskRules(governance.ToolRiskRule{Tool: toolName, Level: plugin.RiskCritical}),
			governance.WithApprovals(approvals),
		)}
		var mu sync.Mutex
		round := 0
		approvedID := ""
		provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
			mu.Lock()
			round++
			current := round
			mu.Unlock()
			switch current {
			case 1:
				return ntToolTurn(ntCall("call-approval-1", toolName, args))
			case 2:
				pending := approvals.Pending()
				if len(pending) != 1 {
					t.Errorf("pending approval requests = %d, want the formal hold to register exactly one", len(pending))
					return ntFinalTurn("no approval request")
				}
				approvedID = pending[0].ID
				if want := approval.ArgsDigest([]byte(args)); pending[0].ArgsDigest != want {
					t.Errorf("pending request digest = %q, want %q", pending[0].ArgsDigest, want)
				}
				if _, err := approvals.Decide(pending[0].ID, approval.ApprovalApproved, "operator"); err != nil {
					t.Errorf("approve the formally registered request: %v", err)
				}
				return ntToolTurn(ntCall("call-approval-2", toolName, args))
			default:
				return ntFinalTurn("settled")
			}
		}}
		run, err := assembly.Assemble(ctx, declarationOf(root), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{probe}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithPolicy(recorder))
		if err != nil {
			t.Fatalf("assemble the approval runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-approval"}
		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-approval",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the approval runtime: %v", err)
		}
		if approvedID == "" {
			t.Fatalf("no approval request was registered through the formal governance path")
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 3 {
			t.Fatalf("turn = (%v, %d decisions), want the wait, the approved retry and the closing answer as three decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := probe.calls(); got != 1 {
			t.Fatalf("%s executions = %d, want the formally approved retry executed exactly once", toolName, got)
		}
		requests := provider.recorded()
		if len(requests) != 3 {
			t.Fatalf("recorded requests = %d, want the wait round, the retry round and the closing round", len(requests))
		}
		checkPair(t, pairedOf(t, &requests[1], "call-approval-1"), "call-approval-1", toolName, "parked awaiting approval")
		checkReceipt(t, pairedOf(t, &requests[2], "call-approval-2"), "call-approval-2", toolName, "probe-out:"+toolName)
		checkObservation(t, settledOf(t, &requests[2], "call-approval-2"), "call-approval-2", toolName, "probe-out:"+toolName)
		rulings := recorder.rulings()
		if len(rulings) != 2 {
			t.Fatalf("rulings = %+v, want the wait of the first call beside the ruling of its retry", rulings)
		}
		if rulings[0].decision.Disposition != plugin.DispositionHold || rulings[0].decision.Denial != nil || rulings[0].decision.Refused() {
			t.Fatalf("ruling of the first call = %+v, want the wait carrying no denial", rulings[0].decision)
		}
		if approved := rulings[1].decision; approved.Disposition != plugin.DispositionRecord || approved.Refused() || !strings.Contains(approved.Reason, "approved") {
			t.Fatalf("ruling of the approved retry = %+v, want the approved call recorded for execution carrying no denial", approved)
		}
		settled := approvals.Lookup(toolName, approval.ArgsDigest([]byte(args)))
		if settled == nil || settled.ID != approvedID || settled.Status != approval.ApprovalApproved || settled.DecidedBy != "operator" {
			t.Fatalf("approval lookup = %+v, want the formally registered request approved by the operator", settled)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}
		reading, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the approval scene: %v", err)
		}
		waiting := 0
		for _, detail := range reading.Details {
			if detail.Ref.Kind != query.ObjectTask {
				continue
			}
			if detail.State.TaskStatus == task.StatusAwaitingApproval {
				waiting++
			}
		}
		if waiting != 1 {
			t.Fatalf("tasks awaiting approval = %d, want the parked call to read as one waiting task", waiting)
		}
	})
}

// approvalHoldOf returns the waiting receipt one parked call answers with.
func approvalHoldOf(t *testing.T, out any) task.HoldReceipt {
	t.Helper()
	receipt, ok := out.(task.HoldReceipt)
	if !ok {
		t.Fatalf("call answer = %T, want the waiting receipt of a parked call", out)
	}
	return receipt
}
