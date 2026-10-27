# 设计决策（D0–D6，基线 HEAD `bd05430e`，重定基线 2026-10-27）

## D0 结构与派发模型（skill 粒度判据 + R21 闭包实测）

**单层 change + ORCHESTRATION.md，不设二级子变更**。判据（skill 粒度规则「一级拆不出 ≥3 个文件零相交的可并发任务则合并为单」）：逐叶触碰集实测——

| 叶 | 改写文件 | 所属包/闭包 |
|---|---|---|
| 1.1 | facts.go / tools.go / age_fold.go | 根包 |
| 1.2 | memory/event_body.go（消费者 run.go/read.go/store_query.go 为根包+memory） | memory → 根包 |
| 1.3 | facts.go / run.go / request.go | 根包 |
| 2.1 | （核验叶，只读+证据） | 读根包测试闭包 |
| 2.2 | run.go / request.go / rl/ | 根包 + rl |
| 2.3 | api/plugin/ + telemetry/ + 注册面（settle_turn.go 等） | api/plugin → 根包 |
| 3.1 | api/plugin/present.go / phases.go / material.go | api/plugin → 根包 |
| 3.2 | memory/replay/ + request.go | memory/replay → 根包 |

任两叶的构建闭包皆在**根包**相交（根包 import api/plugin、memory、rl；根包测试编译全链）——按 R21（闭包污染按构建系统传递依赖传导，非包名直觉），**批内并发=半成品互踩**，零并发收益。故：派发单元=批（D4 合单），批间串行（正确性依赖：批 2 的 2.2 前置=无、2.3 无前置但同簇；批 3 的 3.2 前置=2.2 快照单源）。**并发轴三角色**：实施子代理（逐批）× 波后独立验证子代理（R30）× 后台门禁 shell（R32b，长跑门禁不占编排回合）。

被否方案：①二级子变更三份四件套——纯增仪式，无并发收益（闭包实测）；②批内多代理并发——违反 R21，红绿采证互相污染；③编排者亲实施——违背用户「全程多 agent」裁定。

## D1 三批顺序与启动门

批 1 删正确性分支 → 批 2 删重复权威 → 批 3 降抽象成本。启动门=fidelity-and-recovery 全完——**已满足**（17/17 归档于 `3c2cf89`）。

## D2 子域表（重定基线后）

| # | 批 | 一句话边界 | 外部依赖 | 锚测试（正锚） |
|---|---|---|---|---|
| 1.1 | a | 提交单入口（删吞错路径，失败观测归调用点） | facts.go/tools.go/age_fold.go | TestFactFidelity |
| 1.2 | c | 完整事件为恢复源，索引派生，legacy 删除 | memory/event_body.go | TestFactFidelity + 冷启动 RequestFidelity |
| 1.3 | b | 历史读取统一（删 history map 与注册门） | facts.go/run.go/request.go | TestHistoryRestoresWithoutBudget |
| 2.1 | d | 核验+关闭（旧状态已删，边界登记） | settle_turn.go 只读 | TestSettleWakeSingleDrive |
| 2.2 | e | 快照单源（删 Run.snapshots，读回走 Capture） | run.go/request.go/rl | rl Seal 契约 + TestRequestFidelity |
| 2.3 | ① | Observer 责任分层（Observer-only/Driver） | api/plugin/telemetry | TestSettleWakeSingleDrive + telemetry |
| 3.1 | f | 删 Phase 保 Port | api/plugin | api/plugin + evolution 包测试 |
| 3.2 | g | 呈现无长期决策（删 decisions 复用） | memory/replay/request.go | TestRequestBudgetEnforced（G9 锚不动） |
| 3.3 | — | 不变量锚全量核验 | 测试面 | D5 四锚全量 |

## D3 关键取舍

1. **Seal 不动**：rl.Capture 接契（二封返回首账），封存责任另评估——(e) 只做快照单源。
2. **no legacy, pure and clean**（用户裁定推翻原 Q4）：legacy 反造读分支**删除**；旧格式数据不迁移、冷启动重建（kv.json 先例）；EventBody 只有全字段一种格式。
3. **G9 症状锚不动**：(g) 只加"无长期决策状态"断言——R29 安全。
4. **批准权**：改语义断言=编排者批准+登记；掩盖缺陷的改断言=禁止。
5. **预算硬约束优先**：noticeCard 无下限=预算漏洞，(g) 修（room<0 分支）。
6. **2.1 降格为核验叶**（2026-10-27 重定基线）：旧状态已删是已落事实，核验不改产品码；跨会话串行 pump 登记为已知边界。
7. **主规格风险点（F1 活查）**：`decision-capture` 主规格有「可选SDK决策快照」条款——2.2 删 Run.snapshots 不得破坏该契约（快照能力由 Capture 承接后条款仍真）；批内若发现条款失真，走 MODIFIED delta 并登记。

## D4 合并规则

批=派发单元（根包簇合单）：批 1 三叶一单（1.1→1.2→1.3 单代理内串行）、批 2 三叶一单（2.1 核验先行→2.2→2.3）、批 3 三叶一单（3.1→3.2→3.3）；批间串行（2.2→3.2 前置链）。

## D5 不变量判定锚（红=违例，全量命令见 tasks 各叶）

| 不变量 | 锚测试 | 命令 |
|---|---|---|
| 事实重启同语义 | TestFactFidelity + 重装配全量 | `go test ./tests/integration/host -run 'TestFactFidelity' -count=1` |
| 协议合法 | TestRequestFidelity（三场景）+ TestRequestFidelityProtocolGate（负锚） | `go test ./tests/integration/host -run 'RequestFidelity' -count=1`；冷启动 `TAGENT_FIDELITY_COLD_BOOT=1` 同命令 |
| 责任有据 | TestSettleWakeSingleDrive（重启恰一驱动/回执/重放三子测） | `go test ./tests/integration/host -run 'TestSettleWakeSingleDrive' -count=3` |
| 状态有主 | 读写点收敛断言：结构 grep（history[/snapshots/commitFact 单入口/decisions 复用=0）+ TestRequestSnapshotsSingleSource（2.2 新建） | 见各叶验证面 |

## D6 逐叶验收锚（R29：随任务行冻结，执行期只对账不缩水）

**负锚均为勾选硬门**（测试不存在或未被证据点名=不得勾选）；**消费者锚=正式入口/真实路径调用证据**（组件绿≠消费者成立）。

| 叶 | 正锚 | 负锚（新建，具名） | 消费者锚 |
|---|---|---|---|
| 1.1 | TestFactFidelity 复跑 | TestFoldCommitFailureSurfaces（新建 tests/integration/host：注入拒写 tier→age_fold/tools 提交失败必可观测，不静默成功） | TestRequestFidelity 复跑 + `go build ./...` |
| 1.2 | `go test ./memory/... -short` + TestFactFidelity | TestDecodeEventBodyLegacyRefused（新建 memory 包：legacy event_key 字节→ErrEventBody 具名拒绝） | `TAGENT_FIDELITY_COLD_BOOT=1` RequestFidelity（冷启动正式读回派生字段） |
| 1.3 | TestHistoryRestoresWithoutBudget + `go test ./runtime/agent -short` | TestSingleHistoryPath（新建 tests/integration/host：同会话有/无预算恢复历史逐消息相等）+ 结构断言 `grep -c "history\[" facts.go`=0 | RequestFidelity + 冷启动复跑 |
| 2.1 | TestSettleWakeSingleDrive `-count=3` | 结构证据 `grep -cE "retained\|firing\|rearm" settle_turn.go`=0 | 同正锚（真子进程 SIGKILL+重启=正式路径） |
| 2.2 | rl `-short`（Seal 二封契约）+ TestRequestFidelity | TestRequestSnapshotsSingleSource（新建 rl 包：RequestSnapshots/callArgumentsOf 与 Capture SnapshotOf 同源）+ `grep -c "snapshots" run.go`=0 | `go test ./runtime/agent -short`（loop sealCapture 生产消费） |
| 2.3 | TestSettleWakeSingleDrive 复跑 + `go test ./telemetry ./rl -short` | TestObserverLayering（新建 api/plugin：Observer-only 失败=记缺口流程继续；Driver 失败=义务保留，只记日志=红） | 同正锚（真实 Driver=SettleDriver） |
| 3.1 | `go test ./api/plugin -short` + `go build ./...` | 结构断言：plugin.Phase/RouteOf 仓内非测试引用=0；examples/contract api_plugin 面同步 | `go test ./evolution/... -short`（meditation 经 Port） |
| 3.2 | TestRequestBudgetEnforced 复跑（G9 锚不动） | TestPresentationRecomputedOnBudgetShrink + TestNoRecordedPresentationReuse（新建 memory/replay：预算缩→重裁；两次装配预算不同→呈现不同；recordLocked 复用路径=0） | TestRequestFidelity 三场景 |
| 3.3 | D5 四锚全量 | F3 定点破坏（四不变量各一） | 全部消费者锚复跑 |
