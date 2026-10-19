package rl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
)

// identitySeparator is the single separator a rendered model identity carries
// between the model name and its version.
const identitySeparator = "@"

// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
//
// ModelIdentity is one model beside the version of that model that served a
// request: the model name a declaration references and the generation of that
// name the request actually ran.
//
// The identity is what makes a model replacement readable in the records: two
// generations of one model name are two identities, so a record written before
// a replacement is never read as a record of the version installed after it.
type ModelIdentity struct {
	// Model is the model name the request was declared against.
	Model string
	// Version is the version of that model. It is empty when the model ran
	// unversioned, which is recorded as unversioned rather than assumed to be
	// the version in effect.
	Version string
}

// Name renders one identity as the single string a model declares on the
// responses it serves: the model name alone for an unversioned model, and the
// name, the separator and the version for a versioned one.
func (i ModelIdentity) Name() string {
	if i.Version == "" {
		return i.Model
	}
	return i.Model + identitySeparator + i.Version
}

// ParseModelIdentity reads one declared model name back into the name and the
// version it was rendered from. Exactly one separator with a non-empty name and
// version on its two sides is a rendered identity; every other name is read
// whole as an unversioned model name rather than split at a guessed position,
// so a provider name that happens to carry the separator is never split into a
// version it never declared.
func ParseModelIdentity(name string) ModelIdentity {
	modelName, version, found := strings.Cut(name, identitySeparator)
	if !found || modelName == "" || version == "" || strings.Contains(version, identitySeparator) {
		return ModelIdentity{Model: name}
	}
	return ModelIdentity{Model: modelName, Version: version}
}

// ReplacementState states how one replacement attempt ended.
//
// ReplacementRefused is the zero value: an attempt that published no binding is
// never reported as a version that took effect.
type ReplacementState int

const (
	// ReplacementRefused marks an attempt that published no binding: the
	// version in effect is the one it was before the attempt.
	ReplacementRefused ReplacementState = iota
	// ReplacementApplied marks an attempt whose rebuilt model the publication
	// entry accepted.
	ReplacementApplied
)

// replacementStateNames is the explicit wire spelling of each replacement
// state, so a refused attempt is never read as a bare zero.
var replacementStateNames = map[ReplacementState]string{
	ReplacementRefused: "refused",
	ReplacementApplied: "applied",
}

// MarshalJSON writes one replacement state as its explicit wire name.
func (s ReplacementState) MarshalJSON() ([]byte, error) {
	name, ok := replacementStateNames[s]
	if !ok {
		return nil, errUnknownState("replacement state", int(s))
	}
	return json.Marshal(name)
}

// UnmarshalJSON reads one replacement state from its explicit wire name; a name
// this package does not know is refused rather than read as the zero value.
func (s *ReplacementState) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	for state, known := range replacementStateNames {
		if known == name {
			*s = state
			return nil
		}
	}
	return errUnknownState("replacement state", name)
}

// ErrLeaseModelMissing and the lease errors beside it name the way a lease or a
// replacement was refused, so a caller routes each by name instead of matching
// on message text.
var (
	// ErrLeaseModelMissing reports a lease built beside no model: a version in
	// effect is never invented.
	ErrLeaseModelMissing = errors.New("rl: the model lease was built beside no model")
	// ErrLeaseVersionMissing reports a replacement that names no version: an
	// unidentified model is never installed as the version in effect.
	ErrLeaseVersionMissing = errors.New("rl: the model replacement names no version")
	// ErrLeaseVersionHeld reports a replacement naming the version already in
	// effect: nothing is rebuilt and nothing is published.
	ErrLeaseVersionHeld = errors.New("rl: the model version is already the version in effect")
	// ErrLeaseRebuildMissing reports a replacement carrying no rebuild step: no
	// model is invented for it.
	ErrLeaseRebuildMissing = errors.New("rl: the model replacement carries no rebuild")
	// ErrLeaseRebuildFailed reports a rebuild that failed or yielded no model.
	// No publication runs and the version in effect is untouched.
	ErrLeaseRebuildFailed = errors.New("rl: the model rebuild failed and no binding was published")
	// ErrLeasePublishMissing reports a replacement carrying no publication
	// entry: a rebuilt model is never installed inside the lease alone.
	ErrLeasePublishMissing = errors.New("rl: the model replacement carries no publication entry")
	// ErrLeasePublishFailed reports a publication that refused the rebuilt
	// model. Nothing took effect and the version in effect is untouched.
	ErrLeasePublishFailed = errors.New("rl: the rebuilt model was not published and no binding took effect")
	// ErrLeaseVersionClosed reports a request that reached a version already
	// retired and closed. The refusal is named rather than served from a
	// released model.
	ErrLeaseVersionClosed = errors.New("rl: the model version was retired and closed")
	// ErrLeaseClosed reports a replacement offered to a closed lease: a closed
	// lease installs no further version.
	ErrLeaseClosed = errors.New("rl: the model lease is closed")
	// ErrLeaseHistoryInvalid reports a replacement record that cannot stand on
	// its own: no version to name the attempt by.
	ErrLeaseHistoryInvalid = errors.New("rl: the replacement record cannot stand on its own")
	// ErrLeaseStreamMissing reports a version that handed over no stream at all:
	// an absent stream is named rather than read as a completed one.
	ErrLeaseStreamMissing = errors.New("rl: the model version handed over no stream")
	// ErrLeaseRestoreMismatch reports a restored replacement history whose last
	// applied version is not the version the lease holds: a restarted lease is
	// never read across versions.
	ErrLeaseRestoreMismatch = errors.New("rl: the restored replacement history names a version the lease does not hold")
)

// ReplacementRecord is one replacement attempt as data: the version it started
// from, the version it asked for, whether a binding took effect and the named
// reason of a refusal.
//
// The record is an account and not a switch: a refused attempt leaves the
// version in effect as it was, so the history of a lease reads as the versions
// it actually served rather than the versions it was asked for.
type ReplacementRecord struct {
	// Schema is the record schema version the attempt was recorded under.
	Schema SchemaVersion `json:"schema"`
	// From is the identity in effect when the attempt began; it is empty for a
	// lease that held no version.
	From string `json:"from,omitempty"`
	// To is the identity the attempt asked for. A refused attempt names the
	// version asked for under the model name in effect, because no rebuilt
	// model stood to declare its own.
	To string `json:"to"`
	// State states whether a binding took effect.
	State ReplacementState `json:"state"`
	// Reason is the named refusal of an attempt that took no effect; it is
	// empty for an applied replacement.
	Reason string `json:"reason,omitempty"`
	// At is the time the attempt was recorded.
	At time.Time `json:"at"`
}

// leaseVersion is one version the lease holds: the identity it serves, the
// model instance behind it and the count of streams it is still serving.
type leaseVersion struct {
	identity ModelIdentity
	inner    model.Model
	inFlight int64
	retired  bool
	closed   bool
}

// ModelLease is the replacement lease of one model reference: the version in
// effect, every version still serving a stream and the ordered account of every
// replacement attempt.
//
// A model version is never swapped under a request that already started: the
// lease covers the whole returned stream, and the version in effect is what a
// request that begins afterwards borrows. A replacement is a rebuild, a
// publication and only then an installation, so a failed replacement is never
// mistaken for a version that took effect; and the lease never edits the records
// written under an earlier version, so a decision, attempt or reward of the
// retired version stays readable as the record of the version that served it.
type ModelLease struct {
	// replaceMu serializes replacements, so the history is one ordered account
	// and two attempts never install a version beside each other.
	replaceMu sync.Mutex

	mu       sync.Mutex
	versions []*leaseVersion
	current  *leaseVersion
	history  []ReplacementRecord
	closed   bool
	now      func() time.Time
}

// NewModelLease returns the lease of one model reference, holding initial as the
// version named by version. An empty version is the unversioned model of a
// deployment that names no generation, and it is recorded unversioned rather
// than assumed to be a current version.
func NewModelLease(initial model.Model, version string) (*ModelLease, error) {
	if initial == nil {
		return nil, ErrLeaseModelMissing
	}
	held := &leaseVersion{
		identity: ModelIdentity{Model: initial.Info().Name, Version: version},
		inner:    initial,
	}
	return &ModelLease{
		versions: []*leaseVersion{held},
		current:  held,
		now:      func() time.Time { return time.Now().UTC() },
	}, nil
}

// Current returns the model handle of the version in effect: the model a
// publication installs in a binding. The handle serves exactly one version, so
// the requests a generation borrows are the requests of that version.
func (l *ModelLease) Current() *VersionedModel {
	l.mu.Lock()
	defer l.mu.Unlock()
	return &VersionedModel{lease: l, version: l.current}
}

// CurrentVersion returns the identity name of the version in effect: the name a
// declaration references for it.
func (l *ModelLease) CurrentVersion() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current.identity.Name()
}

// InFlight reports how many model streams the lease is still serving across
// every version it holds.
func (l *ModelLease) InFlight() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var total int64
	for _, held := range l.versions {
		total += held.inFlight
	}
	return total
}

// Replacements returns the replacement attempts the lease recorded, in the
// order they were attempted.
func (l *ModelLease) Replacements() []ReplacementRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]ReplacementRecord(nil), l.history...)
}

// Replace rebuilds the model reference at one new version and installs it as the
// version in effect, in three steps that are refused as a whole: the rebuild
// produces the model, the publication entry accepts it, and only then does the
// lease make it the version a request that begins afterwards borrows.
//
// A rebuild that fails or yields no model records the attempt as refused beside
// ErrLeaseRebuildFailed and never reaches the publication entry, so a failed
// rebuild publishes no binding. A publication that refuses records the attempt
// as refused beside ErrLeasePublishFailed, releases the rebuilt model exactly
// once and leaves the version in effect untouched. An applied replacement
// retires the version it replaced: the retired version keeps serving the streams
// it already holds and is closed exactly once when the last of them ends.
func (l *ModelLease) Replace(ctx context.Context, version string, rebuild func() (model.Model, error), publish func(ModelIdentity, model.Model) error) (ReplacementRecord, error) {
	if version == "" {
		return ReplacementRecord{}, ErrLeaseVersionMissing
	}
	if err := ctx.Err(); err != nil {
		return ReplacementRecord{}, err
	}
	l.replaceMu.Lock()
	defer l.replaceMu.Unlock()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return ReplacementRecord{}, ErrLeaseClosed
	}
	from := l.current.identity.Name()
	heldVersion := l.current.identity.Version
	l.mu.Unlock()

	if version == heldVersion {
		return ReplacementRecord{}, fmt.Errorf("%w: %q", ErrLeaseVersionHeld, version)
	}
	requested := ModelIdentity{Model: ParseModelIdentity(from).Model, Version: version}.Name()
	if rebuild == nil {
		return l.refuse(from, requested, ErrLeaseRebuildMissing)
	}
	rebuilt, err := rebuild()
	if err != nil {
		return l.refuse(from, requested, fmt.Errorf("%w: %w", ErrLeaseRebuildFailed, err))
	}
	if rebuilt == nil {
		return l.refuse(from, requested, ErrLeaseRebuildFailed)
	}
	identity := ModelIdentity{Model: rebuilt.Info().Name, Version: version}
	if publish == nil {
		closeModel(rebuilt)
		return l.refuse(from, requested, ErrLeasePublishMissing)
	}
	next := &leaseVersion{identity: identity, inner: rebuilt}
	if err := publish(identity, &VersionedModel{lease: l, version: next}); err != nil {
		closeModel(rebuilt)
		return l.refuse(from, requested, fmt.Errorf("%w: %w", ErrLeasePublishFailed, err))
	}
	record := ReplacementRecord{
		Schema: SchemaCurrent,
		From:   from,
		To:     identity.Name(),
		State:  ReplacementApplied,
		At:     l.now(),
	}
	l.mu.Lock()
	previous := l.current
	l.versions = append(l.versions, next)
	l.current = next
	if previous != nil && previous != next {
		previous.retired = true
	}
	l.history = append(l.history, record)
	l.mu.Unlock()
	l.sweep()
	return record, nil
}

// Restore reads one persisted replacement history back into the lease and
// refuses a history whose last applied version is not the version the lease
// holds: a restarted lease attributes a request to the version persisted as the
// one in effect, and never strings a request across the version an earlier
// process served.
func (l *ModelLease) Restore(history []ReplacementRecord) error {
	var last string
	for i, record := range history {
		if record.To == "" {
			return fmt.Errorf("%w: record %d names no version", ErrLeaseHistoryInvalid, i)
		}
		if record.State == ReplacementApplied {
			last = record.To
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if last != "" && last != l.current.identity.Name() {
		return fmt.Errorf("%w: the history ends at %q and the lease holds %q", ErrLeaseRestoreMismatch, last, l.current.identity.Name())
	}
	for _, record := range history {
		if record.Schema == "" {
			record.Schema = SchemaCurrent
		}
		l.history = append(l.history, record)
	}
	return nil
}

// Close marks the lease closed and releases every retired version no stream
// still holds. The version in effect stays open: the binding that installed it
// still serves requests through it, and closing it here would pull the model
// from under those requests.
func (l *ModelLease) Close() error {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.sweep()
	return nil
}

// refuse records one attempt that took no effect and returns its named cause, so
// the refusal and the record of it reach the caller together.
func (l *ModelLease) refuse(from, to string, cause error) (ReplacementRecord, error) {
	record := ReplacementRecord{
		Schema: SchemaCurrent,
		From:   from,
		To:     to,
		State:  ReplacementRefused,
		Reason: cause.Error(),
		At:     l.now(),
	}
	l.mu.Lock()
	l.history = append(l.history, record)
	l.mu.Unlock()
	return record, cause
}

// enter takes one stream lease on a version. A version already retired and
// closed refuses by name rather than serving from a released model.
func (l *ModelLease) enter(held *leaseVersion) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if held.closed {
		return fmt.Errorf("%w: %q", ErrLeaseVersionClosed, held.identity.Name())
	}
	held.inFlight++
	return nil
}

// leave drops one stream lease and closes the version it served when that was
// the last stream of a retired version. The close runs outside the lock and the
// version is marked closed under it, so a version is closed exactly once.
func (l *ModelLease) leave(held *leaseVersion) {
	var closes bool
	l.mu.Lock()
	if held.inFlight > 0 {
		held.inFlight--
	}
	if held.inFlight == 0 && held.retired && held != l.current && !held.closed {
		held.closed = true
		l.versions = dropVersion(l.versions, held)
		closes = true
	}
	l.mu.Unlock()
	if closes {
		closeModel(held.inner)
	}
}

// sweep closes every retired version no stream still holds and that is not the
// version in effect. The decisions are taken under the lock and the closes run
// outside it, so a version is closed exactly once and a version reselected as
// the one in effect is never closed.
func (l *ModelLease) sweep() {
	var closing []model.Model
	l.mu.Lock()
	keep := l.versions[:0]
	for _, held := range l.versions {
		if held == l.current || !held.retired || held.inFlight != 0 || held.closed {
			keep = append(keep, held)
			continue
		}
		held.closed = true
		closing = append(closing, held.inner)
	}
	l.versions = keep
	l.mu.Unlock()
	for _, inner := range closing {
		closeModel(inner)
	}
}

// dropVersion returns the version set without one version, keeping the order of
// the others.
func dropVersion(versions []*leaseVersion, drop *leaseVersion) []*leaseVersion {
	kept := make([]*leaseVersion, 0, len(versions))
	for _, held := range versions {
		if held != drop {
			kept = append(kept, held)
		}
	}
	return kept
}

// closeModel closes one released model exactly once when it carries a close
// entry. A model that carries none is an immutable client and needs no release.
func closeModel(inner model.Model) {
	if closer, ok := inner.(io.Closer); ok {
		_ = closer.Close()
	}
}

// VersionedModel is the model handle of one version: the model a publication
// installs in a binding, serving exactly the version it was built for.
//
// The handle leases every call for the whole returned stream, so a version
// retired while the stream is open keeps serving it and is not closed until the
// stream ends. The handle declares the identity it serves on every response it
// hands over, which is what makes the served version readable in the attempt
// record without a second source of truth.
type VersionedModel struct {
	lease   *ModelLease
	version *leaseVersion
}

var (
	_ model.Model     = (*VersionedModel)(nil)
	_ model.IterModel = (*VersionedModel)(nil)
)

// Info reports the descriptor of the version this handle serves: the model name
// it was built for beside the context window of the pinned instance.
func (m *VersionedModel) Info() model.Info {
	inner := m.version.inner.Info()
	return model.Info{Name: m.version.identity.Model, ContextWindow: inner.ContextWindow}
}

// GenerateContent serves one request from the pinned version under a stream
// lease that covers the whole returned stream. A stream that fails to
// establish releases the lease at once; a stream whose caller stops reading
// keeps the lease, because a stream this handle cannot account as ended is
// never released under a possibly live producer.
func (m *VersionedModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	if err := m.lease.enter(m.version); err != nil {
		return nil, err
	}
	stream, err := m.version.inner.GenerateContent(ctx, request)
	if err != nil || stream == nil {
		m.lease.leave(m.version)
		return stream, err
	}
	out := make(chan *model.Response, cap(stream))
	go func() {
		defer close(out)
		defer m.lease.leave(m.version)
		released := false
		for {
			select {
			case response, ok := <-stream:
				if !ok {
					return
				}
				if released {
					continue
				}
				select {
				case out <- m.served(response):
				case <-ctx.Done():
					released = true
				}
			case <-ctx.Done():
				if released {
					for range stream {
					}
					return
				}
				released = true
			}
		}
	}()
	return out, nil
}

// GenerateContentIter serves one request from the pinned version in the caller
// goroutine. The sequence is lazy: building it takes no lease and touches no
// model, and the lease is taken when the caller drives it. An inner model that
// iterates itself is delegated to rather than bridged, so the iterator
// capability of the pinned instance is kept instead of hidden behind a channel.
// An entry refusal reaches the caller as the stream item the terminal reduction
// reads, so a refusal is never an empty sequence read as a completed one.
func (m *VersionedModel) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error) {
	return func(yield func(*model.Response) bool) {
		if err := m.lease.enter(m.version); err != nil {
			yield(refusedResponse(err))
			return
		}
		released := false
		defer func() {
			if !released {
				released = true
				m.lease.leave(m.version)
			}
		}()
		if iterator, ok := m.version.inner.(model.IterModel); ok {
			seq, err := iterator.GenerateContentIter(ctx, request)
			if err != nil {
				yield(refusedResponse(err))
				return
			}
			if seq == nil {
				yield(refusedResponse(ErrLeaseStreamMissing))
				return
			}
			seq(func(response *model.Response) bool {
				return yield(m.served(response))
			})
			return
		}
		stream, err := m.version.inner.GenerateContent(ctx, request)
		if err != nil || stream == nil {
			yield(refusedResponse(err))
			return
		}
		for {
			select {
			case response, ok := <-stream:
				if !ok {
					return
				}
				if !yield(m.served(response)) {
					go func() {
						for range stream {
						}
					}()
					return
				}
			case <-ctx.Done():
				for range stream {
				}
				return
			}
		}
	}, nil
}

// served declares the identity of the pinned version on one response and hands
// the caller its own copy, so the identity a response carries is the version
// that actually served it and never the version in effect at the time the
// response is read.
func (m *VersionedModel) served(response *model.Response) *model.Response {
	if response == nil {
		return nil
	}
	served := *response
	served.Model = m.version.identity.Name()
	return &served
}

// refusedResponse reports one entry refusal as the stream item the terminal
// reduction reads, so the refusal reaches the attempt record instead of a
// silently empty stream.
func refusedResponse(cause error) *model.Response {
	message := ErrLeaseStreamMissing.Error()
	if cause != nil {
		message = cause.Error()
	}
	return &model.Response{
		Done:  true,
		Error: &model.ResponseError{Message: message, Type: model.ErrorTypeStreamError},
	}
}
