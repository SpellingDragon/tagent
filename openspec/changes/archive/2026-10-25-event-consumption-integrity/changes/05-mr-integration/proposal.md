# 05 MR 集成：事件双档必跑、required 汇总与发布证据

> 状态：规划物化，未实施。`ci.yml` 尚无事件作业，`preflight.sh` 未接事件入口，required 汇总与文档更新均不存在；文中命令一律未运行，不得当作通过证据。

## Why

- `.github/workflows/ci.yml` 当前只有 `test`、`race`、`validators`、`openspec`、`soak` 五个作业；事件 short/long/mutations 没有任何每 PR 入口，分支保护也没有可挂的稳定检查名。
- `soak` 是 `workflow_dispatch` 手动作业，且命令与现树不一致：`./tests/` 包路径不存在，`TestSoak_Continuity` 也不存在（实际为 `tests/integration/host` 的 `TestSoakContinuity`，无 `soak` tag）。这是静态线索，实施时先验证归属再最小纠正。
- `scripts/preflight.sh` 只跑 lint、deterministic golden、三模块测试与单次 smoke；未调用事件入口。它当前只有 `--skip-smoke`，没有事件 long 入口；本域恢复真源：默认同一完整事件 short/long/mutations 入口，不新增以成本为由默认关闭 long 的 opt-in 开关，也不以“成本高”猜测取消默认 long。
- `README.md`、`docs/OPERATION.md`、`docs/FACT-VOCABULARY.md`、`docs/GLOSSARY.md` 未登记事件门、档位、证据边界与失败处置；若照旧描述，会让读者以为旧检查器已支持新图。
- 本地确定性完成、远端 MR/required、72h 与真实服务三层证据没有分层登记点，容易被一次本地绿证冒充为发布门通过。

## What Changes

- **05.1 每 PR 事件作业**：在 `ci.yml` 增加 `event-integrity-short`（十次独立 `-count=1 -race -json`，逐次 UUID run-id/日志/证据）、`event-integrity-long`（`seed [1,7,42] × window [256,32768]` 六分片，`fail-fast: false`，每片 `-count=1 -race`）、`event-integrity-mutations`，保留全部旧作业；无路径过滤、无手动条件、无 `continue-on-error`、无凭据跳过；显式模块与 tags。**单 long 分片是 1200 输入/120 重启，六分片合计 7200/720**，不得把任一片写成 7200/720。同步 `preflight.sh` 默认调用同一 short/long/mutations 入口；`--skip-smoke` 保留原语义并标注“不具备完整 MR 等价证据”，事件 long 仍默认运行，跳过项不影响 MR 必跑。对手动 `soak` 做最小接线纠正，不把它算作新 MR long。
- **05.2 required 汇总**：新增 `event-integrity-required`（`if: always()`，`needs` 三档），逐项核对上游 success、六组合清单、short 十次清单、变异控制与具名拒绝证据、同一基线源树摘要；cancelled/skipped/缺失一律失败，失败也上传证据；扩展现有 `scripts/test_check_runtime_workflow.py` 同时解析 `runtime-contracts.yml` 与 `ci.yml`，保留旧目标，不新建第二解析器。`required` 只是**新增事件三档的汇总检查**，不是全库“唯一必跑”，不得替代或删除 `test`/`race`/`validators`/`openspec` 等旧检查；分支保护可同时保留旧检查名。
- **05.3 文档与证据边界**：更新 README、OPERATION、FACT-VOCABULARY、GLOSSARY 的事件门、档位、命令、失败处置与可证明边界；旧的 `check_runtime_evidence.py` 不得被描述为已支持新图；历史文档只附勘误、不改写；不新增文档篇目、不上调 `scripts/doc_budget.json`。**终验明确依赖 05.2**：可先写草稿，但 CI 作业段落只能标“拟新增/未启用”，05.2 真实输出与 workflow 自测通过后才算完成。
- **05.4 最终集成与分层验收**：在最终集成树运行 MR 等价命令，独立偏差验收；本地完成（L1）、远端 MR/required（L2/F4）、72h/真实服务（L3）分别登记，未授权的远端动作保持未勾选/待验。remote CI 绿不是 05.1/05.2 本地叶的完成门；本地消费者是“与 CI 相同的命令 + workflow 结构自测”，远端 required 真实生效另由 F4 验证。

## 边界与依赖

- **依赖（开发前置与最终绿证前置分开）**：05.1 ← 03.11、04.5；05.2 ← 05.1（04.5 经 05.1 传递）；05.3 开发前置 ← 01.5、04.5，**最终绿证前置 ← 05.2**（草稿可先写，不算完成）；05.4 ← 05.2、05.3。依赖无环；波次只作汇报分组。同步图：05.1 → 05.2 → 05.3 → 05.4，且 05.2 → 05.4。
- **被依赖方**：F2 全量门禁、F3 独立验收、F4 远端授权与归档。F4 的远端 CI 绿、required 保护生效与归档不在 05.1/05.2 的本地完成门内。
- **接口面**：稳定的 job 名（`event-integrity-short`/`long`/`mutations`/`required`）、artifact 名、`check_runtime_evidence.py --bundle` 汇总模式（由 04.5 提供）、`preflight.sh` 默认事件入口、`test_check_runtime_workflow.py` 的双 workflow 目标、`runner.temp` 下的产物目录。不是产品 API。
- **禁止事项**：不新建第二 workflow 解析器或平行评估系统；不以路径过滤、手动条件、凭据、`continue-on-error` 跳过事件门；不把六组合压成一次、不把十次 short 合成一次 count 放大；不把 `soak` 手动作业算作 MR long；不把 `required` 做成全库唯一必跑或替代旧检查；不新增以成本为由默认关闭 long 的开关，也不以成本猜测取消默认 long；不把本地绿证当远端 required 通过；不把未核验目录宣称为已被忽略；未授权不提交/推送/建 MR/改合并保护；不改产品代码/测试/CI 之外的 03/04/05 文档边界。

## Capabilities

### Modified Capabilities

- `resident-release-evidence`：新增“每 MR 事件双档与故障变异必跑”“required 汇总不因取消跳过缺失而放行”“文档只陈述已落地能力与可证明边界”“本地完成与远端必跑门分层登记”四条义务；delta 位于本域 `specs/resident-release-evidence/spec.md`。既有“双模块与真实消费者验收”“完整门禁与 golden 真实运行输出”“发布与外部操作权限”条款原样适用，不复制为新增要求。

## Impact

- 未来实施只改 `.github/workflows/ci.yml`、`scripts/preflight.sh`、`scripts/test_check_runtime_workflow.py` 与 README/docs 四篇；不改产品源码、产品测试、既有检查器判据或 `race_check.sh` 参数系统。
- `race` 作业的受影响集由**全部修改的产品文件与全部正式测试包**映射（至少含 `tests/integration/{execution,history,host,capabilities,evolution,learning}` 与所有被改根包，含 `runtime/{agent,task,state}`、`memory`、`evolution`、`rl`、`tool`、`api/event`、`api/plugin` 与根包），不仅 host/history；旧 `race_check.sh` 的上游 waiver 只对非 tagent frame 生效，不豁免新事件套件，新测试不得以 `-skip`/build tag 逃避 race。
- 长程预算（short 20m、long 45m、mutations 30m）仍只是提议；05.1 的 `timeout-minutes` 只作熔断，需用 03.10 的六项校准值加明确余量后冻结，未能校准时如实标注“待校准”，不把提议值当结论。
- 05.4 只读最终树并保存证据；提交、推送、远端 MR、required 保护规则与 72h/真实服务均在另行授权后才执行，未授权即 BLOCKED/待验。原始运行产物（原始日志、`evidence.json`、原始退出码）写仓库外临时目录或 CI `runner.temp`；证据摘要与 `log_ref` 统一记入根 `ORCHESTRATION.md` 台账，不另建每叶 summary 文件；不把原始日志写进 change 规划目录，也不宣称未核验目录已被忽略。
