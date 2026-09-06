package governance

import (
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestDenialLedger_BindStoreDeferred 是 N2（§8.9）回归：DenialLedger 先以 nil store 创建（纯
// 内存），后 BindStore 绑定持久 store——绑定后 Record 写 governance 事件（durable），重启可从
// store 重建。模拟子 agent gate 先构造（entry memStore 未就绪）、entry 后绑定共享账本的时序，
// 修复 W3 子 agent gate 兜底内存账本致治理审计重启即失。
func TestDenialLedger_BindStoreDeferred(t *testing.T) {
	// 共享 Ledger 先创建（nil store，子 agent 构造期，entry memStore 未就绪）。
	shared := NewDenialLedger(nil, 0)
	// 绑定前 Record 只入内存（store nil，不写事件）。
	shared.Record(DenialRecord{Subtype: event.SubtypeDenial, ToolName: "exec", Reason: "pre-bind"})

	store := memory.NewInMemoryStore()
	pid := 1
	// entry memStore 就绪 → 延迟绑定共享账本。
	shared.BindStore(store, pid)
	// 绑定后 Record 写 governance 事件（durable）。
	shared.Record(DenialRecord{Subtype: event.SubtypeDenial, ToolName: "exec", Reason: "post-bind"})

	// 验证事件写入 store（子 agent 治理记录经共享账本落 entry governance 分区）。
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{pid}, EventTypes: []string{event.TypeGovernance},
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("N2: BindStore 后 Record 应写 governance 事件到 store（durable 审计，非内存易失）")
	}

	// 重启：新 Ledger 从 store rebuild → 恢复治理记录（durable）。
	reloaded := NewDenialLedger(store, pid)
	if len(reloaded.Query(100)) == 0 {
		t.Fatal("N2: 重启后 Ledger 应从 store 重建治理记录（子 agent 审计 durable，重启可 recall）")
	}
}

// TestDenialLedger_BindStoreNilSafe 验证 BindStore 的 nil 安全（nil receiver / nil store 不 panic）。
func TestDenialLedger_BindStoreNilSafe(t *testing.T) {
	var nilLedger *DenialLedger
	nilLedger.BindStore(memory.NewInMemoryStore(), 1) // 不 panic
	l := NewDenialLedger(nil, 0)
	l.BindStore(nil, 1) // nil store 不绑定、不 panic
}
