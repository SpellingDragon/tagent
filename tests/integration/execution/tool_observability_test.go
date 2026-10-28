package execution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
)

// captureLogf records every Debugf line the faces under test render, so the
// observability contract is asserted on the lines an operator reads.
type captureLogf struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureLogf) add(level, format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, level+" "+fmt.Sprintf(format, args...))
}

func (c *captureLogf) Debug(args ...any) {}
func (c *captureLogf) Debugf(format string, args ...any) {
	c.add("DEBUG", format, args...)
}
func (c *captureLogf) Info(args ...any)      {}
func (c *captureLogf) Infof(string, ...any)  {}
func (c *captureLogf) Warn(args ...any)      {}
func (c *captureLogf) Warnf(string, ...any)  {}
func (c *captureLogf) Error(args ...any)     {}
func (c *captureLogf) Errorf(string, ...any) {}
func (c *captureLogf) Fatal(args ...any)     {}
func (c *captureLogf) Fatalf(string, ...any) {}

func (c *captureLogf) join() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.lines, "\n")
}

// installCaptureLog points the process logger at the capture and restores the
// original on cleanup, the same substitution the upstream log package's own
// tests use.
func installCaptureLog(t *testing.T) *captureLogf {
	t.Helper()
	capture := &captureLogf{}
	original := trpclog.Default
	trpclog.Default = capture
	t.Cleanup(func() { trpclog.Default = original })
	return capture
}

// TestToolRoundLeavesADebugTrail pins the tool face's debug trail: one line per call outcome and per After pass, beside the drive's own boundary lines.
//   - the admitted call names the invocation, call, tool and task identity; a refused call names the refusing face
//   - the After pass names the fact it dispatched and the gaps it recorded
//   - the trail carries identities and outcomes only — never arguments, prompts or model output
//
// 契约: docs/OPERATION.md#event-gates
func TestToolRoundLeavesADebugTrail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	capture := installCaptureLog(t)

	adapter := &minimalAdapter{name: "probe", output: "adapter-out"}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-observability"}
	run, err := assembly.Assemble(ctx, adapterDeclaration(), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: adapterProvider(), Tools: []trpctool.CallableTool{adapter}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-observability",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "use the tool"}},
	}); err != nil {
		t.Fatalf("invoke the tool round: %v", err)
	}

	trail := capture.join()
	for _, want := range []string{
		"tagent: consumer drive start agent=agent-a session=session-observability invocation=inv-observability",
		"tagent: consumer tool admitted invocation=inv-observability call=call-1 tool=probe task=task-",
		"tagent: consumer after-pass agent=agent-a fact=",
		"tagent: consumer drive end invocation=inv-observability decisions=2 terminal=completed",
	} {
		if !strings.Contains(trail, want) {
			t.Fatalf("the debug trail lacks %q;\ntrail:\n%s", want, trail)
		}
	}
	if strings.Contains(trail, "adapter-out") {
		t.Fatalf("the debug trail carries the tool output, want identities and outcomes only")
	}
}
