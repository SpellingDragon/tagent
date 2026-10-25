# 03 正式装配模拟：多决策、共享 Host、控制消费者与真实重启

> 状态：规划回写（03 叶细化），未新增实施。`TestEventIntegrity*` 用例中仅 03.1 台架与旧 03.3 monolith 已存在；其余为**拟新增**。可复用 fixture **已存在**（`tests/integration/host/scripted_provider_test.go` 的 `answer(index)` 多决策 hook、`recordingSender` 发送替身、`meditation_boot_test.go`/`public_runtime_entry_test.go` 的 Boot/公开入口夹具、`soak_continuity_test.go` 的父子进程模式、`examples/wechat-bot/smoke_harness_test.go` 的正式宿主装配），本域按需扩展，不得把已有夹具误述为不存在。除 03.1 外文中命令均未运行，不得当作已通过证据。

## Why

现有组件级 mock 与单点测试只能证明局部行为，不能证明正式装配在多决策、并发会话、真实进程重启与长程混合负载下既不重复处理也不遗漏。现有微信 smoke 只覆盖 3 个会话、300 轮、单次宿主重建，无法承载本次冻结的 4 会话共享 Host、每会话 30 个重启触发点（合计 120 次真实重启/组合）与 3 seed × 2 window 的矩阵；根 fixture 的 `answer(index)` 已能按调用序号返回不同决策，但缺少按请求内容/身份选择、记录型 sender、模型/工具屏障与可确认排空点。

原 03.2/03.3 叶过粗（一个父测试覆盖多机制），导致单机制红无法定位、负锚无法按机制写、派发无法按戳集拆分。用户裁定按“单文件单机制单测试”重拆为 11 叶。

## What Changes

- **03.1 模拟基础（已实施并勾选）**：扩展根 fixture 面（`scripted_provider_test.go`、`meditation_boot_test.go`、`public_runtime_entry_test.go`）的 `scriptedProvider.answer` hook 与 Boot/公开入口夹具，增加按实际请求内容/身份选择、记录型 sender、模型/工具屏障、可确认排空点和只读原始观测；`TestEventIntegrityHarness` 已十轮绿。本域不改其验收面。
- **03.2 输入身份与会话矩阵**：根模块 `TestEventIntegrityIdentityMatrix`——同正文不同身份不误去重、同身份重送幂等、会话不串。落 `tests/integration/host/event_integrity_test.go`（与 00.2/01.3/01.4/01.5/02.2/02.4 共用，锁 `host-event-integrity-test`）。
- **03.3 多决策流矩阵**：同一文件 `TestEventIntegrityDecisionFlowMatrix`——`send` 后继续决策、忙/闲续转交错、工具与子代理交错。与 03.2 同文件，串行 03.2 → 03.3。
- **03.4 审批控制**：微信 `TestEventIntegrityApprovalControl`——`held -> approved -> executed once`、错误裁决不驱动请求、审批人会话零出站、无会话 `/task` 503；消费者为 `/task` 审批重试全链。
- **03.5 反思回流控制**：`TestEventIntegrityReflectionDeliveryControl`——回流落授权目标一次、越权目标具名拒绝可读、拒绝后合法路径仍通、回流不重入新颖门；消费者为 reflect 链。
- **03.6 压缩投影控制**：`TestEventIntegrityFoldControl`——小窗折叠、原文票据可达、综述进请求、折叠产物不自激；消费者为请求装配。
- **03.7 替换控制**：`TestEventIntegrityReplacementControl`——`/reload` 安装新版本、下一请求新版本、在途流保持开始版本；消费者为 `/reload -> /task`。
- **03.8 采集控制**：`TestEventIntegrityCaptureControl`——记录数 = 请求数、seal 完整（`dropped=0`/`in-flight=0`）；消费者为 `attempts.jsonl` 读取。
- **03.9 独立进程重启矩阵**：`tests/integration/history/event_integrity_test.go` 的 `TestEventIntegrityRestart`（原 03.4 内容不变，仅重编号），父子进程在提交前/后、领取后、完成确认前/后中断，同一临时 localfile 重启核对。该文件与 01.2/02.3 共用，全部单写。
- **03.10 长程校准（只读、一次性）**：微信 `TestEventIntegrityLongRunCalibration`，两窗口各测一次单组合成本，记录冷构建/热运行/120 重启/`-race`/存储增长/峰值资源六项并产出校准报告；`non_product` 叶，不进 event 合同与 MR 选择器。
- **03.11 长程执行**：微信 `TestEventIntegrityLongRun` 跑冻结六组合：4 会话共享同一 Host × 300 输入 = **1200 输入/组合**，每会话 30 个重启触发点 = **120 次真实重启/组合**；3 seed × 2 window = 6 组合，合计 **7200 输入/720 重启**。单分片口径 1200/120，不是 7200/720。交付稳定性 10 轮 × 六组合（60 次独立执行）另记，每 MR 仍六组合各一次。

## 边界与依赖

- **依赖（接口常数 vs 实现依赖）**：00.3 是**实现依赖**（契约冻结后才可判定正负断言语义）；01.2 是 03.9 的**实现依赖**；01.5 是 03.2—03.9/03.11 的**最终绿证前置**，不是开发前置；03.10 `verify_after=[]`（只读校准，不设正式绿证前置）；03.1 已就绪。03.9 复用 `tests/integration/host/soak_continuity_test.go` 既有子进程模式，不自造第二套。
- **叶级依赖**：03.2/03.3 ← 03.1+00.3；03.4—03.8 ← 03.1+00.3；03.9 ← 03.1+01.2；03.10 ← 03.1+00.3；03.11 ← 03.2、03.3、03.9、03.10。无环。
- **被依赖方**：04.4 故障变异复用本域正式测试（03.2/03.3/03.9）作为“杀死断言”的消费者；04.2 生产验收跑 03 的已就绪父测试；05.1 每 MR job 直接运行本域选择器并用 03.10 校准值冻结超时；F2/F3 的短程/长程证据来自本域。
- **接口面**：11 个父测试（03.10 不进 event 合同）与各自的 `positive`/`negative`/`consumer` 三锚；三锚共享同次执行负载、不按三锚乘三；01/02 的逐子场景清单由 04.1 合同登记；长程仅暴露 `-seed`/`-window`，校准仅暴露 `-window`，会话数/输入数/重启数/种子集合由合同冻结为常数，不提供缩减入口。测试夹具是 Go 测试包内部接口，不是产品 API。
- **已实施物**：旧 03.3 monolith `TestEventIntegrityControls` 保留代码、标记 `superseded`，由 03.4—03.8 五父测试取代；不删除、不两套并存计入数量。
- **写锁与并发**：03.2/03.3 与 03.9 虽分属不同包，但同属根模块，共享构建/测试闭包并分别与既有文件交叉，**不得据“不同包”判并行**；默认串行，确需并发先做 W/R 实测，否则隔离 worktree/副本树。03.4—03.8/03.10/03.11 同属 `wechat-smoke` 簇，严格串行。P1 文件写锁优先于 P2 包级并发。
- **禁止事项**：不复制产品状态机；不以 sleep 后“暂时没消息”作为无重复的唯一证据；不在测试里手工补产品缺失的观察者/邮箱/审批器/采集器；不改成 4 个独立 Host；不把一次重启按四次计数；不关闭冥想消费者；不削 fsync/持久化强度；不减少输入、重启、种子或窗口覆盖；不访问真实模型/微信；不为新测试文件申请 test-mirror 例外（控制/长程/校准全落现有 `smoke_test.go`）；不建立平行评估系统或第二份合同解析器；不按三锚乘三倍矩阵；不把交付稳定性缩为单组合 10 次；不使用单次 count 放大冒充十轮；不把校准塞进每 MR 矩阵。

## Capabilities

### Modified Capabilities

- `evals-harness`：新增正式装配确定性验收、五类控制事件消费者验收、共享 Host 真实重启长程矩阵与成本校准义务；delta 位于本域 `specs/evals-harness/spec.md`。不恢复旧 `evals/` 目录或旧框架。

## Impact

- 本域只改测试与夹具，不改产品代码；若发现必须新增产品接缝（例如可注入时钟），先停止并报告最小接缝需求，不偷偷加入仅测试用公共 API。
- 03.4—03.8/03.10/03.11 扩展既有 `examples/wechat-bot/smoke_test.go` 与 `smoke_harness_test.go`；新增根/历史测试文件位于 `tests/integration/` 的 mirror-exempt 树，不触发 test-mirror 门。
- 长程成本在校准前不冻结；附稿的 short 20m、long 45m、mutations 30m 仅为提议预算，不是已测结论；03.10 产出校准值后才写 CI 超时。
- 原始运行产物一律用 `--out` 指向仓库外临时目录；change 内只留证据摘要与 `log_ref`（统一记入根 `ORCHESTRATION.md` 台账，不另建每叶 summary 文件）；仓库内未核验忽略规则的目录不得被宣称为已被忽略，也不得把原始运行日志写进本 change 规划目录。
