# Proposal: 子 agent 配置热更全景补齐（subagent-config-hotreload）

## Why

用户要求所有子 agent 配置可热更新（prompt、上下文大小、压缩与摘要设置、模型参数、模型 ID、模型路由等），并明确约束：**机制不得熵增，须回归一套机制自然消解各类问题**。规划期五轴走查定谳（见 design D1）：push→pull 归一**已在 main**（单真源+恒装源+消费边界拉取+提交点零写入），多数维度已热；真缺口仅两处——**摘要策略 knob**（OrgHotParams 无此字段）与 **per-call 覆盖通道**（调用时指定 prompt/模型/工具/上下文，上轮已裁"特殊 tool agent + 参数覆盖"方案）。用户三项裁决已入档：双通道全含、代际渐进语义（=现状对齐）、标准门禁；生成参数保持 fp 面粒度、SwappableModel 维持独立（两项"不做"防熵）。

## What Changes

- **01 热更矩阵审计与契约化**（W0）：产出"维度×通道×消费点×证据"矩阵；钉住"源里有但消费点没读=假热更"的契约红线；实证摘要 knob 的配置位置与归属。
- **02 摘要策略热参补维**：`OrgHotParams` 扩摘要 knob 字段 → 源自然携带 → `SmartCompressor` 契约扩 → 消费点契约测——**一处加字段，长在既有骨架上**。
- **03 per-call 子 agent 覆盖层**：本计划唯一新机制面——调用作用域覆盖栈（generation 默认 → 热参记录 → 调用覆盖 三层解析）+ Declarative 冻结（重放一致）+ 最大工具域治理 + 防泄漏 fail-before。
- **F 收尾**：契约零改动断言（fp 白名单 / 恒装源 / 零写入提交点 三禁区不被顺手改）、全量门禁、DoD 核验、归档。

## Capabilities

### New Capabilities

- `per-call-subagent-overrides`: 调用时以参数覆盖子 agent 的 prompt/模型/工具子集/上下文引用；三层解析优先序、最大工具域硬上界、Declarative 冻结、覆盖不跨调用泄漏。（specs 下沉至二级 03）

### Modified Capabilities

- `config-hot-reload`: +1 需求「配置热更维度矩阵」（每维度必属 fp 代际面或源拉取面之一且消费点有契约测试）；+1 需求「摘要策略热参」。（specs 下沉至二级 01/02）

> 本一级 change 仅留 orchestration 契约（启动门/波次/契约守护/DoD），域内验收契约全部下沉二级。
