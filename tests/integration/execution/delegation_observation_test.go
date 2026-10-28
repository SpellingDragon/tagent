package execution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegationObservationThroughTheSubagentTool checks the lines one real call_subagent call writes.
//   - the admission of the call writes one line carrying the task, the agent and the call
//   - the landed settle writes one line carrying the terminal and the elapsed time of the call
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegationObservationThroughTheSubagentTool(t *testing.T) {
	t.Run("the admission and the settle of one call write their lines", func(t *testing.T) {
		observer := &delegationLinesObserver{}
		defer watchDelegationLines(t, observer)()
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer", gate: make(chan struct{}), started: make(chan struct{})}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		rig.start(t, &convModel{name: "model-caller", answer: func(*model.Request) *model.Response {
			return convFinalTurn("the caller decides alone")
		}}, nil, issuer)

		answered := make(chan error, 1)
		go func() {
			out, err := tool.Call(task.WithCallFrame(context.Background(), subagentCallFrame(issuer, "call-review")), reviewCallArgs())
			if err == nil {
				if receipt, ok := out.(task.AdmissionReceipt); !ok || !receipt.Accepted {
					err = fmt.Errorf("call result = %v, want one accepted admission", out)
				}
			}
			answered <- err
		}()
		if err := awaitAnswered(t, answered); err != nil {
			t.Fatalf("call the tool = %v, want the admission receipt", err)
		}
		taskID := rig.tasks.admitted()
		if taskID == "" {
			t.Fatalf("the call was admitted as no task beside its receipt")
		}
		if !observer.mentions("debug", "delegation admitted result=fresh", "task="+string(taskID),
			"agent=agent-caller", "call=call-review") {
			t.Fatalf("debug lines = %v, want the admission beside its task, agent and call", observer.all())
		}
		select {
		case <-delegated.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the delegated turn never started beside the receipt")
		}
		close(delegated.gate)
		awaitEnvelope(t, rig.arrived)
		awaitSettledLine(t, observer, "info", "delegation settled", "task="+string(taskID), "agent=agent-caller",
			"status=settled", "elapsed=")
	})
}

// awaitSettledLine waits for one line of the delegation face to be written: the
// delegated turn settles on a goroutine of its own, so a check reads the line
// instead of assuming the delivery of the notification already wrote it.
func awaitSettledLine(t *testing.T, observer *delegationLinesObserver, level string, fragments ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if observer.mentions(level, fragments...) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s lines = %v, want one line carrying %v", level, observer.all(), fragments)
		}
		time.Sleep(time.Millisecond)
	}
}

// reviewCallArgs returns the arguments of one call_subagent call over the
// scripted reviewer.
func reviewCallArgs() []byte {
	return []byte(`{"name":"reviewer","description":"reviews one change","prompt":"review the change",` +
		`"model":"model-reviewer","input":"the change under review"}`)
}

// awaitAnswered waits for one tool call to answer.
func awaitAnswered(t *testing.T, answered <-chan error) error {
	t.Helper()
	select {
	case err := <-answered:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not answer")
		return nil
	}
}

// delegationLinesObserver is the framework logger the observation checks of this
// package read the delegation face through: it records every line beside its
// level.
type delegationLinesObserver struct {
	mu    sync.Mutex
	lines []string
}

// record takes one line at the named level.
func (o *delegationLinesObserver) record(level, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lines = append(o.lines, level+": "+text)
}

// Debug records one debug line.
func (o *delegationLinesObserver) Debug(args ...any) { o.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (o *delegationLinesObserver) Debugf(format string, args ...any) {
	o.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (o *delegationLinesObserver) Info(args ...any) { o.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (o *delegationLinesObserver) Infof(format string, args ...any) {
	o.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (o *delegationLinesObserver) Warn(args ...any) { o.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (o *delegationLinesObserver) Warnf(format string, args ...any) {
	o.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (o *delegationLinesObserver) Error(args ...any) { o.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (o *delegationLinesObserver) Errorf(format string, args ...any) {
	o.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (o *delegationLinesObserver) Fatal(args ...any) { o.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (o *delegationLinesObserver) Fatalf(format string, args ...any) {
	o.record("fatal", fmt.Sprintf(format, args...))
}

// all reports every line taken, whatever the level.
func (o *delegationLinesObserver) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// mentions reports whether one line taken at the level carries every fragment.
func (o *delegationLinesObserver) mentions(level string, fragments ...string) bool {
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

// watchDelegationLines points the framework logger at the observer for the
// duration of one check and answers the restore that ends it, so a check reads the
// lines of its own face and leaves the package logger as it found it.
func watchDelegationLines(t *testing.T, observer trpclog.Logger) func() {
	t.Helper()
	original := trpclog.Default
	trpclog.Default = observer
	restore := func() { trpclog.Default = original }
	t.Cleanup(restore)
	return restore
}
