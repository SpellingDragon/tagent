// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
//
// Package replica is the memory domain's on-disk scratch layer: it holds the temporary
// replicas materialised from durable originals and reclaims them, and it carries the
// port that commits an oversized result as an original before it serves a bounded
// preview of that result.
//
// The package reads originals through the memory port and publishes nothing but facts
// about scratch files, so it can never be reversed into the durable side or the
// governing side. Replicas are materialised under stable paths and read back in
// byte-bounded segments through the scoped read face of memory, so a replica never
// outruns the authorization it was materialised under.
package replica

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"

	"github.com/SpellingDragon/tagent/memory/ledger"
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
	Original ledger.OriginalRef
	// Path is the on-disk location of the replica, declared once the replica was
	// written successfully.
	Path string
	// CreatedAt is the time the replica was materialised.
	CreatedAt time.Time
	// ActiveRW reports whether the replica is being read or written, so reclamation
	// leaves it alone while it is in use.
	ActiveRW bool
}

// ErrReplicaRootRequired reports a materializer constructed without a replica root,
// so no replica could ever be written under a declared path.
var ErrReplicaRootRequired = errors.New("workspace: replica root is required")

// ErrReplicaUnauthorized reports a replica request that lies outside the trusted
// read scope the materializer runs in: no replica is written and no byte is served,
// and the refusal names the authorization boundary rather than the content behind
// it.
var ErrReplicaUnauthorized = errors.New("workspace: replica request lies outside the read scope")

// ErrOriginalIneffective reports a replica that cannot be materialised again because
// the original it would be rebuilt from is missing or outside its retention window
// inside the bound read scope. The refusal names the absent rebuild instead of
// declaring a replica whose bytes were never written.
var ErrOriginalIneffective = errors.New("workspace: the original is ineffective, its replica cannot be rebuilt")

// ErrReplicaParentLinked reports a materialisation refused by name because a parent
// of the replica path is a symbolic link: the declared path would cross a link to
// reach its bytes, so no replica is written and nothing stands beside what the link
// points at.
var ErrReplicaParentLinked = errors.New("workspace: a parent of the replica path is a symbolic link")

// ErrReplicaProtectionUnavailable reports an active-use replica request on a
// materializer with no replica cleaner wired, so there is no protection to hold over
// the working copy it would serve. The request is refused by name instead of serving
// a copy no protection covers.
var ErrReplicaProtectionUnavailable = errors.New("workspace: active replica protection requires a wired replica cleaner")

// ErrCleanerRootMismatch reports a materializer wired to a replica cleaner that
// reclaims a replica area other than the one under this materializer's root: the
// protection behind a working copy would name an area its bytes never stand in. The
// construction is refused by name.
var ErrCleanerRootMismatch = errors.New("workspace: the wired cleaner reclaims a different replica area")

// Materializer materialises replicas of durable originals on disk inside one trusted
// read scope: it reads originals only through that scope, and it declares a replica
// path only where the replica bytes were written successfully.
//
// One materialisation target is the pair of a registered reference and the consumer
// it is materialised for. Every target resolves to its own stable path derived from
// those two alone, so the same target lands on the same path across process restarts
// and across owner, tool and session boundaries no two targets share a path or
// overwrite one another. A replica standing at its path is evaluated against its
// original at every materialisation: bytes that still answer the effective original
// are reused as they stand, and an original that moved on is shadowed again in place
// at that evaluation, while a read or write already covering those bytes defers the
// rewrite to a later one. A missing replica of an ineffective original is refused by
// name, so the original's validity and the replica's lifetime stay two separate
// facts.
type Materializer struct {
	root    string
	reader  *ledger.ScopedReader
	cleaner *ReplicaCleaner
}

// MaterializerOption names one setting of a materializer at construction time.
type MaterializerOption func(*Materializer)

// WithCleaner wires the replica cleaner whose active read/write protection covers
// the replicas materialised here: a working copy and a materialisation span hold
// their marks on that cleaner, so every reclamation pass it runs leaves those
// replicas alone. A cleaner that reclaims another replica area than the one under
// this materializer's root is refused by name at construction.
func WithCleaner(cleaner *ReplicaCleaner) MaterializerOption {
	return func(materializer *Materializer) { materializer.cleaner = cleaner }
}

// NewMaterializer returns a materializer that declares replicas under root and reads
// originals only through store, inside scope. The scope comes from the trusted state
// of the caller rather than from any per-replica request, and a scope that names
// nothing is refused by name before any replica exists. The options may wire the
// active read/write protection behind the replicas written here, and a cleaner
// reclaiming another replica area is refused by name.
func NewMaterializer(root string, store ledger.ScopedOriginalStore, scope ledger.ReadScope, options ...MaterializerOption) (*Materializer, error) {
	if root == "" {
		return nil, ErrReplicaRootRequired
	}
	reader, err := ledger.NewScopedReader(store, scope)
	if err != nil {
		return nil, err
	}
	materializer := &Materializer{root: root, reader: reader}
	for _, option := range options {
		option(materializer)
	}
	if materializer.cleaner != nil && materializer.cleaner.replicas != filepath.Join(filepath.Clean(root), "replica") {
		return nil, ErrCleanerRootMismatch
	}
	return materializer, nil
}

// Reader returns the authorized segmented read face of this materializer, bound to
// the same trusted read scope and the same replica root.
func (m *Materializer) Reader() *ReplicaReader {
	return &ReplicaReader{materializer: m}
}

// Materialize materialises one replica of ref for consumer and returns its stable
// reference. A reference outside the bound read scope is refused by name and writes
// no replica, a replica standing at the target path is evaluated against its original
// and answered as it stands or shadowed again on the answer, and a missing one is
// written again from the original while that original is effective inside the bound
// scope. The path is part of the returned reference only once the replica bytes were
// written successfully, so a declared path always names a replica that exists whole.
// The call holds active protection on the target until it returns, so no reclamation
// pass takes the replica away across a materialisation.
func (m *Materializer) Materialize(ctx context.Context, ref ledger.OriginalRef, consumer string) (Replica, error) {
	if ref.Key == "" {
		return Replica{}, ledger.ErrKeyRequired
	}
	if ref.ReadScope() != m.reader.Scope() {
		return Replica{}, ErrReplicaUnauthorized
	}
	path := m.replicaPath(ref, consumer)
	release := m.holdSpan(path)
	defer release()
	return m.materializeAt(ctx, ref, path)
}

// materializeAt materialises one replica of ref at path and returns its reference,
// under the protection the caller already holds on that path: a parent of the path
// that is a symbolic link is refused by name before anything is written, and a
// replica standing at the path is evaluated against its original before it is
// answered.
func (m *Materializer) materializeAt(ctx context.Context, ref ledger.OriginalRef, path string) (Replica, error) {
	if err := m.refuseLinkedParents(path); err != nil {
		return Replica{}, err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		return m.evaluate(ctx, ref, path, info)
	}
	recalled := m.reader.Get(ctx, ref.Key)
	switch recalled.Outcome {
	case ledger.RecallFound:
	case ledger.RecallFailed:
		return Replica{}, recalled.Err
	default:
		return Replica{}, ErrOriginalIneffective
	}
	created, err := m.writeReplica(path, recalled.Body)
	if err != nil {
		return Replica{}, err
	}
	return Replica{Original: ref, Path: path, CreatedAt: created}, nil
}

// evaluate answers a replica standing at path at this materialisation: the bytes on
// disk are compared with the effective original, and an original that moved on is
// shadowed again in place while the caller is the only one covering those bytes. A
// read or write already covering them defers the rewrite to a later materialisation,
// and an original that is missing or ineffective leaves the standing replica as the
// scratch file it is.
func (m *Materializer) evaluate(ctx context.Context, ref ledger.OriginalRef, path string, info os.FileInfo) (Replica, error) {
	recalled := m.reader.Get(ctx, ref.Key)
	if recalled.Outcome == ledger.RecallFound && m.holdsOn(path) <= 1 {
		if standing, err := os.ReadFile(path); err == nil && !bytes.Equal(standing, recalled.Body) {
			created, err := m.writeReplica(path, recalled.Body)
			if err != nil {
				return Replica{}, err
			}
			return Replica{Original: ref, Path: path, CreatedAt: created}, nil
		}
	}
	return Replica{Original: ref, Path: path, CreatedAt: info.ModTime()}, nil
}

// holdSpan returns the release of the active read/write mark covering one
// materialisation at path on the wired cleaner. A materializer with no cleaner holds
// no mark, so its spans protect nothing and end with the call.
func (m *Materializer) holdSpan(path string) Release {
	if m.cleaner == nil {
		return func() {}
	}
	return m.cleaner.hold(path)
}

// holdsOn reports the number of active read/write marks standing on path, the
// caller's own mark among them: one is the caller alone, and anything above it is
// another reader or writer covering those bytes. A materializer with no cleaner sees
// no marks at all.
func (m *Materializer) holdsOn(path string) int {
	if m.cleaner == nil {
		return 0
	}
	return m.cleaner.holders(path)
}

// Open materialises one working copy of ref for consumer and returns the handle that
// ends its active read/write protection. The protection is held before the copy's
// bytes land and stays held until Close, so every reclamation pass leaves the copy
// alone and every evaluation of its path defers a rewrite while the copy is open. A
// reference outside the bound read scope is refused by name and holds nothing, and a
// materializer wired to no cleaner is refused the same way: a working copy is served
// only where its protection exists.
func (m *Materializer) Open(ctx context.Context, ref ledger.OriginalRef, consumer string) (*ActiveReplica, error) {
	if ref.Key == "" {
		return nil, ledger.ErrKeyRequired
	}
	if ref.ReadScope() != m.reader.Scope() {
		return nil, ErrReplicaUnauthorized
	}
	if m.cleaner == nil {
		return nil, ErrReplicaProtectionUnavailable
	}
	path := m.replicaPath(ref, consumer)
	release := m.cleaner.hold(path)
	replica, err := m.materializeAt(ctx, ref, path)
	if err != nil {
		release()
		return nil, err
	}
	replica.ActiveRW = true
	return &ActiveReplica{Replica: replica, release: release}, nil
}

// ActiveReplica is one working copy of a durable original on disk: the replica the
// copy stands at and the close that ends the protection behind it. The replica keeps
// its exact bytes while the handle is open, and every reclamation pass leaves it
// alone until Close ends the protection.
type ActiveReplica struct {
	Replica
	release Release
}

// Close ends the active read/write protection behind this working copy, so a
// reclamation pass may take the replica back and a later materialisation may shadow
// it again. Closing twice ends the protection once, and closing a nil handle ends
// nothing and breaks nothing.
func (a *ActiveReplica) Close() {
	if a == nil {
		return
	}
	a.release()
}

// Rebuildable reports whether a replica of key can be materialised again from its
// original: true while the original is still effective inside the bound read scope,
// false once it is missing or outside its retention window there. The answer is
// presented as such, so a missing replica is never claimed as rebuildable once the
// original behind it is ineffective.
func (m *Materializer) Rebuildable(ctx context.Context, key event.EventID) (bool, error) {
	recalled := m.reader.Get(ctx, key)
	switch recalled.Outcome {
	case ledger.RecallFound:
		return true, nil
	case ledger.RecallFailed:
		return false, recalled.Err
	default:
		return false, nil
	}
}

// writeReplica writes the replica bytes to path behind a temporary file, so the
// declared path holds the complete replica or nothing at all. The returned time is
// the moment the replica landed at its path, a write that cannot be completed is
// reported as ErrReplicaWriteFailed with its cause, and a parent of the path that is
// a symbolic link is refused as ErrReplicaParentLinked before any byte is written.
func (m *Materializer) writeReplica(path string, body []byte) (time.Time, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", ErrReplicaWriteFailed, err)
	}
	if err := m.refuseLinkedParents(path); err != nil {
		return time.Time{}, err
	}
	temp, err := os.CreateTemp(directory, ".replica-*")
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", ErrReplicaWriteFailed, err)
	}
	tempName := temp.Name()
	_, writeErr := temp.Write(body)
	closeErr := temp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(tempName)
		cause := writeErr
		if cause == nil {
			cause = closeErr
		}
		return time.Time{}, fmt.Errorf("%w: %w", ErrReplicaWriteFailed, cause)
	}
	if err := os.Rename(tempName, path); err != nil {
		os.Remove(tempName)
		return time.Time{}, fmt.Errorf("%w: %w", ErrReplicaWriteFailed, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", ErrReplicaWriteFailed, err)
	}
	return info.ModTime(), nil
}

// refuseLinkedParents reports ErrReplicaParentLinked when an existing parent of
// path, up to and including the replica root, is a symbolic link, so no byte of a
// replica is read or written through that link. A parent that names nothing yet and
// a parent that is an ordinary file are left to the write that follows to report.
func (m *Materializer) refuseLinkedParents(path string) error {
	root := filepath.Clean(m.root)
	for parent := filepath.Dir(filepath.Clean(path)); ; parent = filepath.Dir(parent) {
		if info, err := os.Lstat(parent); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return ErrReplicaParentLinked
		}
		if parent == root || parent == filepath.Dir(parent) {
			return nil
		}
	}
}

// replicaPath returns the stable path one materialisation target is declared at: one
// directory per original and one file per target identity, derived from the reference
// and the consumer alone, so the path survives a process restart and no two targets
// ever collide on one.
func (m *Materializer) replicaPath(ref ledger.OriginalRef, consumer string) string {
	original := sha256.Sum256([]byte(ref.Key))
	identity := sha256.Sum256([]byte(replicaIdentity(ref, consumer)))
	return filepath.Join(m.root, "replica", hex.EncodeToString(original[:]), hex.EncodeToString(identity[:]))
}

// replicaIdentity is the canonical text one materialisation target is derived from:
// the key, the owner, the format, the retention policy and the consumer, each
// length-prefixed so no value can be confused with the one beside it.
func replicaIdentity(ref ledger.OriginalRef, consumer string) string {
	fields := []string{
		string(ref.Key),
		string(ref.Owner.AgentID),
		string(ref.Owner.SessionID),
		ref.Format,
		ref.RetentionPolicy,
		consumer,
	}
	var identity strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&identity, "%d:%s", len(field), field)
	}
	return identity.String()
}

// ReplicaReader is the authorized segmented read face over replicas: it serves
// byte-bounded segments of replica content on exactly the terms of
// ledger.SegmentRequest and ledger.SegmentResult, and only inside one trusted read
// scope.
//
// Authorization runs through the scoped read face of memory: a request outside the
// bound scope is refused by name before any replica is touched, the bytes are reached
// through the scope the replica was materialised under, and a replica that is missing
// while its original is still effective is materialised again before the read is
// served. The segments of one replica concatenate back to its exact bytes, cut at
// byte boundaries whatever the content holds.
type ReplicaReader struct {
	materializer *Materializer
}

// Scope returns the trusted read scope the reader serves inside, so a consumer can
// record which authority a result was read under.
func (r *ReplicaReader) Scope() ledger.ReadScope {
	return r.materializer.reader.Scope()
}

// ReadSegment returns one byte-bounded segment of the replica materialised for ref
// under consumer, on exactly the terms of ledger.SegmentRequest and
// ledger.SegmentResult. A request outside the bound read scope is refused by name and
// serves no byte, a missing replica is materialised again from its original before
// any byte is served, and an ineffective original refuses the read by name instead of
// handing out bytes no replica carries.
func (r *ReplicaReader) ReadSegment(ctx context.Context, ref ledger.OriginalRef, consumer string, req ledger.SegmentRequest) (ledger.SegmentResult, error) {
	if ref.Key == "" {
		return ledger.SegmentResult{}, ledger.ErrKeyRequired
	}
	if ref.ReadScope() != r.Scope() {
		return ledger.SegmentResult{}, ErrReplicaUnauthorized
	}
	if _, err := r.materializer.Materialize(ctx, ref, consumer); err != nil {
		return ledger.SegmentResult{}, err
	}
	return r.materializer.reader.ReadSegment(ctx, ref.Key, req)
}
