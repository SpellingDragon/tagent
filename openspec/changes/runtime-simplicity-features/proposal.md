# Proposal: runtime-simplicity-features（deferred，不在本轮实施）

> **状态：deferred（冻结承接面，无代码开工）**。本变更是 `hot-update-and-purity-closure/changes/09-runtime-simplicity` 特性叶的独立承接面，按用户 2026-11-08 裁定"09 其余特性叶拆 follow-up、本轮只收口纯核心"从 09 剥离。`tasks.md` 全部为 `- [ ]`，勾选即意味开工；本轮不派发、不占写面、不计入任何一级变更的完成度。

## Why

09 在"纯核心"上已闭合并附复跑凭据（1.1–1.6 追加日志/原子提交/单屏障、4.1 观测实例注入、以及已勾的 2.1/2.3/5.1/5.2/5.3/6.1）。余下特性叶长期未勾，原因不是判断未定，而是**验收面或生产者落在尚未开放的写面**：`tests/integration/upgrade/` 目录不存在、`assembly/**` 与 `runtime/agent/**` 归他单、`training/` 目录在本仓库不存在、`scripts/**`+`docs/**` 归文档与门禁单。把它们留在 09，会让一级变更的归档被这些"等写面放开"的叶无限期拖住，也让 09 的开放数虚高。故独立成变更，逐叶登记阻塞归因与重入触发条件。

## What Changes

- 基线见证与规格落定：`TestRuntimeBaselineWitness` 缺陷基线（0.1）、受影响主规格清单（0.2）。
- 请求证据闭环：loop 内生效参数合并进证据（2.2）、按引用读回并校验摘要（2.4）、读取面口径声明与 `capture.go` 注释纠正（2.5）。
- 观测补齐：请求终态/阶段耗时/token 用量（缺失记 unknown）/订阅丢弃指标（4.2）；查询面补采集损失与待处理义务数量/年龄/下一推进者（4.3）。
- 训练导出真实消费者：`training/tests/test_consumer.py` 用真实 tokenizer/chat template/整理器断言历史掩码为零（5.4）。
- 收敛与门禁：删除只固定旧代码形状的门（6.2）；文档分区落地与 `docs/api` 再生成（6.3）。
- 收尾 F1–F4：契约核对、全量门禁、DoD 核验、归档（原属 09 的收尾，随特性叶一并转移）。

不做：任何流式改造（已独立至 `runtime-streaming-increments`）、在线 RL、权重训练、第二 provider、SQLite。

## Capabilities

### New Capabilities
- 无。

### Modified Capabilities
- `request-evidence`：证据覆盖生效参数的落点从"代码已落、验收面缺"补齐为可复跑验收；读取面标明"运行时请求快照"而非 provider wire body。
- `runtime-observability`：补关键指标（请求终态、阶段耗时、token 用量、订阅丢弃），标签限于有限枚举。
- `runtime-inspection`：补采集损失与待处理义务数量/年龄及下一推进者。
- `offline-training-export`：真实 tokenizer/chat template/整理器消费，历史掩码为零、模板不支持即失败。

### 无 capability delta 的叶（过程/门禁/文档）
0.1、0.2（基线与规格清单）、6.2（形状门删除）、6.3（文档分区）、F1–F4 不新增或修改主规格条款，验收入 tasks 的逐条命令，不另立 spec。

## Impact

### 代码（预计触碰，本轮不改）
- `runtime/agent/loop.go`（2.2 已有 `withEffectiveConfig`，缺验收面）、`assembly/request.go`（2.4）、`features/rl/{capture,line}.go`（2.5 注释口径）。
- `features/telemetry/metrics.go`（4.2，文件当前不存在）、`assembly/host_diagnostics.go`（4.3）。
- `training/tests/test_consumer.py`（5.4，目录当前不存在）。
- `scripts/lint.sh`、`.github/`、`features/rl/skeleton_assert.go`（6.2）、`docs/**`（6.3）。

### 数据与兼容
- 不改持久格式、不改 `api/event`、不改 `api/config`（F1 契约零改动断言）。

### 验收面（冻结，不得缩水）
- 0.1：`go test ./tests/integration/upgrade -run '^TestRuntimeBaselineWitness$' -count=1 -json`，红项须命中"参数缺失"或"证据不可读"，编译失败/超时/零匹配不算红。
- 2.2/2.4/4.2/4.3/5.4/6.2/6.3：逐叶命令与负锚见 `tasks.md`，按原 09 冻结文本原样保留。

## 重入触发条件（未决定性，先登记）

1. `tests/integration/upgrade/` 目录建立（0.1/0.2/2.2 共用，与流式变更的 P1 同批）。
2. `assembly/**`、`runtime/agent/**`、`scripts/**`、`docs/**` 的写面放开（按叶分别申请，不做一次性全放开）。
3. `training/` 目录与 Python 依赖链在本仓库落定：5.4 的前提不是授权而是目录存在性（`test -e training` 当前为 MISSING，`training/tests/test_consumer.py` 与 `uv` 配置均为新立面）。
4. 用户明示"特性叶开工"。
