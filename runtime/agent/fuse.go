// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
//
// The execution-side storm fuses of the loop: one model response that issues
// more tool calls than the loop's declaration admits is parked as one approval
// batch instead of being dispatched, and both fuses — the per-response call cap
// and the long-context threshold — park through this one code point, so neither
// the direct drive nor a consumption segment carries a storm the other path
// would have stopped.
//
// A parked batch is neither truncated nor degraded: it is registered as one
// approval request a human reads, the wait happens inside the dispatch that
// would have admitted it, and an approved batch runs every call in the order
// the response issued them. A batch that is denied, that outlives its
// validity, or whose invocation is cancelled runs nothing, and a batch beside
// no wired approval book is refused whole rather than cut down to a limit
// nobody approved. The redline list (4 rules):
//
//   - MUST park one over-limit response as exactly one approval request, and answer every call of it with its own receipt.
//   - MUST admit an approved batch whole and in the order the response issued it, with no call dropped.
//   - MUST hold no model seat while a batch waits: the decision slot is vacated before the dispatch that parks it.
//   - MUST NOT change anything when both limits stand at zero, so a loop beside no declaration admits exactly as it did before.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

const (
	// DefaultResponseCallCap is the call cap a producer applies when it wants
	// the documented production default: one percent of the storm the fuse was
	// built for, and well above the peak of a healthy session.
	DefaultResponseCallCap = 64
	// DefaultLongContextTokens is the assembled prompt size past which a
	// producer holds a response for approval, chosen where the model starts
	// degrading on a long conversation.
	DefaultLongContextTokens = 50_000
	// approvalPollInterval is how often a parked batch reads its request back
	// while it waits: frequent enough that a ruling lands without a delay a
	// human notices, sparse enough that a batch parked for its whole validity
	// costs a few thousand index reads instead of a spin.
	approvalPollInterval = 250 * time.Millisecond
	// batchApprovalTool is the tool a parked batch is registered under. A batch
	// is a group of calls rather than one call, so it names the group the
	// approver reads instead of borrowing the name of a member.
	batchApprovalTool = "tagent:tool-call-batch"
	// batchApprovalIDPrefix is the prefix the approval identity of one parked
	// batch is minted under, so the request file names the decision that
	// parked it.
	batchApprovalIDPrefix = "approval-batch-"
	// fuseRuleResponseCap names the per-response call cap in the rule block of
	// the request an approver reads.
	fuseRuleResponseCap = "response-call-cap"
	// fuseRuleLongContext names the long-context threshold in that same block.
	fuseRuleLongContext = "long-context"
	// bytesPerToken is the approximation the long-context fuse judges a prompt
	// by: the assembled request carries no usage before the request runs, so its
	// message bytes standing for a quarter of that in tokens is the reading the
	// fuse decides on.
	bytesPerToken = 4
	// messageOverheadBytes is what one message costs beside its content: the
	// role, the separators and the framing a provider renders around it.
	messageOverheadBytes = 16
)

// FuseLimits is the execution-side fuse declaration of one loop: a member at
// zero is its fuse off, so the zero declaration admits exactly what the runtime
// admitted before the fuses existed.
type FuseLimits struct {
	// CallCap bounds the tool calls one model response may admit without an
	// approval.
	CallCap int
	// LongContextTokens is the assembled prompt size past which a response is
	// held for approval whatever it issues.
	LongContextTokens int
}

// constructedFuses is the declaration a loop is constructed beside: the
// composition root declares it once for the process, and an agent whose own
// declaration differs replaces it through the setter below.
var constructedFuses struct {
	mu     sync.RWMutex
	limits FuseLimits
}

// SetDefaultFuseLimits declares the fuses every loop constructed after this call
// is built beside, and reports the declaration it replaced so a caller restores
// the previous one when its own scope ends. A loop already running keeps the
// declaration it was constructed with until its own setter replaces it.
func SetDefaultFuseLimits(limits FuseLimits) FuseLimits {
	constructedFuses.mu.Lock()
	defer constructedFuses.mu.Unlock()
	previous := constructedFuses.limits
	constructedFuses.limits = limits
	return previous
}

// DefaultFuseLimits reports the declaration a loop is constructed beside.
func DefaultFuseLimits() FuseLimits {
	constructedFuses.mu.RLock()
	defer constructedFuses.mu.RUnlock()
	return constructedFuses.limits
}

// SetFuseLimits attaches the fuses this loop admits under: the declaration the
// composition root reads off the agent specification, replaced whole so a
// reload of the declaration never leaves half of a previous one in force.
func (l *Loop) SetFuseLimits(limits FuseLimits) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fuses = limits
}

// SetResponseCallCap attaches the per-response tool call cap alone; zero or a
// negative value leaves the cap off. The composition root that carries only
// this key wires it without assembling a second declaration.
func (l *Loop) SetResponseCallCap(cap int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fuses.CallCap = cap
}

// SetLongContextTokens attaches the long-context threshold alone; zero or a
// negative value leaves the fuse off.
func (l *Loop) SetLongContextTokens(tokens int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fuses.LongContextTokens = tokens
}

// fusesOf reports the declaration this loop admits under.
func (l *Loop) fusesOf() FuseLimits {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fuses
}

// ApprovalBook is the narrow face one parked batch is registered and resolved
// through: the registration an approver reads, and the read the wait polls for
// its ruling. It is exactly the part of the approval channel the fuses use, so
// the approval manager satisfies it as it stands and the execution core keeps
// no second book of its own.
//
// The redline list (3 rules):
// MUST register one request per batch under the identity the fuse minted, and report the request as registered.
// MUST report the effective state of that request, so an expired request never reads as a standing approval.
// MUST NOT answer a batch the book never filed: an unknown digest answers none, never a fabricated approval.
type ApprovalBook interface {
	// Request registers one pending approval request and reports it as stored.
	Request(ctx context.Context, id, tool string, jsonArgs []byte, risk plugin.RiskLevel, rule, reason string) (*approval.ApprovalRequest, error)
	// Lookup reports the request one (tool, arguments) pair is read under with
	// its effective status.
	Lookup(tool, argsDigest string) *approval.ApprovalRequest
}

// constructedBook is the approval book a loop is constructed beside: a
// composition root that wires the channel once declares it here, and a loop
// whose own agent wires another replaces it through the setter below.
var constructedBook struct {
	mu   sync.RWMutex
	book ApprovalBook
}

// SetDefaultApprovalBook declares the approval book every loop constructed
// after this call parks through, and reports the book it replaced so a caller
// restores the previous one when its own scope ends.
func SetDefaultApprovalBook(book ApprovalBook) ApprovalBook {
	constructedBook.mu.Lock()
	defer constructedBook.mu.Unlock()
	previous := constructedBook.book
	constructedBook.book = book
	return previous
}

// DefaultApprovalBook reports the approval book a loop is constructed beside.
func DefaultApprovalBook() ApprovalBook {
	constructedBook.mu.RLock()
	defer constructedBook.mu.RUnlock()
	return constructedBook.book
}

// SetApprovalBook attaches the approval book this loop parks a batch through. A
// nil book leaves the fuses with no channel, and a parked batch is then refused
// whole instead of being truncated.
func (l *Loop) SetApprovalBook(book ApprovalBook) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.book = book
}

// bookOf reports the approval book this loop parks through.
func (l *Loop) bookOf() ApprovalBook {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.book
}

// callRuling is the ruling one response's tool calls are admitted under: the
// number a response may issue on its own and the prompt size past which the
// response is held however many calls it carries.
type callRuling struct {
	// cap is the per-response call cap; zero admits every call.
	cap int
	// threshold is the long-context threshold; zero leaves that fuse off.
	threshold int
	// tokens is the approximate prompt size the ruling was read at.
	tokens int
}

// rulingOf reads the ruling of one assembled request off the declaration of the
// loop and the size of the request: the long-context fuse judges the request it
// is about to run, because the usage of a response lands after the dispatch the
// fuse has to rule.
func (l *Loop) rulingOf(messages []model.Message) callRuling {
	limits := l.fusesOf()
	return callRuling{
		cap:       limits.CallCap,
		threshold: limits.LongContextTokens,
		tokens:    approxPromptTokens(messages),
	}
}

// fuseOver names the fuse that holds a response issuing the given number of
// calls, or the empty rule when the response is admitted as it stands. The cap
// is read first: a storm on a long conversation is held by the sharper of the
// two limits, and the approver reads why it was held.
func (r callRuling) fuseOver(calls int) string {
	if r.cap > 0 && calls > r.cap {
		return fuseRuleResponseCap
	}
	if r.threshold > 0 && r.tokens > r.threshold && calls > 1 {
		return fuseRuleLongContext
	}
	return ""
}

// hold names the limit a parked batch was held by, in the words the receipt one
// call is answered with carries.
func (r callRuling) hold(fuse string) string {
	if fuse == fuseRuleLongContext {
		return fmt.Sprintf("the long-context threshold of %d", r.threshold)
	}
	return fmt.Sprintf("the per-response cap of %d", r.cap)
}

// batchItem is one call of a parked batch as the approver reads it: the tool it
// names and the digest of the exact argument bytes it carries. The call
// identifier is deliberately not part of it: a resend of the same calls binds
// to the same request, while the identifiers the response minted are listed on
// the read side the approver answers.
type batchItem struct {
	Tool       string `json:"tool"`
	ArgsDigest string `json:"args_digest"`
}

// batchDoc is the canonical document a parked batch is registered under: the
// fuse that held it, the counts an approver judges it by, and one item per call
// in the order the response issued them. It is the exact bytes the request
// digest is computed over, so a resend of the same calls names the same request
// and a changed call never rides an old approval.
type batchDoc struct {
	Fuse         string      `json:"fuse"`
	Calls        int         `json:"calls"`
	Cap          int         `json:"cap,omitempty"`
	Threshold    int         `json:"threshold,omitempty"`
	PromptTokens int         `json:"prompt_tokens,omitempty"`
	Items        []batchItem `json:"items"`
}

// parkedBatch is one response's batch as the dispatch registers it: the
// identity it is registered under, the rule and document of its registration,
// and the reason an approver reads.
type parkedBatch struct {
	// id is the approval identity the batch is registered under.
	id string
	// rule names the fuse that held the batch.
	rule string
	// doc is the canonical document the request digest is computed over.
	doc []byte
	// reason is the readable account of why the batch was held.
	reason string
}

// parkedBatchOf builds the one request a response over its limit is registered
// as: one item per call in the order the response issued them, the counts the
// fuse ruled at, and the call identifiers listed beside the digest so the
// approver can map the ruling back to the calls it answers.
func parkedBatchOf(decision Decision, calls []model.ToolCall, ruling callRuling, fuse string) parkedBatch {
	items := make([]batchItem, 0, len(calls))
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		items = append(items, batchItem{
			Tool:       call.Function.Name,
			ArgsDigest: approval.ArgsDigest([]byte(call.Function.Arguments)),
		})
		ids = append(ids, call.ID)
	}
	doc := batchDoc{
		Fuse:         fuse,
		Calls:        len(calls),
		Cap:          ruling.cap,
		Threshold:    ruling.threshold,
		PromptTokens: ruling.tokens,
		Items:        items,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"fuse":%q,"calls":%d}`, fuse, len(calls)))
	}
	return parkedBatch{
		id:     batchApprovalIDPrefix + string(decision.DecisionID),
		rule:   fuse,
		doc:    raw,
		reason: fmt.Sprintf("%s holds this response: %d tool calls and %d approximate prompt tokens; none of them runs until an approver answers, and an approved batch runs them all in the order the response issued them; call ids: %s", ruling.hold(fuse), len(calls), ruling.tokens, strings.Join(ids, ", ")),
	}
}

// batchOutcome is the terminal one parked batch resolves under.
type batchOutcome string

const (
	// batchApproved lets the whole batch run in the order it was issued.
	batchApproved batchOutcome = "approved"
	// batchDenied is the approver's refusal: no call of the batch runs.
	batchDenied batchOutcome = "denied"
	// batchExpired is a batch whose request outlived its validity undecided:
	// no call of it runs.
	batchExpired batchOutcome = "expired"
	// batchCancelled is a batch whose invocation ended while it waited: no call
	// of it runs.
	batchCancelled batchOutcome = "cancelled"
	// batchUnwired is a parked batch beside no approval book: the fuse refuses
	// it whole instead of truncating it.
	batchUnwired batchOutcome = "unwired"
	// batchUnregistered is a batch the approval book could not register: it is
	// refused whole, and no call of it runs.
	batchUnregistered batchOutcome = "unregistered"
)

// awaitBatch registers one batch and waits, bounded by the validity of the
// request it registered, for the ruling an approver gives it. The wait reads
// the request back every approvalPollInterval and ends with the ruling, the
// expiry of the request, or the cancellation of the invocation. It holds no
// model seat: the decision slot of the invocation is vacated before the
// dispatch that parks the batch.
//
// - A registration the book reports as failed beside a request that stays registered is logged rather than returned: only a registration that reports no request at all leaves the batch unrunnable.
func (l *Loop) awaitBatch(ctx context.Context, batch parkedBatch) (batchOutcome, string) {
	book := l.bookOf()
	if book == nil {
		return batchUnwired, ""
	}
	req, err := book.Request(ctx, batch.id, batchApprovalTool, batch.doc, plugin.RiskLevel(""), batch.rule, batch.reason)
	if err != nil {
		consumerDebug("tagent: consumer tool fuse registration failed approval=%s err=%v", batch.id, err)
	}
	if req == nil || req.ExpiresMs <= 0 {
		return batchUnregistered, ""
	}
	deadline := time.UnixMilli(req.ExpiresMs)
	for {
		if surfaced := book.Lookup(batchApprovalTool, req.ArgsDigest); surfaced != nil {
			switch surfaced.Status {
			case approval.ApprovalApproved:
				return batchApproved, surfaced.DecidedBy
			case approval.ApprovalDenied:
				return batchDenied, surfaced.DecidedBy
			case approval.ApprovalExpired:
				return batchExpired, ""
			}
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			return batchExpired, ""
		}
		if wait > approvalPollInterval {
			wait = approvalPollInterval
		}
		select {
		case <-ctx.Done():
			return batchCancelled, ""
		case <-time.After(wait):
		}
	}
}

// batchReceipts answers every call of a batch that did not run with its own
// receipt: the whole batch is answered, so no call of a held response is left
// without a paired reply, and the text names the ruling that stopped it.
func batchReceipts(calls []model.ToolCall, text string) []model.Message {
	receipts := make([]model.Message, 0, len(calls))
	for _, call := range calls {
		receipts = append(receipts, paired(call.ID, call.Function.Name, text))
	}
	return receipts
}

// batchRefusalText reports the receipt one call of a batch is answered with
// when the batch did not run: the ruling, the batch it applied to and the fact
// that this call never executed. Every call of the batch reads the same line,
// because the ruling is the batch's and not the call's.
func batchRefusalText(outcome batchOutcome, batch parkedBatch, calls int, ruling callRuling, by string) string {
	switch outcome {
	case batchDenied:
		return fmt.Sprintf("tagent: the approver%s refused this batch of %d tool calls (approval %s); this call was not run", byClause(by), calls, batch.id)
	case batchExpired:
		return fmt.Sprintf("tagent: the approval request of this batch of %d tool calls (approval %s) expired without a ruling; this call was not run", calls, batch.id)
	case batchCancelled:
		return fmt.Sprintf("tagent: the invocation was cancelled while this batch of %d tool calls (approval %s) awaited a ruling; this call was not run", calls, batch.id)
	case batchUnregistered:
		return fmt.Sprintf("tagent: this batch of %d tool calls could not be registered for approval; this call was not dispatched", calls)
	default:
		return fmt.Sprintf("tagent: %s holds this response's %d tool calls for approval, but no approval channel is wired; this call was not dispatched", ruling.hold(batch.rule), calls)
	}
}

// batchApprovalNotice is the one receipt a batch that ran leaves beside the
// admission receipt of each of its calls: the batch was approved whole, and the
// model reads that as input material of the range rather than as a second
// answer under a call identifier it already received one for.
func batchApprovalNotice(calls int, batch parkedBatch, by string) model.Message {
	return model.Message{
		Role: model.RoleUser,
		Content: fmt.Sprintf("tagent: the batch of %d tool calls was approved%s (approval %s); every call of it was dispatched in the order the response issued it",
			calls, byClause(by), batch.id),
	}
}

// byClause names the approver a ruling came from, or nothing when the channel
// recorded none.
func byClause(by string) string {
	if by == "" {
		return ""
	}
	return " " + by
}

// approxPromptTokens approximates the prompt size of one assembled request as
// its message bytes over four: the usage of a response lands after the dispatch
// the long-context fuse has to rule, so the fuse judges the request it is about
// to send — the content, the reasoning, the tool identities and the arguments
// the model issued, each beside the fixed overhead a provider frames a message
// with. It is an approximation by construction and never a token count a record
// carries.
func approxPromptTokens(messages []model.Message) int {
	bytes := 0
	for _, msg := range messages {
		bytes += len(msg.Content) + len(msg.ReasoningContent) + len(msg.ToolID) + len(msg.ToolName) + messageOverheadBytes
		for _, part := range msg.ContentParts {
			if part.Text != nil {
				bytes += len(*part.Text)
			}
		}
		for _, call := range msg.ToolCalls {
			bytes += len(call.ID) + len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	return bytes / bytesPerToken
}
