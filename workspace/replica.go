// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
//
// Package workspace is the runtime's on-disk scratch layer: it holds the temporary
// replicas materialised from durable originals and reclaims them, and it carries the
// port that commits an oversized result as an original before it serves a bounded
// preview of that result.
//
// The package reads originals through the memory port and publishes nothing but facts
// about scratch files, so it can never be reversed into the durable side or the
// governing side. This is the phase 1 skeleton: it declares shapes and holds no
// behaviour.
package workspace

import (
	"time"

	"github.com/SpellingDragon/tagent/memory"
)

// Replica is one materialised copy of a durable original on disk: the original it was
// materialised from, the path it was written to, the time it was created, and whether
// it is being read or written at this moment.
//
// A replica's life is separate from the original's: reclaiming a replica never
// invalidates the original, and a replica that is missing while its original is still
// effective is materialised again. The path is declared only after the replica was
// written successfully, and a replica's identity is stable across the tool and the
// owner that asked for it, across sessions, and across a process restart, so two
// replicas of the same original never overwrite one another.
type Replica struct {
	// Original is the durable original this replica was materialised from.
	Original memory.OriginalRef
	// Path is the on-disk location of the replica, declared once the replica was
	// written successfully.
	Path string
	// CreatedAt is the time the replica was materialised.
	CreatedAt time.Time
	// ActiveRW reports whether the replica is being read or written, so reclamation
	// leaves it alone while it is in use.
	ActiveRW bool
}
