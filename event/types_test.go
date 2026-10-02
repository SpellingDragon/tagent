package event

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	frameworkevent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestExtractEventType_ThinkingPlan(t *testing.T) {
	tests := []struct {
		name     string
		msg      model.Message
		expected string
	}{
		{
			name:     "user message is external_input",
			msg:      model.Message{Role: model.RoleUser, Content: "hello"},
			expected: TypeExternalInput,
		},
		{
			name: "assistant with tool calls is thinking_plan",
			msg: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{
					{Function: model.FunctionDefinitionParam{Name: "echo", Arguments: []byte(`"hello"`)}},
				},
			},
			expected: TypeThinkingPlan,
		},
		{
			name:     "assistant without tool calls is agent_output",
			msg:      model.Message{Role: model.RoleAssistant, Content: "done"},
			expected: TypeAgentOutput,
		},
		{
			name:     "tool result is action_command",
			msg:      model.Message{Role: model.RoleTool, Content: "result"},
			expected: TypeActionCommand,
		},
		{
			name:     "system message is external_input",
			msg:      model.Message{Role: model.RoleSystem, Content: "injection"},
			expected: TypeExternalInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEventType(tt.msg)
			if got != tt.expected {
				t.Errorf("ExtractEventType(%v) = %q, want %q", tt.msg.Role, got, tt.expected)
			}
		})
	}
}

func TestIsSpecialEventType_ThinkingPlan(t *testing.T) {
	tests := []struct {
		eventType string
		expected  bool
	}{
		{TypeExternalInput, true},
		{TypeAgentOutput, true},
		{TypeThinkingPlan, true},
		{TypeActionCommand, false},
		{TypeContextCompress, false},
		{TypeThinkingRecall, false},
		{TypeThinkingKnowledge, false},
		{"unknown_type", false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := IsSpecialEventType(tt.eventType)
			if got != tt.expected {
				t.Errorf("IsSpecialEventType(%q) = %v, want %v", tt.eventType, got, tt.expected)
			}
		})
	}
}

// TestGenerateEventSummary_ToolCallThinkingPlan 钉住纯工具调用的 thinking_plan 视图：
//
// 契约: docs/wiki/event/event-architecture.md#summary-naming
func TestGenerateEventSummary_ToolCallThinkingPlan(t *testing.T) {
	opts := DefaultOptionsForLLMContext()
	tc := func(names ...string) []model.ToolCall {
		var calls []model.ToolCall
		for _, n := range names {
			calls = append(calls, model.ToolCall{Function: model.FunctionDefinitionParam{Name: n, Arguments: []byte(`{}`)}})
		}
		return calls
	}

	got := GenerateEventSummary(model.Message{Role: model.RoleAssistant, ToolCalls: tc("read_file", "grep")}, TypeThinkingPlan, opts)
	if got != "调用 read_file、grep" {
		t.Errorf("multi tool-call summary = %q, want %q", got, "调用 read_file、grep")
	}
	got = GenerateEventSummary(model.Message{Role: model.RoleAssistant, ToolCalls: tc("edit_file")}, TypeThinkingPlan, opts)
	if got != "调用 edit_file" {
		t.Errorf("single tool-call summary = %q, want %q", got, "调用 edit_file")
	}
	got = GenerateEventSummary(model.Message{Role: model.RoleAssistant, Content: "先读文件再分析", ToolCalls: tc("read_file")}, TypeThinkingPlan, opts)
	if got != "先读文件再分析" {
		t.Errorf("thinking_plan with content = %q, want original content", got)
	}
}

// TestParseEventMeta_RoundTrip 钉住 元数据完整性：契约键写入的值必须被原样解析回来，不得在往返中丢失或改写。
func TestParseEventMeta_RoundTrip(t *testing.T) {
	evt := frameworkevent.New("inv-1", "tagent")
	evt.StateDelta = map[string][]byte{
		MetaKeyEventKey:          []byte(FormatEventKey(1297375767008641024)),
		MetaKeyPartitionID:       []byte("144"),
		MetaKeyEventType:         []byte("agent_output"),
		MetaKeyEventSummary:      []byte("summary text"),
		MetaKeyTriggerSource:     []byte("task"),
		MetaPrefix + "chat_id":   []byte("room-42"),
		MetaPrefix + "user_name": []byte("alice"),
		"unrelated":              []byte("ignored"),
	}

	meta := ParseEventMeta(evt)
	if meta.EventKey != 1297375767008641024 {
		t.Errorf("EventKey = %d", meta.EventKey)
	}
	if meta.PartitionID != 144 {
		t.Errorf("PartitionID = %d", meta.PartitionID)
	}
	if meta.EventType != "agent_output" || meta.EventSummary != "summary text" {
		t.Errorf("type/summary mismatch: %+v", meta)
	}
	if meta.TriggerSource != "task" {
		t.Errorf("TriggerSource = %q", meta.TriggerSource)
	}
	if meta.Meta["chat_id"] != "room-42" || meta.Meta["user_name"] != "alice" {
		t.Errorf("passthrough meta mismatch: %+v", meta.Meta)
	}
	if _, ok := meta.Meta["unrelated"]; ok {
		t.Error("non-contract keys must not leak into Meta")
	}
}

// TestParseEventMeta_NilSafe 钉住解析端永不产出 nil map，缺失字段留零值。
func TestParseEventMeta_NilSafe(t *testing.T) {
	meta := ParseEventMeta(nil)
	if meta.Meta == nil {
		t.Fatal("Meta must be non-nil")
	}
	if meta.EventKey != 0 || meta.TriggerSource != "" {
		t.Errorf("zero values expected: %+v", meta)
	}
}

// checkParseEventKeyNoPanic 断言 ParseEventKey 是全函数：只返回值或错误，不 panic。
func checkParseEventKeyNoPanic(t testing.TB, s string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ParseEventKey(%q) panicked: %v", s, r)
		}
	}()
	_, _ = ParseEventKey(s)
}

// TestEventKey_BoundedFuzz 钉住 召回键解析器的确定性：模型回显的任意文本（超长、带边界值、畸形）都只能被安全解析，不得 panic 或越界。
func TestEventKey_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xfeedf00d))

	seeds := []string{
		"", " ", "0x", "evt_", "[evt_]", "[]", "-0x", "0x0", "-9223372036854775808",
		"zzz", "12:g4", "[evt_1f|agent_output]", "0XABCDEF", "  1f  ", "- - 1f",
		strings.Repeat("f", 40), "7fffffffffffffffffff", "evt_-1a", "[evt_-2b|x]",
	}
	for i := 0; i < 20000; i++ {
		seeds = append(seeds, randomKeyString(rng))
	}
	for _, s := range seeds {
		checkParseEventKeyNoPanic(t, s)
	}

	for i := 0; i < 50000; i++ {
		k := int64(rng.Uint64())
		if k == math.MinInt64 {
			continue
		}
		canonical := FormatEventKey(k)
		got, err := ParseEventKey(canonical)
		if err != nil {
			t.Fatalf("ParseEventKey(%q) rejected canonical form of %d: %v", canonical, k, err)
		}
		if got != k {
			t.Fatalf("round-trip: k=%d canonical=%q got=%d", k, canonical, got)
		}
		for _, variant := range tolerantForms(k, canonical) {
			got, err := ParseEventKey(variant)
			if err != nil {
				t.Fatalf("ParseEventKey(%q) [variant of %d] rejected: %v", variant, k, err)
			}
			if got != k {
				t.Fatalf("variant round-trip: k=%d variant=%q got=%d", k, variant, got)
			}
		}
	}

	if _, err := ParseEventKey("[AAAA0004G]"); err == nil {
		t.Fatalf("non-hex bracketed text parsed without error (should reject)")
	}
}

// tolerantForms 返回 ParseEventKey 承诺接受的各包装形态，均编码同一个键。
func tolerantForms(k int64, canonical string) []string {
	if k < 0 {
		return []string{fmt.Sprintf("evt_%s", canonical), fmt.Sprintf("[evt_%s|agent_output]", canonical)}
	}
	return []string{
		"0x" + canonical,
		"0X" + strings.ToUpper(canonical),
		"evt_" + canonical,
		fmt.Sprintf("[evt_%s|external_input]", canonical),
		canonical + "|agent_output",
	}
}

func randomKeyString(rng *rand.Rand) string {
	tokens := []string{"0x", "0X", "evt_", "[", "]", "|", "-", " ", "f", "a", "1", "9",
		"g", "z", "9223372036854775808", "agent_output", "\n", strings.Repeat("0", 30)}
	var b strings.Builder
	n := 1 + rng.Intn(7)
	for i := 0; i < n; i++ {
		b.WriteString(tokens[rng.Intn(len(tokens))])
	}
	return b.String()
}

// FuzzParseEventKey 是供扩展挖掘的原生目标，不作为常规门禁。
func FuzzParseEventKey(f *testing.F) {
	for _, s := range []string{"1f", "-2a", "0x1f", "evt_1f", "[evt_1f|agent_output]", "bad", "9223372036854775808"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		checkParseEventKeyNoPanic(t, s)
	})
}
