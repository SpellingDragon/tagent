package agent

// Full-lifecycle context simulation (stable-context-compaction verification):
// drive the REAL pipeline (newTaskSettledEvent spill + ContextCompressor
// render-freeze compaction) through a session script and assert the exact
// message structure the model sees at each stage.
//
//	Session phases:
//	  A. small session, 3 plain turns                  → everything native full
//	  B. task_settled arrivals (small + oversized)     → inline / spill ticket
//	  C. more turns with tool runs                     → aged material forms
//	  D. capacity-triggered compaction (low budget)    → rolling summary +
//	                                                     tool_chain + cards +
//	                                                     boundary anchor
//	  E. post-compaction append (under budget)         → byte-stable prefix +
//	                                                     new full frontier
//
// Companion coverage (not duplicated here): recall(turn_key) full-turn
// reconstruction — tool/recall/recall_test.go; L3 card-line shape —
// agent/compress tests.
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// dumpPhase prints the model-visible message structure for a phase (verbose
// runs only) — the "what does the model actually see" evidence trail.
func dumpPhase(t *testing.T, name string, msgs []model.Message) {
	t.Helper()
	t.Logf("===== phase %s: %d messages =====", name, len(msgs))
	for i, m := range msgs {
		c := strings.ReplaceAll(m.Content, "\n", "⏎")
		if len(c) > 72 {
			c = c[:72] + "…"
		}
		t.Logf("  [%2d] %-9s %s", i, m.Role, c)
	}
}

// simTurn stores one plain task turn (ext → tp+toolcall → ac → out) and
// returns its refs. Keys are monotonically increasing (render-freeze relies
// on key monotonicity).
func simTurn(store *memory.InMemoryStore, base int64, label string) []memory.EventReference {
	ref := func(key int64, typ, summary string, role string) memory.EventReference {
		return memory.EventReference{EventKey: key, EventType: typ, EventSummary: summary, Timestamp: key, Role: role}
	}
	refs := []memory.EventReference{
		ref(base, "external_input", "请求 "+label, "user"),
		ref(base+1, "thinking_plan", "调用 read_"+label, "assistant"),
		ref(base+2, "action_command", "结果 "+label, "tool"),
		ref(base+3, "agent_output", "完成 "+label, "assistant"),
	}
	fulls := []memory.FullEvent{
		{EventKey: base, EventType: "external_input", Content: "用户请求 " + label + " 的完整原文，稍微长一点以便有内容可观。", EventSummary: "请求 " + label},
		{EventKey: base + 1, EventType: "thinking_plan", Content: "调用 read_" + label, EventSummary: "调用 read_" + label},
		{EventKey: base + 2, EventType: "action_command", Content: "工具执行结果 " + label + " 的完整输出内容。", EventSummary: "结果 " + label},
		{EventKey: base + 3, EventType: "agent_output", Content: "任务 " + label + " 已完成的答复全文。", EventSummary: "完成 " + label},
	}
	for _, fe := range fulls {
		if err := store.StoreEvent(fe.EventKey, fe); err != nil {
			panic(err)
		}
	}
	return refs
}

// simSettle builds a task_settled event via the REAL constructor (spill
// included), stores it, and returns its ref.
func simSettle(store *memory.InMemoryStore, key int64, output string, spillDir string) memory.EventReference {
	tk := &task.Task{ID: fmt.Sprintf("sim-%012d", key), Spec: task.TaskSpec{Kind: "command", Desc: "后台命令 " + fmt.Sprintf("%x", key)}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: output}, 100000, spillDir)
	if err := store.StoreEvent(key, memory.FullEvent{
		EventKey: key, EventType: "external_input",
		Content: evt.Message.Content, EventSummary: evt.Message.Content, Timestamp: key,
	}); err != nil {
		panic(err)
	}
	return memory.EventReference{EventKey: key, EventType: "external_input", EventSummary: evt.Message.Content, Timestamp: key, Role: "user"}
}

func TestContextLifecycleSimulation(t *testing.T) {
	store := memory.NewInMemoryStore()
	spillDir := t.TempDir()

	join := func(msgs []model.Message) string {
		var b strings.Builder
		for _, m := range msgs {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
		return b.String()
	}

	// Healthy-budget compressor for phases A-C, E (8000 tokens, threshold
	// 6400); phase D builds a low-budget one sharing the same store.
	ccHealthy := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2), compress.WithMaxTokens(8000)),
		store, compress.NewDefaultTokenCounter(), 8000, 0.8, 2,
		compress.WithRecentFullCount(8))

	// ---- Phase A: small session — everything native full ----
	var refs []memory.EventReference
	refs = append(refs, simTurn(store, 100, "A1")...)
	refs = append(refs, simTurn(store, 200, "A2")...)
	refs = append(refs, simTurn(store, 300, "A3")...)
	rA := ccHealthy.Compress(context.Background(), refs)
	if len(rA.RetainedRefs) != len(refs) {
		t.Fatalf("A: under-budget pass-through must keep all refs (%d != %d)", len(rA.RetainedRefs), len(refs))
	}
	joinedA := join(rA.Messages)
	dumpPhase(t, "A small-session", rA.Messages)
	if strings.Contains(joinedA, "[Compacted") || strings.Contains(joinedA, "工具链") {
		t.Fatalf("A: small session must have no rolling summary / tool chains:\n%s", joinedA)
	}
	if !strings.Contains(joinedA, "A1 的完整原文") {
		t.Fatalf("A: pre-compaction everything renders full (boundary=0)")
	}

	// ---- Phase B: settle arrivals ----
	small := strings.Repeat("小结果内容。", 40) // ~240 chars, inline
	refs = append(refs, simSettle(store, 400, small, spillDir))
	rB := ccHealthy.Compress(context.Background(), refs)
	joinedB := join(rB.Messages)
	dumpPhase(t, "B small-settle-inline", rB.Messages)
	if strings.Contains(joinedB, "output_spilled") || !strings.Contains(joinedB, "小结果内容") {
		t.Fatalf("B: small settle must stay fully inline:\n%.300s", joinedB)
	}
	big := strings.Repeat("x", 300000) // > 100k threshold → spill
	settleRef := simSettle(store, 500, big, spillDir)
	if !strings.Contains(settleRef.EventSummary, "output_spilled") {
		t.Fatalf("B: oversized settle must carry the spill ticket")
	}
	if len(settleRef.EventSummary) > 5000 {
		t.Fatalf("B: settle event Content must stay bounded (len=%d)", len(settleRef.EventSummary))
	}
	matches, _ := filepath.Glob(filepath.Join(spillDir, "task-sim-*.txt"))
	if len(matches) == 0 {
		t.Fatalf("B: spilled file must exist under %s", spillDir)
	}
	if data, err := os.ReadFile(matches[0]); err != nil || len(data) != len(big) {
		t.Fatalf("B: spilled file must hold the full body (err=%v len=%d want %d)", err, len(data), len(big))
	}
	refs = append(refs, settleRef)

	// ---- Phase C: more turns (material for folding / cards) ----
	refs = append(refs, simTurn(store, 600, "C1")...)
	refs = append(refs, simTurn(store, 700, "C2")...)
	refs = append(refs, simTurn(store, 800, "C3")...)
	refs = append(refs, simTurn(store, 900, "C4")...)

	// ---- Phase D: capacity-triggered compaction ----
	// Budget passes the capacity gate (render ≈ thousands of tokens vs
	// threshold 800) without the all-L3 escalation storm, so mid-aged turns
	// dwell at L1/L2 where their tool_chain lines survive.
	ccSmall := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2), compress.WithMaxTokens(1000)),
		store, compress.NewDefaultTokenCounter(), 1000, 0.8, 2,
		compress.WithRecentFullCount(8))
	rD := ccSmall.Compress(context.Background(), refs)
	dumpPhase(t, "D compaction", rD.Messages)
	hasSummary, hasChain := false, false
	for _, r := range rD.RetainedRefs {
		if r.EventType == "context_compress" {
			hasSummary = true
			if !strings.Contains(r.EventSummary, "[Compacted") {
				t.Fatalf("D: rolling summary must carry the compacted count")
			}
			for _, line := range strings.Split(r.EventSummary, "\n") {
				if strings.HasPrefix(line, "- ") && len(line) > 120 {
					t.Fatalf("D: card line must be bounded (~80 chars + ticket):\n%s", line)
				}
			}
		}
		if r.EventType == "tool_chain" {
			hasChain = true
			if !strings.Contains(r.EventSummary, "[evt_") || !strings.Contains(r.EventSummary, "（") {
				t.Fatalf("D: tool_chain line must carry the step count + tickets: %s", r.EventSummary)
			}
		}
	}
	if !hasSummary || !hasChain {
		t.Fatalf("D: compaction must form a rolling summary and tool chains (summary=%v chain=%v)", hasSummary, hasChain)
	}
	retainedD := rD.RetainedRefs

	// ---- Phase E: post-compaction append (under budget, frozen prefix) ----
	ccE := compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2), compress.WithMaxTokens(8000)),
		store, compress.NewDefaultTokenCounter(), 8000, 0.8, 2,
		compress.WithRecentFullCount(8))
	ccE.SetFullBoundary(ccSmall.FullBoundary())
	rE1 := ccE.Compress(context.Background(), retainedD)
	refsE := append(append([]memory.EventReference{}, retainedD...), simTurn(store, 1000, "E1")...)
	rE2 := ccE.Compress(context.Background(), refsE)
	dumpPhase(t, "E frozen-prefix + new frontier", rE2.Messages)
	if len(rE2.Messages) < len(rE1.Messages) {
		t.Fatalf("E: appending events must not shrink the rendered timeline")
	}
	for i := 0; i < len(rE1.Messages); i++ {
		if rE1.Messages[i].Content != rE2.Messages[i].Content ||
			rE1.Messages[i].Role != rE2.Messages[i].Role {
			t.Fatalf("E: message %d must be byte-stable across under-budget rounds (prefix freeze):\n%q\nvs\n%q",
				i, rE1.Messages[i].Content, rE2.Messages[i].Content)
		}
	}
	last := rE2.Messages[len(rE2.Messages)-1]
	if !strings.Contains(last.Content, "E1") {
		t.Fatalf("E: newly appended events must render full (active frontier): %s", last.Content)
	}
}
