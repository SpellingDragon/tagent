package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

var (
	_ state.OriginalMigrator  = (*memory.ArchiveOriginals)(nil)
	_ state.OriginalReader    = (*memory.ArchiveOriginals)(nil)
	_ state.ArchiveTaskWriter = (*state.DurableCommit)(nil)
	_ state.ArchiveTaskReader = (*state.DurableCommit)(nil)
)

// fixtureArchive mirrors the old program's checkpoint shape: agent definitions
// beside the task records each agent carries.
type fixtureArchive struct {
	Agents []fixtureAgent
}

// fixtureAgent is one agent record of the fixture checkpoint.
type fixtureAgent struct {
	Definition fixtureDefinition
	Tasks      []fixtureTask
}

// fixtureDefinition is the agent identity part of the fixture checkpoint.
type fixtureDefinition struct {
	ID string
}

// fixtureTask is one old task record of the fixture checkpoint.
type fixtureTask struct {
	ID      int
	OwnerID string
	Source  string
	Call    fixtureCall
	Status  string
	Output  string
	Error   string
}

// fixtureCall is the tool call one fixture task record answers.
type fixtureCall struct {
	Name string
	Args string
}

// dumpBytes is the full body the fixture's external dump holds.
const dumpBytes = "the full oversized result\nsecond line\n"

// errInjected is the failure the injected interruption reports.
var errInjected = errors.New("acceptance: injected interruption")

// faultLog is a fact log that refuses writes beyond a budget, so an import is
// interrupted with part of its facts stored.
type faultLog struct {
	inner  state.FactLog
	writes int
	budget int
}

// Put stages one write while the budget holds and refuses once it does not.
func (f *faultLog) Put(ctx context.Context, key string, value []byte) error {
	if f.writes >= f.budget {
		return errInjected
	}
	f.writes++
	return f.inner.Put(ctx, key, value)
}

// Get returns the value held under key on the wrapped log.
func (f *faultLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return f.inner.Get(ctx, key)
}

// Scan returns every pair under the prefix on the wrapped log.
func (f *faultLog) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	return f.inner.Scan(ctx, prefix)
}

// Sync completes the barrier on the wrapped log.
func (f *faultLog) Sync(ctx context.Context) error {
	return f.inner.Sync(ctx)
}

// faultOriginals is an original face that refuses puts beyond a budget, so an
// import is interrupted with part of its originals stored.
type faultOriginals struct {
	inner  state.OriginalMigrator
	writes int
	budget int
}

// PutOriginal stores one original while the budget holds and refuses once it
// does not.
func (f *faultOriginals) PutOriginal(ctx context.Context, key event.EventID, owner event.Address, format, retention, digest string, length int64, body []byte) error {
	if f.writes >= f.budget {
		return errInjected
	}
	f.writes++
	return f.inner.PutOriginal(ctx, key, owner, format, retention, digest, length, body)
}

// GetOriginal returns the bytes held under key on the wrapped face.
func (f *faultOriginals) GetOriginal(ctx context.Context, key event.EventID) ([]byte, bool, error) {
	return f.inner.GetOriginal(ctx, key)
}

// pathInventory is the byte-level state one new path holds for a report: the
// rendered fact chain of every migrated task beside the bytes of every
// migrated original.
type pathInventory struct {
	Facts     map[event.TaskID]string
	Originals map[event.EventID]string
}

// newInventory renders the new-path state one report covers, so two runs are
// compared by what they actually stored.
func newInventory(t *testing.T, ctx context.Context, commit *state.DurableCommit, originals state.OriginalReader, report state.MigrationReport) pathInventory {
	t.Helper()
	inventory := pathInventory{Facts: map[event.TaskID]string{}, Originals: map[event.EventID]string{}}
	for _, migrated := range report.Tasks {
		chain, err := commit.Records(ctx, migrated.TaskID)
		if err != nil {
			t.Fatalf("Records(%s): %v", migrated.TaskID, err)
		}
		rendered := ""
		for _, link := range chain {
			body, err := task.EncodeTaskRecord(link)
			if err != nil {
				t.Fatalf("EncodeTaskRecord: %v", err)
			}
			rendered += string(body) + "\n"
		}
		inventory.Facts[migrated.TaskID] = rendered
	}
	for _, migrated := range report.Originals {
		body, found, err := originals.GetOriginal(ctx, migrated.Key)
		if err != nil {
			t.Fatalf("GetOriginal(%s): %v", migrated.Key, err)
		}
		if found {
			inventory.Originals[migrated.Key] = string(body)
		}
	}
	return inventory
}

// openNewPath opens one durable new path over a fresh directory: the task
// commit carrier and the original face of the same tier.
func openNewPath(t *testing.T) (*state.DurableCommit, *memory.ArchiveOriginals) {
	t.Helper()
	tier, err := ledger.OpenTier(context.Background(), ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("OpenTier: %v", err)
	}
	commit, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("NewDurableCommit: %v", err)
	}
	return commit, memory.NewArchiveOriginals(tier.Originals)
}

// buildArchive writes one old archive under a fresh directory and returns that
// directory: a checkpoint with recoverable tasks beside named gaps, and one
// external dump whose file is present.
func buildArchive(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	snapshot := fixtureArchive{Agents: []fixtureAgent{
		{Definition: fixtureDefinition{ID: "agent-a"}, Tasks: []fixtureTask{
			{ID: 1, OwnerID: "agent-a", Source: "user", Call: fixtureCall{Name: "shell", Args: "ls"}, Status: "completed", Output: "total 3\n"},
			{ID: 2, OwnerID: "agent-a", Source: "user", Call: fixtureCall{Name: "shell", Args: "cat big"}, Status: "failed", Output: "tail view\ntool-output/task-2-1.txt", Error: "exit 1"},
			{ID: 3, OwnerID: "agent-a", Source: "user", Call: fixtureCall{Name: "shell", Args: "cat lost"}, Status: "completed", Output: "tail view\ntool-output/task-3-1.txt"},
			{ID: 4, OwnerID: "agent-a", Source: "user", Call: fixtureCall{Name: "shell", Args: "run"}, Status: "running"},
			{ID: 5, OwnerID: "agent-a", Source: "user", Call: fixtureCall{Name: "shell", Args: "stale"}, Status: "unknown"},
			{ID: 6, OwnerID: "agent-x", Source: "user", Call: fixtureCall{Name: "shell", Args: "misowned"}, Status: "completed"},
			{ID: 7, OwnerID: "agent-a", Source: "user", Call: fixtureCall{Name: "shell", Args: "run lost"}, Status: "running", Output: "tail view\ntool-output/task-7-1.txt"},
		}},
		{Definition: fixtureDefinition{ID: "agent-b"}, Tasks: []fixtureTask{
			{ID: 1, OwnerID: "agent-b", Source: "user", Call: fixtureCall{Name: "shell", Args: "boom"}, Status: "failed", Output: "boom"},
		}},
	}}
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal fixture checkpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "checkpoint.json"), body, 0o644); err != nil {
		t.Fatalf("write fixture checkpoint: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tool-output"), 0o755); err != nil {
		t.Fatalf("make fixture dump directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tool-output", "task-2-1.txt"), []byte(dumpBytes), 0o644); err != nil {
		t.Fatalf("write fixture dump: %v", err)
	}
	return root
}

// TestOfflineMigration drives the read-only archive import and its write-free rollback in the four G2 sequences.
//
//   - G2.1 源字节不变: the archive digest holds, imported bytes read back equal, and no new format lands in the old path
//   - G2.2 缺口具名: a missing external dump and an unrecoverable task are named and refused, never skipped silently
//   - G2.3 中断重跑: an interrupted import reruns to the same report and the same new-path inventory, repeat included
//   - G2.4 回退只读: the rollback reads old data back through the new path and fakes no restore side effect
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestOfflineMigration(t *testing.T) {
	ctx := context.Background()

	t.Run("G2.1 源字节不变: archive bytes hold and the new format lands only in the new path", func(t *testing.T) {
		root := buildArchive(t)
		before, err := state.ArchiveDigest(root)
		if err != nil {
			t.Fatalf("ArchiveDigest before: %v", err)
		}
		commit, originals := openNewPath(t)
		report, err := state.MigrateArchive(ctx, root, commit, originals)
		if err != nil {
			t.Fatalf("MigrateArchive: %v", err)
		}
		after, err := state.ArchiveDigest(root)
		if err != nil {
			t.Fatalf("ArchiveDigest after: %v", err)
		}
		if after != before {
			t.Fatalf("archive digest after migration = %s, want unchanged %s", after, before)
		}
		if report.SourceDigest != before {
			t.Fatalf("report source digest = %s, want %s", report.SourceDigest, before)
		}
		sum := sha256.Sum256([]byte(dumpBytes))
		wantDigest := "sha256:" + hex.EncodeToString(sum[:])
		var imported *state.MigratedOriginal
		for i := range report.Originals {
			if report.Originals[i].Source == "tool-output/task-2-1.txt" {
				imported = &report.Originals[i]
			}
		}
		if imported == nil {
			t.Fatalf("report originals = %+v, want one entry for tool-output/task-2-1.txt", report.Originals)
		}
		if imported.Digest != wantDigest {
			t.Fatalf("imported digest = %s, want independently computed %s", imported.Digest, wantDigest)
		}
		if imported.Length != int64(len(dumpBytes)) {
			t.Fatalf("imported length = %d, want %d", imported.Length, len(dumpBytes))
		}
		stored, found, err := originals.GetOriginal(ctx, imported.Key)
		if err != nil || !found {
			t.Fatalf("GetOriginal(%s) = found %v, err %v, want found", imported.Key, found, err)
		}
		if !bytes.Equal(stored, []byte(dumpBytes)) {
			t.Fatalf("new-path read-back differs from the archive dump bytes")
		}
		files := map[string]bool{}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				files[filepath.ToSlash(rel)] = true
			}
			return nil
		}); err != nil {
			t.Fatalf("walk archive root: %v", err)
		}
		wantFiles := map[string]bool{"checkpoint.json": true, "tool-output/task-2-1.txt": true}
		if !reflect.DeepEqual(files, wantFiles) {
			t.Fatalf("archive files after migration = %v, want %v", files, wantFiles)
		}
	})

	t.Run("G2.2 缺口具名: missing dumps and unrecoverable tasks are named and refused", func(t *testing.T) {
		root := buildArchive(t)
		commit, originals := openNewPath(t)
		report, err := state.MigrateArchive(ctx, root, commit, originals)
		if err != nil {
			t.Fatalf("MigrateArchive: %v", err)
		}
		wantGaps := map[state.MigrationGap]bool{
			{Kind: state.GapMissingExternalFile, Name: "tool-output/task-3-1.txt", Detail: "referenced by agent-a#3"}:                true,
			{Kind: state.GapMissingExternalFile, Name: "tool-output/task-7-1.txt", Detail: "referenced by agent-a#7"}:                true,
			{Kind: state.GapUnrecoverableTask, Name: "agent-a#4", Detail: "status running carries no terminal outcome"}:              true,
			{Kind: state.GapUnrecoverableTask, Name: "agent-a#5", Detail: "status unknown carries no terminal outcome"}:              true,
			{Kind: state.GapUnrecoverableTask, Name: "agent-a#6", Detail: "owner agent-x does not match the carrying agent agent-a"}: true,
			{Kind: state.GapUnrecoverableTask, Name: "agent-a#7", Detail: "status running carries no terminal outcome"}:              true,
		}
		gotGaps := map[state.MigrationGap]bool{}
		for _, gap := range report.Gaps {
			gotGaps[gap] = true
		}
		if !reflect.DeepEqual(gotGaps, wantGaps) {
			t.Fatalf("report gaps = %+v, want %+v", report.Gaps, wantGaps)
		}
		wantTasks := map[event.TaskID]task.Status{
			"archive:agent-a#1": task.StatusSettled,
			"archive:agent-a#2": task.StatusFailed,
			"archive:agent-b#1": task.StatusFailed,
		}
		if len(report.Tasks) != len(wantTasks) {
			t.Fatalf("report tasks = %+v, want %d entries", report.Tasks, len(wantTasks))
		}
		for _, migrated := range report.Tasks {
			if wantTasks[migrated.TaskID] != migrated.Status {
				t.Fatalf("migrated task %+v, want status %v", migrated, wantTasks[migrated.TaskID])
			}
			if facts := commit.Facts(migrated.TaskID); len(facts) != 1 {
				t.Fatalf("facts of %s = %d, want exactly one imported fact", migrated.TaskID, len(facts))
			}
		}
		for _, refused := range []event.TaskID{"archive:agent-a#3", "archive:agent-a#4", "archive:agent-a#5", "archive:agent-a#6", "archive:agent-a#7"} {
			if facts := commit.Facts(refused); len(facts) != 0 {
				t.Fatalf("refused task %s carries %d facts, want none", refused, len(facts))
			}
		}
	})

	t.Run("G2.3 中断重跑: an interrupted import reruns to the report and inventory of an uninterrupted one", func(t *testing.T) {
		referenceRoot := buildArchive(t)
		referenceCommit, referenceOriginals := openNewPath(t)
		reference, err := state.MigrateArchive(ctx, referenceRoot, referenceCommit, referenceOriginals)
		if err != nil {
			t.Fatalf("uninterrupted MigrateArchive: %v", err)
		}
		referenceInventory := newInventory(t, ctx, referenceCommit, referenceOriginals, reference)

		dumpRoot := buildArchive(t)
		dumpTier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		dumpCommit, err := state.NewDurableCommit(ledger.TierFactLog(dumpTier), nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		dumpFault := &faultOriginals{inner: memory.NewArchiveOriginals(dumpTier.Originals), budget: 0}
		if _, err := state.MigrateArchive(ctx, dumpRoot, dumpCommit, dumpFault); !errors.Is(err, errInjected) {
			t.Fatalf("interrupted MigrateArchive err = %v, want %v", err, errInjected)
		}
		dumpOriginals := memory.NewArchiveOriginals(dumpTier.Originals)
		dumpReport, err := state.MigrateArchive(ctx, dumpRoot, dumpCommit, dumpOriginals)
		if err != nil {
			t.Fatalf("rerun after dump interruption: %v", err)
		}
		if !reflect.DeepEqual(dumpReport, reference) {
			t.Fatalf("rerun report = %+v, want %+v", dumpReport, reference)
		}
		if got := newInventory(t, ctx, dumpCommit, dumpOriginals, dumpReport); !reflect.DeepEqual(got, referenceInventory) {
			t.Fatalf("rerun inventory = %+v, want %+v", got, referenceInventory)
		}

		logRoot := buildArchive(t)
		logTier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
		if err != nil {
			t.Fatalf("OpenTier: %v", err)
		}
		logFault := &faultLog{inner: ledger.TierFactLog(logTier), budget: 2}
		logCommit, err := state.NewDurableCommit(logFault, nil)
		if err != nil {
			t.Fatalf("NewDurableCommit: %v", err)
		}
		logOriginals := memory.NewArchiveOriginals(logTier.Originals)
		if _, err := state.MigrateArchive(ctx, logRoot, logCommit, logOriginals); !errors.Is(err, errInjected) {
			t.Fatalf("interrupted MigrateArchive err = %v, want %v", err, errInjected)
		}
		logCommit, err = state.NewDurableCommit(ledger.TierFactLog(logTier), nil)
		if err != nil {
			t.Fatalf("rebuild NewDurableCommit: %v", err)
		}
		logReport, err := state.MigrateArchive(ctx, logRoot, logCommit, logOriginals)
		if err != nil {
			t.Fatalf("rerun after fact interruption: %v", err)
		}
		if !reflect.DeepEqual(logReport, reference) {
			t.Fatalf("rerun report = %+v, want %+v", logReport, reference)
		}
		if got := newInventory(t, ctx, logCommit, logOriginals, logReport); !reflect.DeepEqual(got, referenceInventory) {
			t.Fatalf("rerun inventory = %+v, want %+v", got, referenceInventory)
		}

		repeat, err := state.MigrateArchive(ctx, logRoot, logCommit, logOriginals)
		if err != nil {
			t.Fatalf("repeat MigrateArchive: %v", err)
		}
		if !reflect.DeepEqual(repeat, logReport) {
			t.Fatalf("repeat report = %+v, want %+v", repeat, logReport)
		}
		if got := newInventory(t, ctx, logCommit, logOriginals, repeat); !reflect.DeepEqual(got, referenceInventory) {
			t.Fatalf("repeat inventory = %+v, want %+v", got, referenceInventory)
		}
	})

	t.Run("G2.4 回退只读: the rollback reads old data back through the new path and fakes no restore side effect", func(t *testing.T) {
		root := buildArchive(t)
		commit, originals := openNewPath(t)
		report, err := state.MigrateArchive(ctx, root, commit, originals)
		if err != nil {
			t.Fatalf("MigrateArchive: %v", err)
		}
		before, err := state.ArchiveDigest(root)
		if err != nil {
			t.Fatalf("ArchiveDigest before: %v", err)
		}
		inventoryBefore := newInventory(t, ctx, commit, originals, report)
		rollback, err := state.RollbackArchive(ctx, root, commit, originals)
		if err != nil {
			t.Fatalf("RollbackArchive: %v", err)
		}
		if rollback.SourceDigest != before {
			t.Fatalf("rollback source digest = %s, want %s", rollback.SourceDigest, before)
		}
		after, err := state.ArchiveDigest(root)
		if err != nil {
			t.Fatalf("ArchiveDigest after: %v", err)
		}
		if after != before {
			t.Fatalf("archive digest after rollback = %s, want unchanged %s", after, before)
		}
		if got := newInventory(t, ctx, commit, originals, report); !reflect.DeepEqual(got, inventoryBefore) {
			t.Fatalf("new path changed across rollback: %+v want %+v", got, inventoryBefore)
		}
		if len(rollback.Items) != len(report.Tasks)+len(report.Originals) {
			t.Fatalf("rollback items = %+v, want one per migrated task and original", rollback.Items)
		}
		for _, item := range rollback.Items {
			if !item.Present || !item.Verified {
				t.Fatalf("rollback item %+v, want present and verified against the archive", item)
			}
		}

		emptyCommit, emptyOriginals := openNewPath(t)
		emptyRollback, err := state.RollbackArchive(ctx, root, emptyCommit, emptyOriginals)
		if err != nil {
			t.Fatalf("RollbackArchive over an empty new path: %v", err)
		}
		if len(emptyRollback.Items) != len(report.Tasks)+len(report.Originals) {
			t.Fatalf("empty rollback items = %+v, want one per migrated item", emptyRollback.Items)
		}
		for _, item := range emptyRollback.Items {
			if item.Present || item.Verified {
				t.Fatalf("empty rollback item %+v, want named absent with no write-back", item)
			}
		}
	})
}
