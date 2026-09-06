package evolution

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestStoreEvidenceSource_ActivationWindowStart 是 W4（§8.3）回归：Collect 以 bundle 激活时刻
// 为证据窗口起点——激活前的事件被 cutoff 排除。否则 CanaryHold=0「激活即评估」时固定回看窗
// （默认 10m）全是旧 bundle 数据，judge 对新激活 bundle 无判别力，"劣化即回滚"形同虚设。
func TestStoreEvidenceSource_ActivationWindowStart(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := 1
	src := NewStoreEvidenceSource(store, pid, 10*time.Minute)
	actLog := NewActivationLog()
	src.SetActivationLog(actLog)

	now := time.Now().UnixMilli()
	bundleID := "bundle-w4"
	actLog.Record(bundleID, now) // 激活时刻 = now

	// 激活前事件（timestamp = now-60s）——有激活记录时应被窗口排除。
	oldKey := memory.NewSnowflakeEventKey(pid, now-60000)
	if err := store.StoreEvent(oldKey, memory.FullEvent{
		EventKey: oldKey, PartitionID: pid, EventType: event.TypeExternalInput, Timestamp: now - 60000,
	}); err != nil {
		t.Fatalf("store old event: %v", err)
	}

	ev, err := src.Collect(context.Background(), bundleID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ev.TurnCount != 0 {
		t.Fatalf("W4: 激活前事件不应计入窗口(cutoff=激活时刻), got TurnCount=%d", ev.TurnCount)
	}
	// WindowMs 应≈0（激活即评估），而非固定 10m=600000。
	if ev.WindowMs > 5000 {
		t.Fatalf("W4: 窗口应为激活时至今(≈0), got WindowMs=%d", ev.WindowMs)
	}

	// 对照：未记录激活时刻的 bundle → 回退固定 10m 窗，60s 前事件计入（验证 Since 未命中分支）。
	ev2, err := src.Collect(context.Background(), "bundle-unknown")
	if err != nil {
		t.Fatalf("Collect unknown: %v", err)
	}
	if ev2.TurnCount != 1 {
		t.Fatalf("W4: 无激活记录应回退固定窗(含 60s 前事件), got TurnCount=%d", ev2.TurnCount)
	}
}
