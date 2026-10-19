package inspection

import (
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/query"
)

// TestSecondWaveConsumersShareTheProtocol pins the push consumer of the query read face to the shared consumer protocol.
// - the subscription reads one object identity at its revision and receives the increment that names the same object
// - the delivered increment carries the object as a detail read at its revision carries it, and advances the position only to that revision
// - a second subscription at the same revision receives the same increment: the consumer wrote nothing back into the source
// 契约: docs/EXTENSIONS.md#consumer-protocol
func TestSecondWaveConsumersShareTheProtocol(t *testing.T) {
	f := newRuntimeFixture(t)
	proj := newTestProjection(f.inventory, 8)
	snap1, _, err := proj.Snapshot(f.ctx, filterAll)
	if err != nil {
		t.Fatalf("snapshot at the read position: %v", err)
	}

	f.leases.Register("storage", "slot-b", nil)
	snap2, cur2, err := proj.Snapshot(f.ctx, filterAll)
	if err != nil {
		t.Fatalf("snapshot beside the new reading: %v", err)
	}
	want, ok := findLease(snap2.Details, "storage", "slot-b")
	if !ok {
		t.Fatalf("the registered lease is absent from the reading the increment must name")
	}

	next, ups, err := proj.Follow(f.ctx, query.Subscription{Revision: snap1.Revision, Filter: filterAll}, 100)
	if err != nil {
		t.Fatalf("follow the projection: %v", err)
	}
	if len(ups) != 1 || ups[0].Op != query.UpdateUpsert {
		t.Fatalf("increments = %+v, want the one upsert of the object the source gained", ups)
	}
	if !reflect.DeepEqual(ups[0].Ref, want.Ref) {
		t.Fatalf("increment identity = %+v, want the object identity it read (%+v)", ups[0].Ref, want.Ref)
	}
	if !reflect.DeepEqual(ups[0].Detail, want) {
		t.Fatalf("increment detail = %+v, want the detail the same revision reads (%+v)", ups[0].Detail, want)
	}
	if ups[0].Revision != snap2.Revision || next.Revision != snap2.Revision {
		t.Fatalf("increment/position revisions = %d/%d, want %d", ups[0].Revision, next.Revision, snap2.Revision)
	}

	nextOther, upsOther, err := proj.Follow(f.ctx, query.Subscription{Revision: snap1.Revision, Filter: filterAll}, 100)
	if err != nil {
		t.Fatalf("follow beside the first consumer: %v", err)
	}
	if !reflect.DeepEqual(upsOther, ups) || !reflect.DeepEqual(nextOther, next) {
		t.Fatalf("second consumer at the same revision = %+v/%+v, want the same batch (%+v/%+v)", upsOther, nextOther, ups, next)
	}

	snap3, _, err := proj.Snapshot(f.ctx, filterAll)
	if err != nil {
		t.Fatalf("snapshot after the delivery: %v", err)
	}
	if !reflect.DeepEqual(snap3.Details, snap2.Details) || !reflect.DeepEqual(snap3.Watermarks, snap2.Watermarks) {
		t.Fatalf("the push consumer rewrote the source: details/watermarks moved after a delivery")
	}
	reread, err := proj.Detail(snap2.Revision, cur2, filterAll, want.Ref)
	if err != nil {
		t.Fatalf("re-read the object at its revision: %v", err)
	}
	if !reflect.DeepEqual(reread, want) {
		t.Fatalf("detail at the revision = %+v, want %+v unchanged", reread, want)
	}
}
