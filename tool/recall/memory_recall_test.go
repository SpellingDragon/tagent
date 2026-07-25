package recall

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func callMemoryRecall(t *testing.T, tl tool.Tool, args string) string {
	t.Helper()
	ct, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("memory_recall must be callable")
	}
	out, err := ct.Call(context.Background(), []byte(args))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func seedStore(t *testing.T) memory.MemoryStore {
	t.Helper()
	store := memory.NewInMemoryStore()
	must := func(err error) {
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	must(store.StoreEvent(100, memory.FullEvent{EventKey: 100, EventType: "external_input", EventSummary: "部署请求", Content: "请部署服务到测试环境", Timestamp: 1710000000000}))
	must(store.StoreEvent(200, memory.FullEvent{EventKey: 200, EventType: "agent_output", EventSummary: "部署完成", Content: "服务已部署,健康检查通过", Timestamp: 1710000100000}))
	return store
}

// TestMemoryRecall_ItemsPrecise: tickets are resolved precisely, in order,
// with hints echoed back and full content returned.
func TestMemoryRecall_ItemsPrecise(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), nil)
	out := callMemoryRecall(t, tl, `{"items":[{"key":"`+tagentevent.FormatEventKey(200)+`","hint":"部署完成卡片"},{"key":"`+tagentevent.FormatEventKey(100)+`"}]}`)

	for _, want := range []string{`"mode":"items"`, "服务已部署", "请部署服务", "部署完成卡片"} {
		if !strings.Contains(out, want) {
			t.Errorf("items recall must contain %q, got: %s", want, out)
		}
	}
	if strings.Contains(out, `"miss":true`) {
		t.Errorf("no miss expected, got: %s", out)
	}
}

// TestMemoryRecall_MissReported: unknown keys are explicitly marked miss,
// never silently omitted.
func TestMemoryRecall_MissReported(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), nil)
	out := callMemoryRecall(t, tl, `{"items":[{"key":"dead"},{"key":"`+tagentevent.FormatEventKey(100)+`"}]}`)

	if !strings.Contains(out, `"miss":true`) || !strings.Contains(out, `"misses":1`) {
		t.Errorf("miss must be reported explicitly, got: %s", out)
	}
	if !strings.Contains(out, "请部署服务") {
		t.Errorf("hit entry must still resolve, got: %s", out)
	}
}

// TestMemoryRecall_QuerySemantic: free-text query goes through the retrieval
// layer (keyword match).
func TestMemoryRecall_QuerySemantic(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), nil)
	out := callMemoryRecall(t, tl, `{"query":"部署"}`)

	if !strings.Contains(out, `"mode":"query"`) || !strings.Contains(out, "部署完成") {
		t.Errorf("query recall must match by keyword, got: %s", out)
	}
}

// TestMemoryRecall_ItemsPrecedence: when both items and query are provided,
// items win (protocol rule).
func TestMemoryRecall_ItemsPrecedence(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), nil)
	out := callMemoryRecall(t, tl, `{"items":[{"key":"`+tagentevent.FormatEventKey(100)+`"}],"query":"部署"}`)

	if !strings.Contains(out, `"mode":"items"`) {
		t.Errorf("items must take precedence over query, got: %s", out)
	}
}

// TestMemoryRecall_TicketLosslessness: a rendered index-card line's [hex] key
// can be cut out verbatim and used as a ticket.
func TestMemoryRecall_TicketLosslessness(t *testing.T) {
	cardLine := "- 03-09 17:20 [" + tagentevent.FormatEventKey(200) + "] 部署完成"
	// Model-side extraction: text between "[" and "]".
	start := strings.IndexByte(cardLine, '[')
	end := strings.IndexByte(cardLine, ']')
	ticket := cardLine[start+1 : end]

	tl := NewMemoryRecallTool(seedStore(t), nil)
	out := callMemoryRecall(t, tl, `{"items":[{"key":"`+ticket+`"}]}`)
	if strings.Contains(out, `"miss":true`) || !strings.Contains(out, "服务已部署") {
		t.Errorf("card-line ticket must resolve losslessly, got: %s", out)
	}
}
