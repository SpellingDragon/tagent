# spec-conformance：关键特性规格对齐修复

## Why

用户给出 tagent 关键特性权威规格 10 条；对齐审计判定 4/5/6/7 零实现零场景、3/8/9 部分缺失，且样例考古发现 subagent tool 形态/knowledge/recall 读面等 24 项声明零实现。主循环保持现状（裁定：trpc runner 无法充分承接特性），其余差距立案修复。

## What

上下文语义主干（映射序列→三龄段→动态环境块）＋task 逻辑状态＋插件式摘要相位＋样例特性回归（subagent tool/recall 两段/knowledge 工具面）＋观测收口（span 去重/六机制）＋清债（无效机制/热参/f·g 语义/golden 场景）。

## Capabilities

- context-assembly：映射序列记录、三龄段（新=L0/中=L1L2/老=L3）、动态环境块置底
- task-lifecycle：逻辑状态（stable/suspect/watch）、TTL、progress 回调
- plugin-phases：摘要触发/环境注入相位
- sample-features：subagent tool 形态、recall 两段消费、knowledge 工具面
- observability：span 去重、六机制观测
- conformance-golden：S13-S15 场景、S10 测试、f·g 语义

## 裁决记录

- 主循环不复用 trpc runner（用户裁定：无法充分承接特性）——R9 结案保持现状。
- 三龄段语义＝段龄轴（新=L0 全留／中=L1/L2 折叠过程信息／老=L3 历史摘要），与 deterministic-compress-level 的 age 规则同轴（审计判定，用户未否决）。
