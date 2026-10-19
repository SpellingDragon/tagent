// 契约: docs/FACT-VOCABULARY.md#families
//
// The replay settle family: the presentation decision one assembly settled on,
// stated as the fact it is — which entry, which presentation and the text the
// decision carries — and quoted by the identity that decision answers. A
// settled decision lands one fact; an entry the budget skipped settles nothing
// and lands none.
package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// ReplaySettleType is the frozen type of a replay settle fact.
const ReplaySettleType = "replay.settle"

// ReplaySettle is the fact one presentation decision is filed as: the entry the
// assembly settled, the presentation it settled on, the bounded text that
// presentation carries and the moment the settlement was recorded. The identity
// is the decision's own, so the same decision always answers the same fact.
type ReplaySettle struct {
	// ID is the identity of the settled decision: the content-addressed key the
	// fact is quoted by, derived from the entry, the mode and the preview.
	ID event.EventID
	// Entry is the entry the assembly settled.
	Entry event.EventID
	// Mode is the presentation the assembly settled on.
	Mode PresentationMode
	// Preview is the bounded text the settled presentation carries, empty for a
	// full-text settlement.
	Preview string
	// At is the moment the settlement was recorded.
	At time.Time
}

// ReplaySettleFor returns the settle fact one presentation decision is filed as:
// the identity is content-addressed over the entry, the mode and the preview, so
// the same decision always answers the same identity and a repeated settlement
// adds no second fact. The recording moment stays zero for the caller to stamp.
func ReplaySettleFor(decision PresentationDecision) ReplaySettle {
	sum := sha256.Sum256([]byte(string(decision.Key) + "\x00" + strconv.Itoa(int(decision.Mode)) + "\x00" + decision.Preview))
	return ReplaySettle{
		ID:      event.EventID("set-" + hex.EncodeToString(sum[:8])),
		Entry:   decision.Key,
		Mode:    decision.Mode,
		Preview: decision.Preview,
	}
}

// FactKey returns the settle fact's identity: the decision identity the fact is
// quoted by, unchanged by the moment it was recorded.
func (s ReplaySettle) FactKey() string { return string(s.ID) }

// FactType returns the frozen type a settle fact is read as.
func (s ReplaySettle) FactType() string { return ReplaySettleType }

// The replay settle family satisfies the fact category contract.
var _ event.Fact = ReplaySettle{}
