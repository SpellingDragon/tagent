// 职责：事件消费壳——消费 AgentState 队列、按类型分派、驱动插件与模型，模型产出全部回队列。
// 未实现：背压、流式、持久化、强杀；壳只持 State 与函数字段，停止壳不关 owner 任务。
// 接线：装配方注入 Run/OnEvents/ModelCompletion/ResolveTool/Deliver/Compact；队列、上下文、任务在 AgentState。
package prototype

import "context"

// BaseTAgent 是事件驱动壳：只持 State 引用与装配注入的函数字段，不持队列/上下文/任务。
type BaseTAgent struct {
	State *AgentState

	Run             func(context.Context) error
	OnEvents        func(context.Context, []Event) error
	ModelCompletion func(context.Context, []string) (ModelOutput, error)
	ResolveTool     func(string) ToolFunc
	Plugins         []Plugin
	// Deliver 是投递任务的执行体（与 ToolFunc 同形）：EventReply 路由不直调它，而是经 owner 任务表
	// Spawn 一条 Call.Name="deliver" 的任务执行；成败以 ToolResult 通知回管线，循环不因投递失败终止。
	Deliver ToolFunc
	Compact func()
}

// Input 注入一条外部输入事件（来源默认 user）。
func (a *BaseTAgent) Input(ctx context.Context, text string) error {
	return a.Publish(ctx, Event{Type: EventInput, Source: "user", Data: text})
}

// Publish 把事件投回本壳所属 owner 队列；归属由 AgentState 补全/校验。
func (a *BaseTAgent) Publish(ctx context.Context, e Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return a.State.Publish(e)
}

// DefaultRun 独占消费本 owner 队列：Begin/End 由 State 管理，Next 阻塞取事件。
func (a *BaseTAgent) DefaultRun(ctx context.Context) error {
	if err := a.State.Begin(); err != nil {
		return err
	}
	defer a.State.End()
	for {
		events, err := a.State.Next(ctx)
		if err != nil {
			return err
		}
		if err := a.OnEvents(ctx, events); err != nil {
			return err
		}
	}
}

// DefaultOnEvents 逐事件：Before 插件 → 核心按类型分派并先执行 → After 插件 → 内核追加上下文/投递。
// 一批已到达事件中有 Input 或 ToolResult 时调用一次模型，不等待其他任务；ToolCall 交任务 API，Reply 只投递。
func (a *BaseTAgent) DefaultOnEvents(ctx context.Context, events []Event) error {
	driving := false
	for _, e := range events {
		e, err := ApplyPlugins(ctx, a.Plugins, e, true)
		if err != nil {
			return err
		}
		if e.Type == 0 {
			continue // 插件显式丢弃
		}
		route := e.Type // 核心按类型决定去向，After 变换不能改路由。
		switch route {
		case EventToolCall:
			a.spawn(e) // 核心先派发
		case EventCompact:
			if a.Compact != nil {
				a.Compact() // 核心先折叠
			}
		}
		if e, err = ApplyPlugins(ctx, a.Plugins, e, false); err != nil {
			return err
		}
		if e.Type == 0 {
			continue
		}
		switch route {
		case EventInput, EventToolResult:
			a.State.Append(e)
			driving = true
		case EventReply:
			a.State.Append(e)
			if a.Deliver != nil {
				// 投递任务化：不再直调 Deliver，交给 owner 任务表；成败以 ToolResult 通知回管线，
				// 循环不因投递失败终止（重投与否是模型决策域，原型不做输出通道治理）。
				a.State.Tasks.Spawn(ToolCall{Name: "deliver", Args: e.Data}, a.Deliver, e.Source)
			}
		case EventTaskNotice, EventModelDelta:
			// TaskNotice 只留状态；ModelDelta 是模型中间叙述——进上下文投影，不驱动、不投递。
			a.State.Append(e)
		}
	}
	if driving {
		return a.invokeModel(ctx)
	}
	return nil
}

// invokeModel 用当前上下文调用一次模型：带工具调用时把 Output/Reasoning 作为 EventModelDelta 回送本管线
// （中间叙述进上下文，不驱动、不外投），随后逐个 Publish 工具调用，不发 Reply；仅无工具调用且 Output
// 非空才 Publish Reply（final）。调用后折叠刚用过的结果。
func (a *BaseTAgent) invokeModel(ctx context.Context) error {
	if a.ModelCompletion == nil {
		return nil
	}
	var inputs []string
	for _, e := range a.State.Context() {
		inputs = append(inputs, e.Data)
	}
	out, err := a.ModelCompletion(ctx, inputs)
	if a.Compact != nil {
		a.Compact()
	}
	if err != nil {
		return err
	}
	if len(out.ToolCalls) > 0 {
		if out.Output != "" {
			if err := a.Publish(ctx, Event{Type: EventModelDelta, Source: "model", Data: out.Output}); err != nil {
				return err
			}
		}
		if out.Reasoning != "" {
			if err := a.Publish(ctx, Event{Type: EventModelDelta, Source: "model", Data: out.Reasoning}); err != nil {
				return err
			}
		}
		for _, call := range out.ToolCalls {
			if err := a.Publish(ctx, Event{Type: EventToolCall, Source: "model", Call: call}); err != nil {
				return err
			}
		}
		return nil
	}
	if out.Output != "" {
		if err := a.Publish(ctx, Event{Type: EventReply, Source: "model", Data: out.Output}); err != nil {
			return err
		}
	}
	return nil
}

// spawn 在消费点解析工具并交给 owner 任务管理器执行。
func (a *BaseTAgent) spawn(e Event) {
	var tool ToolFunc
	if a.ResolveTool != nil {
		tool = a.ResolveTool(e.Call.Name)
	}
	a.State.Tasks.Spawn(e.Call, tool, e.Source)
}
