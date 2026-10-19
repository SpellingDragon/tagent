package replica

import (
	"context"
	"time"
)

const (
	// DefaultCleanupInterval is the period between two temporary-replica reclamation
	// passes.
	DefaultCleanupInterval = time.Hour
	// DefaultReplicaMaxAge is the idle age above which a temporary replica is reclaimed.
	DefaultReplicaMaxAge = 24 * time.Hour
	// DefaultReplicaMaxCount is the number of temporary replicas kept before the oldest
	// are reclaimed.
	DefaultReplicaMaxCount = 200
)

// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
//
// Cleaner is the temporary-replica reclamation port: it reclaims replicas at startup and
// on a period, bounded by age and by count, and it waits for its own stop so the close
// sequence confirms the reclaimer ended.
//
// The redline list (4 rules):
// MUST leave a replica that is being read or written alone.
// MUST limit reclamation to this workspace's own temporary replicas.
// MUST NOT remove a user file.
// MUST NOT follow a symbolic link out of the replica root.
type Cleaner interface {
	// CleanupStartup reclaims the temporary replicas left behind by a previous run.
	CleanupStartup(ctx context.Context) error
	// CleanupByAge reclaims every replica that has been idle longer than maxAge.
	CleanupByAge(ctx context.Context, maxAge time.Duration) error
	// CleanupByCount keeps at most maxCount replicas, reclaiming the oldest first.
	CleanupByCount(ctx context.Context, maxCount int) error
	// Run reclaims on the configured period until ctx is done or the cleaner is stopped.
	Run(ctx context.Context) error
	// Wait blocks until a running cleaner has stopped, so a close sequence confirms the
	// reclaimer ended instead of assuming it did.
	Wait(ctx context.Context) error
}
