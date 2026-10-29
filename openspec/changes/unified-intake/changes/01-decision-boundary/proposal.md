# 01 decision-boundary：在途 turn 的决策边界就绪增量

父变更：unified-intake（域 A）。

## 边界与依赖

- **依赖**：`persistent-event-loop` 现条款 :94/:105/:123（接口常数，实现不动，只补外部输入 ADDED 条款）；`Invocation.Ready` 语义（既有）；`addressGate`（既有，语义改）。
- **被依赖方**：02 域读「决策席占用」面（本域 A4 后席位语义稳定）。
- **接口面**：`agent.ReadyFeed` 窄面（消费方定义 `func(ctx, addr event.Address, after event.EventID) ([]event.Event, error)`——读失败 SHALL 以错误上报，不得读作无增量）；`Run.LandInput`/`Run.InvokeLanded`；host `gateSeat` handoff latch；`Host.Invoke` 受理语义（`ErrAcceptedInFlight`）；gate=turn 发起权门。
- **禁止**：不改 decision 事实的 inputs 落链语义（可重放性）；不改 ErrSlotHeld/ErrShutdownRefused；不引入 interrupt 语义。

## 症状

「不插入进行中的 turn」：慢工具轮 turn 进行中发第二条消息，现状排队到 turn 结束另起 turn——与 persistent-event-loop「排队至下一轮 decision 被消费」规格偏离。
