package consolidation // import "github.com/SpellingDragon/tagent/evolution/consolidation"

The consolidation channel's vocabulary: the trigger-source stamp of the internal
consolidation wake-up. A consolidation pass hands itself a candidate list to
consider, so the traffic it files carries this stamp and never the stamp of
a host artifact. The channel declares its own reading of that traffic here,
beside the two readings every stamp has: self-managed and deliverable.

CONSTANTS

const LineageConsolidationHint = "consolidation_hint"
    LineageConsolidationHint is the trigger-source stamp of the internal
    consolidation wake-up: self-managed traffic whose payload is a candidate
    list and never a host artifact. The stamp is declared rather than left to
    "not recognised", so both readings of it ship together and a stamp added
    to the deliverable vocabulary cannot quietly turn capacity pressure into a
    host-facing product.
