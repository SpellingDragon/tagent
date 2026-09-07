# persistent-event-loop 规格增量

## ADDED Requirements

### Requirement: outputCh 宽限与溢出落盘
RunFlow 向 outputCh 发送事件时 MUST 设 2s 宽限；超限将事件全文落盘至 `<workspace>/tool-output/output-overflow/` 并投递摘要票据事件（路径+首尾片段），不阻塞主循环、不静默丢弃；SessionHook default 丢弃分支同步计数可观测。

#### Scenario: 慢消费者不卡死主循环
- **WHEN** 消费者停止读取 outputCh 超过 2s
- **THEN** 主循环继续运行，事件落盘可经票据找回，SessionHook 丢弃计数增加
