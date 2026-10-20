// 契约: docs/wiki/platform/org-hot-reload.md#generations
//
// The heat matrix of the configuration dimensions beside the routing that
// keeps the two consumption paths apart. Every dimension of every sub-agent
// belongs to exactly one channel — the FP generation face, the SRC source-pull
// face, the FILE lazy-read face, or the named RESTART refusal — and one change
// takes effect only on the channel its dimension belongs to.
//
// The two paths never stand in for each other: a structural change runs
// the strict decode → Compile → Prepare → the precondition checks → commit and
// commits only through the sole publication entry, while a file change takes
// effect directly at the request boundary. A structural difference never rides
// the file channel around the precondition checks, and a file change is never
// claimed to have passed them.
package state

import (
	"context"
	"errors"
	"fmt"
)

// Channel names the one heat channel a configuration dimension belongs to.
type Channel int

const (
	// ChannelFP is the FP generation face: a view change takes effect at the
	// next round, a request already under way keeps the generation it took, and
	// the change commits only through the publish precondition gate.
	ChannelFP Channel = iota
	// ChannelSRC is the SRC source-pull face: a parameter change takes effect
	// at the next consumption of that parameter, with no generation assigned.
	ChannelSRC
	// ChannelFile is the FILE lazy-read face: the file is the source of truth
	// and its content is read at the consumption boundary, as the prompt body
	// and the skill body are. No register, no evaluation and no precondition
	// gate stands between the change and its effect.
	ChannelFile
	// ChannelRestart is the named restart refusal: the dimension names a
	// resource that cannot migrate, and refusing the change is the correct
	// behavior.
	ChannelRestart
)

// String reports the channel by the name the matrix registers it under.
func (c Channel) String() string {
	switch c {
	case ChannelFP:
		return "FP"
	case ChannelSRC:
		return "SRC"
	case ChannelFile:
		return "FILE"
	case ChannelRestart:
		return "RESTART"
	}
	return fmt.Sprintf("Channel(%d)", int(c))
}

// Dimension names one configuration dimension of a sub-agent.
type Dimension string

// DimensionModel and the other dimensions of one sub-agent the matrix registers: a structural
// dimension names a view the generation carries, a file dimension names content that is its own
// source of truth, and a restart dimension names a resource that cannot migrate.
const (
	// DimensionModel names the model the agent runs against.
	DimensionModel Dimension = "model"
	// DimensionTools names the tool and delegation references of the agent.
	DimensionTools Dimension = "tools"
	// DimensionPlugins names the ordered plugin pipeline of the agent.
	DimensionPlugins Dimension = "plugins"
	// DimensionTaskPolicy names the task policy applied to the agent's tasks.
	DimensionTaskPolicy Dimension = "task-policy"
	// DimensionResourceRefs names the resource references the agent holds.
	DimensionResourceRefs Dimension = "resource-refs"
	// DimensionMemory names the memory bindings of the agent.
	DimensionMemory Dimension = "memory"
	// DimensionPromptBody names the prompt file body of the agent.
	DimensionPromptBody Dimension = "prompt-body"
	// DimensionSkillBody names the skill file body of the agent.
	DimensionSkillBody Dimension = "skill-body"
	// DimensionRoots names a directory the agent serves from.
	DimensionRoots Dimension = "roots"
	// DimensionEndpoints names a remote endpoint the agent is wired to.
	DimensionEndpoints Dimension = "endpoints"
)

// ChannelRow is one row of the heat matrix: the dimension, the single channel
// it belongs to and the evidence its consumption point carries — a contract
// test name or a code line. A row whose consumption point never reads what it
// registers is a fake hot update and is a defect rather than partial support,
// so a dimension registers its row only beside the consumption point that is
// wired for it.
type ChannelRow struct {
	// Dimension is the configuration dimension the row registers.
	Dimension Dimension
	// Channel is the single channel the dimension belongs to.
	Channel Channel
	// Evidence names the contract test or code line of the consumption point.
	Evidence string
}

// ChannelMatrix returns the registered heat matrix: one row per registered
// dimension beside the channel it belongs to and the evidence of its
// consumption point.
//
// Rows grow with their consumption points: a later change that wires the
// consumption of another dimension adds its row here beside its evidence. The
// model parameter dimension takes the SRC source-pull face (a parameter change
// takes effect at the next consumption) and the mcp-servers dimension takes the
// FILE lazy-read face; neither is registered yet because their consumption
// points land outside this change, and a row carried without a consumption
// point would be a fake hot update. The RESTART rows are registered beside the
// refusal this matrix itself performs: refusing such a change is the correct
// behavior.
func ChannelMatrix() []ChannelRow {
	return []ChannelRow{
		{Dimension: DimensionModel, Channel: ChannelFP, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionTools, Channel: ChannelFP, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionPlugins, Channel: ChannelFP, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionTaskPolicy, Channel: ChannelFP, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionResourceRefs, Channel: ChannelFP, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionMemory, Channel: ChannelFP, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionPromptBody, Channel: ChannelFile, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionSkillBody, Channel: ChannelFile, Evidence: "TestFileAndStructuralChange"},
		{Dimension: DimensionRoots, Channel: ChannelRestart, Evidence: "state/channels.go Route"},
		{Dimension: DimensionEndpoints, Channel: ChannelRestart, Evidence: "state/channels.go Route"},
	}
}

// ChannelOf reports the single channel one registered dimension belongs to and
// whether the matrix registers that dimension at all.
func ChannelOf(d Dimension) (Channel, bool) {
	for _, row := range ChannelMatrix() {
		if row.Dimension == d {
			return row.Channel, true
		}
	}
	return 0, false
}

var (
	// ErrDimensionUnknown reports a change touching a dimension the matrix does
	// not register: the change takes no path until the dimension is registered
	// beside a consumption point.
	ErrDimensionUnknown = errors.New("state: the change touches no registered configuration dimension")
	// ErrChannelMismatch reports a change arriving through a channel other than
	// the one its dimension belongs to. The matrix gives each dimension exactly
	// one channel, and no change chooses a second one to take effect on.
	ErrChannelMismatch = errors.New("state: the change arrives through a channel its dimension does not belong to")
	// ErrStructuralViaFile reports the named case of ErrChannelMismatch the
	// matrix forbids outright: a structural change arriving through the file
	// channel. The structural path runs the precondition checks first, and a
	// structural difference never takes effect through the file channel around
	// them. It is a named case of ErrChannelMismatch, so a caller matching the
	// refusal of any misrouted change matches it too.
	ErrStructuralViaFile = fmt.Errorf("%w: a structural change never takes effect through the file channel", ErrChannelMismatch)
	// ErrRestartRefused reports a change to a dimension the matrix declares
	// RESTART: the resource it names cannot migrate, and refusing the change is
	// the correct behavior rather than a failure to support it.
	ErrRestartRefused = errors.New("state: the dimension names a resource that cannot migrate and is refused until restart")
	// ErrRouterFileSourceMissing reports a file read through a router that
	// carries no file source.
	ErrRouterFileSourceMissing = errors.New("state: the router carries no file source")
	// ErrRouterGateMissing reports a structural change routed through a router
	// that carries no publication entry to gate it.
	ErrRouterGateMissing = errors.New("state: the router carries no publication entry")
)

// Change is one submitted configuration change attempt: the dimension it
// touches, the channel it arrives through and the source origin it was written
// in, so a refusal stays locatable beside the normalized line it names.
type Change struct {
	// Dimension is the configuration dimension the change touches.
	Dimension Dimension
	// Via is the channel the change arrives through.
	Via Channel
	// Source is the source file the change was written in.
	Source string
	// Line is the normalized line within that source.
	Line int
}

// Route decides the one channel a submitted change takes effect on. The matrix
// registers each dimension beside exactly one channel, so a change arriving on
// any other channel is refused by name — a structural change never rides the
// file channel — and a change to a RESTART dimension is refused outright,
// because refusing it is the correct behavior. A change the matrix registers
// nowhere is refused by name as well and takes no path at all.
func Route(change Change) (Channel, error) {
	channel, ok := ChannelOf(change.Dimension)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrDimensionUnknown, change.Dimension)
	}
	if channel == ChannelRestart {
		return 0, fmt.Errorf("%w: %s", ErrRestartRefused, change.Dimension)
	}
	if change.Via != channel {
		if channel == ChannelFP && change.Via == ChannelFile {
			return 0, fmt.Errorf("%w: %s", ErrStructuralViaFile, change.Dimension)
		}
		return 0, fmt.Errorf("%w: %s", ErrChannelMismatch, change.Dimension)
	}
	return channel, nil
}

// Router routes one submitted change onto the single path its dimension
// belongs to: the FILE face takes effect at the request boundary with no gate
// in its way, and the structural face commits only through the publish
// precondition gate of the sole publication entry — for a fresh structural
// change and for a rollback alike. The two paths never stand in for each
// other, and every refusal keeps its cause chain: the source file, the
// normalized line and the named reason.
type Router struct {
	files *FileSource
	gate  *Publisher
}

// NewRouter returns the router over the given file source and publication
// entry. Either may be nil for a caller that only uses the other face; a read
// without a file source and a structural change without a gate are each
// refused by name.
func NewRouter(files *FileSource, gate *Publisher) *Router {
	return &Router{files: files, gate: gate}
}

// Read takes the FILE face: the content is read and fixed at the request
// boundary and takes effect at once — no register, no evaluation and no
// precondition gate stands in its way, and no structural generation is
// assigned or advanced.
func (r *Router) Read(ctx context.Context, prompt PromptSource) (FileSnapshot, error) {
	if r == nil || r.files == nil {
		return FileSnapshot{}, NewCause("", 0, "read the file content at the request boundary", ErrRouterFileSourceMissing)
	}
	return r.files.Fixate(ctx, prompt)
}

// Commit takes the structural face: the change is routed by the matrix first,
// then committed through the publish precondition gate of the sole
// publication entry. A refusal anywhere leaves the effective binding as it was
// and reports the failure beside its cause chain — the source file, the
// normalized line and the named reason — never as a generic reload failure.
func (r *Router) Commit(ctx context.Context, change Change, cand Candidate) (PublishOutcome, error) {
	return r.structural(ctx, change, cand, (*Publisher).Publish)
}

// Rollback takes the structural face as well: a rollback rebuilds the previous
// full configuration as one more generation through the same gate and the same
// publication entry, and a rollback arriving through the file channel is
// refused like any other structural change.
func (r *Router) Rollback(ctx context.Context, change Change, cand Candidate) (PublishOutcome, error) {
	return r.structural(ctx, change, cand, (*Publisher).Rollback)
}

// structural runs one structural change through the routing decision and the
// publication entry it belongs to, keeping the cause chain of every failure it
// meets on the way.
func (r *Router) structural(ctx context.Context, change Change, cand Candidate, commit func(*Publisher, context.Context, Candidate) (PublishOutcome, error)) (PublishOutcome, error) {
	if r == nil || r.gate == nil {
		return PublishOutcome{}, NewCause(change.Source, change.Line, "commit the structural change through the publish precondition gate", ErrRouterGateMissing)
	}
	if _, err := Route(change); err != nil {
		return PublishOutcome{}, NewCause(change.Source, change.Line, "route the change onto the channel its dimension belongs to", err)
	}
	out, err := commit(r.gate, ctx, cand)
	if err != nil {
		return PublishOutcome{}, NewCause(change.Source, change.Line, "commit the structural change through the publish precondition gate", err)
	}
	return out, nil
}
