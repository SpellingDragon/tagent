// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
//
// Skeleton compile-time assertions for the phase 1 workspace shapes. The phase 2
// implementation types that will satisfy the two ports do not exist yet, so there is no
// `var _ Iface = (*Impl)(nil)` to write; the assertions below prove what the skeleton can
// prove on its own, pinning every declared field name and type, every declared method
// signature, and every constant that must stay a compile-time constant.
package workspace

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/memory"
)

// The declared shapes and their fields.
var (
	_ = Replica{
		Original:  memory.OriginalRef{},
		Path:      "",
		CreatedAt: time.Time{},
		ActiveRW:  false,
	}

	_ = Externalized{
		Original: memory.OriginalRef{},
		Preview:  "",
		Segment:  SegmentEntry{},
	}

	_ = SegmentEntry{
		StartLine: 0,
		NumLines:  0,
	}
)

// The exported port methods must keep exactly these signatures.
var (
	_ func(Externalizer, context.Context, memory.OriginalRef, []byte) (Externalized, error) = Externalizer.Externalize

	_ func(Cleaner, context.Context) error = Cleaner.CleanupStartup

	_ func(Cleaner, context.Context, time.Duration) error = Cleaner.CleanupByAge

	_ func(Cleaner, context.Context, int) error = Cleaner.CleanupByCount

	_ func(Cleaner, context.Context) error = Cleaner.Run

	_ func(Cleaner, context.Context) error = Cleaner.Wait
)

// The declared ports must stay interface types.
var (
	_ Externalizer = (Externalizer)(nil)
	_ Cleaner      = (Cleaner)(nil)
)

// ErrReplicaWriteFailed must stay a named error value.
var _ error = ErrReplicaWriteFailed

// The reclamation defaults must stay compile-time constants: a variable or a computed
// value stops this block from compiling.
const (
	_ = DefaultCleanupInterval
	_ = DefaultReplicaMaxAge
	_ = DefaultReplicaMaxCount
)
