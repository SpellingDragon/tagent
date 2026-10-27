// 契约: docs/FACT-VOCABULARY.md#families
//
// The compress request family: the judgment that material is due for
// compression, stated as the fact it is — who judged, why, what was judged and
// the identity the judgment is quoted by — and never as the instruction that
// performs the compression. A producer files the request it judged, a consumer
// reads it, compresses the material the request names and files the product it
// minted. A repeated judgment keeps the identity and the first filing moment of
// the request already on record, so one body of material carries one request
// however often it is judged.
//
// The judgment is vocabulary rather than a domain mechanism: the mechanisms
// that judge it (workspace on a tool result, features/evolution/meditation on a card
// sequence) and the log that files it (memory) live in three domains, and the
// event axis is the one package all three already speak. The producer and the
// reason a mechanism states stay with that mechanism; this file states the fact
// they are quoted by.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// CompressRequestType is the frozen type of a compress request fact.
const CompressRequestType = "compress.request"

// CompressRequest is one compress request fact: the judgment that material is
// over its bound. It names who judged, why, what was judged and the identity the
// request is quoted by, and it is immutable once filed — the same judgment files
// the same identity, and a retry never refiles it under a second one.
type CompressRequest struct {
	// ID is the identity of the judgment: the content-addressed key the request
	// is quoted by, derived from the producer, the subject and the reason.
	ID EventID
	// Producer names the mechanism that judged the material due for compression.
	Producer string
	// Reason states why that mechanism judged it due.
	Reason string
	// Subject is the identity of the material judged: the content-addressed key
	// of the sequence or result the request names.
	Subject string
	// At is the moment the judgment was filed.
	At time.Time
}

// CompressRequestFor returns the request fact one judgment files: the identity is
// content-addressed over the producer, the subject and the reason, so the same
// judgment always answers the same identity and a re-judgment adds no second
// request. The filing moment stays zero for the log to stamp on first filing.
func CompressRequestFor(producer, subject, reason string) CompressRequest {
	sum := sha256.Sum256([]byte(producer + "\x00" + subject + "\x00" + reason))
	return CompressRequest{
		ID:       EventID("cmp-" + hex.EncodeToString(sum[:8])),
		Producer: producer,
		Reason:   reason,
		Subject:  subject,
	}
}

// FactKey returns the compress request's identity as a fact key: the
// content-addressed key one judgment is quoted by, unchanged by every re-filing
// of that judgment.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r CompressRequest) FactKey() string { return string(r.ID) }

// FactType returns the frozen type a compress request is read as.
func (r CompressRequest) FactType() string { return CompressRequestType }

// The compress request family satisfies the fact category contract.
var _ Fact = CompressRequest{}
