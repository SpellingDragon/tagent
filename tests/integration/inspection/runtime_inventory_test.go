// Package inspection exercises the read-only runtime inspection projection of
// the query package over a real runtime assembly.
package inspection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// planAgents reads the configured agent identities out of the same
// orchestration plan the fixture publishes into the real binding owner.
type planAgents struct{ plan state.StatePlan }

// Agents returns the configured agent identities of the plan.
func (p planAgents) Agents(ctx context.Context) ([]event.AgentID, error) {
	ids := make([]event.AgentID, 0, len(p.plan.Agents))
	for id := range p.plan.Agents {
		ids = append(ids, id)
	}
	return ids, nil
}

// siteRef names one established session site of the fixture.
type siteRef struct {
	addr     event.Address
	instance event.InstanceID
}

// storeSites reads every established site through the state owner's own
// authorized read, so an ungranted subject is refused by the owner rather than
// by the fixture.
type storeSites struct {
	store   *state.MemoryStore
	subject state.Subject
	keys    []siteRef
}

// Sites returns one owner snapshot per established site.
func (s storeSites) Sites(ctx context.Context) ([]state.Session, error) {
	sites := make([]state.Session, 0, len(s.keys))
	for _, key := range s.keys {
		site, err := s.store.Site(ctx, s.subject, key.addr, key.instance)
		if err != nil {
			return nil, err
		}
		sites = append(sites, *site)
	}
	return sites, nil
}

// taskLookup reads the declaration and status of one task from the store that
// holds them, so a store that refuses the read is observable by name.
type taskLookup interface {
	Lookup(ctx context.Context, taskID event.TaskID) (task.Spec, task.Status, error)
}

// fixtureReading is the single read port of the fixture: the plan it publishes,
// the session sites the state owner holds and the tasks the task carriers hold.
// A site source that is not wired at all reports its refusal through sitesErr
// instead of answering an empty site list, so a refused dimension stays
// distinguishable from an empty one.
type fixtureReading struct {
	agents   planAgents
	sites    storeSites
	sitesErr error
	slots    *state.Occupancy
	leases   *state.Leases
	durable  *state.DurableCommit
	lookup   taskLookup
	manager  *task.MemoryManager
}

// The read port answers the inventory of the fixture.
var _ query.Reading = fixtureReading{}

// Published returns the generation the fixture publishes: its agents beside the
// resource registrations it holds.
func (r fixtureReading) Published(ctx context.Context) (query.Generation, error) {
	ids, err := r.agents.Agents(ctx)
	if err != nil {
		return query.Generation{}, err
	}
	generation := query.Generation{Agents: ids}
	if r.leases != nil {
		generation.Leases = r.leases.Refs()
	}
	return generation, nil
}

// Sites returns one owner snapshot per established site, each beside the slot
// the occupancy owner holds for it.
func (r fixtureReading) Sites(ctx context.Context) ([]query.SiteReading, error) {
	if r.sitesErr != nil {
		return nil, r.sitesErr
	}
	sessions, err := r.sites.Sites(ctx)
	if err != nil {
		return nil, err
	}
	readings := make([]query.SiteReading, 0, len(sessions))
	for _, session := range sessions {
		reading := query.SiteReading{Session: session}
		if r.slots != nil {
			reading.Invocation, reading.Decision, reading.Held = r.slots.Slot(session.Address, session.Instance)
		}
		readings = append(readings, reading)
	}
	return readings, nil
}

// Tasks returns every task the commit carrier lists, each beside its declaration
// and status, the obligations its manager keeps and the notifications its record
// still owes.
func (r fixtureReading) Tasks(ctx context.Context) ([]query.TaskReading, error) {
	ids := r.durable.TaskIDs()
	readings := make([]query.TaskReading, 0, len(ids))
	for _, id := range ids {
		spec, status, err := r.lookup.Lookup(ctx, id)
		if err != nil {
			return nil, err
		}
		obligations, err := r.manager.Obligations(ctx, id)
		if err != nil {
			return nil, err
		}
		readings = append(readings, query.TaskReading{
			TaskID:      id,
			Spec:        spec,
			Status:      status,
			Obligations: obligations,
			Outbox:      r.durable.Outbox(id),
		})
	}
	return readings, nil
}

// runtimeFixture is one assembled runtime: the real state and task owners
// beside the inventory reading them.
type runtimeFixture struct {
	ctx       context.Context
	inventory *query.Inventory
	store     *state.MemoryStore
	authority *state.Authority
	occupancy *state.Occupancy
	leases    *state.Leases
	durable   *state.DurableCommit
	registry  *task.MemoryRegistry
	manager   *task.MemoryManager
	plan      state.StatePlan
	siteKeys  []siteRef
	inspector state.Subject
	instance  event.InstanceID
	addrA     event.Address
	addrB     event.Address
	addrC     event.Address
	task1     event.TaskID
	task2     event.TaskID
	spec1     task.Spec
	spec2     task.Spec
	notices   []task.Notice
	refusal   error
}

// newRuntimeFixture assembles the real owners and drives them into one known
// multi-dimensional state: three session sites, one held execution slot, two
// tasks, four notifications standing in four delivery phases and two held
// resource leases.
func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	ctx := context.Background()
	instance := event.InstanceID("inst-1")
	inspector := state.Subject("inspector")
	addrA := event.Address{AgentID: event.AgentID("agent-a"), SessionID: event.SessionID("s1")}
	addrB := event.Address{AgentID: event.AgentID("agent-b"), SessionID: event.SessionID("s1")}
	addrC := event.Address{AgentID: event.AgentID("agent-c"), SessionID: event.SessionID("s1")}
	tier, err := memory.OpenTier(ctx, memory.StorageConfig{Type: memory.StorageLocalFile, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("OpenTier: %v", err)
	}
	durable, err := state.NewDurableCommit(memory.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("NewDurableCommit: %v", err)
	}
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	authority := state.NewAuthority()
	store := state.NewMemoryStore(authority)
	siteKeys := []siteRef{{addrA, instance}, {addrB, instance}, {addrC, instance}}
	for _, key := range siteKeys {
		if _, err := store.Establish(ctx, key.addr, key.instance); err != nil {
			t.Fatalf("Establish: %v", err)
		}
		if err := authority.GrantRead(ctx, inspector, key.addr); err != nil {
			t.Fatalf("GrantRead: %v", err)
		}
	}
	occupancy := state.NewOccupancy()
	if err := occupancy.Occupy(addrA, instance, event.InvocationID("inv-a1"), event.DecisionID("dec-a1")); err != nil {
		t.Fatalf("Occupy: %v", err)
	}
	leases := state.NewLeases()
	leases.Register("model", "quota-a", nil)
	leases.Register("storage", "slot-b", nil)
	plan := state.StatePlan{Agents: map[event.AgentID]state.AgentBinding{
		event.AgentID("agent-a"): {},
		event.AgentID("agent-b"): {},
	}}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	refusal := errors.New("the target refused the replayed delivery")
	deliver := func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		if notice.Seq == 3 {
			return task.DeliveryRejected, refusal
		}
		if err := store.AcceptNotice(ctx, notice); err != nil {
			return task.DeliveryPending, err
		}
		if _, err := durable.Accept(ctx, notice); err != nil {
			return task.DeliveryPending, err
		}
		return task.DeliveryAccepted, nil
	}
	manager := task.NewMemoryManager(registry, deliver)
	spec1 := task.Spec{
		CallID:       "call-1",
		DedupKey:     task.DedupKey("inst-1\x00inv-a1\x00dec-a1\x00call-1"),
		InvocationID: event.InvocationID("inv-a1"),
		InstanceID:   instance,
		Description:  "agent-b runs the requested work",
		Source:       addrA,
		Target:       addrB,
		Execution:    "serialized",
	}
	receipt1, err := registry.Register(ctx, spec1)
	if err != nil {
		t.Fatalf("Register task-1: %v", err)
	}
	task1 := receipt1.TaskID
	if err := durable.Declare(ctx, task1, spec1); err != nil {
		t.Fatalf("Declare task-1: %v", err)
	}
	notices := make([]task.Notice, 0, 4)
	envelopes := make([]task.NoticeEnvelope, 0, 4)
	for seq := int64(0); seq < 4; seq++ {
		notice := task.Notice{
			TaskID: task1,
			Round:  1,
			Kind:   task.NoticeSettled,
			Seq:    seq,
			Event:  event.EventID(fmt.Sprintf("evt-%d", seq+1)),
		}
		notices = append(notices, notice)
		envelopes = append(envelopes, task.NoticeEnvelope{Notice: notice, Target: addrB, Source: addrA, TargetInstance: instance})
	}
	if err := store.TrackDelivery(ctx, task1, task.HandoffTarget{Target: addrB, TargetInstance: instance}); err != nil {
		t.Fatalf("TrackDelivery: %v", err)
	}
	result, err := registry.Advance(ctx, task1, task.StatusSettled, task.MutationID("settle:"+string(task1)), envelopes)
	if err != nil || result != task.CommitCommitted {
		t.Fatalf("Advance settle = %v, %v, want committed", result, err)
	}
	framed := task.WithCallFrame(ctx, task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   instance,
			InvocationID: event.InvocationID("inv-a1"),
			DecisionID:   event.DecisionID("dec-a1"),
			CallID:       "call-1",
		},
		Address:   addrA,
		Execution: "serialized",
	})
	if err := manager.Flush(framed, task1); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := store.Claim(ctx, addrB, instance, 1); err != nil {
		t.Fatalf("Claim first input: %v", err)
	}
	if err := store.Consume(ctx, addrB, instance, 1); err != nil {
		t.Fatalf("Consume first input: %v", err)
	}
	if err := store.Claim(ctx, addrB, instance, 2); err != nil {
		t.Fatalf("Claim second input: %v", err)
	}
	spec2 := task.Spec{
		CallID:       "call-2",
		DedupKey:     task.DedupKey("inst-1\x00inv-c1\x00dec-c1\x00call-2"),
		InvocationID: event.InvocationID("inv-c1"),
		InstanceID:   instance,
		Description:  "agent-a reviews the result",
		Source:       addrC,
		Target:       addrA,
		Execution:    "serialized",
	}
	receipt2, err := registry.Register(ctx, spec2)
	if err != nil {
		t.Fatalf("Register task-2: %v", err)
	}
	task2 := receipt2.TaskID
	if err := durable.Declare(ctx, task2, spec2); err != nil {
		t.Fatalf("Declare task-2: %v", err)
	}
	inventory := query.NewInventory(fixtureReading{
		agents:  planAgents{plan: plan},
		sites:   storeSites{store: store, subject: inspector, keys: siteKeys},
		slots:   occupancy,
		leases:  leases,
		durable: durable,
		lookup:  registry,
		manager: manager,
	})
	return &runtimeFixture{
		ctx:       ctx,
		inventory: inventory,
		store:     store,
		authority: authority,
		occupancy: occupancy,
		leases:    leases,
		durable:   durable,
		registry:  registry,
		manager:   manager,
		plan:      plan,
		siteKeys:  siteKeys,
		inspector: inspector,
		instance:  instance,
		addrA:     addrA,
		addrB:     addrB,
		addrC:     addrC,
		task1:     task1,
		task2:     task2,
		spec1:     spec1,
		spec2:     spec2,
		notices:   notices,
		refusal:   refusal,
	}
}

// TestRuntimeInventory proves the read-only runtime inventory and its derived counts over a real runtime assembly.
// - 六维明细与源一致: every dimension entry matches the owner reading it projects
// - 统计等于明细派生: every count is derived from the detail entries of the one view it reports
// - 跨维不相加: the counts keep the dimensions apart and hold no summed "running" number
// - 只读不改授权: inventory reads leave every recorded grant exactly as recorded
// - 源不可读具名: an unreadable source refuses by name beside the dimension it affects
// - 读面不可变: a write through a returned slice reaches neither the view nor a later read
// - 公开投影保真: the projected detail carries every real reading of its kind instead of one compressed label
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestRuntimeInventory(t *testing.T) {
	f := newRuntimeFixture(t)

	t.Run("S1 六维明细与源一致: every dimension entry matches its owner reading", func(t *testing.T) {
		view, err := f.inventory.View(f.ctx)
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		wantAgents := []query.AgentEntry{
			{AgentID: "agent-a", Configured: true, Resident: true, Busy: true},
			{AgentID: "agent-b", Configured: true, Resident: true, Busy: false},
			{AgentID: "agent-c", Configured: false, Resident: true, Busy: false},
		}
		if agents := view.Agents(); !reflect.DeepEqual(agents, wantAgents) {
			t.Fatalf("agents = %+v, want %+v", agents, wantAgents)
		}
		wantSessions := []query.SessionEntry{
			{Address: f.addrA, Instance: f.instance},
			{Address: f.addrB, Instance: f.instance, Accepted: 3, Claimed: 2, Completed: 1, Consumed: 1},
			{Address: f.addrC, Instance: f.instance},
		}
		sessions := view.Sessions()
		if !reflect.DeepEqual(sessions, wantSessions) {
			t.Fatalf("sessions = %+v, want %+v", sessions, wantSessions)
		}
		for i, want := range wantSessions {
			site, err := f.store.Site(f.ctx, f.inspector, want.Address, want.Instance)
			if err != nil {
				t.Fatalf("Site(%v): %v", want.Address, err)
			}
			claimed, completed := 0, 0
			for _, input := range site.Mailbox {
				if input.Claim != state.ClaimPending {
					claimed++
				}
				if input.Receipt.Completed {
					completed++
				}
			}
			got := sessions[i]
			if got.Accepted != len(site.Mailbox) || got.Claimed != claimed || got.Completed != completed || got.Consumed != site.Consumed {
				t.Fatalf("session %+v = %+v, disagrees with owner site %+v", want.Address, got, site)
			}
		}
		invocations := view.Invocations()
		if len(invocations) != 2 {
			t.Fatalf("invocations = %+v, want 2 entries", invocations)
		}
		if invocations[0].InvocationID != "inv-a1" || invocations[0].Address != f.addrA || invocations[0].Instance != f.instance || invocations[0].Decision != "dec-a1" {
			t.Fatalf("first invocation = %+v, want inv-a1 on agent-a/s1 holding dec-a1", invocations[0])
		}
		if len(invocations[0].Pending) != 1 || invocations[0].Pending[0] != f.notices[3] {
			t.Fatalf("inv-a1 pending = %+v, want the refused notification only", invocations[0].Pending)
		}
		if invocations[1].InvocationID != "inv-c1" || invocations[1].Address != f.addrC || invocations[1].Decision != "" || len(invocations[1].Pending) != 0 {
			t.Fatalf("second invocation = %+v, want inv-c1 on agent-c/s1 with no slot and no pending notification", invocations[1])
		}
		slotInvocation, slotDecision, held := f.occupancy.Slot(f.addrA, f.instance)
		if !held || slotInvocation != invocations[0].InvocationID || slotDecision != invocations[0].Decision {
			t.Fatalf("invocation reading = %+v, disagrees with owner slot %v %v %v", invocations[0], slotInvocation, slotDecision, held)
		}
		tasks := view.Tasks()
		if len(tasks) != 2 {
			t.Fatalf("tasks = %+v, want 2 entries", tasks)
		}
		if tasks[0].TaskID != f.task1 || tasks[0].Spec != f.spec1 || tasks[0].Status != task.StatusSettled {
			t.Fatalf("first task = %+v, want %s settled with its own declaration", tasks[0], f.task1)
		}
		ownerObligations, err := f.manager.Obligations(f.ctx, f.task1)
		if err != nil {
			t.Fatalf("Obligations(%s): %v", f.task1, err)
		}
		if !reflect.DeepEqual(tasks[0].Obligations, ownerObligations) {
			t.Fatalf("task-1 obligations = %+v, want owner reading %+v", tasks[0].Obligations, ownerObligations)
		}
		if tasks[0].Obligations.Count != 1 || tasks[0].Obligations.Blocked[0].Reason != f.refusal.Error() {
			t.Fatalf("task-1 obligations = %+v, want one refused delivery beside its reason", tasks[0].Obligations)
		}
		if tasks[1].TaskID != f.task2 || tasks[1].Spec != f.spec2 || tasks[1].Status != task.StatusAccepted || tasks[1].Obligations.Count != 1 || tasks[1].Obligations.Open[0].Kind != task.ObligationAwaitingTool {
			t.Fatalf("second task = %+v, want %s accepted with its admission obligation open", tasks[1], f.task2)
		}
		wantNotices := []query.NoticeEntry{
			{Notice: f.notices[0], Target: f.addrB, TargetInstance: f.instance, Phase: query.NoticePhaseConsumed, Delivery: task.DeliveryAccepted, AcceptSeq: 1, Claim: state.ClaimConsumed, Completed: true},
			{Notice: f.notices[1], Target: f.addrB, TargetInstance: f.instance, Phase: query.NoticePhaseClaimed, Delivery: task.DeliveryAccepted, AcceptSeq: 2, Claim: state.ClaimClaimed},
			{Notice: f.notices[2], Target: f.addrB, TargetInstance: f.instance, Phase: query.NoticePhaseAccepted, Delivery: task.DeliveryAccepted, AcceptSeq: 3, Claim: state.ClaimPending},
			{Notice: f.notices[3], Target: f.addrB, TargetInstance: f.instance, Phase: query.NoticePhaseOwed, Delivery: task.DeliveryPending, Blocked: []task.BlockedObligation{{Notice: f.notices[3], Reason: f.refusal.Error()}}},
		}
		notices := view.Notices()
		if !reflect.DeepEqual(notices, wantNotices) {
			t.Fatalf("notices = %+v, want %+v", notices, wantNotices)
		}
		for _, owed := range f.durable.Outbox(f.task1) {
			matched := false
			for _, entry := range notices {
				if entry.Notice != owed.Notice.Notice {
					continue
				}
				matched = true
				if entry.Delivery != owed.Delivery || entry.Target != owed.Notice.Target || entry.TargetInstance != owed.Notice.TargetInstance {
					t.Fatalf("notice %+v = %+v, disagrees with owner outbox %+v", entry.Notice, entry, owed)
				}
			}
			if !matched {
				t.Fatalf("owner outbox notice %+v is missing from the view", owed.Notice.Notice)
			}
		}
		ownerRefs := f.leases.Refs()
		leaseEntries := view.Leases()
		if len(leaseEntries) != len(ownerRefs) {
			t.Fatalf("leases = %+v, want the %d owner registrations", leaseEntries, len(ownerRefs))
		}
		for i, ref := range ownerRefs {
			if leaseEntries[i].Lease != ref {
				t.Fatalf("lease %d = %+v, want owner order %+v", i, leaseEntries[i].Lease, ref)
			}
		}
	})

	t.Run("S2 统计等于明细派生: counts are derived from the detail of the one view they report", func(t *testing.T) {
		view, err := f.inventory.View(f.ctx)
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		counts := view.Counts()
		if counts.Sessions != len(view.Sessions()) || counts.Invocations != len(view.Invocations()) || counts.Tasks != len(view.Tasks()) || counts.Notices != len(view.Notices()) || counts.Leases != len(view.Leases()) {
			t.Fatalf("counts = %+v, disagree with the detail lengths of the same view", counts)
		}
		derived := query.AgentCounts{}
		for _, agent := range view.Agents() {
			if agent.Configured {
				derived.Configured++
			}
			if agent.Resident {
				derived.Resident++
			}
			if agent.Busy {
				derived.Busy++
			}
		}
		if counts.Agents != derived {
			t.Fatalf("agent counts = %+v, want the same readings derived from the agent entries %+v", counts.Agents, derived)
		}
		if derived != (query.AgentCounts{Configured: 2, Resident: 3, Busy: 1}) {
			t.Fatalf("agent counts = %+v, want the fixture readings configured 2, resident 3, busy 1", derived)
		}
		if counts.Sessions != 3 || counts.Invocations != 2 || counts.Tasks != 2 || counts.Notices != 4 || counts.Leases != 2 {
			t.Fatalf("counts = %+v, want the fixture readings 3 2 2 4 2", counts)
		}
		if counts.Tasks != len(f.durable.TaskIDs()) || counts.Leases != len(f.leases.Refs()) {
			t.Fatalf("counts = %+v, disagree with the owner totals of tasks and leases", counts)
		}
		if repeated := view.Counts(); repeated != counts {
			t.Fatalf("repeated counts = %+v, want the same derived readings %+v", repeated, counts)
		}
	})

	t.Run("S3 跨维不相加: the counts keep the dimensions apart and hold no summed reading", func(t *testing.T) {
		fieldNames := func(value any) []string {
			shape := reflect.TypeOf(value)
			names := make([]string, 0, shape.NumField())
			for i := 0; i < shape.NumField(); i++ {
				names = append(names, shape.Field(i).Name)
			}
			sort.Strings(names)
			return names
		}
		gotCounts := fieldNames(query.InventoryCounts{})
		wantCounts := []string{"Agents", "Invocations", "Leases", "Notices", "Sessions", "Tasks"}
		if !reflect.DeepEqual(gotCounts, wantCounts) {
			t.Fatalf("InventoryCounts fields = %v, want %v and no field summing dimensions", gotCounts, wantCounts)
		}
		gotAgents := fieldNames(query.AgentCounts{})
		wantAgents := []string{"Busy", "Configured", "Resident"}
		if !reflect.DeepEqual(gotAgents, wantAgents) {
			t.Fatalf("AgentCounts fields = %v, want %v and no field summing the three readings", gotAgents, wantAgents)
		}
		view, err := f.inventory.View(f.ctx)
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		counts := view.Counts()
		readings := []int{counts.Agents.Configured, counts.Agents.Resident, counts.Agents.Busy, counts.Sessions, counts.Invocations, counts.Tasks, counts.Notices, counts.Leases}
		total := 0
		for _, reading := range readings {
			total += reading
		}
		for i, reading := range readings {
			if reading == total {
				t.Fatalf("reading %d equals the cross-dimension total %d: dimensions were added together", i, total)
			}
		}
	})

	t.Run("S4 只读不改授权: inventory reads leave every recorded grant exactly as recorded", func(t *testing.T) {
		view, err := f.inventory.View(f.ctx)
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		if len(view.Agents())+len(view.Sessions())+len(view.Invocations())+len(view.Tasks())+len(view.Notices())+len(view.Leases()) == 0 {
			t.Fatalf("inventory reads answered no detail")
		}
		if err := f.authority.CheckRead(f.ctx, f.inspector, f.addrB); err != nil {
			t.Fatalf("CheckRead(inspector) after inventory reads: %v, want the recorded grant intact", err)
		}
		if _, err := f.store.Site(f.ctx, state.Subject("outsider"), f.addrA, f.instance); !errors.Is(err, state.ErrReadNotAllowed) {
			t.Fatalf("outsider site read after inventory reads = %v, want ErrReadNotAllowed unchanged", err)
		}
		if err := f.authority.CheckRead(f.ctx, state.Subject("outsider"), f.addrA); !errors.Is(err, state.ErrReadNotAllowed) {
			t.Fatalf("outsider check after inventory reads = %v, want no grant appeared", err)
		}
	})

	t.Run("S5 源不可读具名: an unreadable source refuses by name beside the dimension it affects", func(t *testing.T) {
		unreadable := query.NewInventory(fixtureReading{
			agents:   planAgents{plan: f.plan},
			sitesErr: errors.New("the site source is not wired"),
			slots:    f.occupancy,
			leases:   f.leases,
			durable:  f.durable,
			lookup:   f.registry,
			manager:  f.manager,
		})
		empty, err := unreadable.View(f.ctx)
		if err == nil {
			t.Fatalf("View without a session face = %+v, want a named refusal", empty)
		}
		if !errors.Is(err, query.ErrSourceUnreadable) {
			t.Fatalf("View refusal = %v, want the unreadable-source class", err)
		}
		var sourceErr *query.SourceError
		if !errors.As(err, &sourceErr) {
			t.Fatalf("View refusal = %v, want a SourceError naming the affected dimension", err)
		}
		if len(empty.Agents())+len(empty.Sessions())+len(empty.Invocations())+len(empty.Tasks())+len(empty.Notices())+len(empty.Leases()) != 0 {
			t.Fatalf("refused view = %+v, want no detail beside the refusal", empty)
		}
		refused := query.NewInventory(fixtureReading{
			agents:  planAgents{plan: f.plan},
			sites:   storeSites{store: f.store, subject: state.Subject("outsider"), keys: f.siteKeys},
			slots:   f.occupancy,
			leases:  f.leases,
			durable: f.durable,
			lookup:  f.registry,
			manager: f.manager,
		})
		sessions, err := refused.Sessions(f.ctx)
		if err == nil {
			t.Fatalf("Sessions under an unauthorized subject = %+v, want a named refusal", sessions)
		}
		if !errors.Is(err, state.ErrReadNotAllowed) || !errors.Is(err, query.ErrSourceUnreadable) {
			t.Fatalf("Sessions refusal = %v, want the owner cause kept beside the unreadable-source class", err)
		}
		sourceErr = nil
		if !errors.As(err, &sourceErr) || sourceErr.Dimension != query.ObjectSession {
			t.Fatalf("Sessions refusal = %v, want a SourceError naming the session dimension", err)
		}
		if len(sessions) != 0 {
			t.Fatalf("Sessions under refusal = %+v, want no detail beside the error", sessions)
		}
		if partial, err := refused.View(f.ctx); err == nil || len(partial.Agents())+len(partial.Sessions())+len(partial.Invocations())+len(partial.Tasks())+len(partial.Notices())+len(partial.Leases()) != 0 {
			t.Fatalf("View under an unauthorized session face = %+v, %v, want a named refusal instead of a partial view", partial, err)
		}
	})

	t.Run("S6 读面不可变: a write through a returned slice reaches neither the view nor a later read", func(t *testing.T) {
		view, err := f.inventory.View(f.ctx)
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		mutatedAgents := view.Agents()
		mutatedAgents[0].Busy = false
		mutatedTasks := view.Tasks()
		mutatedTasks[0].Obligations.Blocked[0].Reason = "tampered"
		mutatedNotices := view.Notices()
		mutatedNotices[3].Blocked[0].Reason = "tampered"
		mutatedInvocations := view.Invocations()
		mutatedInvocations[0].Pending[0] = task.Notice{}
		if !view.Agents()[0].Busy {
			t.Fatalf("agent reading changed through a returned slice: %+v", view.Agents()[0])
		}
		if view.Tasks()[0].Obligations.Blocked[0].Reason == "tampered" {
			t.Fatalf("blocked reason changed through a returned slice: %+v", view.Tasks()[0].Obligations)
		}
		if view.Notices()[3].Blocked[0].Reason == "tampered" {
			t.Fatalf("notice blocked reason changed through a returned slice: %+v", view.Notices()[3])
		}
		if view.Invocations()[0].Pending[0] == (task.Notice{}) {
			t.Fatalf("pending notification changed through a returned slice: %+v", view.Invocations()[0])
		}
		if counts := view.Counts(); counts.Agents.Busy != 1 {
			t.Fatalf("counts changed through a returned slice: %+v", counts)
		}
	})

	t.Run("S7 公开投影保真: the projected detail carries every real reading of its kind", func(t *testing.T) {
		proj := newTestProjection(f.inventory, 8)
		snap, _, err := proj.Snapshot(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}

		wantAgents := map[event.AgentID]query.AgentState{
			"agent-a": {Configured: true, Resident: true, Busy: true},
			"agent-b": {Configured: true, Resident: true, Busy: false},
			"agent-c": {Configured: false, Resident: true, Busy: false},
		}
		for id, want := range wantAgents {
			d, ok := findDetail(snap.Details, agentRef(id))
			if !ok {
				t.Fatalf("agent %s 缺席快照", id)
			}
			if d.State.Agent != want {
				t.Fatalf("agent %s 读数 = %+v, want 三读数并存 %+v", id, d.State.Agent, want)
			}
			if d.State.TaskStatus != 0 || d.State.NoticePhase != "" || d.State.Decision != "" {
				t.Fatalf("agent %s 读数混入他类词汇: %+v", id, d.State)
			}
		}

		d, ok := findDetail(snap.Details, sessionRef(f, f.addrB))
		if !ok {
			t.Fatalf("会话 addrB 缺席快照")
		}
		if want := (query.SessionState{Accepted: 3, Claimed: 2, Completed: 1, Consumed: 1}); d.State.Session != want {
			t.Fatalf("会话 addrB 邮箱读数 = %+v, want %+v", d.State.Session, want)
		}

		for _, inv := range []struct {
			id       event.InvocationID
			addr     event.Address
			decision event.DecisionID
		}{{"inv-a1", f.addrA, "dec-a1"}, {"inv-c1", f.addrC, ""}} {
			ref := query.ObjectRef{Kind: query.ObjectInvocation, Address: inv.addr, InstanceID: f.instance, InvocationID: inv.id}
			d, ok := findDetail(snap.Details, ref)
			if !ok {
				t.Fatalf("invocation %s 缺席快照", inv.id)
			}
			if d.State.Decision != inv.decision {
				t.Fatalf("invocation %s 活动决策 = %q, want %q", inv.id, d.State.Decision, inv.decision)
			}
		}

		d1, ok := findDetail(snap.Details, taskRef(f))
		if !ok {
			t.Fatalf("task-1 缺席快照")
		}
		if d1.State.TaskStatus != task.StatusSettled {
			t.Fatalf("task-1 状态 = %+v, want 原生 settled", d1.State)
		}
		ref2 := query.ObjectRef{Kind: query.ObjectTask, Address: f.spec2.Source, InstanceID: f.spec2.InstanceID, TaskID: f.task2}
		d2, ok := findDetail(snap.Details, ref2)
		if !ok {
			t.Fatalf("task-2 缺席快照")
		}
		if d2.State.TaskStatus != task.StatusAccepted {
			t.Fatalf("task-2 状态 = %+v, want 原生 accepted", d2.State)
		}
		if d1.State == d2.State {
			t.Fatalf("两任务读数被压成同一值: %+v", d1.State)
		}
		wantPhases := []query.NoticePhase{
			query.NoticePhaseConsumed, query.NoticePhaseClaimed, query.NoticePhaseAccepted, query.NoticePhaseOwed,
		}
		for i, want := range wantPhases {
			d, ok := findDetail(snap.Details, noticeRef(f, i))
			if !ok {
				t.Fatalf("通知 %d 缺席快照", i)
			}
			if d.State.NoticePhase != want {
				t.Fatalf("通知 %d 投递相 = %q, want %q", i, d.State.NoticePhase, want)
			}
			if want == query.NoticePhaseConsumed && d.State != query.DetailRetired {
				t.Fatalf("已消费通知读数 = %+v, want 消费相锚 %+v", d.State, query.DetailRetired)
			}
			if d.State.TaskStatus != 0 || d.State.Agent != (query.AgentState{}) {
				t.Fatalf("通知 %d 读数混入他类词汇: %+v", i, d.State)
			}
		}
		consumed, _ := findDetail(snap.Details, noticeRef(f, 0))
		if d1.State == consumed.State {
			t.Fatalf("终态任务与已消费通知被压成同一值: %+v", d1.State)
		}

		lease, ok := findLease(snap.Details, "model", "quota-a")
		if !ok {
			t.Fatalf("租约 model/quota-a 缺席快照")
		}
		if lease.State.Holder != lease.Ref.Lease {
			t.Fatalf("租约持有者注册 = %+v, want 资源账登记 %+v", lease.State.Holder, lease.Ref.Lease)
		}

		settled, _, err := proj.Snapshot(f.ctx, query.ListFilter{Scope: scopeAll, States: []query.DetailState{{TaskStatus: task.StatusSettled}}})
		if err != nil {
			t.Fatalf("Snapshot 状态过滤: %v", err)
		}
		if len(settled.Details) != 1 || !reflect.DeepEqual(settled.Details[0].Ref, taskRef(f)) {
			t.Fatalf("settled 状态过滤 = %+v, want 仅终态 task-1", settled.Details)
		}
		phase, _, err := proj.Snapshot(f.ctx, query.ListFilter{Scope: scopeAll, States: []query.DetailState{{NoticePhase: query.NoticePhaseClaimed}}})
		if err != nil {
			t.Fatalf("Snapshot 相过滤: %v", err)
		}
		if len(phase.Details) != 1 || !reflect.DeepEqual(phase.Details[0].Ref, noticeRef(f, 1)) {
			t.Fatalf("claimed 相过滤 = %+v, want 仅 claimed 相通知 1", phase.Details)
		}

		derived := query.AgentCounts{}
		kinds := map[query.ObjectKind]int{}
		for _, d := range snap.Details {
			kinds[d.Ref.Kind]++
			if d.State.Agent.Configured {
				derived.Configured++
			}
			if d.State.Agent.Resident {
				derived.Resident++
			}
			if d.State.Agent.Busy {
				derived.Busy++
			}
		}
		if derived != (query.AgentCounts{Configured: 2, Resident: 3, Busy: 1}) {
			t.Fatalf("明细派生 agent 统计 = %+v, want configured 2, resident 3, busy 1", derived)
		}
		if kinds[query.ObjectAgent] != 3 || kinds[query.ObjectSession] != 3 || kinds[query.ObjectInvocation] != 2 || kinds[query.ObjectTask] != 2 || kinds[query.ObjectNotice] != 4 || kinds[query.ObjectLease] != 2 {
			t.Fatalf("明细派生分类统计 = %v, want 六维 3 3 2 2 4 2", kinds)
		}
		total := 0
		for _, n := range kinds {
			total += n
		}
		if total != len(snap.Details) {
			t.Fatalf("派生合计 = %d, want 明细条目数 %d", total, len(snap.Details))
		}
	})
}
