# Proposal: complete-meditation-loop

## Why

上批（unify-meditation-external-form）收口时留下两笔：
1. **概念税**：`curator` agent / `curation` session 是"双形态"时代的遗留词，单机制统一后读者还要额外建立"curator≈配了冥想的 agent"这层翻译——命名应与冥想语族一致。
2. **闭环差最后一针**：冥想 agent 的卡片回流靠宿主手工调 `DeliverToAgent`（示例只落日志）；旁路冥想的"发送"环节对 LLM 不可见，机制上等于"能看不能言"。

## What Changes

1. **新增 `deliver` 工具**（ADDED capability `meditation-delivery-tool`）：冥想 agent 的反思回合可自主调用，把卡片投给自己的 `deliver_to` 白名单目标。安全不变量复用投递缝四道门，另加三条：投递方身份由装配固定（LLM 不可伪造 from）；仅授予 `meditation.enabled` 且 `deliver_to` 非空的 agent；参数尺寸有界。拒绝以工具结果文本回给模型（与 governance 拒绝同风格——让模型能自纠），MUST NOT 静默。
2. **改名统一冥想语族**：示例 `curator`→`meditator`、session `curation`→`meditation`（保留名语义不变：与宿主路由永不撞名）；提示词文件、main.go 常量、README 双语、wiki §2.14/runtime 同步；主 spec「反思事件注入本 agent 循环 session」条款的推荐名措辞经 MODIFIED 跟进。
3. 分层约束：`tool/*` 不得 import 根包——工具实现落 `tool/meditation/` 叶子包（注入 `DeliverFunc` 闭包），根包装配期绑定 `DeliverToAgent` 的部分应用；方向断言不破。

## Capabilities

| Capability | 变化 |
|---|---|
| meditation-delivery-tool | ADDED：授予条件/身份固定/白名单调用时复核/错误回模型/尺寸上界/投递可观测 |
| meditation-agent-partition | MODIFIED（仅"保留名推荐 curation"措辞→`meditation`，行为条款不动） |
| cross-session-delivery | 不动（Go API 语义不变；工具是其消费者） |

## 边界与依赖

- **不动**：投递缝四道具名拒绝、novelty 判据、观察面授权模型、真实模型用例（不重跑，e2e 用 scripted toolcall 覆盖自主投递路径）。
- **pre-release 改名代价**：`PartitionIDFromName("curator")` 的既有示例数据成孤儿分区（无迁移负担，注记说明即可）。
- 依赖：无外部。`tool/meditation` 只 import event/memory/框架类型，装配在 build_agent。
