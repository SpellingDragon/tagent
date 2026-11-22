// The delivery fact of one terminal notification: the record a restart pairs a
// settled result with, the fact the settle pump drives the follow-up turn from
// and the marker the delivery account reads. Two write faces commit it — the
// backflow of a settled call in this process and the delivery a target accepts —
// and one read face looks it up by identity, so the key and the body are stated
// here once instead of assembled at each face.
//
// - one notification has one delivery fact: the identity is the notice's own four-part key, so a second face committing the same notification meets the fact the first one landed
// - the rendered fact is the same fact from every face: the type, the metadata and the body are the notification's, and the stamps the commit protocol generates beside a fact (the time, the invocation and the instance) describe the commit rather than the fact
// - the body states the settle line of the notification — its task, its kind and the result text it carries — so a reader that opens a segment on the fact reads what settled without a second copy of the result
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
package task

import (
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// DeliveryFactKey reports the canonical identity the delivery fact of one
// terminal notification commits under. A retried delivery and a second write face
// keep that identity, so one notification stands for one delivery fact.
//
// 契约: docs/FACT-VOCABULARY.md#families
func DeliveryFactKey(notice Notice) event.EventID {
	return event.EventID(fmt.Sprintf("settle-%x", notice.FactKey()))
}

// DeliveryFactOf renders the one delivery fact of a terminal notification: the
// fact the identity above names, owned by and routed to the address the
// notification was accepted at, beside the task it settles. The invocation and
// the instance are the caller's — they name the commit rather than the fact — so
// two faces rendering one notification produce the fact the storage answers as
// the same one, and the second commit is its repeat instead of a second fact.
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func DeliveryFactOf(notice Notice, addr event.Address, instance event.InstanceID, invocation event.InvocationID) event.Event {
	kind := string(notice.Kind)
	return event.Event{
		Key:       DeliveryFactKey(notice),
		Type:      event.TypeTaskSettled,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeySettleTriggerSource: kind,
			event.MetaKeyEventSummary:        kind,
			event.MetaKeyNoticeKind:          kind,
			event.MetaKeyTaskID:              string(notice.TaskID),
		},
		Message:      model.Message{Role: model.RoleUser, Content: deliveryFactText(notice)},
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		TaskID:       notice.TaskID,
		InstanceID:   instance,
		InvocationID: invocation,
	}
}

// deliveryFactText renders the body of one delivery fact: the settle line of the
// notification — the terminal kind it states and, on a line of its own, the
// result text it carries — so the fact a reader opens states what settled.
func deliveryFactText(notice Notice) string {
	text := fmt.Sprintf("tagent: task %s %s", notice.TaskID, notice.Kind)
	if body := strings.TrimSpace(notice.Text); body != "" {
		text += "\nresult:\n" + body
	}
	return text
}
