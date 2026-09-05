package agent

import (
	"context"
	"fmt"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// busCap 是 EventBus channel 容量（与 NewEventBus 一致）。
const busCap = 256

func publishN(bus *EventBus, n int, source string) {
	for i := 0; i < n; i++ {
		bus.Publish(NewExternalInputEvent(source, model.Message{Role: model.RoleUser, Content: fmt.Sprintf("c%d", i)}))
	}
}

// TestEventBus_DefaultNoSpill 验证向后兼容：NewEventBus 无 spill（现状纯 channel）。
func TestEventBus_DefaultNoSpill(t *testing.T) {
	if NewEventBus().spill != nil {
		t.Fatal("NewEventBus 应无 spill（向后兼容，现状纯 channel）")
	}
	// 空 dir 的 NewReliableEventBus 也回退无 spill。
	if NewReliableEventBus("").spill != nil {
		t.Fatal("空 spillDir 应回退纯 channel bus")
	}
}

// TestReliableEventBus_SpillOnFullNoDrop 验证核心可靠性：channel 满时溢出落盘而非丢弃，
// Pull 回收全部（at-least-once，不丢事件）。
func TestReliableEventBus_SpillOnFullNoDrop(t *testing.T) {
	bus := NewReliableEventBus(t.TempDir())
	if bus.spill == nil {
		t.Fatal("NewReliableEventBus(dir) 应启用 spill")
	}
	total := busCap + 4
	publishN(bus, total, "user")

	if bus.spill.Len() < 1 {
		t.Fatalf("channel 满(%d)后应溢出落盘, got spill=%d", busCap, bus.spill.Len())
	}
	// Pull 回收：溢出 + channel 全部，不丢。
	batch, err := bus.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	got := len(batch)
	// 排空剩余（TryPull）。
	for {
		b := bus.TryPull()
		if len(b) == 0 {
			break
		}
		got += len(b)
	}
	if got != total {
		t.Fatalf("应回收全部 %d 事件（不丢）, got %d", total, got)
	}
	if bus.spill.Len() != 0 {
		t.Fatalf("回收后溢出应清空, got %d", bus.spill.Len())
	}
}

// TestReliableEventBus_SpillPreservesEvent 验证溢出事件序列化/反序列化保真（内容不丢）。
func TestReliableEventBus_SpillPreservesEvent(t *testing.T) {
	bus := NewReliableEventBus(t.TempDir())
	total := busCap + 1
	publishN(bus, total, "meditation")
	if bus.spill.Len() != 1 {
		t.Fatalf("应恰 1 溢出, got %d", bus.spill.Len())
	}
	batch, _ := bus.Pull(context.Background())
	seen := make(map[string]bool, len(batch))
	for _, e := range batch {
		if e.Message != nil {
			seen[e.Message.Content] = true
		}
		if e.Source != "meditation" {
			t.Fatalf("溢出/回收后 source 应保真, got %q", e.Source)
		}
	}
	for i := 0; i < total; i++ {
		if !seen[fmt.Sprintf("c%d", i)] {
			t.Fatalf("事件 c%d 丢失（溢出序列化未保真）", i)
		}
	}
}

// TestReliableEventBus_RecoverAfterReopen 验证重启恢复：溢出的未消费项落盘后，新 bus 同 dir
// 可回收（常驻不丢，跨重启）。
func TestReliableEventBus_RecoverAfterReopen(t *testing.T) {
	dir := t.TempDir()
	bus1 := NewReliableEventBus(dir)
	publishN(bus1, busCap+2, "user") // 2 溢出落盘
	if bus1.spill.Len() != 2 {
		t.Fatalf("应 2 溢出, got %d", bus1.spill.Len())
	}
	// 模拟重启：新 bus 同 dir（channel 全新为空，溢出项仍在磁盘）。
	bus2 := NewReliableEventBus(dir)
	batch, err := bus2.Pull(context.Background()) // spill 有项 → 不阻塞，回收
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("重启后应回收 2 溢出项, got %d", len(batch))
	}
	if bus2.spill.Len() != 0 {
		t.Fatal("回收后溢出应清空")
	}
}
