package event

import "trpc.group/trpc-go/trpc-agent-go/model"

// TypeInboxReceipt 标记「一个输入信封已被确认消费」的记账事实：其真源是事实链而非
// inbox 文件。它的 TTL 就是 request-id 的 30 天去重窗口，过期后同一 request-id 重投
// 不保证幂等。注册为非投影、非嵌入、非召回。
//
// 契约: docs/wiki/event/event-architecture.md#internal-retention
const TypeInboxReceipt = "inbox_receipt"

func init() {
	RegisterEventType(EventTypeSpec{
		Name:          TypeInboxReceipt,
		Role:          model.RoleUser,
		Skeleton:      false,
		TTLDays:       30,
		Embeddable:    false,
		Recallable:    false,
		NonProjection: true,
	})
}
