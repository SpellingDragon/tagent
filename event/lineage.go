// 谱系投递策略的单一真源：宿主投递门与折叠外显判定同源消费此白名单。
package event

// LineageMeditation is the self-initiated reflection turn's trigger source.
// It is host-visible (its output may be delivered) yet remains self-managed
// traffic for the telemetry audit — the two-layer meaning every consumer of
// this package must keep.
const LineageMeditation = "meditation"

// DeliverableLineage reports whether a trigger_source lineage is host-facing
// (externally visible): a reclaim carrying it was (or can be) delivered to
// the host, so its verbatim notice may age out. This whitelist is the SINGLE
// SOURCE OF TRUTH shared by the host delivery gate and the settle-notice
// externalization check — anything outside it is internal and FAIL-CLOSED
// withheld (unknown values are never delivered), the same conservative
// direction as the unknown-withhold philosophy.
//
// Contract: adding an externally-visible lineage means adding it HERE and
// nowhere else; consumers derive from this predicate, never from private
// copies.
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
