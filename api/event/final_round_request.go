// 契约: docs/FACT-VOCABULARY.md#families
//
// The final round request family: the judgment that one range's budget is spent
// and its next decision is the closing round, stated as the fact it is — which
// range, why, and the inputs the judgment read. A producer files the judgment,
// the loop reads it at a decision boundary and runs the closing round; a range
// carries one request however often the judgment is re-evaluated.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// FinalRoundRequestType is the frozen type of a final round request fact.
const FinalRoundRequestType = "final_round.request"

// FinalRoundRequest is one final round request fact: the judgment that a range's
// budget is spent. It is immutable once filed, and its identity is independent
// of the moment and the measured inputs, so one range answers one request.
type FinalRoundRequest struct {
	// ID is the identity of the judgment: content-addressed over the range and
	// the reason, so the same judgment never files a second request.
	ID EventID
	// Range is the invocation whose budget the judgment read.
	Range InvocationID
	// Reason states why the budget is spent: the round count or the elapsed time.
	Reason string
	// Rounds is the number of tool rounds the range had run when judged.
	Rounds int
	// Elapsed is the time the range had spent when judged.
	Elapsed time.Duration
}

// FinalRoundRequestFor returns the request fact one judgment files: the identity
// is content-addressed over the range and the reason, so a re-evaluation of the
// same spent budget answers the same identity. The filing moment stays zero for
// the log to stamp on first filing.
func FinalRoundRequestFor(rangeID InvocationID, reason string, rounds int, elapsed time.Duration) FinalRoundRequest {
	sum := sha256.Sum256([]byte(string(rangeID) + "\x00" + reason))
	return FinalRoundRequest{
		ID:      EventID("frr-" + hex.EncodeToString(sum[:8])),
		Range:   rangeID,
		Reason:  reason,
		Rounds:  rounds,
		Elapsed: elapsed,
	}
}

// FactKey returns the request's identity as a fact key: the identity one range
// is quoted by, unchanged by every re-evaluation of the same budget.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r FinalRoundRequest) FactKey() string { return string(r.ID) }

// FactType returns the frozen type a final round request is read as.
func (r FinalRoundRequest) FactType() string { return FinalRoundRequestType }

// The final round request family satisfies the fact category contract.
var _ Fact = FinalRoundRequest{}
