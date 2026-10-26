// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// The meditation domain's own judgments over the facts it consumes: the
// freshness judgment that reads one observed fact and the delivery judgment
// that reads one fired reflection's input. A fact carries its trigger-source
// stamp as recording metadata; these judgments state, in the domain's own
// vocabulary, which stamps open the freshness gate and which may leave through
// the delivery port.
package meditation

import "github.com/SpellingDragon/tagent/api/event"

// hostFacingStamps is the declared vocabulary of trigger sources that name an
// interaction someone awaits. A stamp outside it names traffic the agent
// manages for itself — its own reflection, a consolidation wake-up, or a stamp
// the face cannot read — and never opens the freshness gate.
var hostFacingStamps = map[string]struct{}{
	"user":          {},
	"task":          {},
	"reincarnation": {},
	"system_alert":  {},
}

// hostFacingStamp reports whether one trigger-source stamp names an interaction
// someone awaits.
func hostFacingStamp(stamp string) bool {
	_, hostFacing := hostFacingStamps[stamp]
	return hostFacing
}

// hostVisibleStamp reports whether one trigger-source stamp names traffic the
// host may see delivered: every host-facing interaction beside the reflection
// turn's own stamp.
func hostVisibleStamp(stamp string) bool {
	return stamp == LineageReflection || hostFacingStamp(stamp)
}

// NoveltyPolicy is the meditation domain's freshness policy: one observed fact
// in, whether it is new material for the reflection out. The criteria live in
// this domain, so a stamp the policy does not name is never new material.
type NoveltyPolicy struct{}

// Novel reports whether one observed fact opens the freshness gate.
func (NoveltyPolicy) Novel(fact event.Event) bool {
	return hostFacingStamp(fact.Meta[event.MetaKeyTriggerSource])
}

// DeliveryPolicy is the meditation domain's delivery policy: one fired
// reflection's own input in, whether its output may leave through the delivery
// port out. A stamp the policy does not name is withheld.
type DeliveryPolicy struct{}

// Deliverable reports whether the output behind one fired reflection input may
// leave through the delivery port.
func (DeliveryPolicy) Deliverable(input event.Event) bool {
	return hostVisibleStamp(input.Meta[event.MetaKeyTriggerSource])
}

// DeliverableLineage reports whether a trigger-source stamp names traffic the
// host may see delivered, from the vocabulary the delivery policy reads. The
// stamp stays on the fact as recording metadata.
func DeliverableLineage(stamp string) bool {
	return hostVisibleStamp(stamp)
}

// SelfManagedLineage reports whether a trigger-source stamp names traffic the
// agent manages for itself rather than an interaction someone awaits. The
// derivation is the recording reading of the stamp: an unknown or empty stamp
// is self-managed, and no freshness decision is read from this derivation.
func SelfManagedLineage(stamp string) bool {
	return !hostFacingStamp(stamp)
}
