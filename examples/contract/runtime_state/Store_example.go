package stateexample

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// errIncompleteAddress is reported for a site asked to be established without an explicit
// (agent, session) address.
var errIncompleteAddress = errors.New("stateexample: the site names an incomplete address")

// noticeID is the stable identity of one notification: task, round, kind and sequence.
type noticeID struct {
	taskID event.TaskID
	round  int64
	kind   task.NoticeKind
	seq    int64
}

// sharedFace is the canonical shared store face: it establishes the session sites, admits
// one prepared plan as the next effective generation, closes a session's own side and
// accepts the notifications a task intends to deliver.
type sharedFace struct {
	mu          sync.Mutex
	instance    event.InstanceID
	generation  uint64
	fingerprint string
	sites       map[event.Address]*state.Session
	closed      map[event.Address]struct{}
	deliveries  map[event.TaskID]task.HandoffTarget
	accepted    map[noticeID]state.WorkSeat
	nextAccept  state.AcceptSeq
}

// Establish creates the session site for one explicit address on the given runtime
// instance.
func (s *sharedFace) Establish(_ context.Context, addr event.Address, instance event.InstanceID) (*state.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !addr.IsComplete() {
		return nil, errIncompleteAddress
	}
	if s.sites == nil {
		s.sites = map[event.Address]*state.Session{}
	}
	site := &state.Session{Address: addr, Instance: instance}
	s.sites[addr] = site
	return site, nil
}

// Publish admits a fully prepared plan as the next effective binding generation, and
// discards a plan whose baseline was already advanced.
func (s *sharedFace) Publish(_ context.Context, plan state.StatePlan) (state.Publication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan.Plan.Fingerprint == s.fingerprint {
		return state.Publication{Generation: s.generation, Fingerprint: s.fingerprint, Baseline: plan.Plan.Baseline}, nil
	}
	s.generation++
	s.fingerprint = plan.Plan.Fingerprint
	return state.Publication{Generation: s.generation, Fingerprint: s.fingerprint, Baseline: plan.Plan.Baseline}, nil
}

// CloseSession closes the session's own side, refusing new input while the accepted
// obligations of the site are kept.
func (s *sharedFace) CloseSession(_ context.Context, addr event.Address) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sites[addr]; !ok {
		return errIncompleteAddress
	}
	if s.closed == nil {
		s.closed = map[event.Address]struct{}{}
	}
	s.closed[addr] = struct{}{}
	return nil
}

// AcceptNotice accepts one notification into the mailbox of the target session, idempotent
// by its stable identity.
func (s *sharedFace) AcceptNotice(ctx context.Context, notice task.Notice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acceptNotice(ctx, notice)
}

func (s *sharedFace) acceptNotice(_ context.Context, notice task.Notice) error {
	if notice.TaskID == "" {
		return state.ErrNoticeIdentityMissing
	}
	id := noticeID{taskID: notice.TaskID, round: notice.Round, kind: notice.Kind, seq: notice.Seq}
	if prior, ok := s.accepted[id]; ok {
		if prior.Notice != notice {
			return state.ErrNoticeConflict
		}
		return nil
	}
	route, ok := s.deliveries[notice.TaskID]
	if !ok {
		return state.ErrDeliveryNotAllowed
	}
	site, ok := s.sites[route.Target]
	if !ok {
		return state.ErrUnknownTarget
	}
	if site.Instance != route.TargetInstance {
		return state.ErrInstanceRetired
	}
	if _, closed := s.closed[route.Target]; closed {
		return fmt.Errorf("%w: %w", state.ErrDeliveryRefused, state.ErrSessionClosed)
	}
	if s.accepted == nil {
		s.accepted = map[noticeID]state.WorkSeat{}
	}
	s.nextAccept++
	s.accepted[id] = state.WorkSeat{
		Address:   route.Target,
		Instance:  route.TargetInstance,
		AcceptSeq: s.nextAccept,
		Notice:    notice,
		Claim:     state.ClaimPending,
	}
	return nil
}

// AcceptNoticeClaimed accepts and claims one notification in the same call.
func (s *sharedFace) AcceptNoticeClaimed(ctx context.Context, notice task.Notice) (state.WorkSeat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.acceptNotice(ctx, notice); err != nil {
		return state.WorkSeat{}, err
	}
	id := noticeID{taskID: notice.TaskID, round: notice.Round, kind: notice.Kind, seq: notice.Seq}
	seat := s.accepted[id]
	if seat.Claim != state.ClaimPending {
		return state.WorkSeat{}, state.ErrClaimConflict
	}
	seat.Claim = state.ClaimClaimed
	s.accepted[id] = seat
	return seat, nil
}

var _ state.Store = (*sharedFace)(nil)

// ExampleStore establishes one site, publishes two generations of which the repeated content
// yields no new one, accepts the same notification twice as one fact, claims it once and closes the site.
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
func ExampleStore() {
	ctx := context.Background()
	store := &sharedFace{
		instance:   "instance-1",
		deliveries: map[event.TaskID]task.HandoffTarget{},
	}
	addr := event.Address{AgentID: "assistant", SessionID: "session-1"}
	store.deliveries["task-1"] = task.HandoffTarget{Target: addr, TargetInstance: "instance-1"}

	site, err := store.Establish(ctx, addr, "instance-1")
	if err != nil || site.Instance != "instance-1" {
		return
	}
	if _, err := store.Establish(ctx, event.Address{AgentID: "assistant"}, "instance-1"); !errors.Is(err, errIncompleteAddress) {
		return
	}
	first, err := store.Publish(ctx, state.StatePlan{Plan: state.OrgPlan{Baseline: "baseline-1", Fingerprint: "fingerprint-1"}})
	if err != nil || first.Generation != 1 {
		return
	}
	second, err := store.Publish(ctx, state.StatePlan{Plan: state.OrgPlan{Baseline: "baseline-1", Fingerprint: "fingerprint-1"}})
	if err != nil || second.Generation != 1 {
		return
	}
	notice := task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 1}
	if err := store.AcceptNotice(ctx, notice); err != nil {
		return
	}
	if err := store.AcceptNotice(ctx, notice); err != nil || len(store.accepted) != 1 {
		return
	}
	seat, err := store.AcceptNoticeClaimed(ctx, notice)
	if err != nil || seat.Claim != state.ClaimClaimed {
		return
	}
	if _, err := store.AcceptNoticeClaimed(ctx, notice); !errors.Is(err, state.ErrClaimConflict) {
		return
	}
	if err := store.CloseSession(ctx, addr); err != nil {
		return
	}
}
