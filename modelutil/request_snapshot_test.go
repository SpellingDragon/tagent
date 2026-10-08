package modelutil

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// declTool implements only tool.Tool (Declaration) — enough for the snapshot.
type declTool struct{ decl *tool.Declaration }

func (d declTool) Declaration() *tool.Declaration { return d.decl }

// callableTool also implements tool.CallableTool so we can pin that the
// read-only snapshot never invokes Call nor replaces the executable capability.
type callableTool struct {
	declTool
	called bool
}

func (c *callableTool) Call(_ context.Context, _ []byte) (any, error) {
	c.called = true
	return nil, nil
}

func richSchema() *tool.Schema {
	return &tool.Schema{
		Type:        "object",
		Description: "a tool input",
		Required:    []string{"query"},
		Properties: map[string]*tool.Schema{
			"query": {Type: "string", Description: "search text"},
		},
	}
}

// TestRequestSnapshot_DeepCopyIsolation pins the frozen-snapshot contract.
//   - mutating the live request afterwards cannot reach the recorded copy
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestRequestSnapshot_DeepCopyIsolation(t *testing.T) {
	text := "hello"
	idx := 0
	args := []byte(`{"a":1}`)
	imgData := []byte{1, 2, 3, 4}
	msgs := []model.Message{
		{
			Role:             model.RoleAssistant,
			Content:          "orig content",
			ReasoningContent: "orig reasoning",
			ToolCalls: []model.ToolCall{
				{Type: "function", ID: "call-1", Index: &idx,
					Function: model.FunctionDefinitionParam{Name: "search", Arguments: args}},
			},
			ContentParts: []model.ContentPart{
				{Type: model.ContentTypeText, Text: &text},
				{Type: model.ContentTypeImage, Image: &model.Image{URL: "u", Data: imgData}},
			},
		},
	}
	decl := &tool.Declaration{
		Name:         "search",
		Description:  "orig description",
		InputSchema:  richSchema(),
		OutputSchema: &tool.Schema{Type: "string"},
	}
	tools := map[string]tool.Tool{"search-key": declTool{decl}}

	snap := NewRequestSnapshot(msgs, tools)

	msgs[0].Content = "mutated content"
	msgs[0].ReasoningContent = "mutated reasoning"
	msgs[0].ToolCalls[0].Function.Arguments[2] = '9'
	msgs[0].ToolCalls[0].ID = "mutated-id"
	*msgs[0].ContentParts[0].Text = "mutated part"
	msgs[0].ContentParts[1].Image.Data[0] = 99
	*msgs[0].ToolCalls[0].Index = 42
	decl.Description = "mutated description"
	decl.InputSchema.Properties["injected"] = &tool.Schema{Type: "object"}
	decl.InputSchema.Required = append(decl.InputSchema.Required, "extra")

	if got := snap.Messages[0].Content; got != "orig content" {
		t.Fatalf("message content leaked: %q", got)
	}
	if got := snap.Messages[0].ReasoningContent; got != "orig reasoning" {
		t.Fatalf("reasoning leaked: %q", got)
	}
	if got := string(snap.Messages[0].ToolCalls[0].Function.Arguments); got != `{"a":1}` {
		t.Fatalf("tool-call arguments leaked: %q", got)
	}
	if got := snap.Messages[0].ToolCalls[0].ID; got != "call-1" {
		t.Fatalf("tool-call id leaked: %q", got)
	}
	if got := *snap.Messages[0].ToolCalls[0].Index; got != 0 {
		t.Fatalf("tool-call index leaked: %d", got)
	}
	if got := *snap.Messages[0].ContentParts[0].Text; got != "hello" {
		t.Fatalf("content-part text leaked: %q", got)
	}
	if got := snap.Messages[0].ContentParts[1].Image.Data[0]; got != 1 {
		t.Fatalf("content-part image data leaked: %d", got)
	}
	if got := snap.Tools[0].Description; got != "orig description" {
		t.Fatalf("declaration description leaked: %q", got)
	}
	if _, ok := snap.Tools[0].InputSchema.Properties["injected"]; ok {
		t.Fatal("declaration schema properties leaked")
	}
	if len(snap.Tools[0].InputSchema.Required) != 1 {
		t.Fatalf("declaration schema required leaked: %v", snap.Tools[0].InputSchema.Required)
	}
}

func TestRequestSnapshot_RoundTripsRichFields(t *testing.T) {
	text := "part text"
	msgs := []model.Message{
		{
			Role:               model.RoleAssistant,
			Content:            "answer",
			ReasoningContent:   "think",
			ReasoningSignature: "sig",
			ToolCalls: []model.ToolCall{
				{Type: "function", ID: "c1",
					Function: model.FunctionDefinitionParam{Name: "n", Arguments: []byte(`{"k":9}`)}},
			},
			ContentParts: []model.ContentPart{
				{Type: model.ContentTypeText, Text: &text},
				{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://x", Detail: "low"}},
			},
		},
	}
	snap := NewRequestSnapshot(msgs, nil)
	m := snap.Messages[0]
	if m.ReasoningContent != "think" || m.ReasoningSignature != "sig" {
		t.Fatalf("reasoning fields lost: %+v", m)
	}
	if len(m.ToolCalls) != 1 || m.ToolCalls[0].ID != "c1" ||
		string(m.ToolCalls[0].Function.Arguments) != `{"k":9}` ||
		m.ToolCalls[0].Function.Name != "n" {
		t.Fatalf("tool calls lost: %+v", m.ToolCalls)
	}
	if len(m.ContentParts) != 2 || *m.ContentParts[0].Text != "part text" ||
		m.ContentParts[1].Image.URL != "http://x" || m.ContentParts[1].Image.Detail != "low" {
		t.Fatalf("content parts lost: %+v", m.ContentParts)
	}
}

func TestRequestSnapshot_DeclarationParity(t *testing.T) {
	mk := func(name string) tool.Tool {
		return declTool{&tool.Declaration{Name: name, InputSchema: richSchema()}}
	}
	tools := map[string]tool.Tool{
		"zeta":   mk("z"),
		"alpha":  mk("a"),
		"micro":  mk("m"),
		"bravo":  mk("b"),
		"yankee": mk("y"),
	}
	// Map iteration order is randomized, so repeated calls exercise shuffled
	// inputs; the frozen slice must always serialise to identical bytes.
	var first string
	for i := 0; i < 20; i++ {
		snap := NewRequestSnapshot(nil, tools)
		if len(snap.Tools) != len(tools) {
			t.Fatalf("tool count = %d, want %d", len(snap.Tools), len(tools))
		}
		for j := 1; j < len(snap.Tools); j++ {
			if snap.Tools[j-1].RegistryKey > snap.Tools[j].RegistryKey {
				t.Fatalf("tools not sorted by registry key at %d: %v", j, keyOrder(snap.Tools))
			}
		}
		b, err := json.Marshal(snap.Tools)
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("iteration %d produced different bytes:\n%s\n%s", i, first, got)
		}
	}
	want := []string{"alpha", "bravo", "micro", "yankee", "zeta"}
	snap := NewRequestSnapshot(nil, tools)
	for i, w := range want {
		if snap.Tools[i].RegistryKey != w {
			t.Fatalf("order[%d] = %q, want %q (full: %v)", i, snap.Tools[i].RegistryKey, w, keyOrder(snap.Tools))
		}
	}
}

func keyOrder(ts []ToolDeclarationSnapshot) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.RegistryKey
	}
	return out
}

func TestRequestSnapshot_DoesNotInvokeCall(t *testing.T) {
	decl := &tool.Declaration{Name: "n", Description: "d"}
	ct := &callableTool{declTool: declTool{decl}}
	tools := map[string]tool.Tool{"k": ct}

	snap := NewRequestSnapshot(nil, tools)

	if ct.called {
		t.Fatal("snapshot invoked CallableTool.Call")
	}
	if _, ok := tools["k"].(tool.CallableTool); !ok {
		t.Fatal("live tool lost CallableTool capability")
	}
	if snap.Tools[0].Name != "n" || snap.Tools[0].RegistryKey != "k" {
		t.Fatalf("declaration not captured: %+v", snap.Tools[0])
	}
}

func TestRequestSnapshot_EmptyBoundaries(t *testing.T) {
	snap := NewRequestSnapshot(nil, nil)
	if len(snap.Messages) != 0 || len(snap.Tools) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
	b := snap.EstimateBudget("")
	if b.Total != 0 {
		t.Fatalf("empty budget total = %d, want 0", b.Total)
	}
	if len(b.Unknown) != 0 {
		t.Fatalf("empty budget unknown = %v, want none", b.Unknown)
	}
	snap2 := NewRequestSnapshot([]model.Message{}, map[string]tool.Tool{})
	if len(snap2.Messages) != 0 || len(snap2.Tools) != 0 {
		t.Fatal("non-nil empty inputs must still yield empty snapshot")
	}
}

func TestRequestBudget_LongArgumentsCounted(t *testing.T) {
	long := strings.Repeat("x", 2000)
	msgs := []model.Message{
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{Function: model.FunctionDefinitionParam{Name: "n", Arguments: []byte(long)}},
		}},
	}
	snap := NewRequestSnapshot(msgs, nil)
	b := snap.EstimateBudget("")
	if want := PerToolCallOverheadTokens + 1000; b.ToolCallArguments != want {
		t.Fatalf("ToolCallArguments = %d, want %d", b.ToolCallArguments, want)
	}
	if b.Total < b.ToolCallArguments {
		t.Fatal("total must include tool-call arguments")
	}
}

func TestRequestBudget_UnknownMedia(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, ContentParts: []model.ContentPart{
			{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://x/y.png"}},
			{Type: model.ContentTypeAudio, Audio: &model.Audio{Data: []byte{1, 2, 3}}},
		}},
	}
	snap := NewRequestSnapshot(msgs, nil)
	b := snap.EstimateBudget("")
	if b.ContentParts != 0 {
		t.Fatalf("ContentParts should not fabricate media cost, got %d", b.ContentParts)
	}
	if len(b.Unknown) != 2 {
		t.Fatalf("expected 2 unknown media entries, got %d: %v", len(b.Unknown), b.Unknown)
	}
	joined := strings.Join(b.Unknown, "|")
	if !strings.Contains(joined, "image") || !strings.Contains(joined, "audio") {
		t.Fatalf("unknown entries must name the media type, got %v", b.Unknown)
	}
}

func TestRequestBudget_FullInput(t *testing.T) {
	partText := "multimodal text part"
	msgs := []model.Message{
		{Role: model.RoleSystem, Content: "systemprompt"},
		{Role: model.RoleUser, Content: "user question here"},
		{Role: model.RoleAssistant, Content: "assistant reply", ReasoningContent: "reasoning text",
			ToolCalls: []model.ToolCall{
				{Function: model.FunctionDefinitionParam{Name: "search", Arguments: []byte(`{"q":"cat"}`)}},
			},
			ContentParts: []model.ContentPart{{Type: model.ContentTypeText, Text: &partText}}},
	}
	bigProps := map[string]*tool.Schema{}
	for i := 0; i < 40; i++ {
		bigProps[strings.Repeat("p", 10)+string(rune('a'+i))] = &tool.Schema{Type: "string", Description: "field description"}
	}
	tools := map[string]tool.Tool{
		"search": declTool{&tool.Declaration{Name: "search", Description: "web search tool",
			InputSchema: &tool.Schema{Type: "object", Properties: bigProps, Required: []string{"q"}}}},
	}
	snap := NewRequestSnapshot(msgs, tools, WithGenerationConfig(model.GenerationConfig{}))
	b := snap.EstimateBudget("dynamic task board notice text")

	if b.System != 16 {
		t.Fatalf("System = %d, want 16", b.System)
	}
	if b.Messages == 0 {
		t.Fatal("Messages bucket empty")
	}
	if b.Reasoning == 0 {
		t.Fatal("Reasoning bucket empty")
	}
	if b.ContentParts == 0 {
		t.Fatal("ContentParts text not counted")
	}
	if b.Notices == 0 {
		t.Fatal("Notices bucket empty")
	}
	if b.ToolCallArguments < PerToolCallOverheadTokens {
		t.Fatalf("tool-call overhead missing: %d", b.ToolCallArguments)
	}
	if b.ToolDeclarations < 100 {
		t.Fatalf("large schema under-counted: ToolDeclarations = %d", b.ToolDeclarations)
	}
	if b.ProtocolOverhead == 0 {
		t.Fatal("ProtocolOverhead empty")
	}
	if want := b.System + b.Messages + b.ToolDeclarations + b.ToolCallArguments +
		b.Reasoning + b.ContentParts + b.Notices + b.ProtocolOverhead; b.Total != want {
		t.Fatalf("Total = %d, want sum %d", b.Total, want)
	}
}
