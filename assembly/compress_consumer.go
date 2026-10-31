// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The compression consumer: the execution half of compression. A producer judges
// material over its bound and files a compress request; the consumer reads the
// material that request names, folds it through the projection and files the
// compress product the execution minted. Judgment and execution share no step,
// so what material went is a fact of the chain beside why it went.
package assembly

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// CompressionConsumerName is the stable name the compression consumer answers.
const CompressionConsumerName = "compress-consumer"

// compressionProducer names the mechanism a compress product is filed under: the
// consumer itself, so a product states who executed the judgment it answers.
const compressionProducer = "assembly.compress-consumer"

// metaKeyCompressRequest, metaKeyCompressProducer, metaKeyCompressKey,
// metaKeyCompressCovered and metaKeyCompressText are the metadata keys a compress
// product fact carries: the product states its five fields, and the event metadata
// is the one durable carrier of them, because a product is material of the
// projection and never a message of the transcript.
const (
	metaKeyCompressRequest  = "compress_request"
	metaKeyCompressProducer = "compress_producer"
	metaKeyCompressKey      = "compress_key"
	metaKeyCompressCovered  = "compress_covered"
	metaKeyCompressText     = "compress_text"
)

// compressCoveredSeparator separates the covered keys one product fact states:
// an event key carries no newline, so the list reads back in its own order.
const compressCoveredSeparator = "\n"

// CompressionConsumer executes the compression judgments filed on a chain. It
// reads the material one request names, executes the fold the judgment already
// made and files the product, so the projection restores that product from the
// chain instead of folding the material a second time.
//
// The redline list (3 rules):
// MUST file at most one product per request: the product identity is content-addressed over the judgment.
// MUST NOT judge material of its own: only the requests already on the chain are executed.
// MUST NOT execute a judgment whose material does not stand: it stays pending rather than answering a product of other material.
type CompressionConsumer struct {
	sessions compressSessions
	mu       sync.Mutex
	// judgments holds the requests one session filed, in filing order, keyed by
	// the session whose material they name.
	judgments map[event.SessionID][]event.CompressRequest
	// answered holds the request identities a product already answers, so a
	// repeated observation of one request executes it once.
	answered map[event.EventID]struct{}
}

// compressSessions is the session face the consumer reads one commit through:
// the session chain a committed fact belongs to, the projection and the window
// that chain registered, and the filing of one execution result.
type compressSessions interface {
	// SessionOf reports the session chain one committed fact belongs to.
	SessionOf(ctx context.Context, id event.EventID) (event.SessionID, bool)
	// ProjectionOf reports the projection one session registered for replay
	// beside the window it was registered under.
	ProjectionOf(ctx context.Context, session event.SessionID) (*replay.ReplayProjection, FoldWindow, bool)
	// FileCompressProduct files one execution result on the chain of the session
	// whose material was folded.
	FileCompressProduct(ctx context.Context, session event.SessionID, product event.CompressProduct) error
}

// compressExecutor is the face a producer hands one session's judgments to, so
// the material folds in the commit that judged it instead of waiting for the
// next one.
type compressExecutor interface {
	// Execute runs every judgment of one session that no product answers yet.
	Execute(ctx context.Context, session event.SessionID) error
}

// The consumer serves the fact→observation port the age fold plugin serves, and
// executes the judgments a producer filed.
var (
	_ plugin.Instance           = (*CompressionConsumer)(nil)
	_ plugin.PostCommitObserver = (*CompressionConsumer)(nil)
	_ compressExecutor          = (*CompressionConsumer)(nil)
)

// NewCompressionConsumer assembles the consumer over the session face of one
// run. A nil face answers no session, so an assembly mounted beside none
// executes nothing instead of failing on the first commit.
func NewCompressionConsumer(sessions compressSessions) *CompressionConsumer {
	return &CompressionConsumer{
		sessions:  sessions,
		judgments: map[event.SessionID][]event.CompressRequest{},
		answered:  map[event.EventID]struct{}{},
	}
}

// Name reports the stable name of this plugin instance.
func (c *CompressionConsumer) Name() string { return CompressionConsumerName }

// Port reports the consumption the consumer serves: one committed fact in, one
// execution of the judgments already on the chain.
func (c *CompressionConsumer) Port() plugin.Port { return plugin.PortFactObservation }

// ObserveCommit executes the judgments one session filed that no product answers
// yet: the fact that triggered it names the session, and every judgment of that
// session stands where the producer left it.
func (c *CompressionConsumer) ObserveCommit(ctx context.Context, id event.EventID) error {
	if c == nil || c.sessions == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session, held := c.sessions.SessionOf(ctx, id)
	if !held {
		return nil
	}
	return c.Execute(ctx, session)
}

// Record notes one judgment a producer filed for a session, so the consumer
// executes it. A judgment already noted is noted once: the identity is
// content-addressed, so a re-filed judgment is the same judgment.
func (c *CompressionConsumer) Record(session event.SessionID, request event.CompressRequest) {
	if c == nil || session == "" || request.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, held := range c.judgments[session] {
		if held.ID == request.ID {
			return
		}
	}
	c.judgments[session] = append(c.judgments[session], request)
}

// NoteProduct notes one product a chain already carries, so the judgment it
// answers is never executed a second time: this is the restart path, where the
// products of a chain answer judgments the process does not hold.
func (c *CompressionConsumer) NoteProduct(product event.CompressProduct) {
	if c == nil || product.Answers.Request == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.answered[product.Answers.Request] = struct{}{}
}

// Execute runs every judgment of one session no product answers yet: it reads
// the material the judgment named, folds it and files the product beside it. A
// session whose chain registered no projection, and a window naming no positive
// token window, execute nothing — the judgment stays pending as the unexecuted
// judgment it is.
//
// The pass reads the projection and never judges it: whether material is over
// its bound is already the request on the chain, and the plan only answers which
// product that request names.
func (c *CompressionConsumer) Execute(ctx context.Context, session event.SessionID) error {
	if c == nil || c.sessions == nil || session == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pending := c.pending(session)
	if len(pending) == 0 {
		return nil
	}
	projection, window, carried := c.sessions.ProjectionOf(ctx, session)
	if !carried || projection == nil {
		return nil
	}
	threshold := window.VolumeThresholdBytes()
	if threshold <= 0 {
		return nil
	}
	report, err := projection.PlanByAge(ctx, replay.AgeFoldPlan{
		KeepRecent:      window.KeepRecentTasks,
		VolumeThreshold: threshold,
	})
	if err != nil || len(report.Products) == 0 {
		return err
	}
	named := make(map[event.EventID]replay.FoldProduct, len(report.Products))
	for _, product := range report.Products {
		named[product.Request.ID] = product
	}
	for _, request := range pending {
		product, stands := named[request.ID]
		if !stands {
			continue
		}
		minted := event.CompressProductFor(request.ID, compressionProducer, product.Entry.Key, product.Covered, product.Entry.Content)
		if err := c.sessions.FileCompressProduct(ctx, session, minted); err != nil {
			return err
		}
		c.noteAnswered(request.ID)
		projection.ApplyProducts([]replay.FoldProduct{product})
	}
	return nil
}

// pending reports the judgments one session filed that no product answers yet,
// in filing order.
func (c *CompressionConsumer) pending(session event.SessionID) []event.CompressRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	filed := c.judgments[session]
	if len(filed) == 0 {
		return nil
	}
	out := make([]event.CompressRequest, 0, len(filed))
	for _, request := range filed {
		if _, done := c.answered[request.ID]; done {
			continue
		}
		out = append(out, request)
	}
	return out
}

// noteAnswered reports one judgment as answered by the product just filed.
func (c *CompressionConsumer) noteAnswered(request event.EventID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.answered[request] = struct{}{}
}

// FileCompressProduct files one execution result on the chain of the session
// whose material was folded.
func (s ageFoldSessions) FileCompressProduct(ctx context.Context, session event.SessionID, product event.CompressProduct) error {
	return s.run.fileCompressProduct(ctx, session, product)
}

// fileCompressProduct lands one execution result in the live ledger of this run
// under the product's content-addressed identity: a product already on record is
// left as it stands, so one judgment answers one product however often the
// execution runs.
func (r *Run) fileCompressProduct(ctx context.Context, session event.SessionID, product event.CompressProduct) error {
	if session == "" {
		return errors.New("tagent: the compression product names no session chain")
	}
	if product.FactKey() == "" {
		return errors.New("tagent: the compression product carries no identity")
	}
	r.mu.Lock()
	_, filed := r.facts[product.ID]
	agent := r.agentOfSessionLocked(session)
	r.mu.Unlock()
	if filed {
		return nil
	}
	if _, err := r.commitFactChecked(ctx, compressProductEvent(session, agent, product, time.Now())); err != nil {
		assemblyWarn("tagent: the compression product of session %s was not committed err=%v", session, err)
		return err
	}
	return nil
}

// agentOfSessionLocked reports the agent one session's mapped sequence was
// registered under: a registration naming the bare session answers no agent, so
// a product of that chain is kept in the live ledger alone, exactly as the
// projection it folds. The read is taken under the run lock the caller holds.
func (r *Run) agentOfSessionLocked(session event.SessionID) event.AgentID {
	if _, bare := r.replays[event.Address{SessionID: session}]; bare {
		return ""
	}
	agent := event.AgentID("")
	for addr := range r.replays {
		if addr.SessionID != session || addr.AgentID == "" {
			continue
		}
		if agent == "" || addr.AgentID < agent {
			agent = addr.AgentID
		}
	}
	return agent
}

// compressProductEvent renders one product fact as the event it commits as: the
// product identity as the event key, the session chain as the owner and the five
// fields of the product in the event metadata. The message stays empty, because a
// product is material the projection rebuilds and never material the transcript
// carries.
func compressProductEvent(session event.SessionID, agent event.AgentID, product event.CompressProduct, at time.Time) event.Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	covered := make([]string, 0, len(product.Covered))
	for _, key := range product.Covered {
		covered = append(covered, string(key))
	}
	return event.Event{
		Key:       product.ID,
		Type:      product.FactType(),
		Timestamp: at,
		Meta: map[string]string{
			event.MetaKeyEventSummary: "compress product " + string(product.Key),
			metaKeyCompressRequest:    string(product.Answers.Request),
			metaKeyCompressProducer:   product.Answers.Producer,
			metaKeyCompressKey:        string(product.Key),
			metaKeyCompressCovered:    strings.Join(covered, compressCoveredSeparator),
			metaKeyCompressText:       product.Text,
		},
		Owner: event.Address{AgentID: agent, SessionID: session},
	}
}

// compressProductOf reads one committed product fact back as the product it
// carries, and reports whether the fact states every field of one. A fact
// spelling another identity than its own key, and a fact naming no product key,
// answer no product: a product is restored from the one fact that states it, and
// a half-stated fact is refused by name instead of restoring invented material.
func compressProductOf(evt event.Event) (event.CompressProduct, bool) {
	key := event.EventID(evt.Meta[metaKeyCompressKey])
	request := event.EventID(evt.Meta[metaKeyCompressRequest])
	if key == "" || request == "" {
		return event.CompressProduct{}, false
	}
	product := event.CompressProductFor(request, evt.Meta[metaKeyCompressProducer], key, compressCoveredOf(evt.Meta[metaKeyCompressCovered]), evt.Meta[metaKeyCompressText])
	if product.ID != evt.Key {
		return event.CompressProduct{}, false
	}
	return product, true
}

// compressCoveredOf reads the covered keys one product fact states, in their
// recorded order.
func compressCoveredOf(covered string) []event.EventID {
	if covered == "" {
		return nil
	}
	parts := strings.Split(covered, compressCoveredSeparator)
	out := make([]event.EventID, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, event.EventID(part))
	}
	return out
}
