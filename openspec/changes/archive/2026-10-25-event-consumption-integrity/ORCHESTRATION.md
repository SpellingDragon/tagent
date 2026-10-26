# event-consumption-integrity 编排

## 一、结论

抵达本地 DoD 的路径是：契约与复现证据 → 已证实业务修复 → 正式消费者集成 → 短/长程与故障变异 → 每 MR 接线及独立验收。当前已完成计划回写（用户 2026-10-23 裁定 03 域细化为 11 叶）、计划回写②（01.5 拆为 01.5/01.6/01.7、02.5 拆为 02.5/02.6、采集顺移 02.7）、计划回写③（用户评审 v3：01 域 7→10 叶，新增 01.8/01.9/01.10；01.2/01.3 补差重开并回退勾选；00.3 增补 observer 失败语义；另立三项登记）与计划回写④（用户裁定 C：01.10 拆为 01.10a/01.10b 两叶分批，最终验收面不变；共 42 叶/32 勾）；业务实施按叶授权推进，最大未决项是原调用/版本与恢复责任的契约核准。规划齐全不解锁业务实施。

## 二、编排四问

### 2.1 次序：产物就绪与最终验收分开

[task-graph.json](task-graph.json) 的 42 条 `leaves` 是逐叶前置、写集、命令与三锚的完整清单。本文件是派发唯一入口；派发前读取对应叶及二级四件套，禁止只凭会话摘要执行。

- `depends_on`：前置域产物经本单组件/接口核验可供集成，不要求其依赖集成后的 checkbox 已勾选。
- `verify_after`：本叶最终验收前置；缺正式消费者证据时不勾选。
- 实际调度按两阶段节点展开：每叶 `ready` 与 `verified`；执行依赖连接前置 ready 到本叶 ready，本叶 ready 到自身 verified，验收依赖连接前置 verified 到本叶 verified。展开图无环才可派发，不能仅因两种边分别无环就假定整个调度无环。
- 02 域先交可集成产物，01.5 单写根装配、01.6 单写微信宿主装配，再正式复验 02；01.5/01.6 不等 02 的最终 checkbox。共享文件锁按就绪队列仲裁，不写出“01.5 后才允许 02 开发”的反向顺序。
- 00.2 红证产物先供 00.3；原案例未复现与独立风险复现分别记录。已确定回归测试在修复后仍加入最终 short，不因早期诊断角色而永久排除。
- 04.2 实跑验证消费 01.5/01.6/01.7/03.2—03.9 的已就绪正式测试；03 直接执行 go test 不依赖 runner。05.1 在 03.10 校准与 03.11 长程产物就绪后接线。05.3 可先写草稿，最终验收等 05.2。

graph 中所有叶 `status=planned`（规划态，与 tasks.md 勾选分开）；当前 tasks.md 已勾 32 叶、未勾 10 叶（01.2/01.3 补差重开后复勾、01.8 已勾；01.9 就绪待派；01.10a/01.10b 为拆分新叶；余为 02.3、03.11、04.2、05.1—05.4）。下表是获得实施授权后的就绪关系，不是已发出的实施单：

| 叶 | 派单 | 产物前置 | 最终额外前置 | 文件锁/下一消费者 |
|---|---|---|---|---|
| 00.1 | P0 | 授权 | — | 契约账目；00.3/00.4 |
| 00.2 | P3R | 03.1 | 01.5 后最终回归 | host 测试；00.3 |
| 00.3 | P0 | 00.1、00.2证据、00.4 | 待裁项具名阻塞 | 契约账目；01/02/04 |
| 00.4 | P0B | 00.1 | — | 隔离基线；00.3/F2 |
| 01.1 | P1 | 00.3 | — | 根/任务；01.2 |
| 01.2 | P1 | 01.1 | — | 根/状态/history 测试；补差重开（handover_before_confirmation）；01.3/03.9 |
| 01.3 | P1 | 01.2、03.1 | — | 根/host 测试；补差重开（notice_input_without_projection）；01.4 |
| 01.4 | P1 | 01.3 | — | 根/host 测试；01.5/01.6/01.7 |
| 01.5 | P1 | 01.4、02.1—02.5、02.7 根接线产物 | — | 根/host 测试；02/03 复验 |
| 01.6 | P1 | 01.4 | — | 微信 smoke/public 测试；02.6/03.4 |
| 01.7 | P1 | 01.4 | — | CLI host 测试；05.1 |
| 01.8 | P1P | 00.3 | — | 根/memory/history 测试；01.9/04.4/F 收尾 |
| 01.9 | P1R | 01.8 | — | 根/memory/history 测试；下一个实际请求；04.4 |
| 01.10a | P1X | 00.3 | — | tool/exec 包内测试；01.10b |
| 01.10b | P1X | 01.10a、00.3 | — | tool/exec + execution 测试/根装配；04.4/既有通知链 |
| 02.1 | P2M | 00.3 | 01.5 | meditation；01.5 |
| 02.2 | P2M | 00.3 | 01.5 | meditation/host 测试；01.5 |
| 02.3 | P2P | 00.3 | 01.5 | replay/history 测试；01.5/03.9 |
| 02.4 | P2L | 00.3 | 01.5 | lease/根编译闭包/host 测试；01.5 |
| 02.5 | P2A | 00.3 | 01.5 | approval/capabilities 测试；01.5/03.4 |
| 02.6 | P2A | 00.3 | 01.6 | approval/微信测试；01.6/03.4 |
| 02.7 | P2C | 00.3 | 01.5 | capture/根编译闭包；01.5 |
| 03.1 | P3R | 授权 | — | 根fixture；00.2/03.2—03.3 |
| 03.2 | P3R | 03.1、00.3 | 01.5 | host 测试；04.2 |
| 03.3 | P3R | 03.1、00.3 | 01.5 | host 测试（同文件，串 03.2→03.3）；04.2 |
| 03.4 | P3W | 03.1、00.3 | 01.5 | 微信smoke；03.5—03.8/04.2 |
| 03.5 | P3W | 03.1、00.3 | 01.5 | 微信smoke；03.6/04.2 |
| 03.6 | P3W | 03.1、00.3 | 01.5 | 微信smoke；03.7/04.2 |
| 03.7 | P3W | 03.1、00.3 | 01.5 | 微信smoke；03.8/04.2 |
| 03.8 | P3W | 03.1、00.3 | 01.5 | 微信smoke；03.10/04.2 |
| 03.9 | P3R | 03.1、01.2 | 01.5 | history 测试/测试子进程；04.2/04.4 |
| 03.10 | P3W | 03.1、00.3 | —（只读校准） | 微信smoke/校准报告；03.11/05.1 |
| 03.11 | P3W | 03.2、03.3、03.9、03.10 | 01.5 | 微信smoke/长程CPU与IO；05.1 |
| 04.1 | P4 | 00.3 | — | Golden/evidence；04.2 |
| 04.2 | P4 | 04.1 | 01.5、03.2—03.9 | runner/evidence；04.4/04.5 |
| 04.3 | P4A | 00.3 | — | codetools/architecture/lint；04.5 |
| 04.4 | P4M | 04.2、03.2、03.3、03.9、01.5、01.2、01.8、01.10a与已证实缺陷 | 对应正式绿证 | 隔离变异树；04.5 |
| 04.5 | P4 | 04.1—04.4 | — | plan/evidence/lint；05 |
| 05.1 | P5 | 03.11、04.5 | — | CI/preflight/workflow自测；05.2 |
| 05.2 | P5 | 05.1 | — | CI/workflow自测；05.3/05.4 |
| 05.3 | P5D | 01.5、04.5 | 05.2 | 当前文档；05.4 |
| 05.4 | P5V | 05.2、05.3 | 独立验收 | 最终树只读；F1—F3 |

初始 ready 候选仅 00.1、03.1；00.4 隔离基线与后续新测试互不依赖。未授权、前置不足或读写闭包未核验时不派单。关键路径见 [tasks.md](tasks.md) 的红色节点。

**推进顺序（用户 2026-10-26 指定）**：合单A（01.2/01.3 补差 + 01.8 持久化与消费确认）→ 01.10a 执行资源查询面 → 01.10b 状态事件与通知接线（用户裁定 C 分批）→ 01.9 冷启动恢复 → 04.4 补变异 → 03.11 全量重跑 → 04.2 生产验收 → 05 域 → F 收尾。另立三项（检索候选语义、完整请求预算、业务结果验收与效果评测）待用户另行发起，不在本序列。**注**：02.3（`rebuild_refolds_from_wal`）不在上述序列，保持未勾选，待单独排期确认；不因序列未列出而勾选或删除。

### 2.2 授权：精确写集与单写所有者

本轮写入只限 `openspec/changes/event-consumption-integrity/` 的规划物。未来每单白名单取 `task-graph.json` 的对应 `leaves[id].write_files` 与二级 design 交集，并裁剪至本单所需；二者不一致先修规划，不由执行者自行扩大。当前不授权该列表中的产品文件。

| 派单 | 文件面与归属 |
|---|---|
| P0 | 当前 change 的一级 proposal/design/tasks/ORCHESTRATION/coverage/findings/task-graph、00 域证据摘要；一级账目只由编排者核销 |
| P1 | `host.go`、`invoke.go`、`run.go`、`prepare.go`、`tools.go`、`facts.go`、`request.go`、`settle_turn.go`；任务/state/loop 文件按01各叶精确列表；01.6整合 `examples/wechat-bot/main.go`、`wechat.go`、`wechat_approval.go`；01.7整合 `examples/cli-host/main.go` |
| P1P/P1R | `facts.go`、`run.go`、`prepare.go`、`request.go`、`memory/recall.go`、`memory/store_query.go`、`memory/ledger/{tier_facts,localfile,segment_store,original_store}.go`、`memory/search/{store_searcher,fusion}.go`；01.8/01.9 单写，01.9 只消费 01.8 产物 |
| P1X | `tool/exec/{tool,exec,monitor,tmux,tmux_executor,stop,decl}.go`；01.10a 单写 `tool/exec/event_integrity_test.go`（包内测试），01.10b 单写 execution 测试与根装配；不建第二套执行资源仓；01.10b 新增 public API 先列最小面再动，超面停报 |
| P2M | `evolution/meditation/driver.go`、`reflection.go`、`watermark.go`、`deliver.go`；02.1/02.2共享包单写 |
| P2P | `age_fold.go`、`memory/replay/age_segments.go`、`replay_budget.go`、`fold.go`、`projection.go`、`replay_settle.go` |
| P2L | `host_reload.go`、`host_model.go`、`rl/replacement.go`、`lease_model.go`、`lease_observer.go` |
| P2A | `evolution/governance/policy.go`、`runtime/task/approval/approval.go`、`approval_store.go`；02.5/02.6共享审批面单写；`examples/wechat-bot/wechat_approval.go` 归 02.6 |
| P2C | `host_capture.go`、`rl/capture_wiring.go`、`sink.go`、`produce.go`、`export_chain.go`、`tool/subagent/capture.go`、`builder.go` |
| P3R | 各叶指定的 `tests/integration/{execution,history,host,evolution,capabilities,learning}/event_integrity_test.go`；根fixture在 `scripted_provider_test.go`、`meditation_boot_test.go`、`public_runtime_entry_test.go`；现有 settle/soak 用例不删 |
| P3W | `examples/wechat-bot/smoke_harness_test.go`（含 01.6）、`smoke_test.go`、`smoke_errors_test.go`、`smoke_data_test.go`、`wechat_test.go`（含 02.6）；CLI `examples/cli-host/main_test.go`（01.7）；不新增test-mirror例外 |
| P4/P4A/P4M | 04各叶列出的现有runner/checker/selftest/codetools及拟新增变异脚本、事件合同；Golden编排者单写，执行者提交修改建议 |
| P5/P5D | `.github/workflows/ci.yml`、`scripts/preflight.sh`、`scripts/test_check_runtime_workflow.py`；`README.md`、`docs/OPERATION.md`、`docs/FACT-VOCABULARY.md`、`docs/GLOSSARY.md` |
| P5V | 最终受测树只读，日志写显式临时输出目录；缺陷修复回交原叶，不在验收单越界改代码 |

不修改原有未净子模块、用户配置/数据、旧批准计划。新公开 API、持久格式、依赖、发送重试策略或新产品入口均为裁定边界。现有无生产消费者的声明不自动成为接线目标。

每派单携带：叶ID、必读文件、工具链、前置凭据、精确写集、正负消费者锚、质量门、自清发现责任及上报边界。共享文件发现归编排者；本单文件发现由本单修复。没有独立消费者依据不预建接口或测试后门。

### 2.3 凭据：按单五查

1. 勾选真实性：产物ready与最终verified分栏，缺消费者、未跑子场景或未裁定门不勾选。
2. 产物盘点：写集、实际文件、spec场景、test selector逐项对齐；新增未跟踪文件不从统计漏掉。
3. 数字溯源：short十份、long六组合/7200输入/720重启、变异清单与原始退出码由编排者核验。正文重复次数不作为业务计数。
4. 遗漏检测：原验收面与完整E1—E8覆盖，不以子集绿或聚合记录替代独立负锚。
5. 正式消费者：Boot/公开Run、微信正式应用链、CLI run、审批、反思、投影、替换与采集/显式导出实际消费；测试手工補挂产品缺失接线不合格。

每叶命令原文在二级 tasks 及 graph verification.commands 中。产品命令是拟新增测试的未来验证入口；本轮不执行。单轮 `-count=1 -race -json`，十次独立日志与退出码；现有checker扩展后逐轮校验真实run/pass。不得用grep计数、echo末行、编译失败或超时替代业务判定。

波后独立验证者与实施者分离，对照原任务行验收面；达标才核销，缺消费者补差重开，前置变化连带回归，计划缺口先回写。验证工具失败或不可用如实记录，不以主执行者自查冒充独立验收。

### 2.4 释放：锁与等待表

| 资源锁 | 持有人 | 释放条件 | 等待者 |
|---|---|---|---|
| root-runtime | P1 | 当前根改动可编译且本单证据已核验 | 01后续叶（含 01.8/01.9）、01.5、P2根文件、P3正式测试 |
| host-event-integrity-test | 当时就绪单的唯一写者 | 写后读回、完整用例留存 | 00.2、01.3—01.5、02.2、02.4、03.2、03.3中其余ready叶 |
| history-event-integrity-test | 当时就绪单的唯一写者 | 同上 | 01.2、01.8、01.9、02.3、03.9 中其余 ready 叶（按 01.2→01.8→01.9→02.3→03.9 串行） |
| execution-event-integrity-test | 当时就绪单的唯一写者 | 写后读回、完整用例留存 | 01.1、01.10b 中其余 ready 叶（按 01.1→01.10b 串行） |
| exec-package-test | 01.10a 唯一写者 | 写后读回、包内用例留存 | 01.10b 只读；04.4 变异副本 |
| memory-recall | 当时就绪单的唯一写者 | 写后读回、检索/持久化用例留存 | 01.8、01.9 中未 ready 叶 |
| exec-lifecycle | 当时就绪单的唯一写者 | 写后读回、进程/租约用例留存 | 01.10a/01.10b 与 04.4 变异副本 |
| wechat-smoke | 当时就绪单的唯一写者 | 写后读回、完整用例留存 | 01.6、02.6、03.4—03.8、03.10、03.11中其余ready叶 |
| approval-surface | 02.5/01.5/01.6/02.6 的当时唯一写者 | 写后读回、审批面用例留存 | 02.5、02.6、01.5、01.6 其余ready叶、03.4 |
| shared-fixture | P3R/P3W | 接口/脚本与负控可用 | 00.2、03.2—03.11 |
| golden-policy | 编排者 | 独立核准合同差异与消费者映射 | 04.2/04.4/04.5/05 |
| test-process-and-data | 每用例自身 | Close/Wait和独立临时资源回收可证 | 本用例下个恢复点 |
| long-cpu-io | P3W | 当前组合结束且原始证据留存 | 后续组合、变异、最终回归 |
| remote-authority | 用户/管理员 | 明示授权及必要凭据 | F4/真实服务/长期发布证据 |

释放立即重算等待队列和W/R，不等待整个域。没有ready项登记原因。超过预算、缺授权、重大契约未裁、前置断裂是停点；正常波次记账不是再次索取同一授权的理由。

## 三、明细与预算

- 六域叶数为4/11/7/11/5/4，共42；一级另有六域总账与F1—F4。测试注册/场景计数以 graph 显式条目为准，不以手写总数裁剪验收。
- 八类消费者和逐delta场景映射见 [coverage.md](coverage.md)；静态发现、用户裁定、待验证条件见 [findings.md](findings.md)。
- 最多三份源码派单，单个叶可独立派发的收益为解锁下游；共享热点合单。同树验证未排除W/R交叉时不并行。
- 每MR short十次独立，long六组合各一次，mutations有效清单全跑。每组合1200输入/120次真实重启，不重复计算同一运行的三类断言。
- 候选熔断short20m、long45m、mutations30m仅待校准；外部服务支出为零。超线保留证据，先优化分片/编译复用，不调整产品fsync/冥想或覆盖下限。
- 既有preflight、Golden、三模块build/vet/test和race保持；新增事件汇总不替代旧job。真实模型/微信/掉电/72h/训练制品是独立证明边界。

## 四、台账

| 阶段/派单 | 产物/核验 | 凭据摘要 | 状态与未完成 |
|---|---|---|---|
| 现场 | HEAD、status、change目录 | 基线6cc4414；原有子模块未净；仅`.openspec.yaml`为原有规划文件 | 已核验，无清理 |
| 范围裁定 | 用户明确选择 | 全事件、业务修复+检查、每MR双档；四共享会话/720真重启；发送未知不自动重发 | 已纳入规划 |
| 规划物化 | 一级与六域四件套、graph、coverage | 编辑工具写入恢复，逐文件读回；实施checkbox全未勾选 | 规划文件已生成 |
| 规划纠偏 | 独立核验及闭环复查 | 8项跨文件偏差已逐项修正；末轮补齐04.3摘要归属、Golden锁、双角色正锚；区分独立消费者场景与复用正锚，允许一叶多个宿主测试 | 已读回核对；业务契约待裁门仍保留 |
| 规划校验 | CLI strict、graph、范围核对 | 一级4/4 artifacts；一级与六域strict均valid、零issue；29叶全planned；58阶段节点/103边拓扑通过；20父测试/101具名锚、111个delta场景映射 | 仅规划结构通过；旧产品checker与产品测试未运行 |
| W0+实施首轮 | 00.1盘点/03.1台架/00.2诊断/00.4基线/00.3冻结 | 00.2两红证两轮可复现（EVID-02双路材料、EVID-03忙丢）；00.4三模块绿+deterministic既有间歇1/5；00.3裁定invocation冲突=语义收窄（可推翻）；复验：JSON检查/单轮重跑/勾选核对全 exit 0 | 00.2勾选等 01.5 后十轮绿证 |
| W1首批 | 01.1/02.1/04.3 | 01.1六锚十轮绿、未改产品；02.1证实并修复 driver 忙时丢观察（retained+rearm）、十轮绿；04.3归属表14符号/6owner+codetools门+41负例；终态复验 vet/build/归属门/两叶抽验全 exit 0 | 02.1勾选已回退（域头规定 01.5 回验后才可勾，调派令措辞失误，产物保留 ready） |
| 04.3待裁项裁定 | 编排者 | 全部保留现状：track-delivery/feed-boundary/book-notice/advance四符号入表；DelegationTasks.Advance 不入表（00.1无出处，宁缺勿私定）；host 14文件清单口径确认；4 uncovered（run.go:307/tools.go:621方法值端口、delegate.go:414/shutdown.go:192接口门面）保持报告不计数，后续冻结端口走表扩充 | 无 |
| 并发教训 | 编排者 | 04.3 跑全仓 vet 时读到 01.1/02.1 写入中间态报 vet 失败——W/R 验证闭包交叉实测；后续含全仓/lint验证的单与写根测试的单不同批 | 已在终态复验排除 |
| W1二批 | 01.2/04.1 | 04.1合同 20测试/101唯一键/8族/matrix 7200/720，checker v2+9自测 63全绿，v1零改动，复验 ok True/20/101；01.2 五锚十轮绿，S6（跨进程已确认不重做）十轮同红——消费确认仅内存，durable 恢复重放 owed；停点上报候选 A—E | 01.2 曾 blocked；01.3（depends 01.2）曾连带阻塞 |
| 用户裁定① | 用户 | 候选A：授权 FactLog consume: 前缀键持久化逻辑消费回执（其余持久格式/API仍禁）；invocation裁定维持语义收窄 | 已回填 00.3 冻结件 authorized_narrow |
| 01.2完成+seam收尾 | 01.2 | 候选A落地：回执下沉 mailbox Consume（先持久后内存确认、失败不确认，负锚验证），run.go 构造接线，导出面收回（RecordConsumption 零命中）；单轮+十轮全绿，四包回归绿，归属门 violations=0；S6 前后 10轮fail→10轮pass | 已勾选；尾巴：gen_godoc --check 红（runtime/state 构造变参与 godoc 更新；scripts/codetools 为 04.3 批次既存）——docs/api 重生成留 05.3/收尾统一处理 |
| 产品实施累计 | 38叶 | 已勾：00.1/00.3/00.4/01.1/01.2/01.3/03.1/04.1/04.3（9叶）；ready未勾：00.2/02.1（等 01.5 回验）；已证实缺陷：EVID-02双路材料、EVID-03忙丢（01.3已修）、G1冥想忙丢观察（02.1已修） | 下一批就绪：02.2/02.4/02.5/02.6/02.7/04.2/03.2/03.3 |
| 并发波次甲 | 01.4+04.2+02.6 | 01.4七锚十轮绿已勾（零产品改）；04.2开发+自测 30+83 全绿（生产验收留待）；02.6微信审批六锚十轮绿（零产品改，变异证伪） | 04.2未勾等生产验收；02.6等01.6 |
| 并发波次丙（三单） | 回验勾选15叶 / 04.4 / 03.11 | 回验：全族四命令 exit 0+00.2十轮绿→勾选 00.2/02.1/02.2/02.4/02.5/02.6/02.7/03.2—03.9（02.3除外）；04.4：6/6变异killed副本树隔离零污染已勾；03.11：4/6绿（4800/480全对账），2/6红——**发现新产品缺陷 EVID-04**（256窗口下折叠重写后 NoticeBoundary 位置越界→通知正文对模型不可见），未勾 | EVID-04 交修复单 |
| EVID-04修复+04.5 | 单N / 单O | 根因：replayEntry.ended 记提交时位置，折叠重写后越界降级；修复：request.go/facts.go 改按事实身份解析边界（endedAt EventID+endedPrefix）；两红组合重跑绿（47/47通知、58/58消费、gen16 answered），已登记 00.3 defects[EVID-04]，负锚 notice_visible_under_fold；04.5：domain_profile分流/映射闭环/lint对象修正，43+93自测绿已勾 | 尾巴：04.4变异自测因EVID-04登记转红需补变异；03.11待六组合全量重跑；proc-refs两处（mutations.py:46/event_ownership.go:10 引用change工件）待修；comment_policy 203 findings 待收尾清零 |
| 当前账目 | 42叶 | 已勾33：00全4/01九（余01.10a/01.10b）/02六（除02.3）/03十（除03.11）/04五全（04.4补变异后 9/9 killed+1 pending，自测12 OK）/05零 | 未勾9：01.10a（就绪）/01.10b（等01.10a）/02.3（rebuild_refolds待实施）/03.11（待六组合重跑，留干净树批）/04.2（待生产验收，留干净树批）/05.1—05.4；尾巴：一级账目1.1—1.6/F1—F3、docs/api重生成、comment棘轮清零、proc-refs（mutations清单已入 scripts，待05.3核） |
| 计划回写④+01.9+04.4补变异 | 用户裁定C拆叶 / 单Q / 单R | 回写④：01.10拆 01.10a（查询面，包内可做）/01.10b（状态事件生产，最小API面先行列明），00.3 追加授权登记；01.9：红证→读侧缺口（Boot后 r.replays 空）→run.go seedReplay 修复→五锚十轮绿（优雅Close+SIGKILL两态、后续请求实携恢复材料）；04.4补：EVID-04补丁集变异（身份边界回退）+01.8存储伪成功+01.2交接前确认 三变异 killed，01.10b alive-process pending 登记不启用，自测 2red→12OK | 01.9已勾；04.4验收面补全 |
| 并发波次丁（四单，R32前最后一混波） | 03.11重跑 / 04.2生产验收 / 05.1 CI / 05.3文档+尾巴 | 01.10a六锚十轮绿已勾；02.3 rebuild_refolds 六锚+十轮绿已勾（提交边界重放逐字节一致1927B）；03.11：**3/6红**（256 三 seed gen8 通知不可见——归因 seedReplay 折叠 synthetic 重建后占满预算前缀→通知被静默跳过；32768 全对账 47/47）；04.2：受阻（03.6 FoldControl/negative 红：重启后重折产生新 compress.request；变异锚 stale）；05.1：结构完成（4 job+15自测），同命令红因叶外；05.3：尾巴三件绿（docs/api 4文件重生成/注释棘轮 160→0/proc-refs绿）+四篇文档，预算持平 | **R32 教训源**：编排者被 03.11（45min）阻塞 25min；外推差 40× |
| **R32 长跑单阻塞编排（用户纠偏）** | skill 回写 | 并行 Agent 调用返回=max(各单)：03.11 与三快单同波，快单回收后续派被阻塞；且 03.10 外推 10-14s/组合 vs 实测 450-475s（差40×，墙钟被同步等待主导） | 已回写 skill：SKILL.md R32 索引行 + apply-orchestration.md §十二（R32a分波/R32b纯运行单后台化/R32c墙钟实测为准/R32d等待面预算） |
| 归属门外部违规处置 | 编排者 | 02.2 修复使 deliver.go 合法依赖 task.CallFrame（replay_identity 锚 G2/G9 落地）——golden 禁止边系 04.3 批次旧形态；核准同步边表（登记新合法依赖非放宽）：evolution/meditation allow 加 runtime/task、forbidden 移除、reason 补据 | 复跑归属门 exit 0 violations=0 |
| 合同/graph/自测锚同步（回写③遗留） | 编排者 | 合同 02.3 废弃锚 compaction_event_writer→rebuild_refolds_from_wal + EVID-04 两锚登记；graph counts 139/143；自测计数与 fixture 同步（93 OK）；变异注册表两 stale 锚重定（MUT-G1 driver.go 注释被 05.3 删/MUT-EVID-04 facts.go 段注释删）→自测 12 OK | 重锚后全量真跑：10/10 killed pending空 gate.ok=true |
| EVID-05 修复链（AA受阻→AC三轴→AD收口） | 三单接力 | AA：受阻于 03.6 代理断言与 fix_obligation 构造互斥（实测钉死）；裁 A（键集断言）→AC：恢复轴（restoreFoldForm 同形重折+零写入）+预算轴（通知卡片）+MUT-EVID-05，但暴露两 BLOCKER：键集断言在回合后观测点与在线规则不可辨（keep_recent 重铸是规则自身）+256 真根因=intake.cursor 位置游标被折叠重写截断（决定性实验：游标重置→256 全 121 代+重启绿 601s）；裁 A1（观测点移恢复边界）+扩权（loop.go/facts.go）→AD：栅栏身份化（每 drive 由 NoticeBoundary 身份解析+intake.read 身份去重）+03.6 A1 断言+全绿：256/seed1 全绿 604s、全族回归绿、变异 11/11 | EVID-05 complete；02.3/01.x 不回退；03.6/03.11 解锁 |
| 尾巴单（AE/AF/AG）+05.2 | 并行快单 | AE：concept budget S2 具名登记+package-layout 精确豁免；AF：F1 预检（48 tracked/95 untracked/8红旗——2条为并行 AE 时序错位非越界，docx/zip 为用户资料不提交）；AG：lint 三尾巴清零（comment 6→0+gen_godoc 两篇）——**lint.sh 全绿 LINT_EXIT=0**（F2 阻塞解除）；05.2：required 完整化+9新自测+拒收演练 6/6，bundle 门正确拒部分证据 | AG 两处写集外纯注释改动追认（smoke_test.go 在 graph 写集内、package_layout.go 规划 gap）；05.1/05.2/05.3 勾选等证据 |
| **长跑波（R32b 后台运行中）** | 后台 Bash ×2 | 03.11 六组合+稳定性×2（v2 evidence 格式，02:24 起，预估 ~84min）；04.2 生产验收（short 十轮+long 抽片+mutations，02:22 起，预估 ~45min）；首次后台全因 shell 重定向目标子目录未建而秒败（exit=1 全軍）——修正 mkdir -p 后重发；一次 compinit 吞首字符用首行垫 ':' 解决 | 后台完成后：03.11/04.2 勾选→05.1/05.2 bundle+勾选→05.3→05.4→F1-F3 |
| **复杂度误估教训（用户纠偏，后续派发纪律）** | 合单A复盘 | 三叶各自均为会话级体量；01.8归因发现实为**从零实现持久化写侧**（persist-before-live/Seq分配/四文件失败传播）仍与两补差捆单；过程触发 R15 整文件覆盖事故（3026→654行，靠本地历史恢复）；战报偏离申报与实物不符（声称新增 RecordConsumption 导出，grep 实证零命中） | **新纪律**：①一叶一单为默认；②合单仅限轻量纯测试叶且逐叶先估体量；③“验接线”类叶归因发现是“从零实现”立即停报拆单；④重叶绝不合单；⑤战报偏离必须 grep 实证再入账 |
| 合单A结果 | 01.2/01.3/01.8 | 01.2：先交接后确认（Consume 移 Backflow 后，回执失败不确认保持 owed），handover 红转绿+十轮；01.3：无投影链改活账本读通知（三态窗口全达），notice_input 红转绿+十轮；01.8：归因=写侧缺失唯一缺口，实现 persist-before-live+失败传播（invoke/tools/run/facts）+索引欠账重提，6/6 红转绿+十轮；全族回归 host61/exec8/history35 全绿（编排者抽验复确认） | 01.2/01.3 已复勾，01.8 已勾；01.8 候选外 invoke/tools 改动已申报归因；01.10 停报：五态归因完成，状态事件生产需新 public API+根接线（选项A）或缩验收面（B）或拆 01.10a/b（C），建议A待裁 |
| 计划回写（用户裁定 03 域细化） | 03 域四件套、evals-harness spec、task-graph、coverage、ORCHESTRATION、根 tasks、跨域引用、04.1 合同/自测 | 03 域按“单文件单机制单测试”拆为 11 叶（03.1—03.11）；25 父/112 绿锚/116 全锚/118 delta；70 节点/132 边拓扑通过；04.1 已勾选保持，其合同 tests 与自测计数同步属验收面清单同步（不是重开）；旧 03.3 monolith 代码保留、标记 `superseded`，待实施单拆为 03.4—03.8 五父测试 | 合同检查 exit 0、自测 64 tests OK；产品/测试代码未改（仅合同与 checker 计数） |
| 计划回写②（01.5/02.5 拆分，用户裁定全域粒度复审） | 01/02 域四件套、task-graph、coverage、ORCHESTRATION、根 tasks/design/proposal、03/04/05 引用、findings、orchestration spec、golden 合同与 checker 自测 | 01 域 5→7 叶（01.5 根装配 / 01.6 微信装配 / 01.7 CLI 装配）；02 域 6→7 叶（02.5 根审批 / 02.6 微信审批 / 02.7 采集）；总 38 叶；父测试仍 25（24 green+1 diagnostic）、锚仍 112/116，仅 leaf 归属与 contract_id 前缀变化；76 节点/139 边拓扑通过（Kahn 实测无环）；04.4 无 02.6 依赖可改（该叶 depends_on 不含 02.x） | 合同检查 exit 0、自测 64 tests OK；产品/测试代码未改（仅合同与 checker 计数同步）；勾选仍 9 |
| 计划回写③（用户评审 v3） | 01 域四件套与两份新 delta spec、00.3 冻结件、根 design/proposal/tasks、findings、graph、coverage、ORCHESTRATION、golden 合同、checker 与自测计数、03/04/05 引用 | 01 域 7→10 叶（01.8 持久化与实时召回 / 01.9 冷启动恢复与后续使用 / 01.10 外部执行资源生命周期）；01.2/01.3 补差重开并回退勾选（31→29）；00.3 增补 `gate_rulings.OBSERVER_FAILURE_SEMANTICS`；04.4 登记三个后续变异反例；另立三项写入 findings 与根 proposal Impact；总 41 叶；28 父（27 green+1 diagnostic）、132/136 锚、136 delta；82 节点/151 边拓扑通过（Kahn 实测无环）；04.2 `--test-id` 集不变（新测试由各自叶十轮与 03.11/05 消费） | 合同检查 exit 0、evidence 自测 93 OK、plan 自测 43 OK；变异自测 2 red（EVID-04 待 04.4 补变异，属已知后续项）；产品/测试代码未改（仅合同与 checker 计数同步）；勾选 29 |
| 计划回写④（用户裁定 C：01.10 拆两叶分批） | 01 域四件套与 `execution-resource-lifecycle` spec、00.3 冻结件、根 tasks/design/proposal/findings、graph、golden 合同、checker/自测/变异锚点、coverage、ORCHESTRATION、03/04 引用 | 01 域 10→11 叶：01.10a 执行资源查询面（`tool/exec` 包内测试）/ 01.10b 执行状态事件生产与通知接线（execution 测试 + 根装配）；总 42 叶；29 父（28 green+1 diagnostic）、137/141 锚、140 delta；84 节点/154 边拓扑通过（Kahn 实测无环）；勾选保持 32（未勾 10） | 01.10 停报处置：五态归因与泄漏实证（`evidence/01.10-summary.json`）保留为两叶共同输入，归档为两叶共引；未采用选项 A 强行实施，按 C 分批——01.10a 仅依赖 00.3 可先行，01.10b 等 01.10a 且新 public API 须先在实施单列最小面；合同检查 exit 0、evidence 自测 93 OK、plan 43 OK、runner 30 OK、architecture 41 OK、mutations 12 OK；产品/测试代码未改（仅合同与 checker 计数同步） |
| **最终收口（单AH，自含轮询）** | 03.11/1.4/F1 勾选；05 域与 F2/F3 分栏 | ①catchup2 收账：1-32768 exit0 799.2s、7-32768 exit0 877.6s、1-256 exit1（`duplicate=[SMOKE-R10496 x2]`，红证留 `03.11-v2/red-runs/seed-1-256-catchup2`）；②六组合齐：1-256/1-32768/7-256/7-32768/42-256/42-32768 全 `verdict=match`，6×1200=**7200 输入**、6×120=**720 重启**，6 个唯一 run_id、单 digest `9f4e50fe…`；1-256 受控重跑 1 次 match（`bb2de6ec…`、538s、指纹 `4a913817…` 前后一致）；③stability orig/stab2/stab3 三连绿（599.9/600.2/571.3s）；④`evidence/03.11-summary.json`；⑤lint 复跑 exit0（04\.2 转义修复后 leaves 39/42、errors []）；⑥bundle 门 17 份证据（6 long+10 short+1 mutation）**仅 1 条 error**：`bundle mixes 2 different baseline module fingerprints`（short 3 模块 vs long 1 模块）——叶外 scripts 缺陷，见 `evidence/05.2-bundle-gate-red.json`；⑦workflow 结构 39 tests OK、checker 自测 93 OK（fixture 复用同组指纹，未覆盖混合 scope）；⑧CI long 冻结 `--timeout 600`/`timeout-minutes 12` vs 实测分片 533—878s（4/6 超 600s、3/6 顶破 12min）——05.1/05.2 预算失准，见 `03.11-summary.json.ci_budget`；⑨F1 重采样 147 行/8 红旗全归因/边界 6 项净（`evidence/F1-recheck.json`）已勾；⑩F3 独立验收判『重开』（`evidence/F3-acceptance.json`，V1—V7），不勾 | 勾选 39/42 叶（本轮新增 03.11、1.4、F1）；**05.2/05.3/05.4/1.6/F2/F3 因两条叶外缺陷（bundle 指纹口径、CI long 预算）未勾**；F4 无授权未勾；稳定性 10×6 缺 57 次、05 域无 change 内 evidence 登记为债务 |
| **终收口（单AI）** | 05.2/05.3 勾选；05.4 补丁叶与独立验收 | ①bundle 指纹口径：`check_bundle_module_fingerprints` 按 module_path 求交集（共享模块 digest/revision 必须一致，非交集=scope 差异）；fixture 改 short 3 模块/long 1 共享模块，新增 `test_scope_specific_module_sets_are_not_baseline_drift`（共享 digest/revision 漂移必红）；checker 自测 94 OK；17 份证据 bundle 门 ok=true/errors=[]；负例（改写 wechat-bot digest）exit 1 具名报错。②CI 预算：long `--timeout 1200`、job `timeout-minutes 25`、preflight 同步；workflow 自测 39 OK；注释改 533/799/623/792/530/878s 实测口径。③05.2/05.3 已勾；四篇文档状态句转『已落地（L1）』，doc budget 206/21934 持平，lint exit 0。④05.4 批：lint/bundle/三模块 build+vet+test/openspec/doc budget/unittest 全 0（unittest 227 OK），**race exit 1**——2 处 DATA RACE 的 top frame 全在 `trpc-mcp-go@v0.0.10/streamable_client.go`（setSessionID/send/connectGetSSE/handleGetSSEEvents/handleSSEResponse），无 tagent-owned；基线 f900ca2 副本连跑 5/5 红、工作树 8/8 红、`race_check.sh ./tests/integration/capabilities/...` 同红；系 05.1 按 D1 扩 race 集合后暴露的既有上游竞态，非本 change 引入。⑤F3 更新：V1/V2/V7 置 resolved，新增 V8（race），verdict 仍『重开』。证据：`evidence/05.2-summary.json`、`05.3-summary.json`、`05.4-summary.json`；原始日志 `${TMPDIR}/event-consumption-integrity/{05.2-final,05.4}/` | 勾选 41/42 叶（新增 05.2/05.3）；**05.4/1.6/F2/F3 因 V8 待裁未勾**；建议 A：`RACE_WAIVER_SIGNATURES` 增 `trpc-mcp-go.*streamable_client\.go` + LEDGER U4（移除条件=升级到含上游修复版本），或 B：升级 trpc-mcp-go；05.4 字面清单的 short 十轮/long 六片/mutations/preflight 未重跑（bundle 复用相同 .go 树的 05.1/03.11 证据），已申报 |
| **终收口（单AK）** | waiver 登记（编排者裁定选项 A）、05.4 字面全量重跑、1.6/F2/F3 收尾 | ①waiver：`RACE_WAIVER_SIGNATURES` 增 `trpc-mcp-go.*streamableHTTPClientTransport` + `trpc-mcp-go.*streamable_client\.go`（符号行与路径行各一条；单路径签名会漏不含文件名的符号行，探针已实测 exit 1），LEDGER 记 **U5**（U4 已被 BeforeModel 回调时序占用；移除条件=升级到含上游修复版本），`scripts/test_race_check.sh` 增 3 例（waived 正例 / waived+plain failure 仍红 / server 侧帧未登记仍红），15 例全 PASS；真实旧红日志回放 WAIVED exit 0。②05.4 字面 24 条命令真跑全 exit 0（05:01:10→06:40:02，98.9min）：race 05.4 集合 WAIVED 3 races、`ci.yml` 精确集合 WAIVED 2 races；short 10/10 且 10 个唯一 run_id；long 六片全 match（530.97/706.77/531.39/706.76/526.32/706.00s，各 1200 输入/120 重启，合计 7200/720）；mutations killed 11/11、failures=[]；三模块 build/vet/test 9/9；openspec OK、doc budget 206 files/21934 lines 持平、unittest 227 OK；`--bundle` 17 份证据（10+6+1）ok=true/errors=[]；三负锚演练（删片/改名/复用 run_id）全 rc=1 且具名报错。③preflight 唯一红片（seed=42/window=32768，`input identity does not reconcile: missing=[] duplicate=[SMOKE-R421096 x2]`）逐层定性：gen108 报告 inputs 采样仅 1 条、outbound 仅 1 组 `delegate ack`+`settle-ack`、provider 多出 1 次同 last-user-text 入口调用（messages 12→14，发生在 restart_point 代），红 `input_requests=1202` = 绿 `requests=1201` + 1；受控重跑 verdict=match（run_id `01b2b7ab…`，657.18s），红证保留（`preflight/event-long/seed-42-window-32768`），1 红/13 次分片执行。④F3：V8→resolved、新增 V9（info）、verdict 『达标』。 | 勾选 42/42 叶 + 1.6 + F2 + F3；**唯一未勾为 F4**（提交/推送、远端 MR、required 与归档均待授权）。证据：`evidence/05.4-summary.json`（终版，blocked 版 sha256 `20f26594…` 备份于 `${TMPDIR}/event-consumption-integrity/05.4-final/05.4-summary.blocked-2026-10-25.json`）、`evidence/F3-acceptance.json`；原始日志与逐条退出码 `${TMPDIR}/event-consumption-integrity/05.4-final/` |
| 外部动作（单AL） | 提交/推送/远端/归档 | 主规格合并：一级 `specs/orchestration` 4 requirement 由 CLI 归档时应用，17 份嵌套 delta（34 ADDED / 4 MODIFIED，含 2 个新 capability `session-fact-lifecycle`/`execution-resource-lifecycle`）手工按 delta 语义并入 —— `openspec validate --specs --strict` 126/126；`openspec archive event-consumption-integrity`（未用 `--skip-specs`）整目录 86 文件移入 `openspec/changes/archive/2026-10-25-event-consumption-integrity/`（含 `changes/`、`evidence/`、`ORCHESTRATION.md`、`task-graph.json`、`coverage.md`、`findings.md`）；归档后 strict 复跑仍 126/126，`check-openspec.sh` 报 “OK: all main specs pass”；归档引动三处引用改指归档路径（`check_event_integrity_mutations.py` 冻结合同、`golden/runtime/event_integrity_scenarios.json` 的 graph_ref/doc_ref、`golden/runtime/architecture.json` 的 frozen_by）与两处测试读数（`scripts/test_check_runtime_evidence.py`） | 归档与主规格同步完成，F4 已勾；提交/推送与 CI 结果见交付终报 |

### 规划校验复跑入口

以下命令在tagent根目录执行，已分别取得退出码0；嵌套校验使用当前CLI支持的change子命令，只产生弃用提示，不修改主规格或临时复制目录。

```sh
openspec validate event-consumption-integrity --strict --json --no-interactive
openspec status --change event-consumption-integrity --json
openspec change validate event-consumption-integrity/changes/00-contract-evidence --strict --json --no-interactive
openspec change validate event-consumption-integrity/changes/01-runtime-consumption --strict --json --no-interactive
openspec change validate event-consumption-integrity/changes/02-event-consumers --strict --json --no-interactive
openspec change validate event-consumption-integrity/changes/03-formal-simulation --strict --json --no-interactive
openspec change validate event-consumption-integrity/changes/04-regression-gates --strict --json --no-interactive
openspec change validate event-consumption-integrity/changes/05-mr-integration --strict --json --no-interactive
```

JSON从leaves重新构建ready/verified图后拓扑排序，输出`nodes 84 edges 154`；逐叶状态/测试注册/去重锚校验输出`leaves 42; planned 42; parent tests 29; named anchors 141`。计数和拓扑复跑命令见coverage，不依赖graph里缓存的acyclic字段作为证明。

最终Git核验：HEAD仍为基线；本轮计划回写④只改 `openspec/changes/event-consumption-integrity/` 规划物与 00.3 冻结件，外加 04.1 合同 `golden/runtime/event_integrity_scenarios.json`、`scripts/check_runtime_evidence.py`、`scripts/check_runtime_plan.py`、`scripts/runtime_checklib.py`、`scripts/test_check_runtime_evidence.py`、`scripts/check_event_integrity_mutations.py` 的计数/锚点同步（属 04.1/04.4 验收面清单同步，不重开）；工作树其余已跟踪修改与未跟踪测试来自既有实施波次，本轮未触碰。不以 git diff 为空推断无新增。规划文件已读回；未提交、未推送。
