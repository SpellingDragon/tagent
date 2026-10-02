package evals

import (
	"context"
	"os"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

func seedEvents(t *testing.T, store memory.MemoryStore, pid, n int) []string {
	t.Helper()
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		key := memory.NewSnowflakeEventKey(pid, 0)
		content := "原始事件内容-" + string(rune('A'+i)) + "：部署细节/报错堆栈/用户偏好等全文"
		require.NoError(t, store.StoreEvent(key, memory.FullEvent{
			EventKey:     key,
			PartitionID:  pid,
			EventType:    event.TypeExternalInput,
			EventSummary: "seed event " + string(rune('A'+i)),
			Content:      content,
			Timestamp:    1700000000000 + int64(i),
		}))
		keys = append(keys, event.FormatEventKey(key))
	}
	return keys
}

// TestSuite_TicketRecall_Roundtrip 钉住票据零幻觉召回：构造→票据化→取回→原文一致（本文件是四个套件的执行体）。
//
// 契约: docs/wiki/platform/evaluation-suites.md
func TestSuite_TicketRecall_Roundtrip(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")
	const n = 12
	keys := seedEvents(t, store, pid, n)

	for _, ticket := range keys {
		key, err := event.ParseEventKey(ticket)
		require.NoError(t, err, "票据 %s 必须可解析", ticket)
		full, err := store.GetEvent(key)
		require.NoError(t, err, "票据 %s 必须可召回（零幻觉契约）", ticket)
		require.NotNil(t, full)
		require.Contains(t, full.Content, "原始事件内容", "原文必须完整在场")
	}
}

// TestSuite_BadCase_HexBrokenKeyRejected 钉住畸形票据必须显式拒绝，且 0x 前缀作为设计内宽容形式仍被解析。
func TestSuite_BadCase_HexBrokenKeyRejected(t *testing.T) {
	for _, broken := range []string{"zzzz-broken", "", "  ", "key:1a2b", "evt_"} {
		_, err := event.ParseEventKey(broken)
		require.Error(t, err, "畸形 key %q 必须显式拒绝（防静默空结果的召回幻觉）", broken)
	}
	v, err := event.ParseEventKey("0xdeadbeef")
	require.NoError(t, err)
	require.Equal(t, int64(0xdeadbeef), v)
}

// TestSuite_ToolChoice_OpWhitelist 钉住未知 op 一律显式拒绝（工具面路由契约，漂移即红）。
func TestSuite_ToolChoice_OpWhitelist(t *testing.T) {
	g := evolution.NewGitEvolution(evolution.GitEvolutionConfig{WorkDir: t.TempDir()})
	refineTool := evolution.NewRefineTool(g)
	ct, ok := refineTool.(interface {
		Call(ctx context.Context, args []byte) (any, error)
	})
	require.True(t, ok, "refine 工具必须可调用（CallableTool 契约）")
	ctx := context.Background()

	_, err := ct.Call(ctx, []byte(`{"op":"status"}`))
	require.Error(t, err, "非 git 仓下 status 应显式报错（环境如实呈现）")

	_, err = ct.Call(ctx, []byte(`{"op":"activate"}`))
	require.Error(t, err, "未白名单 op 必须拒绝（propose/activate 已随发布道退役）")
	require.Contains(t, err.Error(), "白名单")
}

// TestSuite_ContractPresence 钉住子 agent 提示词里 handoff 四段契约的存在性（不需 LLM 即可判红）。
func TestSuite_ContractPresence(t *testing.T) {
	for _, f := range []string{"../resources/prompts/plan_agent.md", "../resources/prompts/knowledge_agent.md"} {
		b, err := os.ReadFile(f)
		require.NoError(t, err, f)
		for _, seg := range []string{"任务", "上下文摘要", "交付物", "验收标准"} {
			require.Contains(t, string(b), seg, "%s 缺 handoff 契约段：%s", f, seg)
		}
	}
}
