// 契约: docs/wiki/platform/org-hot-reload.md#generations
// 契约: docs/FACT-VOCABULARY.md#fact-category
//
// The configuration change family: the judgment that one dimension of one
// agent's configuration moved onto a published generation, stated as the fact it
// is — which agent, which dimension, the generation it landed in and what
// changed — and never as the instruction that applies the change. A reload states
// the judgment once per agent dimension that moved: the prompt and binding
// dimensions read their move from the description, the model dimension carries
// its version move in the payload beside the exported fields, rendered under the
// replacement metadata keys, the file dimension names the file and the content
// version it observed, and the organization dimensions state the structure the
// declaration moved — one five-field fact per dimension, never a second family.
//
// A configuration change is control-plane material: it names the change, not a
// transcript line, so the frozen request role table does not map its type and a
// drive never reads it as input. The fact is read by its identity and its
// metadata, exactly as the publication ledger it reports beside is read.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TypeConfigChange is the frozen type of a configuration change fact.
const TypeConfigChange = "config_change"

// ConfigChangeSourceConfig is the source stamp of a change judged by a
// declaration reload: the configuration source observed the move and the host
// filed the fact when the new generation settled. The stamp is a vocabulary
// constant, never a field of the fact: every configuration change of this family
// is judged by the same source, so a reader compares it with the trigger source
// stamp the event commit carries.
const ConfigChangeSourceConfig = "config"

// ConfigChangeDimensionPrompt, ConfigChangeDimensionBinding,
// ConfigChangeDimensionModel and ConfigChangeDimensionFile name the per-agent
// configuration dimensions this family states: the prompt sources one agent is
// assembled from, the remaining declaration binding of that agent, the model
// reference a lease serves the agent through and the declared file content a
// request boundary reads.
//
// ConfigChangeDimensionOrgAgents, ConfigChangeDimensionOrgEntry and
// ConfigChangeDimensionOrgAssembly name the organization dimensions the
// declaration diff states: the declared agent set, the entry agent the
// organization speaks to and the delegation assembly the declaration wires. The
// organization is the control plane, so a move of its structure is a change of
// this same family under its own dimension instead of a second fact type.
const (
	// ConfigChangeDimensionPrompt names the prompt sources the agent reads.
	ConfigChangeDimensionPrompt = "prompt"
	// ConfigChangeDimensionBinding names the declaration binding of the agent
	// outside its prompt sources and model reference.
	ConfigChangeDimensionBinding = "binding"
	// ConfigChangeDimensionModel names the model reference the agent's lease
	// serves: the move carries the versions between it in the fact payload.
	ConfigChangeDimensionModel = "model"
	// ConfigChangeDimensionFile names the declared file content the request
	// boundary read: the fact carries which file and which content version it
	// observed, and the boundary projects the content back from it.
	ConfigChangeDimensionFile = "file"
	// ConfigChangeDimensionOrgAgents names the declared agent set: a member
	// added to or removed from the declaration.
	ConfigChangeDimensionOrgAgents = "org.agents"
	// ConfigChangeDimensionOrgEntry names the entry agent the organization
	// speaks to.
	ConfigChangeDimensionOrgEntry = "org.entry"
	// ConfigChangeDimensionOrgAssembly names the config-declared delegation
	// assembly: the agent references one declaration wires — the declared target,
	// the tool name it is reached by, its description file and its event params.
	ConfigChangeDimensionOrgAssembly = "org.assembly"
)

// MetaKeyConfigChangeDimension and MetaKeyConfigChangeGeneration name the
// metadata keys a configuration change fact carries beside the frozen trigger
// source stamp: the dimension that moved and the generation the publication
// assigned. The generation is the publication identity, never a content
// fingerprint, so a reader compares it with the publication ledger directly.
const (
	// MetaKeyConfigChangeDimension names the configuration dimension that moved.
	MetaKeyConfigChangeDimension = "config_change_dimension"
	// MetaKeyConfigChangeGeneration names the generation the change landed in.
	MetaKeyConfigChangeGeneration = "config_change_generation"
)

// ConfigChange is one configuration change fact: the judgment that one
// dimension of one agent's configuration moved onto a published generation. It
// names the agent the fact is owned by, the dimension that moved, the generation
// the publication assigned and the change it describes, and it is immutable once
// filed — the same judgment files the same identity, and a retry never refiles
// it under a second one.
//
// The five exported fields state every configuration change. The model
// dimension's version move is carried in the unexported payload beside them and
// rendered by Event under the replacement metadata keys, so the model move adds
// no exported field and no second fact family: a reader of a model change reads
// the same five-field fact under the model dimension.
type ConfigChange struct {
	// ID is the identity of the judgment: the content-addressed key the change
	// is quoted by, derived from every field that states the move.
	ID EventID
	// AgentID names the agent whose declared dimension moved; the fact is owned
	// by this agent.
	AgentID AgentID
	// Dimension names the configuration dimension that moved.
	Dimension string
	// Generation is the publication identity the change landed in.
	Generation uint64
	// Description states the change the fact reports.
	Description string

	// model carries the version move of the model dimension: the version the
	// move starts from, the version it asks for and the result the producer
	// states. It stays unexported so the dimension adds no exported field to the
	// five that state every configuration change; Event renders it under the
	// model replacement metadata keys, which is where a lease observer reads it.
	model configChangeModel
}

// configChangeModel is the payload of the model dimension: the versions one
// model reference moves between and the result the producer states for the move.
// The zero payload names no model move, so a fact of any other dimension carries
// no replacement metadata.
type configChangeModel struct {
	from   string
	to     string
	result string
}

// ConfigChangeFor returns the change fact one reload files for a dimension
// stated by its description: the identity is content-addressed over the agent,
// the dimension, the generation, the description and the frozen source, so the
// same judgment always answers the same identity and a re-judgment adds no
// second fact. The filing moment stays zero for the log to stamp on first
// filing.
func ConfigChangeFor(agentID AgentID, dimension string, generation uint64, description string) ConfigChange {
	sum := sha256.Sum256([]byte(string(agentID) + "\x00" + dimension + "\x00" + strconv.FormatUint(generation, 10) + "\x00" + description + "\x00" + ConfigChangeSourceConfig))
	return ConfigChange{
		ID:          EventID("cfg-" + hex.EncodeToString(sum[:8])),
		AgentID:     agentID,
		Dimension:   dimension,
		Generation:  generation,
		Description: description,
	}
}

// ConfigChangeModelFor returns the change fact of one model reference move: the
// identity is content-addressed over the agent, the model dimension, the
// generation and the versions and result the move states, so the same judgment
// always answers the same identity and a re-judgment adds no second fact. The
// description names the move; the versions and result ride in the payload and
// are rendered under the replacement metadata keys by Event.
func ConfigChangeModelFor(agentID AgentID, generation uint64, from, to, result string) ConfigChange {
	description := "model reference changed: " + from + " -> " + to
	sum := sha256.Sum256([]byte(string(agentID) + "\x00" + ConfigChangeDimensionModel + "\x00" + strconv.FormatUint(generation, 10) + "\x00" + from + "\x00" + to + "\x00" + result + "\x00" + ConfigChangeSourceConfig))
	return ConfigChange{
		ID:          EventID("cfg-" + hex.EncodeToString(sum[:8])),
		AgentID:     agentID,
		Dimension:   ConfigChangeDimensionModel,
		Generation:  generation,
		Description: description,
		model:       configChangeModel{from: from, to: to, result: result},
	}
}

// Event renders the change fact as the event it commits as: the frozen type, the
// fact identity as the event key, the agent as the owner, the trigger source
// stamp the observation gate reads, the dimension and the publication generation
// in the event metadata — and, for the model dimension, the version move under
// the model replacement metadata keys. The message carries the change
// description, so a committed configuration change is readable on the chain
// exactly as it was filed; the projection boundary maps no entry for the type,
// so a model request never reads the judgment as transcript.
func (c ConfigChange) Event(at time.Time) Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	summary := c.Description
	if summary == "" {
		summary = "configuration change agent=" + string(c.AgentID) + " dimension=" + c.Dimension + " generation=" + strconv.FormatUint(c.Generation, 10)
	}
	meta := map[string]string{
		MetaKeyEventSummary:           summary,
		MetaKeyTriggerSource:          ConfigChangeSourceConfig,
		MetaKeyConfigChangeDimension:  c.Dimension,
		MetaKeyConfigChangeGeneration: strconv.FormatUint(c.Generation, 10),
	}
	if c.model != (configChangeModel{}) {
		meta[MetaKeyModelFromVersion] = c.model.from
		meta[MetaKeyModelToVersion] = c.model.to
		meta[MetaKeyModelReplacementResult] = c.model.result
	}
	return Event{
		Key:       c.ID,
		Type:      TypeConfigChange,
		Timestamp: at,
		Meta:      meta,
		Message:   model.Message{Role: model.RoleSystem, Content: summary},
		Owner:     Address{AgentID: c.AgentID},
	}
}

// FactKey returns the change's identity as a fact key: the content-addressed key
// one judgment is quoted by, unchanged by every re-filing of that judgment.
//
// 契约: docs/FACT-VOCABULARY.md#fact-category
func (c ConfigChange) FactKey() string { return string(c.ID) }

// FactType returns the frozen type a configuration change is read as.
func (c ConfigChange) FactType() string { return TypeConfigChange }

// The configuration change family satisfies the fact category contract.
var _ Fact = ConfigChange{}
