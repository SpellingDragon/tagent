// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// The body schema one committed event carries through the segment store: the
// canonical identity and every field the event itself carries — the light
// fields a query filters and matches on together with the metadata projection,
// the native payload and the lineage identifiers — and the owner, format and
// retention policy the reference beside it is registered under. A stored body
// is self-describing: it decodes back into the query record, the recall
// reference and the very event that committed, so a committed body becomes
// queryable and readable without a second registry and a fact restored across
// a restart keeps every field it committed with. The original text behind the
// event stays in the original store and is read byte for byte through the
// scoped path.
package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"trpc.group/trpc-go/trpc-agent-go/model"
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

	// Meta carries the metadata projection the event committed with: every
	// entry kept whole, so a restored fact reads its summary, its trigger
	// source and its audit entries exactly as it committed them.
	Meta map[string]string `json:"meta,omitempty"`
	// Message is the native model payload the event carries, kept whole: the
	// role, the content, the tool calls and the reasoning a fact commits with
	// are what it says, and no projection of them stands for them.
	Message model.Message `json:"message"`
	// Source is the address the event came from.
	Source event.Address `json:"source"`
	// Target is the address the event is delivered to.
	Target event.Address `json:"target"`
	// InvocationID is the responsibility range the event is scoped to.
	InvocationID event.InvocationID `json:"invocation_id,omitempty"`
	// TaskID is the task the event belongs to.
	TaskID event.TaskID `json:"task_id,omitempty"`
	// InstanceID is the runtime instance the event belongs to.
	InstanceID event.InstanceID `json:"instance_id,omitempty"`
	// EpisodeID is the application level episode the event belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// DecisionID is the model decision the event is scoped to.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID is the model attempt the event is scoped to.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// CallID is the native provider call identifier the event records.
	CallID string `json:"call_id,omitempty"`
	// ApprovalID is the approval ticket identity of the call the event records.
	ApprovalID string `json:"approval_id,omitempty"`
}

// ErrEventBody reports a committed body that does not decode under the body
// schema, so no query record and no reference can be derived from its bytes.
var ErrEventBody = errors.New("memory: event body does not decode under the body schema")

// ErrBodyKeyMismatch reports a committed body whose canonical key disagrees
// with the key it is stored under, so the body cannot stand for that event.
var ErrBodyKeyMismatch = errors.New("memory: event body key disagrees with the stored event key")

// ErrFactKeyConflict reports a commit whose canonical key already names
// another fact: one identity stands for one immutable fact, so a second,
// different content under one key is refused by name instead of overwriting
// the fact a reader already holds.
var ErrFactKeyConflict = errors.New("memory: the event key already names another fact")

// BodyFromEvent derives the body of one event: its canonical key, frozen type,
// summary and content, its event time on the millisecond axis, the whole
// metadata projection, native payload and lineage identifiers the event
// carries, and the owner, format and retention policy the reference beside it
// is registered under. An event carrying no key is refused by name.
func BodyFromEvent(ev event.Event, format, retentionPolicy string) (EventBody, error) {
	if ev.Key == "" {
		return EventBody{}, ledger.ErrEventKeyRequired
	}
	meta := make(map[string]string, len(ev.Meta))
	for key, value := range ev.Meta {
		meta[key] = value
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
		Meta:            meta,
		Message:         ev.Message,
		Source:          ev.Source,
		Target:          ev.Target,
		InvocationID:    ev.InvocationID,
		TaskID:          ev.TaskID,
		InstanceID:      ev.InstanceID,
		EpisodeID:       ev.EpisodeID,
		DecisionID:      ev.DecisionID,
		AttemptID:       ev.AttemptID,
		CallID:          ev.CallID,
		ApprovalID:      ev.ApprovalID,
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

// Event restores the very event the body carries: every field it committed
// with — the whole metadata projection, the native payload and the lineage
// identifiers — read back exactly as it stands. A body the earlier writer
// committed carries the light fields alone: the content and the summary those
// bytes hold fill the message and the metadata they name, and every identity
// those bytes never carried stays empty instead of an invented one.
func (b EventBody) Event() event.Event {
	meta := make(map[string]string, len(b.Meta)+1)
	for key, value := range b.Meta {
		meta[key] = value
	}
	if b.Summary != "" {
		if _, held := meta[event.MetaKeyEventSummary]; !held {
			meta[event.MetaKeyEventSummary] = b.Summary
		}
	}
	message := b.Message
	if emptyMessage(message) && b.Content != "" {
		message = model.Message{Content: b.Content}
	}
	return event.Event{
		Key:          b.Key,
		Type:         b.Type,
		Timestamp:    time.UnixMilli(b.Timestamp),
		Meta:         meta,
		Message:      message,
		Owner:        b.Owner,
		Source:       b.Source,
		Target:       b.Target,
		InvocationID: b.InvocationID,
		TaskID:       b.TaskID,
		InstanceID:   b.InstanceID,
		EpisodeID:    b.EpisodeID,
		DecisionID:   b.DecisionID,
		AttemptID:    b.AttemptID,
		CallID:       b.CallID,
		ApprovalID:   b.ApprovalID,
	}
}

// emptyMessage reports whether one native message carries no payload at all:
// no role, no content, no tool identity, no reasoning and no parts, so the
// content of a light body can fill it without overwriting anything.
func emptyMessage(msg model.Message) bool {
	return msg.Role == "" && msg.Content == "" && msg.ToolID == "" && msg.ToolName == "" &&
		msg.ReasoningContent == "" && msg.ReasoningSignature == "" &&
		len(msg.ContentParts) == 0 && len(msg.ToolCalls) == 0
}

// SameFact reports whether one body names the same fact as another: one
// identity stands for one immutable fact, so a second commit under an identity
// is answered as the fact it already names only when nothing of the fact
// itself moved. The stamps a commit path adds beside a fact — the record time,
// the invocation a delivery minted and the runtime instance — describe the
// commit rather than the fact and decide nothing here; the format and the
// retention policy describe the reference registered beside it, likewise.
func (b EventBody) SameFact(other EventBody) bool {
	left, err := b.canonical()
	if err != nil {
		return false
	}
	right, err := other.canonical()
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

// canonical returns the encoded fact content of one body: the body the event
// behind it projects to with every commit stamp and reference registration
// removed, so two bodies compare as the facts they name and never as two
// commits or two references beside them. A body the earlier writer committed
// and the full body of the same content project to the same bytes, because the
// projection runs through the event the body restores.
func (b EventBody) canonical() ([]byte, error) {
	body, err := BodyFromEvent(b.Event(), b.Format, b.RetentionPolicy)
	if err != nil {
		return nil, err
	}
	body.Timestamp = 0
	body.InvocationID = ""
	body.InstanceID = ""
	body.Format = ""
	body.RetentionPolicy = ""
	return body.Encode()
}
