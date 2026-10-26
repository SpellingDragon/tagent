# 全事件消费完整性与每 MR 防退化

## Why

后台任务完成后出现额外确认的用户现象，暴露了事件生产、唤醒、消费确认与发送之间需要逐身份核验的责任边界。当前已有局部去重、恢复及负向测试，但正式消费者链路与每 MR 必跑证据尚不能充分证明既不重复处理、也不因防重而漏处理；精确运行根因仍待复现。

## What Changes

- 覆盖当前生产支持的八类事件机制，先冻结契约及可证伪证据，再修复证实的业务缺陷并补正式消费者回归。
- 分清通知身份、事实引用、原调用责任、消费确认和外部发送确认；复用现有任务、邮箱与恢复材料，不建立第二消费状态仓。
- 内部运行正式装配，外部模型、微信与外部工具服务使用本地替身；每 MR 运行短程、长程和故障变异，保留原有 CI、Lint、Golden 与质量基线。
- 长程六组合：种子 1/7/42 × 回放窗口 256/32768；每组合四会话共享 Host、1200 个不同业务输入、120 次真实进程重启，合计 7200 输入与 720 次重启。
- 发送结果未知时保留未确认状态和动作关联，不自动重发、不伪报送达，不承诺无渠道幂等能力时的端到端恰好一次。
- 本变更采用一级编排、六个二级域与 42 个实施叶（计划回写④后：01 域 11 叶、02 域 7 叶）；能力细节下沉到二级，一级仅保留编排契约。

## Capabilities

### New Capabilities

- `session-fact-lifecycle`（二级 `01` 域新增）：会话事实提交即持久化、当次召回、索引失败可恢复与新事实可达性底线（`01.8`/`01.9`）。
- `execution-resource-lifecycle`（二级 `01` 域新增）：外部执行五态查询面、状态事件生产与通知接线、租约/取消、重启接管与完成判定（`01.10a`/`01.10b`，两叶分批实施，最终验收面不变）。
- 其余扩展既有能力，不新建事件平台或平行评估系统。

### Modified Capabilities

- `orchestration`：本次变更的规划/实施授权分离、叶级依赖、写入归属、证据与验收门，delta 位于本级 `specs/orchestration/spec.md`。

二级能力登记：

| 子域 | 下沉能力 |
|---|---|
| `00-contract-evidence` | `event-metadata-contract`、`committed-event-attribution` |
| `01-runtime-consumption` | `persistent-event-loop`、`async-task-execution`、`telemetry-channel`、`delivery-dispatch-receipts`、`session-fact-lifecycle`、`execution-resource-lifecycle` |
| `02-event-consumers` | `meditation-idle-gating`、`meditation-delivery-tool`、`cross-session-delivery`、`event-sourced-projection`、`config-hot-reload`、`approval-channels`、`decision-capture`、`offline-training-export` |
| `03-formal-simulation` | `evals-harness` |
| `04-regression-gates` | `architecture-guardrails` |
| `05-mr-integration` | `resident-release-evidence` |

纯引用既有要求不复制为新增要求。二级 delta 归档前按 capability 合并到标准顶层 specs 并校验，再由标准 OpenSpec archive 同步；不能仅归档本级编排契约。

## Impact

- 未来实施涉及根运行时、任务与状态、冥想、投影、模型替换、审批、采集，以及微信和 CLI 两个独立宿主模块；逐叶范围在二级 design 与 ORCHESTRATION 中登记。
- 检查沿用现有 scenario runner、证据/架构/工作流检查器及 preflight；新事件场景合同与旧 S01—S15 共用引擎，旧期望不因新实现而自动更新。
- 本轮授权仅为 OpenSpec 规划物化与校验，不包括产品代码、测试、CI、依赖、主规格、历史归档、用户配置/数据或原有未净子模块的修改。
- 实施、提交、推送、真实服务验证、部署、合并保护和归档分别需要授权。真实模型/渠道、掉电、72h 与训练制品证据不能由本次模拟替代。
- 另立三项后续相依边界（检索候选排序语义、完整请求预算、业务结果验收与效果评测）已登记在 [findings.md](findings.md)「后续相依变更边界」，待用户另行发起；本变更不实施。

## 规划入口

- 静态决策：[design.md](design.md)
- 派发与台账：[ORCHESTRATION.md](ORCHESTRATION.md)
- 编排与收尾：[tasks.md](tasks.md)
- 覆盖及风险：[coverage.md](coverage.md)、[findings.md](findings.md)
- 机器可读依赖与验收锚：[task-graph.json](task-graph.json)
