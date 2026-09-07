package evals

import (
	"os"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// ==================== suite: ticket-recall（票据可召回率，D4 G 精简骨架）====================
//
// 组件级评估：构造事件 → FormatEventKey（压缩卡片上的召回票据）→ ParseEventKey →
// GetEvent → 断言原文一致。全量 roundtrip（失败即 eval 红）——记忆命脉的量化。

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

// TestSuite_TicketRecall_Roundtrip：压缩后票据全量可召回——构造→票据化→取回→原文一致。
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

// TestSuite_BadCase_HexBrokenKeyRejected：Bad Case 资产（tests/README「静默存活多日」
// 教训）——非 hex 字符/空串 MUST 显式拒绝而非静默空结果。
// 附带守护 0x 前缀容忍特性（ParseEventKey 设计内的 model 回显宽容形式）。
func TestSuite_BadCase_HexBrokenKeyRejected(t *testing.T) {
	for _, broken := range []string{"zzzz-broken", "", "  ", "key:1a2b", "evt_"} {
		_, err := event.ParseEventKey(broken)
		require.Error(t, err, "畸形 key %q 必须显式拒绝（防静默空结果的召回幻觉）", broken)
	}
	// 0x 前缀是设计内容回显形式（合法），作为特性回归。
	v, err := event.ParseEventKey("0xdeadbeef")
	require.NoError(t, err)
	require.Equal(t, int64(0xdeadbeef), v)
}

// TestSuite_ContractPresence（3.1 M1 契约守护）：handoff 四段契约写入子 Agent prompt——
// 存在性断言（漂移即红），漂移守护不需 LLM。
func TestSuite_ContractPresence(t *testing.T) {
	for _, f := range []string{"../resources/prompts/plan_agent.md", "../resources/prompts/knowledge_agent.md"} {
		b, err := os.ReadFile(f)
		require.NoError(t, err, f)
		for _, seg := range []string{"任务", "上下文摘要", "交付物", "验收标准"} {
			require.Contains(t, string(b), seg, "%s 缺 handoff 契约段：%s", f, seg)
		}
	}
}
