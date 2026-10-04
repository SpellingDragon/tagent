// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
package main

import (
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// receiptLineage labels the internal delivery receipts fed back to the agent.
// It is deliberately outside the deliverable lineage whitelist: a receipt turn
// stays internal (its own musing never pushes to the user), non-user sources
// never arm the meditation novelty gate, and a receipt sharing a batch with a
// user message rides that turn's delivery instead of being withheld.
const receiptLineage = "delivery_receipt"

// receiptInjector is the narrow host-side view of the agent needed to feed a
// receipt back; *tagent.TagentAgent satisfies it.
type receiptInjector interface {
	InjectMessageWithSource(source string, msg model.Message)
}

// hasDeliveryFeature is a receipt-grading proxy only: content a user plausibly
// awaits. It never decides delivery — grading mistakes cost visibility, while
// inference at the delivery gate would collapse the strong meditation gate.
func hasDeliveryFeature(content string) bool {
	return strings.Contains(content, "[task settled]") || strings.Contains(content, "delivery/")
}

// receiptFor grades a withheld final response into (reason, level); an empty
// reason means the silence is contracted and stays silent. Receipted terminals:
// any undeliverable lineage (an integration defect signal), a meditation
// withhold carrying delivery features, and the error lineage. The receipt
// lineage's own digestion never receipts — the no-recursion red line.
func receiptFor(triggerSource string, deliverable bool, content string) (string, string) {
	switch {
	case triggerSource == receiptLineage:
		return "", ""
	case !deliverable:
		return "undigested-lineage", "WARN"
	case triggerSource == "meditation":
		if hasDeliveryFeature(content) {
			return "settled-delivery-held", "WARN"
		}
		return "", ""
	case triggerSource == "error":
		return "error-output-held", "WARN"
	}
	return "", ""
}

// reportWithheld logs and feeds back a withheld terminal when the silence was
// not contracted. target is the resolved (or missing) delivery destination.
func reportWithheld(inj receiptInjector, triggerSource string, deliverable bool, target, content string) {
	reason, level := receiptFor(triggerSource, deliverable, content)
	if reason == "" {
		return
	}
	emitReceipt(inj, reason, level, triggerSource, target, content)
}

// emitReceipt renders one receipt, logs it at its grade and injects it back
// through the agent's persistent bus, so the terminal survives a restart.
func emitReceipt(inj receiptInjector, reason, level, triggerSource, target, content string) {
	line := fmt.Sprintf("[delivery-receipt] 投递终态=%s 血统=%s 目标=%s：%s",
		reason, triggerSource, orNoTarget(target), truncateLog(content))
	if level == "ERROR" {
		log.Errorf("%s", line)
	} else {
		log.Warnf("%s", line)
	}
	inj.InjectMessageWithSource(receiptLineage, model.Message{
		Role:    model.RoleUser,
		Content: line + "\n该内容未达用户；若属用户等待的交付，请在用户回合投递。",
	})
}

func orNoTarget(target string) string {
	if target == "" {
		return "(无目标)"
	}
	return target
}
