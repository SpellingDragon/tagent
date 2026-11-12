// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// storedRefFixture is the whole reference record the original store persists: the
// scope it was accepted under, its acceptance time and the completeness
// information of its body.
type storedRefFixture struct {
	OwnerAgent      string    `json:"owner_agent"`
	OwnerSession    string    `json:"owner_session"`
	Format          string    `json:"format"`
	RetentionPolicy string    `json:"retention_policy"`
	StoredAt        time.Time `json:"stored_at"`
	Digest          string    `json:"digest"`
	Length          int64     `json:"length"`
}

// TestRecoverOrphanOriginalsRebuildsFromChain pins the rebuild of one orphan reference from the chain event of its key.
//   - the seeded root is the state the boot loader refuses by name
//   - the dry run reports the reference recoverable with its first owner and writes no byte
//   - --restore commits the body, the reopened root boots, and the reference record stays byte for byte as it was committed
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverOrphanOriginalsRebuildsFromChain(t *testing.T) {
	ctx := context.Background()
	const key = "1408-d87b587dc0a350d14b98763f8f14748c"
	const content = "the body the chain still holds"
	evt := chainEvent(key, content)
	tier, root := seedRoot(t, ctx)
	commitChainEvent(t, ctx, tier, evt, 1)
	joined := event.Address{AgentID: evt.Owner.AgentID, SessionID: "session-b"}
	records := []storedRefFixture{refFixture([]byte(content), evt.Owner), refFixture([]byte(content), joined)}
	refValue := refJSON(t, records)
	commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: refValue})
	assertBootRefuses(t, ctx, root, key)
	before := rootFiles(t, root)

	dry, err := recoverOrphanOriginals(ctx, root, false)
	if err != nil {
		t.Fatalf("the dry run over %s returned %v", root, err)
	}
	want := orphan{Key: key, OwnerAgent: string(evt.Owner.AgentID), OwnerSession: string(evt.Owner.SessionID)}
	if len(dry.Recoverable) != 1 || dry.Recoverable[0] != want {
		t.Fatalf("the dry run report = %+v, want the one recoverable reference %+v", dry, want)
	}
	if len(dry.Restored) != 0 || len(dry.TrulyLost) != 0 || len(dry.Mismatch) != 0 {
		t.Fatalf("the dry run report = %+v, want no other class", dry)
	}
	if dry.exitCode() != 0 {
		t.Fatalf("the dry run exit code = %d, want 0", dry.exitCode())
	}
	assertRootUnchanged(t, root, before)
	if held := blobKeysOf(t, ctx, root); len(held) != 0 {
		t.Fatalf("the dry run wrote the bodies %v, want none", held)
	}

	restored, err := recoverOrphanOriginals(ctx, root, true)
	if err != nil {
		t.Fatalf("the restore over %s returned %v", root, err)
	}
	if len(restored.Restored) != 1 || restored.Restored[0] != want {
		t.Fatalf("the restore report = %+v, want the one restored reference %+v", restored, want)
	}
	if len(restored.Recoverable) != 0 || len(restored.TrulyLost) != 0 || len(restored.Mismatch) != 0 {
		t.Fatalf("the restore report = %+v, want no other class", restored)
	}
	if restored.exitCode() != 0 {
		t.Fatalf("the restore exit code = %d, want 0", restored.exitCode())
	}
	reopened := openRoot(t, ctx, root)
	got := reopened.Originals.Get(ctx, event.EventID(key))
	if got.Outcome != ledger.RecallFound || !bytes.Equal(got.Body, []byte(content)) {
		t.Fatalf("the recall of the restored root = %+v, want the exact body %q", got, content)
	}
	refPairs, err := reopened.Store.Read(ctx, ledger.Fetch{Key: "ref:" + key})
	if err != nil {
		t.Fatalf("read the reference of %s: %v", key, err)
	}
	if len(refPairs) != 1 || !bytes.Equal(refPairs[0].Value, refValue) {
		t.Fatalf("the restored root holds the reference records %v, want the one record byte for byte as it was committed", refPairs)
	}
	if held := blobKeysOf(t, ctx, root); !reflect.DeepEqual(held, []string{"blob:" + key}) {
		t.Fatalf("the restored root holds the bodies %v, want only blob:%s", held, key)
	}
}

// TestRecoverOrphanOriginalsTrulyLost pins the refusal to invent a body the chain does not hold.
//   - the reference is reported truly lost by name with its owner and the exit code is nonzero
//   - --restore writes no byte: the reference stays as it was, no body appears, and the root still refuses to boot
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverOrphanOriginalsTrulyLost(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000dead"
	owner := event.Address{AgentID: "agent-lost", SessionID: "session-lost"}
	tier, root := seedRoot(t, ctx)
	records := []storedRefFixture{refFixture([]byte("the body no chain event holds"), owner)}
	commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: refJSON(t, records)})
	before := rootFiles(t, root)

	scanned, err := recoverOrphanOriginals(ctx, root, true)
	if err != nil {
		t.Fatalf("the restore over %s returned %v", root, err)
	}
	want := orphan{Key: key, OwnerAgent: string(owner.AgentID), OwnerSession: string(owner.SessionID)}
	if len(scanned.TrulyLost) != 1 || scanned.TrulyLost[0] != want {
		t.Fatalf("the report = %+v, want the truly lost reference %+v", scanned, want)
	}
	if len(scanned.Restored) != 0 || len(scanned.Recoverable) != 0 || len(scanned.Mismatch) != 0 {
		t.Fatalf("the report = %+v, want no other class", scanned)
	}
	if scanned.exitCode() != 1 {
		t.Fatalf("the exit code = %d, want 1 for a root that still cannot boot", scanned.exitCode())
	}
	assertRootUnchanged(t, root, before)
	if held := blobKeysOf(t, ctx, root); len(held) != 0 {
		t.Fatalf("the scan wrote the bodies %v, want none", held)
	}
	assertBootRefuses(t, ctx, root, key)
}

// TestRecoverOrphanOriginalsDigestMismatch pins the refusal to write a body the reference does not name.
//   - a digest of other bytes and a length of other bytes are both reported mismatch by name
//   - --restore writes no byte in either case and the exit code stays nonzero
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverOrphanOriginalsDigestMismatch(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000beef"
	const content = "the body the chain holds"
	owner := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	otherDigest := refFixture([]byte("another body entirely"), owner)
	otherLength := refFixture([]byte(content), owner)
	otherLength.Length++
	cases := []struct {
		name   string
		record storedRefFixture
	}{
		{name: "digest", record: otherDigest},
		{name: "length", record: otherLength},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, root := seedRoot(t, ctx)
			commitChainEvent(t, ctx, tier, chainEvent(key, content), 1)
			commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: refJSON(t, []storedRefFixture{tc.record})})
			before := rootFiles(t, root)

			scanned, err := recoverOrphanOriginals(ctx, root, true)
			if err != nil {
				t.Fatalf("the restore over %s returned %v", root, err)
			}
			want := orphan{Key: key, OwnerAgent: string(owner.AgentID), OwnerSession: string(owner.SessionID)}
			if len(scanned.Mismatch) != 1 || scanned.Mismatch[0] != want {
				t.Fatalf("the report = %+v, want the mismatched reference %+v", scanned, want)
			}
			if len(scanned.Restored) != 0 || len(scanned.Recoverable) != 0 || len(scanned.TrulyLost) != 0 {
				t.Fatalf("the report = %+v, want no other class", scanned)
			}
			if scanned.exitCode() != 1 {
				t.Fatalf("the exit code = %d, want 1 for a root that still cannot boot", scanned.exitCode())
			}
			assertRootUnchanged(t, root, before)
			if held := blobKeysOf(t, ctx, root); len(held) != 0 {
				t.Fatalf("the scan wrote the bodies %v, want none", held)
			}
			assertBootRefuses(t, ctx, root, key)
		})
	}
}

// TestRecoverOrphanOriginalsPairedRoot pins the parity of a root whose every reference names its body.
//   - the dry run and the restore both report nothing to recover and exit 0
//   - neither writes a byte, and the root recalls the body it already held
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverOrphanOriginalsPairedRoot(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000face"
	const content = "the body already paired with its reference"
	tier, root := seedRoot(t, ctx)
	evt := chainEvent(key, content)
	commitChainEvent(t, ctx, tier, evt, 1)
	commitPairs(t, ctx, tier,
		ledger.Pair{Key: "blob:" + key, Value: []byte(content)},
		ledger.Pair{Key: "ref:" + key, Value: refJSON(t, []storedRefFixture{refFixture([]byte(content), evt.Owner)})})
	before := rootFiles(t, root)

	for _, restore := range []bool{false, true} {
		scanned, err := recoverOrphanOriginals(ctx, root, restore)
		if err != nil {
			t.Fatalf("the scan of the paired root (restore=%t) returned %v", restore, err)
		}
		if len(scanned.Recoverable)+len(scanned.Restored)+len(scanned.TrulyLost)+len(scanned.Mismatch) != 0 {
			t.Fatalf("the scan of the paired root (restore=%t) = %+v, want nothing to recover", restore, scanned)
		}
		if scanned.exitCode() != 0 {
			t.Fatalf("the exit code of the paired root (restore=%t) = %d, want 0", restore, scanned.exitCode())
		}
	}
	assertRootUnchanged(t, root, before)
	reopened := openRoot(t, ctx, root)
	if got := reopened.Originals.Get(ctx, event.EventID(key)); got.Outcome != ledger.RecallFound || !bytes.Equal(got.Body, []byte(content)) {
		t.Fatalf("the recall of the paired root = %+v, want the exact body %q", got, content)
	}
}

// TestRecoverOrphanOriginalsSortedOrder pins the key order of every class across repeated scans of one root.
//   - the references are committed out of key order and each class comes back in key order
//   - ten dry runs report one identical report, and the restore commits the rebuildable keys in that same order
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverOrphanOriginalsSortedOrder(t *testing.T) {
	ctx := context.Background()
	owner := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	tier, root := seedRoot(t, ctx)
	fixtures := []struct {
		key     string
		chain   string
		refBody string
	}{
		{key: "1408-cccc", chain: "the third body", refBody: "the third body"},
		{key: "1408-aaaa", refBody: "the first body"},
		{key: "1408-bbbb", chain: "the body the chain holds", refBody: "the body the reference names"},
		{key: "1408-dddd", chain: "the fourth body", refBody: "the fourth body"},
		{key: "1408-eeee", refBody: "the fifth body"},
	}
	seq := int64(0)
	for _, fixture := range fixtures {
		seq++
		if fixture.chain != "" {
			commitChainEvent(t, ctx, tier, chainEvent(fixture.key, fixture.chain), seq)
		}
		records := []storedRefFixture{refFixture([]byte(fixture.refBody), owner)}
		commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + fixture.key, Value: refJSON(t, records)})
	}
	before := rootFiles(t, root)
	wantRecoverable := []string{"1408-cccc", "1408-dddd"}
	wantLost := []string{"1408-aaaa", "1408-eeee"}
	wantMismatch := []string{"1408-bbbb"}

	var first report
	for run := 0; run < 10; run++ {
		scanned, err := recoverOrphanOriginals(ctx, root, false)
		if err != nil {
			t.Fatalf("dry run %d over %s returned %v", run, root, err)
		}
		if run == 0 {
			first = scanned
			if got := orphanKeys(scanned.Recoverable); !reflect.DeepEqual(got, wantRecoverable) {
				t.Fatalf("the recoverable keys = %v, want %v", got, wantRecoverable)
			}
			if got := orphanKeys(scanned.TrulyLost); !reflect.DeepEqual(got, wantLost) {
				t.Fatalf("the truly lost keys = %v, want %v", got, wantLost)
			}
			if got := orphanKeys(scanned.Mismatch); !reflect.DeepEqual(got, wantMismatch) {
				t.Fatalf("the mismatched keys = %v, want %v", got, wantMismatch)
			}
			if scanned.exitCode() != 1 {
				t.Fatalf("the exit code = %d, want 1 for a root holding unbuildable references", scanned.exitCode())
			}
			continue
		}
		if !reflect.DeepEqual(scanned, first) {
			t.Fatalf("dry run %d = %+v, want the report of the first run %+v", run, scanned, first)
		}
	}
	assertRootUnchanged(t, root, before)

	restored, err := recoverOrphanOriginals(ctx, root, true)
	if err != nil {
		t.Fatalf("the restore over %s returned %v", root, err)
	}
	if got := orphanKeys(restored.Restored); !reflect.DeepEqual(got, wantRecoverable) {
		t.Fatalf("the restored keys = %v, want %v", got, wantRecoverable)
	}
	if got := orphanKeys(restored.TrulyLost); !reflect.DeepEqual(got, wantLost) {
		t.Fatalf("the truly lost keys after the restore = %v, want %v", got, wantLost)
	}
	if got := orphanKeys(restored.Mismatch); !reflect.DeepEqual(got, wantMismatch) {
		t.Fatalf("the mismatched keys after the restore = %v, want %v", got, wantMismatch)
	}
	if restored.exitCode() != 1 {
		t.Fatalf("the exit code after the restore = %d, want 1 for the references left unbuildable", restored.exitCode())
	}
	wantBlobs := []string{"blob:1408-cccc", "blob:1408-dddd"}
	if held := blobKeysOf(t, ctx, root); !reflect.DeepEqual(held, wantBlobs) {
		t.Fatalf("the restored root holds the bodies %v, want %v", held, wantBlobs)
	}
}

// TestRecoverOrphanOriginalsUndecodableReference pins the named refusal of a reference record that states no body to rebuild.
//   - bytes that are no JSON array and an empty array both fail naming the key
//   - neither case writes a byte, even with --restore
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverOrphanOriginalsUndecodableReference(t *testing.T) {
	ctx := context.Background()
	const key = "1408-0000000000000000000000000000abcd"
	cases := []struct {
		name  string
		value []byte
	}{
		{name: "not json", value: []byte("not json")},
		{name: "empty array", value: []byte("[]")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, root := seedRoot(t, ctx)
			commitChainEvent(t, ctx, tier, chainEvent(key, "the body the chain holds"), 1)
			commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: tc.value})
			before := rootFiles(t, root)

			if _, err := recoverOrphanOriginals(ctx, root, true); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("the scan of the %s reference returned %v, want the refusal naming %s", tc.name, err, key)
			}
			assertRootUnchanged(t, root, before)
			if held := blobKeysOf(t, ctx, root); len(held) != 0 {
				t.Fatalf("the scan wrote the bodies %v, want none", held)
			}
		})
	}
}

// TestDigestOfCanonicalForm pins the digest text to the published sha256 of the empty body, the form the original store records.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestDigestOfCanonicalForm(t *testing.T) {
	const want = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := digestOf(nil); got != want {
		t.Fatalf("digestOf(nil) = %q, want %q", got, want)
	}
}

// TestRecoverKVCommandExitCodes pins the flags and the exit status of the built command over live journal roots.
//   - no --root fails nonzero naming the flag
//   - the default scan of a rebuildable root exits 0, reports the dry run and writes no byte
//   - --restore exits 0 and the restored root recalls the exact body
//   - a truly lost reference exits nonzero and names the key with its owner on stdout
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRecoverKVCommandExitCodes(t *testing.T) {
	ctx := context.Background()
	binary := buildRecoverKV(t, ctx)
	const key = "1408-0000000000000000000000000000c0de"
	const content = "the body the command rebuilds"
	owner := event.Address{AgentID: "agent-cli", SessionID: "session-cli"}
	tier, root := seedRoot(t, ctx)
	commitChainEvent(t, ctx, tier, chainEvent(key, content), 1)
	commitPairs(t, ctx, tier, ledger.Pair{Key: "ref:" + key, Value: refJSON(t, []storedRefFixture{refFixture([]byte(content), owner)})})
	lostTier, lostRoot := seedRoot(t, ctx)
	lostKey := "1408-0000000000000000000000000000d00d"
	commitPairs(t, ctx, lostTier, ledger.Pair{Key: "ref:" + lostKey, Value: refJSON(t, []storedRefFixture{refFixture([]byte("the body no chain event holds"), owner)})})

	out, errOut, code := runRecoverKV(t, ctx, binary)
	if code != 1 || !strings.Contains(errOut, "--root is required") {
		t.Fatalf("the command without --root exited %d with stderr %q, want 1 naming the flag", code, errOut)
	}
	before := rootFiles(t, root)
	out, _, code = runRecoverKV(t, ctx, binary, "--root", root)
	if code != 0 || !strings.Contains(out, "1 recoverable") || !strings.Contains(out, "dry run") || !strings.Contains(out, key) {
		t.Fatalf("the default scan exited %d with stdout %q, want 0 reporting the dry run of %s", code, out, key)
	}
	assertRootUnchanged(t, root, before)

	out, _, code = runRecoverKV(t, ctx, binary, "--root", root, "--restore")
	if code != 0 || !strings.Contains(out, "1 restored") {
		t.Fatalf("the restore exited %d with stdout %q, want 0 reporting the one restored body", code, out)
	}
	if got := openRoot(t, ctx, root).Originals.Get(ctx, event.EventID(key)); got.Outcome != ledger.RecallFound || !bytes.Equal(got.Body, []byte(content)) {
		t.Fatalf("the recall of the restored root = %+v, want the exact body %q", got, content)
	}
	out, _, code = runRecoverKV(t, ctx, binary, "--root", root)
	if code != 0 || !strings.Contains(out, "nothing to recover") {
		t.Fatalf("the rescan of the restored root exited %d with stdout %q, want 0 reporting nothing to recover", code, out)
	}

	lostBefore := rootFiles(t, lostRoot)
	out, errOut, code = runRecoverKV(t, ctx, binary, "--root", lostRoot, "--restore")
	if code != 1 {
		t.Fatalf("the restore of a truly lost reference exited %d, want 1", code)
	}
	for _, want := range []string{lostKey, string(owner.AgentID), string(owner.SessionID), "truly lost"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the stdout %q does not name %q", out, want)
		}
	}
	if !strings.Contains(errOut, "unable to boot") {
		t.Fatalf("the stderr %q does not report the root as unable to boot", errOut)
	}
	assertRootUnchanged(t, lostRoot, lostBefore)
}

// seedRoot opens a journal root in a temporary directory for one case to commit
// into, and returns the tier beside the root path.
func seedRoot(t *testing.T, ctx context.Context) (*ledger.Tier, string) {
	t.Helper()
	root := t.TempDir()
	return openRoot(t, ctx, root), root
}

// openRoot opens a fresh view of root, so an assertion reads what the journal
// holds rather than what an earlier view published.
func openRoot(t *testing.T, ctx context.Context, root string) *ledger.Tier {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the journal root %s: %v", root, err)
	}
	return tier
}

// chainEvent returns one event carrying content under key, owned by the address
// the cases register their references under.
func chainEvent(key, content string) event.Event {
	return event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeAgentOutput,
		Timestamp: time.UnixMilli(1760000000123),
		Owner:     event.Address{AgentID: "agent-a", SessionID: "session-a"},
		Message:   model.Message{Role: model.RoleAssistant, Content: content},
	}
}

// commitChainEvent commits the encoded body of evt as one chain record of the
// tier, at the sequence slot the case names.
func commitChainEvent(t *testing.T, ctx context.Context, tier *ledger.Tier, evt event.Event, seq int64) {
	t.Helper()
	body, err := memory.BodyFromEvent(evt, "text/plain", "keep")
	if err != nil {
		t.Fatalf("derive the body of %s: %v", evt.Key, err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("encode the body of %s: %v", evt.Key, err)
	}
	window := evt.Timestamp.UnixMilli()
	err = tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
		PartitionID: memory.PartitionIDFromName(string(evt.Owner.AgentID)),
		WindowTS:    window,
		Seq:         seq,
		EventKey:    evt.Key,
		EventTime:   window,
		Body:        encoded,
	}.WithChain(evt.Type, evt.Owner.SessionID))
	if err != nil {
		t.Fatalf("commit the chain event %s: %v", evt.Key, err)
	}
}

// refFixture returns the reference record of body under owner, its completeness
// information computed over the bytes themselves rather than by the command.
func refFixture(body []byte, owner event.Address) storedRefFixture {
	sum := sha256.Sum256(body)
	return storedRefFixture{
		OwnerAgent:      string(owner.AgentID),
		OwnerSession:    string(owner.SessionID),
		Format:          "text/plain",
		RetentionPolicy: "keep",
		StoredAt:        time.UnixMilli(1760000000123),
		Digest:          "sha256:" + hex.EncodeToString(sum[:]),
		Length:          int64(len(body)),
	}
}

// refJSON encodes the reference records of one key as the value the original
// store's load reads.
func refJSON(t *testing.T, records []storedRefFixture) []byte {
	t.Helper()
	value, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("encode the reference records: %v", err)
	}
	return value
}

// commitPairs commits pairs into the tier behind one barrier.
func commitPairs(t *testing.T, ctx context.Context, tier *ledger.Tier, pairs ...ledger.Pair) {
	t.Helper()
	if err := tier.Store.Commit(ctx, ledger.Batch{Puts: pairs}); err != nil {
		t.Fatalf("commit %d pairs: %v", len(pairs), err)
	}
}

// blobKeysOf returns the body keys a freshly opened view of root holds, in key
// order.
func blobKeysOf(t *testing.T, ctx context.Context, root string) []string {
	t.Helper()
	store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the journal root %s: %v", root, err)
	}
	keys, err := store.Keys(ctx, "blob:")
	if err != nil {
		t.Fatalf("read the body keys of %s: %v", root, err)
	}
	return keys
}

// assertBootRefuses reports the boot load of root as the named refusal of the
// reference of key with no independent body.
func assertBootRefuses(t *testing.T, ctx context.Context, root, key string) {
	t.Helper()
	got := openRoot(t, ctx, root).Originals.Get(ctx, event.EventID(key))
	if got.Outcome != ledger.RecallFailed || !errors.Is(got.Err, ledger.ErrSnapshotCorrupt) || !strings.Contains(got.Err.Error(), key) {
		t.Fatalf("the boot load of %s = %+v, want the named refusal of the reference of %s with no body", root, got, key)
	}
}

// rootFiles returns the bytes of every file under root keyed by its relative
// path, so a case can pin a scan that writes nothing byte for byte.
func rootFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[name] = value
		return nil
	})
	if err != nil {
		t.Fatalf("read the files of %s: %v", root, err)
	}
	return files
}

// assertRootUnchanged names every file of root whose bytes differ from the
// snapshot, and every file of the snapshot that root does not hold.
func assertRootUnchanged(t *testing.T, root string, before map[string][]byte) {
	t.Helper()
	after := rootFiles(t, root)
	if len(after) != len(before) {
		t.Fatalf("the scan left %d files under %s, want the %d it held before", len(after), root, len(before))
	}
	for name, want := range before {
		got, held := after[name]
		if !held {
			t.Fatalf("the scan removed %s of %s", name, root)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("the scan rewrote %s of %s", name, root)
		}
	}
}

// orphanKeys returns the keys of one reported class in the order it holds them.
func orphanKeys(list []orphan) []string {
	keys := make([]string, 0, len(list))
	for _, item := range list {
		keys = append(keys, item.Key)
	}
	return keys
}

// buildRecoverKV builds the command into a temporary directory and returns its
// path, so a case runs the flags and the exit status an operator meets.
func buildRecoverKV(t *testing.T, ctx context.Context) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "recover-kv")
	out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build the command: %v\n%s", err, out)
	}
	return binary
}

// runRecoverKV runs the built command with args and returns its stdout, its
// stderr and its exit status.
func runRecoverKV(t *testing.T, ctx context.Context, binary string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(ctx, binary, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run %s %v: %v", binary, args, err)
	}
	return out.String(), errOut.String(), exit.ExitCode()
}
