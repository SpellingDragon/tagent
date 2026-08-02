# Plan Tool

管理工作计划的创建、进度更新和归档。

## 产出物边界（先读这个）

**plan 产出的是计划与规格文档（proposal/tasks），不是工作成果。** 它负责把复杂任务拆成可执行步骤并管理其生命周期——**执行这些步骤的人是你**（或你派的其他专职工具/agent）。

- ❌ 错误用法：`plan({action:"create", request:"深度分析这份文档并产出评估报告"})` 然后等 plan 交报告——plan 会拒绝代工，只返回"执行该评估的计划"
- ✅ 正确用法：让 plan 拆计划 → **你**按 tasks 逐步执行 → 每完成一步带证据 update → 全部完成后 archive

## 角色定位（外部顾问）

plan 不是任务执行工具，而是**外部顾问角色**：不直接接触执行细节与运行日志，只在与主 Agent 的交互中洞察问题、产出规划。

**三方治理**：用户 = 最终监督者（拥有否决/纠偏权）；主 Agent = 信息提供者 + 执行者（把交互中暴露的矛盾/遗漏喂给 plan，自己下场执行）；plan = 顾问（无权接触信息，交互中洞察、出规划）。

**顶底交互协议**：`create` = 主 Agent 下发规格，plan 返回 proposal+tasks（或澄清问题）；`update` = 主 Agent 带证据报账，plan 勾选进度；`archive` = 收敛门控（全部 `[x]` 且证据齐全才归档，否则退回）。多计划并行时以 `name` 定位。

## 何时使用

**必须使用 plan 的情况：**
- 任务需要 3 个以上步骤
- 任务涉及多个文件或多个知识点
- 任务需要用户确认或分阶段完成
- 你不确定能否一次性完成

**不需要 plan 的情况：**
- 简单的单步操作（如"查看文件"、"回答问题"）
- 明确的单一任务（如"删除文件 X"）

## 参数

| 参数 | 说明 |
|------|------|
| action | `create` / `update` / `archive` / `progress` |
| name | 计划名（kebab-case）。**update/progress/archive 必填**（多计划并行时靠它定位）；create 不填（名字由 plan 生成并在返回首行给出） |
| request | **必填**。自然语言描述你要做什么（即使是 progress/archive 也要写） |

## 各 action 契约

| action | 何时调用 | 必带 | 期望返回 | 传输方式 |
|--------|---------|------|---------|---------|
| create | 复杂任务**执行前** | request（任务描述+已知信息） | 首行 `计划已创建: <name>` + 任务清单（全 `[ ]`），或结构化澄清问题 | 新调用（记住返回的 task id 与 name） |
| update | **每完成一步后** | name + 步骤号 + **产物证据**（路径/结论摘要） | 已勾选的进度 | `resume_task(task_id, ...)` |
| progress | 用户询问或自查时 | name | checkbox 统计（零 LLM 直读） | `resume_task(task_id, ...)` |
| archive | 全部步骤完成后 | name | 已归档，或问题清单（勾选与实际不符时拒绝归档） | `resume_task(task_id, ...)` |

## 生命周期协议（重要）

一个计划 ≙ 一个 task：

1. **create 用新调用**发起。若返回 ACK（后台运行），**等 task_settled 拿到计划后再推进**——拿到 ACK ≠ 计划已建立，此期间不要 update、不要重复 create（同名重复调用会被去重并提示复用）。
2. create settle 后，**记住 task id 与计划名**；后续 update/progress/archive 一律用 `resume_task(task_id, "update name=X: 步骤N完成, 产物=...")` 续行——plan 会带着前几轮上下文，无需重新解释。
3. 任务终态超过保留期（task_terminal_ttl）被回收后 resume 会报"task not found"——此时改用**带 name 的新 plan 调用**继续该计划的生命周期。
4. **update 必须带证据**（产物路径或结论摘要）：plan 是审计者，归档前会逐项核实勾选与产物是否相符；无证据的报账会被要求补充。

## 使用示例

```
// 创建计划（复杂任务执行前）
plan({action: "create", request: "完善 OI 题库，基于 oi-wiki.org 补充 10 个知识点的题目"})
// → 返回首行: 计划已创建: complete-oi-question-bank (task 3f2a...)

// 更新进度（每完成一步，经 resume 续行，带证据）
resume_task("3f2a...", "update name=complete-oi-question-bank: 1.1/1.2 完成, 产物=knowledge/oi/basic-algorithms.md")

// 查看进度
resume_task("3f2a...", "progress name=complete-oi-question-bank")
// 或超窗后: plan({action: "progress", name: "complete-oi-question-bank", request: "查看进度"})

// 归档（全部完成后）
resume_task("3f2a...", "archive name=complete-oi-question-bank: 全部步骤已执行完毕")
```

> **注意**：`request` 字段必须始终填写，不能省略。即使只是查看进度，也要写清楚意图。

## 澄清回路（信息不足时——重要）

plan 在信息不足时**不会硬凑计划**，而是返回一份结构化澄清诉求（【已知】+【待补充】+ 具体问题）。识别到这种输出时：

1. 把 plan 的澄清问题**转达给用户**，收集所需信息（对象/范围/目标/约束/验收）
2. 用 `resume_task(task_id, "补充信息：…")` 把答案传给 plan 继续——plan 任务结算后**仍可 resume**，会带着上一轮上下文续跑
3. plan 据此完善；若仍不足会再次澄清——多轮迭代直至信息充分产出计划

> 不要替 plan 臆测补全信息，也不要因为 plan 提问就放弃或改为自己做——提问正是为了产出可执行的计划。
