// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package tagent

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// AgeFoldPluginName is the stable name the age fold plugin instance answers.
const AgeFoldPluginName = "age-fold"

// FoldWindow is the replay window one session chain registered: the model window
// the chain is replayed into, in tokens, and the fold settings the declaration
// gave that window. The unset settings read their declaration defaults when the
// window is normalized, so a declaration naming only max_tokens folds under the
// default share of its own window.
type FoldWindow struct {
	// MaxTokens is the model window the chain is replayed into, in tokens.
	MaxTokens int
	// CompressThreshold is the share of that window the replayed projection may
	// fill before the age fold runs.
	CompressThreshold float64
	// KeepRecentTasks is the number of recent task turns the age fold keeps whole.
	KeepRecentTasks int
}

// VolumeThresholdBytes reports the rendered volume a projection of this window
// may carry before an age fold is due, in bytes: the share of the window the
// declaration allows, converted at the one character-to-token ratio the budget
// assembly packs requests under, so the fold fires where the assembly would
// start degrading the projection rather than after it already had. A window
// naming no tokens reports no threshold, and a fold read under it never fires.
func (w FoldWindow) VolumeThresholdBytes() int {
	if w.MaxTokens <= 0 {
		return 0
	}
	ratio := w.CompressThreshold
	if ratio <= 0 || ratio > 1 {
		ratio = config.DefaultCompressThreshold
	}
	return int(float64(w.MaxTokens*replay.BudgetCharsPerToken) * ratio)
}

// SessionFolds is the session face the age fold plugin observes one commit
// through: the session chain a committed fact belongs to, the projection and the
// window that chain registered, and the filing of the compression judgment the
// fold answered. The composition root implements the face over the live ledger
// and the replay registrations of its run, so the plugin holds no chain and no
// ledger of its own.
//
// The redline list (3 rules):
// MUST answer a fact no session chain owns as unowned.
// MUST answer the projection and the window the session registered, never one the plugin invents.
// MUST NOT fold a session whose registered window names no positive token window.
type SessionFolds interface {
	// SessionOf reports the session chain one committed fact belongs to.
	SessionOf(ctx context.Context, id event.EventID) (event.SessionID, bool)
	// ProjectionOf reports the projection one session registered for replay
	// beside the window it was registered under.
	ProjectionOf(ctx context.Context, session event.SessionID) (*replay.ReplayProjection, FoldWindow, bool)
	// FileCompressRequest files one compression judgment the fold answered on the
	// chain of the session whose material was judged.
	FileCompressRequest(ctx context.Context, session event.SessionID, request event.CompressRequest) error
}

// AgeFoldPlugin folds the projection of the session the committed fact belongs
// to once its rendered volume passes the window threshold: the process detail of
// the older task turns collapses into fold products and the middle band into one
// history summary, so a long session replays into the window it declared instead
// of over it.
//
// The plugin is an instance of the material flow like any other: it reads the
// fact material and answers an observation, so the pipeline dispatches it beside
// the reflection driver and the commit that grew the projection is the trigger.
// How material folds stays the projection's own rule — the plugin judges nothing
// about it. Every judgment the fold answered is filed as a compress request on
// the chain, so why material went is a fact like any other.
type AgeFoldPlugin struct{ sessions SessionFolds }

// The plugin serves the fact→observation port the reflection driver serves, and
// observes every commit the run makes.
var (
	_ plugin.Instance           = (*AgeFoldPlugin)(nil)
	_ plugin.PostCommitObserver = (*AgeFoldPlugin)(nil)
)

// NewAgeFoldPlugin assembles the fold plugin over the session face of one run. A
// nil face answers no session, so an assembly mounted beside none folds nothing
// instead of failing on the first commit.
func NewAgeFoldPlugin(sessions SessionFolds) *AgeFoldPlugin {
	return &AgeFoldPlugin{sessions: sessions}
}

// Name reports the stable name of this plugin instance.
func (p *AgeFoldPlugin) Name() string { return AgeFoldPluginName }

// Port reports the consumption the plugin serves: one committed fact in, one
// observation out. The observation is the fold the fact may trigger.
func (p *AgeFoldPlugin) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

// ObserveCommit folds the projection of the session the committed fact belongs
// to once that projection's rendered volume passes the window threshold, and
// files the compression judgment of every product the fold minted.
//
// The duty is read-only over the fact: a fact whose session registered no
// projection, a session whose window names no positive token window and a
// projection still inside its threshold each leave the commit as it stands. A
// fold that ran rewrote the projection only, and a failed fold is reported
// without rolling the commit back.
func (p *AgeFoldPlugin) ObserveCommit(ctx context.Context, id event.EventID) error {
	if p == nil || p.sessions == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session, held := p.sessions.SessionOf(ctx, id)
	if !held {
		return nil
	}
	projection, window, carried := p.sessions.ProjectionOf(ctx, session)
	if !carried || projection == nil {
		return nil
	}
	threshold := window.VolumeThresholdBytes()
	if threshold <= 0 {
		return nil
	}
	report, err := projection.FoldByAge(ctx, replay.AgeFoldPlan{
		KeepRecent:      window.KeepRecentTasks,
		VolumeThreshold: threshold,
	})
	if err != nil {
		return err
	}
	if !report.Triggered {
		return nil
	}
	for _, request := range report.Requests {
		if err := p.sessions.FileCompressRequest(ctx, session, request); err != nil {
			return err
		}
	}
	return nil
}

// ageFoldSessions is the session face of one run the fold plugin observes
// through: the live ledger of committed facts beside the replay registrations of
// the sessions this run opened.
type ageFoldSessions struct{ run *Run }

// The face satisfies the session port the fold plugin reads.
var _ SessionFolds = ageFoldSessions{}

// SessionOf reports the session chain one committed fact belongs to; a fact this
// run holds no record of belongs to no chain.
func (s ageFoldSessions) SessionOf(_ context.Context, id event.EventID) (event.SessionID, bool) {
	s.run.mu.Lock()
	evt, held := s.run.facts[id]
	s.run.mu.Unlock()
	if !held {
		return "", false
	}
	session := sessionOf(evt)
	return session, session != ""
}

// ProjectionOf reports the projection one session registered for replay beside
// the window it was registered under; a session whose chains several agents
// share resolves to the chain replayOfSessionLocked names for a reader beside
// no agent identity.
func (s ageFoldSessions) ProjectionOf(_ context.Context, session event.SessionID) (*replay.ReplayProjection, FoldWindow, bool) {
	s.run.mu.Lock()
	held, carried := s.run.replayOfSessionLocked(session)
	s.run.mu.Unlock()
	if !carried {
		return nil, FoldWindow{}, false
	}
	return held.projection, held.window, true
}

// FileCompressRequest files one compression judgment on the chain of the session
// whose material was judged.
func (s ageFoldSessions) FileCompressRequest(_ context.Context, session event.SessionID, request event.CompressRequest) error {
	return s.run.fileCompressRequest(session, request)
}

// fileCompressRequest lands one compression judgment in the live ledger of this
// run under the identity the judgment is quoted by. A judgment already on record
// is left as it stands: the identity is content-addressed, so a re-filed
// judgment is the same judgment and never a second fact. The fact carries no
// message, because a judgment about material is not material of its own and must
// never enter the projection it judged.
func (r *Run) fileCompressRequest(session event.SessionID, request event.CompressRequest) error {
	if session == "" {
		return errors.New("tagent: the compression judgment names no session chain")
	}
	if request.FactKey() == "" {
		return errors.New("tagent: the compression judgment carries no identity")
	}
	r.mu.Lock()
	_, filed := r.facts[request.ID]
	r.mu.Unlock()
	if filed {
		return nil
	}
	stamp := request.At
	if stamp.IsZero() {
		stamp = time.Now()
	}
	r.commitFact(event.Event{
		Key:       request.ID,
		Type:      request.FactType(),
		Timestamp: stamp,
		Meta:      map[string]string{event.MetaKeyEventSummary: request.Reason},
		Owner:     event.Address{SessionID: session},
	})
	return nil
}
