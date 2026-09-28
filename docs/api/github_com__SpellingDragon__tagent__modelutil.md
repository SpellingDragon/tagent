package modelutil // import "github.com/SpellingDragon/tagent/modelutil"

Package modelutil hosts the shared assembly for direct (non-agent) model call
sites — summary compression and the evolution judge. Both previously hand-rolled
model.Request without generation knobs; now they speak the same ModelRef
vocabulary as agents. (tagent-unify-model-call-config.)

FUNCTIONS

func BuildRequest(msgs []model.Message, k Knobs) *model.Request
    BuildRequest assembles a model.Request with generation knobs applied.

func Call(ctx context.Context, m model.Model, req *model.Request) (string, error)
    Call runs one direct completion and collects the final text. It carries the
    reasoning-fallback drain shared by summary/judge sites: when a reasoning
    model returns empty Content but non-empty ReasoningContent, the reasoning
    text is used instead of failing (first generalized from the summary site).


TYPES

type Knobs struct {
	Temperature          *float64
	MaxTokens            *int
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string
}
    Knobs carries the generation settings a direct call site may override.
    Nil fields stay untouched so per-site defaults (e.g. summaryMaxTokens)
    remain authoritative when the ModelRef omits them.

