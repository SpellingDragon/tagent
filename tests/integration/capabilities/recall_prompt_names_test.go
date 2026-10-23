package capabilities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/tool/recall"
)

// recallNamesInOrder is the vocabulary the recall prompt spells, in the order
// the surface declares its four tools.
var recallNamesInOrder = []string{
	recall.QueryToolName,
	recall.GetToolName,
	recall.RecentToolName,
	recall.TraceToolName,
}

// TestRecallPromptNamesMatchTheToolSurface checks that the recall prompt spells exactly the tools the surface declares:
// - normal: every declared name appears in the prompt and in the deployment's tool declaration
// - failure: a name no tool of the surface answers is absent from the prompt
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-agent
func TestRecallPromptNamesMatchTheToolSurface(t *testing.T) {
	prompt := readWechatDeploymentFile(t, filepath.Join("resources", "prompts", "recall_agent.md"))
	declared := readWechatDeploymentFile(t, "tagent.yaml")
	surface := recall.New(recall.Reads{}, nil)
	if len(surface) != len(recallNamesInOrder) {
		t.Fatalf("the recall surface declares %d tools, want %d", len(surface), len(recallNamesInOrder))
	}
	for index, tool := range surface {
		name := tool.Declaration().Name
		if name != recallNamesInOrder[index] {
			t.Fatalf("recall tool %d is declared %q, want %q", index, name, recallNamesInOrder[index])
		}
		if !strings.Contains(prompt, "`"+name+"`") {
			t.Fatalf("the recall prompt never spells %q, so the model cannot call the tool under the name it is declared with", name)
		}
		if !strings.Contains(declared, "id: "+name) {
			t.Fatalf("the wechat deployment declares no tool named %q, so an agent declaring it is assembled without it", name)
		}
	}
	for _, stale := range []string{"memory_query", "memory_get", "memory_recent", "memory_trace"} {
		if strings.Contains(prompt, stale) {
			t.Fatalf("the recall prompt still spells %q, which no tool of the surface answers", stale)
		}
	}
}

// readWechatDeploymentFile reads one file of the wechat deployment example, so
// the assertion above reads the deployment the recall prompt ships with.
func readWechatDeploymentFile(t *testing.T, relative string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "wechat-bot", relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(body)
}
