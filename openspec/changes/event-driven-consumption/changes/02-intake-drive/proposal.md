# 02-intake-drive — 会话入口

父变更：`event-driven-consumption`（依赖 01 的批次接口）。

## 边界与依赖

- **依赖**：01 的批次接口与会话消费者签名（`design.md#D6`）；`Host.Invoke` 同步返回语义不变（提交 + 等待结果）。
- **被依赖方**：05 宿主迁移。
- **接口面**：`Host.Invoke` 的受理语义、会话消费者启动/占用、`fireNoticeTurn` 与 `InjectEvent` 的委托方式。
- **禁止事项**：不改 `runtime/agent` 内部批次判据（01 所有）；不把 `SettleDriver` 扩成总调度；不新增对外 API；不以「谁在跑」决定受理。

## Why

当前 `Host.Invoke` 先用地址门判定「是否在途」，再决定「回落链受理」或「驱动」；在途时还把输入来源改写成在途 range（`acceptInflight`）。这条判定是三条症状的入口侧根因，也让 `fireNoticeTurn` 与 `InjectEvent` 各自维护一条驱动路径。

## What Changes

1. `Host.Invoke` 提交事实后即受理；由会话消费者推进（空闲则本次调用驱动一次消费，忙则由消费者后续驱动）。
2. 移除 `acceptInflight` 与 `gate.inflight` 的受理语义；驱动占用只作为并发原语。
3. 来源身份保持调用者所铸，不再改写为在途 range。
4. `fireNoticeTurn` 与 `InjectEvent` 改为「提交事实 + 唤醒会话消费者」，不再各自持有 drive 路径。

## Risk

- `ErrNoticeTurnIncomplete` 等既有错误语义依赖「同步驱动完成」；改委托后需重新定义「通知是否已交付」的证据。
- 宿主（微信）按 `KindUnknown` 判受理；入口语义变更需与 05 同批验收。
