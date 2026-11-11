// 契约: docs/wiki/platform/org-hot-reload.md#overview
//
// 监视器的三个判据：内容变化（而非 mtime）防抖后只触发一次 Reload；坏配置的
// Reload 被拒后进程与运行中绑定都继续服务，修好后的内容还能再触发一次。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/assembly"
)

// watchTestSource renders the one-agent source document the watcher test boots.
func watchTestSource(root, model string) string {
	return fmt.Sprintf(`entry: user
provider: local
model: %s
prompt_dir: %s
request_timeout_seconds: 60
providers:
  local: { provider: openai, api_endpoint: "http://127.0.0.1:1/v1", api_key_env: TAGENT_TEST_API_KEY }
agents:
  user:
    model: %s
    max_tokens: 256
storage: { type: localfile, root: %s }
`, model, root, model, filepath.Join(root, "storage"))
}

// watcherRecorder counts the reloads a watcher triggered, with the generations
// the successful answers named and the failures it reported.
type watcherRecorder struct {
	mu          sync.Mutex
	generations []uint64
	failures    []error
}

// recordSuccess keeps one successful answer.
func (r *watcherRecorder) recordSuccess(generation uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generations = append(r.generations, generation)
}

// recordFailure keeps one refused answer.
func (r *watcherRecorder) recordFailure(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, err)
}

// snapshot answers the current generations and failures.
func (r *watcherRecorder) snapshot() ([]uint64, []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint64(nil), r.generations...), append([]error(nil), r.failures...)
}

// waitWatch polls one condition until it holds or the deadline expires.
func waitWatch(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestWatcherFileChangeReloads pins the poll-by-content reload: a changed file reloads once after the debounce window.
// - the settled content is reloaded once, not once per poll
// - a touch alone reloads nothing
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
func TestWatcherFileChangeReloads(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatalf("write the source: %v", err)
	}
	recorder := &watcherRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := watchConfig(path, func(context.Context) error {
		recorder.recordSuccess(0)
		return nil
	}, recorder.recordFailure)
	watcher.interval = 5 * time.Millisecond
	watcher.debounce = 30 * time.Millisecond
	go watcher.run(ctx)
	time.Sleep(30 * time.Millisecond)
	if generations, _ := recorder.snapshot(); len(generations) != 0 {
		t.Fatalf("reloads before any content change = %d, want none", len(generations))
	}

	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatalf("rewrite the source: %v", err)
	}
	waitWatch(t, "the changed content to reload", func() bool {
		generations, _ := recorder.snapshot()
		return len(generations) == 1
	})
	time.Sleep(60 * time.Millisecond)
	if generations, failures := recorder.snapshot(); len(generations) != 1 || len(failures) != 0 {
		t.Fatalf("reloads after one change = %d (failures %d), want exactly one", len(generations), len(failures))
	}
}

// TestWatcherIgnoresMtimeOnlyTouch pins the read surface: a touch that keeps the content reloads nothing.
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
func TestWatcherIgnoresMtimeOnlyTouch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(path, []byte("unchanged"), 0o644); err != nil {
		t.Fatalf("write the source: %v", err)
	}
	recorder := &watcherRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := watchConfig(path, func(context.Context) error {
		recorder.recordSuccess(0)
		return nil
	}, recorder.recordFailure)
	watcher.interval = 5 * time.Millisecond
	watcher.debounce = 20 * time.Millisecond
	go watcher.run(ctx)
	time.Sleep(20 * time.Millisecond)
	now := time.Now()
	if err := os.Chtimes(path, now.Add(time.Minute), now.Add(time.Minute)); err != nil {
		t.Fatalf("touch the source: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if generations, failures := recorder.snapshot(); len(generations) != 0 || len(failures) != 0 {
		t.Fatalf("a touch triggered %d reloads and %d failures, want none", len(generations), len(failures))
	}
}

// TestWatcherBadYamlKeepsRunningBinding pins the failure boundary of a broken source.
// - the running binding stays at its generation
// - the watcher keeps watching and the repaired content publishes the next generation
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
func TestWatcherBadYamlKeepsRunningBinding(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	root := t.TempDir()
	path := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(path, []byte(watchTestSource(root, "model-a")), 0o644); err != nil {
		t.Fatalf("write the source: %v", err)
	}
	host, err := assembly.Boot(path)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Errorf("close the host: %v", err)
		}
	}()
	recorder := &watcherRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := watchConfig(path, func(ctx context.Context) error {
		report, err := host.Reload(ctx)
		if err != nil {
			return err
		}
		recorder.recordSuccess(report.Publication.Generation)
		return nil
	}, recorder.recordFailure)
	watcher.interval = 5 * time.Millisecond
	watcher.debounce = 25 * time.Millisecond
	go watcher.run(ctx)
	time.Sleep(40 * time.Millisecond)

	if err := os.WriteFile(path, []byte("entry: [unterminated\n"), 0o644); err != nil {
		t.Fatalf("break the source: %v", err)
	}
	waitWatch(t, "the broken source to be refused", func() bool {
		_, failures := recorder.snapshot()
		return len(failures) > 0
	})
	_, failures := recorder.snapshot()
	if !strings.Contains(failures[0].Error(), "configuration") && !strings.Contains(failures[0].Error(), "yaml") {
		t.Fatalf("refused reload error = %v, want the source failure named", failures[0])
	}

	if err := os.WriteFile(path, []byte(watchTestSource(root, "model-b")), 0o644); err != nil {
		t.Fatalf("repair the source: %v", err)
	}
	waitWatch(t, "the repaired source to publish", func() bool {
		generations, _ := recorder.snapshot()
		return len(generations) > 0
	})
	generations, _ := recorder.snapshot()
	if generations[len(generations)-1] != 2 {
		t.Fatalf("repaired reload generation = %d, want the second generation (the refused attempt published none)", generations[len(generations)-1])
	}
}
