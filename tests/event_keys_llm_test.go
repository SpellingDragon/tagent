package tagent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/testutil"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestRealLLM_ModelCopiesHexEventKeys answers the live-behavior half of the
// event_keys question: given a timeline rendered with [evt_HEX|type] prefixes
// and a tool whose schema asks for hex-string event keys, does a real model
// actually copy the RIGHT keys into the tool call?
//
// (The engineering half — parse/resolve round-trip — is locked by
// agent/event_keys_contract_test.go; this test covers the model-side seam
// that no unit test can.)
func TestRealLLM_ModelCopiesHexEventKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("real-LLM test; skipped in -short")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("LoadConfig: %v", err)
	}

	k1, k2, k3 := int64(0x1201aa10000001), int64(0x1201aa10000002), int64(0x1201aa10000003)
	timeline := tagentevent.FormatEventPrefix(k1, "external_input") + " 用户: 帮我部署 v2 到测试环境\n" +
		tagentevent.FormatEventPrefix(k2, "agent_output") + " 部署完成,健康检查通过\n" +
		tagentevent.FormatEventPrefix(k3, "external_input") + " 用户: 今天天气怎么样"

	decl := &tool.Declaration{
		Name:        "analyze",
		Description: "分析历史事件。传入相关事件的 event_keys（canonical hex 字符串,与时间线 [evt_...] 前缀内的 key 完全一致）。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"request":    {Type: "string"},
				"event_keys": {Type: "array", Description: "相关事件的 hex key,从 [evt_KEY|type] 前缀中原样复制", Items: &tool.Schema{Type: "string"}},
			},
			Required: []string{"request", "event_keys"},
		},
	}

	m := openai.New(cfg.ModelName, openai.WithAPIKey(cfg.APIKey), openai.WithBaseURL(cfg.Endpoint))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个助手。历史时间线中每行以 [evt_KEY|type] 前缀标识事件。调用工具时按工具说明传参。"),
			model.NewUserMessage("以下是历史时间线:\n" + timeline + "\n\n请调用 analyze 工具分析【与部署相关】的事件（只选相关的）。"),
		},
		Tools: map[string]tool.Tool{"analyze": declOnlyTool{decl}},
	}

	respCh, err := m.GenerateContent(ctx, req)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	var args []byte
	for resp := range respCh {
		if resp.Error != nil {
			t.Fatalf("model error: %v", resp.Error)
		}
		for _, c := range resp.Choices {
			for _, tc := range c.Message.ToolCalls {
				if tc.Function.Name == "analyze" && len(tc.Function.Arguments) > 0 {
					args = tc.Function.Arguments
				}
			}
		}
	}
	if len(args) == 0 {
		t.Fatalf("model did not call the analyze tool")
	}
	t.Logf("model tool args: %s", args)

	var parsed struct {
		EventKeys []string `json:"event_keys"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatalf("unmarshal tool args: %v (args=%s)", err, args)
	}

	got := map[int64]bool{}
	for _, ks := range parsed.EventKeys {
		k, err := tagentevent.ParseEventKey(trimEvt(ks))
		if err != nil {
			t.Errorf("model emitted unparseable key %q: %v", ks, err)
			continue
		}
		got[k] = true
	}
	// Deployment-related keys must be selected; the weather key must not.
	if !got[k1] || !got[k2] {
		t.Errorf("model must copy the deployment keys %s,%s; got %v (raw=%v)",
			tagentevent.FormatEventKey(k1), tagentevent.FormatEventKey(k2), got, parsed.EventKeys)
	}
	if got[k3] {
		t.Errorf("model selected the irrelevant weather event %s — selection quality regression", tagentevent.FormatEventKey(k3))
	}
}

func trimEvt(s string) string {
	if len(s) > 4 && s[:4] == "evt_" {
		return s[4:]
	}
	return s
}

// declOnlyTool exposes a Declaration without an implementation (the model
// only needs the schema to produce a tool call).
type declOnlyTool struct{ d *tool.Declaration }

func (t declOnlyTool) Declaration() *tool.Declaration { return t.d }
