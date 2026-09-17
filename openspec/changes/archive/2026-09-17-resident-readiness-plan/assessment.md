# tagent 当前设计与实现评估

## 基线与结论

- 代码基线：`aeb273dee72f8fb72c581a35c31344d6d2db669b`；评估开始时工作树干净。
- 输入：`docs/.dev/tagent深度分析报告(1).md`、`tagent多维度评价报告(1).md`、`tagent重要特征深度调研(1).md`，三者描述的是 2026-09-13 快照。
- 对照：已归档的 `implementation-hardening`、`hardening-review-batch2`，当前主规格、源码和既有测试。归档、勾选或注释均不单独作为实现完成证据。
- 范围：关键链路深查与全包既有测试，不是逐文件全树安全审计。唯一 CodeReview 子代理未完成全树审计，仅返回基线 diff 为空；本报告不代表独立审查门禁通过。
- 本轮仅创建提案工件，不修改生产代码、原始报告、部署配置或主规格，不调用真实模型、不操作运行中的 bot。

**结论：核心架构值得保留，旧报告的若干高危结论已经过时；下一阶段应集中完成“成功返回、持久化、恢复完整性、资源所有权、最终消费”之间的闭环。当前测试基线良好，但不足以证明开箱即用的长期无人值守可靠性。**

不沿用旧报告的 7.5/6.6 分数：源码、测试和规范均已变化，也没有可复现的量化评分基准。没有核查当前 stars、外部用户数、作者人数或生产事故率，不以历史生态数据推断当前成熟度。

## 八维评价

| 维度 | 设计评价 | 当前实现评价与边界 |
|---|---|---|
| 架构一致性 | 保留事件事实链、派生投影、票据水合三层；复用上游 turn 内 ReAct 合理 | `agent/event_bus.go` 已明确 turn 间编排；README 仍写“替代同步 ReAct”，有表达漂移 |
| 正确性 | stored-gate、单消费者和纯回放提供清晰验证面 | 存储读错误被吞、无锚恢复先截后滤、内存后端可覆写，削弱不变量；见 F02/F03/F04 |
| 复杂度 | Engine/Policy 分离、按需启用平台能力合理 | 组合根共享注册表、验证壳与真实执行代交错；宜围绕 ownership 拆职责，不按行数机械拆文件 |
| 可演进性 | MemoryEngine/KVStore/Embedder、声明恒定 MCP 提供接口边界 | 热更多 agent 绑定、有效配置与期望配置混用；旧主规格存在相互矛盾条款 |
| 安全 | OS 隔离与应用治理分层合理，认证已有单点 | RL token 认证已实现；无 token 安全依赖宿主监听守卫；请求/反馈队列无界、端点切换属于高信任控制面 |
| 可靠性 | 恢复和降级路径齐全 | fsync 已加入但 `StoreEvent` 仍早于耐久屏障成功；spill 回收早于事实入库即删除；不能统称 at-least-once |
| 性能 | 冻结渲染、按需压缩、可重建索引值得保留 | 无本轮吞吐/RSS/启动成本实测；字符 token 估算、CLI fork、全量扫描需基准驱动，不直接引入 ANN/数据库 |
| 工程验证 | 根模块与独立 bot 模块均有有效回归；已有前缀反例测试 | race 直接命令通过但有既有豁免；本地包装脚本可假绿；soak 骨架不能证明独立进程/掉电恢复 |

## 历史报告结论处置矩阵

“修复存在”表示已找到机制且所属既有测试通过，不表示完整威胁面或新组合已穷尽。

| 历史结论 | 当前处置 | 当前证据 / 后续 |
|---|---|---|
| LocalFileKV 完全无 fsync | 已过时，耐久闭环仍部分完成 | `memory/kv/local_file_kv.go:248-343` 已 Sync；`415-420` 仍异步成功，F01 |
| RL HTTP 完全无认证 | 修复存在 | `rl/http_api.go:83-147` Bearer + 监听助手；保留宿主/限额验收，F09 |
| recall items 无上限 | 修复存在 | `tool/recall/memory_recall.go:100-106` 上限 50；不重复实现 |
| YAML 非 strict、引用环栈溢出 | 修复存在 | `internal/strictyaml/`；`build_agent.go:70-101` 路径级环检测；根测试通过 |
| Resume 对恢复任务 close(nil) | 修复记录及回归存在 | `agent/task/task_resume_nil_test.go`、`task_prune_nil_detector_test.go`，task 包含 race 通过；继续检验恢复绑定组合 |
| Stop 后同实例 Start panic | 实现改为显式终结态 | `agent/lifecycle.go:142-159,206-223`；主 spec 仍要求可重启，F11 |
| 无 compaction 时重建 no-op | 已过时，替代实现仍不完整 | `agent/projection_rebuild.go:49-54,174-205` 已 fallback；先截后滤和错误状态缺口见 F03 |
| 冷分区完全不可发现 | localfile 部分修复 | `segment_store.go:183-203` 枚举已接；计数仍 0；不具枚举能力的后端降级，F02 |
| 所有事件默认第 8 天失效 | 原报告过度概括 | `memory/lifecycle.go:31-42,275-294`：类型 TTL 覆盖 7 天兜底，常见 3/14/30 天，负全局值才关闭 TTL |
| README 宣称永久保留 | 已部分修正 | README 已说明 TTL；“随时”“宿主机重启 tmux 仍活”等边界仍需修正，F11 |
| TypeToolUse 已/应清除 | 清理未完全兑现 | `agent/event_bus.go:24-31` 称已移除，`55-62` 常量与过时注释仍在；列卫生债，不冒充活路径故障 |
| invBus/activeBus、modelref 均死码 | 不沿用一刀切删除建议 | 涉及历史演进，实施前按引用与协议面取证；不在此认定全部可删 |
| 旧 runner/model 永不回收 | 已部分修复 | `RetireRunner` 与 `SwappableModel.sweepRetired` 存在；model 流生命周期仍有缺口，F07 |
| lastEventKeys 无界增长 | 修复存在 | `plugin/memory_plugin.go:213-243` 封顶 4096；被淘汰会话的因果恢复语义仍待专项验证 |
| 热更后子 agent 存储漂移 | 仍有具体证据 | `build_agent.go:103-115` 壳递归复用 entry store；F06 |
| Rollback 无生产触发面 | 历史清单不能直接沿用 | 已有宿主信号实现记录及回滚闭包；多代/数值/子 agent 的行为验收仍需补齐 |
| governance 关闭导致评估半盲 | 可用性声明已接 | `build_agent.go:220-224`；“声明不可用”不等于证明长期效果；保留开关组合实验 |
| agent 核心未进 race | 已过时 | `.github/workflows/ci.yml:49-55` 纳入，仍有逐测试豁免，F10 |
| 无任何 soak | 已过时，但证据不足 | `tests/soak_test.go` 存在；同进程 + 关闭 fsync + 优雅 Close + 查询摘要，F10 |
| gzip/L3 摘要存在 | 不按旧文案建设新能力 | 存储应称时间窗分层压实；不因名词而引入 gzip/LSM 重写 |
| token 启发式必然低估 / 必然高估 | 三报告相互矛盾 | 字符数无法推出所有语言/代码的误差方向；需要按实际 tokenizer 测量，F08 |
| shell 无默认强沙箱 | 作为部署边界接受，不等于安全完成 | 不把工作目录或子串分类器视为沙箱；不盲目默认 sudo 或关闭所有工具 |

## 当前发现与工作归属

以下为本轮源码路径确认或行为复现；除 F10 外，未为新发现编写 fail-before 测试。实现前必须先补复现。优先级按后果与触发条件，不按旧报告分数。来源为当前基线存量，不臆断具体引入提交。

### F01 — 已见 fsync，不等于已确认事件耐久（P0，WP1）

- 证据：`LocalFileKV.KVPut` 更新 map 后直接 nil（`memory/kv/local_file_kv.go:415-420`）；50 次或 2 秒后 flush（`79-85,404-409`），周期/阈值 flush 丢弃错误（`166,408`）；目录 Sync 错误被吞（`98-109`）。`FileSegmentStore.StoreEvent` 连续写 evt/idx/meta 后缓存并返回，无屏障（`memory/segment_store.go:289-318`）。
- 路径：输入/工具事件 → StoreEvent → 内存 KV → 投影/recall 可见 → 进程在 flush 前退出 → 重启无该事件。即使 fsync=true，也存在未落盘窗口；ENOSPC/EIO 同样不能可靠上报到降级装饰器。
- 影响：用户可见成功与事实链耐久不同步；“Sync 后可恢复”测试不能替代 `StoreEvent` 成功边界测试。
- 修复方向：保留底层异步 KV 契约，在事件提交边界明确屏障，传播写/Sync/目录错误；失败不投影、不对外声称 durable；不引入第二事实存储。

### F02 — 冷分区发现与容量计数未闭合（P1，WP1）

- 证据：`segment_store.go:183-203` 仅注册零值 PartitionState；`lifecycle.go:199-208` 用 eventCount 决定是否淘汰；`segment_store.go:264-285` 在序列化/重复键检查成功前递增。
- 影响：重启后容量阈值忽略存量；写失败也可能增计数。主 spec 明确容许进程内近似计数，故这既是能力边界也是拟议规格升级，不能称已违背“持久总量”契约。
- 修复方向：启动从唯一事件事实重建逻辑存活计数，成功提交后增量维护；重复/墓碑/物理回收只计算一次。枚举不可用时明确 capacity=unknown，不伪报为 0。

### F03 — 恢复完整性在底层错误与过滤顺序上断链（P0，WP2）

- 证据：`segment_store.go:403-413` 批量读逐项错误一律跳过；`424-465,610-626` 分区/段扫描错误跳过仍返回 nil。上层 `projection_rebuild.go:229-263` 只统计返回 error，不核对请求键与返回键；fallback 在 `182-192` 先取最后 500 再过滤非投影记录；`176-178` 空结果提前返回会吞掉失败统计；payload 错误早退无统一结果。
- 影响：真实持久后端出错可能被上层判 FULL；task_spawned/resident 等可挤掉最近有效上下文。日志里的 PARTIAL 尚非宿主/模型可消费的状态。
- 修复方向：typed missing/I/O 区分，键集合对账，过滤后有界，结构化恢复结果贯通 diagnostics 和一次性上下文提示；保留最新 500 可见事件，不自动取消 TTL。

### F04 — 后端的不可变与默认隔离契约不一致（P1，WP1）

- 证据：`in_memory_store.go:39-58` 同 key 覆盖；`168-181` 无分区遍历全部，而 file 后端 `segment_store.go:672-682` 返回空。file 读缓存直接返回内部指针（`362-365`），浅拷贝对象内 map/slice 亦需 ownership 检查。
- 影响：以内存 mock 验证的行为与实际持久后端不等价；非授权调用方可误读跨分区数据；调用者修改返回值可能污染后续读。
- 修复方向：统一重复键拒绝、默认查询隔离和深拷贝契约；明确按显式授权访问，不因知道 EventKey 自动获得权限。

### F05 — ReliableBus 不是端到端耐久交付（P0，WP2）

- 证据：`agent/event_bus.go:317-355` dispatch 未序列化且 backlog 满时重新入 channel，可超越较旧 spill；`agent/reliability/spill.go:127-174` Reclaim 读完即删，早于 `BuildInvocation/persistBusEvent`。`Spill`（`88-116`）无 Sync。低负载事件仍仅在 channel。
- 路径：Publish → channel 或 spill → Pull/Reclaim 删除 → store/投影 → 模型。回收与入库之间崩溃会丢 spill；已进入 channel 的消息也没有重启保证。
- 修复方向：明确 volatile accepted 与 durable accepted；启用可靠模式时所有接收先进入有界持久 inbox，消费使用 claim/ack，事实提交后确认，源 ID 幂等。拒绝超限和不可写，不默默降档；默认不开可靠模式。

### F06 — 多 agent 热更与共享资源所有权不完整（P1，WP3）

- 证据：`build_agent.go:103-115` executor shell 对每个名字都取 entryMemStore；`tagent.go:359-369,431-451` 热参遍历旧 agentCache，而新子树由独立空 cache 构建；`wiring.go:288-390` 全局按 path 复用 store，`agent/lifecycle.go:93-98` 每个 agent 都可 Close；注册表未释放。
- 补充证据：`tagent.go:413-418` 拒绝 memory 热更后仍推进 lastMemFP，混淆期望与生效态；`memory/types.go:263-278` FNV-1a 最终裁为 10 bit，并非旧报告暗示的 32 bit 隔离空间。共享 store 的命名冲突需构造期检测。
- 影响：子 agent 的存储去向/有效参数可能随代际变化；Close 后同进程 New 可复用已关闭 store。当前 soak 正沿用此路径，因此不证明独立恢复。
- 修复方向：有 owner 的运行时资源 registry、显式借用与租约、按 agent 身份重绑真实常驻对象、有效值回读；同路径不兼容配置拒绝，不偷偷采用首次配置。

### F07 — model 回收计数只覆盖“返回流”，未覆盖“流完成”（P1，WP3）

- 证据：`rl/swappable_model.go:77-86` 用 defer 在 GenerateContent 返回 channel 时减 inFlight；`58-72` 随后可 Close 旧 model。
- 影响条件：旧 model 实现 Close 且生成在返回后的异步流继续；Swap 可提前关闭仍在使用的资源。现有无状态 provider 未必触发，不能把条件性缺口说成所有调用都会断流。
- 修复方向：租约覆盖整个返回流，结束/取消/错误恰释放一次；Info 与当前实例同样明确借用；只回收自己拥有的资源。

### F08 — 卡片票据及性能证据仍未机器化（P1/P2，WP5）

- 证据：`agent/compress/context_compressor.go:924-965` 浓缩输出只单行清洗和长度检查，没有首尾/高亮票据集合校验。
- 影响：模型可以返回不含 key 或带未知 key 的短文本而被接受。票据水合真实不等于卡片摘要无幻觉，更不等于原文永不过期。
- 修复方向：校验输出 key 是输入子集且保留首尾、高亮必需 key；不合格走既有确定性下沉；增加原文/票据/计数/预算组合测试。token 误差、重建扫描、fork/RSS 用离线基准定量，不提前换引擎。

### F09 — 认证后的控制面仍缺资源上界（P1，WP4）

- 证据：`rl/http_api.go:205,306` ReadAll 无限制；`264-271` feedback 通知队列无界；`159-179` long-poll 不消费请求取消；`323-338` endpoint 更新与逐消息注入非事务且接受角色直传。
- 影响条件：已授权调用方、可信 loopback 使用者或错误客户端；认证修复不解决大请求、慢连接和通知堆积。端点替换是预期 RL 能力，不能直接把授权操作者等同攻击者。
- 修复方向：配置化有限请求/消息/队列，取消及时退出，结构化丢弃计数与事实链补读提示；endpoint 回调能返回错误，验证 scheme/userinfo 和运维允许列表，未授权目的地在副作用前拒绝。

### F10 — 验收器本身与“长期运行”证据不足（P0/P2，WP0/WP6）

- 行为复现：`bash scripts/race_check.sh ./definitely-nonexistent-audit-package` 输出 `race_check: OK (no data races)` 且 exit=0。根因是 `scripts/race_check.sh:19-24` 忽略 go test 退出码，只匹配 race 文本。
- CI 直接调用 go test，未使用该脚本；不能据此认定 CI 假绿。CI 的 race 列表没有覆盖 `memory/kv`、`memory/engine`、`rl`、`plugin`，本轮单独扩围运行已通过。
- `tests/soak_test.go:38-54,57-108` 同进程 New、fsync=false、优雅 Close、只查询原始标记，不要求实际发生 compaction，也不校验最终模型请求/通知。共享 registry 使“fresh”实例前提不成立。
- 修复方向：先让检测工具对失败可信；再以独立子进程、真实本地持久后端、确定性模型强制压缩与回放，验证所有 accepted ID 和最终请求；72h 故障注入验证单列，短测不能替代。

### F11 — 规范与宣传相互冲突（P1，WP0/WP7）

- `event-sourced-projection/spec.md:86-89` 禁止无锚回放，与 `120-125` 要求回放矛盾。
- `persistent-event-loop/spec.md:130-142` 要求 Stop 后同实例重启，而当前实现明确禁止；采用已实现的终结态，不恢复旧 panic 路径。
- `swappable-executor/spec.md:75-82` 要求 ring 内 runner 保留，而实现保存配置用于重建；应保留配置，不强留已退役对象。
- README 把“宿主机重启”与“tmux 还活着”连在同一场景。tmux server 可跨 agent 进程重启，不跨整机重启；整机重启只能依据声明记录诊断/经授权重建任务，不能假称旧进程仍活。
- 修复方向：delta 修改整个冲突 Requirement，而非在旧条款后再追加相反场景。历史报告保留原文，通过当前评估注明失效范围。

## 完整数据流与生命周期核验

| 链路 | 格式与真源 | 有效期/消费时机 | 降级、边界与最终可见 |
|---|---|---|---|
| 接收→执行 | AgentEvent.ID + message → inbox → FullEvent/EventKey → projection | channel 当前仅到进程结束；spill 到 Reclaim；投影在 BeforeModel 消费 | F01/F05：接收、提交、投影、响应分别计数；不可只证明 enqueue |
| 事件→索引→召回 | FullEvent 是事实；向量和缓存是派生；检索只交 key | 类型 TTL 3/14/30 天等；负全局值禁 TTL；容量可另淘汰 | 命中缓存与未命中都须墓碑检查；引擎关闭/报错降级 keyword，I/O 错误不得冒充不存在 |
| 折叠→恢复→模型 | compaction 事件保存 retained refs/fullBoundary；无独立 checkpoint | 快照自身可长存，被引用原文仍受 TTL；冷启动先恢复再消费 | F03/F08：先过滤、对账、报告 PARTIAL，再校验模型实际请求与缓存前缀 |
| 任务→通知 | Declarative/Origin → task_spawned/settled → registry → detector | 实际进程在 tmux server；registry/metadata 可跨 agent 重启 | nil、stale、service/job、迟到信号、unknown 世系均须到宿主投递门验证；不得只看 tracked=true |
| 配置→热更 | 文件真源 → 解析/校验 → 代际执行器；持久资源属 runtime | 旧 turn 和旧流结束才能退役；缓存不是有效配置的真源 | F06/F07：失败保持旧代，逐 agent 验证实际 store/参数/prompt/tool，不以 applied 日志替代 |
| 反馈→评估→阅读 | feedback/evaluation 事件是真源；队列是增量通知 | 通知丢失可补查；governance 关闭意味着部分证据不可用 | 只建议不自动 revert；报告 insufficient/unavailable；验证下一次 digest/status 是否呈现 |

## 本轮验证记录

环境：Go 1.24.1，darwin/arm64。命令均未调用真实模型。

| 命令 | 结果 | 边界 |
|---|---|---|
| 根 `go build ./...`、`go vet ./...` | 通过 | 不代表动态数据流无缺陷 |
| 根 `go test ./... -short -count=1 -timeout=120s` | 通过 | 真实 LLM 测试按 short 跳过 |
| bot 模块 `go build -o /dev/null .`、`go vet ./...`、`go test ./... -short -count=1 -timeout=120s` | 通过 | 独立 go.mod，已单独执行；未启动 bot |
| 根 `go test ./memory/... ./agent/... ./plugin/... ./rl/... ./evolution/... ./event/... ./tool/... -race -short -count=1 -timeout=120s` | 通过 | 保留现有 raceEnabled 豁免；不是无条件全路径无竞态证明 |
| `python3 scripts/test_verify_restart_prefix.py` | 5 个场景通过 | 说明已有反例修复有效，不证明真实重启完整性 |
| 无效包输入的 `scripts/race_check.sh` | 确认假绿 | 输出 OK、退出 0，已列 WP0 |
| `openspec validate resident-readiness-plan --strict` / `openspec validate --all --strict` | 通过；全量 94 项、0 失败 | 仅证明规格格式有效，不证明主规格语义无矛盾或实现已完成 |

未执行：真实模型/AReaL 训练、72h soak、掉电测试、实际部署重启、上游 race 豁免解除、性能基准、覆盖率测量。未调用这些能力即不得标为已验证。

## 工作计划索引与停止条件

详细设计见 `design.md`，可执行清单见 `tasks.md`。

| 工作包 | 优先级 | 解决的问题 | 完成判断 |
|---|---|---|---|
| WP0 证据与契约基线 | P0 | F10/F11 | 验收器能判失败；冲突规格有唯一语义 |
| WP1 事件存储契约 | P0/P1 | F01/F02/F04 | durable 成功不丢尾；失败可见；后端一致 |
| WP2 接收与恢复 | P0 | F03/F05 | claim/commit/ack 故障矩阵 + 结构化恢复结果 |
| WP3 资源与热更 | P1 | F06/F07 | 多 agent 隔离、独立 reopen、流完成后回收 |
| WP4 控制面与运维 | P1 | F09 | 全路由认证、有限资源、错误不误杀/不误接受 |
| WP5 压缩与性能 | P1/P2 | F08 | 票据守卫通过；量测形成明确容量边界 |
| WP6 综合验证 | P2，发布前置 | F10 | 独立进程 E2E、受控故障、长跑证据 |
| WP7 文档与发布候选 | P1/P2 | F11 + 全部 | 文案只声称已证明能力；发布动作另行授权 |

延后：新平台子系统、默认开启全部治理、无限保留全部记忆、自建 shell 安全墙、替换上游 ReAct、无基准的 ANN/存储重写。旧报告“先打 tag”不是本轮执行动作；发布应在验证门之后由维护者决定。
