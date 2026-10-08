// 请求预算的固定开销视图：装配后不可压缩的那一半如何被诚实计价。
// 契约: docs/wiki/agent/compression-and-telemetry.md
package compress

import (
	"github.com/SpellingDragon/tagent/modelutil"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// BudgetExceededReason is the machine-checkable name of the fixed-overhead
// refusal. A request whose ESTIMABLE fixed part already exceeds the input limit
// must be refused by the caller's gate under this name; the compressor never
// deletes the system prompt or the tool declarations to squeeze through, and it
// never runs a second compression to "make room" — ContextCompressor stays the
// single compression authority.
const BudgetExceededReason = "budget_exceeded"

// RequestBudgetContext is the OPTIONAL fixed-overhead half of one assembled
// request handed to Compress, as a value snapshot rather than a live view: the
// caller passes what THIS request carries (the same frozen declarations the
// outbound SDK request uses), so a concurrent schema hot-change cannot move the
// goalposts mid-call. ContextCompressor prices the resolved history with its own
// TokenCounter; everything the history fold CANNOT pay for — the assembled system
// prompt, the frozen tool declarations, the live task board and recovery notices,
// and the request envelope — arrives here.
//
// Absent and present-with-zero-fields are DIFFERENT statements: absent means the
// caller knows nothing about the fixed part and the hot group's numbers stand;
// present means the fixed part is AUTHORITATIVE, genuine zeros included (an empty
// system prompt and no tools really cost nothing), and the derived
// compressible-content budget then travels to the compressor as an explicit value.
type RequestBudgetContext struct {
	// SystemText is the assembled system prompt for this request ("" = none).
	SystemText string
	// NoticesText is the dynamic task-board / recovery-prompt text injected
	// outside the frozen history messages ("" = none).
	NoticesText string
	// Tools is the frozen S1 declaration view for this request (nil/empty = none).
	Tools []modelutil.ToolDeclarationSnapshot
	// ExtraUnknown declares components the caller could not size (e.g. media
	// without usable metadata). They are REPORTED, never priced: the estimate
	// stays honest about its own coverage instead of claiming an exact total.
	ExtraUnknown []string
}

// FixedOverhead prices the non-compressible part of the request with the SAME
// estimator the outbound request budget uses (modelutil.RequestSnapshot.
// EstimateBudget), so the compressor and the budget can never drift. The
// returned RequestBudget carries only the fixed-side buckets; the history is
// priced by the compressor's own counter and lands in a separate bucket.
//
// The int is the quantified total (a FLOOR whenever Unknown is non-empty — an
// unknown is never counted as zero and never claimed as measured).
func (b RequestBudgetContext) FixedOverhead() (modelutil.RequestBudget, int) {
	var msgs []model.Message
	if b.SystemText != "" {
		msgs = []model.Message{{Role: model.RoleSystem, Content: b.SystemText}}
	}
	snap := modelutil.RequestSnapshot{Messages: msgs, Tools: b.Tools}
	budget := snap.EstimateBudget(b.NoticesText)
	for _, u := range b.ExtraUnknown {
		budget.Unknown = append(budget.Unknown, u)
	}
	fixed := budget.System + budget.ToolDeclarations + budget.Notices + budget.ProtocolOverhead
	return budget, fixed
}
