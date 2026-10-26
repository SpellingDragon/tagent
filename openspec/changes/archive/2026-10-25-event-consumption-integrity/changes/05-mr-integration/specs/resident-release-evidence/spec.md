## ADDED Requirements

### Requirement: 每 MR 事件双档与故障变异必跑

事件完整性回归 SHALL 在每次 `pull_request` 与 `main`/`dev` 的 `push` 上运行短程、长程与故障变异三类作业，MUST NOT 以路径过滤、仅手动触发、`continue-on-error`、凭据缺失或标签条件跳过。短程 SHALL 由共用 runner 发起 **10 次独立** `go test … -count=1 -race -json`，每次独立日志、run-id 与证据，MUST NOT 用单次 count 放大或同一日志冒充十次。长程 SHALL 按 `seed [1,7,42] × window [256,32768]` 分成六片，各片一次 `-count=1 -race -json`，`fail-fast: false` 保留各片证据；**单片 MUST 为 1200 个不同业务输入/120 次真实进程重启，六片合计 7200 输入/720 重启**，MUST NOT 把单个分片写成 7200/720，MUST NOT 缩减、MUST NOT 用 count 或重试放大/重复计数。`race` 作业的包集合 SHALL 由全部修改的产品文件与全部正式测试包映射（至少含 `tests/integration/{execution,history,host,capabilities,evolution,learning}` 与所有被改根包，含 `runtime/{agent,task,state}`、`memory`、`evolution`、`rl`、`tool`、`api/event`、`api/plugin` 与根包），MUST NOT 只补 host/history；`race_check.sh` 的上游 waiver 只对非 tagent frame 生效，MUST NOT 豁免新事件套件。故障变异 SHALL 作为独立作业运行，先证控制树在正式 selector 下全绿，再由具名业务断言杀死。`workflow_dispatch` 的手动 soak 作业 MUST NOT 被当作每 MR 长程或其替代。`scripts/preflight.sh` SHALL 默认调用同一 runner/checker 的完整事件入口（short/long/mutations），MUST NOT 新增以成本为由默认关闭 long 的 opt-in 开关，MUST NOT 因 `--skip-smoke` 取消事件 long；`--skip-smoke` 只跳过 smoke，并 MUST 打印“smoke 未运行，本次 preflight 不具备完整 MR 等价证据”，该标注 MUST NOT 改变退出码，也 MUST NOT 影响 MR 三档必跑。所有原始运行产物（原始日志、`evidence.json`、原始退出码文件）SHALL 写入 CI `runner.temp` 或 `--out` 指定的仓库外临时目录；证据摘要与 `log_ref` MAY 统一记入根 `ORCHESTRATION.md` 台账，SHALL NOT 要求每叶新建 summary 文件。MUST NOT 把原始日志写入仓库内未核验目录或 `openspec/changes/**` 规划目录，MUST NOT 宣称未核验目录已被忽略。

#### Scenario: 普通 PR 也跑双档

- **WHEN** 一个未加特殊标签、未配置模型/渠道凭据的普通 PR 触发 CI
- **THEN** `event-integrity-short`、`event-integrity-long`、`event-integrity-mutations` 均被调度；任一作业缺失、被路径过滤或仅手动可触发即失败

#### Scenario: short 十次独立且单轮唯一

- **WHEN** 执行 `event-short` 合同
- **THEN** 产生 10 份 run-id 与日志路径互不相同的证据并全部通过；同一日志出现重复 run/pass、9 次冒充 10 次、或以单次 count 放大代替十次独立执行均判失败

#### Scenario: long 六组合不合并

- **WHEN** 执行 `event-long` 合同
- **THEN** 六片 `(seed, window)` 集合恰为 `{1,7,42} × {256,32768}`，每片一份独立证据、单片 1200 输入/120 重启，六片合计 7200/720；把单个分片写成 7200/720、五片冒充六片、把六组合塞进一次 `go test`、或提高 count 重复计数均判失败

#### Scenario: 跳过入口被自测拦截

- **WHEN** 给事件作业添加 `paths` 过滤、job 级 `if`、`continue-on-error` 或凭据跳过条件
- **THEN** workflow 自测的非零负控使门变红，不因 YAML 仍可解析而放行

#### Scenario: preflight 默认完整

- **WHEN** 运行 `scripts/preflight.sh`（不带参数）或只带 `--skip-smoke`
- **THEN** short/long/mutations 三档均被调用；`--skip-smoke` 只跳过 smoke 并打印“不具备完整 MR 等价证据”，事件 long 不得被默认取消，也不得新增以成本为由的 opt-in 开关

### Requirement: required 汇总不因取消跳过缺失而放行

事件门 SHALL 提供稳定检查名 `event-integrity-required` 作为**新增事件三档的汇总检查**供分支保护引用；该检查 MUST NOT 替代或删除 `test`/`race`/`validators`/`openspec`/`soak` 等既有检查，旧检查名 MUST 继续保留。该作业 MUST 使用 `if: always()` 并 `needs` 短程、长程、变异三档；MUST 将上游结果逐一核对为 `success`，`cancelled`、`failure`、`skipped` 均 MUST 判失败。汇总 SHALL 通过同一 `check_runtime_evidence.py` 的 bundle 模式消费 runner/checker 已产出的证据事实，MUST NOT 另写第二套合同解析器或矩阵/指纹规则。汇总 MUST 核对：short 恰好 10 份独立证据、long 六组合齐全、mutations 控制树绿且每个登记变异由具名断言杀死、全部证据基线源树摘要一致、同一执行凭据（run-id/日志真实路径/artifact）不得冒充不同独立轮次或不同 matrix 组合（同轮多 case 可共享日志，但每 case 须按具名 selector 独立核验）。缺 artifact、空目录、截断、skip、重复 run、后补 pass 覆盖先 fail 均 MUST 失败。失败时 SHALL 仍上传 required 报告与已下载证据。生产作业 MUST 从源头消除跳过条件，MUST NOT 依赖“被跳过的 required check 可能按成功呈现”的默认行为。汇总范围 MUST 只覆盖新增 short/long/mutations 三档，MUST NOT 把既有产品测试或旧 15 场景排除在既有检查之外。05.1/05.2 的本地完成门 SHALL 是与 CI 完全相同的命令真实跑通加 `CiWorkflowTest` 结构验收；远端 required 真实生效属 L2/F4，未获授权时 MUST NOT 作为本地叶完成门，也 MUST NOT 以远端 CI 绿代替本地证据。

#### Scenario: 上游失败或取消

- **WHEN** 任一生产作业 `failure` 或 `cancelled`
- **THEN** `event-integrity-required` 仍因 `always()` 运行，并在结果核对步非零退出；MUST NOT 因自身 skipped 而被显示为成功

#### Scenario: required 不替代旧检查

- **WHEN** 分支保护或文档引用 `event-integrity-required`
- **THEN** 该检查只汇总新增 short/long/mutations 三档；`test`/`race`/`validators`/`openspec`/`soak` 仍在，删除旧检查或把 required 写成全库唯一入口即判失败

#### Scenario: 缺片缺次或空目录

- **WHEN** 六组合缺一片、short 少一次、mutations 证据缺失，或 artifact 目录为空/截断
- **THEN** bundle 汇总具名缺项并非零退出，不以“没有判定对象”返回成功

#### Scenario: 混基线摘要

- **WHEN** 十次 short 或六组合的证据来自不同的受测源树摘要
- **THEN** 汇总判失败并报告不一致的摘要，不把不同源树的通过拼成一次通过

#### Scenario: 失败也上传证据

- **WHEN** 任一事件作业或 required 自身失败
- **THEN** 其 artifact 仍以 `if: always()` 上传，供定位缺片与失败原因；上传失败本身使作业失败

### Requirement: 文档只陈述已落地能力与可证明边界

`README.md`、`docs/OPERATION.md`、`docs/FACT-VOCABULARY.md`、`docs/GLOSSARY.md` SHALL 登记事件双档、故障变异、required 汇总、证据档位与失败处置，且只陈述已完成并有真实命令/退出码的能力。未落地的作业、合同或检查器 MUST 标注为“拟新增/未启用”，MUST NOT 让旧 `check_runtime_evidence.py`、旧 `scenarios.json` 或旧十域 graph 被描述为已支持新合同/新图。文档中的每条命令 MUST 可在当前树复跑且与 CI 稳定检查名一致。历史文档 SHALL 只就地勘误并标注日期与原因，MUST NOT 改写历史结论或追溯修改归档 change。05.3 的最终验收 SHALL 以 05.2 的真实输出与 workflow 自测通过为前置；在此之前只允许写“拟新增/未启用”草稿，MUST NOT 勾选完成，也 MUST NOT 把 CI 段落写成“可用”。文档更新 MUST NOT 新增 `.md` 篇目，MUST NOT 上调 `scripts/doc_budget.json`；净增行 SHALL 用同篇删减等量置换，确无可删减内容时 MUST 停止上报而非放宽基线。

#### Scenario: 旧检查器不得宣称新图

- **WHEN** 文档声称 `check_runtime_evidence.py` 或旧 `scenarios.json` 已支持 `event_integrity_scenarios.json`/六域 graph，而 04.5 尚未落地
- **THEN** 该表述判失败，必须改为“拟新增/未启用”或删除；独立验收以 grep 与命令实跑复核

#### Scenario: 文档命令可复跑

- **WHEN** 接手者按 OPERATION 的事件门命令执行
- **THEN** job 名、合同路径、artifact 名与 CI 一致，原始退出码可复现；仅名称相似或指向已删文件不算通过

#### Scenario: 历史只勘误

- **WHEN** 旧文档结论与新实现不一致
- **THEN** 只在原处追加带日期的勘误并说明原因；不改写历史结论、不追溯修改归档 change

#### Scenario: 05.3 不抢跑

- **WHEN** 05.2 尚未产出真实证据或 `CiWorkflowTest` 未通过
- **THEN** 05.3 只能保留“拟新增/未启用”草稿，不得勾选完成，也不得把 CI 段落写成“可用”

#### Scenario: 文档预算只降不升

- **WHEN** 四篇文档需要新增内容
- **THEN** 篇数不增、总行数不超 `scripts/doc_budget.json` 基线；`test_extra_file_fails`、`test_extra_lines_fail` 与 `test_tighten_refuses_to_raise_and_leaves_the_file_alone` 保持红，`--tighten` 拒绝任何上升

### Requirement: 本地完成与远端必跑门分层登记

最终验收 SHALL 把证据分为三层并分别登记：L1 本地确定性完成（最终集成树的 MR 等价命令、原始退出码、artifact 与指纹；计划校验 MAY 直接按域执行 `openspec change validate event-consumption-integrity/changes/<domain> --strict --json --no-interactive`，MUST NOT 临时复制子变更或改主规格）、L2 远端 MR 与 required（推送分支、创建 MR、分支保护实际生效）、L3 72h 与真实服务（真实模型/渠道/掉电/长期运行）。L1 通过 MUST NOT 被记录或表述为 L2/L3 通过；L2/L3 未获授权时 SHALL 标为 BLOCKED/待验并保持未勾选。提交、推送、创建/更新 MR、修改分支保护、打 tag 与真实服务调用 MUST 分别取得明确授权，MUST NOT 由 apply 或本域任务隐含授权。最终偏差验收 SHALL 由与实施者分离的验收者执行，逐条保存“计划原文/实际做法/原因/影响”；验收者不可用时 MUST 标“待验”，MUST NOT 以实施者自查冒充独立准出。

#### Scenario: 本地绿不冒充远端

- **WHEN** 05.4 的本地 MR 等价命令全部通过，或 05.1/05.2 用与 CI 相同的命令加 `CiWorkflowTest` 结构验收通过，但尚未推送或未获 MR/保护规则授权
- **THEN** 只登记 L1 完成；L2 保持 BLOCKED/待验，不勾选远端项、不宣称 required 已通过；05.1/05.2 的本地完成门不是远端 CI 绿

#### Scenario: 未授权不执行远端动作

- **WHEN** 仅有规划/实施授权而没有提交、推送、MR 或保护规则授权
- **THEN** 不执行 `git commit`/`git push`/创建 MR/修改 required/打 tag；越权执行即判失败

#### Scenario: 72h 与真实服务单列

- **WHEN** 短程与长程确定性证据通过
- **THEN** 72h 隔离长跑、真实模型、真实渠道与掉电恢复仍单列为待验，不以 mock、优雅关闭或六组合矩阵替代

#### Scenario: 独立验收不可用

- **WHEN** 无法安排与实施者分离的验收者
- **THEN** 偏差验收标“待验”并说明缺口，不以实施者自查签署通过
