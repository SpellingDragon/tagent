# 上游调研：实际依赖与停止确认边界

> 2026-09-23 整体审阅后修订。本地 main 不代表实际依赖或远端最新状态。下方 v1.10.0 Graph 调研仅保留为历史，不启用 saver、内部 activity 或独立图调度。
>
> 2026-09-24（轮三十）实际依赖来源更新：root 与独立 wechat-bot 两模块均 `require trpc.group/trpc-go/trpc-agent-go v1.11.2`，并显式 `replace` 到自有 fork tag **`github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`**（该 tag 为不移动的 annotated tag，peel 至 `3ac216807`＝官方 `v1.11.2`(`5a0030b62`) ＋四个 producer-done 流关闭契约提交）。核对方式：`go list -m trpc.group/trpc-go/trpc-agent-go` → `v1.11.2 => github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`；两模块的 trpc-agent-go replace 均不再指向本地检出；wechat-bot 对 tagent 的 `=> ../..` 同仓引用仍保留。其余 `trpc-agent-go/model/*`、`trpc-a2a-go` 等子模块仍取官方版本，未被本 fork 覆盖。

## 当前结论：具体 race 修复不等于执行完成

| 项 | v1.11.2 已确认能力 | 不可推导的结论 |
|---|---|---|
| U-1 | `session/session.go` 的 UpdateUserSession 在 EventMu 内更新 UpdatedAt，与 Clone 读锁一致 | 不因此要求改变父子 session 记录语义 |
| U-2 | #2165 避免复制 opaque invocation state；#2462 去掉相关 streaming telemetry clone | 不能证明所有生产者在处理后事件流关闭前已退出 |
| #1926 | steer queue cancel signal | 取消信号不是 join／实际完成凭证 |
| runner 取消路径 | 固定 v1.11.2 的 `runner/runner.go` 在 ctx.Done 后可退出事件循环并关闭 processedEventCh | 此处未等待 agentEventCh 生产者；尚不能据流关闭释放全部调用资源 |

## 实际停止能力门：实证判定（§6.3，官方 v1.11.2）

`agent/upstream_stop_capability_test.go::TestUpstreamStopGate_ProcessedCloseIsNotProducerDone` 以**一个取消后仍阻塞的真实生产者**（其产出协程停在测试持有的 gate 上、显式忽略 ctx.Done，模拟卡住的工具/模型流）确定性证伪命题：取消后 runner 唯一的公开完成信号 `processedEventCh` 关闭时，`producerDone` 仍为 false。即 **processed stream close ≠ producer done**（`-count=20 -race` 稳定通过，非统计推断，是结构断言 `require.False`）。

源码根因（静态核验同一结论）：`runEventLoop` 在 `case <-ctx.Done(): return`（runner.go:1533）后经 defer `close(loop.processedEventCh)`（:1503），不排空/不 join `agentEventCh`（:1511 生产者来自 `agent.RunWithPlugins`，:765，通道不外露）；`Close`→`cancelAllRuns` 仅 `cancel()`（:509-527）不等生产者；`safeEmitRunnerCompletion` 在同一非 join 的 defer 内触发；`internal/state/barrier` 是图状态位而非生产者 join。**判定：官方公开接口不足以覆盖完整生产→转发→清理链，§6.3 能力门不通过。**

因此 tagent 的资源释放门 MUST 由自身生产者记账承担（R02/4.1 逐代租约），不得挂到处理后流关闭上；若需让退役也覆盖框架内部生产者的真实停止，须走下方最小 fork 路径。**轮十八只完成本地、无需授权的实证判定；轮二十六完成 L1／L2（本地补丁与本地验收）；轮三十在用户授权 R1 后完成发布与钉版（详见 evidence 轮三十与 tasks 6.3 闭合注记）。**

补丁内容与验证（轮三十实际发布态，非规划）：fork 分支 `release/v1.11.2-tagent`（基线＝官方 v1.11.2，**不是** main）＋4 提交——`runner/runner.go` 取消分支在 `close(processedEventCh)` 前排空 `agentEventCh`（nil channel 时跳过排空）、`agent/run_with_plugins.go` 的 AfterAgent 转发层在关闭自身输出前排空内层源流、覆盖排空循环体的测试补充。上游侧对照证据：非 race 全量 220 ok／唯一失败为 `tool/duckduckgo` 的 macOS unix socket 环境失败；`-race` 失败集与 race 栈签名集相对纯 v1.11.2 基线**逐条相同且数量为子集**（6 失败 vs 7、8 race vs 9）。tagent 侧对 tag 重验：正向能力门 `TestUpstreamStopGate_ProcessedCloseImpliesProducerDone`（原证伪探针在补丁下反转为 PASS）＋全包 `-race` 零 race。

上述发布来源及历史实验保留。2026-09-25 口径校正：当前已使用 producer-done fork，6.6 已移除活动 broad/family 豁免，不再将它们写成待实施；历史 50／60 次 race 通过不是严格停止证明。fork 能力只覆盖其排空合同，tagent 自身的 owner 依赖、公共 Close 有界返回和迟后最终释放仍由 3.2／4.1／4.3 补齐。此次仅修工件，不重新执行 fork 开发、tag 发布或依赖替换。

## 历史执行流程：实际停止能力门与必要 fork

以下是已获授权并完成的 6.3 流程回链，不是本轮重新 fork／发布的任务；当前依赖和剩余边界以上方更新及 tasks 为准。

1. tasks 6.3 在官方固定版本建立可控生产者屏障：取消后消费者可以退出，但生产者未退出时其资源引用仍须保持。覆盖正常、错误、早停、任务 ACK 和清理尾部。
2. 若公开接口可以提供覆盖完整生产／转发／清理的完成信号，采用最小适配；不能满足则按用户已授权方向在 trpc-agent-go 建独立修复分支与修复案，不因旧 race 已修就取消生命周期工作。
3. 自有修复要有修改前失败、修改后通过的上游测试及 tagent 集成验证；不改变共享 session 语义，不无限等待公共取消接口，也不以 Close 处理后流作为 producer done。
4. 验证通过且有自有补丁时打独立、不移动的 SemVer tag（首选未占用 `v1.11.2-tagent.1`）；保持原 module path，由 root／wechat 两模块显式 replace 到指定 fork tag，核 `go list` 的实际来源。无自有补丁继续官方 tag，不伪造修复版本。**（轮三十已执行：tag 已发布并 push 至 fork，两模块 replace 已切至该 tag，`go list -m` 实证来源如上。）**
5. 远端账号／组织及发布权限仅在真正发布时确认。当前规划轮未 fork、改源码、提交或打 tag；外部发布不得由本轮文档更新推定授权。**（轮三十为用户显式授权 R1 后的实际发布，非文档推定；发布仅落在个人 fork，上游 origin 未被 push、未 force。仍独立待办：上游 PR #2637 合并与 `type/bug` 标签需维护者；上游若合并，后续应撤 replace 回归官方版本，归 5.4 交付面。）**

## 历史研究对象

以下表格及钉测针对当时 tagent 使用的 v1.10.0；本地副本的 ahead/behind 只是当时观测，不是当前远端版本结论。

## 已调研能力（不等于全部启用）

| 能力 | 上游位置（v1.10.0） | 结论 |
|---|---|---|
| StateGraph/条件边/入口/终点 | `graph/state_graph.go`、`graph/graph.go` | 覆盖配置图编译目标；无环校验、schema、Command 路由齐备 |
| Executor（BSP 默认/DAG 可选） | `graph/executor.go`（Engine 选项、MaxSteps/StepTimeout/NodeTimeout/MaxConcurrency） | 复用调度内核，不自研 |
| 节点回调 | `graph/callbacks.go`（Before/After/OnNodeError，After 可恢复节点错误） | 用于耐久执行门的三段式钩子 |
| 重试策略 | `graph/retry.go`（RetryPolicy、条件、退避、jitter） | 节点级重试复用；存储类重试仍由引擎层自管（阶段语义不同） |
| CheckpointSaver 接口 | `graph/checkpoint.go` L178–195（Get/GetTuple/List/Put/PutWrites/PutFull/DeleteLineage/Close） | 适配为事实链投影的稳定接缝；PutFull 注释即称原子保存 |
| checkpoint 模型 | `graph/checkpoint.go`（Checkpoint/InterruptState/PendingWrite/Tuple/Filter/树、fork/branch） | lineage/namespace/checkpoint_id 语义完整，适配层可直接映射 |
| interrupt/resume | `graph/interrupt.go`、executor resume 路径（RuntimeState `CfgKeyLineageID/CfgKeyCheckpointID/CfgKeyCheckpointNS`、ResumeCommand） | 覆盖人工审批/外部输入暂停续跑场景 |
| 跨进程示例 | 上游 `examples/graph/{checkpoint,interrupt,external_interrupt,nested_interrupt,diamond}` | in-memory/SQLite/Redis saver 仅为上游示例依赖，tagent 不引入 |

## 原耐久方案的实证缺口（1–3 补偿义务已撤销）

1. **checkpoint 保存失败不阻断执行**：`executor.go` 初始与每步 checkpoint 失败仅 Debug 日志（L478–493、L1572–1601）；上游测试 `TestExecutor_CheckpointSaveError_DoesNotStopRun`（`executor_checkpoint_test.go` L181）将该行为锁定为预期。→ 耐久执行门必须在包装层锁存失败并阻断后续节点。
2. **非显式恢复时读取失败静默从新开始**：`resumeOrInitWithSaver`（executor.go L546–618）仅在请求指定 checkpoint_id 时返回错误；未指定时 GetTuple 失败走 "starting fresh"。上游测试 `TestExecutor_NonResume_GetTupleError_DoesNotStopRun` 锁定。→ 适配层对非显式路径的读取失败必须显式分类（fresh 合法 vs 未知错误 fail-closed），不得把 I/O 错误当空历史。
3. **TaskID 跨恢复漂移**：TaskID 为 `fmt.Sprintf("%s-%d", nodeID, step)`（executor.go L2180 等），恢复重跑同节点即换 ID；不满足"稳定活动身份"要求。→ 引擎自管理活动身份（design D6），不依赖上游 TaskID 持久语义。
4. **无 YAML/配置图装载器**：`graph` 包无任何 yaml/LoadFrom 入口（grep 为空）。→ 配置编译层为 tagent 新增工作（design D3），沿用其现有 strictyaml 基建。

## 验证记录

- 实际执行：`go -C tagent test -mod=readonly trpc.group/trpc-go/trpc-agent-go/graph -run '^(TestExecutor_CheckpointSaveError_DoesNotStopRun|TestExecutor_NonResume_GetTupleError_DoesNotStopRun|TestExecutor_Resume_GetTupleError_ReturnsError|TestExecutor_Resume_AppliesPendingWrites)$'` → `ok ... 0.567s`（2026-09-22，本机 darwin/arm64，Go 1.24.1）。
- 上游模块版本核对：`go -C tagent list -m -json trpc.group/trpc-go/trpc-agent-go` → `v1.10.0`（2026-06-05）。
- 语义断言依据静态阅读 v1.10.0 源码（路径如上），未运行的真实场景（微信/真实模型/长跑）不在本调研范围。

## 对现行计划的影响

- 当前实际依赖为以官方 v1.11.2 为基线的 fork tag v1.11.2-tagent.1，能力判定与发布已完成；本轮不重做。后续撤 replace 必须重新验证完成凭证并获授权，不能静默降回缺凭证的版本。不自建 YAML→StateGraph 编译层。
- v1.10.0 具备条件边（AddConditionalEdges），“Graph 只能静态前向”不成立；不用独立图的原因是现有配置表达可调用关系而非执行流程，无须发明流程语义。
- 历史钉测/能力表仅作上游语义参考，不作为编排热更准出依据；不引入 SQLite/Redis saver 或内部事实 saver。
- 既有 runtime 的 F1–F10 为后续台账，不经 Graph 持久化内部阶段。
