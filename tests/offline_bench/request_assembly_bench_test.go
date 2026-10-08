// Request-assembly cost benchmark: the full-input budget path (deep-copy
// snapshot + per-part estimate) is timed against the projection-only counter
// on the SAME fixture, both inside this tree — the delta is the overhead the
// completeness gate accepts or rejects, so no pre-change tree is needed
// (the pair of APIs lives side by side here).
//
// 规格: docs/wiki/platform/evaluation-suites.md#offline-bench
package offline_bench

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/modelutil"
)

// declOnlyTool exposes a Declaration without a callable body: the snapshot path
// only ever reads declarations, and this keeps the fixture honest about that.
type declOnlyTool struct{ d *tool.Declaration }

func (t declOnlyTool) Declaration() *tool.Declaration { return t.d }

// assemblyFixture builds ~120 messages (with two long-argument tool calls) and
// eight declared tools with 1-2 KiB schemas: the shape a real resident turn
// carries when the fixed overhead actually matters.
func assemblyFixture(tb testing.TB) ([]model.Message, map[string]tool.Tool, string) {
	tb.Helper()
	msgs := make([]model.Message, 0, 120)
	msgs = append(msgs, model.NewSystemMessage("you are a resident agent"))
	for i := 0; i < 58; i++ {
		msgs = append(msgs, model.NewUserMessage(strings.Repeat("u", 1024)))
		am := model.NewAssistantMessage(strings.Repeat("a", 1024))
		if i%29 == 28 {
			call := model.ToolCall{Type: "function", ID: fmt.Sprintf("call-%d", i)}
			call.Function.Name = "alpha"
			call.Function.Arguments = []byte(`{"query":"` + strings.Repeat("q", 2000) + `"}`)
			am.ToolCalls = []model.ToolCall{call}
		}
		msgs = append(msgs, am)
	}
	tools := map[string]tool.Tool{}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("t%d", i)
		tools[name] = declOnlyTool{d: &tool.Declaration{
			Name:        name,
			Description: strings.Repeat("d", 1024),
			InputSchema: &tool.Schema{Type: "object", Properties: map[string]*tool.Schema{
				"query": {Type: "string", Description: strings.Repeat("s", 1024)},
			}},
		}}
	}
	return msgs, tools, strings.Repeat("notice-", 64)
}

func assemblyGate(b *testing.B) {
	if os.Getenv(envRun) != "1" {
		b.Skipf("offline benchmark: set %s=1 to run (see file header)", envRun)
	}
}

// BenchmarkRequestAssembly compares the two construction paths per request:
// projection-only counting versus the full-input snapshot plus budget.
// The accepted reading is the RATIO on one fixture, one tree, one process.
func BenchmarkRequestAssembly(b *testing.B) {
	assemblyGate(b)
	msgs, tools, notices := assemblyFixture(b)

	b.Run("legacy_projection_counter", func(b *testing.B) {
		counter := compress.NewDefaultTokenCounter()
		b.ReportMetric(float64(len(msgs)), "msgs")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if n := counter.Estimate(msgs); n <= 0 {
				b.Fatal("empty estimate on a populated fixture")
			}
		}
	})

	b.Run("snapshot_budget", func(b *testing.B) {
		b.ReportMetric(float64(len(msgs)), "msgs")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			snap := modelutil.NewRequestSnapshot(msgs, tools,
				modelutil.WithGenerationConfig(model.GenerationConfig{Stream: false}))
			bud := snap.EstimateBudget(notices)
			if bud.Total <= 0 || len(bud.Unknown) != 0 {
				b.Fatal("budget must account the fixture and declare no unknowns here")
			}
		}
	})
}
