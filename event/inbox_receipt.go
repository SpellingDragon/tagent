package event

import "trpc.group/trpc-go/trpc-agent-go/model"

// TypeInboxReceipt marks the durable-acknowledgement record of one consumed
// inbox envelope (resident-readiness-plan 3.4): the receipt's truth source is
// the FACT CHAIN, not the inbox file. Its TTL IS the 30-day request-id dedup
// window — after expiry a client re-submitting the same request id is NOT
// guaranteed idempotent (documented boundary, delta spec「输入处理确认与幂等」).
// Registry-declared: non-projection (skipped at rebuild), non-embeddable,
// non-recallable, TTL 30d.
const TypeInboxReceipt = "inbox_receipt"

func init() {
	RegisterEventType(EventTypeSpec{
		Name:       TypeInboxReceipt,
		Role:       model.RoleUser,
		Skeleton:   false,
		TTLDays:    30,
		Embeddable: false,
		Recallable: false,
	})
}
