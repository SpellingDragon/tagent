package agent

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestI1HostDirectFormEquivalence is the OTHER half of the 7.2② 主子同构 gate:
// design line 210/200 requires "同场景在 B 直接接宿主时同样成立." This drives the
// SAME越窗 scenario through the ENTRY-OWNER path — a resident StartLoop that injects
// a message, whose turn spawns a background task (via the owner's own taskManager,
// with NO delegation invocation_id), the task settles late → OnSettle falls back to
// the persistentBus (route finds no sink) → runEventLoop Pulls the task_settled →
// processTurn runs a continuation turn → output lands on the loop's outputCh.
//
// That mirrors d11's callee form (sink+tail) through the host form (bus+loop): both
// consume their OWN late task_settled and continue to their receiver via the SAME
// processTurn primitive. This is what makes the two forms equivalent WITHOUT relying
// on 3.4's cross-publish/generation matrix (a separate concern).
func TestI1HostDirectFormEquivalence(t *testing.T) {
	spawned := make(chan *task.ManualDetector, 4)
	probe := &e2eProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: &echoModel{}, SystemPrompt: "You are a host agent.",
		Name: "host-form-eq", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	outCh, err := b.StartLoop("host-user", "host-sess-eq")
	require.NoError(t, err)

	var mu sync.Mutex
	var texts []string
	stop := make(chan struct{})
	var collectWg sync.WaitGroup
	collectWg.Add(1)
	go func() {
		defer collectWg.Done()
		for {
			select {
			case <-stop:
				return
			case evt, ok := <-outCh:
				if !ok {
					return
				}
				if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
					continue
				}
				m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
				if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
					mu.Lock()
					texts = append(texts, m.Content)
					mu.Unlock()
				}
			}
		}
	}()

	// Drive the resident owner loop: its input enters through the SAME persistent
	// pipeline the host form consumes (InjectMessage → persistentBus → runEventLoop →
	// processTurn). The round-59 failure was that the test started the loop but never
	// injected anything — so the loop had no event to Pull and the owner turn never
	// reached the spawner. Injecting one user message makes the fresh owner turn emit
	// a tool call (echoModel: ≤1 non-system input) that spawns a background task.
	b.InjectMessage(model.NewUserMessage("host go"))

	// The host turn spawns one background task (no delegation id); capture it.
	var det *task.ManualDetector
	select {
	case det = <-spawned:
	case <-time.After(5 * time.Second):
		mu.Lock()
		got := texts
		mu.Unlock()
		close(stop)
		b.StopLoop()
		collectWg.Wait()
		t.Fatalf("host form: no background spawn captured (owner turn did not reach the spawner); texts=%v", got)
	}

	// Settle it late. With no delegation id, OnSettle falls back to the persistentBus,
	// where the resident loop picks it up as a new turn.
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "SETTLE-HOST"})
	det.Done()

	// The loop must consume its OWN task_settled and continue: a second answer whose
	// content echoes the settle ("SETTLE-HOST") arrives on outputCh.
	deadline := time.After(5 * time.Second)
	sawContinuation := false
	for !sawContinuation {
		mu.Lock()
		for _, tx := range texts {
			if strings.Contains(tx, "SETTLE-HOST") {
				sawContinuation = true
			}
		}
		mu.Unlock()
		if sawContinuation {
			break
		}
		select {
		case <-deadline:
			sawContinuation = true // break loop; assertion below reports the miss
		case <-time.After(20 * time.Millisecond):
		}
	}

	mu.Lock()
	final := append([]string(nil), texts...)
	mu.Unlock()
	close(stop)
	b.StopLoop()
	collectWg.Wait()

	// Host form must produce BOTH an initial answer and a continuation from its OWN
	// late task_settled — the same越窗-continue-to-receiver behavior the callee form
	// (d8/d11) shows, via runEventLoop/processTurn instead of sink/tail.
	var initial, continuation int
	for _, tx := range final {
		if strings.Contains(tx, "SETTLE-HOST") {
			continuation++
		} else if strings.HasPrefix(tx, "answer:") {
			initial++
		}
	}
	require.GreaterOrEqual(t, initial, 1, "host form: initial (first-answer) turn must reach outputCh — texts=%v", final)
	require.Equal(t, 1, continuation,
		"host form: the owner's own late task_settled must drive exactly one continuation turn on outputCh — texts=%v", final)
}
