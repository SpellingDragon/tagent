package query

import "context"

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Subscription is one independent consumer's read position in the projection: the
// bounded snapshot revision it follows together with the filter it was opened
// under.
//
// The revision and the filter are bound together, so paging, detail reads and the
// Update increments a subscription receives all describe the same view, and a
// position whose window has expired is named as such rather than silently
// offset. Several consumers advance on their own, and one that falls behind the
// overflow bound is reset from a fresh snapshot instead of losing increments in
// silence.
type Subscription struct {
	// Revision is the snapshot revision the subscription follows.
	Revision Revision
	// Filter is the filter the subscription was opened under.
	Filter ListFilter
}

// GapKind names why an increment stream has a gap: the read window expired, the
// subscription overflowed its bound, or an object was deleted between two
// revisions.
//
// A gap is always named with its kind and is never reported as a silent
// omission; a consumer that receives a reset re-reads a bounded snapshot rather
// than continuing from a position it cannot trust.
type GapKind string

const (
	// GapWindowExpired marks a cursor whose read window has expired.
	GapWindowExpired GapKind = "window-expired"
	// GapOverflow marks a consumer that fell behind the overflow bound.
	GapOverflow GapKind = "overflow"
	// GapDeleted marks an object removed between two revisions.
	GapDeleted GapKind = "deleted"
)

// UpdateOp names which of the four operations one increment of the projection
// carries: upsert, delete, gap and reset. The vocabulary sits beside the gap
// kinds of GapKind and the carriers already declared here; nothing of the
// earlier increment shapes is removed by it.
type UpdateOp string

const (
	// UpdateUpsert names an increment that reports one object as it now reads:
	// the object and its detail arrive together at the revision the increment
	// is anchored to.
	UpdateUpsert UpdateOp = "upsert"
	// UpdateDelete names an increment that reports one object removed from the
	// projection. A deletion is an ordinary delta of the stream: it removes
	// exactly the object it names, it is never defaulted to a data gap, and it
	// is never read as one.
	UpdateDelete UpdateOp = "delete"
	// UpdateGap names an increment that reports an explicit hole: a range of
	// the stream is missing, the missing range is checkable against the
	// revisions it spans, and the hole is reported instead of being silently
	// skipped.
	UpdateGap UpdateOp = "gap"
	// UpdateReset names an increment that re-anchors the per-source watermarks
	// of the subscription; a reset never claims continuity with the increments
	// before it.
	UpdateReset UpdateOp = "reset"
)

// Update is one increment of the projection a subscription follows. It carries
// the four operations upsert, delete, gap and reset beside the revision and the
// object the increment applies to, and only the payload matching Op is
// populated, the way an ObjectRef populates only the identity matching its
// kind.
type Update struct {
	// Op names the operation this increment carries.
	Op UpdateOp
	// Revision is the revision this increment is anchored to.
	Revision Revision
	// Ref names the object the increment applies to; it is empty for a gap or a
	// reset, which describe the stream rather than one object.
	Ref ObjectRef
	// Detail carries the object as it now reads when Op is UpdateUpsert; it is
	// empty for the other operations.
	Detail Detail
	// Gap carries the missing range and its reason when Op is UpdateGap; it is
	// empty for the other operations.
	Gap Gap
	// Reset carries the re-anchored watermarks when Op is UpdateReset; it is
	// empty for the other operations.
	Reset Reset
}

// Gap is the payload of an explicit hole in one increment stream: a range of
// entries is missing, the range itself is checkable against the revisions it
// spans, and the hole is carried as data rather than skipped in silence.
type Gap struct {
	// From is the first missing revision of the range.
	From Revision
	// To is the last missing revision of the range: every revision from From up
	// to and including To is missing.
	To Revision
	// Reason names why the range is missing, in the gap vocabulary the
	// subscription already reports gaps in (GapKind), so the existing
	// classification stands beside the range carrier instead of being replaced
	// by it.
	Reason GapKind
}

// Reset is the payload of a watermark reset: the per-source watermarks a
// subscription is re-anchored to after an overflow or a hole it cannot bridge.
//
// A reset never claims continuity: the increments before it and the increments
// after it do not form one continuous sequence, the watermarks it carries are a
// new anchor rather than a continuation of the old positions, and a consumer
// that receives a reset re-reads a bounded snapshot instead of trusting a
// position across it. A reset is never used to paper over a hole: an unbridgeable
// hole is reported as a named gap first and a reset never pretends the gap did
// not happen.
type Reset struct {
	// Watermarks are the per-source watermarks the subscription is re-anchored
	// to, one entry per source, each carrying its own opaque position and its
	// own lag reading.
	Watermarks []SourceWatermark
}

// Follow advances one subscription by one increment batch and answers with the position to continue
// from beside the increments between the presented position and the newest reading the projection holds.
//
// - Follow is the incremental read face beside the snapshot and the pages: it derives the increments of the revisions the subscription has not consumed, restricted to the filter it was opened under, so one subscription advances on its own and never carries another consumer's position with it.
// - The increments are the ordinary named delta of the kept view — one upsert per new or changed entry and one delete per entry that left it, a narrowed view included — and an unbridgeable range is never answered with a silently truncated batch.
// - overflow is how many revisions one subscription may fall behind before its range is unbridgeable; a non-positive overflow tolerates no backlog at all.
// - A bridgeable range answers with the increments from the presented position up to the newest reading and a continuation anchored at that reading.
// - An unbridgeable one — the oldest reading the range needs has left the bounded window, or the backlog stands beyond the overflow bound — answers with a named gap beside a reset: the gap names the whole missing range (From the first missing revision, To the last) and its reason, the reset re-anchors the per-source watermarks of the newest reading, and the returned position stays exactly where it was, so no position ever advances across a reset and a consumer re-reads a bounded snapshot before it continues.
// - Follow blocks while the projection holds nothing newer than the presented position and returns once the next reading is taken or the context is cancelled: the waiting caller is the waiter itself, a cancelled context releases it with the context's own error, and no waiter is left behind.
// - A position anchored ahead of every reading taken is refused as ErrCursorWindowExpired: it names a view that does not exist rather than one the window dropped.
func (p *Projection) Follow(ctx context.Context, sub Subscription, overflow int) (Subscription, []Update, error) {
	for {
		wait := p.mintedChan()
		floor, current := p.bounds()
		if sub.Revision > current {
			return Subscription{}, nil, ErrCursorWindowExpired
		}
		if current > sub.Revision {
			return p.serve(sub, overflow, floor, current)
		}
		select {
		case <-wait:
			continue
		case <-ctx.Done():
			return Subscription{}, nil, ctx.Err()
		}
	}
}

// serve derives one increment batch for a subscription the projection holds
// newer readings than, naming an unbridgeable range instead of serving a batch
// that silently skips revisions.
func (p *Projection) serve(sub Subscription, overflow int, floor, current Revision) (Subscription, []Update, error) {
	oldest := sub.Revision
	if oldest < 1 {
		oldest = 1
	}
	bound := Revision(0)
	if overflow > 0 {
		bound = Revision(overflow)
	}
	if oldest < floor || current-sub.Revision > bound {
		reason := GapOverflow
		if oldest < floor {
			reason = GapWindowExpired
		}
		marks, err := p.watermarksOf(current)
		if err != nil {
			return Subscription{}, nil, err
		}
		return sub, []Update{
			{Op: UpdateGap, Revision: current, Gap: Gap{From: sub.Revision + 1, To: current, Reason: reason}},
			{Op: UpdateReset, Revision: current, Reset: Reset{Watermarks: marks}},
		}, nil
	}
	updates, err := p.increments(sub, current)
	if err != nil {
		return Subscription{}, nil, err
	}
	next := sub
	next.Revision = current
	return next, updates, nil
}

// increments derives the per-subscription increments between the presented
// position and the newest reading, one batch of adjacent reading pairs in
// revision order. The bridgeable range this is called under guarantees every
// reading the pairs name is still retained; a reading the window dropped is
// refused by name rather than skipped.
func (p *Projection) increments(sub Subscription, current Revision) ([]Update, error) {
	keep := func(d Detail) bool { return detailAllowed(sub.Filter, d) }
	var out []Update
	var prev *reading
	if sub.Revision >= 1 {
		from, err := p.retained(sub.Revision)
		if err != nil {
			return nil, err
		}
		prev = from
	}
	for at := sub.Revision + 1; at <= current; at++ {
		cur, err := p.retained(at)
		if err != nil {
			return nil, err
		}
		out = append(out, deltaOf(prev, cur, keep)...)
		prev = cur
	}
	return out, nil
}

// watermarksOf returns the per-source watermarks one retained reading was
// anchored to, each a copy of its own, so no write through a reset reaches the
// frozen reading.
func (p *Projection) watermarksOf(at Revision) ([]SourceWatermark, error) {
	r, err := p.retained(at)
	if err != nil {
		return nil, err
	}
	return cloneWatermarks(r.watermarks), nil
}
