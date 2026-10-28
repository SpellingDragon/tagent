## Context

tagent 是单机长程 Agent 运行时：一条事实链、三个挂点（生产事实、请求管线、读链消费）、可回捞的上下文与可恢复任务。现行实现的真实形态与若干声明不一致，且若干目标能力只在组件层存在、没有从正式入口可用。

以下结论来自源码正文，不采信可能过时的文档与注释：

- `memory/ledger/localfile.go` 的 `writeShard` 每写一条记录就重写整个分片快照（WriteFile + Rename），没有 fsync。`kvCommit` 先改内存 map 再 flush；flush 失败后 dirty 仍保留，下一次任意 commit 会把这个"已失败"的值一并刷盘。
- `memory/ledger/tier_facts.go` 的 Put 内部已 kvCommit；`Sync` 传空 batch，不能提供独立持久屏障。`durable.go` 因此在 Put 后调用 Sync，实际重复刷盘且 Uncertain 分支在真实后端不可达。
- `runtime/state/durable.go` 已把任务变化与 Outbox 编入同一条 TaskRecord 并更新投影；单条日志记录即可承载，不需要先有多表事务。
- `assembly/facts.go` 先落事实再更新内存链；查询与向量更新失败与事实提交分离，派生面本就允许滞后。
- `assembly/host_delivery.go` 先 `sender.Send` 后落 sent 回执；本地日志或数据库都无法消除远端已发送、本地未记的回执窗口。
- `runtime/agent/loop.go` 构造 `model.Request` 时不设 GenerationConfig；`assembly/host_model.go` 的包装器在快照之后才补 temperature/thinking/reasoning_effort，故现有快照 `Parameters` 恒为 `{"stream":false}`。
- `features/rl/capture_wiring.go` 的请求快照只 `keep()` 进内存 map；sink 只接受 AttemptRecord，`RecordDecision`/`RecordEpisode` 在生产路径无调用者。
- 主路径 `Stream=false`；`api/model/terminal.go` 的 ReduceChannel 不累计 delta，正文来自 provider 聚合包；只发 partial 不发聚合包会落到 KindPartialOnly。
- `assembly/host.go` 用 `http.Client{Timeout: 10m}`（`host_document.go`）；流式下该超时覆盖整个响应体读取。
- openai-go 默认重试 2 次，发生在 `model.Request` 边界之下，attempt 记录看不到。
- `relations_journal.go` 已具备追加 + 每记录 Sync + 尾部残行截断 + 完整行损坏拒绝的形态，可作日志格式的参照，但其载荷写死、无校验和、无段滚。

项目经过大规模重构，README 与 OpenSpec 可能滞后。本设计以正式入口的实际调用链为准；文档差异作为待修项而非实现依据。文档与代码冲突时，先认定"声明失真"还是"行为缺陷"，不混为一件事。

### 已确认的取舍

| 决定 | 内容 | 代价 |
|---|---|---|
| D1 持久边界 | 关键恢复事实 Sync 后确认；派生投影允许滞后 | 关键提交等待刷盘 |
| D2 副作用恢复 | 普通消息自动补投、可重复；命令凭执行证据保守恢复 | 消息可能重发；部分命令需人工判断 |
| D3 数据采集 | 采集不阻断 Agent；不完整样本具名拒收 | 不保证每次调用都成为训练样本 |
| D4 流式范围 | 实时输出 + 最终结果可查询；不做无缝断线续流 | 断线重连只能读到当前状态或已完成结果 |

## Goals / Non-Goals

**Goals:**
- 让持久层名副其实：追加记录、完整前缀、可检测损坏、明确刷盘边界。
- 让请求快照等于实际交给 provider 的参数，并能跨重启读取。
- 让主路径产生实时增量，且不改变"唯一终态判定"。
- 让关键运行指标从正式入口可查，且不引入高基数标签。
- 让 SFT/DPO 导出读取落盘证据，并由真实 tokenizer/collator 验证可消费。
- 删除死路径与冗余写入，使维护面小于现状。

**Non-Goals:**
- 不引入 SQLite 或多表事务；不承诺跨账本原子提交。
- 不自建模型协议、不新增 provider、不合并 module、不重排插件闭集。
- 不做无缝续流、跨进程 chunk 回放、通用订阅平台。
- 不实现在线 RL、权重训练、多机或多租户。

## Decisions

### D1 追加日志替代整分片快照

- 复用 `relations_journal.go` 已验证的形态：`O_APPEND` 单 writer、每条记录一帧、写入后 Sync、启动时截掉最后一个换行之后的残尾、完整行不可解析即具名拒绝。
- 帧格式在 JSON 行之外增加长度与 CRC32C 校验（参照 LevelDB log format 的 checksum+length+type）。理由：换行分帧无法区分"尾部撕裂"与"中间一条恰好被撕成合法 JSON 的行"；后者若被接受就是事实静默消失。**这是新增工程，不是配置开关。**
- 日志保存两类记录：**事实记录**（事件本体）与**任务记录**（TaskRecord，含 Outbox，已有编码）。一条任务记录一次提交，不再拆成多次同分片覆盖写。
- 以单调递增的日志序号作为提交顺序，不再用时间戳或分片内 seq 推导次序；`durable.go` 的 revision 由日志位置承载，避免丢尾后 revision 回退导致互斥转移共用编号。
- `Sync` 只在真实屏障点调用一次：Put 内部不再 flush；确认语义由显式 `Sync` 提供。这修掉重复刷盘与 Uncertain 分支不可达。
- 失败写不进入内存可见状态：先写日志成功再更新内存索引；写失败即返回错误，不留待后续 flush 带出。

**备选与否决理由**
- *SQLite + `synchronous=FULL`*：能获得跨表原子与跨进程写权，但为一个只有两个真实写点的单机运行时引入 schema、迁移、驱动与连接管理；当前 Outbox 已与任务变化同记录，不需要多表原子。否决。
- *tidwall/wal*：具备段滚与单调索引，但引入外部依赖与自己的文件布局；当前只需单文件追加与一个重建索引，复用本仓既有形态更可控。暂不引入，若后续确认需要段滚再评估。
- *保留整分片快照*：写放大 O(历史总字节)，且无 fsync 时"确认成功"没有依据。否决。

### D2 Sync 边界与副作用恢复

- 关键恢复事实（已确认受理的输入、审批决定、外发意图与回执）在 Sync 返回成功后对外确认。
- 派生投影（查询表、向量、折叠产物）异步重建；失败只影响读面新鲜度，不回滚事实。
- 普通消息：先记 owed，再发送，成功后记 sent；回执未落时重启重发一次（与现行取向一致），并在诊断面报告"已发未记"。
- 命令类副作用：只有在可验证的执行证据（tmux 资源存活、输出文件、退出证据）成立时才接管；不能确认时标记 interrupted/unknown，不自动重跑。**不引入命令级幂等键推导或通用补偿事务。**

### D3 请求证据：先合并参数，再固定，再异步落盘

- 在 `host_model.go` 的 `decorate` 语义上增加一个可选窄接口 `EffectiveGenerationConfig()`，由 `hostParameterizedModel` 实现；`loop.go` 在构造 `model.Request` 后、`fixCapture` 前 type assert 填充。这样 `Parameters` 记录的是宿主默认生效后的真实值。
- 记录口径分三层，不得互相冒充：**loop 请求快照**（本次变更持久化的对象）、**provider 序列化 body**（仅 RoundTripper 可观察，本变更不承诺）、**服务端实际采样**（不可观察）。
- 快照沿现有 `CaptureSink` 异步落盘：新增封闭记录 kind（`line.go` 现为三值枚举），复用有界队列与非阻塞准入；拒绝与写失败计入现有损失账目。
- 快照引用继续用 SnapshotID + Digest + ReadEntry；重启后按引用读回并校验摘要，`callArgumentsOf` 从内存 map 改为按引用读取。
- 明确不承诺：provider 内部改写（token tailoring）后的 body、opts 注入字段、隐藏重试次数。若需这些，只能通过 RoundTripper 另行采集，不属于本轮。

### D4 流式增量：tee 在归约旁，不进入归约

- 主路径 `model.Request` 设 `Stream: true`。`ReduceChannel` 与 `terminal.go` 契约不改。
- 在 `loop.go` 调 `ReduceChannel` 前插入 tee：一路进入归约，一路旁路给增量 sink。旁路必须非阻塞（满即丢并计数），否则展示面变成模型路径的背压点。
- 增量来源为 provider partial 包的 `Delta.Content` / `ReasoningContent`；**增量拼接不等于最终正文**（聚合路径有 reasoning fallback 等处理），增量只标为临时观察。
- 首期消费面：嵌入式回调 + CLI 实时输出。不提供 SSE 持久 chunk 回放；断线后读取当前状态或已完成结果。
- 工具调用参数增量默认不展示（provider 默认抑制 tool-delta）。
- 流式必须同时处理 `http.Client.Timeout`：改为按空闲超时或显式放宽，否则长流被截断。该项列入验收。
- openai-go 默认重试需显式关闭或记录该边界，否则一次 attempt 可能包含多次网络请求。

### D5 观测：实例注入

- `slog`、TracerProvider、MeterProvider 由装配注入，不修改进程级默认；OTel 关闭时本地日志与 Inspect 不失效。
- 指标增加：请求终态计数、阶段耗时直方图、token 用量、未结算义务数量/年龄、采集损失、重连/reset。labels 只用有限枚举（provider、路由、工具类别、结果码），**移除 `tagent.event.key` 高基数标签**。
- span 覆盖受理/等待、模型、工具、委派、审批、提交、恢复、发送；跨异步与重启用 link。
- 只在网络调用前开始计完整模型耗时；客户端只掌握 chunk 时称 time-to-first-chunk。

### D6 训练导出读取落盘证据

- 训练域保留 `offline-training-export` 既有契约方向（工具感知 SFT、可靠 loss 边界、manifest、因果分组、用途资格矩阵），仅把来源从"内存快照 + attempt jsonl"改为"落盘请求证据 + 执行事实"。
- 不完整样本（无请求证据、摘要不一致、终态未知、未授权）具名拒收并计入拒收清单，不填零。
- 沿用已确认范围：SFT/DPO 数据 + 真实 tokenizer/chat template/collator 读取；参考 `Qwen/Qwen3-0.6B` 只取 tokenizer 与模板，不下载权重；不执行训练。
- DPO 需要同前缀候选与偏好来源；无证据的偏好不从成功/失败状态推导。

### D7 门禁与文档

- 保留正确性、安全、恢复、竞态与文档真实性验证；取消只固定旧代码形状的骨架断言与"一文件一职责"式布局门。具体删除项在 tasks 中逐项列出并回查引用。
- 文档分为：当前使用/当前架构（以正式入口为准）、生成 API、历史归档。历史 wiki 保持原样不再改写，但在入口标注"可能过时"。

### D8 既有变更处置

- `success-semantics`：已落地代码作为基线，未完成的收尾（远端 CI/PR/merge）不替其勾选。
- `post-training-data-loop`：只有 proposal，能力被本变更的请求证据与导出读取吸收；标记为 superseded 指向本变更，不伪装完成归档。
- `online-rl-training`：保持未实施，前置改指本变更产出的数据管线；不作为本轮 DoD。

## Risks / Trade-offs

- **[尾部丢数据]** 关键事实 Sync 后确认，但 Sync 之前的进程内状态在崩溃时丢失 → 只承认最后一条完整记录；对外确认点严格晚于 Sync。
- **[消息重复]** 回执未落导致重发 → 现行已接受该取向；诊断面报告"已发未记"，不宣称 exactly-once。
- **[增量与最终不一致]** reasoning fallback 等使 delta 拼接 ≠ 最终正文 → 增量标注为临时；最终以聚合包为准。
- **[长流被超时截断]** `http.Client.Timeout` 覆盖 body → 改空闲超时或显式放宽，并在验收中验证长流。
- **[隐藏重试破坏对账]** openai-go 默认重试在 Request 边界下 → 显式关闭或记录该事实，不宣称 attempt 等于一次网络请求。
- **[日志无限增长]** 追加日志只增不减 → 本轮不做 compaction，但记录基线体积；清理策略单列后续，不在本轮引入淘汰以免误删活跃材料。
- **[旧数据不可读]** 新格式与旧目录不兼容 → 新目录 + 旧目录只读保全；启动遇旧格式具名拒绝。

## Migration Plan

1. 新增日志格式与后端，保留旧后端代码到切换验收通过。
2. 新数据目录格式版本写入文件头；旧目录打开时具名拒绝并提示使用新目录。
3. 切换默认存储为追加日志；旧后端在下一批次删除。
4. 快照落盘与参数合并先于流式开启，避免流式放大未修复的快照缺口。
5. 回退：保留旧后端开关一个批次；回退后与基线行为零 diff（不迁移数据）。

## Open Questions

1. 日志帧是否直接采用 LevelDB journal 分块格式，还是简化版"长度+CRC32C+JSON"单帧？前者可复用成熟实现，后者更小。
2. 增量首期是否需要"慢消费者断开后自动降级为只收最终结果"，还是直接断开？
3. `post-training-data-loop` 是标记 superseded 还是归档为空壳？
4. 旧 localfile 后端保留一个批次还是同批删除（当前无生产数据承诺）？
