// 谱系投递策略的单一真源：宿主投递门与折叠外显判定同源消费此白名单。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package event

// LineageMeditation is the self-initiated reflection turn trigger source.
//
// - It is host-visible, so its output may be delivered, yet remains self-managed traffic for the telemetry audit: the two-layer meaning every consumer of this package must keep.
const LineageMeditation = "meditation"

// DeliverableLineage reports whether a trigger_source lineage is host-facing, i.e. a
// reclaim carrying it was or can be delivered to the host, so its verbatim notice may
// age out.
//
// - Anything outside this whitelist is internal and fail-closed withheld: unknown values are never delivered.
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
// initiated for itself rather than a user-awaited interaction. It is derived
// from the same whitelist — withheld lineages are self-managed by definition,
// and meditation keeps its second layer (host-visible yet self-initiated).
func SelfManagedLineage(ts string) bool {
	return ts == LineageMeditation || !DeliverableLineage(ts)
}
