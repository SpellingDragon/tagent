// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the ledger shapes: every declared field
// name and type, every declared method signature, and every constant that must
// stay a compile-time constant. A rename or a widened signature stops this file
// from compiling, so the storage face cannot drift silently.
package ledger

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// The declared shapes and their fields.
var (
	_ = OriginalRef{
		Key:             "",
		Owner:           event.Address{},
		Format:          "",
		RetentionPolicy: "",
		Integrity:       Integrity{},
	}

	_ = Integrity{
		Digest: "",
		Length: 0,
	}

	_ = RecallResult{
		Key:     "",
		Outcome: RecallUnclassified,
		Body:    []byte{},
		Err:     nil,
	}

	_ = ReadScope{
		Owner:           event.Address{},
		Format:          "",
		RetentionPolicy: "",
	}

	_ = HolderID("")

	_ = ProtectionLease("")

	_ = SegmentPosition{Offset: 0}

	_ = SegmentRequest{
		From:     SegmentPosition{},
		MaxBytes: 0,
	}

	_ = SegmentResult{
		Body: []byte{},
		Next: SegmentPosition{},
		EOF:  false,
	}
)

// The declared contract refusals and the pure accessors they resolve through.
var (
	_ error                       = ErrScopeRequired
	_ error                       = ErrLeaseMismatch
	_ func(ReadScope) error       = ReadScope.Validate
	_ func(OriginalRef) ReadScope = OriginalRef.ReadScope
)

// The exported port methods must keep exactly these signatures.
var (
	_ func(OriginalStore, context.Context, OriginalRef, []byte) error = OriginalStore.Put

	_ func(OriginalStore, context.Context, event.EventID) RecallResult = OriginalStore.Get

	_ func(OriginalStore, context.Context, event.EventID, int, int) ([]byte, error) = OriginalStore.ReadSegment

	_ func(ScopedOriginalStore, context.Context, event.EventID, ReadScope) RecallResult = ScopedOriginalStore.GetScoped

	_ func(ScopedOriginalStore, context.Context, event.EventID, ReadScope, SegmentRequest) (SegmentResult, error) = ScopedOriginalStore.ReadSegmentScoped

	_ func(*ScopedReader, context.Context, event.EventID) RecallResult = (*ScopedReader).Get

	_ func(*ScopedReader, context.Context, event.EventID, SegmentRequest) (SegmentResult, error) = (*ScopedReader).ReadSegment

	_ func(Guard, context.Context, OriginalRef, HolderID) (ProtectionLease, error) = Guard.Hold

	_ func(Guard, context.Context, OriginalRef, HolderID, ProtectionLease) error = Guard.Release

	_ func(Guard, context.Context, OriginalRef) (bool, error) = Guard.Protected
)

// The declared ports must stay interface types.
var (
	_ OriginalStore       = (OriginalStore)(nil)
	_ ScopedOriginalStore = (ScopedOriginalStore)(nil)
	_ Guard               = (Guard)(nil)
)

// The recall outcomes and the finite segment bound must stay compile-time
// constants: a variable or a computed value stops this block from compiling.
const (
	_ = RecallUnclassified
	_ = RecallFound
	_ = RecallMissing
	_ = RecallExpired
	_ = RecallFailed

	_ = DefaultSegmentMaxBytes
)

// The durable fact surface one opened tier keeps its documented shapes: put, get,
// prefix scan and the sync barrier a commit reports its success behind.
var (
	_ func(*Tier) *TierFacts                                              = TierFactLog
	_ func(TierFacts, context.Context, string, []byte) error              = TierFacts.Put
	_ func(TierFacts, context.Context, string) ([]byte, bool, error)      = TierFacts.Get
	_ func(TierFacts, context.Context, string) (map[string][]byte, error) = TierFacts.Scan
	_ func(TierFacts, context.Context) error                              = TierFacts.Sync
)

// The retention leases keep their declared shapes: the registration, the
// retention kind and the declarative lifecycle policy.
var (
	_ Guard                  = (*MemoryGuard)(nil)
	_ func(Guard) *Retention = NewRetention

	_                        = RetentionRecord{Kind: "", Type: "", RecordedAt: time.Time{}}
	_                        = LifecycleConfig{GlobalTTLDays: 0, TypeTTL: map[string]int{}, CheckInterval: 0, MaxEventsPerPartition: 0}
	_ func() LifecycleConfig = DefaultLifecycleConfig

	_ func(*Retention, context.Context, event.EventID, HolderID, RetentionRecord) (ProtectionLease, error) = (*Retention).Register
	_ func(*Retention, context.Context, event.EventID, HolderID, ProtectionLease) error                    = (*Retention).Retire
	_ func(*Retention, context.Context, HolderID) (int, error)                                             = (*Retention).ReleaseHolder

	_ = RetentionUnconfirmed
	_ = RetentionRecovery
)

// The storage lifecycle keeps its declared shapes: the enumerated key, the
// declared capabilities, the coordination surface, the scan report and the
// lifecycle constructor that refuses a durable tier missing its capabilities.
var (
	_ = StoredKey{Key: "", Type: "", Partition: 0, RecordedAt: time.Time{}}
	_ = LifecycleCapabilities{DurableRecovery: false, Protection: false, Reclamation: false}
	_ = ScanReport{Tombstoned: []event.EventID{}, Protected: []event.EventID{}, Exempt: []event.EventID{}}

	_ LifecycleStore = (LifecycleStore)(nil)

	_ func(LifecycleConfig, *Retention, LifecycleStore, LifecycleCapabilities) (*Lifecycle, error) = NewLifecycle
	_ func(*Lifecycle, context.Context, StoredKey) error                                           = (*Lifecycle).RegisterRecovered
	_ func(*Lifecycle)                                                                             = (*Lifecycle).RecoveryComplete
	_ func(*Lifecycle, time.Time)                                                                  = (*Lifecycle).ArmGrace
	_ func(*Lifecycle, context.Context, time.Time) (ScanReport, error)                             = (*Lifecycle).Scan
	_ func(*Lifecycle, context.Context, event.EventID, time.Time) error                            = (*Lifecycle).Delete

	_ error = ErrCapabilityIncomplete
	_ error = ErrRecoveryIncomplete
	_ error = ErrEnumerationIncomplete
	_ error = ErrProtected
	_ error = ErrLifecycleUnwired
)
