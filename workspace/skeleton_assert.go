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

	"github.com/SpellingDragon/tagent/event"
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
		Segment:  memory.SegmentRequest{},
	}

	// The byte-bounded segment entry and the result it is read back as stay the
	// memory segment shapes: a byte offset and a byte bound on the way in, the
	// segment bytes, the resumption position and an EOF flag on the way out, and
	// never a line count.
	_ = memory.SegmentRequest{
		From:     memory.SegmentPosition{},
		MaxBytes: 0,
	}

	_ = memory.SegmentResult{
		Body: []byte{},
		Next: memory.SegmentPosition{},
		EOF:  false,
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

// The replica materialisation shapes: the materializer, the authorized segmented
// read face derived from it, and the named refusals they report.
var (
	_ = Materializer{}
	_ = ReplicaReader{}
	_ = ActiveReplica{}

	_ = MaterializerOption(nil)

	_ error = ErrReplicaRootRequired
	_ error = ErrReplicaUnauthorized
	_ error = ErrOriginalIneffective
	_ error = ErrReplicaParentLinked
	_ error = ErrReplicaProtectionUnavailable
	_ error = ErrCleanerRootMismatch
)

// The materialisation and read methods must keep exactly these signatures.
var (
	_ func(string, memory.ScopedOriginalStore, memory.ReadScope, ...MaterializerOption) (*Materializer, error) = NewMaterializer

	_ func(*Materializer) *ReplicaReader = (*Materializer).Reader

	_ func(*ReplicaCleaner) MaterializerOption = WithCleaner

	_ func(*Materializer, context.Context, memory.OriginalRef, string) (Replica, error) = (*Materializer).Materialize

	_ func(*Materializer, context.Context, memory.OriginalRef, string) (*ActiveReplica, error) = (*Materializer).Open

	_ func(*ActiveReplica) = (*ActiveReplica).Close

	_ func(*Materializer, context.Context, event.EventID) (bool, error) = (*Materializer).Rebuildable

	_ func(*ReplicaReader) memory.ReadScope = (*ReplicaReader).Scope

	_ func(*ReplicaReader, context.Context, memory.OriginalRef, string, memory.SegmentRequest) (memory.SegmentResult, error) = (*ReplicaReader).ReadSegment
)

// The reclamation defaults must stay compile-time constants: a variable or a computed
// value stops this block from compiling.
const (
	_ = DefaultCleanupInterval
	_ = DefaultReplicaMaxAge
	_ = DefaultReplicaMaxCount
)

// The reclamation implementation (03.7): the cleaner behind the frozen Cleaner port,
// the active read/write handle it holds replicas with, and the named refusals they
// report.
var (
	_ Cleaner = (*ReplicaCleaner)(nil)

	_ Release = (Release)(nil)

	_ = CleanupOption(nil)

	_ error = ErrCleanupTargetOutOfRange
	_ error = ErrCleanupTargetMissing
	_ error = ErrCleanupAgeInvalid
	_ error = ErrCleanupCountInvalid
	_ error = ErrCleanupIntervalInvalid
	_ error = ErrCleanerAlreadyRunning
)

// The reclamation methods must keep exactly these signatures.
var (
	_ func(string, ...CleanupOption) (*ReplicaCleaner, error) = NewReplicaCleaner

	_ func(time.Duration) CleanupOption = WithCleanupInterval

	_ func(time.Duration) CleanupOption = WithReplicaMaxAge

	_ func(int) CleanupOption = WithReplicaMaxCount

	_ func(*ReplicaCleaner, string) (Release, error) = (*ReplicaCleaner).Acquire

	_ func(*ReplicaCleaner, context.Context) error = (*ReplicaCleaner).CleanupStartup

	_ func(*ReplicaCleaner, context.Context, time.Duration) error = (*ReplicaCleaner).CleanupByAge

	_ func(*ReplicaCleaner, context.Context, int) error = (*ReplicaCleaner).CleanupByCount

	_ func(*ReplicaCleaner, context.Context) error = (*ReplicaCleaner).Run

	_ func(*ReplicaCleaner) = (*ReplicaCleaner).Stop

	_ func(*ReplicaCleaner, context.Context) error = (*ReplicaCleaner).Wait
)
