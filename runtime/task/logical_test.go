package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// logicalRig is one registry read under the given threshold set, beside the commit carrier its facts are stored in.
type logicalRig struct {
	registry *MemoryRegistry
	carrier  *MemoryCommit
}

// logicalRegistry returns one empty registry read under the given threshold set.
func logicalRegistry(policy Policy) logicalRig {
	carrier := NewMemoryCommit(nil)
	return logicalRig{registry: NewMemoryRegistry(carrier, NewMemoryAdmission(), WithPolicy(policy)), carrier: carrier}
}

// logicalThresholds is the threshold set the logical status checks read one task under: every threshold is a minute
// apart, so one probe instant names one threshold and no two of them are reachable at once.
var logicalThresholds = Policy{
	WatchAfter:       time.Minute,
	StableAfter:      2 * time.Minute,
	SuspectAfter:     3 * time.Minute,
	ExecutionTimeout: 4 * time.Minute,
	TTL:              time.Hour,
}

// logicalMargin is the offset a probe instant is placed past a threshold, so the silence it reads is on the far side
// of the threshold whichever way the registry's own stamps fell.
const logicalMargin = 10 * time.Second

// TestLogicalStatusRecognition pins the classification of one task's silence beside the status machine.
// - a task whose state keeps moving is not classified at all, whichever thresholds its policy declares
// - silence crosses the watch, the stable and the suspect thresholds as it grows, and a task past its time to live expires
// - an execution past the execution timeout is marked timed out while its status and its owed result stay as they stand
// - a task holding a terminal fact carries no logical status, and a record carrying no movement stamp is read as moving
// - reading the logical status of a task moves no status of it and stores no fact
// - a suspected or timed out task is marked only: its status, facts and obligations stay as they stand, and no termination is requested
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestLogicalStatusRecognition(t *testing.T) {
	ctx := context.Background()

	t.Run("a task that keeps moving carries no logical status", func(t *testing.T) {
		registry := logicalRegistry(logicalThresholds).registry
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if got, err := registry.Logical(ctx, taskID, time.Now()); err != nil || got != LogicalActive {
			t.Fatalf("logical status of a task admitted just now = (%q, %v), want an unclassified task", got, err)
		}
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		if got, err := registry.Logical(ctx, taskID, time.Now()); err != nil || got != LogicalActive {
			t.Fatalf("logical status of a task that just moved = (%q, %v), want an unclassified task", got, err)
		}
	})

	t.Run("silence crosses the watch, the stable and the suspect thresholds in order", func(t *testing.T) {
		registry := logicalRegistry(logicalThresholds).registry
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		registered := time.Now()
		cases := []struct {
			probe time.Duration
			want  LogicalStatus
		}{
			{time.Minute + logicalMargin, LogicalWatch},
			{2*time.Minute + logicalMargin, LogicalStable},
			{3*time.Minute + logicalMargin, LogicalSuspect},
		}
		for _, one := range cases {
			got, err := registry.Logical(ctx, taskID, registered.Add(one.probe))
			if err != nil || got != one.want {
				t.Fatalf("logical status after %s of silence = (%q, %v), want %q", one.probe, got, err, one.want)
			}
		}
	})

	t.Run("an execution past the timeout is marked timed out beside its result still owed", func(t *testing.T) {
		rig := logicalRegistry(logicalThresholds)
		registry, carrier := rig.registry, rig.carrier
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		running := time.Now()
		if got, err := registry.Logical(ctx, taskID, running.Add(4*time.Minute+logicalMargin)); err != nil || got != LogicalTimedOut {
			t.Fatalf("logical status past the execution timeout = (%q, %v), want %q", got, err, LogicalTimedOut)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusRunning {
			t.Fatalf("status beside a timed out execution = (%v, %v), want the running status kept", status, err)
		}
		settled := []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled-after-timeout", settled); err != nil || result != CommitCommitted {
			t.Fatalf("settle the timed out task = (%v, %v), want its result stored as the notification it owes", result, err)
		}
		if stored := len(carrier.Facts(taskID)); stored != 3 {
			t.Fatalf("stored facts of the timed out task = %d, want the admission, the move under way and the settlement", stored)
		}
		if held, err := manager.Obligations(ctx, taskID); err != nil || held.Count != 1 || held.Open[0].Notice != settled[0].Notice {
			t.Fatalf("obligations beside the timed out settlement = (%+v, %v), want the notification still owed", held, err)
		}
	})

	t.Run("a task unsettled past its time to live expires", func(t *testing.T) {
		registry := logicalRegistry(Policy{TTL: time.Minute}).registry
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		admitted := time.Now()
		if got, err := registry.Logical(ctx, taskID, admitted.Add(time.Minute+logicalMargin)); err != nil || got != LogicalExpired {
			t.Fatalf("logical status past the time to live = (%q, %v), want %q", got, err, LogicalExpired)
		}
	})

	t.Run("a terminal task carries no logical status", func(t *testing.T) {
		registry := logicalRegistry(logicalThresholds).registry
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		settled := []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", settled); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
		}
		if got, err := registry.Logical(ctx, taskID, time.Now().Add(24*time.Hour)); err != nil || got != LogicalActive {
			t.Fatalf("logical status of a settled task = (%q, %v), want an unclassified task", got, err)
		}
	})

	t.Run("a record carrying no movement stamp is read as moving", func(t *testing.T) {
		if got := logicalThresholds.classify(StatusRunning, stamps{}, time.Now().Add(24*time.Hour)); got != LogicalActive {
			t.Fatalf("logical status of an unstamped record = %q, want an unclassified task", got)
		}
	})

	t.Run("reading the logical status moves no status and stores no fact", func(t *testing.T) {
		rig := logicalRegistry(logicalThresholds)
		registry, carrier := rig.registry, rig.carrier
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		stored := len(carrier.Facts(taskID))
		held := invariantObligationCount(t, manager, taskID)
		probe := time.Now().Add(3*time.Minute + logicalMargin)
		if got, err := registry.Logical(ctx, taskID, probe); err != nil || got != LogicalSuspect {
			t.Fatalf("logical status the identification read = (%q, %v), want %q", got, err, LogicalSuspect)
		}
		if changes := registry.Watch(ctx, probe); len(changes) != 1 || changes[0].To != LogicalSuspect {
			t.Fatalf("changes the identification reported = %+v, want the one suspected task", changes)
		}
		if got := len(carrier.Facts(taskID)); got != stored {
			t.Fatalf("stored facts after the identification = %d, want the admission alone", got)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusAccepted {
			t.Fatalf("status after the identification = (%v, %v), want the accepted status kept", status, err)
		}
		if got := invariantObligationCount(t, manager, taskID); got != held {
			t.Fatalf("obligations after the identification = %d, want the open obligation kept", got)
		}
	})

	t.Run("a suspected or timed out task is marked but never terminated", func(t *testing.T) {
		rig := logicalRegistry(logicalThresholds)
		registry, carrier := rig.registry, rig.carrier
		manager := NewMemoryManager(registry, nil)
		silent := invariantRegister(t, registry, "call-1", "dedup-1")
		running := invariantRegister(t, registry, "call-2", "dedup-2")
		if result, err := registry.Advance(ctx, running, StatusRunning, "running", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the second task under way = (%v, %v), want a committed change", result, err)
		}
		probe := time.Now().Add(4*time.Minute + logicalMargin)
		if got, err := registry.Logical(ctx, silent, probe); err != nil || got != LogicalSuspect {
			t.Fatalf("logical status of the silent task = (%q, %v), want %q", got, err, LogicalSuspect)
		}
		if got, err := registry.Logical(ctx, running, probe); err != nil || got != LogicalTimedOut {
			t.Fatalf("logical status of the running task = (%q, %v), want %q", got, err, LogicalTimedOut)
		}
		if changes := registry.Watch(ctx, probe); len(changes) != 2 {
			t.Fatalf("changes the identification reported = %+v, want the suspected and the timed out task", changes)
		}
		facts := map[event.TaskID]int{silent: len(carrier.Facts(silent)), running: len(carrier.Facts(running))}
		held := map[event.TaskID]int{
			silent:  invariantObligationCount(t, manager, silent),
			running: invariantObligationCount(t, manager, running),
		}
		if _, status, err := registry.Lookup(ctx, silent); err != nil || status != StatusAccepted {
			t.Fatalf("status of the suspected task = (%v, %v), want the accepted status kept", status, err)
		}
		if _, status, err := registry.Lookup(ctx, running); err != nil || status != StatusRunning {
			t.Fatalf("status of the timed out task = (%v, %v), want the running status kept", status, err)
		}
		for _, taskID := range []event.TaskID{silent, running} {
			if got := len(carrier.Facts(taskID)); got != facts[taskID] {
				t.Fatalf("stored facts of %s after the identification = %d, want %d", taskID, got, facts[taskID])
			}
			if got := invariantObligationCount(t, manager, taskID); got != held[taskID] {
				t.Fatalf("obligations of %s after the identification = %d, want %d", taskID, got, held[taskID])
			}
			stop := StopEvidence{TaskID: taskID, Stopped: true, Reason: "termination probe"}
			if err := manager.RecordStop(ctx, stop); !errors.Is(err, ErrCancelNotRequested) {
				t.Fatalf("stop evidence of %s beside no cancellation request = %v, want %v", taskID, err, ErrCancelNotRequested)
			}
		}
		if _, status, err := registry.Lookup(ctx, silent); err != nil || status != StatusAccepted {
			t.Fatalf("status of the suspected task after the stop probe = (%v, %v), want the accepted status kept", status, err)
		}
		if _, status, err := registry.Lookup(ctx, running); err != nil || status != StatusRunning {
			t.Fatalf("status of the timed out task after the stop probe = (%v, %v), want the running status kept", status, err)
		}
	})
}

// TestWatchReportsEachLogicalChangeOnce pins the identification of logical status changes over a registry.
// - a task read under a logical status it already reported is not reported again
// - a task whose logical status moves is reported once more, carrying the statuses either side of the change
// - a task holding a terminal fact is never reported, and the report is ordered by task identity
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestWatchReportsEachLogicalChangeOnce(t *testing.T) {
	ctx := context.Background()
	registry := logicalRegistry(logicalThresholds).registry
	first := invariantRegister(t, registry, "call-1", "dedup-1")
	second := invariantRegister(t, registry, "call-2", "dedup-2")
	settled := []Envelope{invariantEnvelope(second, NoticeSettled, 0)}
	if result, err := registry.Advance(ctx, second, StatusSettled, "settled", settled); err != nil || result != CommitCommitted {
		t.Fatalf("settle the second task = (%v, %v), want a committed change", result, err)
	}
	admitted := time.Now()

	if changes := registry.Watch(ctx, admitted); len(changes) != 0 {
		t.Fatalf("changes of two tasks that just moved = %+v, want none", changes)
	}
	observed := registry.Watch(ctx, admitted.Add(time.Minute+logicalMargin))
	if len(observed) != 1 || observed[0].TaskID != first || observed[0].From != LogicalActive || observed[0].To != LogicalWatch {
		t.Fatalf("changes after a minute of silence = %+v, want the watch of the unterminated task alone", observed)
	}
	if changes := registry.Watch(ctx, admitted.Add(time.Minute+logicalMargin)); len(changes) != 0 {
		t.Fatalf("changes of a task reported under its logical status = %+v, want none", changes)
	}
	moved := registry.Watch(ctx, admitted.Add(2*time.Minute+logicalMargin))
	if len(moved) != 1 || moved[0].TaskID != first || moved[0].Status != StatusAccepted ||
		moved[0].From != LogicalWatch || moved[0].To != LogicalStable {
		t.Fatalf("changes after the status moved = %+v, want the stable state of the unterminated task", moved)
	}
}

// TestPolicyNamed pins the resolution of a declared task policy name onto its threshold set.
// - the empty name and the default name both select the default policy
// - every declared name selects a threshold set whose silences are read in the declared order
// - a name outside the table is reported as unknown rather than read as the default
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestPolicyNamed(t *testing.T) {
	for _, name := range []string{"", PolicyDefault} {
		got, ok := PolicyNamed(name)
		if !ok || got != DefaultPolicy {
			t.Fatalf("policy of the name %q = (%+v, %t), want the default threshold set", name, got, ok)
		}
	}
	responsive, ok := PolicyNamed(PolicyResponsive)
	if !ok {
		t.Fatalf("policy of the name %q is reported unknown", PolicyResponsive)
	}
	patient, ok := PolicyNamed(PolicyPatient)
	if !ok {
		t.Fatalf("policy of the name %q is reported unknown", PolicyPatient)
	}
	if patient.WatchAfter <= responsive.WatchAfter || patient.ExecutionTimeout <= responsive.ExecutionTimeout {
		t.Fatalf("patient policy = %+v beside responsive %+v, want the longer silences waited for", patient, responsive)
	}
	if _, ok := PolicyNamed("no-such-policy"); ok {
		t.Fatal("a policy name outside the table is reported known")
	}
}

// TestObligationsReportTheLogicalStatus pins the report face the silence of a task is read through.
// - the obligation report carries the logical status the task is read under at the instant of the report
// - a task that just moved is reported as unclassified
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestObligationsReportTheLogicalStatus(t *testing.T) {
	ctx := context.Background()
	registry := logicalRegistry(Policy{WatchAfter: time.Nanosecond}).registry
	manager := NewMemoryManager(registry, nil)
	taskID := invariantRegister(t, registry, "call-1", "dedup-1")
	if held, err := manager.Obligations(ctx, taskID); err != nil || held.Logical != LogicalWatch {
		t.Fatalf("obligations of a silent task = (%+v, %v), want the watched silence reported", held, err)
	}
	moving := logicalRegistry(DefaultPolicy).registry
	movingManager := NewMemoryManager(moving, nil)
	movingID := invariantRegister(t, moving, "call-2", "dedup-2")
	if held, err := movingManager.Obligations(ctx, movingID); err != nil || held.Logical != LogicalActive {
		t.Fatalf("obligations of a task that just moved = (%+v, %v), want an unclassified task", held, err)
	}
	if _, err := moving.Logical(ctx, event.TaskID("task-unknown"), time.Now()); err == nil {
		t.Fatal("the logical status of an unknown task is reported without a refusal")
	}
}
