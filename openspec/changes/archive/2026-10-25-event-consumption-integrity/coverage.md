# event-consumption-integrity：任务图与覆盖说明

> 状态：**规划物化（planning materialization）+ 计划回写（用户 2026-10-23 裁定 03 域细化）+ 计划回写②（用户裁定全域粒度复审：01.5/02.5 拆分）+ 计划回写③（用户评审 v3：补入完整会话持久化与外部执行生命周期两项 + 01.2/01.3 补差重开 + observer 语义分层 + 另立三项登记）+ 计划回写④（用户裁定 C：01.10 拆为 01.10a/01.10b 两叶分批，最终验收面不变）**，不是 apply**。本文件解释 `task-graph.json` 的 schema、84-phase 展开图、测试注册与 140 个 delta Scenario 的叶/锚映射；不表示未勾选叶已实施或已通过。
>
> 本轮用户授权是**计划回写④（用户裁定 C：01.10 拆两叶分批）**：01.10 拆为 01.10a 执行资源查询面（`tool/exec` 包内测试）与 01.10b 执行状态事件生产与通知接线（新测试 + 根装配）；五态归因与泄漏实证（`evidence/01.10-summary.json`）保留为两叶共同输入；01 域 10→11 叶，总 41→42 叶；级联同步 spec/graph/合同/checker/编排/总账；两叶均未勾选，勾选保持 32。当前 42 叶中 32 叶已勾选、10 叶未勾选（01.9、01.10a、01.10b、02.3、03.11、04.2、05.1—05.4）；03 域 11 叶中 03.1 已勾选，旧 03.3 monolith 标记 `superseded`（代码保留），其余 10 叶 `planned`。回写除 04.1 合同/checker/自测与 04.4 变异锚点的同步外未改产品/测试代码，未提交/推送/建 MR/改保护。
>
> 文中的“锚”是实施后必须取证的 selector/路径，不是已通过证据；“消费者锚”是正式生产入口/调用链证据，不以测试内手工接线冒充。

## 0. 授权、口径与图结构

- 授权：用户 2026-10-23 裁定 03 域细化并授权计划回写（第二轮全域粒度复审；第三轮评审 v3 补入持久化/外部执行生命周期与两项补差；第四轮用户裁定 C 将 01.10 拆为 01.10a/01.10b 分批）；改动面为 01 域四件套与两份 delta spec、00.3 冻结件、`task-graph.json`、本文件、`ORCHESTRATION.md`、根 `tasks.md`/`design.md`/`proposal.md`、`findings.md`、04 域引用、03/05 域引用文字，以及 04.1 合同/checker 自测与 04.4 变异锚点的计数同步。
- 禁止：apply 新叶、产品/测试代码（04.1 合同清单、checker 计数与 04.4 变异锚点除外）、CI、改既有勾选、远端动作（push/MR/保护规则/tag/真实服务/归档）。
- `scripts/check_runtime_plan.py` 已由 04.5 扩展为按 graph `domain_profile` 读取六域/叶数/phase 计数（旧十域常量保留）；本轮 `--change-dir ... --json` 实测 exit 0（42 叶/32 勾/10 未勾/84 节点/154 边/acyclic）。本轮只做只读校验，不跑产品测试。
- 计数口径：42 叶、29 父测试（28 green + 1 diagnostic）；当前 **137** 个 green 具名锚（**不含** 00.2 四个固定诊断锚），全部测试修复后 **29 父/141 具名锚**；**140** 个 delta Scenario。137/141 都是当前清单，**不是允许删项上限**；long 一父 3 锚（03.11）只运行一次 6 组合矩阵，不按 3 倍运行；交付稳定性 6 组合各 10 次另计（72000 输入/7200 重启，实施期研究，非 MR ×10，预算另计）。
- 00.2 的 red 是**预期非零诊断基线**，不是产品回归失败；其 green 是同一父测试修复后的十轮独立证据，纳入最终 `event-short`，不因诊断角色永久排除。四个固定具名子测试为 `control_normal_path`、`duplicate_notice_material`、`busy_notice_loss`、`request_and_send_identity_correlation`；不设 `static_risk_*` 通配替代；静态风险与原案例的差异写入证据 `basis`（`original_case_reproduced=false`、`basis=static_risk`、`original_root_cause=false`）；未来真新增场景必须先同步 manifest 与 `test_registry`，再加具名子测试。

## 1. schema 与可复跑计数

### 1.1 顶层结构

| 字段 | 值/含义 |
|---|---|
| `schema_version` | `1` |
| `change` | `event-consumption-integrity` |
| `domain_profile` | `event-integrity`（顶层 string 保留；04 域后续支持此 shape） |
| `baseline` | `6cc441436c2aab15edcee34cf2561086232c8311` |
| `scope` | `whole-change-planning-materialization` |
| `generated_from` | 根 design/proposal/tasks/ORCHESTRATION/findings、orchestration spec、6 域 tasks/design、全部 delta spec |
| `authorization` | 计划回写授权（`round=plan-revision-04-split-exec-lifecycle`）；`current_pass_edits` 为规划物 + 00.3 冻结件 + 04.1 合同/checker 计数 + 04.4 变异锚点；`implementation_authorized=false`；`checked_leaves=32` |
| `modules` | root `github.com/SpellingDragon/tagent`、`wechat-bot`、`tagent-cli-host`；三个 go.mod 已核对；其余旧 `module` 字段保留向下兼容 |
| `dependency_semantics` | `depends_on`/`verify_after` 定义 + 84-phase 展开 + 单一展开图判环 |
| `phase_graph` | 84 节点 / 154 边 / 单一拓扑排序 / `acyclic=true` |
| `file_locks` | 文件锁簇与 ready 队列仲裁；不生成顺序边 |
| `counts` | 42 叶、29 父、137 当前/141 修复后、140 delta Scenario、short/long 策略 |
| `test_registry` | 29 父测试的 `id/contract_id/module/module_path/dir/package/file/selector/tags/tier/state` |
| `leaves` | 42 叶，每叶 `id,domain,kind,task_file,depends_on,verify_after,write_files,tests,verification,status` |

### 1.2 显式计数（从清单导出，不写死）

| 项 | 值 | 导出方式 |
|---|---|---|
| 叶总数 | 42 | 六域求和：4+11+7+11+5+4 |
| 域分布 | 00:4、01:11、02:7、03:11、04:5、05:4 | 六域 `tasks.md` 逐文件计数 |
| 类型 | product 29、non_product 13 | JSON `kind` 计数 |
| 状态 | planned 42、done 0 | JSON `status` 计数（graph 按规划态记；`tasks.md` 已勾 32 叶、未勾 10 叶） |
| 父测试 | 29 = 28 green + 1 diagnostic（00.2） | `test_registry.length` |
| 当前 green 具名锚 | **137**（不含 00.2 四锚） | `counts.named_green_subtests_current`（按 `(parent, subtest_name)` 去重；consumer 可引用 positive 同名 subtest，不重复加 `subtests[]`；CI 合同分别记录 roles 与引用，不以 raw role list 长度求和） |
| 全部修复后具名锚 | **141** = 137 + 00.2 四锚；29 父/141 锚 | `counts.named_anchors_after_all_fixes` |
| long 父锚 | 3，03.11 共用同一次 6 组合矩阵，不三倍 | `counts.long_parent_anchors` |
| 交付稳定性 | 6 组合 × 10 = 60 次；72000 输入/7200 重启；实施期研究，非 MR ×10，预算另计 | `counts.long_matrix`、`03.11` |
| delta Scenario | **140** = 118 + 10（session-fact-lifecycle）+ 12（execution-resource-lifecycle） | 18 份 `spec.md` 的 `#### Scenario:` 计数（03 域 17） |
| phase 节点/边 | **84 / 154**，单一展开图 `acyclic=true` | `phase_graph` |
| short | 10 次独立、每次 `-count=1 -race -json`；含 00.2 修复后最终绿 | `counts.short_policy` |
| long | 6 组合；单分片 1200 输入/120 重启；合计 7200/720；4 会话共享 Host；每组合 1 次 + race | `counts.long_matrix`、`03.11` |
| 文档预算 | 206 files / 21934 lines（measured 2026-10-22） | `scripts/doc_budget.json`；05.3 不上调 |

### 1.3 复跑命令（本轮只读）

以下命令均在仓库根执行；子模块用 `go -C <相对目录>`，不写本机绝对路径。

```bash
cd "$(git rev-parse --show-toplevel)"
python3 -c "import json; g=json.load(open('openspec/changes/event-consumption-integrity/task-graph.json')); pg=g['phase_graph']; print('leaves',len(g['leaves']),'registry',len(g['test_registry']),'phase_nodes',pg['nodes_total'],'phase_edges',pg['edges_total'],'topo',len(pg['topological_order']),'acyclic',pg['acyclic'])"
python3 -c "import json; g=json.load(open('openspec/changes/event-consumption-integrity/task-graph.json')); r=g['test_registry']; print('current',g['counts']['named_green_subtests_current'],'after_fixes',g['counts']['named_anchors_after_all_fixes'],'long_anchors',g['counts']['long_parent_anchors'],'ids_unique',len({x['id'] for x in r})==len(r),'contract_ids_unique',len({x['contract_id'] for x in r})==len(r))"
grep -rh '^#### Scenario:' openspec/changes/event-consumption-integrity/specs/*/spec.md openspec/changes/event-consumption-integrity/changes/*/specs/*/spec.md | wc -l
grep -rh '^- \[ \]' openspec/changes/event-consumption-integrity/changes/*/tasks.md | wc -l
grep -rh '^- \[x\]' openspec/changes/event-consumption-integrity/changes/*/tasks.md | wc -l
```

实测关键输出：

```text
leaves 42 registry 29 phase_nodes 84 phase_edges 154 topo 84 acyclic True
current 137 after_fixes 141 long_anchors 3 ids_unique True contract_ids_unique True
Scenario 总数 140
tasks 未勾选 10、已勾选 32
```

## 2. 84-phase 展开图：单一图判环

### 2.1 展开规则

| 边 | 连接 | 含义 |
|---|---|---|
| phase | 本叶 `ready` -> 本叶 `verified` | 产物就绪后才做本叶最终验收 |
| `depends_on` | 前置 `ready` -> 本叶 `ready` | 只要求前置产物 ready，不要求前置 checkbox |
| `verify_after` | 前置 `verified` -> 本叶 `verified` | 本叶最终勾选前必须完成的正式复验 |

### 2.2 规模与拓扑

- 节点 **84** = 42 叶 × 2 phase。
- 边 **154** = 42 条 `ready->verified` + 77 条 `depends_on` + 35 条 `verify_after`。相比 41 叶时：新增 1 条叶内 phase 边（01.10 拆为两叶）；depends_on +2（01.10a 一条挂 00.3，01.10b 两条挂 01.10a/00.3；原 01.10 仅一条）；verify_after 不变（两叶均 `[]`，04.4 改指 01.10a 不增边）。
- 单一展开图 `acyclic=true`，84 节点全部进入拓扑序。只对这一张图整体拓扑排序，不再拆成两张图各自判环，也不把跨阶段回边解释为“正确”。
- 拓扑序前 8：`00.1/ready, 03.1/ready, 00.1/verified, 00.4/ready, 00.2/ready, 03.1/verified, 00.4/verified, 00.3/ready`。
- 拓扑序后 8：`05.1/ready, 05.3/ready, 05.1/verified, 05.2/ready, 05.2/verified, 05.4/ready, 05.3/verified, 05.4/verified`。
- 完整顺序见 `phase_graph.topological_order`；判环命令见 §1.3。

### 2.3 关键路径的展开

1. **02 -> 01.5 / 02.6 -> 01.6**：`02.x/ready -> 01.5/ready -> 01.5/verified -> 02.x/verified`（02.1—02.5、02.7）；`02.6/ready -> 01.6/ready -> 01.6/verified -> 02.6/verified`；01.5/01.6 不等 02 的 checkbox。
2. **00.2 -> 00.3 -> 01.x -> 01.5 -> 00.2**：`00.2/ready` 红证供 `00.3/ready`；`01.5/verified` 后回填 `00.2/verified` 绿证；在单一展开图中方向一致，拓扑排序通过。
3. **05.3 -> 05.2**：05.3 草稿可先写；`05.2/verified -> 05.3/verified`。
4. **04.4 同方向重复前置**：`depends_on` 连 ready、`verify_after` 连 verified，不是环；03 侧引用已收敛为 `03.2/03.3/03.9`（重启叶重编号后不再指向旧 03.4）。
5. **03 拆分后的长程链**：`03.2/03.3/03.9/03.10/03.11` 全部 ready 后 05.1 才 ready；03.10 只读校准无 `verify_after`，不引入 01.5 回边。
6. **01 持久化/恢复/执行生命周期链**：`01.8/ready -> 01.9/ready`（恢复消费 01.8 的持久化产物），二者均 `depends_on=00.3`、`verify_after=[]`；`01.10a/ready` 同挂 `00.3`，与 01.9 无依赖边，可并行；`01.10b/ready` 依赖 `01.10a/ready` 与 `00.3/ready`。04.4 对 `01.2/01.8/01.10a` 的 `depends_on` 只要求其 ready，不要求先勾选。

### 2.4 ready 队列与文件锁

- 锁只排除同时写同一文件簇，不表达业务依赖、不生成额外顺序边；持有者=当时 ready 队列中被选中的单写叶，释放后立即重算 ready 队列。
- 文件簇：`host-event-integrity-test`（00.2/01.3/01.4/01.5/02.2/02.4/03.2/03.3）、`history-event-integrity-test`（01.2/01.8/01.9/02.3/03.9）、`execution-event-integrity-test`（01.1/01.10b）、`exec-package-test`（`tool/exec/event_integrity_test.go`；01.10a）、`wechat-smoke`（01.6/03.4/03.5/03.6/03.7/03.8/03.10/03.11）、`golden-runtime`（04.1/04.2/04.3/04.5）、`ci-workflow`（05.1/05.2）、`meditation-core`（`evolution/meditation/{driver,reflection,watermark,deliver}.go`；02.1/02.2）、`root-package-tagent`（根生产文件组，含 `facts.go`/`run.go`/`prepare.go` 等；01.1—01.5/01.8/01.9/01.10b/02.4/02.7）、`memory-recall`（`memory/recall.go`、`memory/store_query.go`、`memory/ledger/*`、`memory/search/*`；01.8/01.9）、`exec-lifecycle`（`tool/exec/*`；01.10a/01.10b）、`approval-surface`（`evolution/governance/policy.go`、`runtime/task/approval/{approval,approval_store}.go`、`examples/wechat-bot/wechat_approval.go`；02.5/01.5/01.6/02.6）。
- 锁只约束同刻写同一文件簇，不新增依赖边；`phase_graph` 仍为 84 节点/154 边。
- 根生产文件在当前基线均已存在：`host.go`、`invoke.go`、`run.go`、`prepare.go`、`tools.go`、`facts.go`、`request.go`、`settle_turn.go`、`host_reload.go`、`host_model.go`、`host_capture.go`；`root-package-tagent` 约束编辑串行与编译闭包，不是新建文件。
- 不为文件锁写死任何固定叶序；同文件簇由 ready 队列仲裁单写，不同簇且构建闭包零交叉才可并发。

## 3. 42 叶总表

> 完整字段以 `task-graph.json` 为准；本表不再重复三锚计数。00.2 为 diagnostic 双模式，当前 137 不含其四锚，全部修复后 141。03 域 11 叶中 03.10 为 `non_product`（只读校准，`verify_after=[]`，不进 event 合同）。01.8/01.9 落 `tests/integration/history`（与 01.2/02.3/03.9 同文件簇串行），01.10a 落 `tool/exec`（包内测试），01.10b 落 `tests/integration/execution`（与 01.1 串行）。

| 叶 | 类型 | depends_on | verify_after | 测试 | 状态 |
|---|---|---|---|---|---|
| 00.1 | non_product | — | — | doc_checker | planned |
| 00.2 | product（diagnostic） | 03.1 | 01.5 | T00.2 | planned |
| 00.3 | non_product | 00.1、00.2、00.4 | — | doc_checker | planned |
| 00.4 | non_product | 00.1 | — | doc_checker | planned |
| 01.1 | product | 00.3 | — | T01.1 | planned |
| 01.2 | product | 01.1 | — | T01.2 | planned（补差重开） |
| 01.3 | product | 01.2、03.1 | — | T01.3 | planned（补差重开） |
| 01.4 | product | 01.3 | — | T01.4 | planned |
| 01.5 | product | 01.4、02.1、02.2、02.3、02.4、02.5、02.7 | — | T01.5 | planned |
| 01.6 | product | 01.4 | — | T01.6 | planned |
| 01.7 | product | 01.4 | — | T01.7 | planned |
| 01.8 | product | 00.3 | — | T01.8 | planned（新叶） |
| 01.9 | product | 01.8 | — | T01.9 | planned（新叶） |
| 01.10a | product | 00.3 | — | T01.10a | planned（拆分新叶） |
| 01.10b | product | 01.10a、00.3 | — | T01.10b | planned（拆分新叶） |
| 02.1 | product | 00.3 | 01.5 | T02.1 | planned |
| 02.2 | product | 00.3 | 01.5 | T02.2 | planned |
| 02.3 | product | 00.3 | 01.5 | T02.3 | planned |
| 02.4 | product | 00.3 | 01.5 | T02.4 | planned |
| 02.5 | product | 00.3 | 01.5 | T02.5 | planned |
| 02.6 | product | 00.3 | 01.6 | T02.6 | planned |
| 02.7 | product | 00.3 | 01.5 | T02.7 | planned |
| 03.1 | product | — | — | T03.1 | planned（已完成并勾选） |
| 03.2 | product | 03.1、00.3 | 01.5 | T03.2 | planned |
| 03.3 | product | 03.1、00.3 | 01.5 | T03.3 | planned |
| 03.4 | product | 03.1、00.3 | 01.5 | T03.4 | planned |
| 03.5 | product | 03.1、00.3 | 01.5 | T03.5 | planned |
| 03.6 | product | 03.1、00.3 | 01.5 | T03.6 | planned |
| 03.7 | product | 03.1、00.3 | 01.5 | T03.7 | planned |
| 03.8 | product | 03.1、00.3 | 01.5 | T03.8 | planned |
| 03.9 | product | 03.1、01.2 | 01.5 | T03.9 | planned |
| 03.10 | non_product | 03.1、00.3 | — | calibration_report | planned |
| 03.11 | product | 03.2、03.3、03.9、03.10 | 01.5 | T03.11 | planned |
| 04.1 | non_product | 00.3 | — | checker | planned |
| 04.2 | non_product | 04.1 | 01.5、03.2—03.9 | checker+production_acceptance | planned |
| 04.3 | non_product | 00.3 | — | checker | planned |
| 04.4 | non_product | 04.1、04.2、00.3、03.2、03.3、03.9、01.5、01.2、01.8、01.10a | 00.3、03.2、03.3、03.9、01.5、01.2、01.8、01.10a | checker | planned |
| 04.5 | non_product | 04.1、04.2、04.3、04.4 | — | checker | planned |
| 05.1 | non_product | 03.11、04.5 | — | workflow_checker | planned |
| 05.2 | non_product | 05.1 | — | workflow_checker | planned |
| 05.3 | non_product | 01.5、04.5 | 05.2 | docs | planned |
| 05.4 | non_product | 05.2、05.3 | — | integration_acceptance | planned |

## 4. 父测试注册表（29 = 28 green + 1 diagnostic）

> 当前仓库已发现 27 个合同内父测试（01.9 于本轮校验期间落地）；01.10a/01.10b 的父测试尚未落地，作为 `pending` 两个父登记（其子场景在源码落地前按 graph `test_registry.state=to_be_added` 与合同冻结选择器门控，不伪造 green）。补差锚 `verification.pending_anchors` 仅用于 01.2/01.3 两个已发现父测试内的新增子场景。旧 03.3 monolith `TestEventIntegrityControls` 已在 `source_only_tests` 标记 `superseded`（代码保留、不进合同），03.10 的 `TestEventIntegrityLongRunCalibration` 标记 `off_contract`（`non_product` 只读校准、不进合同与选择器），登记表不为其建 registry 行。T03.1 是扩展现有 fixture（`scripted_provider_test.go` + `meditation_boot_test.go` + `public_runtime_entry_test.go`），其余未落地者为 `to_be_added`。父测试名从当前 `tasks.md` 逐字取，不冻结硬数；apply 时改名必须同步本表、JSON 与 04.1 合同。
>
> `registry[].id` 是 registry 唯一注册 id；`contract_id` 是 04 合同 `--test-id` 的固定映射（`<leaf>/<ParentTest>`，如 `01.5/TestEventIntegrityFormalAssembly`），不悬空。04.2 命令使用 `contract_id`，与 04 design 的 `01.5/TestName` 形状一致。
>
> 计数唯一键为 `(parent, subtest_name)`：consumer 锚可直接引用 positive 同名 subtest，不重复加入 `subtests[]`；CI 合同分别记录 roles 与引用，137/141 不以 raw role list 长度求和。

| ID | contract_id | 叶 | 父测试 | module（module_path）/ dir | file | selector | tags | tier | state |
|---|---|---|---|---|---|---|---|---|---|
| T00.2 | 00.2/TestEventIntegrityReproduction | 00.2 | TestEventIntegrityReproduction | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityReproduction$` | — | diagnostic-baseline、event-short-final | to_be_added |
| T01.1 | 01.1/TestEventIntegrityNoticeIdentity | 01.1 | TestEventIntegrityNoticeIdentity | .（github.com/SpellingDragon/tagent）/ . | tests/integration/execution/event_integrity_test.go | `^TestEventIntegrityNoticeIdentity$` | — | event-short | to_be_added |
| T01.2 | 01.2/TestEventIntegrityConsumptionRecovery | 01.2 | TestEventIntegrityConsumptionRecovery | .（github.com/SpellingDragon/tagent）/ . | tests/integration/history/event_integrity_test.go | `^TestEventIntegrityConsumptionRecovery$` | — | event-short | to_be_added |
| T01.3 | 01.3/TestEventIntegrityTaskContinuation | 01.3 | TestEventIntegrityTaskContinuation | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityTaskContinuation$` | — | event-short | to_be_added |
| T01.4 | 01.4/TestEventIntegrityDelivery | 01.4 | TestEventIntegrityDelivery | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityDelivery$` | — | event-short | to_be_added |
| T01.5 | 01.5/TestEventIntegrityFormalAssembly | 01.5 | TestEventIntegrityFormalAssembly | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityFormalAssembly$` | — | event-short | to_be_added |
| T01.6 | 01.6/TestEventIntegrityHostAdapters | 01.6 | TestEventIntegrityHostAdapters | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/smoke_harness_test.go | `^TestEventIntegrityHostAdapters$` | smoke | event-short | to_be_added |
| T01.7 | 01.7/TestEventIntegrityCLIAdapter | 01.7 | TestEventIntegrityCLIAdapter | tagent-cli-host（tagent-cli-host）/ examples/cli-host | examples/cli-host/main_test.go | `^TestEventIntegrityCLIAdapter$` | — | event-short | to_be_added |
| T01.8 | 01.8/TestEventIntegrityPersistence | 01.8 | TestEventIntegrityPersistence | .（github.com/SpellingDragon/tagent）/ . | tests/integration/history/event_integrity_test.go | `^TestEventIntegrityPersistence$` | — | event-short | to_be_added |
| T01.9 | 01.9/TestEventIntegrityRestoreUse | 01.9 | TestEventIntegrityRestoreUse | .（github.com/SpellingDragon/tagent）/ . | tests/integration/history/event_integrity_test.go | `^TestEventIntegrityRestoreUse$` | — | event-short | to_be_added |
| T01.10a | 01.10a/TestEventIntegrityExecLifecycle | 01.10a | TestEventIntegrityExecLifecycle | .（github.com/SpellingDragon/tagent）/ . | tool/exec/event_integrity_test.go | `^TestEventIntegrityExecLifecycle$` | — | event-short | to_be_added |
| T01.10b | 01.10b/TestEventIntegrityExecStateEvents | 01.10b | TestEventIntegrityExecStateEvents | .（github.com/SpellingDragon/tagent）/ . | tests/integration/execution/event_integrity_test.go | `^TestEventIntegrityExecStateEvents$` | — | event-short | to_be_added |
| T02.1 | 02.1/TestEventIntegrityMeditationSchedule | 02.1 | TestEventIntegrityMeditationSchedule | .（github.com/SpellingDragon/tagent）/ . | tests/integration/evolution/event_integrity_test.go | `^TestEventIntegrityMeditationSchedule$` | — | event-short | to_be_added |
| T02.2 | 02.2/TestEventIntegrityReflectionDelivery | 02.2 | TestEventIntegrityReflectionDelivery | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityReflectionDelivery$` | — | event-short | to_be_added |
| T02.3 | 02.3/TestEventIntegrityProjection | 02.3 | TestEventIntegrityProjection | .（github.com/SpellingDragon/tagent）/ . | tests/integration/history/event_integrity_test.go | `^TestEventIntegrityProjection$` | — | event-short | to_be_added |
| T02.4 | 02.4/TestEventIntegrityReplacement | 02.4 | TestEventIntegrityReplacement | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityReplacement$` | — | event-short | to_be_added |
| T02.5 | 02.5/TestEventIntegrityApproval | 02.5 | TestEventIntegrityApproval | .（github.com/SpellingDragon/tagent）/ . | tests/integration/capabilities/event_integrity_test.go | `^TestEventIntegrityApproval$` | — | event-short | to_be_added |
| T02.6 | 02.6/TestEventIntegrityApprovalWeChat | 02.6 | TestEventIntegrityApprovalWeChat | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/wechat_test.go | `^TestEventIntegrityApprovalWeChat$` | — | event-short | to_be_added |
| T02.7 | 02.7/TestEventIntegrityCapture | 02.7 | TestEventIntegrityCapture | .（github.com/SpellingDragon/tagent）/ . | tests/integration/learning/event_integrity_test.go | `^TestEventIntegrityCapture$` | — | event-short | to_be_added |
| T03.1 | 03.1/TestEventIntegrityHarness | 03.1 | TestEventIntegrityHarness | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/scripted_provider_test.go | `^TestEventIntegrityHarness$` | — | event-short | extend_existing_fixture |
| T03.2 | 03.2/TestEventIntegrityIdentityMatrix | 03.2 | TestEventIntegrityIdentityMatrix | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityIdentityMatrix$` | — | event-short | to_be_added |
| T03.3 | 03.3/TestEventIntegrityDecisionFlowMatrix | 03.3 | TestEventIntegrityDecisionFlowMatrix | .（github.com/SpellingDragon/tagent）/ . | tests/integration/host/event_integrity_test.go | `^TestEventIntegrityDecisionFlowMatrix$` | — | event-short | to_be_added |
| T03.4 | 03.4/TestEventIntegrityApprovalControl | 03.4 | TestEventIntegrityApprovalControl | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/smoke_test.go | `^TestEventIntegrityApprovalControl$` | smoke | event-short | to_be_added |
| T03.5 | 03.5/TestEventIntegrityReflectionDeliveryControl | 03.5 | TestEventIntegrityReflectionDeliveryControl | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/smoke_test.go | `^TestEventIntegrityReflectionDeliveryControl$` | smoke | event-short | to_be_added |
| T03.6 | 03.6/TestEventIntegrityFoldControl | 03.6 | TestEventIntegrityFoldControl | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/smoke_test.go | `^TestEventIntegrityFoldControl$` | smoke | event-short | to_be_added |
| T03.7 | 03.7/TestEventIntegrityReplacementControl | 03.7 | TestEventIntegrityReplacementControl | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/smoke_test.go | `^TestEventIntegrityReplacementControl$` | smoke | event-short | to_be_added |
| T03.8 | 03.8/TestEventIntegrityCaptureControl | 03.8 | TestEventIntegrityCaptureControl | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/smoke_test.go | `^TestEventIntegrityCaptureControl$` | smoke | event-short | to_be_added |
| T03.9 | 03.9/TestEventIntegrityRestart | 03.9 | TestEventIntegrityRestart | .（github.com/SpellingDragon/tagent）/ . | tests/integration/history/event_integrity_test.go | `^TestEventIntegrityRestart$` | — | event-short | to_be_added |
| T03.11 | 03.11/TestEventIntegrityLongRun | 03.11 | TestEventIntegrityLongRun | wechat-bot（wechat-bot）/ examples/wechat-bot | examples/wechat-bot/smoke_test.go | `^TestEventIntegrityLongRun$` | smoke | event-long | to_be_added |

**00.2 双模式**：

- `diagnostic-baseline`：同一父 selector 跑 1 次，预期**非零**，红证日志单列；只取具名业务断言失败，不用编译失败/超时/零匹配充红证。
- `event-short-final`：修复后同一父 selector 跑 10 次独立 `-count=1 -race`，green 纳入最终 short；不得因其诊断角色永久排除。
- 固定四子测试：`control_normal_path`（正）、`duplicate_notice_material` / `busy_notice_loss`（负）、`request_and_send_identity_correlation`（消费者）。不设 `static_risk_*` 通配替代；静态风险与原案例差异写入证据 `basis`。未来真新增静态风险场景时，先同步 04.1 manifest 与 `test_registry`，再加具名子测试。

## 5. E1—E8：消费者、生产路径、确认、场景、任务

> 生产路径用符号级描述；00.1 产出后必须在 `00.1-contract-inventory.json` 回填 `file:line` 并逐项 `Read` 反查。表中的叶/锚是待实施目标，不是已通过证据。

| 机制 | 生产路径（符号级；00.1 回填 file:line） | 身份 | 确认/保留点 | 消费者叶 | delta 场景 | 测试叶/档位 |
|---|---|---|---|---|---|---|
| E1 用户 HTTP 输入 | 微信 `newWechatApp -> Boot -> bindWechat -> bindChannel -> registerHandlers -> HTTP /task`；CLI `run`；根 `Boot`/`Host.Invoke` | 输入 turn、session、invocation；输入事实持久化 | 输入 durable commit；`task_settled` 驱动原 invocation，不新建；持久化失败不得报成功、索引失败留可恢复欠账 | 01.2、01.3、01.5、01.6、01.7、01.8、01.9；02.* 回验 | 01 persistent 1–6；session-fact-lifecycle 1–10；03 evals 1–4、5–9、11–17 | 01.3/01.5/01.6/01.7、01.8/01.9、03.2—03.9、03.11；event-short |
| E2 工具/子代理通知 | 声明工具/子代理 -> `runtime/task` wrapper/manager -> `runtime/agent` delegate -> `Run.deliver` -> `facts.go` sink/bookNotice/noticeProjection；外部进程 -> `tool/exec` Manager/Monitor -> 既有通知链 | `(TaskID, Round, Kind, Seq)` 通知身份；`EventID` 事实引用；进程五态与退出码 | `Consume` 在 durable commit 后；`Claim` 不算消费；空洞阻断；同通知不重复生成输入；稳定/部分输出不记完成 | 01.1、01.2、01.3、01.8、01.9、01.10a、01.10b；03.1/03.2 | 00 metadata 1–7；01 async 1–2；01 persistent 1–9；telemetry 1–4；execution-resource-lifecycle 1–12 | 01.1–01.3、01.8—01.10b、03.1/03.2；event-short |
| E3 输出渠道 | `hostSendTool -> MessageSender`；用户 turn `deliverFresh`；微信 `bindChannel` | 外部动作身份独立于通知身份；目标/关联保留 | 已确认不重入重发；未知结果保留、不自动重发；显式重试保留关联 | 01.4、01.5、01.6、01.7；02.2；03.4、03.5 | 01 delivery 1–4；00 metadata 4；02 meditation delivery 1–3 | 01.4/01.5/01.6/01.7、02.2、03.4/03.5；event-short |
| E4 治理审批 | 治理 policy -> `runtime/task/wrapper.go` -> ApprovalManager；微信 `runApprovalCLI`/`approvalGateway.Resolve` | digest、有效期、参数摘要、目标 | 批准后只执行一次；过期/重复/拒绝不执行；通道投递失败不阻塞门 | 02.5、02.6；03.4 | 02 approval 1–4；03.4 审批控制 | 02.5 root/02.6 wechat、03.4；event-short |
| E5 冥想回流 | `Boot -> meditation.NewDriver/NewReflection -> Driver.ObserveCommit -> Run.ReflectionInput().InjectReflection`；`deliver_summary` | material、水位、反思消费记录 | 未 completed 终态记推迟；忙时观察不推进水位；自身产物不自激；授权目标全覆盖才报成功 | 02.1、02.2；03.5 | 02 meditation idle 1–4；delivery 1–3；00 metadata 5 | 02.1/02.2、03.5；event-short |
| E6 压缩恢复 | `AgeFoldPlugin.ObserveCommit -> ReplayProjection.FoldByAge -> request.projectedSegment`；`RebuildProjectionFromWAL` | 内容寻址折叠产物；compaction 事件 | 重复折叠不追加第二份；重建不折叠；折叠产物不自激；未消费不丢 | 02.3；03.6；03.9 回验 | 02 projection 1–3；00 metadata 7；01 async 3 | 02.3、03.6/03.9；event-short |
| E7 替换 | `Host.Reload`/`POST /reload -> Run.NoteModelChange -> rl.NewLeaseModel/ModelLease.Replace` | lease 版本/generation | 新请求取新版本；在途流保持开始版本；重复替换不重复安装；失败不安装 | 02.4；03.7 | 02 config 1–4；03.7 替换控制 | 02.4、03.7；event-short |
| E8 采集观测 | `Boot -> openHostCapture/hostRunCapture.Record -> Run.SessionFacts -> rl.NewChainExporter/Export`；`Builder.Capture`；`RecordingLosses` | request/attempt/sample 标识；manifest 关联 | 真实请求逐项有记录；失败如实记录；准入拒绝进损失账目；未发生请求不记录；关联不等于 reward | 02.7；03.8 | 02 decision 1–4；offline 纯引用（见 §7） | 02.7、03.8；event-short |

## 6. Delta Scenario 逐项映射（140）

> 每个 `#### Scenario:` 原文逐行列出；`叶/锚` 指向 JSON 中可定位的 selector 或非产品 checker 锚。一个 Scenario 可对应多个叶/锚；按 requirement 归类由 spec 文件顺序体现。

### 6.1 root orchestration（10）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 只有规划物化授权 | 05.4 `layers.L1`；`authorization`（本轮计划回写、不改既有勾选；范围与写集见 graph `authorization`） |
| 2 | 一级四件套完成而二级未验证 | 05.4 `layers.L2/L3`；05.2 `bundle`（二级证据不得用一级替代） |
| 3 | 消费者域等待根接线 | 01.5 `formal_boot_delivers_turn`；02.1—02.5、02.7 `verify_after=01.5`；02.6 `verify_after=01.6`（02 产物先 ready，01.5/01.6 不等 02 checkbox） |
| 4 | 不同测试包读取同一修改面 | 03.1 `consumer`/file_lock；04.1 `test_event_contract_matches_graph_registry_and_sources` |
| 5 | 实际验收面比计划缩水 | 05.4 `negative`；04.5 `test_every_leaf_is_covered_by_test_or_non_product_evidence` |
| 6 | 少一轮或少一个组合 | 04.2 `test_nine_of_ten_runs_fail`、`test_five_of_six_combos_fail` |
| 7 | 相同日志被重复登记 | 04.5 `test_execution_credential_impersonation_fails`；04.2 `test_duplicate_run_is_rejected` |
| 8 | 长程资源统计 | 03.11 matrix `1200/120`、`7200/720`；交付稳定性 6 组合各 10 次（72000/7200）另计；05.1 `test_long_matrix_is_three_seeds_by_two_windows` |
| 9 | 本地模拟通过但远端未授权 | 05.4 `layers.L2/L3`；05.2 `consumer`（F4/L2 未授权） |
| 10 | 成本超出候选预算 | 03.10 校准记录（45m 仅提议）；05.1 `test_preflight_runs_event_short_long_mutations_by_default` |

### 6.2 committed-event-attribution（3）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 提交不确定不记消费 | 01.2 `consume_only_after_durable_commit` |
| 2 | 两个消费者各自归属 | 01.1 `tool_and_subagent_notice_reaches_original_invocation` |
| 3 | 完成确认引用事实键 | 00.3 五轴（事实引用）冻结检查（doc/checker）；01.2 `claim_then_commit_retry_recovers` |

### 6.3 event-metadata-contract（7）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 同身份异正文被判冲突 | 01.1 `conflicting_body_same_identity` |
| 2 | 不同责任身份的通知不合并 | 01.1 `cross_invocation_not_merged` |
| 3 | 事实引用不代替通知身份 | 01.1 `duplicate_delivery` |
| 4 | 外部发送动作身份独立 | 01.4 `distinct_actions_not_merged` |
| 5 | 卡片携带来源事实键 | 02.3 `actual_request_assembly` |
| 6 | 相似正文不同事实不按文本去重 | 03.2 `positive`；01.1 `conflicting_body_same_identity` |
| 7 | 派生视图不回写源事实 | 02.3 `fold_product_no_self_trigger`；01.3 `no_duplicate_input_for_same_notice` |

### 6.4 async-task-execution（3）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 运输接收不等于消费 | 01.2 `claim_does_not_confirm_early`；补差 `handover_before_confirmation`（交接结果可恢复后才确认，负锚：交接前提前确认=失败） |
| 2 | 发送成功不等于消费 | 01.4 `all_legal_actions_delivered`；01.2 `claim_does_not_confirm_early` |
| 3 | 重启恢复未消费义务 | 01.2 `independent_process_restart`；03.9 `positive` |

### 6.5 delivery-dispatch-receipts（4）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 已确认动作重入不重发 | 01.4 `confirmed_action_not_resent` |
| 2 | 结果未知不自动重发 | 01.4 `unknown_result_not_auto_resent` |
| 3 | 不同动作身份不合并 | 01.4 `distinct_actions_not_merged` |
| 4 | 显式重试保留关联 | 01.4 `explicit_retry_keeps_link` |

### 6.6 persistent-event-loop（12）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 空闲时任务完成唤醒循环 | 01.3 `idle_notice_drives_next_decision` |
| 2 | decision 进行中任务完成则排队 | 01.3 `busy_notice_retained_and_consumed` |
| 3 | 后台结果不新建 invocation | 01.3 `no_new_invocation_for_notice` |
| 4 | 同一通知不重复生成输入 | 01.3 `no_duplicate_input_for_same_notice`；补差 `notice_input_without_projection`（未配置投影/零窗口/有窗口三态下通知材料均可达） |
| 5 | 忙时通知保留待办 | 01.3 `busy_notice_not_dropped` |
| 6 | 多任务多会话各自续转 | 01.3 `two_tasks_same_host_each_continuation`、`multi_session_same_host_isolated` |
| 7 | 空洞阻断后续确认 | 01.2 `no_skip_over_gap` |
| 8 | 领取不提前确认 | 01.2 `claim_does_not_confirm_early` |
| 9 | 恢复从最后确认位置继续 | 01.2 `independent_process_restart`；03.9 `positive` |
| 10 | 根宿主缺 sender 具名拒绝 | 01.5 `missing_sender_fails_closed` |
| 11 | 微信宿主缺声明身份具名拒绝 | 01.6 `missing_entry_fails_closed` |
| 12 | CLI 宿主缺模型凭据具名拒绝 | 01.7 `missing_model_key_fails_closed` |

### 6.7 telemetry-channel（4）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 已外显通知即时退出 | 01.3 `busy_notice_retained_and_consumed` |
| 2 | 内部性通知的 N 轮提醒 | 01.3 `late_notice_not_dropped` |
| 3 | 未消费通知不可丢 | 01.2 `no_skip_over_gap`；01.3 `busy_notice_not_dropped` |
| 4 | 运输接收不触发降级 | 01.2 `independent_process_restart` |

### 6.8 approval-channels（4）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 状态机流转与幂等 | 02.5 `approved_then_executes_once`、`duplicate_response_no_second_execution` |
| 2 | 过期回应不得执行（失败） | 02.5 `expired_response_refused` |
| 3 | 改参不得沿用旧批准（失败） | 02.5 `changed_args_requires_new_approval` |
| 4 | 批准后只执行一次（并发） | 02.5 `approved_then_executes_once` |

### 6.9 config-hot-reload（4）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 同版本重复替换不重复发布/安装（失败） | 02.4 `duplicate_same_version_not_reinstalled` |
| 2 | 替换失败不安装（失败） | 02.4 `failed_replacement_not_installed` |
| 3 | 在途流保持开始版本 | 02.4 `inflight_stream_keeps_start_version` |
| 4 | 替换后新请求取新版本 | 02.4 `new_request_gets_new_version` |

### 6.10 decision-capture（4）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 真实请求逐项有记录 | 02.7 `requests_and_attempts_match_records` |
| 2 | 失败尝试如实记录（失败） | 02.7 `failed_attempt_recorded_as_failure` |
| 3 | 准入被拒进入损失账目（失败） | 02.7 `rejected_admission_in_loss_ledger` |
| 4 | 未发生请求不产生记录（失败） | 02.7 `no_request_no_record` |

### 6.11 event-sourced-projection（3）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 重复折叠不追加第二份产物 | 02.3 `small_window_fold_and_restore`、`rebuild_does_not_refold` |
| 2 | 重建不触发折叠（失败） | 02.3 `rebuild_does_not_refold` |
| 3 | 折叠产物不触发自身（失败） | 02.3 `fold_product_no_self_trigger` |

### 6.12 meditation-delivery-tool（3）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 授权目标全覆盖才报成功 | 02.2 `explicit_call_reaches_target_session`、`same_batch_yield_is_legal_success` |
| 2 | 目标零存活会话不得报成功（失败） | 02.2 `zero_live_target_not_success` |
| 3 | 部分会话提交失败不得报成功（失败） | 02.2 `partial_commit_failure_not_success` |

### 6.13 meditation-idle-gating（4）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 让位不烧窗口 | 02.1 `new_material_on_cadence` |
| 2 | 冷启动直通 | 02.1 `cold_start_passes_through` |
| 3 | 未完成终态记为推迟（失败） | 02.1 `non_completed_terminal_defers` |
| 4 | 忙时观察不推进水位（并发） | 02.1 `busy_observation_holds_watermark` |

### 6.14 evals-harness（17）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 输入身份与会话矩阵（03.2） | 03.2 `positive`；01.1 `conflicting_body_same_identity` |
| 2 | 多决策流矩阵（03.3） | 03.3 `positive`；忙/闲续转与工具/子代理交错 |
| 3 | 未预期请求或缺结果失败（03.1） | 03.1 `negative` |
| 4 | 正式入口消费（03.2、03.3） | 03.2 `consumer`、03.3 `consumer` |
| 5 | 审批控制（03.4） | 03.4 `positive`/`negative`/`consumer` |
| 6 | 反思回流控制（03.5） | 03.5 `positive`/`negative`/`consumer` |
| 7 | 压缩投影控制（03.6） | 03.6 `positive`/`negative`/`consumer` |
| 8 | 替换控制（03.7） | 03.7 `positive`/`negative`/`consumer` |
| 9 | 采集控制（03.8） | 03.8 `positive`/`negative`/`consumer` |
| 10 | 提交领取确认边界重启不重不丢（03.9） | 03.9 `positive`/`negative`/`consumer` |
| 11 | 四会话同一 Host 轮转重启（03.11） | 03.11 `positive`；matrix `sessions_per_host=4`、`restarts_per_combo=120` |
| 12 | 六组合总量与窗口实际触发（03.11） | 03.11 `positive`；matrix 6/7200/720 |
| 13 | 交付稳定性单独登记（03.11） | 03.11 6 组合各 10 次（72000/7200，实施期研究，非 MR ×10，预算另计） |
| 14 | 注入失败不静默（03.11） | 03.11 `negative` |
| 15 | 先校准再冻结（03.10） | 03.10 校准报告（六项实测、两窗口成样本；off_contract，不进 short/long） |
| 16 | 超预算不缩覆盖（03.11） | 03.11 `matrix`；05.1 `test_long_matrix_is_three_seeds_by_two_windows` |
| 17 | 失败不得被吞（03.11） | 03.1 `negative`；04.2 `test_failed_process_exit_is_rejected` |

### 6.15 architecture-guardrails（17）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 八类机制由必跑子场景覆盖 | 04.1 `test_event_contract_matches_graph_registry_and_sources` |
| 2 | 显式清单不因固定十场景漏项 | 04.1 `test_event_contract_matches_graph_registry_and_sources`、`test_event_contract_dual_role_consumer_is_accepted` |
| 3 | 审查删除登记项被拒 | 04.1 `test_event_contract_deleting_graph_entry_is_rejected` |
| 4 | 旧 15 场景不减少 | 04.1 冻结 S01–S15；04.2 `test_v1_command_shape_is_unchanged` |
| 5 | 独立重复与单轮唯一 | 04.2 `test_repeat_runs_are_independent_and_all_required`、`test_duplicate_run_is_rejected` |
| 6 | 合法直接调用通过 | 04.3 `positive` |
| 7 | 可解析的禁止直接调用被拒 | 04.3 `negative` |
| 8 | 无法解析的调用如实暴露 | 04.3 `uncovered` |
| 9 | 控制树先绿 | 04.4 `test_red_control_tree_fails_gate` |
| 10 | 具名断言杀死变异 | 04.4 `test_control_tree_green_then_every_confirmed_mutation_killed_by_named_assertion` |
| 11 | 编译失败或超时不算杀死 | 04.4 `test_compile_failure_is_not_a_kill`、`test_timeout_is_not_a_kill` |
| 12 | 未证实形态不得凑数 | 04.4 `test_unconfirmed_candidate_is_not_claimed_killed` |
| 13 | 改名、缺叶或缺必跑子场景失败 | 04.5 `test_renamed_leaf_fails`、`test_missing_leaf_fails`、`test_missing_mandatory_subtest_fails` |
| 14 | 证据复用失败 | 04.5 `test_execution_credential_impersonation_fails` |
| 15 | bundle 汇总同源 | 04.5 `test_bundle_accepts_ten_short_six_long_one_mutation` |
| 16 | lint 检查活动对象 | 04.5 `test_active_graph_is_checked_not_archive` |
| 17 | 旧规则不被放宽 | 04.5 `test_legacy_ten_domain_rules_are_not_relaxed` |

### 6.16 resident-release-evidence（19）

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 普通 PR 也跑双档 | 05.1 `test_event_producer_jobs_present_with_contract_commands` |
| 2 | short 十次独立且单轮唯一 | 05.1 `test_event_producer_jobs_present_with_contract_commands`；05.2 `test_missing_artifact_or_combo_or_short_run_fails` |
| 3 | long 六组合不合并 | 05.1 `test_long_matrix_is_three_seeds_by_two_windows` |
| 4 | 跳过入口被自测拦截 | 05.1 `test_event_jobs_have_no_skip_hatch` |
| 5 | preflight 默认完整 | 05.1 `test_preflight_runs_event_short_long_mutations_by_default` |
| 6 | 上游失败或取消 | 05.2 `test_cancelled_or_skipped_upstream_fails` |
| 7 | required 不替代旧检查 | 05.2 `test_required_is_event_summary_not_replacement_for_old_jobs` |
| 8 | 缺片缺次或空目录 | 05.2 `test_missing_artifact_or_combo_or_short_run_fails` |
| 9 | 混基线摘要 | 05.2 `test_mixed_baseline_digest_fails` |
| 10 | 失败也上传证据 | 05.2 `test_all_artifacts_uploaded_on_failure` |
| 11 | 旧检查器不得宣称新图 | 05.3 四篇文档逐条核对 + `doc-refs`/`lint` |
| 12 | 文档命令可复跑 | 05.3 `doc-refs` 无缺失、`lint` 绿；05.4 `consumer` |
| 13 | 历史只勘误 | 05.3 四篇文档逐条核对（不新增篇目、不改归档历史） |
| 14 | 05.3 不抢跑 | 05.3 `verify_after=05.2`（草稿可先写，勾选等 05.2） |
| 15 | 文档预算只降不升 | 05.3 `test_baseline_matches_live_tree`、`test_extra_lines_fail` |
| 16 | 本地绿不冒充远端 | 05.4 `layers.L1` vs `layers.L2` |
| 17 | 未授权不执行远端动作 | 05.4 `read_only`（不提交/推送/建 MR/改保护/打 tag） |
| 18 | 72h 与真实服务单列 | 05.4 `layers.L3` |
| 19 | 独立验收不可用 | 05.4 `negative`（删证据/改名/复用 run-id 后 bundle 必红） |

### 6.17 session-fact-lifecycle（10）

> 新增于计划回写③，对应 01.8/01.9；父测试在源码落地前按 `test_registry.state=to_be_added` 与合同冻结选择器门控，不伪造 green。

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 新事实当次可召回 | 01.8 `new_fact_recalled_same_turn`、`formal_recall_tool_returns_new_fact` |
| 2 | 召回原文与提交字节一致 | 01.8 `original_text_matches_committed_bytes` |
| 3 | 存储失败不返回伪成功 | 01.8 `storage_failure_not_reported_as_success` |
| 4 | 索引失败留可恢复欠账 | 01.8 `index_failure_leaves_recoverable_debt` |
| 5 | 新事实不使既有记录不可达 | 01.8 `new_fact_keeps_old_reachable`（只验可达性底线，排序语义归另立变更） |
| 6 | 正式召回工具返回新事实 | 01.8 `formal_recall_tool_returns_new_fact`（consumer） |
| 7 | 正常关闭后恢复原文、归属与工具配对 | 01.9 `restart_restores_original_text`、`restart_restores_invocation_attribution`、`restart_restores_tool_call_result_pairing` |
| 8 | 进程中断后恢复原文、归属与工具配对 | 01.9 同上三锚（进程中断路径） |
| 9 | 下一请求使用恢复材料 | 01.9 `next_request_uses_restored_material`（consumer） |
| 10 | 仅恢复账本不算完成 | 01.9 `restore_only_ledger_is_not_completion`（负锚） |

### 6.18 execution-resource-lifecycle（12）

> 新增于计划回写③，计划回写④按用户裁定 C 拆两叶分批：前三条 Requirement/前 6 个 Scenario 归 01.10a（`tool/exec` 包内查询面），后三条 Requirement/后 6 个 Scenario 归 01.10b（execution 测试 + 根装配）；两叶共引 `evidence/01.10-summary.json`，复用既有通知消费链，不另建第二状态仓。

| # | Scenario（原文） | 叶 / 锚 |
|---|---|---|
| 1 | 调用返回不证明进程结束 | 01.10a `returned_call_does_not_prove_exit` |
| 2 | 同键复用查询可见后续输出 | 01.10a `same_key_capture_sees_more_output` |
| 3 | 真正退出记录退出码 | 01.10a `release_reports_exit_code` |
| 4 | 释放确认或状态具名未知 | 01.10a `released_or_named_unknown` |
| 5 | 稳定不等于成功 | 01.10a `stable_is_not_success`（负锚） |
| 6 | 部分输出不等于完成 | 01.10a `partial_output_is_not_completion`（负锚） |
| 7 | 续租不随调用返回终止 | 01.10b `lease_keeps_renewing_after_return` |
| 8 | 状态变化产生后续通知 | 01.10b `state_change_emits_followup_notice` |
| 9 | 下一轮消费执行状态 | 01.10b `next_turn_consumes_exec_state`（consumer） |
| 10 | Host.Close 释放执行资源 | 01.10b `host_close_releases_exec_resources` |
| 11 | 重启接管或具名未确认 | 01.10b `restart_takes_over_or_names_unconfirmed` |
| 12 | 未确认重启不报已停止 | 01.10b `unconfirmed_restart_not_reported_stopped`（负锚） |

## 7. 纯引用：cross-session-delivery 与 offline-training-export

> 这两份是已存在 spec 的**纯引用**：只引用要求与场景作为 02.2/02.7 的语义来源，不新增产品义务、不新增 CLI/导出入口。映射对子测试改名敏感；改名必须同时更新 `task-graph.json`、`coverage.md` 与 04.1 合同。

### 7.1 cross-session-delivery（12，全部映射到 02.2）

| # | Scenario（原文） | 02.2 锚 | 说明 |
|---|---|---|---|
| 1 | 白名单外目标被拒 | `wrong_target_no_extra_write` | 不额外写 |
| 2 | 未配置白名单 | `empty_whitelist_fail_closed` | fail-closed |
| 3 | 目标未启动 | `target_not_running_named_refusal` | 具名拒绝 |
| 4 | 不走 one-shot 回退 | `target_not_running_named_refusal` | 断言无回退 |
| 5 | 目标正常消费投递 | `explicit_call_reaches_target_session` | 目标下一次真实请求可见 |
| 6 | 投递不喂肥目标新鲜度 | `delivery_does_not_rearm_novelty` | 不重置 novelty |
| 7 | 与用户输入同批让位 | `same_batch_yield_is_legal_success` | 合法成功语义 |
| 8 | 目标名不存在 | `unknown_target_named_refusal` | 具名拒绝 |
| 9 | 白名单外目标具名拒绝 | `wrong_target_no_extra_write` | 与 #1 同一行为的不同表述 |
| 10 | 未配置白名单 fail-closed（失败） | `empty_whitelist_fail_closed` | 与 #2 同一行为 |
| 11 | 目标 loop 未运行具名拒绝（失败） | `target_not_running_named_refusal` | 与 #3 同一行为 |
| 12 | 成功语义为"进入 mailbox"（并发） | `same_batch_yield_is_legal_success` | 进入 mailbox 即合法成功 |

**结论**：12 个纯引用场景收敛到 02.2 的 6 个具名子测试；不新增第 7 个测试，不改变 02.2 的 2/8/1 计数。

### 7.2 offline-training-export（19，2 映射 + 17 真资源待验）

| # | Scenario（原文） | 状态 / 锚 |
|---|---|---|
| 1 | 跨分区父键与缺父 | 真资源待验（训练导出数据集） |
| 2 | 多反馈与未绑定任务 | 02.7 `export_correlation_is_not_reward`（只证“关联≠reward”，不证奖励正确） |
| 3 | 工具调用无正文 | 真资源待验 |
| 4 | 模板或媒体不支持 | 真资源待验（模板/媒体资源） |
| 5 | 子调用与旧记录 | 真资源待验 |
| 6 | 最小可用数据集 | 真资源待验 |
| 7 | 常量替身被判失败（失败） | 真资源待验（负向守卫，不能以模拟替身充真实） |
| 8 | 真实资源未授权（失败） | 真资源待验（未授权即不得勾选） |
| 9 | 延迟反馈到达（正常） | 真资源待验 |
| 10 | 无证据不臆造奖励（失败） | 真资源待验 |
| 11 | 并发导出同分区（并发） | 真资源待验 |
| 12 | 重启后 reward 版本稳定（重启） | 真资源待验 |
| 13 | manifest 可追溯（正常） | 02.7 `formal_capture_then_offline_export`（capture manifest 可追溯部分） |
| 14 | manifest 缺失或不一致（失败） | 真资源待验 |
| 15 | 因果分组防泄漏（并发） | 真资源待验 |
| 16 | 重启后 manifest 稳定（重启） | 真资源待验 |
| 17 | DPO 缺候选对标 unknown | 真资源待验（算法资格） |
| 18 | 采样 RL 资格取决于真实算法 | 真资源待验（算法资格） |
| 19 | 用途矩阵四要素齐备（正常） | 真资源待验 |

**结论**：19 个 offline 场景中只有 2 个可由 02.7 的捕获/关联行为覆盖；其余 17 个依赖真实训练资源、模板/媒体、tokenizer/collator、算法资格与授权，**不得**用模拟替身或 02.7 的 green 冒充，保持待验。

## 8. 非产品叶证据（13 叶，不编造 product）

| 叶 | 证据类型 | 证据/检查器 | 消费者锚 | 不做什么 |
|---|---|---|---|---|
| 00.1 | doc/checker | `00.1-contract-inventory.json` 八项字段非空；`production_entry` 逐项 `file:line` 反查 | 00.3 冻结件逐项引用 | 不新增产品测试 |
| 00.3 | doc/checker | `00.3-frozen-contract.json` 五轴/E1—E8/confirmed→repair+negative/unresolved→blocked | 01.1/01.3/04.1 读取 | 不把未证实项扩大为全域 blocked |
| 00.4 | baseline | 三模块命令 + deterministic golden；原始退出码/日志/隔离树 | 05.1/05.4/F2 复用 | 不用既有绿证明新行为 |
| 03.10 | calibration_report | 两窗口单组合校准报告（冷构建/热运行/120 重启/race/存储增长/峰值资源）；`off_contract` 不进 event 合同 | 05.1 `timeout-minutes` 与预算冻结读取同一 JSON 字段 | 不登记 short/long 选择器、不参与父测试/锚计数 |
| 04.1 | checker | 合同 `event_integrity_scenarios.json` + `check_runtime_evidence.py` 自测 | 04.2/05.1/05.2 同一合同 | 不把 137/141 当上限、不删子场景 |
| 04.2 | checker+runner | runner/checklib/evidence 扩展；真实 short `--test-id` 十轮 | 04.5/05.2 同一 checker | 不用 checker 合成 fixture 顶生产验收 |
| 04.3 | checker | `architecture.json` + `codetools event-ownership` + lint | `scripts/lint.sh` 正式调用 | 不声称完整调用图/任意第二状态仓语义 |
| 04.4 | checker | `check_event_integrity_mutations.py` + 自测；仅 00.3 已证实形态；计划回写③登记三个后续反例（存储失败报成功/交接前提前确认/进程仍活报完成），实际变异实施在后续单 | 04.5 lint、05.1 mutations job | 不把未证实形态凑数；已登记未启用反例不计数为 killed |
| 04.5 | checker | plan/evidence/lint/bundle；活动 graph 对象 | 05.1/05.2 合同与 `--bundle` | 不全局放宽旧十域规则 |
| 05.1 | workflow checker | `ci.yml` 三生产 job + preflight + workflow 自测 | GitHub Actions / preflight 同入口 | 不把远端绿当本地完成门 |
| 05.2 | workflow checker | required 汇总 + `--bundle` + 自测 | 分支保护（F4/L2 待授权） | 不替代/删除旧 job |
| 05.3 | docs/checker | 四篇文档 + doc_budget + doc-refs + lint | README/OPERATION 接手者入口 | 不新增文档、不上调预算、不把未启用写成已支持 |
| 05.4 | integration acceptance | L1 本地完整命令 + bundle + 独立偏差验收 | 与 CI 完全相同的命令/job 名 | 只读；L2/L3 未授权不勾选 |

## 9. 真资源待验（未授权，不勾选）

以下不是本轮或本地模拟能证明的，保持待验；不得用 02.x/03.x 的 green 冒充：

- 真实微信账号/通道/公网回调与真实 `/task` 请求。
- 真实模型 provider/API key/配额与真实模型行为。
- 真实审批通道端到端（微信审批回应、根 CLI `tagent approve <digest>` 仍为独立待验）。
- 远端 CI required/branch protection 生效（L2/F4）。
- 72h 稳定性与真实服务混跑（L3）。
- 真实训练导出：数据集父键/反馈/模板/媒体/tokenizer/collator/资格矩阵/reward 版本。
- 掉电/fsync 故障注入与真实磁盘故障；03.9 只证进程重启，不宣称掉电恢复。
- 真实远端 push/MR/保护规则/tag。

## 10. 风险、冲突与待裁（只列待验证业务风险）

- **R1 00.2 双模式**：04 design 曾说诊断测试不纳入绿矩阵；用户本轮要求同一测试保留“诊断基线红 + 最终修复绿”双模式。JSON 已编码 `diagnostic-baseline + event-short-final`；04.1 若 schema 无法表达双模式，停报冲突，不得静默排除。
- **R2 旧 checker 不能证明新图（04.5 已解除）**：`scripts/check_runtime_plan.py` 曾硬编码旧十域；04.5 已扩展为按 graph `domain_profile` 读取六域/叶数/phase（旧十域常量保留），本轮 `--change-dir ... --json` 实测 exit 0（42 叶/84 阶段节点/154 边）。残留边界：不得以旧十域常量或旧计数字面量宣称新图通过；自测 fixture 为独立的 38 叶合成图，不代表真实变更规模。
- **R3 已实现与拟新增**：当前仓库已发现 27 个合同内父测试（含 00.2/01.1/01.2/01.3/01.8/01.9/02.1/03.1），01.10a/01.10b 两个父测试尚未落地；旧 03.3 monolith（`superseded`，代码保留但不在合同）。29 父/当前 137/修复后 141 中其余为**拟新增清单**，apply 时需重新核对文件与父名；数字不是删项上限。未落地的两个父测试不得以 pending 门控冒充 green。
- **R4 根 CLI approval 待验**：`runApprovalCLI` 只证明微信宿主命令行回应入口；根 `tagent approve <digest>` 是独立待验项，02.5/02.6 不得声称覆盖。
- **R5 soak 命令**：`TestSoakContinuity` 在 `tests/integration/host/soak_continuity_test.go:106`，`-rounds`/`-events-per-round` flags 在 73–74 行；05.1 仍须实跑核验，且 soak 不算每 MR long。
- **R6 文档预算**：baseline 206 files/21934 lines（2026-10-22）。05.3 不得上调，净增行用同篇删减置换；apply 时重测。
- **R7 硬门**：G2（02.2 replay 身份）、G4（02.3 compaction 写入点）、G6（02.7 导出关联）未验则对应叶不勾选；00.3 `invocation_version_conflict=blocked` 时 01.1/01.3 blocked，不得新建 invocation 绕过。
- **R8 纯引用映射脆弱**：cross-session 12 → 02.2 的 6 个具名子测试、offline 2 → 02.7 的两个锚；子测试改名会同时打断 graph/coverage/合同，必须三处同步。
- **R9 变异基准**：04.4 只在 00.3 已证实缺陷上启用；若 `defects[].confirmed` 为空，则没有可启用 mutation，不能声称 killed；新增证实缺陷不得漏登记。计划回写③在 04.4 任务行登记三个后续反例（存储失败报成功→01.8 负锚、交接前提前确认→01.2 补差负锚、进程仍活报完成→01.10a 负锚），但**不启用**，实际变异实施在后续单；“旧记忆被新无关记录挤掉”归另立变更，不在 04.4 登记。
- **R10 长程成本**：45m 仅为提议，03.10 先校准、05.1 随后冻结；不得缩 6 组合/4 会话共享 Host/300 输入/30 重启/7200/720；冥想保持、fsync 不削；交付稳定性 6 组合各 10 次为实施期研究（72000/7200），预算另计，非 MR ×10。
- **R11 preflight 跳过口**：`--skip-smoke` 只跳微信 smoke，事件 long 仍默认运行并打印“不具备完整 MR 等价证据”；不得新增以成本为由默认关闭 long 的 opt-in。
- **R12 L2/L3 未授权**：远端 required、branch protection、72h、真实服务均未授权；05.4 只登记 L1，L2/L3 保持待验。
- **R13 05.4 只读**：`write_files=[]`，证据只写 `${TMPDIR:-/tmp}/event-consumption-integrity/05.4` 或 CI `runner.temp`；不改仓库。
- **R14 00.2 red 记账**：baseline 1 次预期非零，必须保留原始退出码与具名业务断言失败；不得用编译失败/超时/零匹配充红证；最终 green 十轮独立，红绿分开记账。

## 11. 本轮明确未执行

- 未执行 `apply`；本轮为**计划回写④**（用户裁定 C：01.10 拆为 01.10a/01.10b 两叶分批）。勾选保持 32 个不变：01.10a/01.10b 均未勾选，未勾选由 9 个增为 10 个（01.9、01.10a、01.10b、02.3、03.11、04.2、05.1—05.4），六域 `tasks.md` 合计 10 个 `- [ ]`、32 个 `- [x]`。
- 未改产品代码、产品行为与产品测试；合同与计数同步范围：`task-graph.json`、`golden/runtime/event_integrity_scenarios.json`、`scripts/check_runtime_evidence.py`、`scripts/check_runtime_plan.py`、`scripts/runtime_checklib.py`、`scripts/test_check_runtime_evidence.py`、`scripts/check_event_integrity_mutations.py`。
- 未运行任何产品 `go test`/`-race`/长程/CI/远端命令。已跑：`openspec validate event-consumption-integrity --strict`（exit 0）、`openspec change validate .../01-runtime-consumption --strict`（exit 0）、plan 检查器（exit 0，42 叶/32 勾/10 未勾/84 节点/154 边/acyclic）、evidence 合同检查（exit 0，42 叶覆盖/29 父/137 绿/141 全锚/0 selector 不匹配）。
- 自测：evidence 93 OK、plan 43 OK、runner 30 OK、architecture 41 OK、mutations 12 OK，全绿；本轮修了 evidence 自测 4 处计数/门控预期（27→28、133→138、pending 3→1，后者因 01.2/01.3 补差锚已随既有实施波次落源码；selector 检查改为“已检查+待发现=29 父”不变量，不再硬编码单时点发现数）。
- 本轮同步了 01 域四件套（proposal/design/tasks/specs/execution-resource-lifecycle）、`00.3` 冻结件（`public_api_or_format_change.plan_revision_04_exec_lifecycle`）、04 域四件套计数与 04.4 锚点、根 `tasks.md`/`proposal.md`/`design.md`/`findings.md`/`ORCHESTRATION.md`/`coverage.md`；03/05 域仅同步引用文字。
- 未创建任何证据日志/临时产物到仓库；未提交、未推送、未建 MR、未改保护规则、未打 tag。

**写后自检**：`task-graph.json` 以 Python 复核 84 节点/154 边/`acyclic=true`、137/141 计数、29 个唯一 registry id 与 contract_id、140 行映射；`coverage.md` 以 `Read`/`Grep` 复核关键计数与映射。
