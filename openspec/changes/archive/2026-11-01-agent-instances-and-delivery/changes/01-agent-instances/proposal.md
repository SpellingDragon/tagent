# 01 agent-instances：每 agent 常驻实例（G1）

父变更：agent-instances-and-delivery（依赖：无——本域是关键路径，W0 防护先行）。

## Why

设计预期"无限异步事件流驱动"应对每个 agent 成立；现状常驻消费实例仅挂入口 agent，非入口（子代理、meditator 等）走单槽 gate 同步直驱（`lockAddressContext`→`driveLocked`），调用方阻塞等待。用户裁定（Q5=a/Q9=a）：**每个 agent 都有自己的实例**，main–sub 只是数据编排关系（main 持有与 sub 通信的事件管线）；同步直驱退役，委派管线化，做好规划和用例防护。

## What Changes

- 常驻消费实例推广到**全部 agent 地址**（`Consumers.Ensure` 语义泛化）。
- 同步直驱路径（`lockAddressContext`/`driveLocked`）**退役删除**（清除式，不留双路径）。
- `call_subagent` serve 管线化：委派=向 sub 实例投递输入，结果以事件回流（task 受理/Notice 身份/settle 恰好一次语义**不变**）。
- W0 防护面先行：settle 恰好一次、会话占位、白名单 fail-closed 三条红线既有测试先绿 + 防护用例立起。

## 边界与依赖

- **依赖**：无（W0 防护面自立）。`runtime/agent/consumer.go`/`assembly/host.go` 为实现依赖独占。
- **被依赖方**：02（账目消费面挂常驻实例）、03（golden 跑在常驻实例形态上）。
- **接口面**：`Consumers.Ensure/Wake/Has` 语义、`Host.Invoke` 分流收敛、`subagent` serve 生命周期。
- **禁止事项**：不改 settle 恰好一次与会话占位语义（红线）；不改 task 受理/Notice 身份；不做 route 端口/业务 Thread；send 参数契约不动。
