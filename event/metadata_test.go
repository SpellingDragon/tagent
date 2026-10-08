package event

import (
	"testing"

	"github.com/stretchr/testify/require"
	frameworkevent "trpc.group/trpc-go/trpc-agent-go/event"
)

// TestMetaKeyCallIDContract 钉住调用关联键的拼写与归属面。
//   - call_id 只由写入方（MemoryPlugin 经注入解析器，命中才盖）落在 FullEvent.Metadata
//   - 读取方是训练导出侧的 rawMetadata
//   - 它不是投递 StateDelta 契约键、也不是 meta_ 透传键，事件解析面绝不消费它
//
// 契约: docs/wiki/event/event-architecture.md#metadata-keys
func TestMetaKeyCallIDContract(t *testing.T) {
	require.Equal(t, "call_id", MetaKeyCallID, "训练导出侧按字面量读取，拼写即跨包契约")

	evt := &frameworkevent.Event{StateDelta: map[string][]byte{MetaKeyCallID: []byte("call-1")}}
	meta := ParseEventMeta(evt)
	require.Zero(t, meta.EventKey, "call_id 不得被当成投递键解析")
	require.Empty(t, meta.Meta["call_id"], "call_id 不带 meta_ 前缀，不进透传元数据")
	require.Empty(t, meta.TriggerSource)
	require.NotEqual(t, MetaKeyEventKey, MetaKeyCallID, "新键不得与既有投递键撞名")
	require.NotEqual(t, MetaKeyAgentName, MetaKeyCallID, "归因章与调用关联章各是各的键")
}
