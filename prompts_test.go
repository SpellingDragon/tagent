package tagent

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/prompt"
)

// TestEmbeddedPrompts_ResolveViaFallback verifies the framework shared prompts are embedded and resolve through the loader fallback from an empty on-disk dir.
// - That is the mechanism letting examples drop their duplicate copies.
//
// 契约: docs/wiki/prompt/prompt-architecture.md#embedded-fallback
func TestEmbeddedPrompts_ResolveViaFallback(t *testing.T) {
	l := prompt.NewLoader(t.TempDir(), prompt.WithFallback(defaultPromptsFS, DefaultPromptsPrefix))

	shared := []string{
		"action_agent.md", "action_tool_desc.md",
		"exec_tool_desc.md",
		"knowledge_agent.md", "knowledge_tool_desc.md",
		"meditation.md",
		"recall_agent.md", "recall_tool_desc.md",
	}
	for _, f := range shared {
		content, err := l.LoadFromFile(f)
		if err != nil {
			t.Errorf("%s: embedded fallback resolve failed: %v", f, err)
			continue
		}
		if content == "" {
			t.Errorf("%s: embedded content is empty", f)
		}
	}
}

// TestPlanPromptContract locks the invariants the blocks below assert, over the canonical plan prompt and its example copy.
// - 五组断言：幻影工具逃逸口不得出现；双路径基准表须在；A 级收尾走 status 自检且禁止 validate；勾选权属于 update 报账；父级契约表须声明产出物边界与 resume 协议。
// - 副本必须与 canonical 一致：副本存在时 runtime 读的是 example-local 那一份，两处一旦漂移，运行期读到的就不是测所钉的那一份。
func TestPlanPromptContract(t *testing.T) {
	agentPrompt, err := os.ReadFile("resources/prompts/plan_agent.md")
	if err != nil {
		t.Fatalf("read plan_agent.md: %v", err)
	}
	s := string(agentPrompt)

	for _, forbidden := range []string{"不受限", "replace_anchors", "python3 heredoc"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("plan_agent.md must not contain phantom-tool escape hatch %q", forbidden)
		}
	}

	for _, want := range []string{"路径基准", "openspec/changes/my-plan/tasks.md", "changes/my-plan/tasks.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("plan_agent.md must document dual path bases, missing %q", want)
		}
	}

	if !strings.Contains(s, `spec(op="status"`) || !strings.Contains(s, "禁止调用 validate") {
		t.Error("plan_agent.md must route A-level closing to status self-check and forbid validate")
	}

	if !strings.Contains(s, "勾选权属于执行后的 update 报账") {
		t.Error("plan_agent.md must state that check-off right belongs to update accounting")
	}

	toolDesc, err := os.ReadFile("resources/prompts/plan_tool_desc.md")
	if err != nil {
		t.Fatalf("read plan_tool_desc.md: %v", err)
	}
	d := string(toolDesc)

	for _, want := range []string{"产出物边界", "不是工作成果", "resume_task", "各 action 契约", "task_terminal_ttl"} {
		if !strings.Contains(d, want) {
			t.Errorf("plan_tool_desc.md must contain %q (parent-facing contract)", want)
		}
	}

	exampleCopy, err := os.ReadFile("examples/wechat-bot/resources/prompts/plan_agent.md")
	if err == nil && !bytes.Equal(exampleCopy, agentPrompt) {
		t.Error("examples/wechat-bot plan_agent.md copy has drifted from resources/prompts/plan_agent.md — re-sync them")
	}
}
