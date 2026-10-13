package query

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Subscription is one independent consumer's read position in the projection: the
// bounded snapshot revision it follows together with the filter it was opened
// under.
//
// The revision and the filter are bound together, so paging, detail reads and the
// increments a subscription receives all describe the same view, and a position
// whose window has expired is named as such rather than silently offset. Several
// consumers advance on their own, and one that falls behind the overflow bound is
// reset from a fresh snapshot instead of losing increments in silence.
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
