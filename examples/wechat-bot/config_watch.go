// 契约: docs/wiki/platform/org-hot-reload.md#overview
//
// config_watch.go 是宿主进程的配置监视器：源文件是编辑面，链是账本，所以监视器
// 只回答“内容变了吗”，不回答“mtime 变了吗”。它以内容哈希轮询（默认 500ms），
// 变化在防抖窗口（默认 1s）内只触发一次 Host.Reload；Reload 是唯一发布入口的
// 既有调用面，失败（坏 YAML、治理拒绝、安装被拒）只记录并把已知哈希推进到这份
// 坏内容，进程继续服务、绑定保持原代——同一个坏文件不会被每个轮询周期重试。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/SpellingDragon/tagent/assembly"
)

// configWatchInterval is how often the watcher reads the source file's content.
const configWatchInterval = 500 * time.Millisecond

// configWatchDebounce is how long one content change must stand still before the
// watcher reloads it: an editor that writes through a temporary file and a rename
// states the settled content once instead of reloading the half-written one.
const configWatchDebounce = time.Second

// configWatcher polls one source file by content and reloads it through the
// host's one reload entry. It keeps no copy of the configuration and publishes
// nothing itself: reload is the supervised host's own entry.
type configWatcher struct {
	path     string
	interval time.Duration
	debounce time.Duration
	reload   func(context.Context) error
	onError  func(error)
}

// watchConfig returns the watcher of one source path: the production intervals
// by default, overridable by the caller for its own tests.
func watchConfig(path string, reload func(context.Context) error, onError func(error)) *configWatcher {
	return &configWatcher{
		path:     path,
		interval: configWatchInterval,
		debounce: configWatchDebounce,
		reload:   reload,
		onError:  onError,
	}
}

// watchHostConfig wires the watcher to the host's one reload entry: the file
// content change re-enters the same Reload a manual /reload runs, and a refused
// reload is reported without publishing anything itself.
func watchHostConfig(path string, host *assembly.Host, onError func(error)) *configWatcher {
	return watchConfig(path, func(ctx context.Context) error {
		report, err := host.Reload(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "config reload: generation %d in service\n", report.Publication.Generation)
		return nil
	}, onError)
}

// run polls until the context ends. A read failure is reported and retried: the
// file may be replaced mid-write, and the settled content that follows is the
// one the debounce window reloads.
func (w *configWatcher) run(ctx context.Context) {
	if w == nil || w.reload == nil {
		return
	}
	interval := w.interval
	if interval <= 0 {
		interval = configWatchInterval
	}
	debounce := w.debounce
	if debounce <= 0 {
		debounce = configWatchDebounce
	}
	known, err := configContentHash(w.path)
	if err != nil {
		w.report(err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	candidate := known
	var candidateSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			sum, err := configContentHash(w.path)
			if err != nil {
				w.report(err)
				continue
			}
			if sum != candidate {
				candidate = sum
				candidateSince = now
				continue
			}
			if sum == known || now.Sub(candidateSince) < debounce {
				continue
			}
			known = candidate
			if err := w.reload(ctx); err != nil {
				w.report(err)
			}
		}
	}
}

// report hands one watcher failure to the process log; a nil callback keeps the
// failure silent for the tests that inject their own accounting.
func (w *configWatcher) report(err error) {
	if err == nil || w.onError == nil {
		return
	}
	w.onError(err)
}

// configContentHash answers the digest of one source file's content. The digest
// covers no modification time: a touch alone changes nothing the watcher reads.
func configContentHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read the configuration source %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
