# deterministic-compress-level Specification

## Purpose

基于段龄的确定性压缩分级规则：以纯函数把任务回合段映射到 0–3 级并归入新/中/老三带，替代 LLM 价值评估；相同输入始终产出相同级别与带去，不读消息内容、不调用模型。分级只决定折叠路径（新带折过程细节，中/老带折为历史综述），不构成第二触发条件。

## Requirements

### Requirement: 确定性段龄定级纯函数

系统 SHALL 提供纯函数 `LevelOfAge(age, keepRecent) int`：给定一个已闭合任务回合段的段龄（0 = 最新段），按线性边界返回压缩级别——`age < keepRecent` 为 L0、`age < 2×keepRecent` 为 L1、`age < 3×keepRecent` 为 L2，其余为 L3。`BandOfLevel(level)` SHALL 把 L0 归为新带、L1 与 L2 归为中带、L3 归为老带。函数 SHALL 为纯函数：无副作用、不调用 LLM 或任何外部服务、不读取 MemoryStore 或 Projection；对相同输入 MUST 返回相同输出，返回值 MUST 落在 [0,3]。`keepRecent < 1` SHALL 读默认值 2。MUST NOT 使用指数段龄边界（`k×2^level`），也 MUST NOT 把基础定级封顶于 L2。进行中段（无 `agent_output` 闭合）SHALL 读 L0/新带。

#### Scenario: 线性边界定级

- **GIVEN** keepRecent=2
- **WHEN** 计算已闭合段的级别
- **THEN** age=0 与 age=1 SHALL 为 L0（新带）
- **AND** age=2 与 age=3 SHALL 为 L1（中带）
- **AND** age=4 与 age=5 SHALL 为 L2（中带）
- **AND** age=6 与 age=9 SHALL 为 L3（老带），而非指数边界的更早档位

#### Scenario: keepRecent=1 的三带

- **GIVEN** keepRecent=1
- **WHEN** 计算已闭合段的级别
- **THEN** age=0 SHALL 为 L0、age=1 SHALL 为 L1、age=2 SHALL 为 L2、age=3 SHALL 为 L3
- **AND** 级别对应的带 SHALL 依次为新带、中带、中带、老带

#### Scenario: 进行中段恒为 L0

- **GIVEN** 一个无 `agent_output` 闭合的进行中段
- **WHEN** 读取它的级别与带
- **THEN** 它 SHALL 为 L0/新带，不参与任何老化折叠

#### Scenario: 未设置窗口读默认值

- **GIVEN** keepRecent=0 或未设置
- **WHEN** 计算段龄级别
- **THEN** SHALL 以默认 keepRecent=2 计算，而不是返回错误或零值窗口
