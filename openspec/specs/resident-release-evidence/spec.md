# resident-release-evidence Specification

## Purpose
发布证据工具对底层失败保真：包装器不得把编译/超时/断言失败粉饰为绿色，race 豁免仅限登记签名。
## Requirements

### Requirement: 验证工具必须对底层失败保真

测试包装器 SHALL 保留底层命令状态与失败输出；编译失败、非法包、超时、普通断言失败 MUST 非零。上游 race 豁免 SHALL 只适用于登记的具体签名、版本和测试，不因栈位于依赖包就一律放行。没有 race 文本 SHALL NOT 被视为测试成功。

#### Scenario: 非法包
- **WHEN** race wrapper 收到不存在的 Go 包
- **THEN** 返回非零并显示失败，不输出成功 verdict

#### Scenario: race 豁免混合普通失败
- **WHEN** 输出含已知上游 race 和普通断言失败
- **THEN** 总结果失败，不被 race 豁免覆盖

### Requirement: 证据分层与可复现性

每个工作包 SHALL 记录基线 commit、配置、命令、结果、跳过项、失败原因、影响文件到测试的映射；真实 LLM、独立进程、掉电、长期运行证据 MUST 分开，不以 mock/优雅 Close/同进程 New 代替。历史报告的已修/部分/仍在/未验证 SHALL 明确标注，归档勾选不得单独证明修复。

#### Scenario: 只有 mock 与优雅关闭
- **WHEN** 仅完成 mock 或同进程 write→Close→New 测试
- **THEN** 报告限定证据范围，不宣称真实模型质量、掉电恢复或多天稳定性已验证

### Requirement: 双模块与真实消费者验收

CI SHALL 分别覆盖根模块与 wechat-bot 独立模块 build/vet/short，并覆盖 memory 子包、agent、plugin、rl、evolution、event、tool 的定向 race。恢复/任务/热更的集成验收 SHALL 到达实际模型请求、diagnostics、任务结算及宿主投递决定，不能以写盘、tracked=true 或 applied 日志作为终点。

#### Scenario: 恢复后实际请求
- **WHEN** 新子进程从真实 localfile 恢复并发出首次模型请求
- **THEN** 测试捕获实际消息序列、source IDs、票据、恢复状态，与死亡前基线对账

### Requirement: 常驻准出证据

发布候选 SHALL 具备确定性 30 轮独立进程重启 E2E 与 72h 隔离长跑记录。长跑 SHALL 包含并发输入、存储拒写、慢消费者、mock 模型/MCP 故障、任务重挂及热更；durable 接收项不得无解释丢失，确认项不得重复投影。资源指标按 design D7 的稳态阈值验收，失败先定因而非调宽门槛。

#### Scenario: 尚无 72h 证据
- **WHEN** 短程回归通过但未完成长跑
- **THEN** 可标实现阶段完成，不能标长期运行发布门通过；遗留明确待验

### Requirement: 发布与外部操作权限

提案/apply SHALL NOT 隐含授权实际部署、付费模型调用、提交/推送/tag/发布或对外提单。外部验证缺授权 SHALL 标为 BLOCKED/待验并说明，不伪造完成；独立代码审查失败/不可用也 SHALL 明示，不能以主执行者自查冒充独立准出。

#### Scenario: 缺少部署授权
- **WHEN** 本地实现与测试已完成但没有外部环境授权
- **THEN** 交付本地结果和待验项，不操作真实部署或打发布 tag

### Requirement: 编排热更以生产委派变化验收

编排热更 SHALL 以宿主生产入口验收：修改现有 YAML 后，新开始请求的实际工具声明与子调用目标随之变化；进行中请求及其重试、嵌套子调用、ACK 后后台执行保持原绑定；常驻 store/session/projection/TaskManager 身份不漂移；旧绑定与资源在实际执行停止后安全退役。候选失败、回滚、agent 增删、同名重入与并发发布交叉场景须有真实行为断言，不以指针、hash 或代际日志编号代替。

性能 SHALL 分报候选构造/发布成本、请求获取版本开销与退役回收成本。真实 LLM、掉电、72h、真实渠道和生产存储单列授权；历史完成勾选不得继承为本目标完成证据。

#### Scenario: 生产入口切代验证

- **WHEN** R1 在 G1（A→B）开始执行后发布 G2（A→C），随后 R2 开始
- **THEN** R1 的模型声明与实际调用保持 B 直至结束（含后台），R2 调用 C；两者均不以日志编号替代行为断言

#### Scenario: 常驻身份与任务板不漂移

- **WHEN** 连续多次编排发布/回滚并伴随在途请求
- **THEN** 既有 resident 契约测的存储身份、会话续写与任务板断言原样通过；被替代的拓扑冻结断言按新规格迁移并有记录

### Requirement: 安全验收不由统计或豁免替代

短锁 SHALL 用阻塞构建／阻塞清理的屏障验证，owner 有界性 SHALL 覆盖不断更换名字后的收敛，实际停止 SHALL 覆盖取消后仍存活的生产者。平均耗时、有限次数 race 未命中、固定几个名字发布多代均 MUST NOT 替代相应安全断言。

race 验收 SHALL 保留全部失败，包括全上游访问栈；family 和 broad framework 分类只能用于诊断，不得吞掉子进程错误。命令记录原始退出码。完整补丁的必要未跟踪文件 SHALL 进入交付清单，HEAD-only 基线结果不能冒充新补丁验证。

#### Scenario: 纯上游 race 也阻断验收

- **WHEN** 子进程只报告纯上游栈 DATA RACE，或同时含断言失败／panic
- **THEN** 验收保持失败并保留原输出和退出码，不因分类器识别旧家族或没有 tagent 帧而通过

#### Scenario: 慢构建与历史名字分别验证

- **WHEN** 构建／回收停在屏障，同时持续发布不同名字并令无引用旧资源收敛
- **THEN** 请求获取不等长操作，已结束 owner 数量回落；性能样本单独记录，不用机器平均延迟替代行为证据

### Requirement: 验收覆盖完整装配链与复杂度收敛

最终证据 SHALL 覆盖热增后的真实存储／热参消费、回滚共享依赖、多级重入、remote-only 热更、ACK 后父 turn 结束的后台回流、Close 超时后无新活动的最终资源退出。MUST NOT 把 resident getter 测与独立 shell 单测、inline 返回与 ACK 返回、远端重试与本地跨发布分别通过拼成整链已通过。

性能 SHALL 分开配置读取、完整候选构建、已准备候选提交、租约获取和具体回收；计时不混入编辑、探针、断言、日志或轮询等待，若测试测的是端到端则明确标注。所有未发布候选与测试资源 SHALL 关闭。复杂度验收 SHALL 展示重复热更 shell／状态／双事务／广播和角色专用执行旁路已移除，构造数量随真正完整 tagent 和实际上下文而非发布次数增长；MUST NOT 以全组织只剩一个 TaskManager 或删掉子 agent 的 bus 作为优化证据。

#### Scenario: ACK 后的跨发布与回流

- **WHEN** 宿主已收到 ACK 且父 turn 结束，后台 producer 仍停在屏障，此间发布 G2
- **THEN** 后台按 G1 完成，实际资源随后释放，原 TaskManager 的 task_settled 唤起 G2 新 turn；dense 窗内 inline 返回不能替代此测试

#### Scenario: 完整补丁的最终门禁

- **WHEN** 准备标记全部任务完成
- **THEN** 先列所有修改及必要未跟踪文件的测试映射，root build/vet/short 包含 tests 子包，wechat-bot 独立执行 build/vet/short，全部受影响包与跨发布矩阵零豁免 race；保存真实退出码、命中过滤器及未跑范围，工件 strict 不能替代代码验收

#### Scenario: 资源测试路径不跨用例共享

- **WHEN** 同一进程重复运行不同热更／恢复测试
- **THEN** 路径由 testing.TB 生命周期管理，重启模拟只在同一测试内显式共享根；先关闭资源再结束临时根，无工作树 fallback，缺少前后快照时不声称未新增用户数据

### Requirement: 完整门禁与 golden 真实运行输出

准出 SHALL 以**完整门禁命令集**为据，且逐条命令**独立记录原始退出码**（MUST NOT 用 `grep/head/tail` 的退出码代替原命令）；两 module（根 module 与微信独立 module）SHALL 分别 build/vet/test。零失败 MUST NOT 由过滤输出、命中过滤器或子集读数得出。

正式装配场景 SHALL 输出**真实观测**并与 golden 对照，断言落到**值/消息/身份关系**（MUST NOT 以键名对齐或"人工 PASS 汇总"代替）；缺 case、错值、未知断言、子测试 Skip、包级 Fail 均 SHALL 判失败。随机 ID/时间可规范化，但 SHALL 保留一对一关系、顺序与因果；观测器 MUST NOT 写成期望值复制器。

#### Scenario: 门禁不靠过滤得出

- **WHEN** 汇总准出门禁结果
- **THEN** 每条命令含原始退出码与未跑范围；任一 非零 退出即失败；不以子集或过滤后的读数宣称全绿

#### Scenario: 值断言而非键名

- **WHEN** 场景 runner 与 golden 对照
- **THEN** 报告给出值/消息/身份关系的差异；仅键名一致不构成通过；子测试 Skip 视为失败

### Requirement: 离线迁移与回退保真（读旧写新，不伪 restore 副作用）

离线迁移 SHALL 只读旧数据并导入**新路径**：源数据摘要（字节）MUST 保持不变，MUST NOT 让旧程序读新格式（新格式 MUST NOT 回写旧路径）；缺失外置文件与不可恢复任务 SHALL 具名列出。迁移中断 SHALL 可重跑，回退 SHALL 只读旧数据、走新路径。

回退 MUST NOT 伪造 restore 副作用：不得假装写回了本不存在的记录、不得自动改写旧档、不得以"回退成功"掩盖未清理的新路径残留。

#### Scenario: 源字节不变

- **WHEN** 完成一次离线迁移
- **THEN** 源数据摘要（字节）与迁移前一致；旧程序无法读到新格式（新格式未回写旧路径）

#### Scenario: 中断可重跑、缺口具名

- **WHEN** 迁移中断后重跑
- **THEN** 可收敛到一致结果；缺失外置文件/不可恢复任务被具名列出，不静默跳过

#### Scenario: 不伪 restore 副作用

- **WHEN** 执行回退
- **THEN** 仅只读旧数据、走新路径；无伪造的写回记录、无自动改写旧档；残留如实呈现

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

`README.md`、`docs/OPERATION.md`、`docs/FACT-VOCABULARY.md`、`docs/GLOSSARY.md` SHALL 登记事件双档、故障变异、required 汇总、证据档位与失败处置，且只陈述已完成并有真实命令/退出码的能力。未落地的作业、合同或检查器 MUST 标注为“拟新增/未启用”，MUST NOT 让旧 `check_runtime_evidence.py`、旧 `scenarios.json` 或旧十域 graph 被描述为已支持新合同/新图。文档中的每条命令 MUST 可在当前树复跑且与 CI 稳定检查名一致。历史文档 SHALL 只就地勘误并标注日期与原因，MUST NOT 改写历史结论或追溯修改归档 change。05.3 的最终验收 SHALL 以 05.2 的真实输出与 workflow 自测通过为前置；在此之前只允许写“拟新增/未启用”草稿，MUST NOT 勾选完成，也 MUST NOT 把 CI 段落写成“可用”。文档更新 MUST NOT 新增 `.md` 篇目，MUST NOT 上调 `scripts/gates/doc_budget.json`；净增行 SHALL 用同篇删减等量置换，确无可删减内容时 MUST 停止上报而非放宽基线。

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
- **THEN** 篇数不增、总行数不超 `scripts/gates/doc_budget.json` 基线；`test_extra_file_fails`、`test_extra_lines_fail` 与 `test_tighten_refuses_to_raise_and_leaves_the_file_alone` 保持红，`--tighten` 拒绝任何上升

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
