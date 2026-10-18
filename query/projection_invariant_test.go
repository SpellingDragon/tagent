package query

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// invariantReadLog records every identity the projection materialized, in order.
type invariantReadLog struct {
	mu  sync.Mutex
	ids []refID
}

// record appends one materialized identity.
func (l *invariantReadLog) record(id refID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ids = append(l.ids, id)
}

// snapshot copies the identities recorded so far.
func (l *invariantReadLog) snapshot() []refID {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]refID(nil), l.ids...)
}

// has reports whether one identity was materialized at all.
func (l *invariantReadLog) has(id refID) bool {
	for _, seen := range l.snapshot() {
		if seen == id {
			return true
		}
	}
	return false
}

// invariantFrozen is one frozen reading serving a copy of the identity order it froze.
type invariantFrozen struct {
	order   []ObjectRef
	details map[refID]Detail
	reads   *invariantReadLog
}

// Order answers a copy of the frozen identity order.
func (f invariantFrozen) Order() []ObjectRef { return append([]ObjectRef(nil), f.order...) }

// DetailOf materializes one frozen identity and records that it was read.
func (f invariantFrozen) DetailOf(ref ObjectRef) (Detail, bool) {
	f.reads.record(refIDOf(ref))
	d, ok := f.details[refIDOf(ref)]
	return d, ok
}

// invariantSource is a reading source whose owners can be advanced between freezes.
type invariantSource struct {
	mu         sync.Mutex
	order      []ObjectRef
	details    map[refID]Detail
	watermarks []SourceWatermark
	reads      invariantReadLog
	freezes    int
}

// Freeze copies the current owner content into one frozen reading.
func (s *invariantSource) Freeze(context.Context) (DetailSource, []SourceWatermark, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.freezes++
	details := make(map[refID]Detail, len(s.details))
	for id, d := range s.details {
		details[id] = d
	}
	frozen := invariantFrozen{
		order:   append([]ObjectRef(nil), s.order...),
		details: details,
		reads:   &s.reads,
	}
	return frozen, append([]SourceWatermark(nil), s.watermarks...), nil
}

// put installs one detail entry beside its identity and appends its reference once.
func (s *invariantSource) put(d Detail) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.details == nil {
		s.details = map[refID]Detail{}
	}
	id := refIDOf(d.Ref)
	if _, seen := s.details[id]; !seen {
		s.order = append(s.order, d.Ref)
	}
	s.details[id] = d
}

// advance rewrites one already held entry without moving its identity.
func (s *invariantSource) advance(d Detail) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.details[refIDOf(d.Ref)] = d
}

// materialized reports the identities one projection read materialized.
func (s *invariantSource) materialized() []refID { return s.reads.snapshot() }

// invariantAgentDetail builds one agent entry reading its three agent readings.
func invariantAgentDetail(id event.AgentID, configured, resident, busy bool) Detail {
	return Detail{
		Ref:   ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: id}},
		State: DetailState{Agent: AgentState{Configured: configured, Resident: resident, Busy: busy}},
	}
}

// invariantSessionDetail builds one session entry reading its mailbox.
func invariantSessionDetail(agent event.AgentID, session event.SessionID, instance event.InstanceID) Detail {
	return Detail{
		Ref:   ObjectRef{Kind: ObjectSession, Address: event.Address{AgentID: agent, SessionID: session}, InstanceID: instance},
		State: DetailState{Session: SessionState{Accepted: 2, Claimed: 1, Completed: 1}},
	}
}

// invariantTaskDetail builds one task entry carrying the native task status.
func invariantTaskDetail(id event.TaskID) Detail {
	return Detail{
		Ref:   ObjectRef{Kind: ObjectTask, TaskID: id},
		State: DetailState{TaskStatus: task.StatusAccepted},
	}
}

// invariantNoticeDetail builds one notice entry carrying its delivery phase.
func invariantNoticeDetail(id event.TaskID, seq int64) Detail {
	return Detail{
		Ref:   ObjectRef{Kind: ObjectNotice, Notice: task.Notice{TaskID: id, Round: 1, Kind: "progress", Seq: seq}},
		State: DetailState{NoticePhase: NoticePhaseOwed},
	}
}

// invariantLeaseDetail builds one addressless lease entry.
func invariantLeaseDetail(kind, key string) Detail {
	lease := state.LeaseRef{Kind: kind, Key: key, Ref: "release-" + key}
	return Detail{Ref: ObjectRef{Kind: ObjectLease, Lease: lease}, State: DetailState{Holder: lease}}
}

// invariantFilter builds one filter over the given authorization agents and kinds.
func invariantFilter(agents []event.AgentID, kinds ...ObjectKind) ListFilter {
	return ListFilter{Scope: AuthorizationScope{Agents: agents}, Kinds: kinds}
}

// TestProjectionRevisionInvariants pins the revision and cursor identity of one read service.
// - every snapshot mints a revision strictly after the one before it and above zero
// - a revision that has left the bounded window is refused as expired instead of served
// - a cursor replayed at another revision, scope or filter is refused by name
// - a reordered authorization scope or filter is the same read and never a refusal
// - the watermarks of one reading stay per source and a returned list never rewrites the frozen one
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestProjectionRevisionInvariants(t *testing.T) {
	ctx := context.Background()
	agent := event.AgentID("agent-a")

	t.Run("snapshot revisions strictly increase from one", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(agent, true, false, false))
		projection := NewBoundedProjection(src, 3, Budget{})

		var revisions []Revision
		for range 3 {
			snap, _, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{agent}))
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			revisions = append(revisions, snap.Revision)
		}
		if revisions[0] != 1 {
			t.Fatalf("first revision = %d, want 1", revisions[0])
		}
		for i := 1; i < len(revisions); i++ {
			if revisions[i] <= revisions[i-1] {
				t.Fatalf("revisions = %v, want one strictly increasing sequence", revisions)
			}
		}
		if src.freezes != 3 {
			t.Fatalf("freezes = %d, want one freeze per snapshot", src.freezes)
		}
	})

	t.Run("a revision outside the window is refused as expired", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(agent, true, false, false))
		projection := NewBoundedProjection(src, 2, Budget{})
		filter := invariantFilter([]event.AgentID{agent})

		first, firstCursor, err := projection.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("first snapshot: %v", err)
		}
		if _, _, err := projection.Snapshot(ctx, filter); err != nil {
			t.Fatalf("second snapshot: %v", err)
		}
		if _, _, err := projection.Snapshot(ctx, filter); err != nil {
			t.Fatalf("third snapshot: %v", err)
		}
		if _, err := projection.List(first.Revision, firstCursor, filter, ObjectRef{}, 1); !errors.Is(err, ErrCursorWindowExpired) {
			t.Fatalf("list of the evicted revision = %v, want %v", err, ErrCursorWindowExpired)
		}
		if _, err := projection.Detail(first.Revision, firstCursor, filter, ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: agent}}); !errors.Is(err, ErrCursorWindowExpired) {
			t.Fatalf("detail of the evicted revision = %v, want %v", err, ErrCursorWindowExpired)
		}
		if _, err := projection.List(Revision(0), Cursor{}, filter, ObjectRef{}, 1); !errors.Is(err, ErrCursorWindowExpired) {
			t.Fatalf("list of the zero revision = %v, want %v", err, ErrCursorWindowExpired)
		}
	})

	t.Run("a cursor is bound to its revision, scope and filter", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(agent, true, false, false))
		projection := NewBoundedProjection(src, 4, Budget{})
		minted := invariantFilter([]event.AgentID{agent}, ObjectAgent)

		at, cursor, err := projection.Snapshot(ctx, minted)
		if err != nil {
			t.Fatalf("mint snapshot: %v", err)
		}
		if _, _, err := projection.Snapshot(ctx, minted); err != nil {
			t.Fatalf("advance the revision: %v", err)
		}
		if _, err := projection.List(at.Revision+1, cursor, minted, ObjectRef{}, 1); !errors.Is(err, ErrCursorRevisionMismatch) {
			t.Fatalf("replayed cursor = %v, want %v", err, ErrCursorRevisionMismatch)
		}
		widened := invariantFilter([]event.AgentID{agent, "agent-b"}, ObjectAgent)
		if _, err := projection.List(at.Revision, cursor, widened, ObjectRef{}, 1); !errors.Is(err, ErrCursorScopeChanged) {
			t.Fatalf("widened scope = %v, want %v", err, ErrCursorScopeChanged)
		}
		if _, err := projection.List(at.Revision, cursor, invariantFilter([]event.AgentID{agent}, ObjectTask), ObjectRef{}, 1); !errors.Is(err, ErrCursorFilterChanged) {
			t.Fatalf("changed filter = %v, want %v", err, ErrCursorFilterChanged)
		}
	})

	t.Run("a reordered scope or filter is the same read", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(agent, true, false, false))
		projection := NewBoundedProjection(src, 2, Budget{})
		minted := ListFilter{
			Scope: AuthorizationScope{Agents: []event.AgentID{"agent-b", agent}},
			Kinds: []ObjectKind{ObjectTask, ObjectAgent},
		}
		at, cursor, err := projection.Snapshot(ctx, minted)
		if err != nil {
			t.Fatalf("mint snapshot: %v", err)
		}
		reordered := ListFilter{
			Scope: AuthorizationScope{Agents: []event.AgentID{agent, "agent-b"}},
			Kinds: []ObjectKind{ObjectAgent, ObjectTask},
		}
		if _, err := projection.List(at.Revision, cursor, reordered, ObjectRef{}, 1); err != nil {
			t.Fatalf("reordered read = %v, want the same binding accepted", err)
		}
	})

	t.Run("watermarks stay per source and returned lists never rewrite the freeze", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(agent, true, false, false))
		src.watermarks = []SourceWatermark{
			{Source: "state", Position: "7", LagState: WatermarkLagKnown, Lag: SourceLag{Amount: 2, Unit: "revisions", Origin: "state-owner"}},
			{Source: "task", Position: "12", LagState: WatermarkLagUnknown},
		}
		projection := NewBoundedProjection(src, 2, Budget{})
		first, _, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{agent}))
		if err != nil {
			t.Fatalf("first snapshot: %v", err)
		}
		if len(first.Watermarks) != 2 {
			t.Fatalf("watermarks = %+v, want one per source without a merged global position", first.Watermarks)
		}
		if first.Watermarks[0].LagState != WatermarkLagKnown || first.Watermarks[1].LagState != WatermarkLagUnknown {
			t.Fatalf("watermark states = (%q, %q), want the known and unknown readings kept apart",
				first.Watermarks[0].LagState, first.Watermarks[1].LagState)
		}
		first.Watermarks[0].Source = "rewritten"
		first.Watermarks[0].Lag.Amount = 99
		second, _, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{agent}))
		if err != nil {
			t.Fatalf("second snapshot: %v", err)
		}
		if second.Watermarks[0].Source != "state" || second.Watermarks[0].Lag.Amount != 2 {
			t.Fatalf("watermarks after a caller mutation = %+v, want the frozen reading untouched", second.Watermarks[0])
		}
	})
}

// TestProjectionScopeInvariants pins that authorization narrows before anything is read or counted.
// - an object outside the authorization scope is never materialized and never counted as omitted
// - an empty scope covers nothing while a lease needs only some authorized reader
// - a detail outside the scope is refused as outside, never disguised as absent
// - a detail inside the scope but outside the filter is refused as absent
// - a frozen reading keeps answering its own content after the owners advance
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestProjectionScopeInvariants(t *testing.T) {
	ctx := context.Background()
	allowed := event.AgentID("agent-a")
	denied := event.AgentID("agent-b")

	t.Run("unauthorized objects are never materialized nor counted", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(allowed, true, true, false))
		src.put(invariantAgentDetail(denied, true, true, true))
		src.put(invariantSessionDetail(allowed, "s1", "instance-1"))
		src.put(invariantSessionDetail(denied, "s2", "instance-1"))
		src.put(invariantTaskDetail("task-1"))
		src.put(invariantLeaseDetail("model", "native"))

		projection := NewBoundedProjection(src, 2, Budget{})
		snap, _, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{allowed}))
		if err != nil {
			t.Fatalf("scoped snapshot: %v", err)
		}
		for _, detail := range snap.Details {
			if detail.Ref.Address.AgentID == denied {
				t.Fatalf("detail = %+v, want no object of an unauthorized agent", detail.Ref)
			}
			if detail.Ref.Kind == ObjectTask {
				t.Fatalf("detail = %+v, want no addressless object under an agent-level scope", detail.Ref)
			}
		}
		if snap.Truncated.OmittedObjects != 0 {
			t.Fatalf("omitted objects = %d, want unauthorized objects never counted as budget omissions",
				snap.Truncated.OmittedObjects)
		}
		if src.reads.has(refIDOf(ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: denied}})) {
			t.Fatalf("unauthorized agent materialized, want the scope applied before any read")
		}
		if src.reads.has(refIDOf(ObjectRef{Kind: ObjectTask, TaskID: "task-1"})) {
			t.Fatalf("out-of-scope task materialized, want the scope applied before any read")
		}
		if !src.reads.has(refIDOf(ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: allowed}})) {
			t.Fatalf("authorized agent not materialized, want the in-scope objects read")
		}
	})

	t.Run("an empty scope covers nothing while a lease needs some authorized reader", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(allowed, true, false, false))
		src.put(invariantLeaseDetail("gpu", "0"))
		projection := NewBoundedProjection(src, 2, Budget{})

		empty, _, err := projection.Snapshot(ctx, ListFilter{})
		if err != nil {
			t.Fatalf("empty-scope snapshot: %v", err)
		}
		if len(empty.Details) != 0 {
			t.Fatalf("details = %+v, want an empty scope to read nothing", empty.Details)
		}
		if len(src.materialized()) != 0 {
			t.Fatalf("materialized = %+v, want nothing read under an empty scope", src.materialized())
		}

		scoped, _, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{allowed}))
		if err != nil {
			t.Fatalf("scoped snapshot: %v", err)
		}
		var leases int
		for _, detail := range scoped.Details {
			if detail.Ref.Kind == ObjectLease {
				leases++
			}
		}
		if leases != 1 {
			t.Fatalf("leases = %d, want the addressless lease visible to one authorized reader", leases)
		}
	})

	t.Run("a detail outside the scope is refused as outside", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(allowed, true, false, false))
		src.put(invariantAgentDetail(denied, true, false, false))
		projection := NewBoundedProjection(src, 2, Budget{})
		filter := invariantFilter([]event.AgentID{allowed})

		at, cursor, err := projection.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		_, err = projection.Detail(at.Revision, cursor, filter, ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: denied}})
		if !errors.Is(err, ErrDetailOutsideScope) {
			t.Fatalf("detail of an unauthorized object = %v, want %v", err, ErrDetailOutsideScope)
		}
		if errors.Is(err, ErrDetailAbsent) {
			t.Fatalf("detail refusal = %v, want the authorization boundary told apart from a missing object", err)
		}

		_, err = projection.Detail(at.Revision, cursor, invariantFilter([]event.AgentID{allowed}, ObjectTask), ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: allowed}})
		if !errors.Is(err, ErrCursorFilterChanged) {
			t.Fatalf("detail under another filter = %v, want %v", err, ErrCursorFilterChanged)
		}

		narrowedAt, narrowedCursor, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{allowed}, ObjectTask))
		if err != nil {
			t.Fatalf("narrowed snapshot: %v", err)
		}
		_, err = projection.Detail(narrowedAt.Revision, narrowedCursor, invariantFilter([]event.AgentID{allowed}, ObjectTask), ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: allowed}})
		if !errors.Is(err, ErrDetailAbsent) {
			t.Fatalf("detail outside the filter = %v, want %v", err, ErrDetailAbsent)
		}
	})

	t.Run("a frozen reading keeps its content after the owners advance", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(allowed, true, false, false))
		projection := NewBoundedProjection(src, 2, Budget{})
		filter := invariantFilter([]event.AgentID{allowed})

		at, cursor, err := projection.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		src.advance(invariantAgentDetail(allowed, false, true, true))
		detail, err := projection.Detail(at.Revision, cursor, filter, ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: allowed}})
		if err != nil {
			t.Fatalf("detail of the frozen revision: %v", err)
		}
		if !detail.State.Agent.Configured || detail.State.Agent.Resident || detail.State.Agent.Busy {
			t.Fatalf("agent reading = %+v, want the frozen reading instead of the advanced owner", detail.State.Agent)
		}
	})
}

// TestProjectionOrderAndBudgetInvariants pins the identity order and the honest budget boundary.
// - kinds order agent, session, invocation, task, notice, lease whatever order they are held in
// - content beside an identity never splits one object into two identities
// - the pages of one read concatenate into exactly its snapshot details
// - an object budget holds its bound and counts every identity it left out
// - a byte boundary stops at the entry that would overflow and counts it beside the later ones
// - a page larger than the object budget is refused instead of silently shortened
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestProjectionOrderAndBudgetInvariants(t *testing.T) {
	ctx := context.Background()
	allowed := event.AgentID("agent-a")

	t.Run("kinds and identity fields form one stable total order", func(t *testing.T) {
		refs := []ObjectRef{
			{Kind: ObjectLease, Lease: state.LeaseRef{Kind: "gpu", Key: "1"}},
			{Kind: ObjectNotice, Notice: task.Notice{TaskID: "task-2", Round: 1, Kind: "progress", Seq: 2}},
			{Kind: ObjectTask, TaskID: "task-2"},
			{Kind: ObjectInvocation, InvocationID: "inv-2"},
			{Kind: ObjectSession, Address: event.Address{AgentID: allowed, SessionID: "s2"}, InstanceID: "instance-1"},
			{Kind: ObjectAgent, Address: event.Address{AgentID: "agent-b"}},
			{Kind: ObjectAgent, Address: event.Address{AgentID: allowed}},
			{Kind: ObjectLease, Lease: state.LeaseRef{Kind: "gpu", Key: "0"}},
			{Kind: ObjectNotice, Notice: task.Notice{TaskID: "task-2", Round: 0, Kind: "progress", Seq: 9}},
			{Kind: ObjectSession, Address: event.Address{AgentID: allowed, SessionID: "s1"}, InstanceID: "instance-1"},
		}
		sort.SliceStable(refs, func(i, j int) bool { return refLess(refs[i], refs[j]) })
		var kinds []ObjectKind
		for _, ref := range refs {
			kinds = append(kinds, ref.Kind)
		}
		want := []ObjectKind{
			ObjectAgent, ObjectAgent,
			ObjectSession, ObjectSession,
			ObjectInvocation,
			ObjectTask,
			ObjectNotice, ObjectNotice,
			ObjectLease, ObjectLease,
		}
		if len(kinds) != len(want) {
			t.Fatalf("order = %v, want %v", kinds, want)
		}
		for i := range want {
			if kinds[i] != want[i] {
				t.Fatalf("order = %v, want the kind sequence %v", kinds, want)
			}
		}
		if refs[0].Address.AgentID != allowed || refs[1].Address.AgentID != "agent-b" {
			t.Fatalf("agent order = (%q, %q), want agents inside one kind by identity",
				refs[0].Address.AgentID, refs[1].Address.AgentID)
		}
		full := ObjectRef{Kind: ObjectSession, Address: event.Address{AgentID: allowed, SessionID: "s1"}, InstanceID: "instance-1"}
		contentOnly := ObjectRef{Kind: ObjectSession, Address: event.Address{AgentID: allowed, SessionID: "s1"}, InstanceID: "instance-1", TaskID: "ignored"}
		if refIDOf(full) != refIDOf(contentOnly) {
			t.Fatalf("identities = (%+v, %+v), want content fields to take no part", refIDOf(full), refIDOf(contentOnly))
		}
		if refLess(full, contentOnly) || refLess(contentOnly, full) {
			t.Fatalf("one identity sorted before itself through a content field")
		}
		agentWithoutSession := ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: allowed}}
		agentWithSession := ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: allowed, SessionID: "s9"}}
		if refIDOf(agentWithoutSession) != refIDOf(agentWithSession) {
			t.Fatalf("agent identities differ by a session, want one agent named by its agent identity")
		}
		noticeA := ObjectRef{Kind: ObjectNotice, Notice: task.Notice{TaskID: "task-1", Round: 1, Kind: "progress", Seq: 1, Event: "event-1"}}
		noticeB := ObjectRef{Kind: ObjectNotice, Notice: task.Notice{TaskID: "task-1", Round: 1, Kind: "progress", Seq: 1, Event: "event-2"}}
		if refIDOf(noticeA) != refIDOf(noticeB) {
			t.Fatalf("notice identities differ by the pointed fact, want the fact left as content")
		}
		leaseA := ObjectRef{Kind: ObjectLease, Lease: state.LeaseRef{Kind: "gpu", Key: "0", Ref: "one"}}
		leaseB := ObjectRef{Kind: ObjectLease, Lease: state.LeaseRef{Kind: "gpu", Key: "0", Ref: "two"}}
		if refIDOf(leaseA) != refIDOf(leaseB) {
			t.Fatalf("lease identities differ by the release reference, want one resource identity")
		}
	})

	t.Run("pages concatenate into exactly the snapshot details", func(t *testing.T) {
		src := &invariantSource{}
		for _, id := range []event.AgentID{"agent-a", "agent-b", "agent-c"} {
			src.put(invariantAgentDetail(id, true, false, false))
		}
		projection := NewBoundedProjection(src, 2, Budget{})
		scope := []event.AgentID{"agent-a", "agent-b", "agent-c"}
		filter := invariantFilter(scope)

		snap, cursor, err := projection.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if len(snap.Details) != 3 {
			t.Fatalf("details = %d, want the three in-scope entries", len(snap.Details))
		}
		if snap.Truncated != (SnapshotTruncation{}) {
			t.Fatalf("truncation = %+v, want a complete view", snap.Truncated)
		}
		var paged []Detail
		var after ObjectRef
		for range 3 {
			page, err := projection.List(snap.Revision, cursor, filter, after, 1)
			if err != nil {
				t.Fatalf("page: %v", err)
			}
			if len(page) != 1 {
				t.Fatalf("page = %d, want one entry", len(page))
			}
			paged = append(paged, page...)
			after = page[len(page)-1].Ref
		}
		if len(paged) != len(snap.Details) {
			t.Fatalf("paged entries = %d, want the snapshot's %d", len(paged), len(snap.Details))
		}
		for i := range paged {
			if refIDOf(paged[i].Ref) != refIDOf(snap.Details[i].Ref) {
				t.Fatalf("paged identity %d = %+v, want %+v", i, paged[i].Ref, snap.Details[i].Ref)
			}
		}
	})

	t.Run("an object budget holds its bound and counts the rest", func(t *testing.T) {
		src := &invariantSource{}
		for _, id := range []event.AgentID{"agent-a", "agent-b", "agent-c"} {
			src.put(invariantAgentDetail(id, true, false, false))
		}
		projection := NewBoundedProjection(src, 2, Budget{MaxObjects: 1})
		snap, _, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{"agent-a", "agent-b", "agent-c"}))
		if err != nil {
			t.Fatalf("bounded snapshot: %v", err)
		}
		if len(snap.Details) != 1 {
			t.Fatalf("details = %d, want the object bound of one", len(snap.Details))
		}
		if snap.Truncated.OmittedObjects != 2 {
			t.Fatalf("omitted objects = %d, want the two identities the bound left out",
				snap.Truncated.OmittedObjects)
		}
	})

	t.Run("a byte boundary counts the boundary entry and the rest", func(t *testing.T) {
		src := &invariantSource{}
		first := invariantAgentDetail("agent-a", true, false, false)
		src.put(first)
		src.put(invariantAgentDetail("agent-b", true, false, false))
		src.put(invariantAgentDetail("agent-c", true, false, false))
		bound := DetailBytes(first)
		projection := NewBoundedProjection(src, 2, Budget{MaxBytes: bound})
		snap, _, err := projection.Snapshot(ctx, invariantFilter([]event.AgentID{"agent-a", "agent-b", "agent-c"}))
		if err != nil {
			t.Fatalf("byte-bounded snapshot: %v", err)
		}
		if len(snap.Details) != 1 || refIDOf(snap.Details[0].Ref) != refIDOf(first.Ref) {
			t.Fatalf("details = %+v, want exactly the entry fitting the byte bound", snap.Details)
		}
		if snap.Truncated.OmittedObjects != 2 {
			t.Fatalf("omitted objects = %d, want the overflowing entry and every later one counted",
				snap.Truncated.OmittedObjects)
		}
	})

	t.Run("a page larger than the object budget is refused", func(t *testing.T) {
		src := &invariantSource{}
		src.put(invariantAgentDetail(allowed, true, false, false))
		projection := NewBoundedProjection(src, 2, Budget{MaxObjects: 2})
		filter := invariantFilter([]event.AgentID{allowed})
		at, cursor, err := projection.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if _, err := projection.List(at.Revision, cursor, filter, ObjectRef{}, 3); !errors.Is(err, ErrSnapshotBudgetExceeded) {
			t.Fatalf("oversized page = %v, want %v", err, ErrSnapshotBudgetExceeded)
		}
		page, err := projection.List(at.Revision, cursor, filter, ObjectRef{}, 0)
		if err != nil {
			t.Fatalf("empty page: %v", err)
		}
		if len(page) != 0 {
			t.Fatalf("empty page = %+v, want a non-positive limit answered with no entry", page)
		}
		if notices := filter; len(notices.States) != 0 {
			t.Fatalf("filter states = %d, want the helper to carry no state dimension", len(notices.States))
		}
	})
}
