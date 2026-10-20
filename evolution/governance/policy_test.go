package governance

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// invariantRulingCall builds one native tool call for the ruling to judge.
func invariantRulingCall(id, tool string) trpcmodel.ToolCall {
	return trpcmodel.ToolCall{ID: id, Function: trpcmodel.FunctionDefinitionParam{Name: tool, Arguments: []byte(`{"command":"ls"}`)}}
}

// invariantMatchingRule builds one rule that fires at every rendered grade.
func invariantMatchingRule(reason string) RiskRule {
	return RiskRule{
		ID:      "no-shell",
		MinRisk: plugin.RiskLow,
		Match:   func(RiskContext) bool { return true },
		Reason:  reason,
	}
}

// TestGovernanceRulingInvariants pins the one pre-execution ruling a call receives.
// - a hold is a wait and never a denial, while a refused call carries its denial beside it
// - a critical call beside no approval channel is refused instead of waiting forever
// - an exhausted budget refuses the call whatever the enforcement degree says
// - a pass carrying no grade is ruled at the ordinary mutating grade and never passes ungraded
// - a warn rule records the call while the same rule under strict refuses it
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
func TestGovernanceRulingInvariants(t *testing.T) {
	call := invariantRulingCall("call-1", "shell")

	t.Run("a hold waits and is never reported as a denial", func(t *testing.T) {
		ruling, err := Open(WithApprovals(approval.NewApprovalManager("", 0))).Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the critical call: %v", err)
		}
		if ruling.Disposition != plugin.DispositionHold {
			t.Fatalf("disposition = %q, want %q while approval is pending", ruling.Disposition, plugin.DispositionHold)
		}
		if ruling.Denial != nil || ruling.Refused() {
			t.Fatalf("held ruling = %+v, want a wait without a denial", ruling)
		}
		if ruling.Disposition == plugin.DispositionRecord || ruling.Disposition == plugin.DispositionAllow {
			t.Fatalf("disposition = %q, want the held call to stay parked", ruling.Disposition)
		}
	})

	t.Run("a critical call beside no approval channel is refused", func(t *testing.T) {
		ruling, err := Open().Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the unapprovable call: %v", err)
		}
		if ruling.Denial == nil || !ruling.Refused() {
			t.Fatalf("ruling = %+v, want a terminal refusal", ruling)
		}
		if ruling.Disposition == plugin.DispositionHold {
			t.Fatalf("disposition = %q, want a refusal told apart from a hold", ruling.Disposition)
		}
		if !strings.Contains(ruling.Reason, "approval") {
			t.Fatalf("reason = %q, want the missing approval channel named", ruling.Reason)
		}
	})

	t.Run("an exhausted budget refuses the call whatever the enforcement degree says", func(t *testing.T) {
		budget := NewBudgetManager(BudgetConfig{MaxHighRisk: 1}, "")
		warn := Open(WithBudget(budget))
		first, err := warn.Judge(plugin.CallPass{Final: call, Risk: plugin.RiskHigh})
		if err != nil {
			t.Fatalf("judge the first high call: %v", err)
		}
		if first.Refused() || first.Disposition != plugin.DispositionRecord {
			t.Fatalf("first high ruling = %+v, want it admitted and recorded", first)
		}
		second, err := warn.Judge(plugin.CallPass{Final: call, Risk: plugin.RiskHigh})
		if err != nil {
			t.Fatalf("judge the second high call: %v", err)
		}
		if !second.Refused() || second.Denial == nil {
			t.Fatalf("second high ruling = %+v, want a refusal once the window is spent", second)
		}
		if second.Reason != budgetGateReason {
			t.Fatalf("reason = %q, want the budget gate reason", second.Reason)
		}

		strictBudget := NewBudgetManager(BudgetConfig{MaxHighRisk: 1}, "")
		strict := Open(WithBudget(strictBudget), WithStrict())
		if _, err := strict.Judge(plugin.CallPass{Final: call, Risk: plugin.RiskHigh}); err != nil {
			t.Fatalf("judge the strict first high call: %v", err)
		}
		refused, err := strict.Judge(plugin.CallPass{Final: call, Risk: plugin.RiskHigh})
		if err != nil {
			t.Fatalf("judge the strict second high call: %v", err)
		}
		if !refused.Refused() {
			t.Fatalf("strict second high ruling = %+v, want the budget to refuse under strict too", refused)
		}
	})

	t.Run("a pass carrying no grade is ruled at the ordinary mutating grade", func(t *testing.T) {
		ruling, err := Open().Judge(plugin.CallPass{Final: call})
		if err != nil {
			t.Fatalf("judge the ungraded call: %v", err)
		}
		if ruling.Risk != plugin.RiskMedium {
			t.Fatalf("risk = %q, want the conservative fallback %q", ruling.Risk, plugin.RiskMedium)
		}
		if ruling.Risk == "" {
			t.Fatalf("risk = %q, want the ungraded pass ruled instead of passing ungraded", ruling.Risk)
		}
		if ruling.Disposition != plugin.DispositionAllow {
			t.Fatalf("disposition = %q, want an ordinary mutating call allowed", ruling.Disposition)
		}

		fallback, err := Open(WithDefaultRisk(plugin.RiskHigh)).Judge(plugin.CallPass{Final: call})
		if err != nil {
			t.Fatalf("judge the ungraded call beside a default grade: %v", err)
		}
		if fallback.Risk != plugin.RiskHigh || fallback.Disposition != plugin.DispositionRecord {
			t.Fatalf("ruling = %+v, want the configured default grade read as high and recorded", fallback)
		}
	})

	t.Run("a warn rule records the call while the same rule under strict refuses it", func(t *testing.T) {
		warned, err := Open(WithRules(invariantMatchingRule("shell needs a review"))).Judge(plugin.CallPass{Final: call, Risk: plugin.RiskMedium})
		if err != nil {
			t.Fatalf("judge beside a warn rule: %v", err)
		}
		if warned.Disposition != plugin.DispositionRecord || warned.Rule != "no-shell" {
			t.Fatalf("warn ruling = %+v, want the fired rule recorded", warned)
		}
		if warned.Refused() {
			t.Fatalf("warn ruling = %+v, want a recording instead of a refusal", warned)
		}

		refused, err := Open(WithRules(invariantMatchingRule("shell needs a review")), WithStrict()).
			Judge(plugin.CallPass{Final: call, Risk: plugin.RiskMedium})
		if err != nil {
			t.Fatalf("judge beside a strict rule: %v", err)
		}
		if !refused.Refused() || refused.Denial == nil {
			t.Fatalf("strict ruling = %+v, want the fired rule refused", refused)
		}
		if refused.Denial.Rule != "no-shell" || refused.Denial.CallID != call.ID || refused.Denial.Tool != call.Function.Name {
			t.Fatalf("denial = %+v, want the fired rule and the native call identity named", refused.Denial)
		}
		if refused.Denial.Result.ToolID != call.ID || refused.Denial.Result.ToolName != call.Function.Name {
			t.Fatalf("denial result = %+v, want the paired result bound to the same call", refused.Denial.Result)
		}
	})
}

// TestApprovalRulingObservation pins the lines the approval channel writes for every ruling the record answers.
// - an approved, a denied and an expired request each write one INFO line carrying the digest, the ruling and the approver
// - a call beside no approval channel writes its unapprovable ruling at INFO
// - a pending wait writes one DEBUG line and keeps the INFO stream free of it
// - a logger that fails changes no ruling: the bypass drops the line and keeps the decision
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalRulingObservation(t *testing.T) {
	ctx := context.Background()
	call := invariantRulingCall("call-1", "shell")
	digest := approval.ArgsDigest(call.Function.Arguments)

	t.Run("an approved ruling writes its line beside the approver", func(t *testing.T) {
		observer := &approvalObserver{}
		defer watchApprovalLog(t, observer)()
		manager := approval.NewApprovalManager("", time.Hour)
		if _, err := manager.Request(ctx, "approval-1", call.Function.Name, call.Function.Arguments, plugin.RiskCritical, "no-shell", "shell needs a review"); err != nil {
			t.Fatalf("register the approval request: %v", err)
		}
		if _, err := manager.Decide("approval-1", approval.ApprovalApproved, "alice"); err != nil {
			t.Fatalf("approve the request: %v", err)
		}
		ruling, err := Open(WithApprovals(manager), WithRules(invariantMatchingRule("shell needs a review"))).
			Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the approved call: %v", err)
		}
		if ruling.Refused() || ruling.Disposition != plugin.DispositionRecord {
			t.Fatalf("ruling = %+v, want the approved call recorded", ruling)
		}
		if !observer.mentions("info", "approval ruled", "tool=shell", "digest="+digest, "ruling=approved", "by=alice") {
			t.Fatalf("info lines = %v, want the approved ruling beside its digest and approver", observer.all())
		}
	})

	t.Run("a denied ruling writes its line beside the approver", func(t *testing.T) {
		observer := &approvalObserver{}
		defer watchApprovalLog(t, observer)()
		manager := approval.NewApprovalManager("", time.Hour)
		if _, err := manager.Request(ctx, "approval-1", call.Function.Name, call.Function.Arguments, plugin.RiskCritical, "", ""); err != nil {
			t.Fatalf("register the approval request: %v", err)
		}
		if _, err := manager.Decide("approval-1", approval.ApprovalDenied, "bob"); err != nil {
			t.Fatalf("deny the request: %v", err)
		}
		ruling, err := Open(WithApprovals(manager)).Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the denied call: %v", err)
		}
		if !ruling.Refused() {
			t.Fatalf("ruling = %+v, want the denied call refused", ruling)
		}
		if !observer.mentions("info", "approval ruled", "digest="+digest, "ruling=denied", "by=bob") {
			t.Fatalf("info lines = %v, want the denied ruling beside its digest and approver", observer.all())
		}
	})

	t.Run("an expired request writes its line instead of a standing approval", func(t *testing.T) {
		observer := &approvalObserver{}
		defer watchApprovalLog(t, observer)()
		clock := newObservationClock()
		manager := approval.NewApprovalManager("", time.Minute, approval.WithApprovalClock(clock.Now))
		request, err := manager.Request(ctx, "approval-1", call.Function.Name, call.Function.Arguments, plugin.RiskCritical, "", "")
		if err != nil {
			t.Fatalf("register the approval request: %v", err)
		}
		clock.advance(2 * time.Minute)
		ruling, err := Open(WithApprovals(manager)).Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the expired call: %v", err)
		}
		if ruling.Disposition != plugin.DispositionHold || ruling.Refused() {
			t.Fatalf("ruling = %+v, want the expired call to wait instead of proceeding", ruling)
		}
		if !observer.mentions("info", "approval ruled", "digest="+digest, "ruling=expired", fmt.Sprintf("expires_ms=%d", request.ExpiresMs)) {
			t.Fatalf("info lines = %v, want the expired ruling beside its validity", observer.all())
		}
	})

	t.Run("a pending wait writes a debug line and no info line", func(t *testing.T) {
		observer := &approvalObserver{}
		defer watchApprovalLog(t, observer)()
		manager := approval.NewApprovalManager("", time.Hour)
		if _, err := manager.Request(ctx, "approval-1", call.Function.Name, call.Function.Arguments, plugin.RiskCritical, "", ""); err != nil {
			t.Fatalf("register the approval request: %v", err)
		}
		ruling, err := Open(WithApprovals(manager)).Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the pending call: %v", err)
		}
		if ruling.Disposition != plugin.DispositionHold {
			t.Fatalf("disposition = %q, want the pending call held", ruling.Disposition)
		}
		if !observer.mentions("debug", "approval awaited", "digest="+digest) {
			t.Fatalf("debug lines = %v, want the pending wait beside its digest", observer.all())
		}
		if observer.mentions("info", "approval ruled") {
			t.Fatalf("info lines = %v, want the repeated wait kept out of the INFO stream", observer.all())
		}
	})

	t.Run("a call beside no approval channel writes its unapprovable ruling", func(t *testing.T) {
		observer := &approvalObserver{}
		defer watchApprovalLog(t, observer)()
		ruling, err := Open().Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the unapprovable call: %v", err)
		}
		if !ruling.Refused() {
			t.Fatalf("ruling = %+v, want the unapprovable call refused", ruling)
		}
		if !observer.mentions("info", "approval ruled", "tool=shell", "ruling=unavailable") {
			t.Fatalf("info lines = %v, want the unapprovable ruling named", observer.all())
		}
	})

	t.Run("a failing logger changes no ruling", func(t *testing.T) {
		observer := &approvalObserver{panicsLeft: 1}
		defer watchApprovalLog(t, observer)()
		manager := approval.NewApprovalManager("", time.Hour)
		if _, err := manager.Request(ctx, "approval-1", call.Function.Name, call.Function.Arguments, plugin.RiskCritical, "", ""); err != nil {
			t.Fatalf("register the approval request: %v", err)
		}
		if _, err := manager.Decide("approval-1", approval.ApprovalApproved, "alice"); err != nil {
			t.Fatalf("approve the request: %v", err)
		}
		ruling, err := Open(WithApprovals(manager)).Judge(plugin.CallPass{Final: call, Risk: plugin.RiskCritical})
		if err != nil {
			t.Fatalf("judge the approved call beside a failing logger: %v", err)
		}
		if ruling.Refused() || ruling.Disposition != plugin.DispositionRecord {
			t.Fatalf("ruling = %+v, want the approved call recorded beside a failing logger", ruling)
		}
	})
}

// observationClock is the approval clock of the observation checks: it serves the
// instant it was moved to, so one request can be read past its validity.
type observationClock struct {
	mu  sync.Mutex
	now time.Time
}

// newObservationClock returns a clock standing at the current instant.
func newObservationClock() *observationClock {
	return &observationClock{now: time.Now()}
}

// Now reports the instant the clock stands at.
func (c *observationClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// advance moves the clock forward by one span.
func (c *observationClock) advance(span time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(span)
}

// approvalObserver is the framework logger the observation checks of this
// package read the approval face through: it records every line beside its level
// and refuses the writes its counter still holds by panicking.
type approvalObserver struct {
	mu         sync.Mutex
	lines      []string
	panicsLeft int
}

// record takes one line at the named level, or refuses it by panicking.
func (o *approvalObserver) record(level, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.panicsLeft > 0 {
		o.panicsLeft--
		panic("the logger refused the write")
	}
	o.lines = append(o.lines, level+": "+text)
}

// Debug records one debug line.
func (o *approvalObserver) Debug(args ...any) { o.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (o *approvalObserver) Debugf(format string, args ...any) {
	o.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (o *approvalObserver) Info(args ...any) { o.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (o *approvalObserver) Infof(format string, args ...any) {
	o.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (o *approvalObserver) Warn(args ...any) { o.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (o *approvalObserver) Warnf(format string, args ...any) {
	o.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (o *approvalObserver) Error(args ...any) { o.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (o *approvalObserver) Errorf(format string, args ...any) {
	o.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (o *approvalObserver) Fatal(args ...any) { o.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (o *approvalObserver) Fatalf(format string, args ...any) {
	o.record("fatal", fmt.Sprintf(format, args...))
}

// all reports every line taken, whatever the level.
func (o *approvalObserver) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// mentions reports whether one line taken at the level carries every fragment.
func (o *approvalObserver) mentions(level string, fragments ...string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, line := range o.lines {
		if !strings.HasPrefix(line, level+": ") {
			continue
		}
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
				break
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// watchApprovalLog points the framework logger at the observer for the duration
// of one check and answers the restore that ends it, so a check reads the lines of
// its own face and leaves the package logger as it found it.
func watchApprovalLog(t *testing.T, observer trpclog.Logger) func() {
	t.Helper()
	original := trpclog.Default
	trpclog.Default = observer
	restore := func() { trpclog.Default = original }
	t.Cleanup(restore)
	return restore
}

// TestPolicyDeclaresTheRulingPort pins the gate as one declared call->ruling instance.
func TestPolicyDeclaresTheRulingPort(t *testing.T) {
	policy := Open()
	if got, want := policy.Name(), "governance-policy"; got != want {
		t.Fatalf("policy name = %q, want %q", got, want)
	}
	want := plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductRuling}
	if got := policy.Port(); got != want {
		t.Fatalf("policy port = %s, want %s", got, want)
	}
	if got, want := policy.Port().String(), "call->ruling"; got != want {
		t.Fatalf("policy port reads = %q, want %q", got, want)
	}
	phase, err := plugin.RouteOf(policy.Port())
	if err != nil {
		t.Fatalf("route the policy port: %v", err)
	}
	if phase != plugin.PhaseProduce {
		t.Fatalf("policy phase = %v, want the producing phase %v", phase, plugin.PhaseProduce)
	}
	if _, err := plugin.NewFlow([]plugin.Instance{policy}); err != nil {
		t.Fatalf("assemble the policy into the flow: %v", err)
	}
}

// TestDeclaredPortMustMatchTheFace pins the refusal of a declared port with no matching face.
func TestDeclaredPortMustMatchTheFace(t *testing.T) {
	_, err := plugin.NewFlow([]plugin.Instance{misdeclaredPolicy{Policy: Open()}})
	if err == nil {
		t.Fatal("NewFlow accepted a policy declaring a port it does not serve")
	}
	if !strings.Contains(err.Error(), "implements no narrow interface") {
		t.Fatalf("refusal = %q, want the missing narrow interface named", err)
	}
}

// misdeclaredPolicy declares the fact->compression port while carrying only the
// governance ruling face, so the flow must refuse it by name.
type misdeclaredPolicy struct{ *Policy }

// Port reports a port the embedded policy does not implement.
func (misdeclaredPolicy) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductCompression}
}
