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

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/workspace"
)

// TestReplicaLifecycleProtectsActiveUse proves the active-use protection contract of the replica lifecycle on the formal workspace face.
//
//   - 工作副本保护: an open working copy keeps its exact bytes through concurrent re-materialisation and reclamation passes
//   - 真源重定影: a true source that moved on is shadowed again at the next evaluation, and the standing bytes are never served in its place
//   - 保护释放: the protection ends with the materialisation span and with the close of a working copy, and reclamation takes what was released
//   - 父级链接拒绝: a symbolic link standing where a parent directory belongs refuses the request by name and writes nothing outside the replica area
//   - TOCTOU 并发窗口: interleaved materialisation, opening and reclamation never pull the bytes away from a working copy in use
//   - 启动遗留暂存件: a leftover staging file is never taken as a replica and never served as one
//
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
func TestReplicaLifecycleProtectsActiveUse(t *testing.T) {
	ctx := context.Background()
	owner := event.Address{AgentID: event.AgentID("agent-a"), SessionID: event.SessionID("session-a")}
	scope := memory.ReadScope{Owner: owner, Format: "tool-output", RetentionPolicy: "keep"}
	ref := memory.OriginalRef{Key: "result-a", Owner: owner, Format: "tool-output", RetentionPolicy: "keep"}
	firstBody := []byte("working copy bytes: the exact original bytes one replica carries")
	movedBody := []byte("moved-on bytes: the true source answers different bytes now")

	seeded := func(t *testing.T, body []byte) memory.ScopedOriginalStore {
		t.Helper()
		store := memory.NewMemoryOriginalStore()
		if err := store.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		return store
	}
	cleanerFor := func(t *testing.T, root string) *workspace.ReplicaCleaner {
		t.Helper()
		cleaner, err := workspace.NewReplicaCleaner(root)
		if err != nil {
			t.Fatalf("NewReplicaCleaner: %v", err)
		}
		return cleaner
	}
	materializerFor := func(t *testing.T, root string, store memory.ScopedOriginalStore, cleaner *workspace.ReplicaCleaner) *workspace.Materializer {
		t.Helper()
		materializer, err := workspace.NewMaterializer(root, store, scope, workspace.WithCleaner(cleaner))
		if err != nil {
			t.Fatalf("NewMaterializer: %v", err)
		}
		return materializer
	}
	requireBytes := func(t *testing.T, path string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read replica %s: %v", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("replica %s holds %d bytes, want the %d bytes of the source", path, len(got), len(want))
		}
	}

	t.Run("S0 保护注入: a working copy is served only where its protection exists, and a cleaner of another replica area is refused", func(t *testing.T) {
		root := t.TempDir()
		unwired, err := workspace.NewMaterializer(root, seeded(t, firstBody), scope)
		if err != nil {
			t.Fatalf("NewMaterializer without a cleaner: %v", err)
		}
		if _, err := unwired.Open(ctx, ref, "tool-shell"); !errors.Is(err, workspace.ErrReplicaProtectionUnavailable) {
			t.Fatalf("Open with no protection wired = %v, want ErrReplicaProtectionUnavailable", err)
		}
		other := cleanerFor(t, t.TempDir())
		if _, err := workspace.NewMaterializer(root, seeded(t, firstBody), scope, workspace.WithCleaner(other)); !errors.Is(err, workspace.ErrCleanerRootMismatch) {
			t.Fatalf("NewMaterializer with a cleaner of another replica area = %v, want ErrCleanerRootMismatch", err)
		}
	})

	t.Run("S1 工作副本保护: an open working copy keeps its bytes while re-materialisation and reclamation run over its path", func(t *testing.T) {
		root := t.TempDir()
		cleaner := cleanerFor(t, root)
		materializer := materializerFor(t, root, seeded(t, firstBody), cleaner)
		handle, err := materializer.Open(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer handle.Close()
		if !handle.ActiveRW {
			t.Fatalf("Open returned ActiveRW=false, want the working copy marked in use")
		}
		moved := materializerFor(t, root, seeded(t, movedBody), cleaner)
		done := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if _, err := moved.Materialize(ctx, ref, "tool-shell"); err != nil {
					t.Errorf("concurrent Materialize: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if err := cleaner.CleanupStartup(ctx); err != nil {
					t.Errorf("concurrent CleanupStartup: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				got, err := os.ReadFile(handle.Path)
				if err != nil {
					t.Errorf("read the working copy while in use: %v", err)
					return
				}
				if !bytes.Equal(got, firstBody) {
					t.Errorf("working copy holds %d bytes while in use, want the %d bytes it was opened with", len(got), len(firstBody))
					return
				}
			}
		}()
		time.Sleep(150 * time.Millisecond)
		close(done)
		wg.Wait()
		requireBytes(t, handle.Path, firstBody)
	})

	t.Run("S2 真源重定影: a source that moved on is shadowed again at the next evaluation and the standing bytes are not served in its place", func(t *testing.T) {
		root := t.TempDir()
		cleaner := cleanerFor(t, root)
		materializer := materializerFor(t, root, seeded(t, firstBody), cleaner)
		first, err := materializer.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		requireBytes(t, first.Path, firstBody)
		moved := materializerFor(t, root, seeded(t, movedBody), cleaner)
		second, err := moved.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("re-materialise after the source moved on: %v", err)
		}
		if second.Path != first.Path {
			t.Fatalf("re-materialisation resolved to %s, want the stable path %s", second.Path, first.Path)
		}
		requireBytes(t, second.Path, movedBody)
	})

	t.Run("S3 保护释放: the protection ends with the materialisation span and the close of a working copy, and the next evaluation answers the moved source", func(t *testing.T) {
		root := t.TempDir()
		cleaner := cleanerFor(t, root)
		materializer := materializerFor(t, root, seeded(t, firstBody), cleaner)
		spanned, err := materializer.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup after one materialisation: %v", err)
		}
		if _, err := os.Lstat(spanned.Path); !os.IsNotExist(err) {
			t.Fatalf("replica %s is still standing (Lstat err=%v), want the span protection released at return", spanned.Path, err)
		}

		handle, err := materializer.Open(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup with an open working copy: %v", err)
		}
		requireBytes(t, handle.Path, firstBody)

		moved := materializerFor(t, root, seeded(t, movedBody), cleaner)
		deferred, err := moved.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize over an open working copy: %v", err)
		}
		if deferred.Path != handle.Path {
			t.Fatalf("materialisation over the working copy resolved to %s, want the stable path %s", deferred.Path, handle.Path)
		}
		requireBytes(t, handle.Path, firstBody)

		handle.Close()
		handle.Close()
		corrected, err := moved.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("re-materialise after the working copy closed: %v", err)
		}
		if corrected.Path != handle.Path {
			t.Fatalf("re-materialisation resolved to %s, want the stable path %s", corrected.Path, handle.Path)
		}
		requireBytes(t, corrected.Path, movedBody)

		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup after every protection ended: %v", err)
		}
		if _, err := os.Lstat(corrected.Path); !os.IsNotExist(err) {
			t.Fatalf("replica %s is still standing (Lstat err=%v), want it reclaimed once every protection ended", corrected.Path, err)
		}
	})

	t.Run("S4 父级链接拒绝: a symbolic link standing where a parent directory belongs refuses the request by name and nothing is written outside", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		victim := filepath.Join(outside, "user.txt")
		victimBody := []byte("user file bytes: untouched by any replica request")
		if err := os.WriteFile(victim, victimBody, 0o644); err != nil {
			t.Fatalf("prepare user file: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "replica")); err != nil {
			t.Fatalf("plant symbolic link: %v", err)
		}
		cleaner := cleanerFor(t, root)
		materializer := materializerFor(t, root, seeded(t, firstBody), cleaner)
		if _, err := materializer.Materialize(ctx, ref, "tool-shell"); !errors.Is(err, workspace.ErrReplicaParentLinked) {
			t.Fatalf("Materialize across a linked parent = %v, want ErrReplicaParentLinked", err)
		}
		if _, err := materializer.Open(ctx, ref, "tool-shell"); !errors.Is(err, workspace.ErrReplicaParentLinked) {
			t.Fatalf("Open across a linked parent = %v, want ErrReplicaParentLinked", err)
		}
		requireOnlyUserFile := func() {
			t.Helper()
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatalf("read the linked area: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "user.txt" {
				t.Fatalf("the linked area holds %v, want only the user file that was already there", entries)
			}
			requireBytes(t, victim, victimBody)
		}
		requireOnlyUserFile()
		linkInfo, err := os.Lstat(filepath.Join(root, "replica"))
		if err != nil {
			t.Fatalf("Lstat the planted link: %v", err)
		}
		if linkInfo.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the planted link has mode %v, want the symbolic link left as it stood", linkInfo.Mode())
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup over the linked area: %v", err)
		}
		requireOnlyUserFile()
	})

	t.Run("S4b 文件级链接治愈: a symbolic link standing at the replica path is replaced in place and the file it pointed at keeps its bytes", func(t *testing.T) {
		root := t.TempDir()
		cleaner := cleanerFor(t, root)
		materializer := materializerFor(t, root, seeded(t, firstBody), cleaner)
		first, err := materializer.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		outside := t.TempDir()
		victim := filepath.Join(outside, "user.txt")
		victimBody := []byte("user file bytes: the link pointed here")
		if err := os.WriteFile(victim, victimBody, 0o644); err != nil {
			t.Fatalf("prepare user file: %v", err)
		}
		if err := os.Remove(first.Path); err != nil {
			t.Fatalf("take the replica away: %v", err)
		}
		if err := os.Symlink(victim, first.Path); err != nil {
			t.Fatalf("plant symbolic link: %v", err)
		}
		healed, err := materializer.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("re-materialise over a linked replica path: %v", err)
		}
		if healed.Path != first.Path {
			t.Fatalf("re-materialisation resolved to %s, want the stable path %s", healed.Path, first.Path)
		}
		info, err := os.Lstat(healed.Path)
		if err != nil {
			t.Fatalf("Lstat the healed replica: %v", err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("the replica path has mode %v, want the symbolic link replaced by the replica", info.Mode())
		}
		requireBytes(t, healed.Path, firstBody)
		requireBytes(t, victim, victimBody)
	})

	t.Run("S5 TOCTOU 并发窗口: interleaved materialisation, opening and reclamation never pull the bytes away from a working copy in use", func(t *testing.T) {
		root := t.TempDir()
		cleaner := cleanerFor(t, root)
		materializer := materializerFor(t, root, seeded(t, firstBody), cleaner)
		done := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(4)
		for opener := 0; opener < 2; opener++ {
			go func() {
				defer wg.Done()
				for {
					select {
					case <-done:
						return
					default:
					}
					handle, err := materializer.Open(ctx, ref, "tool-shell")
					if err != nil {
						t.Errorf("Open under contention: %v", err)
						return
					}
					got, err := os.ReadFile(handle.Path)
					if err != nil {
						t.Errorf("a working copy under an active read/write handle vanished mid-read: %v", err)
						handle.Close()
						return
					}
					if !bytes.Equal(got, firstBody) {
						t.Errorf("working copy holds %d bytes under contention, want the %d bytes of the source", len(got), len(firstBody))
						handle.Close()
						return
					}
					handle.Close()
				}
			}()
		}
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if _, err := materializer.Materialize(ctx, ref, "tool-shell"); err != nil {
					t.Errorf("Materialize under contention: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if err := cleaner.CleanupByAge(ctx, 0); err != nil {
					t.Errorf("CleanupByAge under contention: %v", err)
					return
				}
				if err := cleaner.CleanupByCount(ctx, 0); err != nil {
					t.Errorf("CleanupByCount under contention: %v", err)
					return
				}
			}
		}()
		time.Sleep(150 * time.Millisecond)
		close(done)
		wg.Wait()
		if err := cleaner.CleanupByAge(ctx, 0); err != nil {
			t.Fatalf("final CleanupByAge: %v", err)
		}
		var leftovers []string
		if err := filepath.WalkDir(filepath.Join(root, "replica"), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type().IsRegular() {
				leftovers = append(leftovers, path)
			}
			return nil
		}); err != nil {
			t.Fatalf("walk the replica area: %v", err)
		}
		if len(leftovers) != 0 {
			t.Fatalf("replica area still holds %v, want every standing replica reclaimed once every protection ended", leftovers)
		}
	})

	t.Run("S6 启动遗留暂存件: a leftover staging file is never taken as a replica and never served as one", func(t *testing.T) {
		root := t.TempDir()
		stagingDir := filepath.Join(root, "replica", "deadbeef")
		if err := os.MkdirAll(stagingDir, 0o755); err != nil {
			t.Fatalf("prepare leftover directory: %v", err)
		}
		staging := filepath.Join(stagingDir, ".replica-orphan")
		stagingBody := []byte("half-written staging bytes")
		if err := os.WriteFile(staging, stagingBody, 0o644); err != nil {
			t.Fatalf("prepare leftover staging file: %v", err)
		}
		cleaner := cleanerFor(t, root)
		materializer := materializerFor(t, root, seeded(t, firstBody), cleaner)
		replica, err := materializer.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup: %v", err)
		}
		if _, err := os.Lstat(replica.Path); !os.IsNotExist(err) {
			t.Fatalf("replica %s is still standing (Lstat err=%v), want it reclaimed", replica.Path, err)
		}
		requireBytes(t, staging, stagingBody)
		served, err := materializer.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("re-materialise beside the leftover staging file: %v", err)
		}
		if served.Path == staging {
			t.Fatalf("materialisation resolved to the staging file %s, want the replica path of the target", staging)
		}
		requireBytes(t, served.Path, firstBody)
		requireBytes(t, staging, stagingBody)
	})
}
