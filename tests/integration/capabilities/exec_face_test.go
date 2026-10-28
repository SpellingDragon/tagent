package capabilities

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
)

// TestExecFaceRunsTheDeclaredRhythm drives the execution face one declaration grants through the public assembly:
// - the one declared exec id answers with the run and the release tool under their own names
// - the run tool executes against the wired resource ledger and holds the acquisition it opened
// - the monitor rhythm the declaration fixed rides the acquisition the run opened
// - the release tool stops the very resource the run started and frees it
// - a declaration that grants the exec face beside no ledger refuses both calls by name and starts nothing
//
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestExecFaceRunsTheDeclaredRhythm(t *testing.T) {
	t.Run("the declared exec id runs and releases the resource under the declared rhythm", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		journal := &elJournal{}
		executor := newELExecutor(journal)
		manager := tagentexec.NewManager(executor)

		round := 0
		provider := &ntModel{name: "model-exec", answer: func(req *model.Request) *model.Response {
			round++
			switch round {
			case 1:
				return ntToolTurn(ntCall("call-run", "exec_run", `{"command":"probe","kind":"probe","resource":"r1"}`))
			case 2:
				for _, name := range []string{"exec_run", "exec_release"} {
					if req.Tools[name] == nil {
						t.Errorf("second request tools carry no %q, want the declared exec face granted", name)
					}
				}
				if req.Tools[tagentexec.PresetName] != nil {
					t.Errorf("second request tools carry %q, want the declaration id to name the face and never become a tool", tagentexec.PresetName)
				}
				checkObservedBody(t, req, "call-run", "exec_run", "executed probe/r1: frame:r1")
				explanation, err := manager.Explain(tagentexec.ResourceKey{Kind: "probe", Key: "r1"})
				if err != nil || !explanation.Held {
					t.Fatalf("explanation of the held resource = (%+v, %v), want the run to hold the acquisition it opened", explanation, err)
				}
				if explanation.Rhythm != execDeclaredRhythm() {
					t.Errorf("held rhythm = %+v, want the monitor rhythm the declaration fixed", explanation.Rhythm)
				}
				return ntToolTurn(ntCall("call-release", "exec_release", `{"kind":"probe","resource":"r1"}`))
			default:
				checkObservedBody(t, req, "call-release", "exec_release", "released probe/r1 after confirmed stop: exit=0")
				return ntFinalTurn("the execution face ran and released")
			}
		}}

		run, err := assembly.Assemble(ctx, execFaceDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-exec": {Model: provider},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithExecManager(manager))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-exec-face",
			Address:      event.Address{AgentID: "agent-exec", SessionID: "session-exec-face"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "run the probe"}},
		})
		if err != nil {
			t.Fatalf("invoke the exec face: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 3 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside three decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := executor.starts; got != 1 {
			t.Errorf("execution starts = %d, want the run tool to start exactly one execution", got)
		}
		if got := executor.stops; got != 1 {
			t.Errorf("execution stops = %d, want the release tool to stop the very resource the run started", got)
		}
		if held := manager.Held(); held != 0 {
			t.Errorf("held resources = %d, want the confirmed release to free the acquisition", held)
		}
	})

	t.Run("a declaration beside no ledger refuses both calls by name and starts nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		executor := newELExecutor(&elJournal{})

		round := 0
		provider := &ntModel{name: "model-exec", answer: func(req *model.Request) *model.Response {
			round++
			if round == 1 {
				return ntToolTurn(
					ntCall("call-run", "exec_run", `{"command":"probe","kind":"probe","resource":"r1"}`),
					ntCall("call-release", "exec_release", `{"kind":"probe","resource":"r1"}`),
				)
			}
			refusal := tagentexec.ErrUnwired.Error()
			checkObservedBody(t, req, "call-run", "exec_run", refusal)
			checkObservedBody(t, req, "call-release", "exec_release", refusal)
			return ntFinalTurn("the unwired execution face refused both calls")
		}}

		run, err := assembly.Assemble(ctx, execFaceDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-exec": {Model: provider},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-exec-face-unwired",
			Address:      event.Address{AgentID: "agent-exec", SessionID: "session-exec-face-unwired"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "run the probe"}},
		}); err != nil {
			t.Fatalf("invoke the exec face: %v", err)
		}
		if got := executor.starts; got != 0 {
			t.Errorf("execution starts = %d, want the refusal to start nothing", got)
		}
	})
}

// execFaceDeclaration declares one agent that grants the execution face under
// the monitor rhythm the deployment fixes.
func execFaceDeclaration() *config.Declaration {
	spec := config.AgentSpec{
		ID:    "agent-exec",
		Model: "model-exec",
		Tools: []config.ToolRef{{Name: tagentexec.PresetName}},
	}
	spec.Exec = config.ExecSpec{Monitor: execDeclaredMonitor()}
	return &config.Declaration{
		Entry:  "agent-exec",
		Agents: []config.AgentSpec{spec},
	}
}

// execDeclaredMonitor is the monitor rhythm the execution declaration fixes:
// the cadence, the stable span and the fake-dead span of the old deployment,
// beside no interactive span, which keeps its runtime default.
func execDeclaredMonitor() config.MonitorSpec {
	return config.MonitorSpec{
		Interval:         3 * time.Second,
		StableDuration:   30 * time.Second,
		FakeDeadDuration: 180 * time.Second,
	}
}

// execDeclaredRhythm is the rhythm the run is expected to acquire under: the
// declared spans beside the runtime default of the interactive span.
func execDeclaredRhythm() tagentexec.MonitorConfig {
	return tagentexec.MonitorConfig{
		Interval:                  3 * time.Second,
		StableDuration:            30 * time.Second,
		InteractiveStableDuration: tagentexec.DefaultMonitorConfig.InteractiveStableDuration,
		FakeDeadDuration:          180 * time.Second,
	}
}
