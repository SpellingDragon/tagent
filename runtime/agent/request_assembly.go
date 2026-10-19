package agent

import (
	"context"
	"errors"

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

// RoleOf reports the native message role one frozen event type renders as on
// the replay path, keeping the transcript shape the event loop speaks.
//
// The two settled-result paths are deliberately different and never conflated:
// on this replay path a settled fact renders under the tool role its result
// fact originally answered a call with, while the live settled observation a
// dispatch feeds the next decision renders as user input material under its
// call identity (see observationOf). One is history replayed as it stood, the
// other is fresh input of the range.
func RoleOf(eventType string) model.Role {
	switch eventType {
	case event.TypeThinkingPlan, event.TypeAgentOutput:
		return model.RoleAssistant
	case event.TypeActionCommand, event.TypeTaskSettled:
		return model.RoleTool
	default:
		return model.RoleUser
	}
}
