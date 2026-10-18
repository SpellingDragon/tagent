package stateexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
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
	instance    event.InstanceID
	generation  uint64
	fingerprint string
	sites       map[event.Address]*state.Session
	closed      map[event.Address]struct{}
	accepted    map[noticeID]struct{}
}

// Establish creates the session site for one explicit address on the given runtime
// instance.
func (s *sharedFace) Establish(_ context.Context, addr event.Address, instance event.InstanceID) (*state.Session, error) {
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
func (s *sharedFace) AcceptNotice(_ context.Context, notice task.Notice) error {
	id := noticeID{taskID: notice.TaskID, round: notice.Round, kind: notice.Kind, seq: notice.Seq}
	if s.accepted == nil {
		s.accepted = map[noticeID]struct{}{}
	}
	if _, ok := s.accepted[id]; ok {
		return nil
	}
	s.accepted[id] = struct{}{}
	return nil
}

var _ state.Store = (*sharedFace)(nil)

// ExampleStore establishes one site, publishes two generations of which the repeated content
// yields no new one, closes the site and accepts the same notification twice as one fact.
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
func ExampleStore() {
	ctx := context.Background()
	store := &sharedFace{instance: "instance-1"}
	addr := event.Address{AgentID: "assistant", SessionID: "session-1"}

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
	if err := store.CloseSession(ctx, addr); err != nil {
		return
	}
	notice := task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 1}
	if err := store.AcceptNotice(ctx, notice); err != nil {
		return
	}
	if err := store.AcceptNotice(ctx, notice); err != nil || len(store.accepted) != 1 {
		return
	}
}
