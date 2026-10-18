// 职责：调用框架已确定实例的模型并记录完整请求/输出/错误用于训练监控。
// 未实现：调度、重试、缓存、provider 检测、system 组装、流式与计费。
// 接线：config 在请求时准备模型实例、参数与 system，再调用 CallModel(ctx, model, request, record)。
// 契约: docs/wiki/agent/prototype-skeleton.md#six-pieces
package prototype

import "context"

// CallModel 调用指定模型并记录一次成功或失败轨迹；本身无状态，不改动原请求，也不访问 agent state。
func CallModel(ctx context.Context, model ModelFunc, request ModelRequest, record func(ModelTrace)) (ModelOutput, error) {
	snapshot := cloneRequest(request)
	out, err := model(ctx, request)
	trace := ModelTrace{Request: snapshot, Response: cloneOutput(out)}
	if err != nil {
		trace.Error = err.Error()
	}
	if record != nil {
		record(trace)
	}
	return out, err
}

// cloneRequest 复制可变 slice/map，保证记录点无法回改实际请求。
func cloneRequest(r ModelRequest) ModelRequest {
	if r.Skills != nil {
		r.Skills = append([]string(nil), r.Skills...)
	}
	if r.Tools != nil {
		r.Tools = append([]string(nil), r.Tools...)
	}
	if r.Inputs != nil {
		r.Inputs = append([]string(nil), r.Inputs...)
	}
	if r.Params != nil {
		params := make(map[string]float64, len(r.Params))
		for k, v := range r.Params {
			params[k] = v
		}
		r.Params = params
	}
	return r
}

// cloneOutput 复制工具调用切片，避免回调修改影响返回给调用方的输出。
func cloneOutput(o ModelOutput) ModelOutput {
	if o.ToolCalls != nil {
		calls := make([]ToolCall, len(o.ToolCalls))
		copy(calls, o.ToolCalls)
		o.ToolCalls = calls
	}
	return o
}
