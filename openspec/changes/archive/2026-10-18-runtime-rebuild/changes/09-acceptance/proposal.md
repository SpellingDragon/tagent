# 09-acceptance — 完整验收与回退（二级子变更 proposal）

> 层级：隶属于一级变更 `runtime-rebuild`（上级目录 `../../`）。本域是**责任与验收单位**，不是代码包数量，也不是整域派发单位。
> 本轮只产规划文档（md）；产品代码、测试、脚本、检查资产、CI 均不在本轮写域。孙任务 `09.1–09.6` 全部未勾选。

## Why

运行时重建的最终判据是可复跑的**真实**证据，而非"人工 PASS 汇总"。历史教训集中在三处：把格式有效当产品成立、把结构覆盖用通配掩盖、把键名对齐当行为通过；以及把合成/mock 消费者当真实路径、把回退写成伪造的 restore 副作用。本域从头建立检查资产与完整门禁，并区分**产品验收**与**检查器自验收**。

## What Changes

- **09.1 正式场景与 golden**：所有正式装配场景输出真实观测并对照 golden（**值/消息/身份关系**断言，非键名）；不用人工 PASS 汇总。
- **09.2 离线迁移与回退**：只读旧数据导入新路径、缺失外置文件/不可恢复任务具名、迁移中断重跑与回退；源摘要不变，不让旧程序读新格式。
- **09.3 完整门禁**：两 module 完整 build/vet/test/lint、第一方子包 race 与检查器负控；零失败不能由过滤输出得出。
- **09.4 常驻与恢复**：常驻 soak、独立进程崩溃恢复、热更回滚、权限与资源泄漏（部署环境实跑）。
- **09.5 各资源子场景**：存储档、真实模型/MCP/A2A/embedding/collector/SFT/微信逐项验收；未启用可 Skip，正式必验项不得以 Skip 结项。
- **09.6 闭合**：主规格 delta、文档/API 与消费者逐项闭合，提交未决事项与最终证据清单。

## 边界与依赖（四要素）

### 依赖

- **实现依赖**：`01`–`08` 相关确定性实施项完成（09.1）；`00.1`（残件盘点）、`00.6`（CI/lint 迁移）；`03.3`/`03.5`（持久提交/保留）；宿主确定性验收（09.3）；部署环境（09.4）；授权资源（09.5）。
- 09.1 与 09.5 分列：确定性场景先跑，真实资源欠账另列 09.5，互不等待无关服务。

### 被依赖方（消费者）

- 一级编排（本域消费者）与最终交付；归档（另行授权，不在本轮）。

### 接口面（**名称固定**，由主代理 tooling 单创建；本域仅引用，不重命名、不新建）

- 检查脚本：`scripts/check_runtime_plan.py`、`scripts/check_runtime_architecture.py`、`scripts/check_runtime_structure.py`、`scripts/check_runtime_evidence.py`。
- 检查器自测：`scripts/test_check_runtime_*.py`。
- golden：`golden/runtime/architecture.json`、`golden/runtime/scenarios.json`。
- CI：`.github/workflows/runtime-contracts.yml`。
- 计划清单 manifest：`openspec/changes/runtime-rebuild/task-graph.json`（**65 task 已定**；json 只含 `planned`，带 `dep`/`schema`）；计划**结构**校验 `check_runtime_plan.py --change-dir <dir> --json`；**合同**校验 `check_runtime_evidence.py --check-contract`；**严格行为**校验 `check_runtime_evidence.py --evidence RUN-file.json --test-log PATH`（成对，只给一半即失败）。

### 禁止事项

- **不伪 restore 副作用**：回退只读旧数据、走新路径；不得伪造恢复写回/自动改写旧档（迁移保持**读旧写新**、源字节不变）。
- **不把合成 SFT consumer / mock 当真实**：真实 tokenizer/chat template/批次构造/标签与动作 mask 检查是必验项。
- **新架构 check strict 全 coverage 不得靠 `tagent: *` 通过**（允许集按职责人工登记，不从残件 imports 自动学习，不用通配掩盖未覆盖子包）。
- **不提高空注释 baseline**（棘轮只降不升）；不删除失败门；不用 `grep/head/tail` 退出码代替原命令。
- 不改产品、不动主 spec、不动旧 change、不提交/归档。

## Capabilities

### 本域 deltas（`specs/` 内）

- `resident-release-evidence`：**ADDED** — 完整门禁与 golden 真实运行输出（值断言非键名）+ 离线迁移只读旧数据新路径 / 不伪 restore 副作用。
- `wiki-code-sync`：**MODIFIED** — 历史 wiki 以**固定 SHA** 校验（历史记录非现行代码）；新文档映射**新代码**。
- `race-gate-coverage`：**MODIFIED** — race 门覆盖第一方全包 + 检查器负控，MUST NOT 收窄集合或放水分类器换取绿。

### 引用（不重复持有 delta，避免同一能力多写者）

- `architecture-guardrails`：**由 `00-foundation` 收敛唯一承载**（`changes/00-foundation/specs/architecture-guardrails/spec.md`：分层依赖方向可机械断言 / 唯一编排发布权与中性契约 / 检查资产区分三类证据 / 质量门迁移不得放宽判据）。本域只**引用**该 delta 并以 09.1/09.3 验收，**不再另持一份**，也**不声称旧 architecture 条款语义不变继承**。

### 继承（requirement 语义不变，**不复制归档 history 全套**）

- `code-documentation`：代码注释形态白名单、go doc 契约边界、索引落点、注释剥离等价证明、生成式 API 文档新鲜度、棘轮基线扫描面、等价对账仪器判据、源文件职责索引——**继承**。
- `ops-deployment-integrity`：运维探针认证集成、部署状态链完整、HTTP 服务宿主持有——**继承**。

## Impact

- 未来实现范围：`scripts/check_runtime_*.py`、`scripts/test_check_runtime_*.py`、`golden/runtime/*.json`、`.github/workflows/runtime-contracts.yml`、`tests/integration/acceptance/**`（均由主代理 tooling 单创建/落盘）。
- 本轮写域仅 `openspec/changes/runtime-rebuild/changes/09-acceptance/**md`。
- 09 域孙任务共 **6** 项（09.1–09.6），全部未勾选。
