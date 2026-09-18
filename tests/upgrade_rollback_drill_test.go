package tagent_test

// UPGRADE / ROLLBACK DRILL (resident-remaining-hardening 4.6, 8.4):
// an operator-shaped rehearsal of the durable-input upgrade/rollback gates,
// chained as one lifecycle rather than as isolated atoms.
//
// Atom gates already live elsewhere — flock single-writer + fingerprint config
// conflict (resources_lock_test.go), redirect allowlist (endpoint_redirect_test.go),
// reopen requeue (inbox_test.go). This drill covers the three SEQUENCE surfaces
// that had no cohesive coverage:
//  1. upgrade: legacy *.spill sibling → refuse → drain → accept → full ack
//  2. rollback condition: unacked (pending+claimed+receipted) > 0 blocks a
//     downgrade to a pre-inbox binary; draining to zero unblocks it
//  3. partition-collision READ-ONLY pre-upgrade diagnosis: flag two names that
//     hash to the same 10-bit pid on one store, WITHOUT migrating/re-keying
//     (registerStoreOwner fails closed at build; rename is the only fix)
//
// Runs by default (file ops + pure functions, no sleeps, no real restart).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

func newEnv(id string) *reliability.Envelope {
	return &reliability.Envelope{
		RequestID: id, Source: "user", State: reliability.InboxStatePending,
		Messages: []reliability.EnvelopeMessage{{Role: "user", Content: "m-" + id}},
	}
}

// 1. Upgrade path: the inbox-v1 binary refuses to open a dir that still holds
// pre-migration *.spill items (the version that wrote them must drain them),
// then, once drained, accepts the dir and runs the full durable lifecycle.
func TestDrill_UpgradeDrainsLegacySpillThenAccepts(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "00000000000000000001.spill")
	require.NoError(t, os.WriteFile(legacy, []byte(`{"legacy":"pre-migration"}`), 0o644))

	_, err := reliability.NewInbox(dir, 10)
	require.ErrorIs(t, err, reliability.ErrLegacySpillNotDrained,
		"upgrade MUST fail loud while legacy .spill sibling present (never silently reinterpreted)")

	// Operator drains with the previous binary (modelled here as removal).
	require.NoError(t, os.Remove(legacy))
	in, err := reliability.NewInbox(dir, 10)
	require.NoError(t, err, "after drain the upgrade binary opens the same dir")
	defer in.Close()

	require.NoError(t, func() error { _, e := in.Enqueue(newEnv("req-1")); return e }())
	env, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "req-1", env.RequestID)
	require.NoError(t, in.RecordReceipt(path, "turn done"))
	require.NoError(t, in.Ack(path))
	require.EqualValues(t, 0, in.Pending(), "acked envelope leaves nothing outstanding")
}

// 2. Rollback condition: a pre-inbox binary ignores inbox-v1, so downgrading
// while unacked envelopes exist would silently drop them. The operator's
// read-only gate is Pending()>0 → refuse; drain-to-zero → safe.
func TestDrill_RollbackRefusedWhileOutstandingThenSafeAfterDrain(t *testing.T) {
	dir := t.TempDir()
	in, err := reliability.NewInbox(dir, 10)
	require.NoError(t, err)
	require.NoError(t, func() error { _, e := in.Enqueue(newEnv("req-0")); return e }())
	require.NoError(t, func() error { _, e := in.Enqueue(newEnv("req-1")); return e }())
	require.EqualValues(t, 2, in.Pending())

	// Crash mid-processing: claim one, never receipt/ack, then close.
	_, _, err = in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.Close())

	// Reopen (next boot): the claimed item returns to pending; both still count
	// as outstanding — the rollback gate must therefore refuse a downgrade.
	in2, err := reliability.NewInbox(dir, 10)
	require.NoError(t, err)
	defer in2.Close()
	require.EqualValues(t, 2, in2.Pending(), "crash leaves both envelopes outstanding after reopen")
	require.Greater(t, in2.Pending(), int64(0), "outstanding>0 → MUST NOT downgrade to pre-inbox binary")

	// Drain to zero (claim→receipt→ack) before any downgrade is data-safe.
	for in2.Pending() > 0 {
		env, path, cerr := in2.ClaimNext()
		require.NoError(t, cerr)
		require.NotNil(t, env)
		require.NoError(t, in2.RecordReceipt(path, "drain"))
		require.NoError(t, in2.Ack(path))
	}
	require.EqualValues(t, 0, in2.Pending(), "drained → rollback to pre-inbox binary is now data-safe")
}

// 3. Read-only partition-collision diagnosis. Pigeonhole guarantees detection:
// 1200 distinct names over a 10-bit (1024) pid space MUST collide, independent
// of hash distribution. The scan flags collisions and mutates nothing.
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
	// Read-only: the diagnosis never re-keys — every pid mapping is unchanged.
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
