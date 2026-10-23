// The 05.2 acceptance test of the bounded read faces of the query package over
// a real runtime assembly.
package inspection

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// withWatermarks is the reading source of these tests: it freezes the source it
// wraps and reports one fixed per-source reading beside it, so a reset payload
// is checkable against values the test knows.
type withWatermarks struct {
	src   query.ReadingSource
	marks []query.SourceWatermark
}

// Freeze freezes the wrapped source beside the fixed per-source readings.
func (s withWatermarks) Freeze(ctx context.Context) (query.DetailSource, []query.SourceWatermark, error) {
	frozen, _, err := s.src.Freeze(ctx)
	if err != nil {
		return nil, nil, err
	}
	return frozen, append([]query.SourceWatermark(nil), s.marks...), nil
}

var _ query.ReadingSource = withWatermarks{}

// newTestProjection returns the bounded read service of one inventory under the
// fixed per-source readings of these tests.
func newTestProjection(inventory *query.Inventory, window int) *query.Projection {
	return query.NewBoundedProjection(withWatermarks{src: query.NewInventorySource(inventory), marks: testWatermarks()}, window, query.DefaultBudget)
}

// testWatermarks returns the fixed per-source readings these tests report: one
// caught-up source and one source with a measured lag.
func testWatermarks() []query.SourceWatermark {
	return []query.SourceWatermark{
		{Source: "task-log", Position: "pos-9", LagState: query.WatermarkCaughtUp},
		{
			Source:   "mailbox",
			Position: "pos-4",
			LagState: query.WatermarkLagKnown,
			Lag:      query.SourceLag{Amount: 2, Unit: "revisions", Origin: "source-owner"},
		},
	}
}

// cloneDetails copies a detail list, blocked records included, so a frozen
// expectation is unreachable through the value it was copied from.
func cloneDetails(list []query.Detail) []query.Detail {
	out := make([]query.Detail, len(list))
	copy(out, list)
	for i := range out {
		if len(out[i].Blocked) > 0 {
			out[i].Blocked = append([]query.ObligationProgress(nil), out[i].Blocked...)
		}
	}
	return out
}

// findDetail locates one detail entry by the reference it names.
func findDetail(list []query.Detail, ref query.ObjectRef) (query.Detail, bool) {
	for _, d := range list {
		if reflect.DeepEqual(d.Ref, ref) {
			return d, true
		}
	}
	return query.Detail{}, false
}

// findLease locates one resource lease entry by the resource it names, never
// by the registration reference: the resource is one object and the release
// reference is content beside it.
func findLease(list []query.Detail, kind, key string) (query.Detail, bool) {
	for _, d := range list {
		if d.Ref.Kind == query.ObjectLease && d.Ref.Lease.Kind == kind && d.Ref.Lease.Key == key {
			return d, true
		}
	}
	return query.Detail{}, false
}

// agentRef names one agent object.
func agentRef(id event.AgentID) query.ObjectRef {
	return query.ObjectRef{Kind: query.ObjectAgent, Address: event.Address{AgentID: id}}
}

// sessionRef names one session site of the fixture.
func sessionRef(f *runtimeFixture, addr event.Address) query.ObjectRef {
	return query.ObjectRef{Kind: query.ObjectSession, Address: addr, InstanceID: f.instance}
}

// taskRef names the first task of the fixture.
func taskRef(f *runtimeFixture) query.ObjectRef {
	return query.ObjectRef{Kind: query.ObjectTask, Address: f.spec1.Source, InstanceID: f.spec1.InstanceID, TaskID: f.task1}
}

// noticeRef names one notification of the first task of the fixture by its
// acceptance index in the fixture's notice list.
func noticeRef(f *runtimeFixture, seq int) query.ObjectRef {
	return query.ObjectRef{Kind: query.ObjectNotice, Address: f.addrB, InstanceID: f.instance, Notice: f.notices[seq]}
}

// scopeAll, scopeA, filterAll, filterA and filterAccepted name the scopes and
// filters the sub-cases read under.
var (
	scopeAll       = query.AuthorizationScope{Agents: []event.AgentID{"agent-a", "agent-b", "agent-c"}}
	scopeA         = query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}}
	filterAll      = query.ListFilter{Scope: scopeAll}
	filterA        = query.ListFilter{Scope: scopeA}
	filterAccepted = query.ListFilter{Scope: scopeAll, States: []query.DetailState{{NoticePhase: query.NoticePhaseAccepted}}}
)

// TestQueryAuthorizationAndCursor proves the authorization-filtered read faces of the bounded projection.
// - 授权过滤: unauthorized entries never enter a snapshot and an empty scope reads nothing
// - 不可变快照: a constructed snapshot never follows its sources and admits no write-through
// - 稳定分页: paging holds one stable sequence per revision and names an expired window
// - 详情读面: a detail read names a scope refusal apart from an absent object
// - 订阅 gap/reset: an unbridgeable range is a named gap beside a named reset, never a silent skip
// - 游标绑定: a cursor follows one revision under one scope and one filter, and every divergence is named
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestQueryAuthorizationAndCursor(t *testing.T) {
	t.Run("S1 授权过滤: unauthorized entries never enter the snapshot", func(t *testing.T) {
		f := newRuntimeFixture(t)
		proj := newTestProjection(f.inventory, 8)

		snap, _, _ := proj.Snapshot(f.ctx, filterA)
		outside := []query.ObjectRef{
			agentRef("agent-b"),
			agentRef("agent-c"),
			sessionRef(f, f.addrB),
			sessionRef(f, f.addrC),
			noticeRef(f, 0),
			noticeRef(f, 3),
			{Kind: query.ObjectTask, Address: f.spec2.Source, InstanceID: f.spec2.InstanceID, TaskID: f.task2},
		}
		for _, ref := range outside {
			if _, ok := findDetail(snap.Details, ref); ok {
				t.Fatalf("越权条目进入快照: %+v", ref)
			}
		}
		for _, ref := range []query.ObjectRef{agentRef("agent-a"), sessionRef(f, f.addrA), taskRef(f)} {
			if _, ok := findDetail(snap.Details, ref); !ok {
				t.Fatalf("授权内条目缺席快照: %+v", ref)
			}
		}
		if _, ok := findLease(snap.Details, "model", "quota-a"); !ok {
			t.Fatalf("非空授权下资源租约缺席快照")
		}
		for _, d := range snap.Details {
			if d.Ref.Kind == query.ObjectLease {
				continue
			}
			if d.Ref.Address.AgentID != "agent-a" {
				t.Fatalf("越权条目进入快照: %+v", d.Ref)
			}
		}

		snapEmpty, _, _ := proj.Snapshot(f.ctx, query.ListFilter{})
		if len(snapEmpty.Details) != 0 {
			t.Fatalf("空授权读到明细: %+v", snapEmpty.Details)
		}

		snapFull, _, _ := proj.Snapshot(f.ctx, filterAll)
		for _, ref := range []query.ObjectRef{agentRef("agent-b"), noticeRef(f, 0), noticeRef(f, 3)} {
			if _, ok := findDetail(snapFull.Details, ref); !ok {
				t.Fatalf("全量对照中越权条目缺席: %+v", ref)
			}
		}
		if len(snapFull.Details) <= len(snap.Details) {
			t.Fatalf("全量对照不比窄授权宽: %d <= %d", len(snapFull.Details), len(snap.Details))
		}
	})

	t.Run("S2 不可变快照: a constructed snapshot never follows its sources", func(t *testing.T) {
		f := newRuntimeFixture(t)
		proj := newTestProjection(f.inventory, 8)
		snap, cur, _ := proj.Snapshot(f.ctx, filterAll)
		frozen := cloneDetails(snap.Details)
		frozenMarks := append([]query.SourceWatermark(nil), snap.Watermarks...)

		f.leases.Register("storage", "slot-b", nil)
		snap2, _, _ := proj.Snapshot(f.ctx, filterAll)

		if !reflect.DeepEqual(snap.Details, frozen) {
			t.Fatalf("快照随源变: %+v, want %+v", snap.Details, frozen)
		}
		if !reflect.DeepEqual(snap.Watermarks, frozenMarks) {
			t.Fatalf("水位随源变: %+v, want %+v", snap.Watermarks, frozenMarks)
		}
		before, _ := findLease(frozen, "storage", "slot-b")
		after, ok := findLease(snap2.Details, "storage", "slot-b")
		if !ok || reflect.DeepEqual(before, after) {
			t.Fatalf("新快照未反映源变: before=%+v after=%+v", before, after)
		}

		snap.Details[0].State = query.DetailState{Decision: "tampered"}
		if len(snap.Details[0].Blocked) > 0 {
			snap.Details[0].Blocked[0].State = "tampered"
		}
		got, err := proj.Detail(snap.Revision, cur, filterAll, frozen[0].Ref)
		if err != nil {
			t.Fatalf("Detail: %v", err)
		}
		if !reflect.DeepEqual(got, frozen[0]) {
			t.Fatalf("写穿透入冻结读代: %+v, want %+v", got, frozen[0])
		}
	})

	t.Run("S3 稳定分页: paging holds one stable sequence per revision", func(t *testing.T) {
		f := newRuntimeFixture(t)
		proj := newTestProjection(f.inventory, 8)
		snap, cur, _ := proj.Snapshot(f.ctx, filterAll)

		f.leases.Register("model", "quota-a", nil)
		if _, _, err := proj.Snapshot(f.ctx, filterAll); err != nil {
			t.Fatalf("Snapshot: %v", err)
		}

		var merged []query.Detail
		after := query.ObjectRef{}
		for {
			page, err := proj.List(snap.Revision, cur, filterAll, after, 3)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page) == 0 {
				break
			}
			merged = append(merged, page...)
			after = page[len(page)-1].Ref
			if len(page) < 3 {
				break
			}
		}
		if !reflect.DeepEqual(merged, snap.Details) {
			t.Fatalf("翻页序列不稳定: %d 页拼接与快照不一致", len(merged))
		}

		var again []query.Detail
		after = query.ObjectRef{}
		for {
			page, err := proj.List(snap.Revision, cur, filterAll, after, 3)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page) == 0 {
				break
			}
			again = append(again, page...)
			after = page[len(page)-1].Ref
			if len(page) < 3 {
				break
			}
		}
		if !reflect.DeepEqual(again, merged) {
			t.Fatalf("同 revision 两次翻页序列不一致")
		}

		projNarrow := newTestProjection(f.inventory, 1)
		snapOld, curOld, _ := projNarrow.Snapshot(f.ctx, filterAll)
		if _, _, err := projNarrow.Snapshot(f.ctx, filterAll); err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if _, err := projNarrow.List(snapOld.Revision, curOld, filterAll, query.ObjectRef{}, 3); !errors.Is(err, query.ErrCursorWindowExpired) {
			t.Fatalf("过期游标 = %v, want ErrCursorWindowExpired", err)
		}
	})

	t.Run("S4 详情读面: a detail read names a scope refusal apart from an absence", func(t *testing.T) {
		f := newRuntimeFixture(t)
		proj := newTestProjection(f.inventory, 8)
		snap, cur, _ := proj.Snapshot(f.ctx, filterAll)

		ref := taskRef(f)
		got, err := proj.Detail(snap.Revision, cur, filterAll, ref)
		if err != nil {
			t.Fatalf("Detail: %v", err)
		}
		want, ok := findDetail(snap.Details, ref)
		if !ok {
			t.Fatalf("任务缺席快照: %+v", ref)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("详情与快照条目不一致: %+v, want %+v", got, want)
		}
		if got.State.TaskStatus != task.StatusSettled {
			t.Fatalf("任务状态 = %+v, want 原生 settled 读数（阻塞独立在 Blocked 列表，不压成 blocked 单值）", got.State)
		}
		if len(got.Blocked) != 1 {
			t.Fatalf("阻塞义务 = %d 条, want 1 条", len(got.Blocked))
		}
		blocked := got.Blocked[0]
		if blocked.Advancer != query.AdvancerNoticeTransport || blocked.State != query.AdvanceWaiting {
			t.Fatalf("推进者 = %+v, want notice-transport/waiting", blocked)
		}
		if !reflect.DeepEqual(blocked.Obligation.Notice, f.notices[3]) {
			t.Fatalf("阻塞义务通知 = %+v, want 被拒投递的 %+v", blocked.Obligation.Notice, f.notices[3])
		}
		if blocked.Obligation.Reason == "" {
			t.Fatalf("阻塞义务缺可读原因")
		}

		snapA, curA, err := proj.Snapshot(f.ctx, filterA)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if _, err := proj.Detail(snapA.Revision, curA, filterA, sessionRef(f, f.addrB)); !errors.Is(err, query.ErrDetailOutsideScope) {
			t.Fatalf("越权详情 = %v, want ErrDetailOutsideScope", err)
		}
		absent := query.ObjectRef{Kind: query.ObjectTask, Address: f.addrA, InstanceID: f.instance, TaskID: "task-absent"}
		if _, err := proj.Detail(snapA.Revision, curA, filterA, absent); !errors.Is(err, query.ErrDetailAbsent) {
			t.Fatalf("缺席详情 = %v, want ErrDetailAbsent", err)
		}

		filterAgents := query.ListFilter{Scope: scopeAll, Kinds: []query.ObjectKind{query.ObjectAgent}}
		snapG, curG, err := proj.Snapshot(f.ctx, filterAgents)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if _, err := proj.Detail(snapG.Revision, curG, filterAgents, ref); !errors.Is(err, query.ErrDetailAbsent) {
			t.Fatalf("窄化视图外详情 = %v, want ErrDetailAbsent", err)
		}
	})

	t.Run("S5 订阅 gap/reset: an unbridgeable range is named, never silently skipped", func(t *testing.T) {
		t.Run("增量透传: increments arrive named and scoped", func(t *testing.T) {
			f := newRuntimeFixture(t)
			proj := newTestProjection(f.inventory, 8)
			snap1, _, _ := proj.Snapshot(f.ctx, filterAll)
			f.leases.Register("storage", "slot-b", nil)
			snap2, _, _ := proj.Snapshot(f.ctx, filterAll)

			sub := query.Subscription{Revision: snap1.Revision, Filter: filterAll}
			next, ups, err := proj.Follow(f.ctx, sub, 100)
			if err != nil {
				t.Fatalf("Follow: %v", err)
			}
			if len(ups) != 1 {
				t.Fatalf("增量 = %d 条, want 1 条: %+v", len(ups), ups)
			}
			if ups[0].Op != query.UpdateUpsert {
				t.Fatalf("增量操作 = %q, want upsert", ups[0].Op)
			}
			if ups[0].Revision != snap2.Revision {
				t.Fatalf("增量修订 = %d, want %d", ups[0].Revision, snap2.Revision)
			}
			want, ok := findLease(snap2.Details, "storage", "slot-b")
			if !ok || !reflect.DeepEqual(ups[0].Detail, want) || !reflect.DeepEqual(ups[0].Ref, want.Ref) {
				t.Fatalf("增量内容 = %+v, want %+v", ups[0], want)
			}
			if next.Revision != snap2.Revision {
				t.Fatalf("续读位置 = %d, want %d", next.Revision, snap2.Revision)
			}

			subEmpty := query.Subscription{Revision: snap1.Revision, Filter: query.ListFilter{}}
			nextEmpty, upsEmpty, err := proj.Follow(f.ctx, subEmpty, 100)
			if err != nil {
				t.Fatalf("Follow: %v", err)
			}
			if len(upsEmpty) != 0 {
				t.Fatalf("空授权流 = %+v, want 空批", upsEmpty)
			}
			if nextEmpty.Revision != snap2.Revision {
				t.Fatalf("空授权流位置 = %d, want %d", nextEmpty.Revision, snap2.Revision)
			}
		})

		t.Run("溢出具名 reset: overflow names the missing range and re-anchors", func(t *testing.T) {
			f := newRuntimeFixture(t)
			proj := newTestProjection(f.inventory, 8)
			snap1, _, _ := proj.Snapshot(f.ctx, filterAll)
			var snap4 query.Snapshot
			for i := 0; i < 3; i++ {
				s, _, err := proj.Snapshot(f.ctx, filterAll)
				if err != nil {
					t.Fatalf("Snapshot: %v", err)
				}
				snap4 = s
			}

			sub := query.Subscription{Revision: snap1.Revision, Filter: filterAll}
			next, ups, err := proj.Follow(f.ctx, sub, 1)
			if err != nil {
				t.Fatalf("Follow: %v", err)
			}
			if len(ups) != 2 {
				t.Fatalf("溢出批 = %d 条, want gap+reset: %+v", len(ups), ups)
			}
			gap, reset := ups[0], ups[1]
			if gap.Op != query.UpdateGap || reset.Op != query.UpdateReset {
				t.Fatalf("溢出批次序 = %q,%q, want gap 先 reset 后", gap.Op, reset.Op)
			}
			if gap.Gap != (query.Gap{From: 2, To: 4, Reason: query.GapOverflow}) {
				t.Fatalf("缺口范围 = %+v, want {2 4 overflow}", gap.Gap)
			}
			if gap.Gap.To-gap.Gap.From+1 != 3 {
				t.Fatalf("缺口未覆盖 3 修订: %+v", gap.Gap)
			}
			if gap.Revision != snap4.Revision || reset.Revision != snap4.Revision {
				t.Fatalf("缺口锚点 = %d,%d, want %d", gap.Revision, reset.Revision, snap4.Revision)
			}
			if !reflect.DeepEqual(reset.Reset.Watermarks, snap4.Watermarks) {
				t.Fatalf("重锚水位 = %+v, want %+v", reset.Reset.Watermarks, snap4.Watermarks)
			}
			if !reflect.DeepEqual(next, sub) {
				t.Fatalf("位置跨越 reset 前进: %+v, want %+v", next, sub)
			}

			f.leases.Register("model", "quota-a", nil)
			snap5, _, err := proj.Snapshot(f.ctx, filterAll)
			if err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			recovered := query.Subscription{Revision: snap4.Revision, Filter: filterAll}
			next5, ups5, err := proj.Follow(f.ctx, recovered, 100)
			if err != nil {
				t.Fatalf("Follow: %v", err)
			}
			if len(ups5) != 1 || ups5[0].Op != query.UpdateUpsert {
				t.Fatalf("重锚后增量 = %+v, want 1 upsert", ups5)
			}
			if next5.Revision != snap5.Revision {
				t.Fatalf("重锚后位置 = %d, want %d", next5.Revision, snap5.Revision)
			}
		})

		t.Run("窗口过期: an expired window is named even under a generous bound", func(t *testing.T) {
			f := newRuntimeFixture(t)
			proj := newTestProjection(f.inventory, 2)
			snapOld, _, _ := proj.Snapshot(f.ctx, filterAll)
			var snap3 query.Snapshot
			for i := 0; i < 2; i++ {
				s, _, err := proj.Snapshot(f.ctx, filterAll)
				if err != nil {
					t.Fatalf("Snapshot: %v", err)
				}
				snap3 = s
			}

			sub := query.Subscription{Revision: snapOld.Revision, Filter: filterAll}
			next, ups, err := proj.Follow(f.ctx, sub, 100)
			if err != nil {
				t.Fatalf("Follow: %v", err)
			}
			if len(ups) != 2 {
				t.Fatalf("过期批 = %d 条, want gap+reset: %+v", len(ups), ups)
			}
			if ups[0].Op != query.UpdateGap || ups[1].Op != query.UpdateReset {
				t.Fatalf("过期批次序 = %q,%q, want gap 先 reset 后", ups[0].Op, ups[1].Op)
			}
			if ups[0].Gap != (query.Gap{From: 2, To: 3, Reason: query.GapWindowExpired}) {
				t.Fatalf("过期缺口 = %+v, want {2 3 window-expired}", ups[0].Gap)
			}
			if !reflect.DeepEqual(ups[1].Reset.Watermarks, snap3.Watermarks) {
				t.Fatalf("重锚水位 = %+v, want %+v", ups[1].Reset.Watermarks, snap3.Watermarks)
			}
			if !reflect.DeepEqual(next, sub) {
				t.Fatalf("位置跨越 reset 前进: %+v, want %+v", next, sub)
			}
		})

		t.Run("多消费者独立: one consumer's gap is another consumer's batch", func(t *testing.T) {
			f := newRuntimeFixture(t)
			proj := newTestProjection(f.inventory, 8)
			snap1, _, _ := proj.Snapshot(f.ctx, filterAll)
			f.leases.Register("model", "quota-a", nil)
			if _, _, err := proj.Snapshot(f.ctx, filterAll); err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			f.leases.Register("storage", "slot-b", nil)
			snap3, _, err := proj.Snapshot(f.ctx, filterAll)
			if err != nil {
				t.Fatalf("Snapshot: %v", err)
			}

			subA := query.Subscription{Revision: snap1.Revision, Filter: filterAll}
			subB := query.Subscription{Revision: snap1.Revision, Filter: filterAll}
			nextA, upsA, err := proj.Follow(f.ctx, subA, 100)
			if err != nil {
				t.Fatalf("Follow A: %v", err)
			}
			if nextA.Revision != snap3.Revision || len(upsA) != 2 {
				t.Fatalf("消费者 A = %+v,%+v, want 前进至 %d 并 2 增量", nextA, upsA, snap3.Revision)
			}
			for _, u := range upsA {
				if u.Op != query.UpdateUpsert {
					t.Fatalf("消费者 A 批掺入 %q: %+v", u.Op, u)
				}
			}

			nextB, upsB, err := proj.Follow(f.ctx, subB, 1)
			if err != nil {
				t.Fatalf("Follow B: %v", err)
			}
			if len(upsB) != 2 || upsB[0].Op != query.UpdateGap || upsB[1].Op != query.UpdateReset {
				t.Fatalf("消费者 B = %+v, want gap+reset", upsB)
			}
			if !reflect.DeepEqual(nextB, subB) {
				t.Fatalf("消费者 B 位置 = %+v, want %+v", nextB, subB)
			}
		})

		t.Run("等待者唤醒与取消: a waiter wakes on the next reading and cancels clean", func(t *testing.T) {
			f := newRuntimeFixture(t)
			proj := newTestProjection(f.inventory, 8)
			snap1, _, _ := proj.Snapshot(f.ctx, filterAll)
			sub := query.Subscription{Revision: snap1.Revision, Filter: filterAll}

			type followResult struct {
				next query.Subscription
				ups  []query.Update
				err  error
			}
			ch := make(chan followResult, 1)
			go func() {
				next, ups, err := proj.Follow(f.ctx, sub, 100)
				ch <- followResult{next, ups, err}
			}()
			snap2, _, err := proj.Snapshot(f.ctx, filterAll)
			if err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			select {
			case r := <-ch:
				if r.err != nil {
					t.Fatalf("Follow: %v", r.err)
				}
				if r.next.Revision != snap2.Revision {
					t.Fatalf("唤醒后位置 = %d, want %d", r.next.Revision, snap2.Revision)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("等待者未被下一读代唤醒")
			}

			ctx, cancel := context.WithCancel(context.Background())
			errCh := make(chan error, 1)
			go func() {
				_, _, err := proj.Follow(ctx, query.Subscription{Revision: snap2.Revision, Filter: filterAll}, 100)
				errCh <- err
			}()
			cancel()
			select {
			case err := <-errCh:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("取消返回 = %v, want context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("取消未释放等待者")
			}
		})

		t.Run("视图进出具名: a re-registered lease upserts and a narrowed exit deletes", func(t *testing.T) {
			f := newRuntimeFixture(t)
			proj := newTestProjection(f.inventory, 8)
			snap1, _, _ := proj.Snapshot(f.ctx, filterAll)
			f.leases.Register("model", "quota-a", nil)
			snap2, _, _ := proj.Snapshot(f.ctx, filterAll)
			f.leases.Register("model", "quota-a", nil)
			snap3, _, _ := proj.Snapshot(f.ctx, filterAll)

			next, ups, err := proj.Follow(f.ctx, query.Subscription{Revision: snap1.Revision, Filter: filterAll}, 100)
			if err != nil {
				t.Fatalf("Follow: %v", err)
			}
			if len(ups) != 2 {
				t.Fatalf("重注册增量 = %d 条, want 2 条: %+v", len(ups), ups)
			}
			for i, u := range ups {
				if u.Op != query.UpdateUpsert {
					t.Fatalf("重注册第 %d 条 = %q, want upsert（同状态字段变化不误判为新增+消失）", i+1, u.Op)
				}
				if u.Ref.Kind != query.ObjectLease || u.Ref.Lease.Kind != "model" || u.Ref.Lease.Key != "quota-a" {
					t.Fatalf("重注册第 %d 条对象 = %+v, want model/quota-a", i+1, u.Ref)
				}
			}
			if ups[0].Revision != snap2.Revision || ups[1].Revision != snap3.Revision {
				t.Fatalf("重注册增量锚点 = %d,%d, want %d,%d", ups[0].Revision, ups[1].Revision, snap2.Revision, snap3.Revision)
			}
			if next.Revision != snap3.Revision {
				t.Fatalf("重注册后位置 = %d, want %d", next.Revision, snap3.Revision)
			}

			f2 := newRuntimeFixture(t)
			proj2 := newTestProjection(f2.inventory, 8)
			snapA, _, _ := proj2.Snapshot(f2.ctx, filterAll)
			ref := noticeRef(f2, 2)
			if d, ok := findDetail(snapA.Details, ref); !ok || d.State.NoticePhase != query.NoticePhaseAccepted {
				t.Fatalf("前置：通知 2 应在场且处于 accepted 相: %+v", d)
			}
			if err := f2.store.Claim(f2.ctx, f2.addrB, f2.instance, 3); err != nil {
				t.Fatalf("Claim: %v", err)
			}
			if err := f2.store.Consume(f2.ctx, f2.addrB, f2.instance, 3); err != nil {
				t.Fatalf("Consume: %v", err)
			}
			snapB, _, _ := proj2.Snapshot(f2.ctx, filterAll)
			if d, ok := findDetail(snapB.Details, ref); !ok || d.State != query.DetailRetired {
				t.Fatalf("前置：通知 2 应退役: %+v", d)
			}

			subActive := query.Subscription{Revision: snapA.Revision, Filter: filterAccepted}
			subFull := query.Subscription{Revision: snapA.Revision, Filter: filterAll}
			_, upsActive, err := proj2.Follow(f2.ctx, subActive, 100)
			if err != nil {
				t.Fatalf("Follow 活跃流: %v", err)
			}
			_, upsFull, err := proj2.Follow(f2.ctx, subFull, 100)
			if err != nil {
				t.Fatalf("Follow 全量流: %v", err)
			}
			if len(upsActive) != 1 || upsActive[0].Op != query.UpdateDelete || !reflect.DeepEqual(upsActive[0].Ref, ref) {
				t.Fatalf("窄化流 = %+v, want 1 具名 delete %+v", upsActive, ref)
			}
			if len(upsFull) != 2 {
				t.Fatalf("全量流 = %+v, want 2 upsert（通知相变 + 承接会话邮箱读数推进）", upsFull)
			}
			sawNotice := false
			for _, u := range upsFull {
				if u.Op != query.UpdateUpsert {
					t.Fatalf("全量流增量 = %+v, want upsert", u)
				}
				if reflect.DeepEqual(u.Ref, ref) {
					sawNotice = true
					if u.Detail.State != query.DetailRetired {
						t.Fatalf("全量流增量状态 = %+v, want consumed 相锚 %+v", u.Detail.State, query.DetailRetired)
					}
				}
				if u.Ref.Kind != query.ObjectNotice && u.Ref.Kind != query.ObjectSession {
					t.Fatalf("全量流增量 = %+v, want 通知或会话的读数推进", u)
				}
			}
			if !sawNotice {
				t.Fatalf("全量流 = %+v, want 通知 %+v 的 upsert 在场", upsFull, ref)
			}
		})
	})

	t.Run("S6 游标绑定: a cursor follows one revision under one scope and one filter", func(t *testing.T) {
		f := newRuntimeFixture(t)
		proj := newTestProjection(f.inventory, 8)
		filterX := query.ListFilter{Scope: scopeAll, Kinds: []query.ObjectKind{query.ObjectTask, query.ObjectNotice}}
		snap1, cur1, err := proj.Snapshot(f.ctx, filterX)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		snap2, _, err := proj.Snapshot(f.ctx, filterX)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}

		if _, err := proj.List(snap2.Revision, cur1, filterX, query.ObjectRef{}, 3); !errors.Is(err, query.ErrCursorRevisionMismatch) {
			t.Fatalf("换修订 = %v, want ErrCursorRevisionMismatch", err)
		}
		filterKinds := query.ListFilter{Scope: scopeAll, Kinds: []query.ObjectKind{query.ObjectTask}}
		if _, err := proj.List(snap1.Revision, cur1, filterKinds, query.ObjectRef{}, 3); !errors.Is(err, query.ErrCursorFilterChanged) {
			t.Fatalf("换过滤 = %v, want ErrCursorFilterChanged", err)
		}
		filterScope := query.ListFilter{Scope: scopeA, Kinds: []query.ObjectKind{query.ObjectTask, query.ObjectNotice}}
		if _, err := proj.List(snap1.Revision, cur1, filterScope, query.ObjectRef{}, 3); !errors.Is(err, query.ErrCursorScopeChanged) {
			t.Fatalf("换授权 = %v, want ErrCursorScopeChanged", err)
		}

		filterY := query.ListFilter{
			Scope: query.AuthorizationScope{Agents: []event.AgentID{"agent-c", "agent-a", "agent-b"}},
			Kinds: []query.ObjectKind{query.ObjectNotice, query.ObjectTask},
		}
		pageX, err := proj.List(snap1.Revision, cur1, filterX, query.ObjectRef{}, 10)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		pageY, err := proj.List(snap1.Revision, cur1, filterY, query.ObjectRef{}, 10)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if !reflect.DeepEqual(pageX, pageY) {
			t.Fatalf("重排同条件被拒或页不同: %+v vs %+v", pageX, pageY)
		}
		if len(pageX) == 0 {
			t.Fatalf("翻页空转")
		}

		if _, err := proj.List(snap1.Revision, query.Cursor{}, filterX, query.ObjectRef{}, 3); !errors.Is(err, query.ErrCursorRevisionMismatch) {
			t.Fatalf("零游标 = %v, want 具名拒绝", err)
		}
		if _, err := proj.Detail(snap1.Revision, cur1, filterKinds, taskRef(f)); !errors.Is(err, query.ErrCursorFilterChanged) {
			t.Fatalf("详情换过滤 = %v, want ErrCursorFilterChanged", err)
		}
		if _, _, err := proj.Follow(f.ctx, query.Subscription{Revision: snap2.Revision + 1, Filter: filterX}, 100); !errors.Is(err, query.ErrCursorWindowExpired) {
			t.Fatalf("未来位置 = %v, want ErrCursorWindowExpired", err)
		}
	})
}
