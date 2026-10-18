// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
//
// Temporary-replica reclamation: the startup and periodic cleanup that take the
// replicas back, and the active read/write protection that keeps a replica in use
// out of every reclamation pass.
//
// Reclamation owns exactly one area on disk: the replica area the materializer
// writes into, `<root>/replica/`. Everything outside that area is a user file and is
// never touched, and inside the area only a regular file is ever removed: a
// traversal never follows a symbolic link and a removal is preceded by a link-free
// check, so reclamation can never cross a link out of the replica root. A replica
// being read or written is held by an active read/write handle and is left alone by
// every trigger until the handle is released.
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrCleanupTargetOutOfRange reports an active read/write handle asked to protect a
// path outside this workspace's own replica area. Nothing is protected and nothing is
// ever reclaimed outside that area.
var ErrCleanupTargetOutOfRange = errors.New("workspace: cleanup target lies outside the replica root")

// ErrCleanupTargetMissing reports an active read/write handle asked to protect a path
// that is not a standing regular file in the replica area, so there is no replica
// there to hold a read or a write on.
var ErrCleanupTargetMissing = errors.New("workspace: cleanup target is not a standing replica file")

// ErrCleanupAgeInvalid reports a reclamation pass started with a negative age bound,
// which would delete replicas that were never idle at all.
var ErrCleanupAgeInvalid = errors.New("workspace: cleanup max age must not be negative")

// ErrCleanupCountInvalid reports a reclamation pass started with a negative count
// bound, which has no meaning as a number of replicas to keep.
var ErrCleanupCountInvalid = errors.New("workspace: cleanup max count must not be negative")

// ErrCleanupIntervalInvalid reports a cleaner constructed without a positive period,
// which could never trigger a periodic reclamation pass at all.
var ErrCleanupIntervalInvalid = errors.New("workspace: cleanup interval must be positive")

// ErrCleanerAlreadyRunning reports a second Run started on a cleaner whose
// reclamation loop is already running, so two loops would reclaim over one another.
var ErrCleanerAlreadyRunning = errors.New("workspace: cleaner is already running")

// replicaStagingPrefix names the write staging files the materializer creates beside
// the replica it is about to rename into place. Reclamation never takes one: it is
// half of an active write, not a standing replica.
const replicaStagingPrefix = ".replica-"

// Release frees one active read/write handle acquired by Acquire. Releasing twice
// frees the protection once.
type Release func()

// cleanupConfig holds the period and the bounds one cleaner reclaims under. Every
// value falls back to its package default when the caller does not name it.
type cleanupConfig struct {
	interval time.Duration
	maxAge   time.Duration
	maxCount int
}

// CleanupOption names one setting of a cleaner at construction time.
type CleanupOption func(*cleanupConfig)

// WithCleanupInterval sets the period between two periodic reclamation passes.
func WithCleanupInterval(interval time.Duration) CleanupOption {
	return func(config *cleanupConfig) { config.interval = interval }
}

// WithReplicaMaxAge sets the idle age above which the periodic pass reclaims a
// replica.
func WithReplicaMaxAge(maxAge time.Duration) CleanupOption {
	return func(config *cleanupConfig) { config.maxAge = maxAge }
}

// WithReplicaMaxCount sets the number of idle replicas the periodic pass keeps before
// the oldest are reclaimed.
func WithReplicaMaxCount(maxCount int) CleanupOption {
	return func(config *cleanupConfig) { config.maxCount = maxCount }
}

// ReplicaCleaner is the temporary-replica reclaimer behind the Cleaner port: it
// reclaims the replicas left behind at startup and on the configured period, bounded
// by age and by count, and it leaves every replica an active read/write handle holds.
//
// Reclamation is limited to this workspace's own ownership on disk: the replica area
// under the root the materializer writes into. A user file is never touched whatever
// its name, and no symbolic link is ever followed out of the replica root, so a link
// planted in that area neither hides a user file from its owner nor exposes it to
// reclamation. The age and count bounds apply to the replicas no handle holds; a
// held replica is kept regardless of either bound and is not counted against the
// number kept.
type ReplicaCleaner struct {
	root     string
	replicas string
	interval time.Duration
	maxAge   time.Duration
	maxCount int

	mu     sync.Mutex
	active map[string]int
	run    bool
	stop   chan struct{}
	done   chan struct{}
}

// NewReplicaCleaner returns a cleaner that reclaims only the replicas materialised
// under root, on the given period and within the given bounds. A root that names
// nothing is refused by name before any cleaner exists, and a period that could never
// trigger is refused the same way.
func NewReplicaCleaner(root string, options ...CleanupOption) (*ReplicaCleaner, error) {
	if root == "" {
		return nil, ErrReplicaRootRequired
	}
	config := cleanupConfig{
		interval: DefaultCleanupInterval,
		maxAge:   DefaultReplicaMaxAge,
		maxCount: DefaultReplicaMaxCount,
	}
	for _, option := range options {
		option(&config)
	}
	if config.interval <= 0 {
		return nil, ErrCleanupIntervalInvalid
	}
	if config.maxAge < 0 {
		return nil, ErrCleanupAgeInvalid
	}
	if config.maxCount < 0 {
		return nil, ErrCleanupCountInvalid
	}
	cleaned := filepath.Clean(root)
	return &ReplicaCleaner{
		root:     cleaned,
		replicas: filepath.Join(cleaned, "replica"),
		interval: config.interval,
		maxAge:   config.maxAge,
		maxCount: config.maxCount,
		active:   make(map[string]int),
	}, nil
}

// Acquire marks the replica at path as being read or written right now and returns
// the handle that releases it. Every reclamation pass leaves that replica alone until
// the handle is released, so the bytes a reader or writer holds are never pulled away
// underneath them. A path outside the replica area is refused by name and protects
// nothing, and a path where no standing regular file exists is refused the same way:
// a handle holds a replica that is there, not one that might appear later.
func (c *ReplicaCleaner) Acquire(path string) (Release, error) {
	target := filepath.Clean(path)
	if !withinRoot(c.replicas, target) {
		return nil, ErrCleanupTargetOutOfRange
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := standingRegular(target); err != nil {
		return nil, err
	}
	return c.registerLocked(target), nil
}

// hold marks path as being read or written and returns the release that frees the
// mark. The registration is unchecked: the callers that cover a replica across its
// materialisation register the target before its bytes land, and a handle that must
// name a standing replica goes through Acquire instead. Releasing twice frees the
// mark once.
func (c *ReplicaCleaner) hold(path string) Release {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registerLocked(filepath.Clean(path))
}

// registerLocked adds one active read/write mark on the cleaned target and returns
// the release that takes it back. The registration shares the lock every reclamation
// pass holds from collection to removal, so it is visible to that pass or arrives
// after it.
func (c *ReplicaCleaner) registerLocked(target string) Release {
	c.active[target]++
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.active[target] > 1 {
				c.active[target]--
				return
			}
			delete(c.active, target)
		})
	}
}

// holders reports how many active read/write marks stand on path right now, so a
// caller that would rewrite the replica there learns whether anyone besides itself
// covers those bytes.
func (c *ReplicaCleaner) holders(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active[filepath.Clean(path)]
}

// CleanupStartup reclaims the temporary replicas left behind by a previous run: every
// replica in this workspace's own area that no active read/write handle holds. A
// replica reclaimed here is written again from its original the next time it is
// needed, so startup reclamation costs a replica, never an original.
func (c *ReplicaCleaner) CleanupStartup(ctx context.Context) error {
	return c.reclaim(ctx, func(idle []replicaCandidate) []replicaCandidate { return idle })
}

// CleanupByAge reclaims every replica that has been idle longer than maxAge and holds
// no active read/write handle. Idle time is measured on the file itself, so it
// survives a process restart the way the replica does. A negative maxAge is refused
// by name and reclaims nothing.
func (c *ReplicaCleaner) CleanupByAge(ctx context.Context, maxAge time.Duration) error {
	if maxAge < 0 {
		return ErrCleanupAgeInvalid
	}
	deadline := time.Now().Add(-maxAge)
	return c.reclaim(ctx, func(idle []replicaCandidate) []replicaCandidate {
		var expired []replicaCandidate
		for _, candidate := range idle {
			if candidate.modified.Before(deadline) {
				expired = append(expired, candidate)
			}
		}
		return expired
	})
}

// CleanupByCount keeps at most maxCount replicas no active read/write handle holds,
// reclaiming the oldest first. A held replica is kept whatever the bound and is not
// counted against the number kept, so the count can never reclaim one that is in use.
// A negative maxCount is refused by name and reclaims nothing.
func (c *ReplicaCleaner) CleanupByCount(ctx context.Context, maxCount int) error {
	if maxCount < 0 {
		return ErrCleanupCountInvalid
	}
	return c.reclaim(ctx, func(idle []replicaCandidate) []replicaCandidate {
		sort.SliceStable(idle, func(i, j int) bool {
			return idle[i].modified.Before(idle[j].modified)
		})
		if len(idle) <= maxCount {
			return nil
		}
		return idle[:len(idle)-maxCount]
	})
}

// Run reclaims on the configured period: one pass by age and one by count every
// period, until ctx is done or Stop is called. Run reports the end of the loop
// through Wait, so a close sequence confirms the reclaimer ended instead of assuming
// it did. A second Run on a running cleaner is refused by name instead of reclaiming
// over the loop that is already there.
func (c *ReplicaCleaner) Run(ctx context.Context) error {
	c.mu.Lock()
	if c.run {
		c.mu.Unlock()
		return ErrCleanerAlreadyRunning
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	c.run = true
	c.stop = stop
	c.done = done
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.run = false
		c.stop = nil
		c.mu.Unlock()
		close(done)
	}()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		case <-ticker.C:
			if err := c.CleanupByAge(ctx, c.maxAge); err != nil {
				return err
			}
			if err := c.CleanupByCount(ctx, c.maxCount); err != nil {
				return err
			}
		}
	}
}

// Stop ends a running reclamation loop. Stopping a cleaner whose loop is not running
// ends nothing and breaks nothing, so the close sequence can call it unconditionally.
func (c *ReplicaCleaner) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
}

// Wait blocks until a running reclamation loop has stopped, so a close sequence
// confirms the reclaimer ended instead of assuming it did. A cleaner whose loop never
// started or has already ended reports the end immediately; a ctx that runs out first
// reports its own error instead.
func (c *ReplicaCleaner) Wait(ctx context.Context) error {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// replicaCandidate is one reclaimable file in the replica area: its path and the time
// it was last written, which is the idle clock reclamation ages it on.
type replicaCandidate struct {
	path     string
	modified time.Time
}

// reclaim runs one reclamation pass over this workspace's own replica area: it
// collects the standing regular files there, keeps the ones an active read/write
// handle holds, hands the rest to select to choose the victims and removes exactly
// those. The pass holds its lock from collection to removal, so a hold taken while
// a pass runs is either seen by that pass or never threatened by it. Files outside
// the replica area and symbolic links inside it are never candidates.
func (c *ReplicaCleaner) reclaim(ctx context.Context, selectVictims func([]replicaCandidate) []replicaCandidate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	idle, err := c.collect(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, victim := range selectVictims(idle) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.remove(victim.path); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// collect lists the reclaimable files in the replica area: the standing regular files
// no active read/write handle holds. The traversal never follows a symbolic link, a
// path that would leave the replica area is never a candidate, and the write staging
// files the materializer renames into place are never candidates either: a staged
// write is an active write, and reclamation takes replicas, never half of one.
func (c *ReplicaCleaner) collect(ctx context.Context) ([]replicaCandidate, error) {
	var idle []replicaCandidate
	err := filepath.WalkDir(c.replicas, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), replicaStagingPrefix) {
			return nil
		}
		if !withinRoot(c.replicas, path) {
			return nil
		}
		if c.active[filepath.Clean(path)] > 0 {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		idle = append(idle, replicaCandidate{path: path, modified: info.ModTime()})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return idle, err
}

// remove unlinks one replica from the replica area. The file is checked link-free
// right before the unlink, so the removal takes a regular file that is still there and
// can never reach through a link to something else. A file that disappeared between
// the pass that chose it and this removal is already reclaimed, which is not a
// failure.
func (c *ReplicaCleaner) remove(path string) error {
	if err := standingRegular(path); err != nil {
		if errors.Is(err, ErrCleanupTargetMissing) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// standingRegular reports nil only while path names a standing regular file: a
// symbolic link is refused with it, so the caller never treats a link as the replica
// it points at.
func standingRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return ErrCleanupTargetMissing
	}
	if !info.Mode().IsRegular() {
		return ErrCleanupTargetMissing
	}
	return nil
}

// withinRoot reports whether path lies inside root without crossing out of it: a
// parent traversal, an absolute step aside and a representation that cannot be made
// relative to root all answer false.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}
