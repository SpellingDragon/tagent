// The 05.2 negative anchor of the single-snapshot budget: a bounded read
// materializes the snapshot objects it serves under a named object and byte
// bound, names what the bound leaves out instead of dropping it in silence,
// and keeps the paging semantics of the bounded view while doing so.
package inspection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// countingSource is the counting reading source of these tests: one frozen
// reading of synthetic task objects that counts the snapshot objects every
// request materializes beside the estimated bytes those objects hold, so the
// single-snapshot budget is provable against what a read actually built
// instead of what it answered with.
type countingSource struct {
	refs    []query.ObjectRef
	payload int
	built   int
	bytes   int
}

// newCountingSource returns a counting source holding count synthetic task
// objects under one authorized agent, each carrying payload bytes of blocked-
// record reason beside its state reading so its snapshot size is controllable.
func newCountingSource(count, payload int) *countingSource {
	src := &countingSource{payload: payload}
	for i := 0; i < count; i++ {
		src.refs = append(src.refs, query.ObjectRef{
			Kind:       query.ObjectTask,
			Address:    event.Address{AgentID: "agent-a", SessionID: "s-1"},
			InstanceID: "instance-1",
			TaskID:     event.TaskID(fmt.Sprintf("task-%03d", i)),
		})
	}
	return src
}

// Freeze answers with the counting detail source of this reading beside the
// fixed per-source readings the tests supply.
func (s *countingSource) Freeze(ctx context.Context) (query.DetailSource, []query.SourceWatermark, error) {
	return countingDetailSource{src: s}, nil, nil
}

var _ query.ReadingSource = (*countingSource)(nil)

// countingDetailSource is the frozen detail source of the counting source: it
// materializes one synthetic snapshot object per identity asked for and counts
// it beside its estimated size.
type countingDetailSource struct {
	src *countingSource
}

// Order returns the stable identity order of the synthetic objects.
func (c countingDetailSource) Order() []query.ObjectRef {
	return append([]query.ObjectRef(nil), c.src.refs...)
}

// DetailOf materializes one synthetic snapshot object and counts it beside its
// estimated size; an identity the reading does not hold answers as absent.
func (c countingDetailSource) DetailOf(ref query.ObjectRef) (query.Detail, bool) {
	found := false
	for _, r := range c.src.refs {
		if r == ref {
			found = true
			break
		}
	}
	if !found {
		return query.Detail{}, false
	}
	d := countingDetail(ref, c.src.payload)
	c.src.built++
	c.src.bytes += query.DetailBytes(d)
	return d, true
}

var _ query.DetailSource = countingDetailSource{}

// countingDetail builds one synthetic task snapshot object: the native task
// status beside one blocked record carrying payload bytes of readable reason,
// so the size of the object is deterministic and controllable.
func countingDetail(ref query.ObjectRef, payload int) query.Detail {
	return query.Detail{
		Ref:   ref,
		State: query.DetailState{TaskStatus: 1},
		Blocked: []query.ObligationProgress{{
			Obligation: task.BlockedObligation{
				Notice: task.Notice{TaskID: ref.TaskID, Round: 1, Kind: "handoff", Seq: 1},
				Reason: strings.Repeat("x", payload),
			},
			Advancer: query.AdvancerNoticeTransport,
			State:    query.AdvanceWaiting,
		}},
	}
}

// TestSnapshotPagesWithinByteBudget proves the single-snapshot object and byte budget of the bounded read faces.
// - 小页请求不物化全量: a small page materializes its own entries and never the whole view (counting source)
// - 对象上界严格: the snapshot objects one read materializes never exceed the object bound
// - 字节上界严格: the estimated bytes the held entries sum to never exceed the byte bound
// - 超预算具名截断: a truncated snapshot names what the budget left out, never a silent drop
// - 超预算具名拒绝: a page larger than the object budget is refused by name, never silently shortened
// - 分页语义不破: the pages of one revision concatenate into exactly its snapshot details, no duplication and no omission
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestSnapshotPagesWithinByteBudget(t *testing.T) {
	ctx := context.Background()
	filter := query.ListFilter{Scope: query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}}}
	total := 64

	t.Run("小页请求不物化全量", func(t *testing.T) {
		src := newCountingSource(total, 0)
		proj := query.NewBoundedProjection(withWatermarks{src: src, marks: testWatermarks()}, 8,
			query.Budget{MaxObjects: 4096, MaxBytes: 4 << 20})
		snap, cur, err := proj.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if len(snap.Details) != total {
			t.Fatalf("complete view holds %d entries, want %d", len(snap.Details), total)
		}
		src.built, src.bytes = 0, 0

		page, err := proj.List(snap.Revision, cur, filter, query.ObjectRef{}, 3)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page) != 3 {
			t.Fatalf("first page holds %d entries, want 3", len(page))
		}
		if src.built >= total {
			t.Fatalf("small page materialized %d objects, want fewer than the %d-object view", src.built, total)
		}
		if src.built > 3 {
			t.Fatalf("small page materialized %d objects for 3 entries, want at most 3", src.built)
		}
		if got := detailBytesOf(page); got > 4<<20 {
			t.Fatalf("small page holds %d estimated bytes, want within the byte bound", got)
		}

		src.built, src.bytes = 0, 0
		next, err := proj.List(snap.Revision, cur, filter, page[2].Ref, 3)
		if err != nil {
			t.Fatalf("List second page: %v", err)
		}
		if len(next) != 3 || !reflect.DeepEqual(next, snap.Details[3:6]) {
			t.Fatalf("second page does not continue the stable sequence: %v", next)
		}
		if src.built >= total {
			t.Fatalf("second page materialized %d objects, want fewer than the %d-object view", src.built, total)
		}
		if src.built > 6 {
			t.Fatalf("second page materialized %d objects, want at most the walked prefix of 6", src.built)
		}
	})

	t.Run("超预算对象界具名截断并拒绝超界请求", func(t *testing.T) {
		src := newCountingSource(total, 0)
		proj := query.NewBoundedProjection(withWatermarks{src: src, marks: testWatermarks()}, 8,
			query.Budget{MaxObjects: 4})
		snap, cur, err := proj.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if len(snap.Details) != 4 {
			t.Fatalf("bounded view holds %d entries, want 4", len(snap.Details))
		}
		if src.built > 4 {
			t.Fatalf("read materialized %d objects, want at most the 4-object bound", src.built)
		}
		if snap.Truncated.OmittedObjects != total-4 {
			t.Fatalf("truncation names %d omitted, want %d", snap.Truncated.OmittedObjects, total-4)
		}
		if _, err := proj.List(snap.Revision, cur, filter, query.ObjectRef{}, 5); !errors.Is(err, query.ErrSnapshotBudgetExceeded) {
			t.Fatalf("oversized page refused with %v, want ErrSnapshotBudgetExceeded", err)
		}
		page, err := proj.List(snap.Revision, cur, filter, query.ObjectRef{}, 4)
		if err != nil {
			t.Fatalf("page within the object bound: %v", err)
		}
		if len(page) != 4 {
			t.Fatalf("page within the object bound holds %d entries, want 4", len(page))
		}
	})

	t.Run("超预算字节界具名截断", func(t *testing.T) {
		payload := 200
		single := query.DetailBytes(countingDetail(query.ObjectRef{
			Kind:       query.ObjectTask,
			Address:    event.Address{AgentID: "agent-a", SessionID: "s-1"},
			InstanceID: "instance-1",
			TaskID:     "task-000",
		}, payload))
		maxBytes := 3 * single
		src := newCountingSource(total, payload)
		proj := query.NewBoundedProjection(withWatermarks{src: src, marks: testWatermarks()}, 8,
			query.Budget{MaxBytes: maxBytes})
		snap, cur, err := proj.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if len(snap.Details) != 3 {
			t.Fatalf("byte-bounded view holds %d entries, want 3", len(snap.Details))
		}
		if got := detailBytesOf(snap.Details); got > maxBytes {
			t.Fatalf("held entries sum to %d estimated bytes, want within the %d-byte bound", got, maxBytes)
		}
		if src.bytes > maxBytes+single {
			t.Fatalf("read materialized %d estimated bytes, want at most the bound plus one boundary probe", src.bytes)
		}
		if snap.Truncated.OmittedObjects != total-3 {
			t.Fatalf("truncation names %d omitted, want %d", snap.Truncated.OmittedObjects, total-3)
		}
		if _, err := proj.List(snap.Revision, cur, filter, query.ObjectRef{}, 4); err != nil {
			t.Fatalf("page of 4 entries within the object bound: %v", err)
		}
	})

	t.Run("截断视图下分页语义不破", func(t *testing.T) {
		src := newCountingSource(total, 0)
		proj := query.NewBoundedProjection(withWatermarks{src: src, marks: testWatermarks()}, 8,
			query.Budget{MaxObjects: 5})
		snap, cur, err := proj.Snapshot(ctx, filter)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if snap.Truncated.OmittedObjects == 0 {
			t.Fatalf("truncated view reports no omission, want a named truncation")
		}
		var stitched []query.Detail
		after := query.ObjectRef{}
		for {
			page, err := proj.List(snap.Revision, cur, filter, after, 2)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page) == 0 {
				break
			}
			stitched = append(stitched, page...)
			after = page[len(page)-1].Ref
		}
		if !reflect.DeepEqual(stitched, snap.Details) {
			t.Fatalf("pages stitch to %d entries, want exactly the %d snapshot entries", len(stitched), len(snap.Details))
		}
		again, err := proj.List(snap.Revision, cur, filter, query.ObjectRef{}, 2)
		if err != nil {
			t.Fatalf("List again: %v", err)
		}
		if !reflect.DeepEqual(again, snap.Details[:2]) {
			t.Fatalf("repeated page is not stable within one revision")
		}
	})

	t.Run("生产读源受同一预算边界且页拼接一致", func(t *testing.T) {
		f := newRuntimeFixture(t)
		proj := query.NewBoundedProjection(withWatermarks{src: query.NewInventorySource(f.inventory), marks: testWatermarks()}, 8,
			query.Budget{MaxObjects: 2})
		snap, cur, err := proj.Snapshot(f.ctx, filterAll)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if len(snap.Details) > 2 {
			t.Fatalf("bounded production view holds %d entries, want at most 2", len(snap.Details))
		}
		if snap.Truncated.OmittedObjects == 0 {
			t.Fatalf("bounded production view reports no omission, want a named truncation")
		}
		var stitched []query.Detail
		after := query.ObjectRef{}
		for {
			page, err := proj.List(snap.Revision, cur, filterAll, after, 1)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page) == 0 {
				break
			}
			stitched = append(stitched, page...)
			after = page[len(page)-1].Ref
		}
		if !reflect.DeepEqual(stitched, snap.Details) {
			t.Fatalf("production pages stitch to %d entries, want exactly the %d snapshot entries", len(stitched), len(snap.Details))
		}
	})
}

// detailBytesOf sums the estimated snapshot size of a detail list with the
// same estimator the byte bound is checked against.
func detailBytesOf(list []query.Detail) int {
	total := 0
	for _, d := range list {
		total += query.DetailBytes(d)
	}
	return total
}
