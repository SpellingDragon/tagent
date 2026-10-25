# 03 正式装配模拟：设计

> 本设计只描述未来实施的技术取舍与精确落点；`TestEventIntegrity*` 为拟新增测试（03.1 台架已实施并勾选），可复用 fixture 已存在并按需扩展；除 03.1 外文中命令均未运行、未通过。
>
> **拆分裁定（用户 2026-10-23，本设计据此重写）**：叶粒度 = 单文件 / 单机制 / 单测试。原 03.2 拆为身份矩阵（03.2）与决策流矩阵（03.3）；原 03.3 控制 monolith 拆为审批/反思/折叠/替换/采集五个父测试（03.4—03.8）；原 03.4 重启顺延为 03.9；原 03.5 拆为只读校准（03.10）与长程执行（03.11）。每叶恰好一个父测试、三个具名锚（`positive`/`negative`/`consumer`）。

## 现状与约束（只读核对）

- 根 fixture `tests/integration/host/scripted_provider_test.go` 的 `scriptedProvider` 已有 `answer func(index int) string` hook：按调用序号返回不同应答（例如 `model_parameters_test.go`、`host_wiring_test.go` 已用 `provider.answer = func(index int) ...` 做多决策），并记录每个请求体与模型名。它**并非单一终局**；仍缺的是按实际请求内容/身份选择、记录型 sender、模型/工具屏障与可确认排空点。
- 同文件族的 `recordingSender` 目前只实现 `Send` 并返回 nil，不记录动作；03.1 已扩展为记录身份、目标、正文摘要、结果与确认状态，测试只读。
- 微信 smoke 台架 `examples/wechat-bot/smoke_harness_test.go` 已装配真实宿主（`hostRuntime`/`SessionRouter`/`SeenStore`/`internalInbox`/`approvalGateway`/`hostSender`/`registerHandlers`）并把模型换成本地 scripted HTTP、出站换成本地记录通道；`smoke_test.go` 固定 3 个会话（`smoke-chat-a/b/c`）、300 轮、在第 200 轮做一次宿主重建。这些是扩展起点，不是本次验收面。
- 旧 03.3 monolith `TestEventIntegrityControls`（`smoke_test.go`）已十轮绿；拆分后代码保留但标记 `superseded`，不再进入 event 合同与 MR 选择器（详见 D9）。
- 子进程恢复已有可复用模式：`tests/integration/host/soak_continuity_test.go` 用环境变量让子进程按 mode 启动，父进程只中断自己创建的子进程并读取子进程 JSONL 事件。03.9 复用该模式，不新建第二套进程管理器。
- `tests/integration/` 树在 `scripts/codetools/package_layout.go` 的 `mirrorExemptTrees` 内；`examples/wechat-bot/smoke_*.go` 只在 `mirrorExemptFiles` 精确名单内。新增微信测试文件会触发 test-mirror 门，因此 03.4—03.8、03.10、03.11 必须落既有 smoke 文件。
- 02.3 的窗口语义是回放 `MaxTokens`：按每 token 四字符、阈值 0.3 计算，256/32768 对应 307/39321 字节；断言取实际触发结果，不沿用固定 2400 字节说明。

## D1 正式装配 + 本地替身，不复制产品状态机

- 根场景从 `Run.Invoke`/`Host.Invoke` / 公开 Boot 入口启动，微信场景从宿主配置、应用 options、Boot、渠道绑定和 `registerHandlers` 启动；HTTP `/task` 在微信模块内验证。
- 模型协议用本地 OpenAI 兼容 provider，渠道出站用记录型 sender；外部工具服务用本地 handler。内部一律使用正式装配与正式消费者。
- 测试只**观测**产品产生的请求、任务、义务、发送动作与恢复结果；不得在测试里补产品缺失的 observer/mailbox/approval/capture 接线。
- 被否方案：另建一套“模拟产品状态机”或平行评估框架——会与被测产品漂移，且无法证明正式入口。

## D2 观测协议：在既有 answer hook 上扩展

- 保留 `scriptedProvider.answer(index int)` 的按调用序号多决策能力；新增 `answerFor(request, index, identity)` 形式的决策入口（或等价的内部选择函数），按“实际请求内容 + 调用序号 + 身份”选择下一步响应；未预期请求立即 `t.Fatalf`，不以默认应答吞掉。
- sender 记录每个实际动作的身份、目标、正文摘要、结果与确认状态；测试只读，不回写产品状态。现有 `recordingSender` 只吞发送，不记录，不能直接冒充记录型 sender。
- 模型/工具屏障用 channel 控制时序；每个用例有可确认的排空点（关闭前停止测试生产者、释放屏障、等待正式义务收敛），不以 `Host.Close` 返回 nil 或固定 `time.Sleep` 代替。
- 微信侧同一台架：`smoke_harness_test.go` 的 scripted HTTP 与记录通道是唯一替身面；控制事件消费者（审批/反思/折叠/替换/采集）的观测都从正式装配产生，测试只在旁读取。
- 被否方案：用 sleep 后“暂时没有新消息”证明没有重复；用提前 Close/Run 取消掉潜在重复来证明无重复；把现有 `answer(index)` 说成单一终局。

## D3 叶粒度与锚结构（拆分后的统一约束）

| # | 叶 | 父测试 | 宿主/文件 | 机制（单文件单机制） | 三锚 |
|---|---|---|---|---|---|
| 03.1 | 台架 | `TestEventIntegrityHarness` | 根 `scripted_provider_test.go` 等 fixture | 决策/观测/屏障/排空点 | `positive`/`negative`/`consumer` |
| 03.2 | 输入身份与会话矩阵 | `TestEventIntegrityIdentityMatrix` | 根 `tests/integration/host/event_integrity_test.go` | 身份去重与重送幂等、会话隔离 | 同上 |
| 03.3 | 多决策流矩阵 | `TestEventIntegrityDecisionFlowMatrix` | 同上 | `send` 后继续决策、忙/闲续转、工具与子代理交错 | 同上 |
| 03.4 | 审批控制 | `TestEventIntegrityApprovalControl` | 微信 `smoke_test.go`/`smoke_harness_test.go` | 审批门到执行一次 | 同上 |
| 03.5 | 反思回流控制 | `TestEventIntegrityReflectionDeliveryControl` | 同上 | 回流投递与新颖门 | 同上 |
| 03.6 | 压缩投影控制 | `TestEventIntegrityFoldControl` | 同上 | 折叠产物与请求装配 | 同上 |
| 03.7 | 替换控制 | `TestEventIntegrityReplacementControl` | 同上 | 模型 lease 版本 | 同上 |
| 03.8 | 采集控制 | `TestEventIntegrityCaptureControl` | 同上 | 记录/seal 完整性 | 同上 |
| 03.9 | 独立进程重启矩阵 | `TestEventIntegrityRestart` | 根 `tests/integration/history/event_integrity_test.go` | 提交/领取/确认边界重启 | 同上 |
| 03.10 | 长程校准（只读） | `TestEventIntegrityLongRunCalibration` | 微信 `smoke_test.go` | 六项成本测量（一次性） | 同上（不进 event 合同） |
| 03.11 | 长程执行 | `TestEventIntegrityLongRun` | 同上 | 共享 Host 六组合矩阵 | 同上 |

- 每叶的 `positive`/`negative`/`consumer` 共享同一次父测试执行负载：十轮重复父测试，不按三锚乘为三倍矩阵；父测试通过不能替代子场景。
- 锚命名固定为 `positive`/`negative`/`consumer`（与 03.1 已实施面一致），机制细节写入任务行与 `verification.scenario_notes`，不再用“一个大 positive 覆盖五机制”的写法。
- 被否方案：保留单一 `TestEventIntegrityControls` 覆盖五机制——无法单机制失败定位、无法按机制派发、无法给单机制设负锚；被否方案：为每机制再建一个微信测试文件——触发 test-mirror 门且违反“不新增 smoke 文件”。

## D4 输入身份与会话矩阵（03.2，根模块）

- 用例：`TestEventIntegrityIdentityMatrix`，落 `tests/integration/host/event_integrity_test.go`，固定子测试 `positive`/`negative`/`consumer`。
- `positive`：同正文不同身份两条输入均保留（不按正文去重）；同身份重送只消费一次（幂等）；两个会话的通知与动作各归其身份、互不串。
- `negative`：按正文去重、跨会话串身份、重送二次消费、身份/目标错配均失败；不以“父测试通过”推断子场景已跑。
- `consumer`：根公开入口（`Run.Invoke`/`Host.Invoke`/Boot 装配）驱动，观测来自实际请求与发送记录。
- 文件归属：该文件由 00.2 创建，01.3/01.4/01.5/02.2/02.4 扩展；锁 `host-event-integrity-test`，由当时 ready 的唯一写者取锁单写，每次取锁先读回当前文件。
- 被否方案：把身份矩阵并进决策流矩阵——两机制失败面不同，合一会让 identity 回归被 decision 噪声掩盖。

## D5 多决策流矩阵（03.3，根模块）

- 用例：`TestEventIntegrityDecisionFlowMatrix`，同一文件；与 03.2 同文件，锁序 03.2 → 03.3。
- `positive`：`send` 后原 invocation 继续决策（不新建 invocation）；忙时通知保留、空闲后续转；工具与子代理通知各自续转，两条合法动作均保留。
- `negative`：忙时丢通知、续转新建 invocation、同一通知二次生成输入、子代理通知丢结果均失败。
- `consumer`：根公开入口驱动，动作与请求由实际观测派生，不复制期望值。
- 被否方案：把决策流与身份矩阵放同一父测试（原 `TestEventIntegrityConversation`）——忙时丢通知与身份串扰无法分栏定位。

## D6 微信控制事件五父测试（03.4—03.8）

同一正式微信宿主编排（配置 → Boot → 渠道绑定 → `registerHandlers` → HTTP `/task`）中，五类控制机制各自独立父测试；同一宿主装配由 `smoke_harness_test.go` 提供，不复制第二套。

| 叶 | 父测试 | `positive`（机制成立） | `negative`（错误路径必须被拒） | `consumer`（正式消费者） |
|---|---|---|---|---|
| 03.4 | `TestEventIntegrityApprovalControl` | `held -> approved -> executed once`；批准后同 digest 不二次执行 | 错误裁决不驱动请求；过期/改参回应不沿用旧批准；审批人会话零出站 | `/task` 审批重试全链（`approvalGateway.Resolve` + `runApprovalCLI`） |
| 03.5 | `TestEventIntegrityReflectionDeliveryControl` | 回流落授权目标一次 | 越权目标具名拒绝可读；拒绝后合法路径仍通；回流不重置目标新颖度 | `deliver_summary -> InjectReflection -> 目标下一次请求` |
| 03.6 | `TestEventIntegrityFoldControl` | 256 窗口折叠实际触发、原文票据可达、综述进请求 | 折叠产物自激、重建再折叠、票据不可达 | `request.projectedSegment` 请求装配 |
| 03.7 | `TestEventIntegrityReplacementControl` | `/reload` 安装新版本、下一请求新版本、在途流保持开始版本 | 同版本重复安装、失败仍安装、在途请求版本漂移被接受 | `/reload -> /task` 正式 HTTP 链 |
| 03.8 | `TestEventIntegrityCaptureControl` | 记录数 = 实际请求数；seal 后 `dropped=0`/`in-flight=0` | 未发生请求产生记录、失败记成功、准入拒绝不入损失账目、seal 后在途 | `attempts.jsonl` 正式读取 |

- 每个父测试只写自己机制的子测试块；同文件单写锁 `wechat-smoke`，串行 03.4 → 03.5 → 03.6 → 03.7 → 03.8 → 03.10 → 03.11。
- 不改 `smoke_test.go` 既有 3 会话/300 轮/第 200 轮重建的既有期望；新机制只加新父测试。
- 被否方案：五机制共享一个 `positive` 子测试（原 monolith）——一个机制红会拖垮整叶，且无法给单机制写负锚；被否方案：新增 `event_integrity_test.go` 微信文件——需 test-mirror 例外，本次明确禁止。

## D7 独立进程重启矩阵（03.9，历史包）

- 用例：`TestEventIntegrityRestart`（原 03.4 内容与验收面不变，仅重编号），落 `tests/integration/history/event_integrity_test.go`。
- 中断边界：提交前、提交后、领取后、完成确认前、完成确认后；由父进程只中断自己创建的子进程，父子握手确认边界；同一临时 localfile 重启后由新进程 Boot，核对实际模型请求、正式义务状态与发送动作。
- `positive`：义务恢复、已确认动作不重做、未确认动作不丢；`negative`：重启后重复执行已确认动作、丢失未确认义务、跨边界误判均失败；`consumer`：新子进程的正式 Boot 与实际请求。
- 文件归属：该文件由 01.2 创建、02.3 扩展、03.9 追加；与 01.2/02.3 **同文件单写**，锁名 `history-event-integrity-test`，串行 `01.2 → 02.3 → 03.9`。
- 明确不宣称掉电恢复；不操作在用机器人、不使用真实数据目录。
- 被否方案：同进程 `Close`→`New` 冒充恢复；由测试直接写存储再读回。

## D8 长程校准（03.10）与长程执行（03.11）

### D8.1 校准固定为独立父测试（03.10）

- 用例：`TestEventIntegrityLongRunCalibration`（`smoke` 标签），与 03.11 同文件、同宿主装配；**一次性只读测量**，不是每 MR 绿测。
- 两个窗口（256/32768）各测一次单组合成本，六项：冷构建、热运行、120 次真实重启、`-race`、存储增长、峰值资源（CPU/内存/句柄）；产出仓库外 JSON 报告 + change 内 `log_ref`。
- 报告是 05.1 预算冻结与 `timeout-minutes` 的唯一输入；不另建第二份预算表。
- **为什么不进 event 合同**：event 合同的 `tests[]` 只有 `event-short`（10 轮独立）与 `event-long`（六组合各 1 次）两个 scope，任何登记测试都会被每 MR 执行；校准是一次性研究，登记进去会让 10 轮/六片矩阵重复付校准成本。因此 03.10 是 `non_product` 叶：父测试登记在 `task-graph.json` 的 `source_only_tests`（`off_contract`），不参与父测试/锚计数与选择器，三锚（`positive`/`negative`/`consumer`）记在叶的 `verification` 与任务行里。
- 被否方案：把校准并入 `TestEventIntegrityLongRun` 的模式参数化——同一父测试带 `-calibrate` 时语义分叉，且合同按父测试选测，无法只跑校准不跑矩阵；被否方案：给校准单独加第三个 scope——`EVENT_SCOPES` 是冻结常量，扩 scope 会改合同语义与 v1 兼容面。

### D8.2 长程矩阵（03.11）

冻结口径（不得放宽）：

- 每组合 **4 个会话共享同一个 Host**；每会话 300 个**不同业务输入**，共 **1200 输入/组合**。
- 每会话**轮流**安排 30 个重启触发点（四会话轮转分布），合计 **120 次真实进程重启/组合**；每次重启恢复全部四会话。初次启动不计重启；一次重启不得按四次计数。
- 矩阵：seed `1/7/42` × window `256/32768` = 6 组合；**单组合 1200 输入/120 重启，六组合合计 7200 输入/720 重启**。
- 冥想保持启用；以受控模型/工具屏障等待正式触发，时间上限只作故障保护；不得把 interval 改成 24h 消掉该消费者。持久化强度与 fsync 不得削弱。
- 输入计数按不同业务输入身份核算；模型请求、HTTP 运输重试、审批控制、初始化消息、渠道分片另账，不以调用总数充输入数。
- 窗口指回放 `MaxTokens`；256/32768 对应 307/39321 字节（每 token 四字符、阈值 0.3）。参数由合成测试配置明确给出，断言取实际触发结果。
- `negative`：多会话同时完成、存储拒写、模型/渠道替身失败、替换与压缩交错；最终待办必须排空或给出具名未确认，零静默丢弃。替换/折叠交错断言复用 03.6/03.7 已建立的机制观测（同簇串行锁序保证其先落地），但 `depends_on` 只登记 03.2/03.3/03.9/03.10（用户裁定），不新增硬边。
- `consumer`：微信正式宿主入口 + 本地 `/task`，全部身份逐项对账。
- 45 分钟超时用 03.10 校准值加余量替换后冻结；交付稳定性 10 轮 × 完整六组合（共 60 次独立 `-count=1`）单独登记、预算另计，不乘进每 MR 六组合矩阵。
- 被否方案：4 个独立 Host；“一次重启 × 4 会话 = 四次”计数；关闭冥想；削 fsync；减少输入/重启/seed/window；把单个 long 分片说成 7200/720。

## D9 精确文件与单写归属

拟新增：

- `tests/integration/host/event_integrity_test.go`：00.2 创建，01.3/01.4/01.5/02.2/02.4 扩展，03.2 追加 `TestEventIntegrityIdentityMatrix`、03.3 追加 `TestEventIntegrityDecisionFlowMatrix`；**非 03 独占**，锁 `host-event-integrity-test`，由当时 ready 的唯一写者取锁，03.2 → 03.3 串行。
- `tests/integration/history/event_integrity_test.go`：01.2 创建，02.3 扩展，03.9 追加 `TestEventIntegrityRestart`；锁 `history-event-integrity-test`，串行 `01.2 → 02.3 → 03.9`。
- `TestEventIntegrityHarness` 落 `tests/integration/host/scripted_provider_test.go`（03.1 已实施），写集还包括 `meditation_boot_test.go`、`public_runtime_entry_test.go` 的既有 Boot/公开入口夹具段。

拟扩展：

- `examples/wechat-bot/smoke_harness_test.go`（03.4—03.8/03.10/03.11）：审批网关观测、反思投递观测、折叠/请求装配观测、lease 版本观测、capture seal 观测、第 4 会话与重启循环、逐身份账、失败/存储拒写开关。
- `examples/wechat-bot/smoke_test.go`（03.4—03.8/03.10/03.11）：五个控制父测试、`TestEventIntegrityLongRunCalibration`、`TestEventIntegrityLongRun`；全部落此既有文件，不新建 `event_integrity_long_test.go`，不申请 test-mirror 例外。

已实施物对齐（superseded）：

- `TestEventIntegrityControls`（`smoke_test.go`，旧 03.3 monolith，十轮绿）**保留代码**，在 `task-graph.json` 的 `source_only_tests` 标记 `status=superseded` 并登记 `superseded_by` 五个新父测试；不进入 event 合同、`scopes` 选择器与父测试/锚计数；删除须另获授权。
- 04.1 已冻结合同随本次拆分同步 `tests[]`（移除 monolith、加入十个新父测试）与 `whole_chain` 归属；这是**验收面清单同步**，不是重开 04.1。

写入规则：

- **P1 文件锁**：03.1 写集为 `scripted_provider_test.go`、`meditation_boot_test.go`、`public_runtime_entry_test.go` 的 fixture/Boot 入口段；03.2/03.3 与 00.2/01.3/01.4/01.5/02.2/02.4 共享 host 文件，全部单写；03.9 与 01.2/02.3 共享 history 文件，全部单写；03.4—03.8、03.10、03.11 同属 smoke 文件簇（01.6 亦写 `smoke_harness_test.go`，入同簇单写），按 03.4 → 03.5 → 03.6 → 03.7 → 03.8 → 03.10 → 03.11 串行。
- **P2 包/模块并发**：03.2/03.3 与 03.9 虽分属 `tests/integration/host` 与 `tests/integration/history`，但同属根模块，共享根模块编译/测试闭包、测试辅助包与可能的包级变量；**“不同包”不是并行许可**。默认串行。确需并发时先做 W/R 实测：列出各自写集（文件、`t.TempDir` 根、回环端口、子进程 PID、共享 fixture）与读集（编译输入、被 import 的测试辅助、包级状态）；仅当 W∩W 与 W∩R 均为空才可同树并发，否则用 `git worktree`/副本树隔离，证据分开存放。P1 文件锁优先于 P2 包级并发。
- 根热点产品文件零改动；若发现必须新增产品接缝，停止并上报，不在测试里补接线。

## D10 风险与回退

| 风险 | 处置 |
|---|---|
| 长程成本超预算 | D8.1 校准（03.10）；先优化编译/证据收集；仍超则上报，不缩覆盖 |
| 重启边界不稳定/端口占用 | 每次重启用独立临时根与回环端口；父进程只杀自己创建的子进程；失败保真，不以重试吞掉 |
| 五父测试挤在同一 smoke 文件互相拖慢 | 同簇单写串行；每个父测试独立 selector，十轮按父测试计；不为并行拆新文件（test-mirror 门） |
| 第 4 会话改变既有 smoke 行为 | 既有 3 会话断言保持；新增会话只加 03.11 用例，不改旧期望 |
| 观测器复制期望值 | 观测由实际请求/发送/恢复记录派生；合同期望只用于检查器对账 |
| 测试补产品接线 | 发现正式入口缺端口时停止并报告，不在测试里补 |
| 同文件交叉写坏 | P1 锁 + 后写者先读回；同簇严格串行，不因“不同机制”拆并发 |
| 校准父测试被误加进每 MR 矩阵 | 03.10 为 `non_product` + `source_only_tests.off_contract`；合同 `tests[]` 不含它，父测试/锚计数不含它 |
| 旧 monolith 被误当验收面 | `source_only_tests.superseded` + `superseded_by` 五父测试；合同/coverage 双处注记；04.1 清单同步 |
| 夹具改动影响旧 smoke | 先跑旧 `TestLocalSmoke`/`TestSoakContinuity` 回归；夹具改动单独提交范围，失败回退测试侧改动 |
| 运行产物误入仓库 | 原始日志/证据用 `--out` 指向仓库外临时目录；摘要与 `log_ref` 记入根 `ORCHESTRATION.md` 台账，不另建每叶 summary 文件；不宣称未核验的目录已被忽略 |

回退：本域只改测试与夹具；回退即还原本域文件，不影响产品行为。旧 monolith 若需恢复为验收面，须回退 03.4—03.8 五父测试并重新登记合同，不得两套并存计入数量。
