# 事件语义边界：静默确认与失败告知

## 症状（验收矩阵第一行，R33——用户原话直译，非工程重述）

1. **“后台任务摘要后又发多余确认”**（2026-10-24 微信实录；2026-10-25 真机仍复现 20:12:26）
   → 直译测试：任务完成结算后，用户在本轮**收到恰好一条**结果消息；受理/进度/回执不产生任何面向用户的消息。
2. **“只有 task 状态变更时才应让模型感知，进入初始派发状态应该静默”**（2026-10-25 用户原话）
   → 直译测试：委派受理后，模型下一决策的上下文**不含**受理回执/任务号/状态词；工具结果仅为最小确认。
3. **“模型失败需要通报用户感知”**（2026-10-25 用户原话；活体红证 20:15:55——provider 真失败，回合静默结束，用户零消息）
   → 直译测试：模型调用**真失败**时，用户收到一条**带原始错误信息**的告知；成功回合不产生失败话术。

三条测试第一天存在且红着（00 域）；收口时回滚即红（F3 双证据）。

## Why

event-consumption-integrity 归档（42 叶全绿）解决的是数量层（通知不重不漏），但真机诊断证明语义层仍在病：
- 系统把**内部确认**（受理回执、send_ok、progress）当素材喂给模型，模型随即向用户复述——“多余确认”的直接成因；
- 模型调用**真失败**（trpc-agent-go 内重试耗尽）时，回合以 `no deliverable assistant body` 静默结束，用户没有任何下落——比答错更伤信任；
- 失败原因不落日志，事后无法归因；
- exec 工具 schema 描述抽象（“the resource family”“canonical key”），模型三连猜错 kind/resource。

根因是一句话：**事件流向模型的通道没有类型语义**——用户世界、业务需注意、系统内部确认三类输入在模型上下文里没有区分。

## What Changes

- **三静默**：受理回执 → 最小 `ok`（无 task id/状态/承诺）；`[send_ok]` → `ok`（成功无素材）；progress → 只落 fact 不进 intake（“还在跑”不构成说话素材）。`[send_denied]` 保留具名原因（需模型应对）。
- **失败告知**：模型真失败且回合无可交付正文时，框架经既有 sender 直投**原始错误信息**（不经模型）；同一事实落 fact 供审计；失败原因（error 文本）进日志（terminal=failed 带原因）。
- **不做的**：不干预 agent 主动 send（用户裁定：根因是系统诱导，不是模型多嘴）；不自建重试（trpc-agent-go 重试内闭环，本次失败是重试耗尽的真失败）；progress 业务白名单不做（防过度设计）。
- **工具感知下沉**：exec_run/exec_release 的 kind/resource 写死枚举与格式（kind 仅 `tmux`；resource 为会话名 `[A-Za-z0-9_-]+`，非带前缀路径）。
- **合同补偿**：persistent-event-loop 追加三场景（受理不产生模型材料 / progress 只落 fact / 失败必达告知）；event_integrity 既有场景对 progress 呈现语义的影响逐条核对并修订。

## Capabilities

### New Capabilities

- `quiet-receipts`：受理/投递成功/progress 三类系统确认的静默语义——什么进模型、什么只落 fact、什么保留具名拒绝。
- `failure-notice`：真失败的用户告知——触发条件（无可交付正文且调用失败）、原始错误直投、fact 与日志留痕。
- `tool-schema-clarity`：exec 资源参数的可枚举描述（kind/resource 合法形态进 schema）。

### Modified Capabilities

- `persistent-event-loop`：追加三条场景；progress 呈现语义修订（只落 fact 不进 intake，seq 有序场景保留）。
- `event-integrity`（golden 合同）：受 progress 语义影响的既有子场景逐条核对修订；症状测试纳入 short scope。

## Impact

- 代码：`runtime/agent/loop.go`（admissionText 删除/progress admitNotice 不 park）、`host.go`（send 回执文本、deliverFresh 失败分支、Invoke 错误贯通）、`tool/exec/tool.go`（两条 Declaration 描述）、`examples/wechat-bot/smoke_*_test.go`（三条症状测试）。
- 合同：`golden/runtime/event_integrity_scenarios.json`（受影响的 progress 场景）、`openspec/specs/persistent-event-loop/spec.md`。
- 用户裁定（本案）：范围全量打包；重试在框架内不做；失败告知透传原始信息；不干预 agent 主动 send。
- 已修两件（embedding 空文本短路、/task 独立会话）为本案基础设施（症状测试在独立会话跑），先于本案入库。

## 必答一问（R33）

“本案全部完成、全绿归档后，用户三句原话还会发生吗？”——不会：三条症状直译测试红转绿 + 回滚即红双证据入 F3。诚实边界：真机 provider 抖动不可控，症状 3 以 smoke 台架确定性失败注入复现（既有 500 注入先例）。
