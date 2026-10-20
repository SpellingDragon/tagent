package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// RequestChain is the causal chain identity one assembled request belongs to:
// the partition and the session the projected facts were replayed under. It is
// part of the assembly boundary, so one user's session chain is never joined
// into another's by a shared partition.
type RequestChain struct {
	// PartitionID is the partition the chain's facts are filed under.
	PartitionID int
	// SessionID is the session the chain belongs to; empty is refused by name.
	SessionID event.SessionID
}

// ErrRequestChainSessionRequired reports a request assembly under a chain that
// names no session: assembling one would join every user of the partition into
// one transcript.
var ErrRequestChainSessionRequired = errors.New("agent: projected request assembly needs a named session chain")

// RequestBoundary is the material one request boundary assembles its prefix
// from: the agent whose request it is, the causal chain the replay is read
// under, the borrowed binding the fixed file contents are read for and whether
// the request is the closing round of its drive.
type RequestBoundary struct {
	// Agent is the agent whose request is being assembled.
	Agent event.AgentID
	// Chain is the causal chain the projected replay belongs to.
	Chain RequestChain
	// Binding is the borrowed binding generation the request runs on.
	Binding state.AgentBinding
	// FinalRound marks the request of a drive's closing round: the round the
	// model delivers in after its budget is spent, whose tool calls are admitted
	// nowhere. It stands false for every ordinary request.
	FinalRound bool
}

// BoundaryView is what the request boundary settled on: the prefix messages
// the request carries in front of the transcript, in the fixed layout the
// request face assembles, and the content version the request runs on. An
// empty PromptVersion keeps the version the binding already carries: a
// binding that names no files fixes no content version of its own.
type BoundaryView struct {
	// Messages are the prefix messages of the request, in fixed layout order.
	Messages []model.Message
	// PromptVersion is the content version the request boundary fixed.
	PromptVersion string
}

// RequestFace assembles the request-boundary material of one decision: the
// fixed file contents read fresh at this boundary, the skill bodies already
// loaded on the binding beside them and the replayed projection of the named
// session chain. The prefix follows one fixed layout: the fixed file contents
// first — the prompt and skill bodies the binding names, in the structural
// order the binding carries — then the skill bodies already loaded on the
// binding's tool resources beside them (skill-name order, once each), then
// the replayed projection of the session chain (each presented entry under
// the frozen event-type→role table), then the transcript the drive
// accumulates behind the prefix. The boundary material enters the actual
// GenerateContent request at the loop, and no projection entry shape is
// mirrored into this package: the face crosses built-in, event, model and
// state types only, so the composition root attaches it with no package edge
// into the projection layer, the same stitching the recording face uses. A
// chain that names no session is refused by name through
// ErrRequestChainSessionRequired; a nil face keeps the request at its
// transcript alone.
// The redline list (3 rules):
// MUST assemble the prefix in the fixed layout from contents read fresh at this boundary.
// MUST refuse a chain that names no session by name.
// MUST NOT cross any package edge into the projection layer.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
type RequestFace func(ctx context.Context, boundary RequestBoundary) (BoundaryView, error)

// ErrProjectionTypeUnmapped reports a projection entry whose frozen type the
// role table does not declare: the mapping from a presented entry type to the
// role it renders as is one-to-one over the declared types, so a type outside
// that table is refused by name instead of rendering under a role no declaration
// gave it.
var ErrProjectionTypeUnmapped = errors.New("agent: the projection type maps to no message role")

// projectionTypeRecoveryHint, projectionTypeSegmentFold and
// projectionTypeHistorySummary are the projection product words the replay layer
// mints for the material it renders itself: a recovery hint answering a budget
// cut, the folded process detail of the newest band, and the history summary of
// the older bands. They name presented entries of the projection, never a frozen
// event type, and they are spelled here as the frozen words they are so this
// package stays clear of the projection layer.
const (
	projectionTypeRecoveryHint   = "recovery_hint"
	projectionTypeSegmentFold    = "segment_fold"
	projectionTypeHistorySummary = "history_summary"
)

// RoleOf reports the native message role one presented projection entry renders
// as on the replay path, keeping the transcript shape the event loop speaks. The
// table is the whole of the mapping and it is single-valued: every declared entry
// type names exactly one role, and a type the table does not declare is refused
// by ErrProjectionTypeUnmapped rather than rendered under a fallback role.
//
// The settled result and the replay products each render under the one role their
// entry type declares:
// - a replayed settled fact is history replayed as it stood, so it renders under the tool role its result fact answered a call with
// - the live settled observation a dispatch feeds the next decision is input of the range, and it renders under its call identity (see observationOf)
// - the recovery hint, the folded segment and the history summary render as user input material, the history a range reads
func RoleOf(eventType string) (model.Role, error) {
	switch eventType {
	case event.TypeExternalInput, event.TypeContextCompress, event.TypeFeedback, event.TypeGovernance,
		projectionTypeRecoveryHint, projectionTypeSegmentFold, projectionTypeHistorySummary:
		return model.RoleUser, nil
	case event.TypeThinkingPlan, event.TypeAgentOutput:
		return model.RoleAssistant, nil
	case event.TypeActionCommand, event.TypeTaskSettled:
		return model.RoleTool, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrProjectionTypeUnmapped, eventType)
	}
}
