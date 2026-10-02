// 本文件负责拒绝账本的存储绑定时序：迟绑定可用、未绑定时 nil 安全——拒绝必须留下可核查
// 记录，而不是 panic 或空返回。
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
package governance

import (
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestDenialLedger_BindStoreDeferred 钉住 N2回归：DenialLedger 先以 nil store 创建（纯。
func TestDenialLedger_BindStoreDeferred(t *testing.T) {
	shared := NewDenialLedger(nil, 0)
	shared.Record(DenialRecord{Subtype: event.SubtypeDenial, ToolName: "exec", Reason: "pre-bind"})

	store := memory.NewInMemoryStore()
	pid := 1
	shared.BindStore(store, pid)
	shared.Record(DenialRecord{Subtype: event.SubtypeDenial, ToolName: "exec", Reason: "post-bind"})

	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{pid}, EventTypes: []string{event.TypeGovernance},
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("N2: BindStore 后 Record 应写 governance 事件到 store（durable 审计，非内存易失）")
	}

	reloaded := NewDenialLedger(store, pid)
	if len(reloaded.Query(100)) == 0 {
		t.Fatal("N2: 重启后 Ledger 应从 store 重建治理记录（子 agent 审计 durable，重启可 recall）")
	}
}

// TestDenialLedger_BindStoreNilSafe 验证 BindStore 的 nil 安全（nil receiver / nil store 不 panic）。
func TestDenialLedger_BindStoreNilSafe(t *testing.T) {
	var nilLedger *DenialLedger
	nilLedger.BindStore(memory.NewInMemoryStore(), 1)
	l := NewDenialLedger(nil, 0)
	l.BindStore(nil, 1)
}
