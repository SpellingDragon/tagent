// One storage root carries one writer at the storage seam: a write-rights open
// admits one writer per root, so a second such open never lands a body over the
// slot the first one's index points at.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// secondWriterRootEnv names the root a re-executed test binary opens as the
// second writer of one storage root.
const secondWriterRootEnv = "TAGENT_RECOVERY_11R2_WRITER_ROOT"

// secondWriterSlot is the window and sequence both writers of the anchor commit
// at: one slot carries one body, so a second commit over it states whether the
// root admitted a second writer.
const secondWriterSlot = 1

// secondWriterAdmitted marks a child process whose open of the root succeeded.
const secondWriterAdmitted = "admitted"

// TestOpenTierRefusesHalfArchiveConfig pins the public tier's archive declaration gate.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestOpenTierRefusesHalfArchiveConfig(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*StorageConfig)
	}{
		{name: "budget without root", set: func(c *StorageConfig) { c.HotLogMaxBytes = 1 }},
		{name: "root without budget", set: func(c *StorageConfig) {
			c.WriteRights = true
			c.ArchiveRoot = t.TempDir()
		}},
		{name: "age without root", set: func(c *StorageConfig) { c.ArchiveMaxAge = time.Hour }},
		{name: "negative budget", set: func(c *StorageConfig) {
			c.ArchiveRoot = t.TempDir()
			c.HotLogMaxBytes = -1
		}},
		{name: "negative age", set: func(c *StorageConfig) {
			c.ArchiveRoot = t.TempDir()
			c.HotLogMaxBytes = 1
			c.ArchiveMaxAge = -time.Hour
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := StorageConfig{Type: StorageJournal, Root: t.TempDir()}
			test.set(&cfg)
			tier, err := OpenTier(context.Background(), cfg)
			if tier != nil {
				tier.Close()
			}
			if !errors.Is(err, ErrArchiveConfig) {
				t.Fatalf("OpenTier with %s = (%v, %v), want ErrArchiveConfig", test.name, tier, err)
			}
		})
	}
}

// TestOpenTierReadOnlyViewDeclaresArchiveRootAlone pins the archive root as a read source.
//   - a view holding no write rights opens with archive_root alone and no hot log budget
//   - a key the hot log does not state reads back through the archive root
//   - the same view still holds no write rights, so the writer's pairing gate is unchanged
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestOpenTierReadOnlyViewDeclaresArchiveRootAlone(t *testing.T) {
	ctx := context.Background()
	root, archiveRoot := t.TempDir(), t.TempDir()
	writerCfg := StorageConfig{Type: StorageJournal, Root: root, WriteRights: true}
	writerCfg.ArchiveRoot = archiveRoot
	writerCfg.HotLogMaxBytes = 1
	writer, err := OpenTier(ctx, writerCfg)
	if err != nil {
		t.Fatalf("open the archive-backed writer: %v", err)
	}
	t.Cleanup(func() { writer.Close() })
	archived := segmentRecord(7, 1000, 1, "read-only-archive-event", 256)
	hot := segmentRecord(7, 1001, 2, "read-only-hot-event", 256)
	if err := writer.Segments.StoreEvent(ctx, archived); err != nil {
		t.Fatalf("store the archived event: %v", err)
	}
	if err := writer.Store.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:7:1", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the first checkpoint: %v", err)
	}
	if err := writer.Segments.StoreEvent(ctx, hot); err != nil {
		t.Fatalf("store the hot event: %v", err)
	}
	if err := writer.Store.Commit(ctx, Batch{Puts: []Pair{{Key: "checkpoint:7:2", Value: []byte("cut")}}}); err != nil {
		t.Fatalf("commit the second checkpoint: %v", err)
	}
	moved, err := filepath.Glob(filepath.Join(archiveRoot, journalSegmentDirName, "seg-*.log"))
	if err != nil || len(moved) != 1 {
		t.Fatalf("the archive segments = %v err=%v, want one moved interval", moved, err)
	}
	readerCfg := StorageConfig{Type: StorageJournal, Root: root}
	readerCfg.ArchiveRoot = archiveRoot
	reader, err := OpenTier(ctx, readerCfg)
	if err != nil {
		t.Fatalf("open the view naming archive_root alone: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	stored, held, err := reader.Segments.GetEvent(ctx, archived.EventKey)
	if err != nil || !held || !bytes.Equal(stored.Body, archived.Body) {
		t.Fatalf("the archived event read through the read-only view = (%t, %v), want its committed bytes", held, err)
	}
	if err := reader.Store.Commit(ctx, Batch{Puts: []Pair{{Key: "read-only:write", Value: []byte("no")}}}); !errors.Is(err, ErrCommitWithoutWriteRights) {
		t.Fatalf("a commit through the view naming archive_root alone = %v, want ErrCommitWithoutWriteRights", err)
	}
}

// TestArchiveMaxAgeReachesJournalView pins the storage configuration's archive window.
//   - OpenAtomicStore passes the configured duration to the journal view unchanged
//   - an omitted duration leaves the view's archive age at zero
func TestArchiveMaxAgeReachesJournalView(t *testing.T) {
	for _, age := range []time.Duration{0, 2 * time.Hour} {
		cfg := StorageConfig{Type: StorageJournal, Root: t.TempDir()}
		cfg.ArchiveMaxAge = age
		store, err := OpenAtomicStore(context.Background(), cfg)
		if err != nil {
			t.Fatalf("open the store with archive age %s: %v", age, err)
		}
		kv, ok := store.(*journalKV)
		if !ok {
			t.Fatalf("OpenAtomicStore returned %T, want the journal view", store)
		}
		if kv.archiveMaxAge != age {
			kv.log.close()
			t.Fatalf("the journal view states archive age %s, want %s", kv.archiveMaxAge, age)
		}
		if err := kv.log.close(); err != nil {
			t.Fatalf("close the store with archive age %s: %v", age, err)
		}
	}
}

// TestSecondProcessOnOneRootIsRefusedWriteRights pins the one-writer admission of one storage root across two processes.
//   - the second process is refused the root by name, so it never lands a body over the slot the first writer's index points at
//   - the first writer's identity answers its own bytes after a reopen
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestSecondProcessOnOneRootIsRefusedWriteRights(t *testing.T) {
	if root := os.Getenv(secondWriterRootEnv); root != "" {
		writeAsSecondProcess(root)
		return
	}
	ctx := context.Background()
	root := t.TempDir()
	bodyA := []byte("the body the first process committed under its own identity")
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the first writer: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, SegmentRecord{
		PartitionID: secondWriterSlot,
		WindowTS:    int64(secondWriterSlot),
		Seq:         int64(secondWriterSlot),
		EventKey:    event.EventID("11r2-writer-a"),
		EventTime:   int64(secondWriterSlot),
		Body:        bodyA,
	}); err != nil {
		t.Fatalf("commit the first writer's body: %v", err)
	}
	out := runSecondWriter(t, root)
	if err := tier.Segments.Close(); err != nil {
		t.Fatalf("close the first writer: %v", err)
	}
	reopened, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the root: %v", err)
	}
	defer reopened.Segments.Close()
	stored, held, err := reopened.Segments.GetEvent(ctx, event.EventID("11r2-writer-a"))
	if err != nil || !held {
		t.Fatalf("read the first writer's identity = (held=%t, %v)", held, err)
	}
	if string(stored.Body) != string(bodyA) {
		t.Fatalf("the first writer's identity answers %q, want its own body: a second writer landed the shared slot (%s)", stored.Body, out)
	}
	if strings.Contains(out, secondWriterAdmitted) {
		t.Fatalf("the second process opened one root for write rights: %s", out)
	}
}

// writeAsSecondProcess opens root for write rights and states what the open
// answered: the refusal, or the body it landed over the first writer's slot.
func writeAsSecondProcess(root string) {
	ctx := context.Background()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		fmt.Printf("refused: %v\n", err)
		return
	}
	if err := tier.Segments.StoreEvent(ctx, SegmentRecord{
		PartitionID: secondWriterSlot,
		WindowTS:    int64(secondWriterSlot),
		Seq:         int64(secondWriterSlot),
		EventKey:    event.EventID("11r2-writer-b"),
		EventTime:   int64(secondWriterSlot),
		Body:        []byte("the body the second process landed over the shared slot"),
	}); err != nil {
		fmt.Printf("commit refused: %v\n", err)
		return
	}
	fmt.Println(secondWriterAdmitted)
}

// runSecondWriter re-executes this test binary as the second process over root
// and answers what it stated. The child commits at the first writer's slot when
// its open is admitted, so an admitted second writer overwrites the body the
// first writer's index points at.
func runSecondWriter(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSecondProcessOnOneRootIsRefusedWriteRights$")
	cmd.Env = append(os.Environ(), secondWriterRootEnv+"="+root)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		t.Fatalf("the second process failed (exit %d): %s", exit.ExitCode(), output)
	} else if err != nil {
		t.Fatalf("start the second process: %v", err)
	}
	return string(output)
}

// TestWriteRightsAdmissionReportsTheRootByItsPath pins what the refusal states and what it leaves alone.
//   - a second write-rights open of one root answers ErrRootWriteLocked and names the root it could not take
//   - a view that declares no write rights still opens and reads that root
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestWriteRightsAdmissionReportsTheRootByItsPath(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writer, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writer: %v", err)
	}
	defer writer.Segments.Close()
	_, err = OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if !errors.Is(err, ErrRootWriteLocked) {
		t.Fatalf("second write-rights open of one root = %v, want ErrRootWriteLocked", err)
	}
	if !strings.Contains(err.Error(), root) {
		t.Fatalf("the refusal names %q, want the root %q", err.Error(), root)
	}
	reader, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open a view that declares no write rights beside the writer: %v", err)
	}
	if _, err := reader.Keys(ctx, ""); err != nil {
		t.Fatalf("read the root beside its writer: %v", err)
	}
}
