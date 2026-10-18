// The deployment soak checks of the host drive the runtime as a resident
// process over real durable storage: the continuity a resident keeps round
// after round, the recovery a killed process is read back from, the switch and
// return of a configuration on a live runtime, and the store ownership beside
// the resource budget the resident holds.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// soakModeResident, soakModeCrash, soakModeReader, soakModeHot and soakModeLeak
// name the work orders a soak child runs in: each mode is its own process, so
// every evidence leg is a real process start or a real process end.
const (
	soakModeResident = "resident"
	soakModeCrash    = "crash"
	soakModeReader   = "reader"
	soakModeHot      = "hot"
	soakModeLeak     = "leak"
)

// soakChildModeEnv, soakChildRootEnv, soakChildOutEnv, soakChildRoundsEnv and
// soakChildEventsEnv name the work-order fields a soak child reads.
const (
	soakChildModeEnv   = "TAGENT_SOAK_CHILD_MODE"
	soakChildRootEnv   = "TAGENT_SOAK_CHILD_ROOT"
	soakChildOutEnv    = "TAGENT_SOAK_CHILD_OUT"
	soakChildRoundsEnv = "TAGENT_SOAK_CHILD_ROUNDS"
	soakChildEventsEnv = "TAGENT_SOAK_CHILD_EVENTS"
)

// soakLedgerName names the account file one soak child writes, and the slack
// constants bound the resident budget between its warm baseline and its last
// round.
const (
	soakLedgerName       = "events.jsonl"
	soakGoroutineSlack   = 8
	soakDescriptorSlack  = 12
	soakChildReadyWait   = 90 * time.Second
	soakChildStartWindow = 120 * time.Second
)

var (
	soakRounds = flag.Int("rounds", 8, "rounds a soak child drives over its resident runtime")
	soakEvents = flag.Int("events-per-round", 4, "invocations a soak child drives in one round")
)

// soakEvent is one line of the machine-readable account a soak child writes:
// the numbers a parent holds the child's claim against.
type soakEvent struct {
	Event       string   `json:"event"`
	PID         int      `json:"pid"`
	Round       int      `json:"round,omitempty"`
	Invocations int      `json:"invocations,omitempty"`
	Tasks       int      `json:"tasks,omitempty"`
	TaskIDs     []string `json:"task_ids,omitempty"`
	Goroutines  int      `json:"goroutines,omitempty"`
	OpenFDs     int      `json:"open_fds,omitempty"`
	Generation  uint64   `json:"generation,omitempty"`
	Rollback    uint64   `json:"rollback_source,omitempty"`
	Model       string   `json:"model,omitempty"`
	Requests    int      `json:"model_requests,omitempty"`
	Executions  int      `json:"tool_executions,omitempty"`
	Drained     bool     `json:"drained,omitempty"`
	Unsettled   int      `json:"unsettled,omitempty"`
	ElapsedMS   int64    `json:"elapsed_ms,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// TestSoakContinuity drives the runtime as a deployed resident process and checks the continuity it keeps across process boundaries.
// - a resident process drives its rounds over real durable storage and reports every round it completed
// - a process killed without a close is read back by a second process that names what it recovered, while a reader over a root nothing wrote recovers nothing
// - a live runtime serves the generation a publication admitted and serves the previous content again after the return
// - the store stays owned by the invoking user with no world-writable entry, and the resident holds a bounded descriptor and goroutine budget
//
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
func TestSoakContinuity(t *testing.T) {
	if mode := os.Getenv(soakChildModeEnv); mode != "" {
		soakChild(t, mode)
		return
	}
	if testing.Short() {
		t.Skip("the deployment soak drives real processes; run it without -short")
	}

	t.Run("常驻 soak：一个真实进程连续承接多轮调用并读回每轮账", func(t *testing.T) {
		root, out := t.TempDir(), t.TempDir()
		child := startSoakChild(t, soakModeResident, root, out, *soakRounds, *soakEvents)
		child.wait(t)
		if child.exitCode != 0 {
			t.Fatalf("resident child exit = %d, want a clean end; stderr = %s", child.exitCode, child.stderr.String())
		}
		events := child.events
		rounds := soakEventsNamed(events, "round")
		if len(rounds) != *soakRounds {
			t.Fatalf("rounds reported = %d, want %d; events = %+v", len(rounds), *soakRounds, events)
		}
		for i, ev := range rounds {
			if ev.PID != child.pid {
				t.Fatalf("round %d reported by pid %d, want the one resident process %d", ev.Round, ev.PID, child.pid)
			}
			if ev.Round != i+1 {
				t.Fatalf("round numbering = %d at position %d, want the round it drove", ev.Round, i+1)
			}
			if want := ev.Round * *soakEvents; ev.Invocations != want || ev.Tasks != want {
				t.Fatalf("round %d = %d invocations beside %d durable tasks, want %d of each", ev.Round, ev.Invocations, ev.Tasks, want)
			}
			if ev.ElapsedMS < 0 {
				t.Fatalf("round %d elapsed = %dms, want the resident's own clock", ev.Round, ev.ElapsedMS)
			}
		}
		closed := soakEventsNamed(events, "closed")
		if len(closed) != 1 {
			t.Fatalf("close events = %d, want the one close the resident reported", len(closed))
		}
		if !closed[0].Drained || closed[0].Unsettled != 0 {
			t.Fatalf("close = %+v, want a converged drain beside no unsettled obligation", closed[0])
		}
		if want := *soakRounds * *soakEvents; closed[0].Executions != want {
			t.Fatalf("tool executions = %d, want the %d admitted calls of the soak", closed[0].Executions, want)
		}
		if closed[0].Tasks != *soakRounds**soakEvents {
			t.Fatalf("tasks at close = %d, want every invocation's task still readable", closed[0].Tasks)
		}
		snapshots := soakSnapshots(t, root)
		if len(snapshots) == 0 {
			t.Fatalf("no snapshot under %s, want the durable bytes of the soak on disk", root)
		}
		t.Logf("resident evidence: pid=%d rounds=%d snapshots=%d elapsed=%dms", child.pid, len(rounds), len(snapshots), closed[0].ElapsedMS)
	})

	t.Run("崩溃恢复：kill -9 后由第二个进程从持久现场读回", func(t *testing.T) {
		root, out := t.TempDir(), t.TempDir()
		crashed := startSoakChild(t, soakModeCrash, root, out, *soakRounds, *soakEvents)
		crashed.waitReady(t)
		ready := soakEventsNamed(crashed.events, "ready")
		if len(ready) != 1 {
			t.Fatalf("ready events = %d, want the one ready marker the crashed process wrote", len(ready))
		}
		if want := *soakRounds * *soakEvents; len(ready[0].TaskIDs) != want {
			t.Fatalf("tasks durable before the crash = %d, want %d; ids = %v", len(ready[0].TaskIDs), want, ready[0].TaskIDs)
		}
		if len(soakSnapshots(t, root)) == 0 {
			t.Fatalf("no snapshot before the kill under %s, want the durable bytes already on disk", root)
		}
		crashedPID := crashed.pid
		if err := crashed.cmd.Process.Kill(); err != nil {
			t.Fatalf("kill -9 the resident process: %v", err)
		}
		crashed.wait(t)
		status, ok := crashed.cmd.ProcessState.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("crashed process state = %+v, want a process ended by SIGKILL", crashed.cmd.ProcessState)
		}
		if code := crashed.cmd.ProcessState.ExitCode(); code != -1 {
			t.Fatalf("crashed process exit = %d, want the signal exit no graceful path produces", code)
		}
		if closed := soakEventsNamed(crashed.events, "closed"); len(closed) != 0 {
			t.Fatalf("crashed process reported %d close events, want none: the process was ended, not closed", len(closed))
		}
		if err := syscall.Kill(crashedPID, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("probe of pid %d = %v, want the killed process gone", crashedPID, err)
		}

		readerOut := t.TempDir()
		reader := startSoakChild(t, soakModeReader, root, readerOut, *soakRounds, *soakEvents)
		reader.wait(t)
		if reader.exitCode != 0 {
			t.Fatalf("reader child exit = %d, want a clean read-back; stderr = %s", reader.exitCode, reader.stderr.String())
		}
		if reader.pid == crashedPID {
			t.Fatalf("reader pid = %d, want a process other than the killed %d", reader.pid, crashedPID)
		}
		recovered := soakEventsNamed(reader.events, "recovery")
		if len(recovered) != 1 {
			t.Fatalf("recovery reports = %d, want the one report of the restarted runtime", len(recovered))
		}
		if got, want := recovered[0].TaskIDs, ready[0].TaskIDs; !sameSoakIDs(got, want) {
			t.Fatalf("recovered tasks = %v, want the %d tasks durable before the crash %v", got, len(want), want)
		}
		restored := soakEventsNamed(reader.events, "read")
		if len(restored) != 1 {
			t.Fatalf("read reports = %d, want the one diagnostic reading of the restarted runtime", len(restored))
		}
		if got, want := restored[0].TaskIDs, ready[0].TaskIDs; !sameSoakIDs(got, want) {
			t.Fatalf("tasks read after the restart = %v, want the crashed process's tasks %v", got, want)
		}

		emptyRoot, emptyOut := t.TempDir(), t.TempDir()
		empty := startSoakChild(t, soakModeReader, emptyRoot, emptyOut, *soakRounds, *soakEvents)
		empty.wait(t)
		if empty.exitCode != 0 {
			t.Fatalf("empty-root reader exit = %d, want a clean read of nothing; stderr = %s", empty.exitCode, empty.stderr.String())
		}
		emptyRecovered := soakEventsNamed(empty.events, "recovery")
		emptyRead := soakEventsNamed(empty.events, "read")
		if len(emptyRecovered) != 1 || len(emptyRecovered[0].TaskIDs) != 0 {
			t.Fatalf("recovery beside an empty root = %+v, want no task recovered from a root nothing wrote", emptyRecovered)
		}
		if len(emptyRead) != 1 || len(emptyRead[0].TaskIDs) != 0 {
			t.Fatalf("tasks read beside an empty root = %+v, want the empty root read as empty", emptyRead)
		}
		t.Logf("crash evidence: killed pid=%d signal=%v, reader pid=%d recovered=%d, empty-root recovered=%d", crashedPID, status.Signal(), reader.pid, len(recovered[0].TaskIDs), len(emptyRecovered[0].TaskIDs))
	})

	t.Run("热更回滚：常驻进程换代后服务新代，回退后再服务旧代内容", func(t *testing.T) {
		root, out := t.TempDir(), t.TempDir()
		child := startSoakChild(t, soakModeHot, root, out, 0, 0)
		child.wait(t)
		if child.exitCode != 0 {
			t.Fatalf("hot child exit = %d, want the switch and return driven on one resident; stderr = %s", child.exitCode, child.stderr.String())
		}
		serving := soakEventsNamed(child.events, "serving")
		if len(serving) != 3 {
			t.Fatalf("serving reports = %d, want one per invocation the resident drove", len(serving))
		}
		wantServing := []struct {
			model string
			count int
		}{{"model-a", 2}, {"model-b", 2}, {"model-a", 4}}
		for i, want := range wantServing {
			if serving[i].Model != want.model || serving[i].Requests != want.count {
				t.Fatalf("serving %d = %s beside %d requests, want %s beside %d", i+1, serving[i].Model, serving[i].Requests, want.model, want.count)
			}
		}
		switched := soakEventsNamed(child.events, "switch")
		if len(switched) != 1 {
			t.Fatalf("switch reports = %d, want the one publication that changed the configuration", len(switched))
		}
		if switched[0].Generation <= 1 {
			t.Fatalf("generation after the switch = %d, want a generation after the startup one", switched[0].Generation)
		}
		returned := soakEventsNamed(child.events, "return")
		if len(returned) != 1 {
			t.Fatalf("return reports = %d, want the one publication that restored the previous content", len(returned))
		}
		if returned[0].Generation <= switched[0].Generation {
			t.Fatalf("generation after the return = %d, want a generation after the switch %d", returned[0].Generation, switched[0].Generation)
		}
		if serving[2].Tasks != 3 {
			t.Fatalf("tasks across the two generations = %d, want the three admitted calls kept; events = %+v", serving[2].Tasks, child.events)
		}
		closed := soakEventsNamed(child.events, "closed")
		if len(closed) != 1 || !closed[0].Drained {
			t.Fatalf("close = %+v, want the resident's converged drain after the return", closed)
		}
		t.Logf("hot evidence: switch generation=%d return generation=%d tasks=%d", switched[0].Generation, returned[0].Generation, serving[2].Tasks)
	})

	t.Run("权限与资源泄漏：存储属主与模式可核，常驻进程的 fd 与协程有界", func(t *testing.T) {
		root, out := t.TempDir(), t.TempDir()
		child := startSoakChild(t, soakModeLeak, root, out, *soakRounds, *soakEvents)
		child.wait(t)
		if child.exitCode != 0 {
			t.Fatalf("leak child exit = %d, want a clean end; stderr = %s", child.exitCode, child.stderr.String())
		}
		samples := soakEventsNamed(child.events, "sample")
		if len(samples) < 3 {
			t.Fatalf("budget samples = %d, want the warm baseline beside every sampled round", len(samples))
		}
		baseline := samples[0]
		for _, sample := range samples[1:] {
			if sample.Goroutines < 0 || sample.OpenFDs < 0 {
				t.Fatalf("sample of round %d = (%d goroutines, %d descriptors), want a real reading", sample.Round, sample.Goroutines, sample.OpenFDs)
			}
			if sample.Goroutines > baseline.Goroutines+soakGoroutineSlack {
				t.Fatalf("round %d holds %d goroutines, want at most %d over the warm baseline %d", sample.Round, sample.Goroutines, soakGoroutineSlack, baseline.Goroutines)
			}
			if sample.OpenFDs > baseline.OpenFDs+soakDescriptorSlack {
				t.Fatalf("round %d holds %d descriptors, want at most %d over the warm baseline %d", sample.Round, sample.OpenFDs, soakDescriptorSlack, baseline.OpenFDs)
			}
		}
		inspectSoakStore(t, root)
		t.Logf("budget evidence: baseline=(%d goroutines, %d fds) last=(%d goroutines, %d fds) samples=%d", baseline.Goroutines, baseline.OpenFDs, samples[len(samples)-1].Goroutines, samples[len(samples)-1].OpenFDs, len(samples))
	})
}

// soakChild dispatches the work order of one soak child, so every evidence leg
// is driven by a process of its own rather than by the test process.
func soakChild(t *testing.T, mode string) {
	t.Helper()
	root := os.Getenv(soakChildRootEnv)
	out := os.Getenv(soakChildOutEnv)
	rounds := soakEnvInt(t, soakChildRoundsEnv)
	events := soakEnvInt(t, soakChildEventsEnv)
	if root == "" || out == "" {
		t.Fatalf("child %s carries no storage root or account directory", mode)
	}
	switch mode {
	case soakModeResident:
		soakResidentChild(t, root, out, rounds, events)
	case soakModeCrash:
		soakCrashChild(t, root, out, rounds, events)
	case soakModeReader:
		soakReaderChild(t, root, out, rounds, events)
	case soakModeHot:
		soakHotChild(t, root, out)
	case soakModeLeak:
		soakLeakChild(t, root, out, rounds, events)
	default:
		t.Fatalf("child mode %q is not one of the soak work orders", mode)
	}
}

// soakResidentChild drives the resident runtime over real durable storage for
// every round of its work order and reports the account of each round.
func soakResidentChild(t *testing.T, root, out string, rounds, events int) {
	t.Helper()
	ledger := newSoakLedger(t, out)
	defer ledger.close()
	ctx, cancel := context.WithTimeout(context.Background(), soakChildStartWindow)
	defer cancel()
	run, provider, tool := assembleSoakRuntime(t, ctx, root)
	started := time.Now()
	sessions := make([]event.Address, 0, rounds)
	for round := 1; round <= rounds; round++ {
		session := soakSession(round)
		sessions = append(sessions, session)
		for n := 1; n <= events; n++ {
			soakDriveInvocation(t, ctx, run, fmt.Sprintf("inv-r%d-e%d", round, n), session)
		}
		ids := soakTaskIDs(t, ctx, run, sessions)
		ledger.report(soakEvent{
			Event:       "round",
			Round:       round,
			Invocations: round * events,
			Tasks:       len(ids),
			Goroutines:  runtime.NumGoroutine(),
			OpenFDs:     openFDs(),
			Requests:    len(provider.recorded()),
			ElapsedMS:   time.Since(started).Milliseconds(),
		})
	}
	report, err := run.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("close the resident: %v", err)
	}
	ledger.report(soakEvent{
		Event:       "closed",
		Round:       rounds,
		Invocations: rounds * events,
		Tasks:       len(soakTaskIDs(t, ctx, run, sessions)),
		Executions:  tool.calls(),
		Drained:     report.Drained,
		Unsettled:   len(report.Unsettled),
		ElapsedMS:   time.Since(started).Milliseconds(),
	})
}

// soakCrashChild drives the work order of a resident and reports its durable
// tasks as a ready marker, then waits to be ended without a close.
func soakCrashChild(t *testing.T, root, out string, rounds, events int) {
	t.Helper()
	ledger := newSoakLedger(t, out)
	ctx, cancel := context.WithTimeout(context.Background(), soakChildStartWindow)
	defer cancel()
	run, _, tool := assembleSoakRuntime(t, ctx, root)
	sessions := make([]event.Address, 0, rounds)
	for round := 1; round <= rounds; round++ {
		session := soakSession(round)
		sessions = append(sessions, session)
		for n := 1; n <= events; n++ {
			soakDriveInvocation(t, ctx, run, fmt.Sprintf("inv-r%d-e%d", round, n), session)
		}
	}
	ids := soakTaskIDs(t, ctx, run, sessions)
	ledger.report(soakEvent{Event: "ready", Round: rounds, Invocations: rounds * events, Tasks: len(ids), TaskIDs: ids, Executions: tool.calls()})
	for {
		time.Sleep(time.Hour)
	}
}

// soakReaderChild assembles a second runtime on the durable storage of a
// crashed process and reports what it recovered beside what it reads back.
func soakReaderChild(t *testing.T, root, out string, rounds, events int) {
	t.Helper()
	ledger := newSoakLedger(t, out)
	defer ledger.close()
	ctx, cancel := context.WithTimeout(context.Background(), soakChildStartWindow)
	defer cancel()
	run, _, _ := assembleSoakRuntime(t, ctx, root)
	recovered := run.Recovery()
	ids := make([]string, 0, len(recovered.Tasks))
	for _, outcome := range recovered.Tasks {
		if !outcome.Recovered || outcome.Refused != nil {
			t.Fatalf("recovery outcome = %+v, want every durable task rebuilt without a refusal", outcome)
		}
		ids = append(ids, string(outcome.TaskID))
	}
	sort.Strings(ids)
	ledger.report(soakEvent{Event: "recovery", Tasks: len(ids), TaskIDs: ids})
	sessions := make([]event.Address, 0, rounds)
	for round := 1; round <= rounds; round++ {
		sessions = append(sessions, soakSession(round))
	}
	read := soakTaskIDs(t, ctx, run, sessions)
	ledger.report(soakEvent{Event: "read", Tasks: len(read), TaskIDs: read, Note: fmt.Sprintf("rounds=%d events=%d", rounds, events)})
}

// soakHotChild switches the configuration of one resident runtime and returns
// it to the previous content, reporting the generation and the model each
// invocation was served by.
func soakHotChild(t *testing.T, root, out string) {
	t.Helper()
	ledger := newSoakLedger(t, out)
	defer ledger.close()
	ctx, cancel := context.WithTimeout(context.Background(), soakChildStartWindow)
	defer cancel()
	providerA := &entryModel{name: "model-a", answer: soakAnswer}
	tool := &entryTool{name: "echo", output: "out-soak"}
	declA := soakDeclaration("model-a")
	agentsA := soakAgents(providerA, tool)
	run, err := tagent.Assemble(ctx, declA, agentsA, tagent.WithGovernance(passPolicy{}), soakStorage(root))
	if err != nil {
		t.Fatalf("assemble the resident: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-hot"}
	servingTasks := func() int { return len(soakTaskIDs(t, ctx, run, []event.Address{addr})) }
	soakDriveInvocation(t, ctx, run, "inv-hot-first", addr)
	ledger.report(soakEvent{Event: "serving", Model: "model-a", Requests: len(providerA.recorded()), Tasks: servingTasks(), Note: "first invocation"})

	providerB := &entryModel{name: "model-b", answer: soakAnswer}
	switched, err := run.Publish(ctx, soakDeclaration("model-b"), soakAgents(providerB, tool))
	if err != nil {
		t.Fatalf("publish the changed configuration: %v", err)
	}
	if switched.Result != state.PublishApplied {
		t.Fatalf("switch result = %v, want the changed configuration applied", switched.Result)
	}
	ledger.report(soakEvent{Event: "switch", Generation: uint64(switched.Publication.Generation)})
	soakDriveInvocation(t, ctx, run, "inv-hot-switched", addr)
	ledger.report(soakEvent{Event: "serving", Model: "model-b", Requests: len(providerB.recorded()), Tasks: servingTasks(), Note: "after the switch"})

	returned, err := run.Publish(ctx, declA, agentsA)
	if err != nil {
		t.Fatalf("publish the previous configuration: %v", err)
	}
	if returned.Result != state.PublishApplied {
		t.Fatalf("return result = %v, want the previous content published as a fresh generation", returned.Result)
	}
	ledger.report(soakEvent{Event: "return", Generation: uint64(returned.Publication.Generation)})
	soakDriveInvocation(t, ctx, run, "inv-hot-returned", addr)
	ledger.report(soakEvent{
		Event:    "serving",
		Model:    "model-a",
		Requests: len(providerA.recorded()),
		Tasks:    servingTasks(),
		Note:     fmt.Sprintf("after the return; model-b served %d requests", len(providerB.recorded())),
	})
	report, err := run.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("close the resident: %v", err)
	}
	ledger.report(soakEvent{Event: "closed", Drained: report.Drained, Unsettled: len(report.Unsettled), Tasks: len(soakTaskIDs(t, ctx, run, []event.Address{addr}))})
}

// soakLeakChild samples the descriptor and goroutine budget of one resident
// runtime behind a warm baseline and after every sampled round.
func soakLeakChild(t *testing.T, root, out string, rounds, events int) {
	t.Helper()
	ledger := newSoakLedger(t, out)
	defer ledger.close()
	ctx, cancel := context.WithTimeout(context.Background(), soakChildStartWindow)
	defer cancel()
	run, _, _ := assembleSoakRuntime(t, ctx, root)
	sessions := make([]event.Address, 0, rounds+2)
	for round := 1; round <= 2; round++ {
		session := soakSession(-round)
		sessions = append(sessions, session)
		for n := 1; n <= events; n++ {
			soakDriveInvocation(t, ctx, run, fmt.Sprintf("inv-warm%d-e%d", round, n), session)
		}
	}
	time.Sleep(100 * time.Millisecond)
	ledger.report(soakEvent{Event: "sample", Round: 0, Tasks: len(soakTaskIDs(t, ctx, run, sessions)), Goroutines: runtime.NumGoroutine(), OpenFDs: openFDs()})
	for round := 1; round <= rounds; round++ {
		session := soakSession(round)
		sessions = append(sessions, session)
		for n := 1; n <= events; n++ {
			soakDriveInvocation(t, ctx, run, fmt.Sprintf("inv-s%d-e%d", round, n), session)
		}
		ledger.report(soakEvent{Event: "sample", Round: round, Tasks: len(soakTaskIDs(t, ctx, run, sessions)), Goroutines: runtime.NumGoroutine(), OpenFDs: openFDs()})
	}
	report, err := run.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("close the resident: %v", err)
	}
	ledger.report(soakEvent{Event: "closed", Drained: report.Drained, Unsettled: len(report.Unsettled), Tasks: len(soakTaskIDs(t, ctx, run, sessions))})
}

// soakAnswer is the scripted model answer of the soak: one tool call for the
// invocation's own input beside the settled answer of the next decision.
func soakAnswer(req *model.Request) *model.Response {
	if last := req.Messages[len(req.Messages)-1]; last.Content == "hello" {
		return entryToolTurn(entryCall("call-1", "echo", `{"x":1}`))
	}
	return entryFinalTurn("settled")
}

// assembleSoakRuntime assembles one runtime over real durable storage at the
// root the work order names.
func assembleSoakRuntime(t *testing.T, ctx context.Context, root string) (*tagent.Run, *entryModel, *entryTool) {
	t.Helper()
	provider := &entryModel{name: "model-a", answer: soakAnswer}
	tool := &entryTool{name: "echo", output: "out-soak"}
	run, err := tagent.Assemble(ctx, soakDeclaration("model-a"), soakAgents(provider, tool), tagent.WithGovernance(passPolicy{}), soakStorage(root))
	if err != nil {
		t.Fatalf("assemble the runtime over %s: %v", root, err)
	}
	return run, provider, tool
}

// soakStorage names the durable tier one soak runtime persists under.
func soakStorage(root string) tagent.Option {
	return tagent.WithStorage(memory.StorageConfig{Type: memory.StorageLocalFile, Root: root})
}

// soakDeclaration is the one-agent declaration the soak runtime is assembled
// over, naming the model of the generation it publishes.
func soakDeclaration(name string) *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: name}},
	}
}

// soakAgents is the native agent set one soak declaration resolves against.
func soakAgents(provider *entryModel, tool *entryTool) map[event.AgentID]tagent.NativeAgent {
	return map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}
}

// soakSession names the session of one soak round.
func soakSession(round int) event.Address {
	return event.Address{AgentID: "agent-a", SessionID: event.SessionID(fmt.Sprintf("session-soak-%d", round))}
}

// soakDriveInvocation drives one invocation through the formal runtime entry and
// holds the turn to the completed shape the soak counts on.
func soakDriveInvocation(t *testing.T, ctx context.Context, run *tagent.Run, invocation string, addr event.Address) {
	t.Helper()
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke %s: %v", invocation, err)
	}
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("turn %s = (%v, %d decisions), want one completed turn of two decisions", invocation, turn.Terminal, len(turn.Decisions))
	}
	if string(turn.InvocationID) != invocation {
		t.Fatalf("turn invocation = %q, want the invocation the drive was admitted for %q", turn.InvocationID, invocation)
	}
}

// soakTaskIDs reads the task dimension of every session the caller names and
// reports the task identities in one order.
func soakTaskIDs(t *testing.T, ctx context.Context, run *tagent.Run, sessions []event.Address) []string {
	t.Helper()
	if len(sessions) == 0 {
		return nil
	}
	scope := query.AuthorizationScope{Sessions: sessions}
	snapshot, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
	if err != nil {
		t.Fatalf("query the task dimension of %d sessions: %v", len(sessions), err)
	}
	ids := make([]string, 0, len(snapshot.Details))
	for _, detail := range snapshot.Details {
		if detail.Ref.Kind != query.ObjectTask {
			t.Fatalf("filtered detail = %+v, want only the requested kind served", detail.Ref)
		}
		ids = append(ids, string(detail.Ref.TaskID))
	}
	sort.Strings(ids)
	return ids
}

// openFDs reports the descriptors this process holds open.
func openFDs() int {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

// soakEnvInt reads one integer work-order field of a soak child.
func soakEnvInt(t *testing.T, name string) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return 0
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		t.Fatalf("work order %s = %q, want an integer: %v", name, raw, err)
	}
	return value
}

// soakLedger is the account file of one soak child: each line is synced on
// write, so a process ended without a close leaves the lines it reported.
type soakLedger struct {
	mu   sync.Mutex
	file *os.File
}

// newSoakLedger opens the account file under the directory the work order
// names.
func newSoakLedger(t *testing.T, out string) *soakLedger {
	t.Helper()
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatalf("prepare the account directory %s: %v", out, err)
	}
	file, err := os.OpenFile(filepath.Join(out, soakLedgerName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open the account file: %v", err)
	}
	return &soakLedger{file: file}
}

// report appends one account line and syncs it before the caller runs on.
func (l *soakLedger) report(ev soakEvent) {
	ev.PID = os.Getpid()
	data, err := json.Marshal(ev)
	if err != nil {
		panic(fmt.Sprintf("marshal the account line: %v", err))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.file.Write(append(data, '\n')); err != nil {
		panic(fmt.Sprintf("append the account line: %v", err))
	}
	if err := l.file.Sync(); err != nil {
		panic(fmt.Sprintf("sync the account line: %v", err))
	}
}

// close releases the account file.
func (l *soakLedger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

// soakChildProcess is one soak child started by the test process.
type soakChildProcess struct {
	cmd      *exec.Cmd
	ledger   string
	pid      int
	events   []soakEvent
	exitCode int
	stderr   bytes.Buffer
}

// startSoakChild starts one soak child as its own process over the storage root
// and account directory the caller names.
func startSoakChild(t *testing.T, mode, root, out string, rounds, events int) *soakChildProcess {
	t.Helper()
	child := &soakChildProcess{ledger: filepath.Join(out, soakLedgerName)}
	child.cmd = exec.Command(os.Args[0], "-test.run=^TestSoakContinuity$", "-test.timeout=10m")
	child.cmd.Env = append(os.Environ(),
		soakChildModeEnv+"="+mode,
		soakChildRootEnv+"="+root,
		soakChildOutEnv+"="+out,
		fmt.Sprintf("%s=%d", soakChildRoundsEnv, rounds),
		fmt.Sprintf("%s=%d", soakChildEventsEnv, events),
	)
	var stdout bytes.Buffer
	child.cmd.Stdout = &stdout
	child.cmd.Stderr = &child.stderr
	if err := child.cmd.Start(); err != nil {
		t.Fatalf("start the %s child: %v", mode, err)
	}
	child.pid = child.cmd.Process.Pid
	return child
}

// waitReady waits for the ready marker of a child that keeps running afterwards.
func (c *soakChildProcess) waitReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(soakChildReadyWait)
	for time.Now().Before(deadline) {
		events, err := readSoakEvents(c.ledger)
		if err == nil {
			c.events = events
			for _, ev := range events {
				if ev.Event == "ready" {
					return
				}
			}
		}
		if c.cmd.ProcessState != nil {
			t.Fatalf("child ended before its ready marker; stderr = %s", c.stderr.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("child %d reported no ready marker within %s; stderr = %s", c.pid, soakChildReadyWait, c.stderr.String())
}

// wait waits for the end of the child and reads every account line it left.
func (c *soakChildProcess) wait(t *testing.T) {
	t.Helper()
	err := c.cmd.Wait()
	if c.cmd.ProcessState != nil {
		c.exitCode = c.cmd.ProcessState.ExitCode()
	}
	if err != nil && c.exitCode == 0 {
		t.Fatalf("wait for child %d: %v; stderr = %s", c.pid, err, c.stderr.String())
	}
	events, readErr := readSoakEvents(c.ledger)
	if readErr != nil {
		t.Fatalf("read the account of child %d: %v", c.pid, readErr)
	}
	c.events = events
	if strings.Contains(c.stderr.String(), "DATA RACE") {
		t.Fatalf("child %d reported a data race: %s", c.pid, c.stderr.String())
	}
}

// readSoakEvents reads the account lines a soak child has written so far,
// dropping a line still in flight.
func readSoakEvents(path string) ([]soakEvent, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	events := make([]soakEvent, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev soakEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, nil
}

// soakEventsNamed reports the account lines of one named event.
func soakEventsNamed(events []soakEvent, name string) []soakEvent {
	out := make([]soakEvent, 0, len(events))
	for _, ev := range events {
		if ev.Event == name {
			out = append(out, ev)
		}
	}
	return out
}

// sameSoakIDs reports whether two task identity sets are the same set.
func sameSoakIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

// soakSnapshots reports the durable snapshot files under one storage root.
func soakSnapshots(t *testing.T, root string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "kv", "*", "snapshot.json"))
	if err != nil {
		t.Fatalf("glob the snapshots under %s: %v", root, err)
	}
	return matches
}

// inspectSoakStore holds the store tree of one soak to its ownership and mode
// contract and refuses temp residue left behind.
func inspectSoakStore(t *testing.T, root string) {
	t.Helper()
	uid := os.Getuid()
	entries := 0
	modes := map[string]int{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		entries++
		mode := info.Mode()
		if mode.IsDir() {
			modes["dir"] = int(mode.Perm())
		} else {
			modes["file"] = int(mode.Perm())
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != uid {
			t.Errorf("store entry %s is owned by uid %d, want the invoking user %d", path, stat.Uid, uid)
		}
		if mode&0o002 != 0 {
			t.Errorf("store entry %s carries mode %v, want no world-writable entry", path, mode.Perm())
		}
		if mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			t.Errorf("store entry %s carries mode %v, want no set-id or sticky bit", path, mode)
		}
		if !mode.IsDir() && strings.HasSuffix(path, ".tmp") {
			t.Errorf("store entry %s is a temp file the resident left behind", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect the store tree %s: %v", root, err)
	}
	if len(soakSnapshots(t, root)) == 0 {
		t.Fatalf("store tree %s holds no snapshot, want the durable bytes of the soak", root)
	}
	if entries < 3 {
		t.Fatalf("store tree %s holds %d entries, want the root, its shard directory and a snapshot", root, entries)
	}
	t.Logf("store evidence: entries=%d dir_mode=%04o file_mode=%04o", entries, modes["dir"], modes["file"])
}
