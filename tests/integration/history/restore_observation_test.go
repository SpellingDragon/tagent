package history

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestRestoreObservationThroughTheFormalEntry checks the lines a restart states, through the formal public entries.
//   - a restart over a durable tier states the tier it opened and the chains it folded
//   - a chain carrying no admission material is counted among the chains dropped
//   - a tier keeping no record across process exit is warned beside the tier it opened
//   - a failing logger changes no recovery: the bypass drops the line and keeps the report
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestRestoreObservationThroughTheFormalEntry(t *testing.T) {
	t.Run("a restart states the chains it folded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-chain"}
		first, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(req *model.Request) *model.Response {
				if len(req.Messages) == 1 {
					return chainToolTurn(chainCall("call-1", "probe", `{"x":1}`))
				}
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(cfg))
		if err != nil {
			t.Fatalf("assemble the durable runtime: %v", err)
		}
		if _, err := first.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-chain",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		}); err != nil {
			t.Fatalf("invoke the durable runtime: %v", err)
		}

		observer := &historyObserver{}
		defer watchHistoryLog(t, observer)()
		restarted, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(cfg))
		if err != nil {
			t.Fatalf("restart the durable runtime: %v", err)
		}
		if report := restarted.Recovery(); len(report.Tasks) != 1 || !report.Tasks[0].Recovered {
			t.Fatalf("recovery report = %+v, want the one chain rebuilt", report.Tasks)
		}
		if !observer.mentions("info", "storage tier opened", "tier=localfile", "durable=true") {
			t.Fatalf("info lines = %v, want the opened durable tier", observer.all())
		}
		if !observer.mentions("info", "restore folded", "chains=1", "recovered=1", "dropped=0", "elapsed=") {
			t.Fatalf("info lines = %v, want the fold beside its chains, its recovery and its drops", observer.all())
		}
		if observer.mentions("warn", "storage tier keeps no record") {
			t.Fatalf("warn lines = %v, want no degradation warning beside a durable tier", observer.all())
		}
	})

	t.Run("a chain carrying no admission material is counted among the chains dropped", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		if err := commitDamagedChain(ctx, cfg); err != nil {
			t.Fatalf("build the damaged chain: %v", err)
		}

		observer := &historyObserver{}
		defer watchHistoryLog(t, observer)()
		run, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(cfg))
		if err != nil {
			t.Fatalf("assemble beside the damaged chain: %v", err)
		}
		if report := run.Recovery(); len(report.Tasks) != 1 || report.Tasks[0].Recovered {
			t.Fatalf("recovery report = %+v, want the one chain refused", report.Tasks)
		}
		if !observer.mentions("info", "restore folded", "chains=1", "recovered=0", "dropped=1") {
			t.Fatalf("info lines = %v, want the refused chain counted among the drops", observer.all())
		}
	})

	t.Run("a tier keeping no record across process exit is warned", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		observer := &historyObserver{}
		defer watchHistoryLog(t, observer)()
		if _, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory})); err != nil {
			t.Fatalf("assemble over the in-process tier: %v", err)
		}
		if !observer.mentions("info", "storage tier opened", "tier=memory", "durable=false") {
			t.Fatalf("info lines = %v, want the opened tier beside its durability", observer.all())
		}
		if !observer.mentions("warn", "storage tier keeps no record", "tier=memory", "restored=none") {
			t.Fatalf("warn lines = %v, want the degradation the in-process tier states", observer.all())
		}
	})

	t.Run("a failing logger changes no recovery", func(t *testing.T) {
		for _, face := range []string{"tagent: storage tier", "tagent: restore"} {
			t.Run(face, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
				if err := commitDamagedChain(ctx, cfg); err != nil {
					t.Fatalf("build the damaged chain: %v", err)
				}
				observer := &historyObserver{refuse: face}
				defer watchHistoryLog(t, observer)()
				run, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
					"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
						return chainFinalTurn("settled")
					}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
				}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(cfg))
				if err != nil {
					t.Fatalf("assemble beside a failing logger: %v", err)
				}
				if report := run.Recovery(); len(report.Tasks) != 1 || report.Tasks[0].TaskID != "task-damaged" {
					t.Fatalf("recovery report = %+v, want the damaged chain reported beside a failing logger", report.Tasks)
				}
				if observer.refusals() == 0 {
					t.Fatalf("refused lines = %d, want the line of %q refused", observer.refusals(), face)
				}
			})
		}
	})
}

// commitDamagedChain lands one durable chain whose links carry no admission
// material, so the restore has to refuse it by name.
func commitDamagedChain(ctx context.Context, cfg ledger.StorageConfig) error {
	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		return err
	}
	fixture, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		return err
	}
	result, err := fixture.Commit(ctx, task.CommitRecord{
		MutationID:       "admit:task-damaged",
		TaskID:           "task-damaged",
		ExpectedRevision: 0,
		Status:           task.StatusAccepted,
	})
	if err != nil {
		return err
	}
	if result != task.CommitCommitted {
		return fmt.Errorf("commit the damaged chain = %v, want it committed", result)
	}
	return nil
}

// historyObserver is the framework logger the observation checks of this package
// read the assembly and compression faces through: it records every line beside
// its level and refuses, by panicking, the writes carrying the fragment it was
// made to refuse.
type historyObserver struct {
	mu         sync.Mutex
	lines      []string
	panicsLeft int
	refuse     string
	refused    int
}

// record takes one line at the named level, or refuses it by panicking.
func (o *historyObserver) record(level, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.panicsLeft > 0 {
		o.panicsLeft--
		panic("the logger refused the write")
	}
	if o.refuse != "" && strings.Contains(text, o.refuse) {
		o.refused++
		panic("the logger refused the write")
	}
	o.lines = append(o.lines, level+": "+text)
}

// Debug records one debug line.
func (o *historyObserver) Debug(args ...any) { o.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (o *historyObserver) Debugf(format string, args ...any) {
	o.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (o *historyObserver) Info(args ...any) { o.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (o *historyObserver) Infof(format string, args ...any) {
	o.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (o *historyObserver) Warn(args ...any) { o.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (o *historyObserver) Warnf(format string, args ...any) {
	o.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (o *historyObserver) Error(args ...any) { o.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (o *historyObserver) Errorf(format string, args ...any) {
	o.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (o *historyObserver) Fatal(args ...any) { o.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (o *historyObserver) Fatalf(format string, args ...any) {
	o.record("fatal", fmt.Sprintf(format, args...))
}

// all reports every line taken, whatever the level.
func (o *historyObserver) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// refusals reports how many lines carrying the refused fragment were turned away.
func (o *historyObserver) refusals() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.refused
}

// mentions reports whether one line taken at the level carries every fragment.
func (o *historyObserver) mentions(level string, fragments ...string) bool {
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

// watchHistoryLog points the framework logger at the observer for the duration of
// one check and answers the restore that ends it, so a check reads the lines of
// its own face and leaves the package logger as it found it.
func watchHistoryLog(t *testing.T, observer trpclog.Logger) func() {
	t.Helper()
	original := trpclog.Default
	trpclog.Default = observer
	restore := func() { trpclog.Default = original }
	t.Cleanup(restore)
	return restore
}
