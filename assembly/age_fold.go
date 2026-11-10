// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package assembly

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
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
	// SettlementBytes is the rendered volume the delivery settlement receipts
	// inside the projection this window is reported beside already spend of it, in
	// bytes. A receipt is presented to the model, so it spends the same window the
	// conversation does and the fold criterion counts it: the conversation's room
	// is the window less this measure (VolumeThresholdBytes), which is the same
	// bound read on the projection the model reads (ModelViewBudgetBytes). A
	// window reported beside no projection spends none.
	SettlementBytes int
}

// ModelViewBudgetBytes reports the rendered volume the projection the model reads
// may carry before an age fold is due, in bytes: the share of the window the
// declaration allows, converted at the one character-to-token ratio the budget
// assembly packs requests under, so the fold fires where the assembly would
// start degrading the projection rather than after it already had.
//
// This is the bound itself. The projection the model reads is the conversation
// material beside the delivery settlement receipts, so the fold judges that
// material against the room these receipts leave (VolumeThresholdBytes) and the
// two readings are one bound. A window naming no tokens reports no budget, and a
// fold read under it never fires.
func (w FoldWindow) ModelViewBudgetBytes() int {
	if w.MaxTokens <= 0 {
		return 0
	}
	ratio := w.CompressThreshold
	if ratio <= 0 || ratio > 1 {
		ratio = config.DefaultCompressThreshold
	}
	return int(float64(w.MaxTokens*replay.BudgetCharsPerToken) * ratio)
}

// VolumeThresholdBytes reports the rendered volume the conversation material of
// one projection may carry before an age fold is due, in bytes: the window's
// model-visible budget less the delivery settlement receipts the projection
// beside it already spends of it. The fold mints its products on the
// conversation material alone, so this is the room that material is judged
// against, and conversation at this bound renders the window's whole budget
// beside its receipts and no more — the bound is the volume the model reads
// however long the history is.
//
// A window naming no tokens reports no threshold, and a fold read under it never
// fires. A window whose receipts already spend the budget leaves the one byte
// that keeps the material beside them due for a fold, so receipts alone never
// read as room for the conversation.
func (w FoldWindow) VolumeThresholdBytes() int {
	budget := w.ModelViewBudgetBytes()
	if budget <= 0 {
		return 0
	}
	room := budget - w.SettlementBytes
	if room < 1 {
		room = 1
	}
	return room
}

// withSettlementAccounting returns the window beside the room the delivery
// settlement receipts inside one projection spend of it, so the criterion the
// fold judges under is the volume the model reads: the projection is the one the
// fold observes, and its receipts are measured by the same per-entry render the
// projection keeps its own volume under.
func (w FoldWindow) withSettlementAccounting(projection *replay.ReplayProjection) FoldWindow {
	if projection == nil {
		return w
	}
	w.SettlementBytes = settlementBytesOf(projection.Entries())
	return w
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
// MUST answer the projection and the window the session registered, the window carrying the room the receipts inside that projection spend and never room the plugin invents.
// MUST NOT fold a session whose registered window names no positive token window.
type SessionFolds interface {
	// SessionOf reports the session chain one committed fact belongs to.
	SessionOf(ctx context.Context, id event.EventID) (event.SessionID, bool)
	// ProjectionOf reports the projection one session registered for replay
	// beside the window it was registered under, whose room the delivery
	// settlement receipts inside that projection spend.
	ProjectionOf(ctx context.Context, session event.SessionID) (*replay.ReplayProjection, FoldWindow, bool)
	// FileCompressRequest files one compression judgment the fold answered on the
	// chain of the session whose material was judged.
	FileCompressRequest(ctx context.Context, session event.SessionID, request event.CompressRequest) error
}

// AgeFoldPlugin judges the projection of the session the committed fact belongs
// to once its rendered volume passes the window threshold: the process detail of
// the older task turns is due for a fold and the middle band for one history
// summary. The plugin files each judgment as a compress request and executes
// nothing of it, so why material goes is a fact beside the product that says
// what went, and the consumer that reads the request does the folding.
//
// The plugin is an instance of the material flow like any other: it reads the
// fact material and answers an observation, so the pipeline dispatches it beside
// the reflection driver and the commit that grew the projection is the trigger.
// How material folds stays the projection's own rule — the plugin judges nothing
// about it.
type AgeFoldPlugin struct {
	sessions SessionFolds
	// executor runs the judgments the plugin files, so the material folds in the
	// commit that judged it instead of waiting for the next commit to observe
	// the request.
	executor compressExecutor
}

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

// setExecutor attaches the consumer that executes the judgments this plugin
// files. A plugin beside no executor still judges and files, so a host that
// mounts no consumer reads every judgment on the chain and gets no product.
func (p *AgeFoldPlugin) setExecutor(executor compressExecutor) {
	if p == nil {
		return
	}
	p.executor = executor
}

// Name reports the stable name of this plugin instance.
func (p *AgeFoldPlugin) Name() string { return AgeFoldPluginName }

// Port reports the consumption the plugin serves: one committed fact in, one
// observation out. The observation is the fold the fact may trigger.
func (p *AgeFoldPlugin) Port() plugin.Port {
	return plugin.PortFactObservation
}

// Material declares what this plugin reads, when it reads it and what a failed
// reading leaves behind: one committed fact read after its commit, where a failed
// fold is a named gap and the fact keeps standing.
func (p *AgeFoldPlugin) Material() plugin.Material {
	return plugin.Material{
		Reads:   plugin.PortFactObservation,
		Timing:  plugin.TimingPostCommit,
		Failure: plugin.RecordGap,
	}
}

// ObserveCommit judges the projection of the session the committed fact belongs
// to once that projection's rendered volume passes the window threshold, and
// files the judgment of every product the fold would mint. The material itself is
// left as it stands: the consumer that reads the judgment does the folding.
//
// The duty is read-only over the fact and over the projection: a fact whose
// session registered no projection, a session whose window names no positive
// token window and a projection still inside its bound each leave the commit as
// it stands. A failed judgment is reported without rolling the commit back.
// - The projection the model reads is read first as the cheap gate: it is judged against the window's whole budget, and the judgment on the conversation material is that same bound with the receipts the model reads beside it taken out, so a projection inside the budget pays one read and no rebuilt view.
// - The products are minted on the conversation material alone (foldProjectionOf), so the receipts the model reads are spend of the bound and never material the products name.
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
	budget := window.ModelViewBudgetBytes()
	if budget <= 0 {
		return nil
	}
	gated, err := projection.PlanByAge(ctx, replay.AgeFoldPlan{
		KeepRecent:      window.KeepRecentTasks,
		VolumeThreshold: budget,
	})
	if err != nil {
		return err
	}
	if !gated.Triggered {
		return nil
	}
	folded := foldProjectionOf(ctx, projection)
	if folded == nil {
		return nil
	}
	report, err := folded.PlanByAge(ctx, replay.AgeFoldPlan{
		KeepRecent:      window.KeepRecentTasks,
		VolumeThreshold: window.VolumeThresholdBytes(),
	})
	if err != nil {
		return err
	}
	if !report.Triggered {
		return nil
	}
	for _, product := range report.Products {
		if err := p.sessions.FileCompressRequest(ctx, session, product.Request); err != nil {
			return err
		}
	}
	if p.executor == nil {
		return nil
	}
	return p.executor.Execute(ctx, session)
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
// the window it was registered under, whose room the delivery settlement
// receipts inside that projection already spend; a session whose chains several
// agents share resolves to the chain replayOfSessionLocked names for a reader
// beside no agent identity.
func (s ageFoldSessions) ProjectionOf(_ context.Context, session event.SessionID) (*replay.ReplayProjection, FoldWindow, bool) {
	s.run.mu.Lock()
	held, carried := s.run.replayOfSessionLocked(session)
	s.run.mu.Unlock()
	if !carried {
		return nil, FoldWindow{}, false
	}
	return held.projection, held.window.withSettlementAccounting(held.projection), true
}

// FileCompressRequest files one compression judgment on the chain of the session
// whose material was judged.
func (s ageFoldSessions) FileCompressRequest(ctx context.Context, session event.SessionID, request event.CompressRequest) error {
	return s.run.fileCompressRequest(ctx, session, request)
}

// fileCompressRequest lands one compression judgment in the live ledger of this
// run under the identity the judgment is quoted by, and notes it to the consumer
// that executes judgments. A judgment already on record is left as it stands: the
// identity is content-addressed, so a re-filed judgment is the same judgment and
// never a second fact. The fact carries no message, because a judgment about
// material is not material of its own and must never enter the projection it
// judged.
// A judgment the commit refused lands in no ledger and reaches no consumer: the
// refusal — a commit that failed, or an identity already naming another fact —
// is answered to the caller instead of being handed to memory as a filed
// judgment, so no product can answer a request the chain never holds. The fact
// carries the owning agent of its session, so the judgment persists beside the
// product that answers it: an ownerless judgment never reaches the durable
// chain, and the resident window would release it with nothing to read back.
//
//   - The judgment fact and the account noted for it are one unit of work: the whole write holds the run's quiet-point fence, so a cut taken after the landing states the judgment instead of restoring a consumer beside a judgment the chain already carries.
func (r *Run) fileCompressRequest(ctx context.Context, session event.SessionID, request event.CompressRequest) error {
	if session == "" {
		return errors.New("tagent: the compression judgment names no session chain")
	}
	if request.FactKey() == "" {
		return errors.New("tagent: the compression judgment carries no identity")
	}
	r.flight.enter()
	defer r.flight.leave()
	r.mu.Lock()
	held, filed := r.facts[request.ID]
	agent := r.agentOfSessionLocked(session)
	consumer := r.compress
	r.mu.Unlock()
	if filed && held.Type != request.FactType() {
		return fmt.Errorf("%w: %s", memory.ErrFactKeyConflict, request.ID)
	}
	if !filed {
		stamp := request.At
		if stamp.IsZero() {
			stamp = time.Now()
		}
		if _, err := r.commitFactChecked(ctx, event.Event{
			Key:       request.ID,
			Type:      request.FactType(),
			Timestamp: stamp,
			Meta:      map[string]string{event.MetaKeyEventSummary: request.Reason},
			Owner:     event.Address{AgentID: agent, SessionID: session},
		}); err != nil {
			return fmt.Errorf("tagent: the compression judgment of session %s was not committed: %w", session, err)
		}
	}
	consumer.Record(session, request)
	return nil
}
