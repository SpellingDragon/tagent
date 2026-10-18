package history

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// failingStore wraps one lifecycle store and fails every enumeration, so the
// reclaimer can be proven to never reconcile a partial view.
type failingStore struct {
	memory.LifecycleStore
	err error
}

func (s failingStore) EnumerateKeys(ctx context.Context) ([]memory.StoredKey, error) {
	return nil, s.err
}

// TestRecoveryBeforeReclamation proves the retention leases, the tombstone-only expiry and the reclamation coordination:
// - 重启登记先于淘汰: the restart registration runs before any eviction
// - 未确认材料越过 TTL 仍保留且释放后按原时间戳复归类: protected material survives its TTL and reclassifies under its original timestamp once released
// - 显式删除撞保护返回 protected: an explicit delete on a protected key is refused
// - 单 agent 关闭不释放共享保护: one holder closing never releases a shared protection
// - 枚举失败不核对残缺视图: a failed enumeration reconciles nothing
// - 持久档缺能力 fail-closed: the durable tier missing coordination refuses to start
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestRecoveryBeforeReclamation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	newDurable := func(t *testing.T, cfg memory.LifecycleConfig, caps memory.LifecycleCapabilities) (*memory.Lifecycle, *memory.Retention, *memory.MemoryLifecycleStore) {
		t.Helper()
		retention := memory.NewRetention(memory.NewMemoryGuard())
		store := memory.NewMemoryLifecycleStore(caps)
		life, err := memory.NewLifecycle(cfg, retention, store, caps)
		if err != nil {
			t.Fatalf("NewLifecycle: %v", err)
		}
		return life, retention, store
	}
	durableCaps := memory.LifecycleCapabilities{DurableRecovery: true, Protection: true, Reclamation: true}
	ready := func(t *testing.T, life *memory.Lifecycle) {
		t.Helper()
		life.ArmGrace(now)
		life.RecoveryComplete()
	}

	t.Run("G2.1 重启登记先于淘汰: the restart registration precedes every eviction", func(t *testing.T) {
		life, retention, store := newDurable(t, memory.LifecycleConfig{GlobalTTLDays: 1}, durableCaps)
		store.Put(memory.StoredKey{Key: "expired-1", Type: event.TypeAgentOutput, Partition: 1, RecordedAt: now.Add(-10 * 24 * time.Hour)})
		store.Put(memory.StoredKey{Key: "material-1", Type: event.TypeExternalInput, Partition: 1, RecordedAt: now.Add(-10 * 24 * time.Hour)})

		if _, err := life.Scan(ctx, now); !errors.Is(err, memory.ErrRecoveryIncomplete) {
			t.Fatalf("scan before arming = %v, want ErrRecoveryIncomplete", err)
		}
		life.ArmGrace(now.Add(-time.Minute))
		if _, err := life.Scan(ctx, now); !errors.Is(err, memory.ErrRecoveryIncomplete) {
			t.Fatalf("scan past grace = %v, want ErrRecoveryIncomplete on the durable tier", err)
		}
		if err := life.RegisterRecovered(ctx, memory.StoredKey{Key: "material-1", Type: event.TypeExternalInput, Partition: 1, RecordedAt: now.Add(-10 * 24 * time.Hour)}); err != nil {
			t.Fatalf("RegisterRecovered: %v", err)
		}
		if _, err := retention.Register(ctx, "material-1", memory.HolderID("shared:recovery-resource"), memory.RetentionRecord{
			Kind:       memory.RetentionRecovery,
			Type:       event.TypeExternalInput,
			RecordedAt: now.Add(-10 * 24 * time.Hour),
		}); err != nil {
			t.Fatalf("Hold recovery material: %v", err)
		}
		if _, err := life.Scan(ctx, now); !errors.Is(err, memory.ErrRecoveryIncomplete) {
			t.Fatalf("scan before the inventory completes = %v, want ErrRecoveryIncomplete", err)
		}
		if _, marked := store.Tombstoned("expired-1"); marked {
			t.Fatal("expired key was tombstoned before the recovery inventory completed")
		}
		life.RecoveryComplete()
		report, err := life.Scan(ctx, now)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(report.Tombstoned) != 1 || report.Tombstoned[0] != event.EventID("expired-1") {
			t.Fatalf("scan tombstoned %v, want only expired-1 after the registration", report.Tombstoned)
		}
		if len(report.Protected) != 1 || report.Protected[0] != event.EventID("material-1") {
			t.Fatalf("scan protected = %v, want the recovered material kept by its lease", report.Protected)
		}
	})

	t.Run("G2.2 未确认材料越过 TTL 仍保留且释放后按原时间戳复归类: protected material survives TTL and reclassifies by its original time", func(t *testing.T) {
		life, retention, store := newDurable(t, memory.LifecycleConfig{GlobalTTLDays: 1}, durableCaps)
		ready(t, life)
		recordedAt := now.Add(-48 * time.Hour)
		store.Put(memory.StoredKey{Key: "unconf-1", Type: event.TypeAgentOutput, Partition: 1, RecordedAt: recordedAt})

		shared := memory.HolderID("shared:outbox-obligation")
		lease, err := retention.Register(ctx, "unconf-1", shared, memory.RetentionRecord{
			Kind:       memory.RetentionUnconfirmed,
			Type:       event.TypeAgentOutput,
			RecordedAt: recordedAt,
		})
		if err != nil {
			t.Fatalf("Hold: %v", err)
		}
		report, err := life.Scan(ctx, now)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(report.Protected) != 1 || report.Protected[0] != event.EventID("unconf-1") {
			t.Fatalf("scan protected = %v, want unconf-1 kept inside retention", report.Protected)
		}
		if len(report.Tombstoned) != 0 {
			t.Fatalf("scan tombstoned %v, want nothing inside retention", report.Tombstoned)
		}

		if err := retention.Retire(ctx, "unconf-1", shared, lease); err != nil {
			t.Fatalf("Release: %v", err)
		}
		if protected, err := retention.Held(ctx, "unconf-1"); err != nil || protected {
			t.Fatalf("Protected after release = %v, %v, want unprotected", protected, err)
		}
		if _, held := store.Tombstoned("unconf-1"); held {
			t.Fatal("release immediately tombstoned the original")
		}
		if record, registered := retention.Record("unconf-1"); !registered || !record.RecordedAt.Equal(recordedAt) {
			t.Fatalf("registration = %+v (%v), want the original recorded time kept, never re-stamped", record, registered)
		}
		report, err = life.Scan(ctx, now)
		if err != nil {
			t.Fatalf("Scan after release: %v", err)
		}
		if len(report.Tombstoned) != 1 || report.Tombstoned[0] != event.EventID("unconf-1") {
			t.Fatalf("scan after release tombstoned %v, want unconf-1 reclassified by its original time", report.Tombstoned)
		}
	})

	t.Run("G2.3 显式删除撞保护返回 protected: an explicit delete on a protected key is refused by name", func(t *testing.T) {
		life, retention, _ := newDurable(t, memory.LifecycleConfig{}, durableCaps)
		ready(t, life)
		holder := memory.HolderID("shared:recovery-resource")
		if _, err := retention.Register(ctx, "kept-1", holder, memory.RetentionRecord{
			Kind:       memory.RetentionRecovery,
			Type:       event.TypeAgentOutput,
			RecordedAt: now,
		}); err != nil {
			t.Fatalf("Hold: %v", err)
		}
		err := life.Delete(ctx, "kept-1", now)
		if !errors.Is(err, memory.ErrProtected) {
			t.Fatalf("Delete on the protected key = %v, want a refusal reported as protected", err)
		}
	})

	t.Run("G2.4 单 agent 关闭不释放共享保护: one holder closing keeps the shared protection standing", func(t *testing.T) {
		_, retention, _ := newDurable(t, memory.LifecycleConfig{}, durableCaps)
		shared := memory.HolderID("shared:recovery-resource")
		closing := memory.HolderID("agent-a:instance-7")
		if _, err := retention.Register(ctx, "kept-2", shared, memory.RetentionRecord{
			Kind:       memory.RetentionRecovery,
			Type:       event.TypeAgentOutput,
			RecordedAt: now,
		}); err != nil {
			t.Fatalf("Hold shared: %v", err)
		}
		if _, err := retention.Register(ctx, "kept-2", closing, memory.RetentionRecord{
			Kind:       memory.RetentionUnconfirmed,
			Type:       event.TypeAgentOutput,
			RecordedAt: now,
		}); err != nil {
			t.Fatalf("Hold closing: %v", err)
		}
		released, err := retention.ReleaseHolder(ctx, closing)
		if err != nil {
			t.Fatalf("ReleaseHolder: %v", err)
		}
		if released != 1 {
			t.Fatalf("ReleaseHolder released %d holds, want exactly its own one", released)
		}
		if protected, err := retention.Held(ctx, "kept-2"); err != nil || !protected {
			t.Fatalf("Protected after one holder closed = %v, %v, want the shared protection kept", protected, err)
		}
		if _, err := retention.ReleaseHolder(ctx, shared); err != nil {
			t.Fatalf("ReleaseHolder shared: %v", err)
		}
		if protected, err := retention.Held(ctx, "kept-2"); err != nil || protected {
			t.Fatalf("Protected after the last holder released = %v, %v, want unprotected", protected, err)
		}
		if _, err := retention.ReleaseHolder(ctx, shared); err != nil {
			t.Fatalf("repeated ReleaseHolder must be idempotent, got %v", err)
		}
	})

	t.Run("G2.5 枚举失败不核对残缺视图: a failed enumeration reconciles nothing", func(t *testing.T) {
		life, _, store := newDurable(t, memory.LifecycleConfig{GlobalTTLDays: 1}, durableCaps)
		ready(t, life)
		store.Put(memory.StoredKey{Key: "expired-2", Type: event.TypeAgentOutput, Partition: 1, RecordedAt: now.Add(-10 * 24 * time.Hour)})
		broken := failingStore{LifecycleStore: store, err: errors.New("disk gone")}
		retention := memory.NewRetention(memory.NewMemoryGuard())
		partial, err := memory.NewLifecycle(memory.LifecycleConfig{GlobalTTLDays: 1}, retention, broken, durableCaps)
		if err != nil {
			t.Fatalf("NewLifecycle over the failing store: %v", err)
		}
		partial.ArmGrace(now)
		partial.RecoveryComplete()
		report, err := partial.Scan(ctx, now)
		if !errors.Is(err, memory.ErrEnumerationIncomplete) {
			t.Fatalf("Scan over a failed enumeration = %v, want ErrEnumerationIncomplete", err)
		}
		if len(report.Tombstoned) != 0 {
			t.Fatalf("failed enumeration tombstoned %v, want the partial view left unreconciled", report.Tombstoned)
		}
		if _, marked := store.Tombstoned("expired-2"); marked {
			t.Fatal("a key was tombstoned off the failed enumeration's partial view")
		}
	})

	t.Run("G2.6 持久档缺能力 fail-closed: a durable store missing coordination refuses to start", func(t *testing.T) {
		retention := memory.NewRetention(memory.NewMemoryGuard())
		blind := memory.NewMemoryLifecycleStore(memory.LifecycleCapabilities{DurableRecovery: true})
		life, err := memory.NewLifecycle(memory.LifecycleConfig{}, retention, blind, memory.LifecycleCapabilities{DurableRecovery: true})
		if !errors.Is(err, memory.ErrCapabilityIncomplete) {
			t.Fatalf("NewLifecycle on the incapable durable store = %v, want ErrCapabilityIncomplete", err)
		}
		if life != nil {
			t.Fatal("the incapable durable store must not be started: fail-closed, never unprotected success")
		}
		plain := memory.NewMemoryLifecycleStore(memory.LifecycleCapabilities{})
		started, err := memory.NewLifecycle(memory.LifecycleConfig{}, retention, plain, memory.LifecycleCapabilities{})
		if err != nil {
			t.Fatalf("NewLifecycle on the non-durable store: %v", err)
		}
		if started == nil {
			t.Fatal("the non-durable store starts with its gap explicit")
		}
		if caps := plain.Capabilities(); caps.DurableRecovery || caps.Protection || caps.Reclamation {
			t.Fatalf("non-durable capabilities = %+v, want the gap declared explicitly", caps)
		}
	})
}
