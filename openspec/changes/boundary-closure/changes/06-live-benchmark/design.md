# 真实任务对照：技术要点

## 文件清单

- bench/tasks/*.json：12 任务夹具（输入、工具需求、验收器声明）
- bench/runners/{baseline,tagent,dsh}.go：三方 runner（同一任务接口）
- bench/metrics/schema.go：五指标 schema 与采集
- bench/report/：报告生成（对照表）
- bench/harness_test.go：harness 自测（scripted 模式）+ 真实模式开关
- **bench 根包（1.1 产出）**：任务定义、任务接口与机读验收器是**可被 import 的导出 API（非仅 `_test.go`）**，供 07 的 `features/rl/sample` 直接复用；**新导出类型（`Task`/`Verifier` 等）在 `golden/runtime/architecture.json` 的 `concept_bindings.bindings` 登记，绑定 `live-benchmark` spec 的 concept（F11）**
- **examples/wechat-bot/cmd/bench-bridge/**：本域新建微型 main（复用 wechat-bot 模块既有装配与工具面；行式 stdin 协议交付任务/回传结果与指标）
- **golden/runtime/architecture.json**：`bench/`、`bench/tasks/`、`bench/runners/`、`bench/metrics/`、`bench/report/` 登记（packages+edges；无 bench/bench）；若选 internal/bench 则补 edges

## 关键决策

- 任务可判定：每个任务带机读验收器（文件存在/内容匹配/命令输出匹配——与 07 共用验收器实现），成功判定不依赖人眼。
- 公平性：三方同模型同温度同 max_tokens 同工具面（等价工具集）；预算=02 的统一预算语义（基线以 trpc-agent-go 原生窗口机制为准并记账差异）。**工具面等价=能力映射**：任务声明能力集合→三方各自工具面覆盖，映射表登记为证（不是“大体相同”，也不要求工具名逐项相等）；由 `TestRunnersShareTaskInterface` 断言覆盖关系（**tagent 侧断言对象=`examples/wechat-bot/cmd/bench-bridge` 声明的工具面**）。
- **送达率口径（统一为"发送方渠道确认"）**：tagent 以 **`bench-bridge` 内渠道 Send 回执**为分子、DSH 以**发送 API 成功**为分子、最小基线以**任务终态**为分子（无渠道），成功任务为分母；报告 MUST 注明三者口径差，MUST NOT 把不同口径当同一指标比较。
- 基线定义：trpc-agent-go 最小循环（单会话、无事件链、无折叠）——衡量 tagent 机制的净增量，而非框架差。
- **tagent runner=子进程协议（r6 F2 定版）**：wechat-bot 是独立 module 的 main 包、根模块不可 import；runner 经子进程驱动本域新建的微型 main **`examples/wechat-bot/cmd/bench-bridge`**（复用 wechat-bot 模块既有装配与工具面；行式 stdin 协议交付任务、回传结果与指标；任务信封与指标回传由 runner 侧编解码），不虚构 wechat-bot 的不存在入口；协议测试=`TestRunnerProtocolSubprocess`（真实起该桥接子进程跑 1 个任务并回传结果）；工具面/预算等价性由桥接声明继承保证（断言进接口测试，断言对象=该桥接声明）。
- DSH 适配（用户裁决：**不分期**）：以 CLI/ACP 驱动其 agent（本机 checkout），任务经 stdin/文件交付；三方同轮出齐为硬要求——适配遇阻时两方结果先行产出但**收尾不豁免 DSH**，攻关并行，确不可行才作为 blocked 法定停点上报。
- 延迟口径（三行）：①首 token 延迟与完成延迟分开记；②**冷启动（每方首个请求）与热路径（后续请求）分列**；③token 成本按 API 计数（prompt+completion）。**人工介入次数口径=审批入口调用数＋blocked 上报数；采集点=runner 记录与 run 日志；metrics JSON 的该指标含 `breakdown` 对象（`approval_calls`/`blocked_reports`）与 `total`，分项和=总。** run 记录含 `status`（completed/skipped/blocked），报告按 status 分列。
- 每方每任务 3 次重复（每方 36 次 attempt），按 runner 汇总：成功率/送达率为比率、延迟为样本均值、token 与人工介入为总量；逐任务逐次明细全部保留可复核。**偏差如实登记**：原文案写"每任务取中位数后汇总"未落地——实现按 36 次 attempt 直接汇总，均值而非中位数；spec 未要求中位数，真实 run 已完成，重跑 640s 配额成本高、结论不改变，故修本文案对齐实现，中位数口径留作后续可选改进。
- scripted 自测：harness 先以 scripted 假模型自测全链路（不耗真实配额），真实 run 只在门开时跑。
- **DSH 测试安全门**：`TestDSHRunnerExecutesRealTask` 以环境门守护（本机 DSH checkout 路径存在+env 标记才跑；缺席=具名 skip）；CI 只跑接口/一致性测试（build tag 或 env 过滤），DSH 真跑凭据=本地输出+run-id 入台账，不靠 CI 绿冒充。
- **PASS 计数与三方齐备**：1.4 的 PASS 行数=实际执行的测试数（DSH 缺席 skip 时期望数随减，并在输出中计数 skip 行=1）；“三方齐备”硬断言由 1.5 的 `TestRunDirContainsAllThreeRunners` 承担。
- **架构 golden**：`bench/`、`bench/tasks/`、`bench/runners/`、`bench/metrics/`、`bench/report/` 登记 `golden/runtime/architecture.json`（packages+edges；无 bench/bench）是 1.1 的显式步骤；**同一文件登记 bench 新导出类型的 `concept_bindings`（Task/Verifier→`live-benchmark` concept；F11）**；product-surface 基线同步在 1.6。
- **显式欠账（F4）**：对照结果的**系统消费=L5 桥**（把 run 结论送进 agent/冥想消费面）属未来变更；本轮只产出 run+报告+可回读明细，不建消费面、不改产品运行时。

## 风险与回退

- 真实 API 波动：重复 3 次取中位+异常轮标记；成本上限（每方每任务预算顶）熔断。
- DSH 适配受阻：两方结果先出、DSH 攻关并行；不分期、不豁免，确不可行=blocked 停点上报用户。
- 回退：harness 独立目录，不影响产品；欠账（对照结果系统消费=L5 桥）显式入 ORCHESTRATION 台账。
