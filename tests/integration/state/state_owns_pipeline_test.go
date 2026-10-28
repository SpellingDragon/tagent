package state

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task"

	orgstate "github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

type stubModel struct {
	name string
}

func (m *stubModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	return nil, nil
}

func (m *stubModel) Info() model.Info {
	return model.Info{Name: m.name}
}

type stubTool struct {
	name string
}

func (t *stubTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

type stubSet struct {
	name   string
	closed bool
}

func (s *stubSet) Tools(context.Context) []trpctool.Tool {
	return nil
}

func (s *stubSet) Close() error {
	s.closed = true
	return nil
}

func (s *stubSet) Name() string {
	return s.name
}

type stubPlugin struct {
	name string
	port plugin.Port
}

func (p *stubPlugin) Name() string {
	return p.name
}

func (p *stubPlugin) Port() plugin.Port {
	return p.port
}

// TestStateOwnsPipeline checks how the state owner holds the candidate live bindings and hands one generation to each request as its execution begins.
// - a prepared plan borrows exactly the binding the state owner holds, with the model, tools and plugin pipeline of one generation in the published order
// - a candidate that stops midway releases its acquired leases in reverse order and once each, leaving the shared faces and the live bindings untouched
// - a request keeps the generation it borrowed until it releases the handle while a later borrow takes the newer generation
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestStateOwnsPipeline(t *testing.T) {
	t.Run("a prepared plan borrows the binding the state owner holds", func(t *testing.T) {
		ctx := context.Background()
		decl := &config.Declaration{
			Entry: "agent-a",
			Agents: []config.AgentSpec{
				{ID: "agent-a", Model: "model-a"},
				{ID: "agent-b", Model: "model-b"},
			},
		}
		plan := orgstate.OrgPlan{Declaration: decl, Baseline: "v1", Fingerprint: "fp-1"}
		first := &stubModel{name: "model-a"}
		second := &stubModel{name: "model-b"}
		set := &stubSet{name: "set-a"}
		bindingA := orgstate.AgentBinding{
			Model: first,
			Tools: []trpctool.Tool{&stubTool{name: "tool-1"}, &stubTool{name: "tool-2"}},
			Plugins: []plugin.Instance{
				&stubPlugin{name: "enrich", port: plugin.PortCallTransform},
				&stubPlugin{name: "risk", port: plugin.PortCallRuling},
			},
			PromptSource: orgstate.PromptSource{Files: []config.FileRef{{Path: "prompts/a.md", Kind: config.FileKindPrompt}}},
			Set:          set,
		}
		bindingB := orgstate.AgentBinding{Model: second}
		leases := orgstate.NewLeases()
		leases.Register("toolset", "agent-a", func(context.Context) error { return nil })

		built, err := orgstate.NewStatePlan(plan, map[event.AgentID]orgstate.AgentBinding{"agent-a": bindingA, "agent-b": bindingB}, leases.Refs())
		if err != nil {
			t.Fatalf("instantiate the candidate plan: %v", err)
		}
		if len(built.Agents) != 2 || len(built.Leases) != 1 {
			t.Fatalf("candidate plan = %d bindings beside %d leases, want one binding per declared agent beside the registrations made", len(built.Agents), len(built.Leases))
		}
		if _, err := orgstate.NewStatePlan(plan, map[event.AgentID]orgstate.AgentBinding{"agent-a": bindingA}, nil); !errors.Is(err, orgstate.ErrAgentBindingMissing) {
			t.Fatalf("plan missing one declared agent = %v, want %v beside a refusal that names the agent", err, orgstate.ErrAgentBindingMissing)
		}

		holder := orgstate.NewBindings()
		if err := holder.Hold(built); err != nil {
			t.Fatalf("hold the candidate bindings: %v", err)
		}
		handle, err := holder.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as the request begins executing: %v", err)
		}
		borrowed := handle.Binding
		if borrowed.Model != first {
			t.Fatalf("borrowed model = %v, want the model the state owner holds", borrowed.Model)
		}
		if len(borrowed.Tools) != 2 || borrowed.Tools[0].Declaration().Name != "tool-1" || borrowed.Tools[1].Declaration().Name != "tool-2" {
			t.Fatalf("borrowed tools = %v, want the tools of the held generation in their declared order", borrowed.Tools)
		}
		if len(borrowed.Plugins) != 2 || borrowed.Plugins[0].Name() != "enrich" || borrowed.Plugins[1].Name() != "risk" {
			t.Fatalf("borrowed plugins = %v, want the published pipeline order held beside the model", borrowed.Plugins)
		}
		if borrowed.Set != set {
			t.Fatalf("borrowed tool set = %v, want the tool-set connection of the same generation", borrowed.Set)
		}
		if _, err := holder.Borrow(ctx, "agent-z"); !errors.Is(err, orgstate.ErrAgentUnknown) {
			t.Fatalf("borrow of an agent no binding serves = %v, want %v beside a refusal that names the agent", err, orgstate.ErrAgentUnknown)
		}
	})

	t.Run("a candidate that stops midway releases its leases in reverse", func(t *testing.T) {
		ctx := context.Background()
		authority := orgstate.NewAuthority()
		store := orgstate.NewMemoryStore(authority)
		addr := event.Address{AgentID: "agent-c", SessionID: "session-c"}
		if err := authority.GrantRead(ctx, "subject-a", addr); err != nil {
			t.Fatalf("record the read grant: %v", err)
		}
		if _, err := store.Establish(ctx, addr, "instance-1"); err != nil {
			t.Fatalf("establish the site: %v", err)
		}
		if err := store.TrackDelivery(ctx, "task-shared", task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}); err != nil {
			t.Fatalf("register the delivery target: %v", err)
		}
		notice := task.Notice{TaskID: "task-shared", Round: 1, Kind: task.NoticeAccepted, Seq: 1}
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("accept the input on the shared face: %v", err)
		}
		if err := store.Claim(ctx, addr, "instance-1", 1); err != nil {
			t.Fatalf("claim the input on the shared face: %v", err)
		}

		live := &stubModel{name: "live"}
		holder := orgstate.NewBindings()
		if err := holder.Hold(orgstate.StatePlan{Agents: map[event.AgentID]orgstate.AgentBinding{"agent-a": {Model: live}}}); err != nil {
			t.Fatalf("hold the generation already in service: %v", err)
		}

		var released []string
		leases := orgstate.NewLeases()
		leases.Register("model", "model-a", func(context.Context) error {
			released = append(released, "model-a")
			return nil
		})
		leases.Register("toolset", "agent-a", func(context.Context) error {
			released = append(released, "agent-a")
			return errors.New("tool-set close refused")
		})
		leases.Register("model", "model-b", func(context.Context) error {
			released = append(released, "model-b")
			return nil
		})

		err := leases.ReleaseAll(ctx)
		if err == nil {
			t.Fatalf("release of a failed candidate = nil, want the joined release failure kept beside the cause")
		}
		if len(released) != 3 || released[0] != "model-b" || released[1] != "agent-a" || released[2] != "model-a" {
			t.Fatalf("released = %v, want the acquisition order reversed and each resource freed once", released)
		}
		if err := leases.ReleaseAll(ctx); err != nil {
			t.Fatalf("second release pass = %v, want nothing left to free", err)
		}
		if len(released) != 3 {
			t.Fatalf("released = %v, want each resource freed exactly once across every release pass", released)
		}

		read := site(t, store, "subject-a", addr, "instance-1")
		if len(read.Mailbox) != 1 || read.Mailbox[0].Claim != orgstate.ClaimClaimed {
			t.Fatalf("shared mailbox = %+v, want the accepted and claimed input kept as the shared face left it", read.Mailbox)
		}
		handle, err := holder.Borrow(ctx, "agent-a")
		if err != nil || handle.Binding.Model != live {
			t.Fatalf("borrow beside a failed candidate = (%v, %v), want the live generation left in service", handle, err)
		}
	})

	t.Run("an in-flight request keeps its borrowed generation", func(t *testing.T) {
		ctx := context.Background()
		decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model"}}}
		oldModel := &stubModel{name: "gen-1"}
		newModel := &stubModel{name: "gen-2"}
		oldPlan := orgstate.StatePlan{Plan: orgstate.OrgPlan{Declaration: decl}, Agents: map[event.AgentID]orgstate.AgentBinding{"agent-a": {Model: oldModel}}}
		newPlan := orgstate.StatePlan{Plan: orgstate.OrgPlan{Declaration: decl}, Agents: map[event.AgentID]orgstate.AgentBinding{"agent-a": {Model: newModel}}}

		holder := orgstate.NewBindings()
		if err := holder.Hold(oldPlan); err != nil {
			t.Fatalf("hold the first generation: %v", err)
		}
		handle, err := holder.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as the in-flight request begins executing: %v", err)
		}
		if holder.Inflight() != 1 {
			t.Fatalf("in-flight holders = %d, want 1 covering the request still running", holder.Inflight())
		}

		if err := holder.Hold(newPlan); err != nil {
			t.Fatalf("hold the newer generation: %v", err)
		}
		if handle.Binding.Model != oldModel {
			t.Fatalf("in-flight binding model = %v, want the generation the request took kept until it finishes", handle.Binding.Model)
		}
		queued, err := holder.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as the queued input begins executing: %v", err)
		}
		if queued.Binding.Model != newModel {
			t.Fatalf("queued input binding model = %v, want the generation effective at execution start and no binding taken while it waited", queued.Binding.Model)
		}
		if holder.Inflight() != 2 {
			t.Fatalf("in-flight holders = %d, want 2 covering both requests still running", holder.Inflight())
		}

		if err := handle.Release(ctx); err != nil {
			t.Fatalf("end the use of the borrowed generation: %v", err)
		}
		if holder.Inflight() != 1 {
			t.Fatalf("in-flight holders = %d, want 1 covering the request still running after its neighbour finished", holder.Inflight())
		}
		if err := handle.Release(ctx); !errors.Is(err, orgstate.ErrHandleReleased) {
			t.Fatalf("second release of one handle = %v, want %v beside a use that ends exactly once", err, orgstate.ErrHandleReleased)
		}
	})
}
