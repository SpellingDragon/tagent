package tagent_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// newEnv builds a minimal inbox-v2 envelope: one fixed slot carrying a lossless
// source_event snapshot (the leaf requires a non-empty source_event per slot —
// D2). The drill only exercises the reliability-leaf lifecycle
// (enqueue→claim→receipt→ack), so the snapshot body is opaque to it.
func newEnv(id string) *reliability.Envelope {
	src, _ := json.Marshal(map[string]any{
		"id": id, "type": "external_input", "source": "user",
		"message": map[string]any{"role": "user", "content": "m-" + id},
	})
	return &reliability.Envelope{
		RequestID: id, Source: "user", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{SourceEvent: src}},
	}
}

// TestDrill_UpgradeTreatsLegacySpillAsInertThenResets pins the upgrade path for pre-migration spill files.
// - A prior-format *.spill does not block boot: the binary opens the directory and classifies the item as inert transitional data.
// - The item is never reinterpreted nor absorbed; an explicit managed ResetTransitional clears it.
// - The full durable lifecycle then runs on the current format.
//
// 契约: docs/wiki/reliability/durable-delivery.md#reopen-refusal
func TestDrill_UpgradeTreatsLegacySpillAsInertThenResets(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "00000000000000000001.spill")
	require.NoError(t, os.WriteFile(legacy, []byte(`{"legacy":"pre-migration"}`), 0o644))

	in, err := reliability.NewInbox(dir, 10)
	require.NoError(t, err, "§3.7: legacy .spill must NOT block boot; it is inert transitional data")
	defer in.Close()

	require.Equal(t, int64(0), in.Pending(), "legacy .spill is never absorbed as a v2 input")
	sp, _ := in.TransitionalData()
	require.Contains(t, sp, legacy, "classified as transitional, awaiting an explicit reset")

	removed, rerr := in.ResetTransitional(true)
	require.NoError(t, rerr)
	require.Equal(t, 1, removed, "managed reset clears exactly the enumerated legacy file")
	require.NoFileExists(t, legacy)

	require.NoError(t, func() error { _, e := in.Enqueue(newEnv("req-1")); return e }())
	env, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "req-1", env.RequestID)
	require.NoError(t, in.PrepareFacts(path, "rk-req-1", []json.RawMessage{json.RawMessage(`{"event_key":11}`)}))
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"completion_version":1}`)))
	require.NoError(t, in.RecordReceipt(path, reliability.ReceiptCredential{ReceiptKey: "rk-req-1"}))
	require.NoError(t, in.Ack(path))
	require.EqualValues(t, 0, in.Pending(), "acked envelope leaves nothing outstanding")
}

// TestDrill_RollbackRefusedWhileOutstandingThenSafeAfterDrain pins when a downgrade is safe.
// - A pre-inbox binary ignores inbox-v1, so downgrading while unacked envelopes exist would silently drop them.
// - The operator gate is read-only: Pending()>0 → refuse, drain-to-zero → safe.
func TestDrill_RollbackRefusedWhileOutstandingThenSafeAfterDrain(t *testing.T) {
	dir := t.TempDir()
	in, err := reliability.NewInbox(dir, 10)
	require.NoError(t, err)
	require.NoError(t, func() error { _, e := in.Enqueue(newEnv("req-0")); return e }())
	require.NoError(t, func() error { _, e := in.Enqueue(newEnv("req-1")); return e }())
	require.EqualValues(t, 2, in.Pending())

	_, _, err = in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.Close())

	in2, err := reliability.NewInbox(dir, 10)
	require.NoError(t, err)
	defer in2.Close()
	require.EqualValues(t, 2, in2.Pending(), "crash leaves both envelopes outstanding after reopen")
	require.Greater(t, in2.Pending(), int64(0), "outstanding>0 → MUST NOT downgrade to pre-inbox binary")

	for in2.Pending() > 0 {
		env, path, cerr := in2.ClaimNext()
		require.NoError(t, cerr)
		require.NotNil(t, env)
		key := "rk-" + env.RequestID
		require.NoError(t, in2.PrepareFacts(path, key, []json.RawMessage{json.RawMessage(`{"event_key":11}`)}))
		require.NoError(t, in2.RecordCompletion(path, json.RawMessage(`{"completion_version":1}`)))
		require.NoError(t, in2.RecordReceipt(path, reliability.ReceiptCredential{ReceiptKey: key}))
		require.NoError(t, in2.Ack(path))
	}
	require.EqualValues(t, 0, in2.Pending(), "drained → rollback to pre-inbox binary is now data-safe")
}

// TestDrill_PartitionCollisionDiagnosisIsReadOnly pins that the collision scan is read-only.
// - Pigeonhole guarantees detection: 1200 distinct names over a 10-bit (1024) pid space MUST collide, independent of hash distribution.
// - The scan flags collisions and mutates nothing.
func TestDrill_PartitionCollisionDiagnosisIsReadOnly(t *testing.T) {
	names := make([]string, 0, 1200)
	for i := 0; i < 1200; i++ {
		names = append(names, fmt.Sprintf("agent-%d", i))
	}
	snapshot := make(map[string]int, len(names))
	for _, n := range names {
		snapshot[n] = memory.PartitionIDFromName(n)
	}

	collisions := diagnosePartitionCollisions(names)
	require.NotEmpty(t, collisions, "1200 names over a 10-bit space must yield a collision")
	for _, g := range collisions {
		require.Greater(t, len(g), 1, "a reported collision group holds >1 distinct name")
		pid := memory.PartitionIDFromName(g[0])
		for _, n := range g {
			require.Equal(t, pid, memory.PartitionIDFromName(n), "collision group members share one pid")
		}
	}
	for _, n := range names {
		require.Equal(t, snapshot[n], memory.PartitionIDFromName(n), "diagnosis must not alter pid mapping")
	}
}

// diagnosePartitionCollisions mirrors registerStoreOwner's read-only intent:
// group names sharing one store by their hash partition and return any pid
// claimed by more than one name. It performs no writes and no migration.
func diagnosePartitionCollisions(names []string) [][]string {
	byPID := make(map[int][]string)
	for _, n := range names {
		pid := memory.PartitionIDFromName(n)
		byPID[pid] = append(byPID[pid], n)
	}
	var out [][]string
	for _, group := range byPID {
		if len(group) > 1 {
			out = append(out, append([]string(nil), group...))
		}
	}
	return out
}
