// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// The body schema one committed event carries through the segment store: the
// canonical identity and every field the event itself carries — the metadata
// projection, the native payload and the lineage identifiers — and the owner,
// format and retention policy the reference beside it is registered under. The
// light fields a query filters and matches on are not stored beside those: the
// summary is read out of the metadata projection and the content out of the
// native payload when the record is built, so no field of a committed event is
// held twice and no second copy of it can drift from the event it names. A
// stored body is self-describing: it decodes back into the query record, the
// recall reference and the very event that committed, so a committed body
// becomes queryable and readable without a second registry and a fact restored
// across a restart keeps every field it committed with. One body format exists:
// bytes an earlier writer committed under another identity field carry no
// identity under this schema and are refused by name, because that state is
// rebuilt cold instead of migrated. The original text behind the event stays in
// the original store and is read byte for byte through the scoped path.
package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
// its event time on the millisecond axis, the whole metadata projection, native
// payload and lineage identifiers the event carries, and the owner, format and
// retention policy the reference beside it is registered under. Nothing the
// event already carries is copied into a light field of its own, so the body
// keeps one copy of every field and the record of a query derives the rest. An
// event carrying no key is refused by name.
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
// One body format exists, and the shape of it is decided by the keys the bytes
// carry, never by the values a decode leaves behind. Bytes an earlier writer
// committed spell their identity as event_key beside event_type and
// event_summary, which is no identity under this schema: they are refused by
// name so an event key can never be invented from the bytes beside it. Bytes of
// the same earlier writer's other shape carry key, content and summary instead,
// and are refused by the same rule at the other end: a body whose top level
// carries no message key is not this schema and is refused by name, so its
// content is refused rather than decoded into an empty payload beside a key
// that reads back as if it committed nothing. The judgment is made on the raw
// top-level keys because the message field of this schema has no way to say
// "absent": an audit fact that commits an empty message is a legal body and its
// zero value is the very thing the bytes of the older shape would decode to, so
// a zero-value judgment would refuse a fact this schema holds. Such state is not
// migrated — a store holding it is rebuilt cold (the kv.json precedent), so the
// refusal names the bytes that must be dropped rather than standing in for a
// migration path.
func DecodeEventBody(body []byte) (EventBody, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return EventBody{}, fmt.Errorf("%w: %w", ErrEventBody, err)
	}
	var decoded EventBody
	if err := json.Unmarshal(body, &decoded); err != nil {
		return EventBody{}, fmt.Errorf("%w: %w", ErrEventBody, err)
	}
	if decoded.Key == "" {
		return EventBody{}, fmt.Errorf("%w: the body carries no event key", ErrEventBody)
	}
	if _, carried := fields["message"]; !carried {
		return EventBody{}, fmt.Errorf("%w: the body is not this schema: it carries no message", ErrEventBody)
	}
	return decoded, nil
}

// Record returns the query record of the body under the partition and segment
// window its event is committed into. The summary and the content of the record
// are derived on read — the summary out of the metadata projection, the content
// out of the native payload — because the body stores neither beside the event
// it carries: a record therefore reads the very fields the event committed with,
// and no second copy of them can stand for them.
func (b EventBody) Record(partitionID int, windowTS int64) EventRecord {
	return EventRecord{
		Key:         b.Key,
		PartitionID: partitionID,
		Type:        b.Type,
		Summary:     b.Meta[event.MetaKeyEventSummary],
		Content:     b.Message.Content,
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
// identifiers — read back exactly as it stands. Nothing is backfilled into it:
// the body keeps no light field beside the event, so the summary stays the one
// entry of the metadata projection and the content the one text of the native
// payload, and a field the bytes never carried stays empty instead of an
// invented one.
func (b EventBody) Event() event.Event {
	meta := make(map[string]string, len(b.Meta))
	for key, value := range b.Meta {
		meta[key] = value
	}
	return event.Event{
		Key:          b.Key,
		Type:         b.Type,
		Timestamp:    time.UnixMilli(b.Timestamp),
		Meta:         meta,
		Message:      b.Message,
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

// SameFact reports whether one body names the same fact as another: one
// identity stands for one immutable fact, so a second commit under an identity
// is answered as the fact it already names only when nothing of the fact
// itself moved. The stamps the commit protocol generates beside a fact — the
// record time, the invocation identity, the runtime instance, the format and
// the retention policy — describe the commit rather than the fact and decide
// nothing here; the format and the retention policy describe the reference
// registered beside it, likewise. The stamps a caller may declare — the
// source, the target and the task — join the judgment only where both sides
// declare them: a stamp one body left unstated matches anything, and two
// different declared stamps never name the same fact.
func (b EventBody) SameFact(other EventBody) bool {
	left, err := b.canonical()
	if err != nil {
		return false
	}
	right, err := other.canonical()
	if err != nil {
		return false
	}
	if !bytes.Equal(left, right) {
		return false
	}
	return sameRangeStamp(b.Source, b.Owner, other.Source, other.Owner) &&
		sameRangeStamp(b.Target, b.Owner, other.Target, other.Owner) &&
		sameTaskStamp(b.TaskID, other.TaskID)
}

// sameRangeStamp reports whether one caller-declared range stamp — a source or
// a target — names the same range as the other: a stamp one side left unstated
// is the wildcard of the comparison and matches anything, and two stamps both
// declared answer equal only when they name the same address. The owner of the
// fact is the backfill default of the commit paths, so a stamp reading the
// owner is exactly an unstated one.
func sameRangeStamp(left, leftOwner, right, rightOwner event.Address) bool {
	if unstatedRange(left, leftOwner) || unstatedRange(right, rightOwner) {
		return true
	}
	return left == right
}

// unstatedRange reports whether one source or target stamp names no range of
// its own: the zero address, or the owner of the fact itself — the value the
// commit paths backfill a stamp the caller left unstated to.
func unstatedRange(stamp, owner event.Address) bool {
	return stamp.IsZero() || stamp == owner
}

// sameTaskStamp reports whether one caller-declared task stamp names the same
// task as the other: a stamp one side left empty is the wildcard of the
// comparison and matches anything, and two stamps both declared answer equal
// only when they name the same task.
func sameTaskStamp(left, right event.TaskID) bool {
	if left == "" || right == "" {
		return true
	}
	return left == right
}

// canonical returns the encoded fact content of one body: the body the event
// behind it projects to with every protocol-generated stamp and reference
// registration removed — the record time, the invocation identity, the runtime
// instance, the format and the retention policy — beside the caller-declared
// range and task stamps, which SameFact judges pairwise instead. Two bodies
// compare as the facts they name and never as two commits or two references
// beside them. The projection runs through the event the body restores, so the
// comparison is over the event itself and not over any copy of it stored beside.
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
	body.Source = event.Address{}
	body.Target = event.Address{}
	body.TaskID = ""
	return body.Encode()
}
