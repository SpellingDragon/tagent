// The 05.3 acceptance tests of the post-commit observation wired into the
// formal assembly: the telemetry observer is consumed through the real
// assembly entry, every observed fact is read back exactly as it committed,
// and the gaps a failed observation leaves are readable from the diagnostic
// read face instead of being dropped.
package inspection

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/telemetry"
	"github.com/SpellingDragon/tagent/view"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// obsObserver captures the facts the observation face receives and forwards
// them to the observer it wraps, so a delivered fact and a failed observation
// are both checkable at the assembly's boundary. The capture is a test mirror
// of the wire and never a second ledger.
type obsObserver struct {
	mu     sync.Mutex
	inner  telemetry.Observer
	events []event.Event
	err    error
}

// Observe records the fact it was handed, reports the failure it was armed
// with, and forwards an accepted fact to the wrapped observer.
func (o *obsObserver) Observe(ctx context.Context, evt event.Event) error {
	o.mu.Lock()
	o.events = append(o.events, evt)
	err := o.err
	inner := o.inner
	o.mu.Unlock()
	if err != nil {
		return err
	}
	if inner != nil {
		return inner.Observe(ctx, evt)
	}
	return nil
}

// observed reports the facts the observation face received.
func (o *obsObserver) observed() []event.Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]event.Event(nil), o.events...)
}

var _ telemetry.Observer = (*obsObserver)(nil)

// obsModel is the scripted native model stand-in of these tests: it answers
// the opening input with one native tool call and everything else with a final
// answer, and never reaches a real endpoint.
type obsModel struct {
	name string
}

// GenerateContent streams the scripted answer of one request.
func (m *obsModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	if last := req.Messages[len(req.Messages)-1]; last.Content == "hello" {
		ch <- obsToolTurn(obsCall("call-1", "echo", `{"x":1}`))
	} else {
		ch <- obsFinalTurn("settled")
	}
	close(ch)
	return ch, nil
}

// Info reports the scripted model's descriptor.
func (m *obsModel) Info() model.Info { return model.Info{Name: m.name} }

var _ model.Model = (*obsModel)(nil)

// obsTool is the native echo tool stand-in: it answers with one fixed output.
type obsTool struct{ name string }

// Declaration returns the declaration naming the echo tool.
func (t *obsTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the echo tool once and reports its output.
func (t *obsTool) Call(ctx context.Context, jsonArgs []byte) (any, error) { return "out-a", nil }

var _ trpctool.CallableTool = (*obsTool)(nil)

// obsPassPolicy is the deterministic governance stand-in the formal assembly
// is driven under: one passing disposition per required check.
type obsPassPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (obsPassPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       obsPassingCheck("eval"),
		"benchmark":  obsPassingCheck("benchmark"),
		"apply-gate": obsPassingCheck("apply-gate"),
	}, nil
}

// obsPassingCheck builds one passing mechanism disposition under the name.
func obsPassingCheck(name state.CheckName) state.CheckAnswer {
	return state.CheckAnswer{Name: name, Verdict: state.CheckPass, Reason: "the stand-in passes the mechanism check", Scope: "assembly", Evidence: []string{"deterministic stand-in"}}
}

var _ state.Governance = obsPassPolicy{}

// obsCall builds one native tool call of a scripted assistant message.
func obsCall(id, name, args string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      name,
			Arguments: []byte(args),
		},
	}
}

// obsToolTurn builds the scripted response carrying one native tool call.
func obsToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Done:    true,
	}
}

// obsFinalTurn builds the scripted response carrying the final answer.
func obsFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

// obsToolOutput is the fixed output the echo tool answers with: the byte-identical
// content every observed fact must carry.
const obsToolOutput = "out-a"

// newObservedRun assembles one runtime through the sole assembly entry beside
// the telemetry observer under test, over a declaration naming the single
// agent and the scripted model and echo tool.
func newObservedRun(t *testing.T, ctx context.Context, observer telemetry.Observer) *tagent.Run {
	t.Helper()
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: &obsModel{name: "model-a"}, Tools: []trpctool.CallableTool{&obsTool{name: "echo"}}},
	}, tagent.WithGovernance(obsPassPolicy{}), tagent.WithTelemetry(observer))
	if err != nil {
		t.Fatalf("assemble the observed runtime: %v", err)
	}
	return run
}

// obsKindCounts derives the per-kind count of one reading from its detail
// entries.
func obsKindCounts(snapshot query.Snapshot) map[query.ObjectKind]int {
	out := map[query.ObjectKind]int{}
	for _, detail := range snapshot.Details {
		out[detail.Ref.Kind]++
	}
	return out
}

// obsInvoke drives one tool-calling turn through the assembled run.
func obsInvoke(t *testing.T, ctx context.Context, run *tagent.Run, invocation, session string) tagent.InvokeRequest {
	t.Helper()
	req := tagent.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Address:      event.Address{AgentID: "agent-a", SessionID: event.SessionID(session)},
		Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}
	turn, err := run.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("invoke the observed run: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the turn to complete regardless of the observation", turn.Terminal)
	}
	return req
}

// TestObservationReachDiagnostics proves the post-commit observation consumption of the formal assembly:
// - the After pass resolves each committed fact through the assembly's own ledger and hands it to the observer
// - every observation failure settles as a visible gap on the diagnostic read face beside the committed fact
// - 正式装配消费: the observer wired at the assembly entry consumes each committed fact of a real turn
// - 观察事实逐字节保真: the observed fact carries the tool output byte-identical beside the call identity it committed under
// - 缺口可从诊断读到: a failed observation lands as a named gap on the read face of the run and drops nothing
// - 观察不建第二真源: the observation hangs off the committed ledger read-only and moves no query reading
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestObservationReachDiagnostics(t *testing.T) {
	t.Run("正式装配消费: the observer wired at the assembly entry consumes each committed fact of a real turn", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		audit := &auditLog{}
		sdk, err := telemetry.NewSDKObserver(audit)
		if err != nil {
			t.Fatalf("NewSDKObserver: %v", err)
		}
		spy := &obsObserver{inner: sdk}
		run := newObservedRun(t, ctx, spy)
		req := obsInvoke(t, ctx, run, "inv-obs", "session-obs")

		observed := spy.observed()
		if len(observed) == 0 {
			t.Fatalf("观察未达: no committed fact reached the wired observer")
		}
		records := audit.snapshot()
		if len(records) != len(observed) {
			t.Fatalf("审计记录数 = %d, want one layered record per observed fact %d", len(records), len(observed))
		}
		for _, record := range records {
			if len(record.Attempts) != 2 || record.Attempts[0].Layer != telemetry.LayerTrace || record.Attempts[1].Layer != telemetry.LayerMetric {
				t.Fatalf("审计记录未分层: %+v", record.Attempts)
			}
			if record.Attempts[0].Skipped || record.Attempts[1].Skipped {
				t.Fatalf("审计记录误报跳过: %+v", record.Attempts)
			}
		}
		presentations := run.Presentations()
		if len(presentations) != len(observed) {
			t.Fatalf("presentation 数 = %d, want one After presentation per committed fact %d", len(presentations), len(observed))
		}
		seen := map[event.EventID]bool{}
		for _, evt := range observed {
			seen[evt.Key] = true
		}
		for _, pres := range presentations {
			if !seen[pres.Fact] {
				t.Fatalf("presentation 事实 %s 未被观察: observed = %v", pres.Fact, seen)
			}
		}
		for i, evt := range observed {
			if records[i].Fact != evt.Key {
				t.Fatalf("审计事实 = %s, want the observed fact identity %s", records[i].Fact, evt.Key)
			}
			if evt.Message.Content != obsToolOutput {
				t.Fatalf("观察事实内容 = %q, want the tool output %q byte-identical", evt.Message.Content, obsToolOutput)
			}
			if evt.Type != event.TypeActionCommand || evt.CallID != "call-1" || evt.InvocationID != req.InvocationID {
				t.Fatalf("观察事实身份 = %+v, want the call identity the fact committed under", evt)
			}
			if evt.Owner != req.Address || evt.Target != req.Address {
				t.Fatalf("观察事实地址 = %+v, want the address the call was admitted at %+v", evt, req.Address)
			}
		}
	})

	t.Run("缺口可从诊断读到: a failed observation lands as a named gap and drops nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		refusal := errors.New("the collector refused the observation")
		spy := &obsObserver{err: refusal}
		run := newObservedRun(t, ctx, spy)
		req := obsInvoke(t, ctx, run, "inv-gap", "session-gap")

		if len(spy.observed()) == 0 {
			t.Fatalf("失败观察未尝试: the observer was never reached")
		}
		presentations := run.Presentations()
		if len(presentations) == 0 {
			t.Fatalf("After 产物丢失: no presentation on the diagnostic read face")
		}
		gaps := 0
		for _, pres := range presentations {
			for _, gap := range pres.Gaps {
				gaps++
				if gap.Plugin != "commit-observer" || gap.Fact != pres.Fact || gap.Err == nil {
					t.Fatalf("缺口未具名: %+v", gap)
				}
				if !strings.Contains(gap.Err.Error(), refusal.Error()) {
					t.Fatalf("缺口原因 = %v, want the named observation failure", gap.Err)
				}
			}
		}
		if gaps == 0 {
			t.Fatalf("缺口被丢弃: %+v", presentations)
		}
		snapshot, err := run.Query(ctx, query.ListFilter{Scope: query.AuthorizationScope{
			Agents:   []event.AgentID{"agent-a"},
			Sessions: []event.Address{req.Address},
		}})
		if err != nil {
			t.Fatalf("query beside the failed observation: %v", err)
		}
		if len(obsKindCounts(snapshot)) == 0 {
			t.Fatalf("query 读面被观察失败改写: %+v", snapshot.Details)
		}
	})

	t.Run("观察不建第二真源: the observation hangs read-only off the committed ledger", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		audit := &auditLog{}
		sdk, err := telemetry.NewSDKObserver(audit)
		if err != nil {
			t.Fatalf("NewSDKObserver: %v", err)
		}
		spy := &obsObserver{inner: sdk}
		observedRun := newObservedRun(t, ctx, spy)
		plainRun := newObservedRun(t, ctx, nil)
		req := obsInvoke(t, ctx, observedRun, "inv-readonly", "session-readonly")
		obsInvoke(t, ctx, plainRun, "inv-readonly", "session-readonly")

		scope := query.AuthorizationScope{
			Agents:   []event.AgentID{"agent-a"},
			Sessions: []event.Address{req.Address},
		}
		withObservation, err := observedRun.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the observed run: %v", err)
		}
		withoutObservation, err := plainRun.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the plain run: %v", err)
		}
		if got, want := obsKindCounts(withObservation), obsKindCounts(withoutObservation); !reflect.DeepEqual(got, want) {
			t.Fatalf("观察改写了读面: %v, want the same readings as the unobserved run %v", got, want)
		}
		for _, evt := range spy.observed() {
			pres := runPresentationOf(observedRun, evt.Key)
			if pres == nil {
				t.Fatalf("观察事实 %s 无 After 产物", evt.Key)
			}
			if pres.Fact != evt.Key {
				t.Fatalf("观察身份与产物不符: %s != %s", evt.Key, pres.Fact)
			}
		}
	})
}

// runPresentationOf reports the After presentation of the named committed
// fact, or nil when the run holds none for it.
func runPresentationOf(run *tagent.Run, id event.EventID) *view.Presentation {
	for _, pres := range run.Presentations() {
		if pres.Fact == id {
			got := pres
			return &got
		}
	}
	return nil
}
