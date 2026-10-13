# 03 history — 任务清单（三级孙任务）

> 本文件是二级子变更 `03-history` 的任务清单，层级为三级「孙任务」。全部未勾选（`- [ ]`），本轮**不执行**。
> 编号沿用权威计划 §7 的 `03.1–03.8`，不修补旧编号、不继承旧完成率。每条编号**只出现一次**。
> 每条格式：`- [ ] <编号> <动词开头产出> + 关键约束 —— 前置 + 候选拟写文件（非本轮授权） + 设计闸(G0–G3) + 验证`。
> 验证口径：点名用例真实 pass 才算过；选择器零匹配/Skip/未命中不得因退出码 0 记通过（计划 §7）。并发/状态任务加 `-race`；真实资源用例独立 `-count=1` 另列资源门。
> 「前置」含设计/实现依赖，不表示当前授权执行；「候选拟写文件」是未来候选写域，不表示已存在或批准修改。

## 前置与边界

- 实现前置：00.3（G1 共享合同冻结）。
- 接口前置：02.1 的事件身份/原生载荷与 task 提交消费端口**签名**；01.4 的 `StatePlan`/`AgentBinding` 签名。
- 本域与 02 共享 `task`/`state` 写面、与 04 共享 `workspace`/`tool` 边界 → G3 登记读写冲突、串行或同一代理。
- 计划 §7 注：03.3 与 02.2 的跨包接线由同一集成单或串行实施，不由两个代理各造一个提交协议。

## 任务

- [ ] 03.1 实现原文引用与 owner/format 隔离、精确召回与分段读取（不复活多套同义接口）—— 前置：02.1 —— 候选拟写文件（非本轮授权）：`memory/reference.go`（`Ref{Key,Owner,Format,RetentionPolicy}` 与去重作用域）、`memory/recall.go`（items 精确回补 + 分段读取）、`tests/integration/history/original_reference_isolation_test.go`（`TestOriginalReferenceIsolation`）—— 设计闸：G0 = 现场清单/保护面/能力档位；G1 = 引用身份字段与去重作用域（owner/format 不同 MUST NOT 互覆盖）；未命中显式报告（DESIGN-GATE：blob 去重边界）；G2 = 序列「同 blob 不同 owner 不去重 / items 原序回补零 LLM / 未命中明确标注 / 分段读取字节一致 / 不留多套同义召回入口」；G3 = 白名单 + 与 02 的 `event` 边界 —— 验证：`go test ./tests/integration/history -run '^TestOriginalReferenceIsolation$' -count=10 -json`

- [ ] 03.2 实现 localfile/file 后端适配与实际提交屏障（内存档明确不耐久）—— 前置：03.1 —— 候选拟写文件（非本轮授权）：`memory/localfile.go`（分片原子快照 `Sync`；包划分按 G1，不预建 `memory/kv` 子包）、`memory/segment_store.go`（FileSegmentStore 提交屏障）、`memory/config.go`（memory/localfile/file 档位适配）、`tests/integration/history/storage_capabilities_test.go`（`TestStorageCapabilities`）—— 设计闸：G1 = 提交屏障契约（evt/idx/必需 meta 写完且 `Sync` 成功才发布缓存/计数/成功）；内存档复用合同但**不承诺进程恢复**；每档单独证明屏障、不假设批量 API 天然原子（DESIGN-GATE：真实 rustviking 资源门）；G2 = 序列「屏障后跨进程读回 / 屏障失败不报 durable 成功 / 内存档重启不出未持久记录 / 三档各自证明」；G3 = 白名单 + 与 01 config 档位 —— 验证：`go test -race ./tests/integration/history -run '^TestStorageCapabilities$' -count=10 -json`（真实 rustviking 另列资源门，`-count=1`）

- [ ] 03.3 实现 task/state 持久提交与幂等接纳、未确认投递重放（按当前编排绑定定位所属 owner）—— 前置：02.2、03.2 —— 候选拟写文件（非本轮授权）：`task/restore.go`（从事实链重建 + 幂等接纳）、`state/durable.go`（持久提交接线）、`memory/replay.go`（未确认投递重放）、`tests/integration/history/durable_task_round_trip_test.go`（`TestDurableTaskRoundTrip`）—— 设计闸：G1 = 幂等提交返回已存在事实 ≠ 通知已交付；恢复/重投按当前编排绑定解析目标所属 owner（不读顶层单例）；闭包 MUST NOT 保留旧 wrapper/binding/lease（DESIGN-GATE：与 02.2 提交端口签名）；G2 = 序列「持久提交往返 / 重复提交幂等不新增 / 未确认投递重放并定位 owner / 目标已移除明确拒绝 / 无需更换数据库」；G3 = 白名单 + 03.3 与 02.2 **同一集成单或串行**（不各造提交协议）—— 验证：`go test -race ./tests/integration/history -run '^TestDurableTaskRoundTrip$' -count=10 -json`

- [ ] 03.4 实现原文/呈现决策/预算组装/投影回放进入实际 Agent 请求（因果链按 session 隔离）—— 前置：02.5、03.3 —— 候选拟写文件（非本轮授权）：`memory/projection.go`（`RebuildProjectionFromWAL` 冷启动回放）、`memory/replay_budget.go`（呈现决策与预算组装）、`agent/request_assembly.go`（投影进入实际请求）、`tests/integration/history/projection_replay_and_budget_test.go`（`TestProjectionReplayAndBudget`）—— 设计闸：G1 = 投影 = 事实链纯回放（无游离 checkpoint）；原文/模型视图/呈现决策分层（仅决策为全文时相同）；因果链按 `(PartitionID, SessionID)` 隔离、经 `RelationStore.SetParent` 维护（DESIGN-GATE：与 02.5 agent 装配边界）；G2 = 序列「逐字节重建 snapshot+尾部 / 无 compaction 有界 fallback / 大结果仅票据不复发全文 / 预算组装含恢复提示 / 同 session 因果链不跨会话误连」；G3 = 白名单 + 与 02.5 的 `agent` 写面串行 —— 验证：`go test -race ./tests/integration/history -run '^TestProjectionReplayAndBudget$' -count=10 -json`

- [ ] 03.5 实现保留/墓碑/恢复清点与回收协调（多持有者保护、能力缺失明确）—— 前置：03.3 —— 候选拟写文件（非本轮授权）：`memory/lifecycle.go`（保留/墓碑/回收协调与 `armGrace`）、`memory/retention.go`（引用计数保留租约）、`memory/lifecycle_config.go`（声明式配置注入）、`tests/integration/history/recovery_before_reclamation_test.go`（`TestRecoveryBeforeReclamation`）—— 设计闸：G1 = 保留租约由**共享资源 owner**（非任一 agent）持有、引用计数递减归零才解除；保留租约端口仅按**真实 consumer 所需**表达、名称/签名不冻结（DESIGN-GATE：依真实 consumer 收敛，不复述旧方法清单）；**持久恢复档**缺保护/回收协调能力 SHALL 具名报告能力不完整并禁止破坏性清理（fail-closed），**非耐久档**显式 capability 缺失；恢复清点先于首轮破坏性扫描；生命周期策略经声明式配置注入（DESIGN-GATE：与 01 config 档位对齐）；G2 = 序列「重启登记先于淘汰 / 未确认材料越过 TTL 仍保留且释放后按原时间戳复归类 / 显式删除撞保护返回 protected / 单 agent 关闭不释放共享保护 / 枚举失败不核对残缺视图 / 持久档缺能力 fail-closed」；G3 = 白名单 + 与 01 config 的档位 —— 验证：`go test -race ./tests/integration/history -run '^TestRecoveryBeforeReclamation$' -count=10 -json`

- [ ] 03.6 实现 workspace 副本物化引用稳定、授权分段读与缺失重物化（原文有效性与副本寿命分开）—— 前置：03.4/03.5 —— 候选拟写文件（非本轮授权）：`workspace/replica.go`（物化引用稳定 + 安全复用/多副本合法 + 缺失重物化）、`workspace/segmented_reader.go`（授权分段读）、`tests/integration/history/replica_materialization_test.go`（`TestReplicaMaterialization`）—— 设计闸：G1 = 原文有效性与副本寿命分开；每次物化引用稳定、跨 owner/tool/restart 不互覆盖；显式允许安全复用、多副本合法（MUST NOT 全局唯一副本/全局锁）；blob 去重 ≠ 引用去重；副本写成功才声明路径存在；分段读受授权约束（DESIGN-GATE：与 04.x 工具读面/授权）；G2 = 序列「物化引用稳定不互覆盖 / 显式安全复用 / 多副本并存合法 / 副本缺失按原文重物化 / 原文失效副本不假称可重建 / 授权外分段读拒绝」；G3 = 白名单 + `workspace` 写面 —— 验证：`go test -race ./tests/integration/history -run '^TestReplicaMaterialization$' -count=10 -json`

- [ ] 03.7 实现临时副本启动/周期清理与活跃读写保护（不删用户文件、不跨链接越界）—— 前置：03.6 —— 候选拟写文件（非本轮授权）：`workspace/cleanup.go`（启动/周期清理 + 活跃读写保护）、`tests/integration/history/replica_cleanup_test.go`（`TestReplicaCleanup`）—— 设计闸：G1 = 活跃读写中的副本 MUST NOT 被删；清理 MUST NOT 触碰用户文件、MUST NOT 跨符号链接越界（DESIGN-GATE：与 03.6 副本物化归属与引用稳定）；G2 = 序列「启动清理 / 周期清理 / 活跃读写的副本不删 / 用户文件不动 / 跨链接不越界」；G3 = 白名单 + `workspace` 写面 —— 验证：`go test -race ./tests/integration/history -run '^TestReplicaCleanup$' -count=10 -json`

- [ ] 03.8 实现通用工具/任务结果/tmux/overflow 接入同一外置路径，验证清副本后独立进程恢复 —— 前置：03.7、04.2 —— 候选拟写文件（非本轮授权）：`workspace/externalize.go`（统一外置路径）、`tool/output_limit.go`（`OutputLimitTool` 接入外置，阈值 `MaxTokens/2*4`）、`tests/integration/history/large_result_restart_test.go`（`TestLargeResultRestart`）—— 设计闸：G1 = 通用工具/任务结果/tmux/overflow 走同一外置路径与同一阈值公式；超大结果首次入模前保存原文、给有界预览与分段入口；清副本后独立进程经原文恢复（DESIGN-GATE：与 04.2 资源/外置边界）；G2 = 序列「大结果先存原文再有界预览 / 阈值与 MaxTokens 对齐 / 清副本后独立进程经原文恢复 / nil 结果不拦截」；G3 = 白名单 + 与 04.2 的 `tool`/资源边界 —— 验证：`go test -race ./tests/integration/history -run '^TestLargeResultRestart$' -count=10 -json`

## 执行期回写（本轮为空）

（执行期新增问题按「场景 → 根源 → 影响面」追加；契约变化先回写 `design.md`，不静默塞进补丁。）
