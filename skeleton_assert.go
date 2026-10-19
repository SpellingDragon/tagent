package tagent

import (
	"context"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Compile-time assertions for the public face of the composition root: the sole
// assembly entry, the faces the host drives the assembled run through, and the
// exact shape of every declaration the root exports.
// The minimal public assembly entry keeps its declared shape: the contributed
// native pieces of one agent, one invocation's request, and the constructor and
// the two entries a caller drives the assembled run through.
var (
	_ = NativeAgent{}

	_ = InvokeRequest{}

	_ func(context.Context, *config.Declaration, map[event.AgentID]NativeAgent, ...Option) (*Run, error) = Assemble

	// The post-commit observation face is attached through its own option, so
	// the composed observer reads committed facts and observes nothing else.
	_ func(plugin.Observer) Option = WithTelemetry

	_ plugin.ObservationSink = plugin.Observer(nil)

	_ func(*Run, context.Context, InvokeRequest) (agent.Turn, error) = (*Run).Invoke

	_ func(*Run, context.Context, *config.Declaration, map[event.AgentID]NativeAgent) (state.PublishOutcome, error) = (*Run).Publish
)

// The public SDK entries the host drives the assembled runtime through keep
// their declared shapes: the close face of the execution domain beside the
// bounded diagnostic reading of the query projection, and the thin command
// shell over both.
var (
	_ func(*Run, context.Context, agent.CloseRequest) (agent.DrainReport, error) = (*Run).Close

	_ func(*Run, context.Context, query.ListFilter) (query.Snapshot, error) = (*Run).Query

	// The After pass presentations settle on the diagnostic read face of the
	// assembled run, so the host reads the rendered views and the visible
	// observation gaps beside the committed facts.
	_ func(*Run) []view.Presentation = (*Run).Presentations

	// The safe request snapshots settle on the diagnostic read face of the
	// assembled run, so the host reads the fixed copy each model request was
	// made on beside the decision whose request it was fixed for.
	_ func(*Run) []RequestSnapshotRecord = (*Run).RequestSnapshots

	_ = RequestSnapshotRecord{Decision: "", Snapshot: rl.RequestSnapshot{}}
)

// The read port the assembled run answers the inventory through, the commit
// carrier its tasks are listed from and the task face its close settles through
// each satisfy the port the owner packages declare.
var (
	_ query.Reading         = runReading{}
	_ task.Commit           = (*runCommit)(nil)
	_ agent.DelegationTasks = closeFace{}
)

// The training record production face of the execution loop is satisfied by the
// record pipeline's adapter across the package boundary: the composition root
// is where both sides meet, so the zero-edge stitching of the face is proven
// here and nowhere else.
var _ agent.Recorder = rl.RecorderAdapter{}

// The session transcript face of the execution loop is satisfied by the run's
// own session history ledger across the package boundary: the composition root
// is where both sides meet, so the history source the loop reads one
// invocation's shared transcript from is proven here.
var _ agent.HistoryFace = (*Run)(nil)

// The safe request snapshot face of the execution loop is satisfied by the
// run's own request-boundary snapshot production across the package boundary:
// the composition root is where both sides meet, so the snapshot the loop
// fixes before its model calls is proven here.
var _ agent.RequestSnapshotFace = (*Run)(nil).snapshotRequest
