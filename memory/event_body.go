// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// The body schema one committed event carries through the segment store: the
// canonical identity and the light fields a query filters and matches on, and
// the owner, format and retention policy the reference beside it is registered
// under. A stored body is self-describing: it decodes back into the query
// record and the recall reference, so a committed body becomes queryable and
// readable without a second registry. The original text behind the event stays
// in the original store and is read byte for byte through the scoped path.
package memory

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// EventBody is the body schema of one committed event: the fields a query
// filters and orders on together with the reference scope the recall of the
// event runs under.
type EventBody struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID `json:"key"`
	// Type is the frozen event type of the event.
	Type string `json:"type"`
	// Summary is the stored summary of the event, the small rendering text.
	Summary string `json:"summary"`
	// Content is the stored content of the event, matched by the keyword leg.
	Content string `json:"content"`
	// Timestamp is the event time in Unix milliseconds.
	Timestamp int64 `json:"timestamp"`
	// Owner is the address the reference of the body is registered for.
	Owner event.Address `json:"owner"`
	// Format is the original format the reference is registered in.
	Format string `json:"format"`
	// RetentionPolicy is the retention policy the reference is held under.
	RetentionPolicy string `json:"retention_policy"`
}

// ErrEventBody reports a committed body that does not decode under the body
// schema, so no query record and no reference can be derived from its bytes.
var ErrEventBody = errors.New("memory: event body does not decode under the body schema")

// ErrBodyKeyMismatch reports a committed body whose canonical key disagrees
// with the key it is stored under, so the body cannot stand for that event.
var ErrBodyKeyMismatch = errors.New("memory: event body key disagrees with the stored event key")

// BodyFromEvent derives the body of one event: its canonical key, frozen type,
// summary and content, its event time on the millisecond axis, and the owner,
// format and retention policy the reference beside it is registered under. An
// event carrying no key is refused by name.
func BodyFromEvent(ev event.Event, format, retentionPolicy string) (EventBody, error) {
	if ev.Key == "" {
		return EventBody{}, ledger.ErrEventKeyRequired
	}
	return EventBody{
		Key:             ev.Key,
		Type:            ev.Type,
		Summary:         ev.Meta[event.MetaKeyEventSummary],
		Content:         ev.Message.Content,
		Timestamp:       ev.Timestamp.UnixMilli(),
		Owner:           ev.Owner,
		Format:          format,
		RetentionPolicy: retentionPolicy,
	}, nil
}

// Encode returns the committed bytes of the body under the body schema.
func (b EventBody) Encode() ([]byte, error) {
	return json.Marshal(b)
}

// DecodeEventBody decodes one committed body under the body schema, or names
// the refusal for bytes that carry no body of this schema.
func DecodeEventBody(body []byte) (EventBody, error) {
	var decoded EventBody
	if err := json.Unmarshal(body, &decoded); err != nil {
		return EventBody{}, fmt.Errorf("%w: %w", ErrEventBody, err)
	}
	return decoded, nil
}

// Record returns the query record of the body under the partition and segment
// window its event is committed into.
func (b EventBody) Record(partitionID int, windowTS int64) EventRecord {
	return EventRecord{
		Key:         b.Key,
		PartitionID: partitionID,
		Type:        b.Type,
		Summary:     b.Summary,
		Content:     b.Content,
		Timestamp:   b.Timestamp,
		WindowTS:    windowTS,
	}
}

// Ref returns the reference of the body: the key it names and the owner,
// format and retention policy its reference is registered under. The integrity
// stays undeclared here — the original store computes it over the bytes it
// commits, so a derived reference never carries a completeness claim of its
// own.
func (b EventBody) Ref() ledger.OriginalRef {
	return ledger.OriginalRef{Key: b.Key, Owner: b.Owner, Format: b.Format, RetentionPolicy: b.RetentionPolicy}
}
