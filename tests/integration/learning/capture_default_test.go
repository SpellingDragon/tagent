package learning

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestRecorderDefaultOff pins the recording default of the assembled runtime: a run assembled beside no capture records nothing.
//   - the formal assembly entry attaches no capture unless one is given
//   - a turn driven beside no capture fixes no request snapshot and lands no record line
//   - the turn still completes, so nothing in the execution path depends on a capture
//   - the capture is opened by the host only when the document names a storage root (assembly/host.go)
//   - the retired trajectory-dump switch stays a rejected declaration field (api/config strict check), so a run not configured for recording writes nothing
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestRecorderDefaultOff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider := &learnModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return learnFinalTurn("answered without a capture")
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&learnTool{name: "echo-a", output: "out-a"}}},
	}, assembly.WithGovernance(formalPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	if fixed := run.RequestSnapshots(); len(fixed) != 0 {
		t.Fatalf("request snapshots = %+v, want none while no capture is attached", fixed)
	}
	turn, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-default-off",
		Episode:      "ep-default-off",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-default-off"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "record me"}},
	})
	if err != nil {
		t.Fatalf("invoke the runtime beside no capture: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed terminal", turn.Terminal)
	}
	if fixed := run.RequestSnapshots(); len(fixed) != 0 {
		t.Fatalf("request snapshots after the turn = %+v, want none while no capture is attached", fixed)
	}
}
