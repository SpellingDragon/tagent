package acceptance

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// scenarioSite names one established session site of the query drive.
type scenarioSite struct {
	addr     event.Address
	instance event.InstanceID
}

// scenarioReading is the inventory read port of the query drive: the agents its
// published generation configures, the session sites the state owner holds and
// the tasks the task owners hold.
type scenarioReading struct {
	agents   []event.AgentID
	store    *state.MemoryStore
	subject  state.Subject
	sites    []scenarioSite
	slots    *state.Occupancy
	durable  *state.DurableCommit
	registry *task.MemoryRegistry
	manager  *task.MemoryManager
}

// Published reports the generation the drive publishes.
func (r scenarioReading) Published(ctx context.Context) (query.Generation, error) {
	return query.Generation{Agents: r.agents}, nil
}

// Sites reports one owner snapshot per established site beside its slot.
func (r scenarioReading) Sites(ctx context.Context) ([]query.SiteReading, error) {
	out := make([]query.SiteReading, 0, len(r.sites))
	for _, site := range r.sites {
		session, err := r.store.Site(ctx, r.subject, site.addr, site.instance)
		if err != nil {
			return nil, err
		}
		reading := query.SiteReading{Session: *session}
		reading.Invocation, reading.Decision, reading.Held = r.slots.Slot(site.addr, site.instance)
		out = append(out, reading)
	}
	return out, nil
}

// Tasks reports every task the commit carrier lists beside its obligations.
func (r scenarioReading) Tasks(ctx context.Context) ([]query.TaskReading, error) {
	ids := r.durable.TaskIDs()
	out := make([]query.TaskReading, 0, len(ids))
	for _, id := range ids {
		spec, status, err := r.registry.Lookup(ctx, id)
		if err != nil {
			return nil, err
		}
		obligations, err := r.manager.Obligations(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, query.TaskReading{
			TaskID:      id,
			Spec:        spec,
			Status:      status,
			Obligations: obligations,
			Outbox:      r.durable.Outbox(id),
		})
	}
	return out, nil
}

// scenarioBlockedTask admits and settles one task whose only notification is
// owed to an instance that closed, so its delivery obligation is blocked beside
// the reason its owner recorded.
func scenarioBlockedTask(t *testing.T, durable *state.DurableCommit, registry *task.MemoryRegistry, manager *task.MemoryManager, source, target event.Address) (event.TaskID, string) {
	t.Helper()
	ctx := context.Background()
	const closedInstance event.InstanceID = "instance-closed"
	spec := task.Spec{
		CallID:       "call-a",
		DedupKey:     task.DedupKey("instance-closed\x00inv-a\x00decision-a\x00call-a"),
		InvocationID: event.InvocationID("inv-a"),
		InstanceID:   closedInstance,
		Description:  "the reviewed work waits for its notification",
		Source:       source,
		Target:       target,
		Execution:    "serialized",
	}
	receipt, err := registry.Register(ctx, spec)
	if err != nil {
		t.Fatalf("register the blocked task: %v", err)
	}
	if err := durable.Declare(ctx, receipt.TaskID, spec); err != nil {
		t.Fatalf("declare the blocked task: %v", err)
	}
	if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusRunning, "run-a", nil); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance the blocked task = (%v, %v), want the task running", result, err)
	}
	envelope, err := registry.IssueNotice(ctx, receipt.TaskID, task.NoticeSettled, "evt-a")
	if err != nil {
		t.Fatalf("issue the blocked notification: %v", err)
	}
	if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, "settle-a", []task.NoticeEnvelope{envelope}); result != task.CommitCommitted || err != nil {
		t.Fatalf("settle the blocked task = (%v, %v), want the settle committed", result, err)
	}
	if err := manager.CloseInstance(ctx, closedInstance); err != nil {
		t.Fatalf("close the target instance: %v", err)
	}
	if err := manager.Flush(ctx, receipt.TaskID); err != nil {
		t.Fatalf("flush the owed notification: %v", err)
	}
	open, err := manager.Obligations(ctx, receipt.TaskID)
	if err != nil {
		t.Fatalf("read the blocked obligations: %v", err)
	}
	reason := ""
	if len(open.Blocked) > 0 {
		reason = open.Blocked[0].Reason
	}
	return receipt.TaskID, reason
}

// TestScenario08QueryWithoutOtel drives the read-only inspection faces over a real runtime state with no telemetry provider wired.
// - every read succeeds, and the reported statistics are derived from the detail they accompany
// - a cursor stays bound to its revision, and a window that left the bounded read is named
// - an unbridgeable backlog is a named gap beside a reset, never a silently truncated batch
// - the authorization filter keeps unauthorized objects out
// - the blockers are explained one row at a time beside their next advancer
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestScenario08QueryWithoutOtel(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S08")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	const instance event.InstanceID = "instance-1"
	addrA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("open the storage tier: %v", err)
	}
	durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("open the durable commit: %v", err)
	}
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	authority := state.NewAuthority()
	store := state.NewMemoryStore(authority)
	const inspector state.Subject = "inspector"
	sites := []scenarioSite{{addr: addrA, instance: instance}, {addr: addrB, instance: instance}}
	for _, site := range sites {
		if _, err := store.Establish(ctx, site.addr, site.instance); err != nil {
			t.Fatalf("establish the site of %s: %v", site.addr.AgentID, err)
		}
		if err := authority.GrantRead(ctx, inspector, site.addr); err != nil {
			t.Fatalf("grant the read of %s: %v", site.addr.AgentID, err)
		}
	}
	slots := state.NewOccupancy()
	if err := slots.Occupy(addrA, instance, "inv-a", "decision-a"); err != nil {
		t.Fatalf("occupy the execution slot: %v", err)
	}
	manager := task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		return task.DeliveryPending, nil
	})
	taskID, blockedReason := scenarioBlockedTask(t, durable, registry, manager, addrA, addrB)
	refs = append(refs, string(taskID))

	inventory := query.NewInventory(scenarioReading{
		agents:   []event.AgentID{addrA.AgentID, addrB.AgentID},
		store:    store,
		subject:  inspector,
		sites:    sites,
		slots:    slots,
		durable:  durable,
		registry: registry,
		manager:  manager,
	})
	view, viewErr := inventory.View(ctx)
	if viewErr != nil {
		t.Fatalf("read the runtime inventory: %v", viewErr)
	}
	explainer := query.NewExplainer(inventory, nil)
	report, explainErr := explainer.Explain(ctx, query.ListFilter{Scope: query.AuthorizationScope{Agents: []event.AgentID{addrA.AgentID, addrB.AgentID}}})
	if explainErr != nil {
		t.Fatalf("explain the runtime blockers: %v", explainErr)
	}
	projection := query.NewProjection(inventory, 8)
	snapshot, cursor, snapshotErr := projection.Snapshot(ctx, query.ListFilter{Scope: query.AuthorizationScope{Agents: []event.AgentID{addrA.AgentID, addrB.AgentID}}})
	if snapshotErr != nil {
		t.Fatalf("read the runtime snapshot: %v", snapshotErr)
	}
	observations["query_ok_with_otel_disabled"] = viewErr == nil && explainErr == nil && snapshotErr == nil

	counts := view.Counts()
	derived := map[string]any{
		"configured": counts.Agents.Configured,
		"resident":   counts.Agents.Resident,
		"busy":       counts.Agents.Busy,
		"blocked":    report.Counts.Blockers,
	}
	observations["derived_counts"] = derived
	fromDetail := 0
	for _, entry := range view.Agents() {
		if entry.Configured {
			fromDetail++
		}
	}
	observations["statistics_derived_from_detail"] = counts.Agents.Configured == fromDetail &&
		counts.Agents.Resident == len(view.Sessions()) &&
		report.Counts.Blockers == len(report.Blockers)

	advancing := 0
	rows := []map[string]any{}
	labels := newScenarioLabels()
	for _, blocker := range report.Blockers {
		if blocker.Advancer != "" {
			advancing++
		}
		row := map[string]any{
			"object":   labels.label("task", string(blocker.Ref.TaskID)),
			"owner":    string(blocker.Ref.Address.SessionID),
			"blocker":  blocker.Reason,
			"advancer": string(blocker.Advancer),
		}
		rows = append(rows, row)
	}
	observations["blockers_with_next_advancer"] = advancing
	observations["blocker_detail_rows"] = rows
	observations["blocker_statistic_matches_detail"] = report.Counts.Blockers == len(report.Blockers) &&
		(len(report.Blockers) == 0 || blockedReason != "")

	filtered, _, err := projection.Snapshot(ctx, query.ListFilter{Scope: query.AuthorizationScope{Agents: []event.AgentID{addrA.AgentID}}})
	if err != nil {
		t.Fatalf("read the authorized snapshot: %v", err)
	}
	normalized := len(filtered.Details) > 0
	for _, detail := range filtered.Details {
		if detail.Ref.Address.AgentID != addrA.AgentID {
			normalized = false
		}
	}
	observations["authorization_filtered_detail_normalized"] = normalized

	all := query.ListFilter{Scope: query.AuthorizationScope{Agents: []event.AgentID{addrA.AgentID, addrB.AgentID}}}
	continued, err := projection.List(snapshot.Revision, cursor, all, query.ObjectRef{}, 1)
	cursorOK := err == nil && len(continued) == 1
	advanced, _, err := projection.Snapshot(ctx, all)
	if err != nil {
		t.Fatalf("read the advanced snapshot: %v", err)
	}
	_, replayErr := projection.List(advanced.Revision, cursor, all, query.ObjectRef{}, 1)
	observations["cursor_revision_bound"] = cursorOK && errors.Is(replayErr, query.ErrCursorRevisionMismatch)

	narrow := query.NewProjection(inventory, 1)
	first, firstCursor, err := narrow.Snapshot(ctx, all)
	if err != nil {
		t.Fatalf("read the narrow snapshot: %v", err)
	}
	if _, _, err := narrow.Snapshot(ctx, all); err != nil {
		t.Fatalf("read the closing snapshot: %v", err)
	}
	_, expiredErr := narrow.List(first.Revision, firstCursor, all, query.ObjectRef{}, 1)
	observations["expired_window_explained"] = errors.Is(expiredErr, query.ErrCursorWindowExpired)

	for i := 0; i < 2; i++ {
		if _, _, err := projection.Snapshot(ctx, all); err != nil {
			t.Fatalf("read the following snapshot: %v", err)
		}
	}
	_, updates, err := projection.Follow(ctx, query.Subscription{Revision: snapshot.Revision, Filter: all}, 0)
	if err != nil {
		t.Fatalf("follow the subscription: %v", err)
	}
	gapped, reset := false, false
	for _, update := range updates {
		if update.Op == query.UpdateGap && update.Gap.Reason == query.GapOverflow {
			gapped = true
		}
		if update.Op == query.UpdateReset {
			reset = true
		}
	}
	observations["subscription_overflow_reset_explained"] = gapped && reset

	EmitScenarioRecord(t, scenario, refs, observations)
}
