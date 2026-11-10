// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The semantic summary of one oldest band: the model face behind the history
// summary a fold files. The bounded mechanical reads of the band are handed to
// the model of the session's agent, and the answer becomes the summary fact's
// text, so what the model reads for the oldest material is a gist of it rather
// than one line per consumed entry. The answer is bounded by the same limit the
// mechanical form obeys, and a session whose agent holds no model answers no
// gist at all: the consumer keeps the mechanical form instead of inventing one.
package assembly

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// ErrNoSummaryModel reports a band summary asked of a session whose agent holds
// no model: the consumer reads it as the degradation the mechanical form stands
// for, never as a failed fold.
var ErrNoSummaryModel = errors.New("assembly: the session names no model for a band summary")

// summaryGists is the face the compression consumer asks for the gist of one
// oldest band: the semantic summary of the material a history summary stands for.
// The face answers the text to record, or an error the consumer degrades on.
type summaryGists interface {
	// SummaryGist answers the bounded summary of one band's mechanical reads.
	SummaryGist(ctx context.Context, session event.SessionID, material string) (string, error)
}

// BandSummaryInstruction is the one instruction the band summary request
// carries: the reads are already anchored, so the model compresses the text
// after each anchor and keeps the anchors themselves. A summary is asked of the
// same model the session's agent decides with and answers no tool call, so the
// instruction is exported for a scripted provider to tell a summary apart from
// a decision and answer it without consuming a scripted decision step.
const BandSummaryInstruction = "Compress the task-turn reads below into one bounded summary. " +
	"Keep every line's leading [MM-DD HH:MM|gen:...] anchor exactly as it stands and shorten only the text after it. " +
	"Keep the decisions, the results and anything still open; drop repetition and step-by-step detail. " +
	"Answer with the summary lines only, within 2048 bytes."

// bandSummaries is the gist face of one run: the model of the session's agent,
// read beside the run so a session that named no agent answers the model of the
// bare chain exactly as its facts were mapped.
type bandSummaries struct{ run *Run }

// The face satisfies the port the compression consumer reads.
var _ summaryGists = bandSummaries{}

// SummaryGist asks the session's model for the bounded gist of one band. A run
// naming no model for the session answers ErrNoSummaryModel, and a request that
// failed, never completed or answered no text is answered as an error, so the
// consumer degrades to the mechanical form instead of recording an empty summary.
func (s bandSummaries) SummaryGist(ctx context.Context, session event.SessionID, material string) (string, error) {
	if s.run == nil || session == "" || strings.TrimSpace(material) == "" {
		return "", ErrNoSummaryModel
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.run.mu.Lock()
	agent := s.run.agentOfSessionLocked(session)
	native, held := s.run.natives[agent]
	s.run.mu.Unlock()
	if !held || native.Model == nil {
		return "", ErrNoSummaryModel
	}
	stream, err := native.Model.GenerateContent(ctx, bandSummaryRequest(material))
	if err != nil {
		return "", fmt.Errorf("tagent: the band summary request of session %s failed: %w", session, err)
	}
	outcome := model.ReduceChannel(ctx, stream, nil)
	if outcome.Kind != model.KindCompleted {
		return "", fmt.Errorf("tagent: the band summary of session %s reached terminal %s", session, outcome.Kind)
	}
	text := strings.TrimSpace(modelTextOf(outcome))
	if text == "" {
		return "", fmt.Errorf("tagent: the band summary of session %s answered no text", session)
	}
	return text, nil
}

// bandSummaryRequest states the one request a band summary is asked under: the
// instruction beside the mechanical reads the fold already produced.
func bandSummaryRequest(material string) *model.Request {
	return &model.Request{
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: BandSummaryInstruction + "\n\n" + material,
		}},
	}
}

// modelTextOf reads the answer text one reduced outcome carries.
func modelTextOf(outcome model.Outcome) string {
	if outcome.Body == nil {
		return ""
	}
	parts := make([]string, 0, len(outcome.Body.Choices))
	for _, choice := range outcome.Body.Choices {
		if choice.Message.Content != "" {
			parts = append(parts, choice.Message.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// boundedSummaryGist cuts one model answer down to the bound a history summary
// obeys, on a rune boundary so the recorded text stays valid UTF-8: the bound is
// the summary's own, because the text a gist replaces answers it too.
func boundedSummaryGist(gist string) string {
	text := strings.TrimSpace(gist)
	if len(text) <= replay.DefaultAgeSummaryTextLimit {
		return text
	}
	cut := replay.DefaultAgeSummaryTextLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut])
}
