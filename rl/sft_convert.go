package rl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// ValidateConversation reports the first legality violation of one conversation:
// a known role, system messages only ahead of the conversation, a tool message
// only as the answer of a pending assistant tool call, an assistant message
// carrying content or a call, and an assistant action as the final message. A
// tool call opened by the final action is answered by the next request of the
// episode, so an outstanding call is legal only there.
func ValidateConversation(messages []model.Message) error {
	if len(messages) == 0 {
		return sftRefuse(SFTGapIllegalMessage, "the conversation carries no message")
	}
	pending := map[string]bool{}
	opened := false
	openedAt := -1
	for i, message := range messages {
		switch message.Role {
		case model.RoleSystem:
			if opened {
				return sftRefuse(SFTGapIllegalMessage, "message %d carries the system role after the conversation opened", i)
			}
		case model.RoleUser:
			if err := unansweredBefore(pending, i, "a user message"); err != nil {
				return err
			}
			opened = true
		case model.RoleAssistant:
			if err := unansweredBefore(pending, i, "an assistant message"); err != nil {
				return err
			}
			if messageEmpty(message) {
				return sftRefuse(SFTGapIllegalMessage, "assistant message %d carries neither content nor a tool call", i)
			}
			opened = true
			for _, call := range message.ToolCalls {
				if call.ID == "" || call.Function.Name == "" {
					return sftRefuse(SFTGapIllegalMessage, "assistant message %d carries a tool call without an identity or name", i)
				}
				pending[call.ID] = true
				openedAt = i
			}
		case model.RoleTool:
			if message.ToolID == "" {
				return sftRefuse(SFTGapIllegalMessage, "tool message %d carries no tool call identity", i)
			}
			if !pending[message.ToolID] {
				return sftRefuse(SFTGapIllegalMessage, "tool message %d answers no pending tool call", i)
			}
			delete(pending, message.ToolID)
		default:
			return sftRefuse(SFTGapIllegalMessage, "message %d carries the unknown role %q", i, message.Role)
		}
	}
	if len(pending) > 0 && openedAt != len(messages)-1 {
		return sftRefuse(SFTGapIllegalMessage, "%d assistant tool call(s) opened at message %d are never answered", len(pending), openedAt)
	}
	if last := messages[len(messages)-1]; last.Role != model.RoleAssistant {
		return sftRefuse(SFTGapIllegalMessage, "the conversation ends on the %q role instead of an assistant action", last.Role)
	}
	return nil
}

// unansweredBefore refuses a new turn while an assistant tool call is pending.
func unansweredBefore(pending map[string]bool, index int, what string) error {
	if len(pending) == 0 {
		return nil
	}
	return sftRefuse(SFTGapIllegalMessage, "%s at %d opens while %d tool call(s) are unanswered", what, index, len(pending))
}

// messageEmpty reports a message carrying no content, no content part and no
// tool call.
func messageEmpty(message model.Message) bool {
	return strings.TrimSpace(message.Content) == "" && len(message.ContentParts) == 0 && len(message.ToolCalls) == 0
}

// maskOf returns the loss side of one message: an assistant action is trained on
// and every other role is context. The side follows the role alone.
func maskOf(role model.Role) SFTMask {
	if role == model.RoleAssistant {
		return SFTMaskAction
	}
	return SFTMaskObservation
}

// sftEvidence is what one cleaning run observed, kept as counts so the
// qualification matrix is read off the run rather than asserted.
type sftEvidence struct {
	kept       int
	unreadable int
	sets       map[string]*sftCandidateSet
	logprobs   map[LogprobsState]int
}

// sftCandidateSet is the response set one fixed prompt was observed with: how
// many distinct completions it carries and how many of its samples carry
// feedback that compares them.
type sftCandidateSet struct {
	responses map[string]bool
	labelled  int
}

// logprobsSeen records one attempt's logprobs state.
func (e *sftEvidence) logprobsSeen(state LogprobsState) {
	if e.logprobs == nil {
		e.logprobs = map[LogprobsState]int{}
	}
	e.logprobs[state]++
}

// unreadableSeen records a refusal that could hide a second response of the
// same prompt.
func (e *sftEvidence) unreadableSeen(kind SFTGapKind) {
	switch kind {
	case SFTGapLegacyReadOnly, SFTGapUnknownSchema, SFTGapBodyUnreadable, SFTGapIdentityMismatch, SFTGapPromptMissing:
		e.unreadable++
	}
}

// addExample records one converted sample against its prompt set.
func (e *sftEvidence) addExample(prompt, completion string, labelled bool) {
	if prompt == "" {
		return
	}
	if e.sets == nil {
		e.sets = map[string]*sftCandidateSet{}
	}
	set := e.sets[prompt]
	if set == nil {
		set = &sftCandidateSet{responses: map[string]bool{}}
		e.sets[prompt] = set
	}
	set.responses[completion] = true
	if labelled {
		set.labelled++
	}
}

// candidateSets counts the prompts observed with more than one response.
func (e *sftEvidence) candidateSets() int {
	count := 0
	for _, set := range e.sets {
		if len(set.responses) > 1 {
			count++
		}
	}
	return count
}

// comparedSets counts the candidate sets their bound feedback compares.
func (e *sftEvidence) comparedSets() int {
	count := 0
	for _, set := range e.sets {
		if len(set.responses) > 1 && set.labelled > 1 {
			count++
		}
	}
	return count
}

// observe records one converted sample against the response set of its fixed
// prompt: a second response under one prompt is a candidate pair, and feedback
// carrying a conclusion and its source labels the sample for a preference.
func (e *sftEvidence) observe(sample SampleLine, prompt []model.Message, completion model.Message) {
	e.addExample(digestOfPrompt(prompt), digestOfJSON(completion), preferenceEvidence(sample, sample.Fact.DecisionID))
}

// convertSFT converts one sample strictly: the stored attempt is read under the
// current schema, the completion is appended to the fixed prompt, the
// conversation is validated and masked, and the split is drawn on the causal
// group the sample's episode belongs to. Every refusal is named.
func convertSFT(sample SampleLine, prompts map[event.DecisionID][]model.Message, index *groupIndex, seed int64, ratio float64, evidence *sftEvidence) (SFTExample, error) {
	if err := sampleSchemaRefusal(sample); err != nil {
		return SFTExample{}, err
	}
	if sample.Fact.AttemptID == "" {
		return SFTExample{}, sftRefuse(SFTGapNotAnAttempt, "the sample carries no observed attempt, so it holds no completion to train on")
	}
	line, err := ParseLine(sample.Fact.Body)
	if err != nil {
		return SFTExample{}, sftRefuse(SFTGapBodyUnreadable, "the sample body does not parse: %v", err)
	}
	if line.Kind != RecordKindAttempt || line.Attempt == nil {
		return SFTExample{}, sftRefuse(SFTGapNotAnAttempt, "the sample body is the %q record and not an attempt", line.Kind)
	}
	attempt := *line.Attempt
	if attempt.ID != sample.Fact.AttemptID || attempt.DecisionID != sample.Fact.DecisionID {
		return SFTExample{}, sftRefuse(SFTGapIdentityMismatch, "the body names (decision %q, attempt %q) and the fact names (decision %q, attempt %q)",
			attempt.DecisionID, attempt.ID, sample.Fact.DecisionID, sample.Fact.AttemptID)
	}
	evidence.logprobsSeen(attempt.Logprobs)
	if attempt.Terminal != model.KindCompleted {
		return SFTExample{}, sftRefuse(SFTGapTerminalNotCompleted, "the attempt reached %q instead of a completed terminal", attempt.Terminal)
	}
	if attempt.Error != "" || attempt.Response.Error != nil {
		return SFTExample{}, sftRefuse(SFTGapAttemptFailed, "the attempt observed an error: %s", attempt.Error)
	}
	if len(attempt.Response.Choices) != 1 {
		return SFTExample{}, sftRefuse(SFTGapCompletionMalformed, "the response carries %d choices and one observed completion is trained on", len(attempt.Response.Choices))
	}
	completion := attempt.Response.Choices[0].Message
	if completion.Role != model.RoleAssistant {
		return SFTExample{}, sftRefuse(SFTGapCompletionMalformed, "the completion carries the %q role instead of an assistant action", completion.Role)
	}
	if messageEmpty(completion) {
		return SFTExample{}, sftRefuse(SFTGapEmptyCompletion, "the completion carries neither content nor a tool call")
	}
	prompt, held := prompts[attempt.DecisionID]
	if !held || len(prompt) == 0 {
		return SFTExample{}, sftRefuse(SFTGapPromptMissing, "the decision %q carries no fixed prompt", attempt.DecisionID)
	}
	messages := make([]model.Message, 0, len(prompt)+1)
	messages = append(messages, prompt...)
	messages = append(messages, completion)
	if err := ValidateConversation(messages); err != nil {
		return SFTExample{}, err
	}
	group := index.groupFor(sample.Fact.EpisodeID)
	if sample.Provenance.GroupID != "" && sample.Provenance.GroupID != group.GroupID {
		return SFTExample{}, sftRefuse(SFTGapGroupMismatch, "the provenance names the group %q and the causal dependencies name %q",
			sample.Provenance.GroupID, group.GroupID)
	}
	example := SFTExample{
		SampleID:     sample.Provenance.SampleID,
		GroupID:      group.GroupID,
		Split:        SplitOf(group.GroupID, seed, ratio),
		DecisionID:   attempt.DecisionID,
		AttemptID:    attempt.ID,
		Messages:     make([]SFTMessage, 0, len(messages)),
		SourceDigest: digestOf(sample.Fact.Body),
	}
	for _, message := range messages {
		mask := maskOf(message.Role)
		if mask == SFTMaskAction {
			example.Actions++
		} else {
			example.Observations++
		}
		example.Messages = append(example.Messages, SFTMessage{Message: message, Mask: mask})
	}
	evidence.observe(sample, prompt, completion)
	return example, nil
}

// sampleSchemaRefusal refuses a sample written under any schema but the current
// one: a v2 sample is routed to the read-only v2 converter and an unrecognised
// version is refused as unknown.
func sampleSchemaRefusal(sample SampleLine) error {
	for _, schema := range []SchemaVersion{sample.Schema, sample.Fact.Schema} {
		switch schema {
		case SchemaCurrent:
		case SchemaLegacyV2, SchemaLegacyV1:
			return sftRefuse(SFTGapLegacyReadOnly, "the sample is written under %q; read it through the read-only legacy converter %s", schema, SFTLegacyConverterV2)
		default:
			return sftRefuse(SFTGapUnknownSchema, "the sample declares the schema %q this package does not read", schema)
		}
	}
	return nil
}

// preferenceEvidence reports whether the feedback bound to one sample carries a
// comparison: a conclusion with the source it came from, on the sample itself.
func preferenceEvidence(sample SampleLine, decision event.DecisionID) bool {
	for _, record := range sample.Feedback {
		if record.Conclusion == "" || record.Source == "" || record.Version == "" {
			continue
		}
		if record.DecisionID == "" || record.DecisionID == decision {
			return true
		}
	}
	return false
}

// digestOf reports the SHA-256 of one stored body, so a cleaning record names
// the exact bytes it read.
func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// digestOfJSON reports the SHA-256 of one value's wire form, so a prompt or a
// completion is compared by the bytes it would be written as. A value that does
// not encode names no key and is never compared with another.
func digestOfJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return digestOf(encoded)
}

// digestOfPrompt reports the key one fixed prompt is read under: its messages
// in the order the request carried them.
func digestOfPrompt(messages []model.Message) string {
	return digestOfJSON(messages)
}
