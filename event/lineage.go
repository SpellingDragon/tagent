// 谱系投递策略的单一真源：宿主投递门与折叠外显判定同源消费此白名单。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package event

// LineageMeditation is the self-initiated reflection turn trigger source.
//
// - It is host-visible, so its output may be delivered, yet remains self-managed traffic for the telemetry audit: the two-layer meaning every consumer of this package must keep.
const LineageMeditation = "meditation"

// LineageConsolidationHint is the pre-existing consolidation-triggers
// capacity-path trigger source, registered explicitly (never left to the
// unknown fail-closed default) with a meaning asymmetric to LineageMeditation:
//
//   - self-managed: an internal tidy-up wake-up, never re-arming novelty
//     (including the cross-partition novelty of an external meditation agent).
//   - NOT deliverable: its payload is a candidate list, not a host artifact.
const LineageConsolidationHint = "consolidation_hint"

// DeliverableLineage reports whether a trigger_source lineage is host-facing, i.e. a
// reclaim carrying it was or can be delivered to the host, so its verbatim notice may
// age out.
//
// - Anything outside this whitelist is internal and fail-closed withheld: unknown values are never delivered.
// - consolidation_hint is deliberately absent: adding it here would silently break the novelty exclusion its explicit self-managed registration guards.
// - Adding an externally-visible lineage means adding it here and nowhere else; consumers derive from this predicate instead of private copies.
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func DeliverableLineage(ts string) bool {
	switch ts {
	case "user", "task", "reincarnation", "system_alert", LineageMeditation:
		return true
	default:
		return false
	}
}

// SelfManagedLineage reports the telemetry-audit sense: traffic the agent
// initiated for itself rather than a user-awaited interaction. Explicitly
// self-managed lineages stay self-managed regardless of the deliverable
// whitelist, which is what pins their novelty exclusion; everything else
// derives from the same whitelist, withheld lineages being self-managed by
// definition.
//
//   - meditation: host-visible yet self-initiated (its second layer).
//   - consolidation: pure internal wake signal, self-managed on purpose.
func SelfManagedLineage(ts string) bool {
	switch ts {
	case LineageMeditation, LineageConsolidationHint:
		return true
	default:
		return !DeliverableLineage(ts)
	}
}
