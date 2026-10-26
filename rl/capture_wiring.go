package rl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// ErrCaptureUnscoped reports a capture the trajectory cannot scope to one
// request view: a capture that cannot name the view of the decision it serves
// cannot fix a snapshot for it.
var ErrCaptureUnscoped = errors.New("rl: the capture cannot be scoped to one request view")

// ErrCaptureViewMissing reports a snapshot asked of the capture itself rather
// than of the scope that carries the view: a request is fixed against the view
// of its own decision, and a viewless snapshot is refused by name instead of
// fixed against an invented view.
var ErrCaptureViewMissing = errors.New("rl: the capture is not scoped to one request view")

// ErrCaptureSnapshotMissing reports a snapshot reference the capture holds no
// snapshot behind: a reference is read back only when its content is held, so
// a missing snapshot is named rather than answered empty.
var ErrCaptureSnapshotMissing = errors.New("rl: the capture holds no snapshot behind the reference")

// ErrCaptureSinkMissing reports a trajectory capture built beside no sink: the
// bounded admission queue is the one write point of a capture, and a capture
// beside none admits nothing.
var ErrCaptureSinkMissing = errors.New("rl: the trajectory capture was built beside no sink")

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// CaptureScope scopes one capture to the request view of one decision: the
// scope carries the view the request was declared against, fixes the snapshot
// of that request and hands back the reference the decision records, while
// records and the seal stay the capture's own.
//
// The scope is what keeps the frozen Capture signature honest: Snapshot is
// handed the request alone, so the view the request runs on is supplied by the
// boundary that declares it and never reconstructed inside the capture.
// The redline list (2 rules):
// MUST scope one capture per decision view, never share a scope across decisions.
// MUST NOT reconstruct a view inside the capture: the boundary declares it.
type CaptureScope interface {
	Capture
	// Scope returns the capture of one decision's request view.
	Scope(view ModelViewRef, decisionID event.DecisionID) Capture
}

// TrajectoryCapture is the run-level capture of one trajectory: it keeps the
// safe snapshots it fixed, admits every attempt it observes through one
// bounded sink and seals that sink at the end of the turn it served.
//
// The capture itself is not a request scope: a snapshot is fixed through the
// scope of the decision whose request it is, so a view is never invented here.
// Records and the seal are shared by every scope of the capture, because a
// trajectory is one account however many decisions it spans.
//
//   - A request view is fixed before the request starts and is never rebuilt from the options of a finished call.
//   - An attempt is admitted through the bounded sink and never waits on the writer: a refused record is a named loss, not a blocked request.
//   - A capture seals at the end of the turn it observed and reports what the writer actually confirmed.
type TrajectoryCapture struct {
	id     string
	sink   *CaptureSink
	policy SnapshotPolicy

	mu        sync.Mutex
	seq       uint64
	refs      []SnapshotRef
	snapshots map[string]RequestSnapshot
}

var _ CaptureScope = (*TrajectoryCapture)(nil)

// NewTrajectoryCapture returns the capture of one trajectory over its bounded
// sink, under the projection policy its snapshots are taken with. A missing
// capture identity or sink is refused by name: a capture seals the account of
// the identity it belongs to and drains into exactly one write point.
func NewTrajectoryCapture(id string, sink *CaptureSink, policy SnapshotPolicy) (*TrajectoryCapture, error) {
	if id == "" {
		return nil, ErrCaptureIDMissing
	}
	if sink == nil {
		return nil, ErrCaptureSinkMissing
	}
	return &TrajectoryCapture{
		id:        id,
		sink:      sink,
		policy:    policy,
		snapshots: map[string]RequestSnapshot{},
	}, nil
}

// CaptureID reports the identity of the capture this trajectory seals under.
func (c *TrajectoryCapture) CaptureID() string {
	if c == nil {
		return ""
	}
	return c.id
}

// Scope returns the capture of one decision's request view: the view the
// request was declared against beside the decision whose request it is.
func (c *TrajectoryCapture) Scope(view ModelViewRef, decisionID event.DecisionID) Capture {
	return &scopedCapture{owner: c, view: view, decisionID: decisionID}
}

// Snapshot refuses a snapshot asked of the capture itself: a view is fixed by
// the scope of the decision that carries it.
func (c *TrajectoryCapture) Snapshot(context.Context, model.Request) (ModelViewRef, error) {
	return ModelViewRef{}, ErrCaptureViewMissing
}

// Record admits one attempt through the capture's bounded sink: admission
// never waits on the writer, and a record a bound refuses is reported with that
// bound rather than dropped in silence.
func (c *TrajectoryCapture) Record(ctx context.Context, attempt AttemptRecord) (CaptureAdmission, error) {
	if c == nil || c.sink == nil {
		return CaptureAdmission{}, ErrCaptureSinkMissing
	}
	return c.sink.Record(ctx, attempt)
}

// Seal closes the capture through its sink and reports the manifest the writer
// confirmed. A second seal returns the first manifest, so the seal of a turn
// that was already sealed is never a second account.
func (c *TrajectoryCapture) Seal(ctx context.Context) (CaptureManifest, error) {
	if c == nil || c.sink == nil {
		return CaptureManifest{}, ErrCaptureSinkMissing
	}
	return c.sink.Seal(ctx)
}

// Ledger reports the loss account of the capture's sink.
func (c *TrajectoryCapture) Ledger() CaptureLedger {
	if c == nil || c.sink == nil {
		return CaptureLedger{}
	}
	return c.sink.Ledger()
}

// keep records one fixed snapshot beside the reference it is read back under.
// The snapshot is kept once per reference: the same decision fixing a second
// snapshot keeps both, each under its own identity.
func (c *TrajectoryCapture) keep(ref SnapshotRef, snapshot RequestSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.refs = append(c.refs, ref)
	c.snapshots[ref.SnapshotID] = snapshot
}

// nextSnapshotID mints the identity of one request snapshot: the decision it
// was fixed for beside the ordinal of the fix, so two snapshots of one decision
// never share an identity.
func (c *TrajectoryCapture) nextSnapshotID(decisionID event.DecisionID) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fmt.Sprintf("snapshot-%s-%d", decisionID, c.seq+1)
}

// SnapshotRefs reports the references of the snapshots this capture fixed, in
// fix order, as a copy the caller owns.
func (c *TrajectoryCapture) SnapshotRefs() []SnapshotRef {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]SnapshotRef(nil), c.refs...)
}

// SnapshotOf reads one fixed snapshot back through its reference: the content
// is rechecked against the reference digest, so a snapshot that does not match
// is refused as a mismatch rather than answered as the referenced one.
func (c *TrajectoryCapture) SnapshotOf(ref SnapshotRef) (RequestSnapshot, error) {
	if c == nil {
		return RequestSnapshot{}, ErrCaptureSnapshotMissing
	}
	c.mu.Lock()
	snapshot, held := c.snapshots[ref.SnapshotID]
	c.mu.Unlock()
	if !held {
		return RequestSnapshot{}, fmt.Errorf("%w: %s", ErrCaptureSnapshotMissing, ref.SnapshotID)
	}
	content, err := MarshalSnapshot(snapshot)
	if err != nil {
		return RequestSnapshot{}, err
	}
	if err := VerifySnapshot(ref, content); err != nil {
		return RequestSnapshot{}, err
	}
	return snapshot, nil
}

// scopedCapture is the capture of one decision's request view: it fixes the
// safe snapshot of that request against the view it was declared with and
// hands records and the seal to the trajectory that owns them.
type scopedCapture struct {
	owner      *TrajectoryCapture
	view       ModelViewRef
	decisionID event.DecisionID
}

var _ Capture = (*scopedCapture)(nil)

// Snapshot fixes the safe snapshot of req as it stands before the request
// starts, under the capture's projection policy, and returns the view reference
// the decision records: the reference names the kept snapshot, its digest and
// the read entry it is read back through, and the parameters digest names the
// generation configuration the request actually carried.
//
// The request is observed and never edited, and a view is never rebuilt from
// the options of a finished call.
func (s *scopedCapture) Snapshot(ctx context.Context, req model.Request) (ModelViewRef, error) {
	if err := ctx.Err(); err != nil {
		return ModelViewRef{}, err
	}
	snapshot, err := SnapshotRequest(s.view, req, s.owner.policy)
	if err != nil {
		return ModelViewRef{}, err
	}
	ref, err := snapshot.Ref(s.owner.nextSnapshotID(s.decisionID), "capture/"+s.owner.id+"/"+string(s.decisionID))
	if err != nil {
		return ModelViewRef{}, err
	}
	s.owner.keep(ref, snapshot)
	view := s.view
	view.Snapshot = ref
	view.ParametersDigest = digestBytes(snapshot.Parameters)
	return view, nil
}

// Record admits one attempt through the trajectory's bounded sink.
func (s *scopedCapture) Record(ctx context.Context, attempt AttemptRecord) (CaptureAdmission, error) {
	return s.owner.Record(ctx, attempt)
}

// Seal closes the trajectory through its bounded sink.
func (s *scopedCapture) Seal(ctx context.Context) (CaptureManifest, error) {
	return s.owner.Seal(ctx)
}

// CaptureAdapter adapts one scoped capture onto the execution loop's narrow
// capture face: the loop hands the scattered observations of a decision — the
// request view material, the stored input events and the reduced outcome — and
// the adapter assembles them into the fixed view, the scoped snapshot and the
// attempt record the capture takes.
//
// The adapter crosses built-in, event and model types only, so the capture side
// attaches through the loop's face with no package edge into the execution
// core. A record the capture refuses is returned as a named refusal, which the
// loop counts as a recording loss and never carries into the model decision.
type CaptureAdapter struct {
	scope CaptureScope

	mu     sync.Mutex
	scoped map[event.DecisionID]Capture
	order  []event.DecisionID
}

var _ interface {
	Snapshot(context.Context, event.DecisionID, string, string, []string, model.Request) error
	Record(context.Context, event.AttemptID, event.DecisionID, event.EpisodeID, model.Outcome) error
	Seal(context.Context) error
} = (*CaptureAdapter)(nil)

// NewCaptureAdapter wraps one scoped capture in the face the execution loop
// attaches. A capture that cannot be scoped is refused by name: the loop hands
// a request's view material per decision, and a capture beside no scope would
// have to invent the view it fixes.
func NewCaptureAdapter(scope CaptureScope) (*CaptureAdapter, error) {
	if scope == nil {
		return nil, ErrCaptureUnscoped
	}
	return &CaptureAdapter{scope: scope, scoped: map[event.DecisionID]Capture{}}, nil
}

// Snapshot fixes the view of one decision's request before the request starts
// and keeps the scope that carries it, so the attempt of the same decision
// records against the very view its request was fixed on.
func (a *CaptureAdapter) Snapshot(ctx context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools []string, req model.Request) error {
	view := ModelViewRef{
		Model:         modelName,
		PromptVersion: promptVersion,
		VisibleTools:  unversionedMembers(visibleTools),
	}
	scoped := a.scope.Scope(view, decisionID)
	if _, err := scoped.Snapshot(ctx, req); err != nil {
		return err
	}
	a.mu.Lock()
	if _, held := a.scoped[decisionID]; !held {
		a.order = append(a.order, decisionID)
	}
	a.scoped[decisionID] = scoped
	a.mu.Unlock()
	return nil
}

// Record admits the attempt of one decision through the capture: the record is
// assembled through the single observation face AttemptOf from the outcome the
// model reduction produced, so the terminal it names is that reduction's own.
func (a *CaptureAdapter) Record(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error {
	scoped := a.scopeOf(decisionID)
	admission, err := scoped.Record(ctx, AttemptOf(attemptID, decisionID, episodeID, outcome))
	if err != nil {
		return err
	}
	if !admission.Admitted {
		return fmt.Errorf("%w: bound=%s: %s", ErrCaptureRefused, admission.Bound, admission.Reason)
	}
	return nil
}

// Seal closes the capture at the end of the turn it observed. The wait is
// bounded: a writer that does not confirm in time leaves the sink's explicit
// partial account, never a completed seal invented here.
func (a *CaptureAdapter) Seal(ctx context.Context) error {
	sealing, cancel := context.WithTimeout(ctx, captureSealTimeout)
	defer cancel()
	_, err := a.scope.Seal(sealing)
	return err
}

// scopeOf reports the scope of one decision, or the capture itself when that
// decision fixed no view: an attempt whose snapshot was refused is still
// recorded against the trajectory rather than dropped.
func (a *CaptureAdapter) scopeOf(decisionID event.DecisionID) Capture {
	a.mu.Lock()
	defer a.mu.Unlock()
	if scoped, held := a.scoped[decisionID]; held {
		return scoped
	}
	return a.scope
}

// unversionedMembers records visible names as unversioned view members: the
// loop names each visible tool it runs with and never invents a version beside
// a member that carries none.
func unversionedMembers(names []string) []VersionedName {
	out := make([]VersionedName, 0, len(names))
	for _, name := range names {
		out = append(out, VersionedName{Name: name})
	}
	return out
}

// digestBytes reports the hex SHA-256 of one parameter body, empty for a body
// that carries no bytes: a digest never stands in for what it cannot name.
func digestBytes(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// captureSealTimeout bounds the seal one turn waits for: a writer that does not
// confirm in time is reported as the explicit partial state of the sink, never
// as a completed seal.
const captureSealTimeout = 5 * time.Second
