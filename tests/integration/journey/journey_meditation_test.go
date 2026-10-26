// J3 is the meditation backflow journey: a reflection turn delivers one summary
// into the user session, the chain records it under the meditation lineage, and
// the next user turn's request presents that summary to the model.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package journey

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// journeyReflectionModel is the model name the meditation agent resolves: its
// own queue keeps the reflection turn apart from the entry agent's.
const journeyReflectionModel = "model-reflect"

// TestJourneyMeditationSummaryVisible drives a reflection delivery and reads it back on the next turn:
// - the user session chain carries a meditation-stamped summary event with the delivered body
// - the next turn's request carries that body, so the delivery is visible to the assembly
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestJourneyMeditationSummaryVisible(t *testing.T) {
	const (
		session      = "session-journey-meditation"
		summary      = "MEDITATION-SUMMARY: the user asked for the artifact and the worker finished it"
		firstAnswer  = "the first journey answer"
		secondAnswer = "the second journey answer"
	)
	provider := newJourneyProvider(t)
	provider.scriptAnswers(journeyEntryModel, firstAnswer, secondAnswer)
	provider.script(journeyReflectionModel, journeyReply{body: journeyToolCall("call-summary-1", "deliver_summary", `{"content":`+journeyQuote(summary)+`}`)})
	provider.scriptAnswers(journeyReflectionModel, "reflection recorded")
	host := bootJourney(t, provider, journeySpec{
		MaxTokens: 4096,
		PromptFiles: map[string]string{
			"meditation.md": "Reflect over the observed session and deliver one summary.\n",
		},
		ExtraAgents: map[string][]string{
			"meditator": {
				"    model: " + journeyReflectionModel,
				"    tools:",
				"      - { kind: tool, id: deliver_summary }",
				"    memory:",
				"      read_namespaces: [user]",
				"    meditation:",
				"      enabled: true",
				"      interval: 10ms",
				"      min_gap: 0s",
				"      prompt_file: meditation.md",
				"      observed_namespaces: [user]",
				"      deliver_to: [user]",
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	host.invokeOK(t, ctx, session, "hello meditation")

	fact := host.waitForFact(t, session, func(fact event.Event) bool {
		return fact.Type == event.TypeExternalInput &&
			fact.Meta[event.MetaKeyTriggerSource] == "meditation" &&
			strings.Contains(fact.Message.Content, summary)
	}, 15*time.Second)
	if summaryLine := fact.Meta[event.MetaKeyEventSummary]; summaryLine == "" {
		t.Fatalf("meditation summary event = %+v, want its one-line summary recorded beside the body", fact)
	} else if !strings.Contains(summaryLine, "MEDITATION-SUMMARY") {
		t.Fatalf("meditation summary line = %q, want the delivered body's first line", summaryLine)
	}

	host.invokeOK(t, ctx, session, "and now continue")

	requests := provider.waitForRequests(t, journeyEntryModel, 2, 15*time.Second)
	if text := requests[1].text(); !strings.Contains(text, summary) {
		t.Fatalf("second turn request text = %q, want the meditation summary presented to the model", text)
	}
}
