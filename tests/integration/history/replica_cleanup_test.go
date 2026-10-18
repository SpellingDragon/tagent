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

// TestReplicaCleanup proves the temporary-replica reclamation contract on the frozen workspace face:
// - 启动清理: startup reclamation takes back the replicas left behind, and one is written again at its stable path
// - 周期清理: the running cleaner reclaims by age and by count on its period, and the close sequence confirms it ended
// - 活跃读写的副本不删: a replica an active read/write handle holds is left alone by every trigger until release
// - 用户文件不动: reclamation never touches a user file, whatever it is named and however hard it is linked
// - 跨链接不越界: reclamation never follows a symbolic link out of the replica root and removes nothing through one
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
func TestReplicaCleanup(t *testing.T) {
	ctx := context.Background()
	owner := event.Address{AgentID: event.AgentID("agent-a"), SessionID: event.SessionID("session-a")}
	scope := memory.ReadScope{Owner: owner, Format: "tool-output", RetentionPolicy: "keep"}
	body := []byte("replica body: exactly these bytes reach the replica and every segment of it")
	ref := memory.OriginalRef{Key: "result-a", Owner: owner, Format: "tool-output", RetentionPolicy: "keep"}

	materializer := func(t *testing.T, root string, store memory.ScopedOriginalStore) *workspace.Materializer {
		t.Helper()
		mat, err := workspace.NewMaterializer(root, store, scope)
		if err != nil {
			t.Fatalf("NewMaterializer: %v", err)
		}
		return mat
	}
	cleanerFor := func(t *testing.T, root string, options ...workspace.CleanupOption) *workspace.ReplicaCleaner {
		t.Helper()
		cleaner, err := workspace.NewReplicaCleaner(root, options...)
		if err != nil {
			t.Fatalf("NewReplicaCleaner: %v", err)
		}
		return cleaner
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
	requireGone := func(t *testing.T, paths ...string) {
		t.Helper()
		for _, path := range paths {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("replica %s is still standing (Lstat err=%v), want it reclaimed", path, err)
			}
		}
	}
	requireStanding := func(t *testing.T, paths ...string) {
		t.Helper()
		for _, path := range paths {
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("replica %s is gone: %v, want it left alone", path, err)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("replica %s is no longer a regular file (mode %v)", path, info.Mode())
			}
		}
	}
	requireSymlink := func(t *testing.T, path string) {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("symbolic link %s is gone: %v, want the link itself left alone", path, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("path %s has mode %v, want the symbolic link left as it stood", path, info.Mode())
		}
	}
	waitReclaimed := func(t *testing.T, paths ...string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			gone := true
			for _, path := range paths {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					gone = false
					break
				}
			}
			if gone {
				return
			}
			if time.Now().After(deadline) {
				requireGone(t, paths...)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	stopCleaner := func(t *testing.T, cleaner *workspace.ReplicaCleaner) {
		t.Helper()
		cleaner.Stop()
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := cleaner.Wait(waitCtx); err != nil {
			t.Fatalf("Wait after Stop: %v", err)
		}
	}

	t.Run("G2.1 启动清理: startup reclamation takes back the leftovers of a previous run and the replica is rebuilt at its stable path", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore()
		if err := store.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store)
		cleaner := cleanerFor(t, root)

		shell, err := mat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize: %v", err)
		}
		editor, err := mat.Materialize(ctx, ref, "tool-editor")
		if err != nil {
			t.Fatalf("Materialize second target: %v", err)
		}
		requireBody(t, shell.Path)
		requireBody(t, editor.Path)

		leftoverDir := filepath.Join(root, "replica", "deadbeef")
		if err := os.MkdirAll(leftoverDir, 0o755); err != nil {
			t.Fatalf("prepare leftover directory: %v", err)
		}
		leftover := filepath.Join(leftoverDir, "cafebabe")
		if err := os.WriteFile(leftover, []byte("stale replica bytes"), 0o644); err != nil {
			t.Fatalf("prepare leftover replica: %v", err)
		}

		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup: %v", err)
		}
		requireGone(t, shell.Path, editor.Path, leftover)

		rebuilt, err := mat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("re-materialise after startup cleanup: %v", err)
		}
		if rebuilt.Path != shell.Path {
			t.Fatalf("re-materialisation resolved to %s, want the stable path %s", rebuilt.Path, shell.Path)
		}
		requireBody(t, rebuilt.Path)

		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("second CleanupStartup: %v", err)
		}
		requireGone(t, rebuilt.Path)

		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup over an empty replica area: %v", err)
		}
	})

	t.Run("G2.2 周期清理: the running cleaner reclaims by age and by count on its period, and its end is confirmed", func(t *testing.T) {
		ageStore := memory.NewMemoryOriginalStore()
		if err := ageStore.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		ageRoot := t.TempDir()
		ageMat := materializer(t, ageRoot, ageStore)
		ageCleaner := cleanerFor(t, ageRoot,
			workspace.WithCleanupInterval(5*time.Millisecond),
			workspace.WithReplicaMaxAge(10*time.Minute),
			workspace.WithReplicaMaxCount(100),
		)
		oldOne, err := ageMat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize oldOne: %v", err)
		}
		oldTwo, err := ageMat.Materialize(ctx, ref, "tool-editor")
		if err != nil {
			t.Fatalf("Materialize oldTwo: %v", err)
		}
		fresh, err := ageMat.Materialize(ctx, ref, "task-result")
		if err != nil {
			t.Fatalf("Materialize fresh: %v", err)
		}
		longAgo := time.Now().Add(-time.Hour)
		for _, path := range []string{oldOne.Path, oldTwo.Path} {
			if err := os.Chtimes(path, longAgo, longAgo); err != nil {
				t.Fatalf("age replica %s: %v", path, err)
			}
		}
		ageDone := make(chan error, 1)
		go func() { ageDone <- ageCleaner.Run(ctx) }()
		waitReclaimed(t, oldOne.Path, oldTwo.Path)
		stopCleaner(t, ageCleaner)
		if err := <-ageDone; err != nil {
			t.Fatalf("Run returned %v, want the clean stop the close sequence asked for", err)
		}
		requireStanding(t, fresh.Path)
		requireBody(t, fresh.Path)

		countStore := memory.NewMemoryOriginalStore()
		if err := countStore.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		countRoot := t.TempDir()
		countMat := materializer(t, countRoot, countStore)
		countCleaner := cleanerFor(t, countRoot,
			workspace.WithCleanupInterval(5*time.Millisecond),
			workspace.WithReplicaMaxAge(10*time.Minute),
			workspace.WithReplicaMaxCount(2),
		)
		var kept [3]workspace.Replica
		for i, consumer := range []string{"tool-shell", "tool-editor", "task-result"} {
			replica, err := countMat.Materialize(ctx, ref, consumer)
			if err != nil {
				t.Fatalf("Materialize %s: %v", consumer, err)
			}
			kept[i] = replica
		}
		for i, replica := range kept {
			aged := time.Now().Add(-time.Duration(3-i) * time.Minute)
			if err := os.Chtimes(replica.Path, aged, aged); err != nil {
				t.Fatalf("age replica %s: %v", replica.Path, err)
			}
		}
		countDone := make(chan error, 1)
		go func() { countDone <- countCleaner.Run(ctx) }()
		waitReclaimed(t, kept[0].Path)
		stopCleaner(t, countCleaner)
		if err := <-countDone; err != nil {
			t.Fatalf("Run returned %v, want the clean stop the close sequence asked for", err)
		}
		requireStanding(t, kept[1].Path, kept[2].Path)

		direct, err := countMat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize direct target: %v", err)
		}
		if err := countCleaner.CleanupByCount(ctx, 2); err != nil {
			t.Fatalf("CleanupByCount: %v", err)
		}
		requireGone(t, kept[1].Path)
		requireStanding(t, kept[2].Path, direct.Path)

		if err := countCleaner.CleanupByAge(ctx, -time.Second); !errors.Is(err, workspace.ErrCleanupAgeInvalid) {
			t.Fatalf("CleanupByAge with a negative bound = %v, want ErrCleanupAgeInvalid", err)
		}
		if err := countCleaner.CleanupByCount(ctx, -1); !errors.Is(err, workspace.ErrCleanupCountInvalid) {
			t.Fatalf("CleanupByCount with a negative bound = %v, want ErrCleanupCountInvalid", err)
		}
		requireStanding(t, kept[2].Path, direct.Path)
		if _, err := workspace.NewReplicaCleaner(countRoot, workspace.WithCleanupInterval(0)); !errors.Is(err, workspace.ErrCleanupIntervalInvalid) {
			t.Fatalf("NewReplicaCleaner with a zero period = %v, want ErrCleanupIntervalInvalid", err)
		}

		idle := cleanerFor(t, countRoot,
			workspace.WithCleanupInterval(5*time.Millisecond),
			workspace.WithReplicaMaxAge(0),
			workspace.WithReplicaMaxCount(0),
		)
		if err := idle.Wait(ctx); err != nil {
			t.Fatalf("Wait on a cleaner that never ran = %v, want the end reported immediately", err)
		}
		reapable, err := countMat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize reapable target: %v", err)
		}
		runningDone := make(chan error, 1)
		go func() { runningDone <- idle.Run(ctx) }()
		waitReclaimed(t, reapable.Path)
		shortCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		if err := idle.Wait(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Wait while the loop runs = %v, want the short ctx to run out first", err)
		}
		if err := idle.Run(ctx); !errors.Is(err, workspace.ErrCleanerAlreadyRunning) {
			t.Fatalf("second Run on a running cleaner = %v, want ErrCleanerAlreadyRunning", err)
		}
		stopCleaner(t, idle)
		if err := <-runningDone; err != nil {
			t.Fatalf("Run returned %v, want the clean stop the close sequence asked for", err)
		}
	})

	t.Run("G2.3 活跃读写的副本不删: a replica an active read/write handle holds is left alone by every trigger until it is released", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore()
		if err := store.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store)
		cleaner := cleanerFor(t, root,
			workspace.WithCleanupInterval(5*time.Millisecond),
			workspace.WithReplicaMaxAge(0),
			workspace.WithReplicaMaxCount(0),
		)

		held, err := mat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize held: %v", err)
		}
		idleOne, err := mat.Materialize(ctx, ref, "tool-editor")
		if err != nil {
			t.Fatalf("Materialize idleOne: %v", err)
		}
		idleTwo, err := mat.Materialize(ctx, ref, "task-result")
		if err != nil {
			t.Fatalf("Materialize idleTwo: %v", err)
		}

		if _, err := cleaner.Acquire(filepath.Join(root, "notes.txt")); !errors.Is(err, workspace.ErrCleanupTargetOutOfRange) {
			t.Fatalf("Acquire outside the replica area = %v, want ErrCleanupTargetOutOfRange", err)
		}
		if _, err := cleaner.Acquire(filepath.Join(root, "replica", "deadbeef", "cafebabe")); !errors.Is(err, workspace.ErrCleanupTargetMissing) {
			t.Fatalf("Acquire on no standing replica = %v, want ErrCleanupTargetMissing", err)
		}

		release, err := cleaner.Acquire(held.Path)
		if err != nil {
			t.Fatalf("Acquire held replica: %v", err)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup: %v", err)
		}
		requireStanding(t, held.Path)
		requireGone(t, idleOne.Path, idleTwo.Path)
		if err := cleaner.CleanupByAge(ctx, 0); err != nil {
			t.Fatalf("CleanupByAge: %v", err)
		}
		if err := cleaner.CleanupByCount(ctx, 0); err != nil {
			t.Fatalf("CleanupByCount: %v", err)
		}
		requireStanding(t, held.Path)
		requireBody(t, held.Path)

		periodic, err := mat.Materialize(ctx, ref, "tool-editor")
		if err != nil {
			t.Fatalf("Materialize periodic target: %v", err)
		}
		periodicDone := make(chan error, 1)
		go func() { periodicDone <- cleaner.Run(ctx) }()
		waitReclaimed(t, periodic.Path)
		stopCleaner(t, cleaner)
		if err := <-periodicDone; err != nil {
			t.Fatalf("Run returned %v, want the clean stop the close sequence asked for", err)
		}
		requireStanding(t, held.Path)
		requireBody(t, held.Path)

		release()
		release()
		if err := cleaner.CleanupByAge(ctx, 0); err != nil {
			t.Fatalf("CleanupByAge after release: %v", err)
		}
		requireGone(t, held.Path)

		stressStore := memory.NewMemoryOriginalStore()
		if err := stressStore.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		stressRoot := t.TempDir()
		stressMat := materializer(t, stressRoot, stressStore)
		stressCleaner := cleanerFor(t, stressRoot,
			workspace.WithCleanupInterval(time.Hour),
			workspace.WithReplicaMaxAge(0),
			workspace.WithReplicaMaxCount(0),
		)
		if err := stressCleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup before the stress run: %v", err)
		}
		stopLoop := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				select {
				case <-stopLoop:
					return
				default:
				}
				if err := stressCleaner.CleanupByAge(ctx, 0); err != nil {
					t.Errorf("concurrent CleanupByAge: %v", err)
					return
				}
			}
		}()
		for reader := 0; reader < 8; reader++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				for {
					select {
					case <-stopLoop:
						return
					default:
					}
					replica, err := stressMat.Materialize(ctx, ref, "tool-shell")
					if err != nil {
						t.Errorf("concurrent Materialize: %v", err)
						return
					}
					release, err := stressCleaner.Acquire(replica.Path)
					if errors.Is(err, workspace.ErrCleanupTargetMissing) {
						continue
					}
					if err != nil {
						t.Errorf("concurrent Acquire: %v", err)
						return
					}
					if _, err := os.ReadFile(replica.Path); err != nil {
						t.Errorf("a replica under an active read/write handle vanished mid-read: %v", err)
					}
					release()
				}
			}()
		}
		time.Sleep(200 * time.Millisecond)
		close(stopLoop)
		wait.Wait()
		if err := stressCleaner.CleanupByAge(ctx, 0); err != nil {
			t.Fatalf("final CleanupByAge: %v", err)
		}
		// Reclamation leaves the empty area directories behind and takes every
		// standing replica file under them.
		var leftovers []string
		if err := filepath.WalkDir(filepath.Join(stressRoot, "replica"), func(path string, entry os.DirEntry, err error) error {
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
			t.Fatalf("replica area still holds %v, want every standing replica reclaimed", leftovers)
		}
	})

	t.Run("G2.4 用户文件不动: reclamation never touches a user file, whatever it is named and however hard it is linked", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore()
		if err := store.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		root := t.TempDir()
		cleaner := cleanerFor(t, root,
			workspace.WithCleanupInterval(5*time.Millisecond),
			workspace.WithReplicaMaxAge(0),
			workspace.WithReplicaMaxCount(0),
		)
		mat := materializer(t, root, store)
		held, err := mat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize held: %v", err)
		}
		release, err := cleaner.Acquire(held.Path)
		if err != nil {
			t.Fatalf("Acquire held replica: %v", err)
		}
		defer release()

		users := map[string]string{
			filepath.Join(root, "notes.txt"):            "user notes that must survive",
			filepath.Join(root, "keep.log"):             "user log",
			filepath.Join(root, "replica-adjacent.txt"): "a name beside the replica area",
			filepath.Join(root, "data", "report.bin"):   "user report bytes",
			filepath.Join(root, "data", "real.bin"):     "the user file a replica-area entry is hard-linked to",
		}
		for path, content := range users {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("prepare user directory: %v", err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("prepare user file %s: %v", path, err)
			}
		}
		before := make(map[string]time.Time, len(users))
		for path := range users {
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("stat user file %s: %v", path, err)
			}
			before[path] = info.ModTime()
		}
		linkDir := filepath.Join(root, "replica", "deadbeef")
		if err := os.MkdirAll(linkDir, 0o755); err != nil {
			t.Fatalf("prepare replica directory: %v", err)
		}
		hardLinked := filepath.Join(linkDir, "cafebabe")
		if err := os.Link(filepath.Join(root, "data", "real.bin"), hardLinked); err != nil {
			t.Fatalf("prepare hard link: %v", err)
		}

		idle, err := mat.Materialize(ctx, ref, "tool-editor")
		if err != nil {
			t.Fatalf("Materialize idle replica: %v", err)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup: %v", err)
		}
		if err := cleaner.CleanupByAge(ctx, 0); err != nil {
			t.Fatalf("CleanupByAge: %v", err)
		}
		if err := cleaner.CleanupByCount(ctx, 0); err != nil {
			t.Fatalf("CleanupByCount: %v", err)
		}
		periodicTarget, err := mat.Materialize(ctx, ref, "task-result")
		if err != nil {
			t.Fatalf("Materialize periodic target: %v", err)
		}
		periodicDone := make(chan error, 1)
		go func() { periodicDone <- cleaner.Run(ctx) }()
		waitReclaimed(t, periodicTarget.Path)
		stopCleaner(t, cleaner)
		if err := <-periodicDone; err != nil {
			t.Fatalf("Run returned %v, want the clean stop the close sequence asked for", err)
		}

		requireGone(t, idle.Path, hardLinked)
		requireStanding(t, held.Path)
		for path, content := range users {
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("user file %s was touched by reclamation: %v", path, err)
			}
			if !bytes.Equal(got, []byte(content)) {
				t.Fatalf("user file %s now holds %q, want %q", path, got, content)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("stat user file %s: %v", path, err)
			}
			if !info.ModTime().Equal(before[path]) {
				t.Fatalf("user file %s was rewritten at %v, want its own time %v", path, info.ModTime(), before[path])
			}
		}
	})

	t.Run("G2.5 跨链接不越界: reclamation never follows a symbolic link out of the replica root and removes nothing through one", func(t *testing.T) {
		outside := t.TempDir()
		outsideFile := filepath.Join(outside, "precious.txt")
		outsideNested := filepath.Join(outside, "nested")
		outsideInner := filepath.Join(outsideNested, "inner.txt")
		if err := os.MkdirAll(outsideNested, 0o755); err != nil {
			t.Fatalf("prepare outside directory: %v", err)
		}
		if err := os.WriteFile(outsideFile, []byte("precious user data"), 0o644); err != nil {
			t.Fatalf("prepare outside file: %v", err)
		}
		if err := os.WriteFile(outsideInner, []byte("inner user data"), 0o644); err != nil {
			t.Fatalf("prepare outside inner file: %v", err)
		}

		store := memory.NewMemoryOriginalStore()
		if err := store.Put(ctx, ref, body); err != nil {
			t.Fatalf("Put: %v", err)
		}
		root := t.TempDir()
		mat := materializer(t, root, store)
		cleaner := cleanerFor(t, root,
			workspace.WithCleanupInterval(5*time.Millisecond),
			workspace.WithReplicaMaxAge(0),
			workspace.WithReplicaMaxCount(0),
		)
		real, err := mat.Materialize(ctx, ref, "tool-shell")
		if err != nil {
			t.Fatalf("Materialize real replica: %v", err)
		}
		fileLink := filepath.Join(root, "replica", "deadbeef", "cafebabe")
		if err := os.MkdirAll(filepath.Dir(fileLink), 0o755); err != nil {
			t.Fatalf("prepare replica directory: %v", err)
		}
		if err := os.Symlink(outsideFile, fileLink); err != nil {
			t.Fatalf("prepare file link: %v", err)
		}
		dirLink := filepath.Join(root, "replica", "feedface")
		if err := os.Symlink(outsideNested, dirLink); err != nil {
			t.Fatalf("prepare directory link: %v", err)
		}

		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup: %v", err)
		}
		if err := cleaner.CleanupByAge(ctx, 0); err != nil {
			t.Fatalf("CleanupByAge: %v", err)
		}
		if err := cleaner.CleanupByCount(ctx, 0); err != nil {
			t.Fatalf("CleanupByCount: %v", err)
		}
		periodicTarget, err := mat.Materialize(ctx, ref, "task-result")
		if err != nil {
			t.Fatalf("Materialize periodic target: %v", err)
		}
		periodicDone := make(chan error, 1)
		go func() { periodicDone <- cleaner.Run(ctx) }()
		waitReclaimed(t, periodicTarget.Path)
		stopCleaner(t, cleaner)
		if err := <-periodicDone; err != nil {
			t.Fatalf("Run returned %v, want the clean stop the close sequence asked for", err)
		}

		requireGone(t, real.Path)
		requireSymlink(t, fileLink)
		requireSymlink(t, dirLink)
		for path, want := range map[string]string{outsideFile: "precious user data", outsideInner: "inner user data"} {
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("user file %s behind a link was taken: %v", path, err)
			}
			if !bytes.Equal(got, []byte(want)) {
				t.Fatalf("user file %s behind a link now holds %q, want %q", path, got, want)
			}
		}

		outside2 := t.TempDir()
		outside2Dir := filepath.Join(outside2, "nested")
		outside2File := filepath.Join(outside2Dir, "keep.txt")
		if err := os.MkdirAll(outside2Dir, 0o755); err != nil {
			t.Fatalf("prepare outside directory: %v", err)
		}
		if err := os.WriteFile(outside2File, []byte("still here"), 0o644); err != nil {
			t.Fatalf("prepare outside file: %v", err)
		}
		root2 := t.TempDir()
		if err := os.Symlink(outside2Dir, filepath.Join(root2, "replica")); err != nil {
			t.Fatalf("prepare replica root link: %v", err)
		}
		cleaner2 := cleanerFor(t, root2)
		if err := cleaner2.CleanupStartup(ctx); err != nil {
			t.Fatalf("CleanupStartup through the replica root link: %v", err)
		}
		if err := cleaner2.CleanupByAge(ctx, 0); err != nil {
			t.Fatalf("CleanupByAge through the replica root link: %v", err)
		}
		if err := cleaner2.CleanupByCount(ctx, 0); err != nil {
			t.Fatalf("CleanupByCount through the replica root link: %v", err)
		}
		if got, err := os.ReadFile(outside2File); err != nil || !bytes.Equal(got, []byte("still here")) {
			t.Fatalf("user file %s through the replica root link = %q, %v, want it untouched", outside2File, got, err)
		}
	})
}
