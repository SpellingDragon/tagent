// 契约: docs/FACT-VOCABULARY.md#families
//
// The model replacement family: the judgment that one agent's model reference
// moves from the version in effect to another version, stated as the fact it is
// — which agent, which versions, what source judged it and the result the
// producer states — and never as the instruction that rebuilds and installs a
// model. A producer files the replacement it judged, the lease that serves the
// agent reads it and runs its own rebuild, publication and installation steps.
// A repeated judgment keeps the identity and the first filing moment of the
// replacement already on record, so one move of one agent carries one fact
// however often it is judged.
//
// The configuration change is one source of the fact, never the fact itself:
// the source stamp names what judged the move ("config" for a declaration
// change) and another source states its own stamp without a second type.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ModelReplacementType is the frozen type of a model replacement fact.
const ModelReplacementType = "model.replacement"

// ModelReplacementSourceConfig is the source stamp of a replacement judged by a
// declaration change: the agent's declared model reference moved and the
// configuration reload filed the fact.
const ModelReplacementSourceConfig = "config"

// ModelReplacementResultRequested is the result a producer states when it files
// a move for the lease to apply: the fact records the judgment, and the lease
// records whether the replacement actually took effect in its own history.
const ModelReplacementResultRequested = "requested"

// MetaKeyModelFromVersion, MetaKeyModelToVersion and MetaKeyModelReplacementResult
// name the metadata keys a model replacement fact carries beside the frozen
// trigger source stamp: the version the move starts from, the version it asks
// for and the result the producer states.
const (
	// MetaKeyModelFromVersion names the version the move starts from.
	MetaKeyModelFromVersion = "model_from_version"
	// MetaKeyModelToVersion names the version the move asks for.
	MetaKeyModelToVersion = "model_to_version"
	// MetaKeyModelReplacementResult names the result the producer states.
	MetaKeyModelReplacementResult = "model_replacement_result"
)

// ModelReplacement is one model replacement fact: the judgment that one agent's
// model reference moves from one version to another. It names the agent the
// fact is owned by, the version it starts from, the version it asks for, the
// source that judged the move and the result the producer states, and it is
// immutable once filed — the same judgment files the same identity, and a retry
// never refiles it under a second one.
type ModelReplacement struct {
	// ID is the identity of the judgment: the content-addressed key the
	// replacement is quoted by, derived from every field that states the move.
	ID EventID
	// AgentID names the agent whose model reference moves; the fact is owned by
	// this agent.
	AgentID AgentID
	// FromVersion is the version the move starts from, as the rendered identity
	// name it was declared under.
	FromVersion string
	// ToVersion is the version the move asks for: the version a rebuilt model
	// is installed under, never a rendered name.
	ToVersion string
	// Source names what judged the move ("config" for a declaration change).
	Source string
	// Result states the result the producer records for the move.
	Result string
	// At is the moment the judgment was filed.
	At time.Time
}

// ModelReplacementFor returns the replacement fact one judgment files: the
// identity is content-addressed over every field that states the move, so the
// same judgment always answers the same identity and a re-judgment adds no
// second fact. The filing moment stays zero for the log to stamp on first
// filing.
func ModelReplacementFor(agentID AgentID, from, to, source, result string) ModelReplacement {
	sum := sha256.Sum256([]byte(string(agentID) + "\x00" + from + "\x00" + to + "\x00" + source + "\x00" + result))
	return ModelReplacement{
		ID:          EventID("mrp-" + hex.EncodeToString(sum[:8])),
		AgentID:     agentID,
		FromVersion: from,
		ToVersion:   to,
		Source:      source,
		Result:      result,
	}
}

// Event renders the replacement fact as the event it commits as: the frozen
// type, the fact identity as the event key, the agent as the owner, the trigger
// source stamp the observation gate reads and the versions and result in the
// event metadata. The message carries the one-line read of the move, so a
// committed replacement is readable in the mapped sequence exactly as it was
// filed.
func (r ModelReplacement) Event(at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	summary := "model replacement agent=" + string(r.AgentID) + " from=" + r.FromVersion + " to=" + r.ToVersion + " source=" + r.Source
	return Event{
		Key:       r.ID,
		Type:      ModelReplacementType,
		Timestamp: at,
		Meta: map[string]string{
			MetaKeyEventSummary:           summary,
			MetaKeyTriggerSource:          r.Source,
			MetaKeyModelFromVersion:       r.FromVersion,
			MetaKeyModelToVersion:         r.ToVersion,
			MetaKeyModelReplacementResult: r.Result,
		},
		Message: model.Message{Role: model.RoleSystem, Content: summary},
		Owner:   Address{AgentID: r.AgentID},
	}
}

// FactKey returns the replacement's identity as a fact key: the
// content-addressed key one judgment is quoted by, unchanged by every re-filing
// of that judgment.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r ModelReplacement) FactKey() string { return string(r.ID) }

// FactType returns the frozen type a model replacement is read as.
func (r ModelReplacement) FactType() string { return ModelReplacementType }

// The model replacement family satisfies the fact category contract.
var _ Fact = ModelReplacement{}
