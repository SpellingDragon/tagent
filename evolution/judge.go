package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/model"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
//
// DefaultJudgeScoreMax is the score ceiling of the LLM judge: only an explicit
// low score — strictly below the threshold — proposes a rollback. A judge that
// cannot produce such an explicit score decides nothing.
const DefaultJudgeScoreMax = 1.0

// judgeThreshold folds the configured score threshold into its effective form:
// a zero or negative threshold is not "never fire" but "not set", and takes the
// midpoint of the score range so the criterion keeps its conservative meaning.
func judgeThreshold(threshold float64) float64 {
	if threshold <= 0 {
		return DefaultJudgeScoreMax / 2
	}
	if threshold > DefaultJudgeScoreMax {
		return DefaultJudgeScoreMax
	}
	return threshold
}

// judgeAnswer is the JSON shape the LLM judge is asked to answer with: a score
// in [0,1] where higher is healthier, and the reason behind it.
//
// Score is a pointer on purpose: a missing score field is not a zero score. The
// conservative principle is that only an explicit low score proposes a rollback,
// so an answer without a score decides nothing and the judgment reports
// insufficient instead of degrading on a field that was never written.
type judgeAnswer struct {
	Score  *float64 `json:"score"`
	Reason string   `json:"reason"`
}

// NewModelJudge builds the default LLM judge over the public model face: it reviews one due window's
// evidence set with one model request and folds the answer into a Judgment.
//
// The judge is conservative on every path. Only an explicit score strictly
// below the threshold reports Degraded; a score at or above it reports a healthy
// judgment; and every failure — a failed model entry, a stream that never
// completed, an unparsable answer or an answer without a score — is digested
// into a Judgment with Decidable false and never an error, so a judge miss
// surfaces as "no decidable verdict" and is reported insufficient instead of
// masquerading as healthy. The answer is read as JSON from the first brace of
// the content, so a markdown fence around it is tolerated; the score is clamped
// into [0,1] and a missing reason is filled with a named placeholder.
func NewModelJudge(m model.Model, threshold float64) Judge {
	limit := judgeThreshold(threshold)
	return func(ctx context.Context, review Review) (Judgment, error) {
		if m == nil {
			return Judgment{Reason: "llm judge has no model"}, nil
		}
		request := &model.Request{
			Messages: []model.Message{{
				Role:    model.RoleUser,
				Content: judgePrompt(review),
			}},
		}
		stream, err := m.GenerateContent(ctx, request)
		if err != nil {
			return Judgment{Reason: fmt.Sprintf("judge model entry failed: %v", err)}, nil
		}
		outcome := model.ReduceChannel(ctx, stream, func(model.Alert) {})
		if outcome.Kind != model.KindCompleted {
			return Judgment{Reason: "judge model stream did not complete: " + kindName(outcome.Kind)}, nil
		}
		if outcome.Body == nil {
			return Judgment{Reason: "judge model returned no content"}, nil
		}
		answer, ok := parseJudgeAnswer(outcomeText(outcome))
		if !ok {
			return Judgment{Reason: "judge answer carries no parsable object"}, nil
		}
		if answer.Score == nil {
			return Judgment{Reason: "judge answer carries no score"}, nil
		}
		score := clampScore(*answer.Score)
		reason := answer.Reason
		if reason == "" {
			reason = "no reason stated"
		}
		if score < limit {
			return Judgment{
				Decidable: true,
				Degraded:  true,
				Reason:    fmt.Sprintf("llm judge score %.3f below threshold %.3f: %s", score, limit, reason),
			}, nil
		}
		return Judgment{
			Decidable: true,
			Reason:    fmt.Sprintf("llm judge score %.3f at or above threshold %.3f: %s", score, limit, reason),
		}, nil
	}
}

// judgePrompt renders the review evidence set and the answer contract for one
// judge request. The judge is handed the facts the post-hoc judgment already
// joined and is never asked to re-derive them; the governance-disabled posture
// is stated by name so a disabled guardrail never reads as clean statistics.
func judgePrompt(review Review) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are reviewing self-improvement version %q after real consumption.\n", review.Version)
	fmt.Fprintf(&b, "Real requests that consumed this version: %d.\n", review.Turns)
	fmt.Fprintf(&b, "Decidable feedback entries joined to this version: %d.\n", len(review.Joined))
	for _, entry := range review.Joined {
		verdict := "positive"
		if entry.Degrades {
			verdict = "negative"
		}
		fmt.Fprintf(&b, "- feedback %s: %s\n", entry.Evidence, verdict)
	}
	if review.Guardrail.Enabled {
		fmt.Fprintf(&b, "Governance guardrails were enabled: refusals %d, critical operations %d.\n",
			review.Guardrail.DenialCount, review.Guardrail.CriticalCount)
	} else {
		b.WriteString("Governance guardrails were disabled: refusal and critical counts are unavailable.\n")
	}
	b.WriteString("Score how healthy this improvement version is, from 0 (clearly degraded) to 1 (clearly healthy).\n")
	b.WriteString("Answer with only a JSON object: {\"score\": <number 0..1>, \"reason\": <short string>}.\n")
	return b.String()
}

// outcomeText folds the reduced outcome body into the text the judge answer is
// parsed from: the content of every choice the retained body carries.
func outcomeText(outcome model.Outcome) string {
	if outcome.Body == nil {
		return ""
	}
	var parts []string
	for _, choice := range outcome.Body.Choices {
		if choice.Message.Content != "" {
			parts = append(parts, choice.Message.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// parseJudgeAnswer reads the judge answer out of one model reply: the first
// brace opens the JSON object and a decoder reads exactly one value from there,
// so a markdown fence or leading prose around the object is tolerated and a
// trailing fence is never fed to the parser. The second return reports whether
// an object was found and parsed at all.
func parseJudgeAnswer(text string) (judgeAnswer, bool) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return judgeAnswer{}, false
	}
	var answer judgeAnswer
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&answer); err != nil {
		return judgeAnswer{}, false
	}
	return answer, true
}

// clampScore bounds one judge score into the [0,1] range the answer contract
// declares, so a wild value cannot decide outside the score scale.
func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > DefaultJudgeScoreMax {
		return DefaultJudgeScoreMax
	}
	return score
}

// kindName renders one terminal kind by name for a judgment reason.
func kindName(kind model.Kind) string {
	switch kind {
	case model.KindCompleted:
		return "completed"
	case model.KindFailed:
		return "failed"
	case model.KindCancelled:
		return "cancelled"
	case model.KindPartialOnly:
		return "partial-only"
	default:
		return "unknown"
	}
}
