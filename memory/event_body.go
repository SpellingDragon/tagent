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
	"strconv"

	"github.com/SpellingDragon/tagent/api/event"
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
//
// Bodies the earlier writer committed spell their identity as event_key beside
// event_type and event_summary. Those bodies decode under the same schema: the
// decimal identity is carried over verbatim, because it is the key the segment
// index stored the event under, and the two renamed fields fill the light
// fields they name. A body carrying no identity at all is refused by name, so
// an event key can never be invented from the bytes beside it.
func DecodeEventBody(body []byte) (EventBody, error) {
	var decoded EventBody
	if err := json.Unmarshal(body, &decoded); err != nil {
		return EventBody{}, fmt.Errorf("%w: %w", ErrEventBody, err)
	}
	if decoded.Key == "" {
		var legacy struct {
			Key     json.RawMessage `json:"event_key"`
			Type    string          `json:"event_type"`
			Summary string          `json:"event_summary"`
		}
		if err := json.Unmarshal(body, &legacy); err != nil {
			return EventBody{}, fmt.Errorf("%w: %w", ErrEventBody, err)
		}
		key, err := legacyBodyKey(legacy.Key)
		if err != nil {
			return EventBody{}, err
		}
		decoded.Key = key
		if decoded.Type == "" {
			decoded.Type = legacy.Type
		}
		if decoded.Summary == "" {
			decoded.Summary = legacy.Summary
		}
	}
	if decoded.Key == "" {
		return EventBody{}, fmt.Errorf("%w: the body carries no event key", ErrEventBody)
	}
	return decoded, nil
}

// legacyBodyKey reads the canonical identity out of the earlier event_key
// field: the decimal literal that writer recorded, as a JSON number or as a
// string. A number is kept verbatim, so the identity matches the segment
// index key character by character; a literal that is not an unsigned integer
// has no canonical spelling and is refused by name.
func legacyBodyKey(raw json.RawMessage) (event.EventID, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if raw[0] == '"' {
		var key string
		if err := json.Unmarshal(raw, &key); err != nil {
			return "", fmt.Errorf("%w: legacy event key: %w", ErrEventBody, err)
		}
		if key == "" {
			return "", nil
		}
		return event.EventID(key), nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return "", fmt.Errorf("%w: legacy event key: %w", ErrEventBody, err)
	}
	if _, err := strconv.ParseUint(number.String(), 10, 64); err != nil {
		return "", fmt.Errorf("%w: legacy event key %q is not an unsigned integer", ErrEventBody, number.String())
	}
	return event.EventID(number.String()), nil
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
