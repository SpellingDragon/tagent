// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
package main

import (
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// receiptLineage labels the internal delivery receipts fed back to the agent. It
// stays outside every deliverable lineage: a receipt turn is internal, its own
// sending never receipts, and a withheld body travels with the receipt of the
// address that produced it.
const receiptLineage = "delivery_receipt"

// receiptInjector is the narrow host-side view of the inbox a receipt is queued
// into; *internalInbox satisfies it.
type receiptInjector interface {
	InjectInternal(addr event.Address, source string, msg trpcmodel.Message)
}

// emitReceipt renders one receipt, logs it at its grade and queues it for the
// address whose delivery ended without reaching the user. A receipt of the
// receipt lineage itself is dropped: the no-recursion red line.
func emitReceipt(inj receiptInjector, addr event.Address, reason, level, triggerSource, target, content string) {
	if inj == nil || triggerSource == receiptLineage {
		return
	}
	line := fmt.Sprintf("[delivery-receipt] 投递终态=%s 血统=%s 目标=%s：%s",
		reason, triggerSource, orNoTarget(target), truncateLog(content))
	if level == "ERROR" {
		log.Errorf("%s", line)
	} else {
		log.Warnf("%s", line)
	}
	inj.InjectInternal(addr, receiptLineage, trpcmodel.Message{
		Role:    trpcmodel.RoleUser,
		Content: line + "\n该内容未达用户；若属用户等待的交付，请在用户回合投递。",
	})
}

// orNoTarget names the missing delivery target of one receipt.
func orNoTarget(target string) string {
	if target == "" {
		return "(无目标)"
	}
	return target
}
