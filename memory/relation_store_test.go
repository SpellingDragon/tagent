package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestRelationStore(t *testing.T) *InMemRelationStore {
	t.Helper()
	dir := t.TempDir()
	rs, err := NewInMemRelationStore(dir)
	if err != nil {
		t.Fatalf("Failed to create InMemRelationStore: %v", err)
	}
	t.Cleanup(func() { rs.Close() })
	return rs
}

func TestSetParent_GetParent(t *testing.T) {
	rs := newTestRelationStore(t)

	// Set parent-child relationship
	err := rs.SetParent(200, 100)
	if err != nil {
		t.Fatalf("SetParent failed: %v", err)
	}

	// Verify GetParent
	parent, err := rs.GetParent(200)
	if err != nil {
		t.Fatalf("GetParent failed: %v", err)
	}
	if parent != 100 {
		t.Errorf("GetParent(200) = %d, want 100", parent)
	}
}

func TestGetChildren(t *testing.T) {
	rs := newTestRelationStore(t)

	// Set multiple children for same parent
	rs.SetParent(200, 100)
	rs.SetParent(300, 100)
	rs.SetParent(400, 100)

	// Verify GetChildren
	children, err := rs.GetChildren(100)
	if err != nil {
		t.Fatalf("GetChildren failed: %v", err)
	}
	if len(children) != 3 {
		t.Errorf("GetChildren(100) = %d children, want 3", len(children))
	}
}

func TestUpdateParent(t *testing.T) {
	rs := newTestRelationStore(t)

	// Initial relationship
	rs.SetParent(300, 200)
	rs.SetParent(200, 100)

	// Update parent (compress scenario)
	rs.SetParent(300, 100)

	// Verify new parent
	parent, _ := rs.GetParent(300)
	if parent != 100 {
		t.Errorf("GetParent(300) after update = %d, want 100", parent)
	}

	// Old parent should no longer list 300 as child
	children200, _ := rs.GetChildren(200)
	for _, c := range children200 {
		if c == 300 {
			t.Error("GetChildren(200) should not contain 300 after parent update")
		}
	}
}

func TestGetParentRootEvent(t *testing.T) {
	rs := newTestRelationStore(t)

	parent, err := rs.GetParent(999)
	if err != nil {
		t.Fatalf("GetParent failed: %v", err)
	}
	if parent != 0 {
		t.Errorf("GetParent(999) for root event = %d, want 0", parent)
	}
}

func TestGetChildrenLeafEvent(t *testing.T) {
	rs := newTestRelationStore(t)

	children, err := rs.GetChildren(999)
	if err != nil {
		t.Fatalf("GetChildren failed: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("GetChildren(999) for leaf event = %d, want 0", len(children))
	}
}

func TestGetParents(t *testing.T) {
	rs := newTestRelationStore(t)

	rs.SetParent(200, 100)
	rs.SetParent(300, 200)
	rs.SetParent(400, 100)

	parents, err := rs.GetParents([]int64{200, 300, 400, 999})
	if err != nil {
		t.Fatalf("GetParents failed: %v", err)
	}

	if parents[200] != 100 {
		t.Errorf("parents[200] = %d, want 100", parents[200])
	}
	if parents[300] != 200 {
		t.Errorf("parents[300] = %d, want 200", parents[300])
	}
	if parents[400] != 100 {
		t.Errorf("parents[400] = %d, want 100", parents[400])
	}
	if parents[999] != 0 {
		t.Errorf("parents[999] = %d, want 0", parents[999])
	}
}

func TestRemoveRelations(t *testing.T) {
	rs := newTestRelationStore(t)

	rs.SetParent(200, 100)
	rs.SetParent(300, 200)
	rs.SetParent(400, 200)

	// Remove 200's relations
	err := rs.RemoveRelations(200)
	if err != nil {
		t.Fatalf("RemoveRelations failed: %v", err)
	}

	// 200 should have no parent
	parent, _ := rs.GetParent(200)
	if parent != 0 {
		t.Errorf("GetParent(200) after remove = %d, want 0", parent)
	}

	// 100 should no longer have 200 as child
	children100, _ := rs.GetChildren(100)
	for _, c := range children100 {
		if c == 200 {
			t.Error("GetChildren(100) should not contain 200 after remove")
		}
	}

	// 200's children (300, 400) still reference 200 as parent until compaction repairs them
	parent300, _ := rs.GetParent(300)
	if parent300 != 200 {
		t.Errorf("GetParent(300) after parent removal = %d, want 200 (dangling ref not yet repaired)", parent300)
	}
}

func TestSnapshotAndLoad(t *testing.T) {
	rs := newTestRelationStore(t)

	rs.SetParent(200, 100)
	rs.SetParent(300, 200)
	rs.SetParent(400, 100)

	// Snapshot
	snapshot, err := rs.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	if len(snapshot) != 3 {
		t.Errorf("Snapshot count = %d, want 3", len(snapshot))
	}

	// Load into new store
	rs2 := newTestRelationStore(t)
	err = rs2.LoadSnapshot(snapshot)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	// Verify
	parent, _ := rs2.GetParent(200)
	if parent != 100 {
		t.Errorf("After snapshot load, GetParent(200) = %d, want 100", parent)
	}
	children, _ := rs2.GetChildren(100)
	if len(children) != 2 {
		t.Errorf("After snapshot load, GetChildren(100) = %d, want 2", len(children))
	}
}

func TestReplayJournal(t *testing.T) {
	rs := newTestRelationStore(t)

	entries := []JournalEntry{
		{Op: "+1", ChildKey: 200, ParentKey: 100},
		{Op: "+1", ChildKey: 300, ParentKey: 200},
		{Op: "+1", ChildKey: 400, ParentKey: 100},
	}

	err := rs.ReplayJournal(entries)
	if err != nil {
		t.Fatalf("ReplayJournal failed: %v", err)
	}

	parent, _ := rs.GetParent(200)
	if parent != 100 {
		t.Errorf("After replay, GetParent(200) = %d, want 100", parent)
	}
}

func TestSaveSnapshotToFile(t *testing.T) {
	dir := t.TempDir()
	rs, err := NewInMemRelationStore(dir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer rs.Close()

	rs.SetParent(200, 100)
	rs.SetParent(300, 200)

	// Save snapshot
	err = rs.SaveSnapshotToFile()
	if err != nil {
		t.Fatalf("SaveSnapshotToFile failed: %v", err)
	}

	// Verify snapshot file exists
	snapPath := filepath.Join(dir, "relations.snap")
	if _, err := os.Stat(snapPath); os.IsNotExist(err) {
		t.Fatalf("Snapshot file not created: %s", snapPath)
	}

	// Verify journal is truncated
	journalPath := filepath.Join(dir, "relations.journal")
	info, _ := os.Stat(journalPath)
	if info.Size() != 0 {
		t.Errorf("Journal not truncated after snapshot, size = %d", info.Size())
	}
}

func TestRecoveryFromSnapshot(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: create store, add some relationships, save snapshot
	rs1, err := NewInMemRelationStore(dir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	rs1.SetParent(200, 100)
	rs1.SetParent(300, 200)
	rs1.SaveSnapshotToFile()
	rs1.Close()

	// Phase 2: add more relationships after snapshot
	rs2, err := NewInMemRelationStore(dir)
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	rs2.SetParent(400, 300)
	rs2.Close()

	// Phase 3: new instance should recover all relationships
	rs3, err := NewInMemRelationStore(dir)
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer rs3.Close()

	// Verify all relationships recovered
	parent200, _ := rs3.GetParent(200)
	if parent200 != 100 {
		t.Errorf("After recovery, GetParent(200) = %d, want 100", parent200)
	}
	parent400, _ := rs3.GetParent(400)
	if parent400 != 300 {
		t.Errorf("After recovery, GetParent(400) = %d, want 300", parent400)
	}
}

func TestEventsCount(t *testing.T) {
	rs := newTestRelationStore(t)

	if rs.EventsCount() != 0 {
		t.Errorf("Initial EventsCount = %d, want 0", rs.EventsCount())
	}

	rs.SetParent(200, 100)
	rs.SetParent(300, 200)
	rs.SetParent(400, 100)

	if rs.EventsCount() != 3 {
		t.Errorf("EventsCount after adds = %d, want 3", rs.EventsCount())
	}

	rs.RemoveRelations(200)
	if rs.EventsCount() != 2 {
		t.Errorf("EventsCount after remove = %d, want 2", rs.EventsCount())
	}
}

func TestIdempotentSetParent(t *testing.T) {
	rs := newTestRelationStore(t)

	// Set same parent twice - should be idempotent
	err := rs.SetParent(200, 100)
	if err != nil {
		t.Fatalf("First SetParent failed: %v", err)
	}
	err = rs.SetParent(200, 100)
	if err != nil {
		t.Fatalf("Idempotent SetParent failed: %v", err)
	}

	parent, _ := rs.GetParent(200)
	if parent != 100 {
		t.Errorf("GetParent(200) after idempotent set = %d, want 100", parent)
	}

	children, _ := rs.GetChildren(100)
	if len(children) != 1 {
		t.Errorf("GetChildren(100) after idempotent set = %d, want 1", len(children))
	}
}

func TestParseJournalLine(t *testing.T) {
	// Test SetParent journal line
	entry, err := parseJournalLine("+1:200:100")
	if err != nil {
		t.Fatalf("parseJournalLine(+1:200:100) failed: %v", err)
	}
	if entry.Op != "+1" || entry.ChildKey != 200 || entry.ParentKey != 100 {
		t.Errorf("parseJournalLine result = %+v, want Op=+1 ChildKey=200 ParentKey=100", entry)
	}

	// Test RemoveRelations journal line
	entry, err = parseJournalLine("-1:200")
	if err != nil {
		t.Fatalf("parseJournalLine(-1:200) failed: %v", err)
	}
	if entry.Op != "-1" || entry.EventKey != 200 {
		t.Errorf("parseJournalLine result = %+v, want Op=-1 EventKey=200", entry)
	}
}
