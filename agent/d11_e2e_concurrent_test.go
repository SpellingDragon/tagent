package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// echoModel is a STATELESS, content-derived model — safe for concurrent delegation
// tests because it holds no per-agent counter that two racing Runs could interleave
// (the shared sequenceMockModel's flaw). Its rule, a pure function of the request:
//   - the history has no assistant tool-call yet → emit ONE tool call (spawns a bg task)
//   - an assistant tool-call already exists → emit a final that echoes the LAST message
//
// So each invocation: turn1 first call → tool call; thereafter → "answer:<last>" (the
// initial answer after the tool result, and the continuation echoing the settle event
// whose rendered content carries the settle Output "SETTLE-<k>"). Detecting on the
// assistant tool-call (not on tool-role messages) is representation-stable. That echo
// lets a test correlate which background settle reached which caller — the I1 check.
type echoModel struct{}

func (m *echoModel) GenerateContent(_ context.Context, request *model.Request) (<-chan *model.Response, error) {
	// Discriminate "fresh turn" from "a tool round already happened" by counting
	// NON-system messages: this framework presents tool results as user-role messages
	// and strips the assistant's ToolCalls from the request, so those fields are not
	// reliable; message count is. Fresh = only the initial user input (≤1 non-system)
	// → emit ONE tool call (spawn a background task). Once more than one non-system
	// message exists (a tool result / a settle arrived) → emit a final echoing the
	// LAST message content, which carries the settle's "SETTLE-<k>" tag for the I1
	// correlation. A pure function of the request → safe under concurrent Runs.
	nonSystem := 0
	for _, msg := range request.Messages {
		if msg.Role != model.RoleSystem {
			nonSystem++
		}
	}
	last := ""
	if n := len(request.Messages); n > 0 {
		last = request.Messages[n-1].Content
	}
	var resp *model.Response
	if nonSystem <= 1 {
		resp = toolCallResponse("spawn", "go")
	} else {
		resp = finalTextResponse("echo", "answer:"+last)
	}
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *echoModel) Info() model.Info { return model.Info{Name: "echo"} }

// e2eProbeTool spawns one background task through the ctx spawner with an EMPTY Key
// (empty Key disables the manager's idempotency dedup, so two concurrent delegations
// each get their OWN task instead of the second being collapsed onto the first) and a
// ManualDetector whose sync-wait window detaches quickly; it hands the detector to the
// test AFTER Spawn returns (so the test's settle Emit is provably post-window/
// background → fires OnSettle → routes to that invocation's sink).
type e2eProbeTool struct{ spawned chan *task.ManualDetector }

func (p *e2eProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "probe",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{"command": {Type: "string"}},
			Required:   []string{"command"},
		},
	}
}

func (p *e2eProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	if !ok {
		return "no spawner", nil
	}
	det := task.NewManualDetectorDetach(20 * time.Millisecond)
	spawner.Spawn(task.TaskSpec{Kind: "probe", Desc: "i1 e2e bg"}, det) // empty Key: no dedup across runs
	p.spawned <- det
	return "spawned", nil
}

// TestI1ConcurrentDelegationsEndToEnd is the 7.2② end-to-end 主子同构 gate: two
// CONCURRENT delegations to the SAME callee, each spawning its own background task
// that settles late, must each (a) deliver the initial answer and (b) independently
// 越窗-continue when ITS OWN settle arrives on the SAME returned channel — and (c)
// never cross receivers (design line 169: "并发调用不串接收者"). Crossing shows up as
// either one channel hanging (its settle was stolen → tail never quiesces) or one
// channel receiving both settles (a duplicate continuation) — both are caught.
func TestI1ConcurrentDelegationsEndToEnd(t *testing.T) {
	spawned := make(chan *task.ManualDetector, 4)
	probe := &e2eProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: &echoModel{}, SystemPrompt: "You are callee B.",
		Name: "callee-i1-e2e", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	ids := []string{"inv-A", "inv-B"}
	texts := make([][]string, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			// Cancelable ctx bounds the tail even if routing were broken (no goroutine
			// leak); a healthy run closes well before this.
			runCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			inv := trpcagent.NewInvocation(
				trpcagent.WithInvocationMessage(model.NewUserMessage("do work " + id)),
			)
			inv.InvocationID = id
			ch, err := b.Run(runCtx, inv)
			require.NoError(t, err)
			for evt := range ch {
				if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
					continue
				}
				m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
				if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
					texts[i] = append(texts[i], m.Content)
				}
			}
		}(i, id)
	}

	// Capture both background detectors (one per concurrent delegation).
	dets := make([]*task.ManualDetector, 0, 2)
	for len(dets) < 2 {
		select {
		case d := <-spawned:
			dets = append(dets, d)
		case <-time.After(5 * time.Second):
			t.Fatal("I1 e2e: fewer than 2 background spawns were captured")
		}
	}

	// Settle each with a DISTINCT tagged output, after both are detached (background).
	for k, d := range dets {
		d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: fmt.Sprintf("SETTLE-%d", k)})
		d.Done()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	timedOut := false
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		timedOut = true
	}

	t.Logf("inv-A texts=%v", texts[0])
	t.Logf("inv-B texts=%v", texts[1])
	if timedOut {
		t.Errorf("I1 e2e: a delegation channel did not close in time (see texts above)")
	}

	// Each delegation received exactly ONE continuation, and the two continuations
	// carry DIFFERENT settle tags → no receiver saw the other's settle.
	var tags []string
	for i := range texts {
		var mine, initial []string
		for _, tx := range texts[i] {
			if strings.Contains(tx, "SETTLE-") {
				mine = append(mine, tx)
			} else {
				initial = append(initial, tx)
			}
		}
		require.NotEmpty(t, initial,
			"I1: delegation %s must receive its initial (first-answer) turn — texts=%v", ids[i], texts[i])
		require.Len(t, mine, 1,
			"I1: delegation %s received %d continuations (want exactly its own 1) — texts=%v", ids[i], len(mine), texts[i])
		tags = append(tags, mine[0])
	}
	require.NotEqual(t, tags[0], tags[1],
		"I1: both delegations received the SAME settle — sinks crossed (receivers not isolated)")
}
