package event

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// RecoveryTicketType is the frozen type of a recovery ticket fact.
const RecoveryTicketType = "recovery.ticket"

// RecoveryTicket is the on-chain envelope of one restart-recovery snapshot:
// it names the digest and byte length of the external read-only blob that
// carries the serialized carrier tables, and the cursor — the fact key of the
// last record the snapshot covers. The blob lives at one fixed current key,
// overwritten each boot; the digest verifies its bytes, the ticket is the
// chain-side truth and the blob only the referenced current bytes. Identity is content-addressed over the schema version,
// the cursor and the digest, so a re-filed snapshot of the same coverage keeps
// one identity, and the writing moment stays out of the identity.
type RecoveryTicket struct {
	// ID is the content-addressed identity of the ticket.
	ID EventID
	// SchemaVersion names the payload layout the blob was serialized under.
	SchemaVersion int
	// Digest is the hex sha256 over the blob bytes.
	Digest string
	// Length is the exact byte length the blob must have.
	Length int64
	// Cursor is the fact key of the last record the snapshot covers.
	Cursor string
}

// RecoveryTicketFor returns the ticket fact for one snapshot: the identity is
// content-addressed over the schema version, the cursor and the digest, so the
// same coverage names one identity however often the snapshot is re-filed.
func RecoveryTicketFor(schemaVersion int, cursor, digest string, length int64) RecoveryTicket {
	sum := sha256.Sum256(ticketIdentityBytes(schemaVersion, cursor, digest))
	return RecoveryTicket{
		SchemaVersion: schemaVersion,
		Digest:        digest,
		Length:        length,
		Cursor:        cursor,
		ID:            EventID("rct-" + hex.EncodeToString(sum[:8])),
	}
}

// ticketIdentityBytes renders the canonical identity material: a fixed field
// order with length-prefixed parts, so no separator can occur inside a part.
func ticketIdentityBytes(schemaVersion int, cursor, digest string) []byte {
	out := make([]byte, 0, 16+len(cursor)+len(digest))
	out = appendInt(out, schemaVersion)
	out = appendPart(out, []byte(cursor))
	out = appendPart(out, []byte(digest))
	return out
}

// appendInt appends one decimal integer with a fixed width prefix.
func appendInt(out []byte, value int) []byte {
	return append(out, []byte(fmt.Sprintf("%016d", value))...)
}

// appendPart appends one length-prefixed part.
func appendPart(out, part []byte) []byte {
	out = appendInt(out, len(part))
	return append(out, part...)
}

// FactKey returns the ticket's identity as a fact key: the identity one filing
// is quoted by, unchanged by every re-file of the same coverage.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (t RecoveryTicket) FactKey() string { return string(t.ID) }

// FactType returns the frozen type a recovery ticket is read as.
func (t RecoveryTicket) FactType() string { return RecoveryTicketType }

// Payload renders the canonical ticket bytes: struct-field order under
// encoding/json is fixed, and the envelope carries no map, so the same ticket
// marshals to identical bytes every time.
func (t RecoveryTicket) Payload() []byte {
	body, _ := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		Digest        string `json:"digest"`
		Length        int64  `json:"length"`
		Cursor        string `json:"cursor"`
	}{t.SchemaVersion, t.Digest, t.Length, t.Cursor})
	return body
}

// A recovery ticket is a fact.
var _ Fact = RecoveryTicket{}
