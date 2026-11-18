package history

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	"github.com/SpellingDragon/tagent/runtime/agent"
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
	if mode := os.Getenv(restoreFoldChildModeEnv); mode != "" {
		runRestoreFoldChild(t, mode)
		return
	}
	t.Run("a restart states the chains it folded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		interruptRestoreFoldRun(t, cfg.Root)

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

// restoreFoldChildModeEnv, restoreFoldChildRootEnv, restoreFoldChildOutEnv and
// restoreFoldChildMode name the handshake the fold check hands the first runtime it
// interrupts: the one phase that child runs, the root it writes and the report it
// lands once its chain is durable.
const (
	restoreFoldChildModeEnv = "TAGENT_RESTORE_FOLD_CHILD_MODE"
	restoreFoldChildRootEnv = "TAGENT_RESTORE_FOLD_CHILD_ROOT"
	restoreFoldChildOutEnv  = "TAGENT_RESTORE_FOLD_CHILD_OUT"

	restoreFoldChildMode = "fold"
)

// interruptRestoreFoldRun runs the runtime the fold check restarts in a process of
// its own and interrupts it once the chain it committed is durable.
//   - A close ends that view with the checkpoint the restart would restore instead of folding, so the kill leaves the root holding no shutdown state.
//   - The root keeps no writer after the kill: the write lock follows the process's descriptors, so the restart takes write rights.
func interruptRestoreFoldRun(t *testing.T, root string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "restore-fold.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRestoreObservationThroughTheFormalEntry$", "-test.timeout=2m")
	cmd.Env = append(os.Environ(),
		restoreFoldChildModeEnv+"="+restoreFoldChildMode,
		restoreFoldChildRootEnv+"="+root,
		restoreFoldChildOutEnv+"="+out,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the first runtime: %v", err)
	}
	report := waitConsumptionReport(t, out, &output, restoreFoldChildMode)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("interrupt the first runtime pid=%d: %v", report.PID, err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatalf("the first runtime ended without the parent's kill; output=%s", output.String())
	}
}

// runRestoreFoldChild drives the chain the restart folds and then waits for the
// parent's interruption, so the root holds the chain and no earlier boot's view.
func runRestoreFoldChild(t *testing.T, mode string) {
	t.Helper()
	root := os.Getenv(restoreFoldChildRootEnv)
	out := os.Getenv(restoreFoldChildOutEnv)
	if mode != restoreFoldChildMode || root == "" || out == "" {
		t.Fatalf("the fold child was handed mode=%q root=%q out=%q, want the one phase over a named root", mode, root, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: &chainModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return chainToolTurn(chainCall("call-1", "probe", `{"x":1}`))
			}
			return chainFinalTurn("settled")
		}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
	}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if err != nil {
		t.Fatalf("assemble the first runtime: %v", err)
	}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-chain",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-chain"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("invoke the first runtime: %v", err)
	}
	writeConsumptionReport(t, out, consumptionChildReport{
		Mode:  mode,
		PID:   os.Getpid(),
		Notes: []string{"the first runtime waits for the parent's kill and files no closing checkpoint"},
	})
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Minute):
	}
}

// commitDamagedChain lands one durable chain whose links carry no admission
// material, so the restore has to refuse it by name.
//   - the open declares the root's write rights, so the fixture commits as the root's one writer
//   - the assemble that follows opens the same root, so this view ends here
func commitDamagedChain(ctx context.Context, cfg ledger.StorageConfig) (err error) {
	cfg.WriteRights = true
	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := tier.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
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
// its own face and leaves the package logger as it found it. The swap runs
// through the locked framework entry: a raw write beside a running consumer is
// the race the lock exists to close.
func watchHistoryLog(t *testing.T, observer trpclog.Logger) func() {
	t.Helper()
	restore := agent.SwapFrameworkLogger(observer)
	t.Cleanup(restore)
	return restore
}
