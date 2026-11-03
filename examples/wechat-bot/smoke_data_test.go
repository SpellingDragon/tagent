//go:build smoke

// smoke_data_test.go 是第二波冒烟的数据面：一条链路核对多轮对话落在会话链上的
// 事实如何被 SFT 导出配对，另一条链路核对 call_subagent 的显式源在宿主文档里
// 被授予后如何走完运行时装配：受理、worker 回合、结算通知带结果正文、投递。
//
// 运行: go test -tags smoke ./... -run TestLocalSmokeWave2Data -count=1
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
)

// smokeDataRounds 是导出对账驱动的轮数：落在 5-10 轮的窗口里，每轮一个唯一标记。
const smokeDataRounds = 6

// smokeChainTurn 是导出配对的一个单位：一个段呈现的输入与回答它的那次 assistant 输出。
type smokeChainTurn struct {
	input string
	label string
}

// smokeChainAppend 把一段正文接到配对的一侧：空正文不占一侧，也不添空行。
func smokeChainAppend(acc, content string) string {
	if content == "" {
		return acc
	}
	if acc == "" {
		return content
	}
	return acc + "\n" + content
}

// TestLocalSmokeWave2Data 是第二波数据面入口。
// - chain export: the formal Run answers rl.FactReader and every row pairs one round
// - call_subagent: the document grants the tool and one call runs the assembly end to end
// - chain export: an unnamed session exports no samples and no error
func TestLocalSmokeWave2Data(t *testing.T) {
	t.Run("chain exporter reconciles the user rounds", func(t *testing.T) {
		cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
		cfg.Doc = func(c *smokeConfig) string { return smokeDataDocument(c, nil, false) }
		provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
		cfg.Endpoint = provider.endpoint()
		cfg.write(t)
		rig := bootSmokeRig(t, cfg, provider)

		chat := smokeChats[0]
		markers := make([]string, 0, smokeDataRounds)
		for round := 1; round <= smokeDataRounds; round++ {
			marker := fmt.Sprintf("SMOKE-R%d", 3000+round)
			markers = append(markers, marker)
			rig.drive(chat, marker+" plain question")
			rig.waitText(chat, "answer "+marker, 30*time.Second)
		}
		addr := rig.addressOf(chat)

		inputs := map[event.InvocationID]string{}
		outputs := map[event.InvocationID]string{}
		turns := make([]smokeChainTurn, 0, len(markers))
		open := -1
		for _, fact := range rig.sessionFacts(chat) {
			switch fact.Type {
			case event.TypeExternalInput:
				inputs[fact.InvocationID] = fact.Message.Content
				if open < 0 {
					turns = append(turns, smokeChainTurn{})
					open = len(turns) - 1
				}
				turns[open].input = smokeChainAppend(turns[open].input, fact.Message.Content)
			case event.TypeAgentOutput:
				outputs[fact.InvocationID] = fact.Message.Content
				if open < 0 {
					turns = append(turns, smokeChainTurn{})
					open = len(turns) - 1
				}
				turns[open].label = smokeChainAppend(turns[open].label, fact.Message.Content)
				open = -1
			}
		}
		if len(turns) != len(markers) {
			t.Fatalf("the chain closed %d turn(s) on %s, want %d", len(turns), addr.SessionID, len(markers))
		}
		for i, marker := range markers {
			if want := marker + " plain question"; turns[i].input != want {
				t.Fatalf("round %d input = %q, want the round's own question %q", i+1, turns[i].input, want)
			}
			if want := "answer " + marker; turns[i].label != want {
				t.Fatalf("round %d output = %q, want %q", i+1, turns[i].label, want)
			}
		}
		t.Logf("[wave2-data] chain turns: %d external inputs over %d invocation(s), %d agent outputs",
			len(inputs), len(inputs), len(outputs))

		// 导出面可达：正式 Run 直接满足 FactReader，导出按链序配对（输入开口、回答闭口）。
		var reader rl.FactReader = rig.host.Run()
		exporter := rl.NewChainExporter(reader)
		samples, err := exporter.Export(context.Background(), rl.ExportOptions{
			SessionIDs: []event.SessionID{addr.SessionID},
		})
		if err != nil {
			t.Fatalf("export the live chain: %v", err)
		}
		if len(samples) != len(markers) {
			t.Fatalf("exported %d samples, want %d: %+v", len(samples), len(markers), samples)
		}
		for i, sample := range samples {
			marker := markers[i]
			if sample.SessionID != string(addr.SessionID) {
				t.Fatalf("sample %d session = %q, want %q", i, sample.SessionID, addr.SessionID)
			}
			if sample.Turn != i+1 {
				t.Fatalf("sample %d turn = %d, want %d", i, sample.Turn, i+1)
			}
			if sample.Input != marker+" plain question" {
				t.Fatalf("sample %d input = %q, want the round's own question", i, sample.Input)
			}
			if sample.Label != "answer "+marker {
				t.Fatalf("sample %d label = %q, want the round's own answer", i, sample.Label)
			}
			if sample.EpisodeID != "" {
				t.Fatalf("sample %d episode = %q, want empty: the wechat Drive path names no episode", i, sample.EpisodeID)
			}
		}

		unread, err := exporter.Export(context.Background(), rl.ExportOptions{
			SessionIDs: []event.SessionID{"smoke-data-unknown"},
		})
		if err != nil {
			t.Fatalf("export an unknown session: %v", err)
		}
		if len(unread) != 0 {
			t.Fatalf("unknown session exported %d samples, want none: %+v", len(unread), unread)
		}

		t.Logf("[wave2-data] negative anchor: unknown session exported %d samples", len(unread))
		t.Logf("[wave2-data] %d rounds landed on %s and exported %d paired rows",
			len(markers), addr.SessionID, len(samples))
		t.Logf("[wave2-data] turn range = %d..%d, episode = %q",
			samples[0].Turn, samples[len(samples)-1].Turn, samples[0].EpisodeID)
		t.Logf("[wave2-data] GAP(episode): the wechat Drive path names no Episode, so EpisodeID is empty and Turn counts the whole session")
	})

	t.Run("call_subagent explicit source runs through the runtime assembly", func(t *testing.T) {
		marker := "SMOKE-R3200"
		cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
		cfg.Doc = func(c *smokeConfig) string {
			return smokeDataDocument(c, []string{"{ kind: tool, id: call_subagent }"}, true)
		}
		provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
		cfg.Endpoint = provider.endpoint()
		cfg.write(t)

		callID := "call-delegate-" + marker
		args := `{"name":"smoke-delegate","prompt":"produce ` + marker + ` artifact","model":"` + smokeWorkerModel + `","input":"produce ` + marker + ` artifact"}`
		provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
			if modelName != smokeEntryModel {
				return false
			}
			text := smokeModelRequest{Model: modelName, Messages: messages}.text()
			switch {
			case strings.Contains(text, "tagent: task") && strings.Contains(text, "settled"):
				fmt.Fprint(w, smokeAnswerBody("settle-ack "+marker))
				return true
			case smokeEndedWithTool(messages):
				fmt.Fprint(w, smokeAnswerBody("delegate ack "+marker))
				return true
			}
			fmt.Fprint(w, smokeToolCallBody(callID, "call_subagent", args))
			return true
		}

		rig := bootSmokeRig(t, cfg, provider)
		chat := smokeChats[2]
		rig.drive(chat, marker+" smoke subagent")
		rig.waitText(chat, "settle-ack "+marker, 120*time.Second)

		var receipt string
		for _, req := range provider.recorded() {
			if req.Model != smokeEntryModel {
				continue
			}
			for _, msg := range req.Messages {
				if msg.Role == string(tagentmodel.RoleTool) && msg.ToolCallID == callID {
					receipt = msg.Content
				}
			}
		}
		if receipt != "ok" {
			t.Fatalf("call_subagent receipt = %q, want the minimal ok admission confirmation", receipt)
		}
		if strings.Contains(receipt, "[call_subagent_denied]") {
			t.Fatalf("call_subagent receipt = %q, want the admission, not a named refusal", receipt)
		}
		if got := smokeRequestsFor(provider, smokeWorkerModel); got == 0 {
			t.Fatal("the explicit source ran no worker model request")
		}
		if !smokeSettleRequestSeen(provider, marker) {
			t.Fatalf("no settle follow-up read the worker body %q", "worker result "+marker)
		}

		t.Logf("[wave2-data] call_subagent receipt = %q", receipt)
		t.Logf("[wave2-data] worker requests=%d, settle notice carried %q, delivery = %q",
			smokeRequestsFor(provider, smokeWorkerModel), "worker result "+marker, "settle-ack "+marker)
	})

}

// smokeDataDocument 渲染数据面的最小宿主文档：一个入口 agent、可选工具面、
// 本地 provider 与 localfile 存储。它不启用冥想，也不声明预置子 agent，
// 所以会话链里只会有本轮自己提交的事实。worker 开关为 call_subagent 的显式源
// 声明一个同名模型的 agent，让运行时装配按名字解析到 scripted worker 模型。
func smokeDataDocument(c *smokeConfig, tools []string, worker bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "entry: %s\n", smokeSessionTag)
	b.WriteString("provider: local\n")
	fmt.Fprintf(&b, "model: %s\n", c.Model)
	fmt.Fprintf(&b, "prompt_dir: %s\n", c.PromptDir)
	b.WriteString("request_timeout_seconds: 60\n")
	fmt.Fprintf(&b, "providers:\n  local: { provider: openai, api_endpoint: %q, api_key_env: SMOKE_API_KEY }\n", c.Endpoint)
	fmt.Fprintf(&b, "agents:\n  %s:\n    model: %s\n", smokeSessionTag, c.Model)
	if len(tools) > 0 {
		b.WriteString("    tools:\n")
		for _, tool := range tools {
			fmt.Fprintf(&b, "      - %s\n", tool)
		}
	}
	b.WriteString("    max_tokens: 2000\n")
	b.WriteString("    compress_threshold: 0.3\n")
	b.WriteString("    keep_recent_tasks: 1\n")
	b.WriteString("    max_tool_iterations: 3\n")
	if worker {
		fmt.Fprintf(&b, "  %s:\n    model: %s\n", smokeWorkerModel, smokeWorkerModel)
	}
	fmt.Fprintf(&b, "storage: { type: localfile, root: %s }\n", c.StorageRoot)
	b.WriteString("app:\n  wechat:\n")
	fmt.Fprintf(&b, "    config_dir: %s\n", c.ConfigDir)
	fmt.Fprintf(&b, "    token_file: %s\n", filepath.Join(c.ConfigDir, "token.json"))
	fmt.Fprintf(&b, "    context_token_dir: %s\n", c.ContextDir)
	fmt.Fprintf(&b, "    workspace_dir: %s\n", c.WorkspaceDir)
	return b.String()
}
