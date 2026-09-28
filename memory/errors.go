package memory

import (
	"errors"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ErrKeyNotFound 等类型化存储契约错误：调用方必须能区分"键确实不存在"与"存储 I/O 失败"。把两者塌缩成一个，
// 等于把一次故障伪装成空召回、把一次恢复失败伪装成"这条链本来就没有"——它们静默产生错答案
// 而不是响亮报错。四类错误各自的处理义务见文档。
//
// 契约: docs/wiki/memory/memory-architecture.md#typed-errors
var (
	// ErrKeyNotFound is wrapped by KV backends when a key genuinely does not
	// exist. Any other error from a KVGet/KVScan is storage I/O and must
	// propagate, never be treated as a miss.
	ErrKeyNotFound = errors.New("kv key not found")

	// ErrDuplicateEventKey is returned by StoreEvent when the EventKey is
	// already committed. An EventKey IS the event's identity (collision
	// guard ): overwriting is refused, never silently applied.
	ErrDuplicateEventKey = errors.New("event key already exists")

	// ErrEventForgotten is returned by the internal replay path when the EventKey
	// is under a legal tombstone: the fact was deliberately deleted, so a replay
	// MUST NOT resurrect it . It is distinct from a duplicate/conflict (a same
	// key with wrong content) and from I/O — callers should hold the recovery
	// material and not ack, exactly as they would for a conflict.
	ErrEventForgotten = errors.New("event was legally forgotten (tombstoned); replay refused")

	// ErrEventProtected 由 DeleteEvent 在键仍受保留租约保护时返回：共享资源的恢复归属方还需要
	// 持久原文来完成 ack/回放未确认的信封或落盘项，因此显式删除被拒且**不销毁记录**（无损搬迁
	// 仍允许，只有销毁被拒）。调用方须在租约释放后重试。
	ErrEventProtected = errors.New("event is retained by an unacked-recovery lease; delete refused")
)

// KeyNotFound wraps err (when non-nil) into a typed missing error carrying
// the key context. KV backends use it so callers can errors.Is.
func KeyNotFound(key string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrKeyNotFound, key)
	}
	return fmt.Errorf("%w: %s: %w", ErrKeyNotFound, key, err)
}

// cloneFullEvent copies the mutable containers of a FullEvent so the store
// and its callers never share Maps/Slices (delta spec「后端不可变与隔离一致
// 性」): a caller mutating a returned event (or the event it passed in) must
// not corrupt stored facts or other readers. Response is intentionally NOT
// deep-copied — model snapshots are treated as read-only by contract
// (documented boundary, one-level containers are the mutation surface).
func cloneFullEvent(e FullEvent) FullEvent {
	if e.Metadata != nil {
		m := make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			m[k] = v
		}
		e.Metadata = m
	}
	if e.ToolCalls != nil {
		s := make([]model.ToolCall, len(e.ToolCalls))
		copy(s, e.ToolCalls)
		e.ToolCalls = s
	}
	if e.ToolResults != nil {
		m := make(map[string]interface{}, len(e.ToolResults))
		for k, v := range e.ToolResults {
			m[k] = v
		}
		e.ToolResults = m
	}
	return e
}

// IsDuplicateEventKey reports whether err is the typed duplicate-key error
// ( Major 1: the replay path treats duplicates as idempotent success).
func IsDuplicateEventKey(err error) bool {
	return errors.Is(err, ErrDuplicateEventKey)
}

// IsEventForgotten reports whether err is the typed tombstone/forgotten error — a
// legal deletion a replay must not resurrect .
func IsEventForgotten(err error) bool {
	return errors.Is(err, ErrEventForgotten)
}

// IsEventProtected 判断 err 是否为类型化的保留租约拒删——仍被未确认恢复所保留的原文上的显式
// 删除请求，需在租约释放后重试。
func IsEventProtected(err error) bool {
	return errors.Is(err, ErrEventProtected)
}
