// This file is the policy journey of the package: Boot attaches the governance
// policy and the enforcement grade its caller configured, and a call the policy
// refuses never runs.
//
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
package journey

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/evolution/governance"
)

// TestJourneyBootForwardsGovernance drives a call Boot must rule on:
// - a configured policy denies the send call, so its body never reaches the user
// - the model's next request reads the refusal and its rule, the seat the policy named
// - a strict enforcement grade beside no approver denies the same call the same way
//
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
func TestJourneyBootForwardsGovernance(t *testing.T) {
	const (
		toolBody = "the governed tool ran and reached the user"
		callArgs = `{"content":"the governed tool ran and reached the user"}`
		refusal  = "the policy stopped the send before it ran"
	)
	t.Run("a configured policy rules the call Boot admits", func(t *testing.T) {
		provider := newJourneyProvider(t)
		provider.script(journeyEntryModel, journeyReply{body: journeyToolCall("call-policy-1", "send", callArgs)})
		provider.scriptAnswers(journeyEntryModel, refusal)
		host := bootJourney(t, provider,
			journeySpec{EntryTools: []string{"send"}, MaxTokens: 4096},
			tagent.WithPolicy(governance.Open(
				governance.WithStrict(),
				governance.WithRules(governance.RiskRule{
					ID:     "journey-refuse-send",
					Match:  func(in governance.RiskContext) bool { return in.ToolName == "send" },
					Reason: "the journey policy refuses every send call",
				}),
			)),
		)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		host.invokeOK(t, ctx, "session-journey-policy", "please send the governed body")
		requests := provider.waitForRequests(t, journeyEntryModel, 2, 30*time.Second)
		text := requests[1].text()
		if !strings.Contains(text, "rule: journey-refuse-send") || !strings.Contains(text, "the journey policy refuses every send call") {
			t.Fatalf("second request text = %q, want the refusal the configured policy rendered", text)
		}
		for _, body := range host.sender.bodies() {
			if strings.Contains(body, toolBody) {
				t.Fatalf("delivered bodies = %q, want the refused call leaving no tool body", host.sender.bodies())
			}
		}
		if !strings.Contains(strings.Join(host.sender.bodies(), "\n"), refusal) {
			t.Fatalf("delivered bodies = %q, want the answer the model gave after the refusal", host.sender.bodies())
		}
	})

	t.Run("a strict enforcement grade beside no approver refuses the call", func(t *testing.T) {
		provider := newJourneyProvider(t)
		provider.script(journeyEntryModel, journeyReply{body: journeyToolCall("call-strict-1", "send", callArgs)})
		provider.scriptAnswers(journeyEntryModel, refusal)
		host := bootJourney(t, provider,
			journeySpec{EntryTools: []string{"send"}, MaxTokens: 4096},
			tagent.WithEnforcement(governance.EnforcementStrict),
		)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		host.invokeOK(t, ctx, "session-journey-strict", "please send the governed body")
		requests := provider.waitForRequests(t, journeyEntryModel, 2, 30*time.Second)
		if text := requests[1].text(); !strings.Contains(text, "strict enforcement admits no call without an approver") {
			t.Fatalf("second request text = %q, want the strict grade refusing the call", text)
		}
		for _, body := range host.sender.bodies() {
			if strings.Contains(body, toolBody) {
				t.Fatalf("delivered bodies = %q, want the strict grade leaving no tool body", host.sender.bodies())
			}
		}
	})
}
