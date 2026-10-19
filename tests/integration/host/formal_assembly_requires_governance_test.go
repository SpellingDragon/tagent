package host

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// passPolicy is the deterministic stand-in of the real governance policy the
// formal assembly is configured with in these checks: the fixed passing
// dispositions are mechanism inputs carrying no quality conclusion, and the
// formal quality assessment lives in the structural evaluation gate.
type passPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (passPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       passingCheck("eval"),
		"benchmark":  passingCheck("benchmark"),
		"apply-gate": passingCheck("apply-gate"),
	}, nil
}

// passingCheck reports the passing disposition of one named mechanism check.
func passingCheck(name state.CheckName) state.CheckAnswer {
	return state.CheckAnswer{Name: name, Verdict: state.CheckPass, Reason: "the stand-in passes the mechanism check", Scope: "assembly", Evidence: []string{"deterministic stand-in"}}
}

// rejectPolicy is the governance stand-in whose disposition refuses the named
// check it carries, so the publication is proved to consult the injected
// governance instead of any fixed pass beside the assembly.
type rejectPolicy struct{ name state.CheckName }

// Check implements state.Governance beside the refusal it carries.
func (p rejectPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		p.name: {Name: p.name, Verdict: state.CheckReject, Reason: "the stand-in refuses this publication", Scope: "assembly"},
	}, nil
}

// TestFormalAssemblyRequiresGovernance drives the sole assembly entry over the public root and checks that a formal assembly is built over real governance alone.
//   - beside no governance the assembly is refused by name before any publication and constructs no run
//   - beside a governance whose disposition refuses the candidate the publication is refused through the injected policy, never a fixed pass
//   - beside a governance that answers the required checks the very same path assembles one live run
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestFormalAssemblyRequiresGovernance(t *testing.T) {
	assemble := func(t *testing.T, ctx context.Context, opts ...tagent.Option) (*tagent.Run, error) {
		t.Helper()
		provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
			return entryFinalTurn("settled")
		}}
		tool := &entryTool{name: "echo", output: "out-a"}
		decl := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
		}
		return tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
		}, opts...)
	}

	t.Run("beside no governance the assembly is refused by name and constructs no run", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, err := assemble(t, ctx)
		if !errors.Is(err, tagent.ErrGovernanceMissing) {
			t.Fatalf("assemble beside no governance = (%v, %v), want the named refusal of the missing governance", run, err)
		}
		if run != nil {
			t.Fatalf("run beside no governance = %v, want no run constructed before the publication", run)
		}
	})

	t.Run("a governance that refuses the candidate refuses the publication", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, err := assemble(t, ctx, tagent.WithGovernance(rejectPolicy{name: "eval"}))
		if !errors.Is(err, state.ErrCheckRefused) {
			t.Fatalf("assemble beside a refusing governance = (%v, %v), want the publication refused by the injected policy", run, err)
		}
		if run != nil {
			t.Fatalf("run beside a refusing governance = %v, want no run constructed beside a refused publication", run)
		}
	})

	t.Run("a governance that answers the required checks assembles one live run", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, err := assemble(t, ctx, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble beside a passing governance: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-governed",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-governed"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the governed run: %v", err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the completed turn of the governed run", turn.Terminal)
		}
	})
}
