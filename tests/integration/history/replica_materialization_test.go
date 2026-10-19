package history

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replica"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestReplicaMaterialization proves the workspace replica materialisation contract on the frozen 03.6 face, in the six G2 sequences.
//
//   - 物化引用稳定不互覆盖: every materialisation resolves to a stable reference and no owner, tool or restart overwrites another target
//   - 显式安全复用: a replica already on disk is reused as it stands, and the reuse never refuses or serialises a second replica
//   - 多副本并存合法: several replicas of one original stand side by side while the blob behind them stays deduplicated
//   - 副本缺失按原文重物化: a missing replica is written again from its original, and a path is declared only where the write succeeded
//   - 原文失效副本不假称可重建: an ineffective original presents its rebuildability as false and refuses the rebuild by name
//   - 授权外分段读拒绝: segmented reads outside the bound read scope are refused by name and serve no byte
//
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
func TestReplicaMaterialization(t *testing.T) {
	ctx := context.Background()
	ownerA := event.Address{AgentID: event.AgentID("agent-a"), SessionID: event.SessionID("session-a")}
	ownerB := event.Address{AgentID: event.AgentID("agent-b"), SessionID: event.SessionID("session-b")}
	scopeA := ledger.ReadScope{Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}
	scopeB := ledger.ReadScope{Owner: ownerB, Format: "tool-output", RetentionPolicy: "keep"}
	body := []byte("replica body: exactly these bytes reach the replica and every segment of it")
	refA := ledger.OriginalRef{Key: "result-a", Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}
	refB := ledger.OriginalRef{Key: "result-b", Owner: ownerB, Format: "tool-output", RetentionPolicy: "keep"}
	refSameBlob := ledger.OriginalRef{Key: "result-a", Owner: ownerB, Format: "tool-output", RetentionPolicy: "keep"}

	materializer := func(t *testing.T, root string, store ledger.ScopedOriginalStore, scope ledger.ReadScope) *replica.Materializer {
		t.Helper()
		mat, err := replica.NewMaterializer(root, store, scope)
		if err != nil {
			t.Fatalf("NewMaterializer: %v", err)
		}
		return mat
	}
	requireBody := func(t *testing.T, path string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read replica %s: %v", path, err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("replica %s holds %d bytes, want the exact original bytes", path, len(got))
		}
	}
	walkSegments := func(t *testing.T, reader *replica.ReplicaReader, ref ledger.OriginalRef, consumer string, max int64) []byte {
		t.Helper()
		var out []byte
		req := ledger.SegmentRequest{MaxBytes: max}
		for {
			result, err := reader.ReadSegment(ctx, ref, consumer, req)
			if err != nil {
				t.Fatalf("ReadSegment at offset %d: %v", req.From.Offset, err)
			}
			out = append(out, result.Body...)
			if result.EOF {
				return out
			}
			req.From = result.Next
		}
	}

	t.Run("G2.1 物化引用稳定不互覆盖: every target resolves to its own stable path across owner, tool and restart", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		if err := store.Put(ctx, refA, body); err != nil {
			t.Fatalf("Put refA: %v", err)
		}
		if err := store.Put(ctx, refSameBlob, body); err != nil {
			t.Fatalf("Put refSameBlob: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store, scopeA)

		first, err := mat.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		if first.Path == "" || first.CreatedAt.IsZero() {
			t.Fatalf("materialised reference = %+v, want a declared path and creation time", first)
		}
		requireBody(t, first.Path)

		repeat, err := mat.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("repeat Materialize: %v", err)
		}
		if repeat.Path != first.Path {
			t.Fatalf("repeat materialisation resolved to %s, want the stable path %s", repeat.Path, first.Path)
		}
		requireBody(t, first.Path)

		otherTool, err := mat.Materialize(ctx, refA, "tool-editor")
		if err != nil {
			t.Fatalf("cross-tool Materialize: %v", err)
		}
		if otherTool.Path == first.Path {
			t.Fatalf("cross-tool materialisation landed on %s, want its own path", otherTool.Path)
		}
		requireBody(t, first.Path)
		requireBody(t, otherTool.Path)

		matB := materializer(t, root, store, scopeB)
		otherOwner, err := matB.Materialize(ctx, refSameBlob, "tool-shell")
		if err != nil {
			t.Fatalf("cross-owner Materialize: %v", err)
		}
		if otherOwner.Path == first.Path || otherOwner.Path == otherTool.Path {
			t.Fatalf("cross-owner materialisation landed on %s, want its own path", otherOwner.Path)
		}
		requireBody(t, first.Path)

		restarted := materializer(t, root, store, scopeA)
		again, err := restarted.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize after restart: %v", err)
		}
		if again.Path != first.Path {
			t.Fatalf("after restart the target resolved to %s, want the stable path %s", again.Path, first.Path)
		}
		if !again.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("after restart the reused replica reports %v, want the creation time %v it already had", again.CreatedAt, first.CreatedAt)
		}
		requireBody(t, first.Path)
	})

	t.Run("G2.2 显式安全复用: a replica already on disk is reused and never refuses a second replica", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		if err := store.Put(ctx, refA, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store, scopeA)

		first, err := mat.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		reused, err := mat.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("reuse Materialize: %v", err)
		}
		if reused.Path != first.Path {
			t.Fatalf("reuse resolved to %s, want the reusable replica at %s", reused.Path, first.Path)
		}
		if !reused.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("reuse reports %v, want the creation time %v of the replica it reused", reused.CreatedAt, first.CreatedAt)
		}
		requireBody(t, first.Path)

		alongside, err := mat.Materialize(ctx, refA, "tool-editor")
		if err != nil {
			t.Fatalf("second replica beside the reusable one: %v", err)
		}
		if alongside.Path == first.Path {
			t.Fatalf("the second replica landed on %s, want its own path beside the reused one", alongside.Path)
		}
		requireBody(t, first.Path)
		requireBody(t, alongside.Path)
	})

	t.Run("G2.3 多副本并存合法: replicas of one original stand side by side while the blob stays deduplicated", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		if err := store.Put(ctx, refA, body); err != nil {
			t.Fatalf("Put refA: %v", err)
		}
		if err := store.Put(ctx, refSameBlob, body); err != nil {
			t.Fatalf("Put refSameBlob: %v", err)
		}
		if count := store.BlobCount(); count != 1 {
			t.Fatalf("BlobCount = %d, want one blob behind the byte-identical references", count)
		}
		if refs := store.References(refA.Key); len(refs) != 2 {
			t.Fatalf("References = %d, want both owner scopes kept apart beside the shared blob", len(refs))
		}

		root := t.TempDir()
		matA := materializer(t, root, store, scopeA)
		matB := materializer(t, root, store, scopeB)
		type target struct {
			mat      *replica.Materializer
			ref      ledger.OriginalRef
			consumer string
		}
		targets := []target{
			{matA, refA, "tool-shell"},
			{matA, refA, "tool-editor"},
			{matA, refA, "task-result"},
			{matB, refSameBlob, "tool-shell"},
			{matB, refSameBlob, "tool-editor"},
			{matB, refSameBlob, "task-result"},
		}
		replicas := make([]replica.Replica, len(targets))
		failures := make([]error, len(targets))
		var wait sync.WaitGroup
		for i, want := range targets {
			wait.Add(1)
			go func(i int, want target) {
				defer wait.Done()
				replicas[i], failures[i] = want.mat.Materialize(ctx, want.ref, want.consumer)
			}(i, want)
		}
		wait.Wait()
		seen := map[string]bool{}
		for i, made := range replicas {
			if failures[i] != nil {
				t.Fatalf("concurrent Materialize %s: %v", targets[i].consumer, failures[i])
			}
			if seen[made.Path] {
				t.Fatalf("concurrent materialisation collided on %s, want one path per target", made.Path)
			}
			seen[made.Path] = true
			requireBody(t, made.Path)
		}
	})

	t.Run("G2.4 副本缺失按原文重物化: a missing replica is written again, and a path is declared only where the write succeeded", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		if err := store.Put(ctx, refA, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store, scopeA)

		kept, err := mat.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		if err := os.Remove(kept.Path); err != nil {
			t.Fatalf("remove replica: %v", err)
		}
		if rebuildable, err := mat.Rebuildable(ctx, refA.Key); err != nil || !rebuildable {
			t.Fatalf("Rebuildable after reclaiming the replica = %v, %v, want the original kept effective", rebuildable, err)
		}
		again, err := mat.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("re-materialise: %v", err)
		}
		if again.Path != kept.Path {
			t.Fatalf("re-materialisation resolved to %s, want the stable path %s", again.Path, kept.Path)
		}
		requireBody(t, again.Path)

		if err := os.Remove(kept.Path); err != nil {
			t.Fatalf("remove replica: %v", err)
		}
		reader := mat.Reader()
		segment, err := reader.ReadSegment(ctx, refA, "tool-shell", ledger.SegmentRequest{})
		if err != nil {
			t.Fatalf("ReadSegment over the missing replica: %v", err)
		}
		if !bytes.Equal(segment.Body, body) || !segment.EOF {
			t.Fatalf("segment after re-materialisation = %d bytes (EOF %v), want the exact original bytes", len(segment.Body), segment.EOF)
		}
		requireBody(t, kept.Path)

		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("prepare blocked root: %v", err)
		}
		blockedMat := materializer(t, blocked, store, scopeA)
		failed, err := blockedMat.Materialize(ctx, refA, "tool-shell")
		if !errors.Is(err, replica.ErrReplicaWriteFailed) {
			t.Fatalf("Materialize under the blocked root = %v, want ErrReplicaWriteFailed", err)
		}
		if failed.Path != "" {
			t.Fatalf("a failed write declared the path %s, want no path before the bytes landed", failed.Path)
		}
		if rebuildable, err := blockedMat.Rebuildable(ctx, refA.Key); err != nil || !rebuildable {
			t.Fatalf("Rebuildable beside the failed replica write = %v, %v, want the original left effective", rebuildable, err)
		}
	})

	t.Run("G2.5 原文失效副本不假称可重建: an ineffective original presents rebuildability as false and refuses the rebuild", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore(ledger.WithRetentionWindow("transient", time.Second))
		refT := ledger.OriginalRef{Key: "result-t", Owner: ownerA, Format: "tool-output", RetentionPolicy: "transient"}
		scopeT := ledger.ReadScope{Owner: ownerA, Format: "tool-output", RetentionPolicy: "transient"}
		if err := store.Put(ctx, refT, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store, scopeT)

		kept, err := mat.Materialize(ctx, refT, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize while the original is effective: %v", err)
		}
		if rebuildable, err := mat.Rebuildable(ctx, refT.Key); err != nil || !rebuildable {
			t.Fatalf("Rebuildable while effective = %v, %v, want true", rebuildable, err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			rebuildable, err := mat.Rebuildable(ctx, refT.Key)
			if err != nil {
				t.Fatalf("Rebuildable: %v", err)
			}
			if !rebuildable {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the original never became ineffective inside its retention window")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if rebuildable, err := mat.Rebuildable(ctx, refT.Key); err != nil || rebuildable {
			t.Fatalf("Rebuildable after the retention window = %v, %v, want the absence presented as false", rebuildable, err)
		}

		unbuilt, err := mat.Materialize(ctx, refT, "tool-rebuild")
		if !errors.Is(err, replica.ErrOriginalIneffective) {
			t.Fatalf("rebuild of the missing replica = %v, want ErrOriginalIneffective instead of a claimed rebuild", err)
		}
		if unbuilt.Path != "" {
			t.Fatalf("the refused rebuild declared the path %s, want no path for a replica that was never written", unbuilt.Path)
		}

		requireBody(t, kept.Path)
		reused, err := mat.Materialize(ctx, refT, "tool-shell")
		if err != nil {
			t.Fatalf("reuse of the surviving replica: %v", err)
		}
		if reused.Path != kept.Path {
			t.Fatalf("after the original became ineffective the replica reference = %s, want the unchanged path %s", reused.Path, kept.Path)
		}
		requireBody(t, kept.Path)
	})

	t.Run("G2.6 授权外分段读拒绝: segmented reads outside the bound read scope are refused and serve no byte", func(t *testing.T) {
		store := ledger.NewMemoryOriginalStore()
		if err := store.Put(ctx, refA, body); err != nil {
			t.Fatalf("Put refA: %v", err)
		}
		if err := store.Put(ctx, refB, []byte("the foreign original another owner registered")); err != nil {
			t.Fatalf("Put refB: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store, scopeA)
		reader := mat.Reader()
		if reader.Scope() != scopeA {
			t.Fatalf("reader scope = %+v, want the trusted scope it was bound to", reader.Scope())
		}

		wanted := bytes.Repeat([]byte("x"), int(ledger.DefaultSegmentMaxBytes)+100)
		refBig := ledger.OriginalRef{Key: "result-big", Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}
		if err := store.Put(ctx, refBig, wanted); err != nil {
			t.Fatalf("Put refBig: %v", err)
		}
		kept, err := mat.Materialize(ctx, refA, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		big, err := mat.Materialize(ctx, refBig, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize big: %v", err)
		}

		walked := walkSegments(t, reader, refA, "tool-shell", 8)
		if !bytes.Equal(walked, body) {
			t.Fatalf("segments concatenated to %d bytes, want the exact replica bytes", len(walked))
		}
		onDisk, err := os.ReadFile(kept.Path)
		if err != nil {
			t.Fatalf("read replica: %v", err)
		}
		if !bytes.Equal(walked, onDisk) {
			t.Fatal("the segments served do not match the replica on disk")
		}

		bounded, err := reader.ReadSegment(ctx, refBig, "tool-shell", ledger.SegmentRequest{})
		if err != nil {
			t.Fatalf("ReadSegment with a zero MaxBytes: %v", err)
		}
		if len(bounded.Body) != int(ledger.DefaultSegmentMaxBytes) || bounded.EOF || bounded.Next.Offset != ledger.DefaultSegmentMaxBytes {
			t.Fatalf("zero MaxBytes served %d bytes (EOF %v, next %d), want the finite default bound", len(bounded.Body), bounded.EOF, bounded.Next.Offset)
		}

		refused, err := reader.ReadSegment(ctx, refB, "tool-shell", ledger.SegmentRequest{})
		if !errors.Is(err, replica.ErrReplicaUnauthorized) {
			t.Fatalf("segment read of the foreign owner = %v, want ErrReplicaUnauthorized", err)
		}
		if len(refused.Body) != 0 {
			t.Fatalf("the refused read served %d bytes, want none", len(refused.Body))
		}
		if _, err := mat.Materialize(ctx, refB, "tool-shell"); !errors.Is(err, replica.ErrReplicaUnauthorized) {
			t.Fatalf("materialisation outside the bound scope = %v, want ErrReplicaUnauthorized", err)
		}

		forged := ledger.OriginalRef{Key: refB.Key, Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}
		outside, err := reader.ReadSegment(ctx, forged, "tool-shell", ledger.SegmentRequest{})
		if !errors.Is(err, replica.ErrOriginalIneffective) {
			t.Fatalf("segment read of a key registered outside the scope = %v, want a refusal that leaks nothing", err)
		}
		if len(outside.Body) != 0 {
			t.Fatalf("the refused read served %d bytes, want none", len(outside.Body))
		}

		files := map[string]bool{}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type().IsRegular() {
				files[path] = true
			}
			return nil
		}); err != nil {
			t.Fatalf("walk replica root: %v", err)
		}
		if len(files) != 2 || !files[kept.Path] || !files[big.Path] {
			t.Fatalf("replica root holds %v, want only the two declared replicas and none for a refused request", files)
		}
	})
}
