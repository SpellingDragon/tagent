package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// busCap 是 EventBus channel 容量（与 NewEventBus 一致）。
const busCap = 256

func publishN(bus *EventBus, n int, source string) {
	for i := 0; i < n; i++ {
		bus.Publish(NewExternalInputEvent(source, model.Message{Role: model.RoleUser, Content: fmt.Sprintf("c%d", i)}))
	}
}

// TestEventBus_DefaultVolatile 验证向后兼容：NewEventBus 与空 dir 均为轻量
// volatile 模式（无 durable inbox，行为与旧纯 channel 一致）。
func TestEventBus_DefaultVolatile(t *testing.T) {
	if NewEventBus().inbox != nil {
		t.Fatal("NewEventBus 应无 durable inbox（向后兼容轻量模式）")
	}
	bus, err := NewReliableEventBus("")
	if err != nil || bus.inbox != nil {
		t.Fatal("空 spillDir 应为纯 volatile 模式")
	}
}

// TestReliableEventBus_AllInputsDurableNoDrop 验证核心可靠性（3.2）：可靠模式
// 下所有输入先持久化（不再只溢出部分），Pull 按 seq 严格序 claim 全部，不丢。
func TestReliableEventBus_AllInputsDurableNoDrop(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	total := 8 // 远小于 channel 容量：durable 模式连低负载输入也先落盘
	publishN(bus, total, "user")
	if got := bus.DurablePending(); got != int64(total) {
		t.Fatalf("全部输入应先持久化, pending=%d want %d", got, total)
	}

	got := 0
	for got < total {
		batch, err := bus.Pull(context.Background())
		if err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if len(batch) == 0 {
			t.Fatal("仍有 pending 却 Pull 为空")
		}
		for _, e := range batch {
			if _, ok := e.Metadata["inbox_path"]; !ok {
				t.Fatal("durable 事件必须携带 inbox 溯源元数据")
			}
			got++
		}
		for _, pr := range bus.DurableProvenance(batch) {
			if err := bus.ConfirmDurable(pr[0]); err != nil {
				t.Fatalf("confirm: %v", err)
			}
		} // receipt+ack：处理完成的输入确认
	}
	if bus.DurablePending() != 0 {
		t.Fatalf("确认后应清空, pending=%d", bus.DurablePending())
	}
}

// TestReliableEventBus_DurableOrderPreserved 验证 durable 输入按接收序严格消费。
func TestReliableEventBus_DurableOrderPreserved(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	total := 5
	publishN(bus, total, "meditation")

	batch, err := bus.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(batch) != total {
		t.Fatalf("单次 Pull 应取回全部 %d（maxClaim=32）, got %d", total, len(batch))
	}
	for i, e := range batch {
		want := fmt.Sprintf("c%d", i)
		if e.Message == nil || e.Message.Content != want {
			t.Fatalf("durable 顺序破坏: idx=%d want %q got %+v", i, want, e.Message)
		}
		if e.Source != "meditation" {
			t.Fatalf("source 应保真, got %q", e.Source)
		}
	}
}

// TestReliableEventBus_RecoverAfterReopen 验证重启恢复：已 durable 未确认的
// 输入，新 bus 同 dir 可按序回收（常驻不丢，跨重启）。
func TestReliableEventBus_RecoverAfterReopen(t *testing.T) {
	dir := t.TempDir()
	bus1, err := NewReliableEventBus(dir)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	publishN(bus1, 3, "user")
	if bus1.DurablePending() != 3 {
		t.Fatalf("应 3 pending, got %d", bus1.DurablePending())
	}
	// 模拟重启：新 bus 同 dir（channel 全新为空，inbox 仍在磁盘）。
	bus2, err := NewReliableEventBus(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	batch, err := bus2.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(batch) != 3 {
		t.Fatalf("重启后应回收 3 项, got %d", len(batch))
	}
}

// TestReliableEventBus_LegacySpillRefused 验证旧 .spill 未排空时拒绝启动
// （fail-loud 迁移指引，绝不静默忽略未完成消息）。
func TestReliableEventBus_LegacySpillRefused(t *testing.T) {
	dir := t.TempDir()
	if err := osWriteFile(dir+"/00000000000000000009.spill", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	_, err := NewReliableEventBus(dir)
	if !errors.Is(err, reliability.ErrLegacySpillNotDrained) {
		t.Fatalf("want ErrLegacySpillNotDrained, got %v", err)
	}
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// TestEventBus_VolatileTimeoutRejected 验证 volatile 模式的可判定拒绝：队列满
// 超时后 PublishContext 返回错误（不再「丢弃即成功」），旧 void 入口计数可见。
func TestEventBus_VolatileTimeoutRejected(t *testing.T) {
	bus := NewEventBus()
	bus.ch = make(chan *AgentEvent, 1) // 缩容便于构造满队列
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "fill"}))
	evt := NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "x"})
	if _, err := bus.PublishContext(context.Background(), evt); !errors.Is(err, ErrPublishTimeout) {
		t.Fatalf("满队列必须返回背压错误, got %v", err)
	}
	bus.Publish(evt) // 旧 void 入口：只计数不 panic
	if bus.PublishDropped() != 2 {
		t.Fatalf("PublishContext + void 各计一次拒绝, got %d", bus.PublishDropped())
	}
}
