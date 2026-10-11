// 职责：按明确 Kinds 在事件前后选择性变换，并提供结果外置插件与折叠。
// 未实现：插件管理器、优先级、失败回滚、异步投递；顺序由调用方切片固定。
// 接线：agent 循环调 ApplyPlugins；config 用 ResultMemoryPlugin 装配；模型消费后调 CompactResults。

package prototype

import "context"

// ApplyPlugins 对显式声明 Kinds 的插件依次应用 Before/After；空 Kinds 与 Type==0 不执行。
func ApplyPlugins(ctx context.Context, plugins []Plugin, e Event, before bool) (Event, error) {
	if e.Type == 0 {
		return e, nil
	}
	for _, p := range plugins {
		matched := false
		for _, k := range p.Kinds {
			if k == e.Type {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		var fn func(context.Context, Event) (Event, error)
		if before {
			fn = p.Before
		} else {
			fn = p.After
		}
		if fn == nil {
			continue
		}
		next, err := fn(ctx, e)
		if err != nil {
			return e, err
		}
		e = next
	}
	return e, nil
}

// DenylistPlugin 是治理插件示例：对命中名单的 ToolCall，Before 把该事件变换为 denied ToolResult
// （保留 Call/Data，置 Status="denied"、Error=拒绝原因）。核心按 Before 之后的 Type 路由，于是走
// ToolResult 路径进上下文并驱动模型——工具零执行、模型读到拒绝事实、据以继续。未命中原样放行由核心
// spawn。owner 为现场接缝（HITL 同机制：变换前先问人），原型不实现。
func DenylistPlugin(owner *AgentState, names ...string) Plugin {
	denied := make(map[string]bool, len(names))
	for _, n := range names {
		denied[n] = true
	}
	return Plugin{
		Kinds: []EventType{EventToolCall},
		Before: func(_ context.Context, e Event) (Event, error) {
			if !denied[e.Call.Name] {
				return e, nil
			}
			e.Type = EventToolResult
			e.Status = "denied"
			e.Error = "denied by governance plugin"
			return e, nil
		},
	}
}

// ResultMemoryPlugin 只对 eligible 的 ToolResult 存全文并注 memory_key；输入不处理，也不压缩正文。
func ResultMemoryPlugin(store *Memory, eligible func(Event) bool) Plugin {
	return Plugin{
		Kinds: []EventType{EventToolResult},
		Before: func(_ context.Context, e Event) (Event, error) {
			if eligible != nil && !eligible(e) {
				return e, nil
			}
			key := store.Put(e)
			if e.Meta == nil {
				e.Meta = map[string]string{}
			}
			e.Meta["memory_key"] = key
			return e, nil
		},
	}
}

// CompactResults 只把带 memory_key 的 ToolResult 折成短预览+票据，幂等，输入/回复不动。
func CompactResults(owner *AgentState) {
	events := owner.Context()
	changed := false
	for i := range events {
		e := events[i]
		if e.Type != EventToolResult || e.Meta["memory_key"] == "" || e.Meta["compacted"] == "true" {
			continue
		}
		e.Data = preview(e.Data, e.Meta["memory_key"])
		e.Meta["compacted"] = "true"
		events[i] = e
		changed = true
	}
	if changed {
		owner.ReplaceContext(events)
	}
}

// preview 取定长前缀（按 rune 计，避免截断多字节字符）并追加票据，作为结果正文的短预览。
func preview(data, key string) string {
	const max = 80
	if runes := []rune(data); len(runes) > max {
		data = string(runes[:max])
	}
	return data + " [memory:" + key + "]"
}
