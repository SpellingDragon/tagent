// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package history

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestRecoverKVReadsARelocatedRoot is the consumer anchor of fact retirement
// (runtime-recovery-ideal 08-fact-retirement 6.3): the formal repair entry
// scripts/recover-kv, run against a root whose settled facts were relocated, exits
// zero and reports nothing to recover.
//   - denominator: the root really is relocated — the pass states a settled key and
//     the hot journal holds neither the reference nor the body of the moved fact —
//     and the cold layer answers the original by identity, so the clean scan is not
//     a root that never moved anything
//   - positive: the command, built from ./scripts/recover-kv and run with the flags
//     an operator runs, exits zero and prints the nothing-to-recover line
//   - negative (false positive): a root whose reference moved cold while its body
//     stayed behind would report that reference as an orphan, and the byte equality
//     of the two families in the moved set is what such a report would contradict
//   - consumer: the command is the shipped main of scripts/recover-kv, its --root is
//     the root the assembly opens, and the scan it runs is the store's own
//     Key/Read faces over that root
func TestRecoverKVReadsARelocatedRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("open the journal tier: %v", err)
	}
	owner := event.Address{AgentID: "agent-a", SessionID: "session-recover-kv"}
	settled := event.EventID("recover-kv-settled-fact")
	live := event.EventID("recover-kv-live-fold")
	factBody := bytes.Repeat([]byte("recover-kv-material "), 32)
	originalBody := bytes.Repeat([]byte("recover-kv-original "), 64)
	commit := func(key event.EventID, window, seq int64, typ string, body []byte, original []byte) {
		t.Helper()
		record := ledger.SegmentRecord{
			PartitionID: 1,
			WindowTS:    window,
			Seq:         seq,
			EventKey:    key,
			EventTime:   window*100 + seq,
			Body:        body,
		}.WithChain(typ, "session-recover-kv")
		if err := tier.Segments.StoreEvent(ctx, record); err != nil {
			t.Fatalf("commit the fact %s: %v", key, err)
		}
		if original == nil {
			return
		}
		if err := tier.Originals.Put(ctx, ledger.OriginalRef{
			Key:             key,
			Owner:           owner,
			Format:          "text",
			RetentionPolicy: "default",
		}, original); err != nil {
			t.Fatalf("commit the original of %s: %v", key, err)
		}
	}
	commit(settled, 1, 1, "fact", factBody, originalBody)
	commit(live, 2, 1, "checkpoint", []byte(`{"fold":true}`), []byte("the fold"))

	report, err := tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{live: {}})
	if err != nil {
		t.Fatalf("relocate the settled fact: %v", err)
	}
	if report.Settled == 0 {
		t.Fatalf("the relocation settled no key, want the settled fact this fixture commits")
	}
	hot, err := tier.Store.Keys(ctx, "")
	if err != nil {
		t.Fatalf("enumerate the hot journal after the pass: %v", err)
	}
	for _, key := range []string{"ref:" + string(settled), "blob:" + string(settled)} {
		for _, held := range hot {
			if held == key {
				t.Fatalf("the hot journal still holds %q after the pass, want the reference and the body of a settled fact moved together", key)
			}
		}
	}
	if err := tier.Segments.Close(); err != nil {
		t.Fatalf("close the journal after the pass: %v", err)
	}

	// The moved original still reads by identity from the root the command scans, so
	// the command below meets a root whose history is kept and not a root missing it.
	reopened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the relocated root: %v", err)
	}
	defer reopened.Segments.Close()
	original := reopened.Originals.Get(ctx, settled)
	if original.Outcome != ledger.RecallFound || !bytes.Equal(original.Body, originalBody) {
		t.Fatalf("the original of the moved fact reads as %v, want the bytes it was committed with", original)
	}

	// The consumer: the shipped command, built and run with the flags an operator runs.
	moduleRoot := moduleRootOf(t)
	binary := filepath.Join(t.TempDir(), "recover-kv")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./scripts/recover-kv")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build scripts/recover-kv: %v\n%s", err, out)
	}
	run := exec.CommandContext(ctx, binary, "-root", root)
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("recover-kv -root %s: %v\nstdout=%s\nstderr=%s", root, err, stdout.String(), stderr.String())
	}
	if want := "nothing to recover, every reference names its independent body"; !strings.Contains(stdout.String(), want) {
		t.Fatalf("recover-kv reported %q, want the line %q: a relocated root carries no orphan reference", stdout.String(), want)
	}
}

// moduleRootOf answers the directory holding the go.mod of this module, so a case
// builds the shipped command from the repository it belongs to.
func moduleRootOf(t *testing.T) string {
	t.Helper()
	_, file, _, held := runtime.Caller(0)
	if !held {
		t.Fatal("locate the test file, want the module root it belongs to")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("walk up from %s to the module root, want the directory holding go.mod", file)
		}
		dir = parent
	}
}
