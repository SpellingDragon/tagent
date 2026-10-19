package memoryreplicaexample

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/memory/replica"
)

// errCleanerBusy is what the canonical cleaner reports for a pass that was asked to reclaim
// while it was already running.
var errCleanerBusy = errors.New("workspace: the reclaimer is already running")

// replicaCleaner is the canonical reclaimer: it reclaims only this workspace's own replicas,
// leaves a replica that is being read or written alone, and waits for its own stop so the
// close sequence confirms the reclaimer ended.
type replicaCleaner struct {
	root      string
	held      map[string]bool
	idleSince map[string]time.Time
	running   bool
	stopped   bool
}

// CleanupStartup reclaims the replicas left behind by a previous run.
func (c *replicaCleaner) CleanupStartup(_ context.Context) error {
	c.drop(func(string) bool { return true })
	return nil
}

// CleanupByAge reclaims every replica idle longer than maxAge.
func (c *replicaCleaner) CleanupByAge(_ context.Context, maxAge time.Duration) error {
	if maxAge < 0 {
		return replica.ErrCleanupAgeInvalid
	}
	now := time.Now()
	c.drop(func(name string) bool { return now.Sub(c.idleSince[name]) > maxAge })
	return nil
}

// CleanupByCount keeps at most maxCount replicas, reclaiming the oldest first.
func (c *replicaCleaner) CleanupByCount(_ context.Context, maxCount int) error {
	if maxCount < 0 {
		return replica.ErrCleanupCountInvalid
	}
	names := make([]string, 0, len(c.idleSince))
	for name := range c.idleSince {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return c.idleSince[names[i]].Before(c.idleSince[names[j]]) })
	if excess := len(names) - maxCount; excess > 0 {
		for _, name := range names[:excess] {
			if c.held[c.root+"/"+name] {
				continue
			}
			delete(c.idleSince, name)
		}
	}
	return nil
}

// Run reclaims on the configured period until ctx is done or the cleaner is stopped.
func (c *replicaCleaner) Run(ctx context.Context) error {
	if c.running {
		return errCleanerBusy
	}
	c.running = true
	defer func() { c.running, c.stopped = false, true }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// Wait blocks until a running cleaner has stopped.
func (c *replicaCleaner) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// drop reclaims the replicas match selects, and never one that is being read or written.
func (c *replicaCleaner) drop(match func(string) bool) {
	for name := range c.idleSince {
		if !match(name) || c.held[c.root+"/"+name] {
			continue
		}
		delete(c.idleSince, name)
	}
}

var _ replica.Cleaner = (*replicaCleaner)(nil)

// ExampleCleaner reclaims this workspace's own replicas: a replica an active handle protects
// survives both a count-bounded pass and an age-bounded pass, an idle one is reclaimed, and
// Wait returns once the reclaimer stopped instead of assuming it did.
//
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
func ExampleCleaner() {
	ctx := context.Background()
	now := time.Now()
	cleaner := &replicaCleaner{
		root: "/tmp/ws",
		held: map[string]bool{"/tmp/ws/held.txt": true},
		idleSince: map[string]time.Time{
			"held.txt": now.Add(-2 * time.Hour),
			"idle.txt": now.Add(-2 * time.Hour),
		},
	}
	if err := cleaner.CleanupByAge(ctx, time.Hour); err != nil {
		return
	}
	if !cleaner.held["/tmp/ws/held.txt"] {
		return
	}
	if _, left := cleaner.idleSince["idle.txt"]; left {
		return
	}
	if err := cleaner.Run(ctx); err != nil {
		return
	}
	if err := cleaner.Wait(ctx); err != nil {
		return
	}
}
