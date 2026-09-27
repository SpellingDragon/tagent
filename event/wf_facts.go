package event

import "trpc.group/trpc-go/trpc-agent-go/model"

// Workflow internal facts (introduce-durable-workflow-engine D1/D4, spec:
// workflow-fact-persistence). The fact chain is the SINGLE source of truth for
// workflow runtime state: reception, activity intent, activity result,
// signals, transitions (checkpoints), finalization, and safe-removal evidence
// are all internal facts under the "wf." type prefix. Every derived view
// (CheckpointSaver adapter, activity index, task board) is folded from these.
//
// Registry-declared as a family: non-projection (§5.5 single source — every
// append path consults IsNonProjectionRecord), non-embeddable, non-recallable.
// The durable engine was withdrawn; these types now survive only to passively
// EXCLUDE historical wf.* records from projection/recall/embedding. That is
// the sole purpose of the registration, so it MUST NOT carry a type TTL: a
// 30-day override here would silently shorten retention for records that were
// written under the pre-existing global/explicit policy (R05). TTLDays 0 keeps
// them inheriting the global default and adds no type entry to DefaultTypeTTL.
const (
	// TypeWFReceived: durable acceptance of an input batch member (receive gate).
	TypeWFReceived = "wf.received"
	// TypeWFIntent: activity intent — {lineage,node,attempt} declared BEFORE
	// the trusted node closure runs (three-phase gate, D2).
	TypeWFIntent = "wf.intent"
	// TypeWFResult: activity result — persisted before progression advances.
	TypeWFResult = "wf.result"
	// TypeWFSignal: external persistent signal (settle, send/resume, cancel,
	// TTL renewal/expiry). Signals land as facts first, then drive the engine.
	TypeWFSignal = "wf.signal"
	// TypeWFTransition: checkpoint transition fact (lineage, step, node,
	// predecessor checkpoint ref, activity handle, output digest).
	TypeWFTransition = "wf.transition"
	// TypeWFFinalized: workflow terminal fact. Framework Done alone NEVER
	// finalizes a workflow; only this fact does (D2).
	TypeWFFinalized = "wf.finalized"
	// TypeWFRemoved: safe-removal evidence for protected material. Retention
	// leases release only on a confirmed removal fact (D4/F5: evidence is the
	// completed removal, not the initiated one).
	TypeWFSafeRemoved = "wf.safe_removed"
)

// WFExcludedTypes lists every workflow internal fact type. Single source for
// the projection/recall/embed exclusion family — mirrored from the registry,
// never re-enumerated per call site (three append paths share
// IsNonProjectionRecord; this list serves diagnostics and guard tests).
func WFExcludedTypes() []string {
	return []string{
		TypeWFReceived,
		TypeWFIntent,
		TypeWFResult,
		TypeWFSignal,
		TypeWFTransition,
		TypeWFFinalized,
		TypeWFSafeRemoved,
	}
}

func init() {
	for _, name := range WFExcludedTypes() {
		RegisterEventType(EventTypeSpec{
			Name:          name,
			Role:          model.RoleUser,
			Skeleton:      false,
			TTLDays:       0, // passive exclusion only: inherit the global TTL, never shorten history retention (R05)
			Embeddable:    false,
			Recallable:    false,
			NonProjection: true,
		})
	}
}

// Workflow fact contract keys (written onto FullEvent.Metadata). These are the
// ONLY authoritative identity names for workflow runtime provenance in the
// fact chain — consumers parse through these constants, never raw literals.
const (
	MetaKeyWFLineage = "wf_lineage" // lineage id (workflow instance identity)
	MetaKeyWFNode    = "wf_node"    // node id within the compiled graph
	MetaKeyWFAttempt = "wf_attempt" // attempt within {lineage,node} (D6 stable identity)
	MetaKeyWFStep    = "wf_step"    // graph step number at record time
	MetaKeyWFKind    = "wf_kind"    // activity/fact kind discriminator within a type
	MetaKeyWFWriter  = "wf_writer"  // claim owner (single-writer arbitration, D6)
	MetaKeyWFExtra   = "wf_extra"   // folded Extra identity part (persisted for symmetric fold)
)
