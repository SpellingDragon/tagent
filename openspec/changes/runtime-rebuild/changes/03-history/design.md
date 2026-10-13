# 03 history — 设计

## Context

历史域把 `memory`（原文与可重建投影）、`workspace`（临时文件副本）、存储后端（localfile/file=rustviking）与 task/state 的持久提交接成一条链。历史生产树已删除，残件不可运行（计划 §1）；旧 `memory/` 的十余个子包划分与类名**不是基线**（计划 §1、§3）。本设计只写「怎么做」，不改一级 design / 主 spec / 旧 change / 外部计划 / prototype。

权威来源：`运行时_重设_实施计划_304e54c3.md` §3（架构与责任）、§4.4（提交、历史与副本）、§5（tRPC 复用验收）、§6、§7（03.1–03.8）。

## Goals / Non-Goals

**Goals**

- 原文与引用元数据分离：`owner/format/保留策略` 不同的引用互不覆盖；原文按字节精确召回与分段读取；不复活多套同义召回入口。
- 后端适配与**实际提交屏障**：每档单独证明屏障；内存档复用同一业务合同但不承诺进程恢复。
- task/state 持久提交与幂等接纳；未确认投递重放时按**当前编排绑定**定位所属 owner，无需更换数据库。
- 投影恒等于事实链回放（无游离 checkpoint）；冷启动逐字节重建；因果链按 session 隔离。
- 保留/墓碑/恢复清点与回收协调在同一存储生命周期内；多持有者引用计数保护；持久档缺能力 fail-closed、非耐久档显式 capability。
- workspace 副本物化引用稳定、显式安全复用、多副本合法、授权分段读、缺失重物化；原文寿命与副本寿命分开。
- 统一外置路径：通用工具/任务结果/tmux/overflow 同路；清副本后独立进程可经原文恢复。

**Non-Goals**

- 不改 01 的 `config` 纯声明解析、02 的事件身份/提交端口实现；不重写 provider/tool 协议。
- 不新增数据库/WAL 选型（保持 memory/localfile/file 档位）；不实现 04 的记忆整理与检索（04.7/04.8）。
- 不实现 Query 读面（05）、RL 采集（06）。

## 语义与最小实体（字段/接口语义，不填空 struct 冒充冻结）

签名未定处标 **DESIGN-GATE(G1)**，须在生产调用链验证后冻结（计划 §4 首句）。

- `Ref`：`{Key(hex), Owner, Format, RetentionPolicy}`。**DESIGN-GATE(G1)**：`Owner`/`Format` 的取值域与去重作用域边界——blob 内容可去重，但 `owner/format/保留策略` 不同的引用 MUST NOT 互覆盖。真实消费者：recall、投影、外置、导出（06.4）。
- 原文读取：`GetEvent(key)` 逐字节；`Recall(items:[{key,hint?}])` 原序回补、零 LLM；分段读取 `read(start_line,num_lines)`。未命中 MUST 显式报告（不静默省略）。
- 存储屏障：`Sync() error`（按分区分片、每分区独立文件、只重写自上次屏障以来变更的分区）；`Close() error`（停扫描/压实、flush dirty 墓碑、关 journal）。
- 保留租约（保护/回收协调）：**DESIGN-GATE(G1)** 端口仅按**真实 consumer 所需**表达，具体名称/签名**不在此冻结**（依真实消费者收敛，不复述旧 5 方法清单）；持有者是**共享资源 owner**（非任一 agent），多持有者引用计数递减归零才解除、幂等。**持久恢复档**缺少保护/回收协调能力 SHALL 具名报告能力不完整并禁止破坏性清理（fail-closed），MUST NOT 当作「无保护即成功」；**非耐久档** SHALL 以**显式 capability 缺失**声明，不得伪造保护已生效。
- 副本：**DESIGN-GATE(G1)** 副本物化引用稳定（每次物化引用稳定可定位，跨 owner/tool/restart 不互覆盖）、显式允许安全复用、多副本合法、活跃读写保护句柄；MUST NOT 强制全局唯一副本、MUST NOT 以全局锁串行物化。blob 内容去重 ≠ 引用去重。与 04.x 工具读面/授权对齐。
- 阈值：`MaxTokens/2*4` 字符（沿用 `tool-output-interception` 既有公式）；`MaxTokens=0` 时默认 8000→16000 字符。

## 构造 / 调用 / 关闭时序

1. **启动**：打开后端 → 重建保留租约（登记屏障先于首轮破坏性扫描）→ 恢复登记与保护租约武装 → 启动消费循环。MUST NOT 先淘汰再登记。
2. **持久提交**：task 决定状态转移 → 经消费侧提交端口交存储实现 → evt/idx/必需 meta 写完且 `Sync()` 成功后才发布缓存、计数与成功结果 → 投影随后 `Add`。屏障未完成 MUST NOT 报 durable 成功。
3. **投影回放**：对空投影执行 `RebuildProjectionFromWAL`，先于 inbox/mem_spill 重放；非空投影不被 Replace。存在有效 compaction 时按 retained-ref 交错顺序复原（负 key 用载荷全文，正 key 从事实链水合），尾部经 MinEventKey 分页按 EventKey 写入序追加。
4. **召回**：显式工具调用 `recall`，按输入形态分流（items 精确 / query 语义 / turn_key 因果链 / orchestrate 显式 opt-in）。确定性形态始终走纯工程路径。
5. **保留/回收**：TTL 扫描与容量淘汰只写墓碑键、不原地删；物理销毁在压实；受保护 key MUST NOT 被物理销毁（只拒销毁，无损搬迁允许）；显式删除受保护 key 返回可判定 `protected` 错误。
6. **副本物化/清理**：原文有效时按需重物化副本（引用稳定、允许安全复用与多副本并存，MUST NOT 强求全局唯一）；启动与周期清理删除临时副本，但活跃读写中的副本 MUST NOT 被删，MUST NOT 触碰用户文件、MUST NOT 跨符号链接越界。
7. **关闭**：`Close()` 幂等；flush dirty 墓碑、停后台循环；未确认材料保护由资源 owner 持有，单 agent 关闭 MUST NOT 释放共享存储仍需要的保护。

**资源/取消/变异/错误**：
- 并发：多持有者引用计数；显式删除与保护竞争返回可判定错误，不静默销毁。
- 取消：读取失败/部分结果 MUST 携带非 nil error；上层处理 partial，不把 I/O 错误吞成完整成功或空库。
- 变异：输入/返回值隔离可变 map/slice；后端拒绝已提交重复 EventKey；内部重放可核对同键同内容、异内容拒绝。
- 错误：typed not-found / duplicate / I/O error 三分；compaction 源窗口读失败 MUST 中止本轮（不据不完整读取删源段）；TTL 扫描读失败 MAY 跳过但 MUST 告警。

## Decisions

- **D03-D1 投影是事实链的纯回放，无游离 checkpoint**：投影态只在事实链（KV LSM 事件日志）里，压缩折叠以 compaction 事件入链。备选（否）：投影态落独立快照——造成双真相源。理由：计划 §4.4、`event-sourced-projection` 一等不变量。
- **D03-D2 原文与引用元数据分离、blob 可去重但引用不覆盖**：`owner/format/保留策略` 参与去重作用域。备选（否）：以 blob 内容为唯一定位——不同 owner 互相覆盖。理由：计划 §4.4。
- **D03-D3 每档单独证明实际屏障、内存档明确不耐久**：不假设批量 API 天然原子；不新增数据库逃避合同。备选（否）：统一抽象成「事务」并假设原子——隐藏档位差异。理由：计划 §4.4。
- **D03-D4 保留保护与回收协调在同一存储生命周期内、由资源 owner 持有**：引用计数、先登记后淘汰；端口名称/签名按真实 consumer 收敛，**持久档缺能力 fail-closed、非耐久档显式 capability**。备选（否）：给 agent 各自私有保留表——单 agent 关闭误释放共享保护；或把缺能力当作无保护成功——与恢复安全相反。理由：计划 §4.4、`event-segment-store` 保留租约。
- **D03-D5 原文寿命与副本寿命分开、副本写成功才声明存在**：清副本后原文仍有效则可重物化。备选（否）：副本即真源——原文失效后无法区分可重建与否。理由：计划 §4.4。
- **D03-D6 统一外置路径与阈值公式单一来源**：通用工具/任务结果/tmux/overflow 同路，阈值 `MaxTokens/2*4`。备选（否）：各工具各造外置/截断——阈值分叉、副本散落。理由：`tool-output-interception`、计划 §4.4。
- **D03-D7 因果链按 session 隔离、经 RelationStore 维护**：`(PartitionID, SessionID)` 复合键；父子经 `RelationStore.SetParent`（非 `FullEvent.ParentKey`）。备选（否）：按 agent 名或全局单调链——跨会话误连。理由：`causal-chain-isolation`。
- **D03-D8 恢复/显式重投按当前编排绑定、闭包不保留旧 wrapper**：TaskManager 只传上下文与原任务材料，不做选版者。备选（否）：复用闭包捕获的旧 wrapper——旧代复活。理由：计划 §4.1、`task-registry-rebuild`。

## 消费者与反例（先枚举消费者再定验收）

| 产出物 | 真实消费者 | 反例（必须被拒绝/证明） |
|---|---|---|
| `Ref`（owner/format 隔离） | recall（03.1）、投影（03.4）、导出（06.4） | 同 blob 不同 owner 被去重覆盖；原文读取与字节不一致 |
| `Sync()` 提交屏障 | task/state 持久提交（03.3）、05.4 阻塞解释 | 屏障未完成即报 durable 成功；内存档重启「补出」未持久记录 |
| 投影回放 | 实际 Agent 请求（03.4）、04.7 检索 | 存在游离 checkpoint；投影回放与呈现决策不一致却称相同 |
| 保留租约（保护/回收协调） | 恢复清点（03.5）、TTL/容量（`event-lifecycle`） | 先淘汰再登记；单 agent 关闭释放共享保护；显式删除静默销毁；持久档缺能力被当无保护成功 |
| 副本物化引用稳定 | 工具读面（04.x）、外置（03.8） | 强制作全局唯一副本/全局锁；跨 owner/tool/restart 互覆盖；删用户文件；跨符号链接越界；活跃副本被删 |
| 外置路径/阈值 | 通用工具、任务结果、tmux、overflow（03.8） | 大结果不经原文直接截断；阈值与 `MaxTokens` 分叉 |
| 投影恢复结果 | 宿主/诊断（03.4） | 部分失败报 full；空结果伴随错误被当空库正常 |

## 覆盖映射（证据映射：每个 task → 场景落点或继承源块）

| task | Requirement（能力） | delta 场景落点 | 继承源块（不改，证据映射） |
|---|---|---|---|
| 03.1 | `recall-protocol` MODIFIED「索引卡为召回标准协议」+ ADDED「原文引用与分段读取保真」 | items 精确回补/未命中显式/分段读字节一致/owner-format 隔离 | 主 spec `event-segment-store`「后端不可变与隔离一致性」 |
| 03.2 | `event-segment-store` MODIFIED「LocalFileKV Sync 为原子快照屏障」；`rustviking-config` MODIFIED「Production MemoryStore wiring uses real RustVikingClient」 | 分片屏障/跨进程读回/内存档不耐久/file 档真实客户端 | 主 spec `event-segment-store`「FileSegmentStore supports graceful shutdown via Close」 |
| 03.3 | `task-registry-rebuild` MODIFIED「恢复与显式重投按当前编排绑定」 | 幂等接纳/当前绑定定位 owner/目标移除拒绝 | 主 spec `event-segment-store`「存储失败不可冒充正常缺失」 |
| 03.4 | `event-sourced-projection` MODIFIED「投影是事实链的纯回放」+「冷启动回放逐字节重建投影」；`causal-chain-isolation` MODIFIED「Causal chain is isolated by session」 | 逐字节重建/无 compaction fallback/因果链 session 隔离 | 主 spec `event-sourced-projection`「恢复观测覆盖全误差面」 |
| 03.5 | `storage-lifecycle-completion` ADDED「保留保护与回收协调」；`event-segment-store` MODIFIED「未确认恢复材料的有限保留租约」；`event-lifecycle` MODIFIED「生命周期策略经声明式配置注入」 | 先登记后淘汰/越过 TTL 仍保留/protected 错误/单 agent 关闭 | 主 spec `event-lifecycle`「Capacity-based eviction」 |
| 03.6 | `tool-output-interception` ADDED「工作区副本物化与引用稳定」 | 物化引用稳定/显式安全复用/多副本合法/缺失重物化/原文寿命 vs 副本寿命 | 主 spec `event-sourced-projection`「折叠原文冷存不删」 |
| 03.7 | `tool-output-interception` ADDED「临时副本清理与活跃读写保护」 | 活跃不删/不删用户文件/不跨链接越界 | — |
| 03.8 | `tool-output-interception` MODIFIED「工具输出大小拦截包装器」 | 统一外置/阈值对齐/清副本后独立进程恢复 | 主 spec `event-sourced-projection`「投影是事实链的纯回放」 |

> 说明：`event-relation-store`、`async-task-lifetime`、`ttl-cursor-scan`、`memory-*` 算法能力属**跨域只读消费或他域落地/已撤回**，本域不出 delta、不声称其已认证。本域**无 REMOVED**。

## 文件白名单（本域本轮可写，其余不动）

- `openspec/changes/runtime-rebuild/changes/03-history/**`（**仅 md 规划，无 Go/脚本**）。
- 实施期（本轮**不**写，候选）：`memory/*.go`（含 localfile/file 后端；包划分按 G1，不预建 `memory/kv` 子包）、`workspace/*.go`（副本/清理/**统一外置输出**，含原生 wrapper 注入端口，输出外置归 `workspace/externalize.go`）、`state/*.go`（持久提交接线）、`task/*.go`（恢复/重投）、`tests/integration/history/**`。**不预建未登记根 `tool` 包**。

## 验证表（可复跑命令；本轮不跑）

| task | 命令 |
|---|---|
| 03.1 | `go test ./tests/integration/history -run '^TestOriginalReferenceIsolation$' -count=10 -json` |
| 03.2 | `go test -race ./tests/integration/history -run '^TestStorageCapabilities$' -count=10 -json`（真实 rustviking 另列资源门，`-count=1`） |
| 03.3 | `go test -race ./tests/integration/history -run '^TestDurableTaskRoundTrip$' -count=10 -json` |
| 03.4 | `go test -race ./tests/integration/history -run '^TestProjectionReplayAndBudget$' -count=10 -json` |
| 03.5 | `go test -race ./tests/integration/history -run '^TestRecoveryBeforeReclamation$' -count=10 -json` |
| 03.6 | `go test -race ./tests/integration/history -run '^TestReplicaMaterialization$' -count=10 -json` |
| 03.7 | `go test -race ./tests/integration/history -run '^TestReplicaCleanup$' -count=10 -json` |
| 03.8 | `go test -race ./tests/integration/history -run '^TestLargeResultRestart$' -count=10 -json` |

> 口径：点名用例真实 pass 才算过；选择器零匹配/Skip 不得因退出码 0 记通过。03.1 为确定性契约用例（不加 `-race`）；其余涉并发/状态/跨进程重启，加 `-race`。

## Risks / Trade-offs

- [投影成第二真源] → 禁止事实链外 checkpoint；`TestProjectionReplayAndBudget` 断言逐字节重建与无游离快照。
- [原文被副本寿命牵连] → 原文寿命与副本寿命分开建模；`TestReplicaMaterialization` 断言副本缺失可重物化、原文失效不假称可重建。
- [保留保护与回收互踩] → 引用计数 + 先登记后淘汰；`TestRecoveryBeforeReclamation` 断言 protected 错误与单 agent 关闭不误释放。
- [召回退化成概率路径] → 确定性形态始终纯工程；`TestOriginalReferenceIsolation` 断言 items 零 LLM、未命中显式。
- [清理误删用户文件/越界] → 活跃读写保护 + 不跨链接；`TestReplicaCleanup` 断言用户文件不动。
- [真实 rustviking 未验证却称完成] → 真实资源用例独立 `-count=1` 且列入资源门，不并入契约用例通过。

## Migration

本域**无 REMOVED**。历史 `memory/` 残件与旧子包划分不作为基线：只需按 `Ref`/`Sync`/租约/副本接口面重新落地，不逐类恢复旧实现。`FullEvent.ParentKey` 已移除，父子关系一律经 `RelationStore.SetParent`（沿用 `causal-chain-isolation` 既有迁移语义，不新增第二路径）。已撤回能力 `ttl-cursor-scan`（游标扫描）不复活；有界增量扫描仍为已知缺口，留待单独变更评估。

## Open Questions（设计待核项 G0–G3）

- **G0（现场与需求）**：需要核实现存 `memory/`、`workspace/` 残件文件清单与保护面；memory/localfile/file 三档真实可用状态；`armGrace`/`L1Threshold`/`L2Threshold` 等数值是否沿用旧值。**未核前 `memory/config.go` 改动 design-blocked。**
- **G1（共享责任）**：
  - `Ref.Owner`/`Ref.Format`/`RetentionPolicy` 的取值域与去重作用域边界（DESIGN-GATE，与 06.4 导出、04.7 检索对齐）。
  - `Sync()` 的分区分片粒度与「自上次屏障以来变更的分区」追踪方式（DESIGN-GATE，与 02.2 提交端口对齐）。
  - 保留租约持有者（共享资源 owner）在进程内的标识与引用计数载体；登记屏障与 `armGrace` 的边界衔接（DESIGN-GATE）；端口名称/签名依真实 consumer 收敛，不复述旧方法清单。
  - workspace 副本物化引用稳定规则、安全复用与多副本边界、活跃读写保护句柄（DESIGN-GATE，与 04.x 工具读面/授权对齐）；MUST NOT 强制全局唯一副本。
- **G2（关键纵向场景）**：逐字节重建须以「Content ≠ EventSummary」数据独立进程重启验证；保留/回收须覆盖「重启登记先于淘汰」与「单 agent 关闭」；外置须覆盖「清副本后独立进程恢复」。**未推演前不得宣称接口冻结。**
- **G3（逐域实施就绪）**：本域与 02 共享 `task`/`state` 写面、与 04 共享 `workspace`/`tool` 边界；03.3 与 02.2 须**同一集成单或串行**（计划 §7 注）；真实 rustviking/embedding 资源门单列，不在本轮开启。

## 待核签名汇总（跨域）

1. `Ref{Key, Owner, Format, RetentionPolicy}` 与去重作用域 —— 影响 03.1/03.4/06.4（G1）。
2. `Sync() error` 分片屏障语义与 `Close() error` —— 影响 03.2/03.3（G1）。
3. 保留租约（保护/回收协调）端口形态与持有者/计数 —— 名称/签名不冻结、按真实 consumer 收敛 —— 影响 03.5/`event-lifecycle`（G1）。
4. workspace 副本物化引用稳定/安全复用/多副本边界与活跃保护句柄 —— 影响 03.6/03.7/03.8 与 04.x（G1）。
5. `task` 恢复闭包契约（不保留旧 wrapper/binding/lease，TaskManager 不选版）—— 影响 03.3 与 02.2/02.7（G1）。
