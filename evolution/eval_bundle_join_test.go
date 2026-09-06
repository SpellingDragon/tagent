package evolution

import (
	"context"
	"strings"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestEvidence_BundleJoin is the D1-B regression (design-report-closeout):
// Evidence collection joins events to the target bundle via the bundle_id
// Metadata stamp first; untagged events fall back to the time-window
// attribution. Events stamped for a DIFFERENT bundle must not leak into this
// bundle's evidence.
func TestEvidence_BundleJoin(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := 1
	now := time.Now().UnixMilli()
	// Keys MUST be real Snowflake ids: GetEvents derives the partition from
	// the key itself (PartitionIDFromEventKey), so bare ints would miss.
	put := func(seq int64, bundleID, subtype string) {
		key := memory.NewSnowflakeEventKey(pid, now+seq)
		md := map[string]string{tagentevent.MetaKeySubtype: subtype}
		if bundleID != "" {
			md[tagentevent.MetaKeyBundleID] = bundleID
		}
		if err := store.StoreEvent(key, memory.FullEvent{
			EventKey: key, PartitionID: pid, EventType: tagentevent.TypeGovernance,
			Timestamp: now + seq, Metadata: md,
		}); err != nil {
			t.Fatal(err)
		}
	}
	put(1, "b1", tagentevent.SubtypeDenial) // b1's denial
	put(2, "b2", tagentevent.SubtypeDenial) // b2's denial — must not leak into b1
	put(3, "", tagentevent.SubtypeDenial)   // untagged → window fallback, counted

	src := NewStoreEvidenceSource(store, pid, time.Hour)

	ev1, err := src.Collect(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	if ev1.DenialCount != 2 { // 101 (exact) + 103 (fallback)
		t.Fatalf("b1 DenialCount = %d, want 2 (exact join + window fallback)", ev1.DenialCount)
	}

	ev2, err := src.Collect(context.Background(), "b2")
	if err != nil {
		t.Fatal(err)
	}
	if ev2.DenialCount != 2 { // 102 (exact) + 103 (fallback)
		t.Fatalf("b2 DenialCount = %d, want 2", ev2.DenialCount)
	}
}

// TestGuardrail_NegativeFeedbackRollback (2.5, design-report-closeout): the
// negative-feedback rate criterion must breach the guardrail when the share
// of negative feedback attributed to the canary bundle exceeds the
// threshold. Fail-before: no such criterion existed.
func TestGuardrail_NegativeFeedbackRollback(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := 1
	now := time.Now().UnixMilli()
	put := func(seq int64, etype, subtype, content string) {
		key := memory.NewSnowflakeEventKey(pid, now+seq)
		md := map[string]string{}
		if subtype != "" {
			md[tagentevent.MetaKeySubtype] = subtype
		}
		_ = store.StoreEvent(key, memory.FullEvent{
			EventKey: key, PartitionID: pid, EventType: etype,
			Timestamp: now + seq, Content: content, Metadata: md,
		})
	}
	// 6 events under b1: 5 neutral turns + 3 negative feedback → rate 0.5 > 0.3.
	for i := 0; i < 5; i++ {
		put(int64(i), tagentevent.TypeExternalInput, "", "turn")
	}
	for i := 5; i < 8; i++ {
		put(int64(i), tagentevent.TypeFeedback, "task_settle", `{"verdict":"negative","source":"task_settle"}`)
	}
	src := NewStoreEvidenceSource(store, pid, time.Hour)
	g := NewMetricGuardrail(src, GuardrailConfig{MinSamples: 5, MaxNegFbRate: 0.3})
	breach, reason := g.Breach("b1")
	if !breach || !strings.Contains(reason, "负反馈率") {
		t.Fatalf("breach=%v reason=%q", breach, reason)
	}
}
