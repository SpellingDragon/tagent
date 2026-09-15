## Context

tagent 四层组织结构：1 全局默认 → 2 MCP servers（toolmcp 等，已有 hot-sync）→ 3 Agents（org 下 per-agent 身份/工具注册表/模型超时，**当前为冷配置**）→ 4 节点引用。第 3 层变更需重启进程，中断有状态会话。toolmcp 已验证的 hot-sync 模式 = 配置指纹比对 + 缓存实例复用，本设计复用该模式将"配置文件 → 组织结构"从启动期一次性构建改为按配置指纹惰性重建。motivation 见 proposal.md - Why。

## Goals / Non-Goals

**Goals:**
- 第 3 层组织配置变更不重启进程即可生效
- 重建期间服务不中断（在途事件、坏配置两条故障路径都有明确行为）
- 与 toolmcp hot-sync 模式在概念上对齐，降低认知负担

**Non-Goals:**
- 不支持配置回滚到任意历史版本（只保留"当前 + 上一个有效"两代）
- 不改动第 1/2/4 层（全局默认、MCP servers、节点引用）的配置加载方式
- 不做集群级配置分发/一致性（单进程内行为）

## Decisions

### D1 重建粒度：整层原子重建（org 级快照），非 per-agent 增量

**选择**：指纹变化时重建整层 Agent 组织结构快照，新旧实例整体替换。
**理由**：① 第 3 层 per-agent 配置间存在引用关系（工具注册表、身份命名），per-agent 增量替换需处理跨 agent 一致性，复杂度远超收益；② toolmcp hot-sync 的既有模式即"指纹 → 实例缓存 → 命中复用"，整层快照天然适配该模式；③ 原子替换使"在途事件持有旧引用"语义简单（快照不可变，引用即安全）。
**备选**：per-agent 粒度增量重建——被否决：引用一致性处理复杂，且单 agent 变更占多数的场景下，整层重建成本仍可接受（配置规模小，重建是纯内存操作）。

### D2 在途事件策略：drain-free（不排空在途），在途事件继续用旧快照完成

**选择**：触发重建时不等待在途事件排空，立即构建新快照并原子切换；在途事件持有旧快照引用继续处理至完成；旧快照无引用后由 GC 回收。

**依赖归属矩阵（哪些依赖随快照走、哪些跨代共享）**：

重建的是"组织结构快照"（agent 身份/工具注册表/模型超时等纯配置派生物），不是全部运行时。两代并存期间，各运行时依赖的归属与交错语义定义如下（依赖清单以 build_agent.go / tagent.go 的实际装配为据）：

| 依赖项 | 归属 | 两代交错语义（为什么正确 / 约束是什么） |
|--------|------|------|
| memory store 实例（InMemoryStore / FileSegmentStore，按 memory.type + path 经 named*Stores 解析） | **随快照走** | 重建配置里若 `memory.type`/`memory.path` 变化 → 新快照解析到不同 store 实例 → 两代各写各的（历史分裂风险）→ **已由 D3 白名单将 `agents.*.memory` 整块排除出指纹**（memory.type/path 不触发重建），非 memory 字段重建时 store 实例按 path 复用（named*Stores 命中即同一实例），两代实际共享同一存储 |
| memory store **共享场景**（多 agent 同 path / 新旧快照同 path） | **共享** | 同一 store 实例被两代并发读写是**正确且预期**的：存储层自带并发保护（InMemoryStore `sync.RWMutex`，in_memory_store.go:16；segment/relation 等各层 mutex），事件写入按 snowflake key（进程唯一）无键冲突；混代事件按事件时间序落同一分区，读取侧（recall/投影）按时间序回放即可见完整时间线，分代不影响存储正确性——这正是"快照只含配置派生物、存储归代际共享"模式的收益 |
| govGate / govLedger（治理闸与共享账本，进程级单例） | **共享** | tagent.go:89-93 定义为跨 agent 共享实例。两代对它们的调用交错是安全的：gate 是无状态请求-响应（风险分级+预算判定），ledger 按 partition 写事件（与 memStore 同理）。**约束**：budget/审批的预算窗口状态跨代连续（旧代消耗的预算新代继续记账）——这是**期望行为**（治理预算是进程级资源，不应因重建清零） |
| event bus（含 ReliableBus 溢出目录） | **随快照走** | per-agent bus 在 buildAgent 内构造（build_agent.go:315-316，`<BusSpillDir>/<agentName>` 隔离），随 agent 实例创建。旧快照的 bus 随旧 agent 退役排空后关闭；新快照用新 bus。溢出目录按 agent 名隔离，两代同 agent 名共用溢出目录（无串扰：同 agent 的溢出事件属于同一逻辑流） |
| MCP registry（进程级，含 mcp_servers 热同步） | **共享** | 第 2 层已有独立热同步（config.go:94-97 mcp_servers 注释），不在本层重建范围，两代共用。toolref 中 kind: mcp 的解析只是引用查找，不持实例 |
| resolvedModels（`provider:model` 缓存） | **共享** | tagent.go:69-71 按 key 缓存跨 agent 复用。模型实例本身无热重载需求（其行为由调用参数决定），两代共用；新配置改 model 名 → 新 key → 新实例，旧 key 实例随旧快照退役 |
| 工具实例（plain tool / agent-tool wrapper） | **随快照走** | 在 buildAgent 内构造，随快照创建/退役。**语义约束（被删工具）**：见下方"在途事件对被删工具的调用语义" |
| compressor（SmartCompressor）及其阈值 | **随快照走** | per-agent 实例，阈值取自快照内 AgentConfig，两代互不干扰 |
| 退化状态机 DegradationManager | 随快照走 | per-agent 构造（build_agent.go:61-105），随快照走；但退化判定依据的 store 是共享的（见第一行），恢复探测路径一致 |
| 其他 rc 字段（model 主实例/evoGit/approvalChannels 等） | 共享 | 进程级注入（WithModel 等 Option），与配置派生物无关，两代共用 |

**在途事件对被删工具的调用语义**：旧快照在途任务调用"新配置已删除的工具"时，调用**按旧快照成功执行**（旧快照的注册表仍含该工具），工具的副作用（文件写入/事件写入）落入共享依赖（memStore/bus 溢出落盘）——这与"在途事件持旧快照完成"的 D2 主张一致：在途事件的世界观就是旧配置，其工具调用以旧配置语义完成为准，不做中途摘除。新事件抵达新快照时才按新注册表处理（调用已删工具 → 走既有"工具不存在"错误路径）。**代价**：切换瞬间后的短窗口内，共享时间线上会出现"新代际事件流 + 少量旧代在途尾部"的混合；这是 drain-free 的已接受代价，混入事件带快照代 ID 可观测（见可观测性）。

**理由**：① 排空等待（drain）在长任务场景会显著推迟生效时间，且需处理超时/强制中断等新边界；② 配置快照不可变 → 持引用即安全，无锁竞争；③ 新事件立即享受新配置，符合热重载直觉。
**备选**：drain 后切换（等待在途事件完成再切换）——被否决：生效延迟不可控，且引入"等待期间到达的新事件用哪代配置"的额外决策复杂度。

### D3 配置指纹算法：规范化 + SHA-256，而非裸文件哈希/文件 mtime；指纹输入为**组织配置子集**（白名单），而非完整配置

**选择**：解析配置后规范化（键序稳定、语义无关空白归一）再取 SHA-256；规范化中间表示作为指纹输入。**指纹输入不是"完整配置"，而是参与组织重建的字段子集（白名单）**：

**白名单（参与指纹，变更触发重建）**——第 3 层组织结构的结构字段：
- `agents.*` 全部结构字段（AgentConfig：model/provider/prompt_dir/system_prompt/tools/max_tool_iterations/max_tokens/temperature/keep_recent_tasks/task_terminal_ttl/resume_context_rounds/thinking/meditation/workspace_root/description 等），**但 `agents.*.memory` 整块排除**（见下），且 `compress_threshold` 已移出（见黑名单，增量 A 修订）
- `entry`（入口 agent 名）
- `prompt_dir`（全局 prompt 基目录）
- `model` / `provider` / `providers.*`（全局默认模型与 provider 声明：模型身份变更直接改变 agent 行为，属组织配置）

**黑名单（排除出指纹，变更不触发重建）**——持久化路径与运行时路径类字段，运行中变更本就无法迁移（新路径是空目录，旧路径持有全部历史），重建只会产生两份落盘目录错乱，且部分字段已有自己的生效路径：
- `agents.*.compress_threshold`（压缩触发阈值）——**增量 A 修订（2026-09-11，实现期发现）**：该字段可经 `ApplyOrgParams`（compressor 原子阈值换装）热生效，按 D3 判据"需重建 agent 实例才能生效才进指纹"不构成重建理由；且若留在白名单，热平移分支（指纹未变 → 热应用数值参数）将永不可达——改阈值 → 指纹变 → RESTART required，与特性目标自相矛盾。故移出白名单，变更走热平移路径。注：`agents.*.compress` 其余子字段（策略配置）仍属白名单（变更需重建 compressor 实例）
- `governance.dir`（budget/approval 持久化目录）及 governance 预算参数——治理闸是进程级共享单例（见 D2 矩阵），其变更属进程级配置，不属第 3 层组织
- `reliability.bus_spill_dir` / `reliability.meditation_anchor_dir` / `reliability.mem_spill_dir`（三持久化目录）及 reliability 开关参数——运行时可靠性子系统配置，同上进程级
- `agents.*.memory.type` / `agents.*.memory.path`（store 类型与共享路径）——变更意味着换存储实例：两代各持不同实例 → 历史分裂 + 两份落盘目录。热重载不承接该变更（需重启，或未来独立能力），指纹排除之
- `agents.*.memory.read_namespaces` / `lifecycle` / `engine` / `rustviking_binary`（记忆策略细节）——与 store 实例绑定（换 store 重建已被排除，策略细节重建无意义，避免误伤）
- `mcp_servers`（第 2 层，已有独立 hot-sync）与 `config_path`
- 进程级杂项：`api_endpoint`（**顶层 Config.APIEndpoint，区别于 providers.*.api_endpoint——后者属白名单**）/`api_key_env`/`log_level`/`request_timeout_seconds`/`app`/`trajectory_dump`/`trajectory_dir`/`working_dir`——影响进程行为而非组织结构，多数只能经重启/环境变更生效

**边界裁定的方法论**：判据是"该字段变更是否意味着**重建 agent 实例才能生效**"。`model.api_endpoint`（providers.*.api_endpoint）变更：模型实例可按新 endpoint 重建（resolveModel 按 key 查 resolvedModels，新 endpoint → 新 key → 新实例），且它直接改变 agent 的模型行为 → **纳入白名单**。而持久化 dir 类字段：变更后旧实例持有的资源无法平移 → 排除。未来新增字段默认按此判据归类，并在 design 此处显式登记（避免指纹覆盖悄悄漂移）。

**理由**：① 裸文件哈希对格式化/注释/键序敏感 → 无谓重建与指纹抖动；② mtime 单独作为判据不可靠（编辑器保存、touch、拷贝都会变 mtime）；③ SHA-256 碰撞概率可忽略，且与 toolmcp 既有指纹实践一致；④ 完整配置作指纹会把"持久化路径变更"误判为组织变更 → 重建产生目录错乱（新 dir 空 + 旧 dir 滞留历史），白名单从语义上限定重建范围，排除字段变更静默忽略（记一条 debug 日志，不重建）。
**备选**：文件级 SHA-256（不规范化）——被否决：语义等价配置触发假重建，违背"惰性重建"目标。完整配置（不含子集裁剪）作指纹输入——被否决：见理由④，持久化路径变更不应触发组织重建。

### D4（支撑决策）坏配置降级：验证前置 + 保留旧代

**选择**：新配置先完整解析+校验，通过后才构建新快照；失败则保留当前活跃快照继续服务，记录降级事件。
**理由**：与 D1 原子替换配合，失败路径天然简单——要么整体生效，要么完全不生效，无中间态。
**备选**：部分加载（valid 的 agent 先生效，invalid 的保留旧）——被否决：引入混合态，违背原子快照语义，排障困难。

## Risks / Trade-offs

- [坏配置长驻] 新配置坏了且迟迟不修复 → 旧配置持续服务，用户以为已生效而实际未生效 → **缓解**：降级事件 MUST 高可见度告警（日志 ERROR + 计数器），spec 场景已覆盖"降级可发现"
- [并发安全] 重建切换与事件处理并发：事件在切换瞬间读活跃快照 → **缓解**：活跃快照指针原子替换（如 atomic.Value / RWMutex），快照本身不可变，读路径无锁
- [混代时间线] drain-free 切换后，共享 memStore 上短窗口内出现"新代事件流 + 旧代在途尾部"混合写入 → **缓解**：存储层自带并发保护（RWMutex），混入事件按时间序回放语义正确（见 D2 矩阵），事件携带快照代 ID 可观测区分；被删工具在旧快照在途任务中的调用按旧语义成功执行（D2 已定义），共享时间线上其副作用完整可见
- [指纹子集漂移] 白名单遗漏某组织字段（变更了但指纹没变，假"未变更"）→ **缓解**：白名单采用"agents.* 全部结构字段减显式排除清单"的宽松策略（新增 AgentConfig 字段默认自动纳入，除非显式登记进黑名单）；新增字段时在 design D3 边界判据处显式登记归类；tasks 1.2 含白名单边界回归测试
- [两代内存驻留] 切换后旧快照等待在途引用归零 → **缓解**：引用计数归零即 GC 可回收，配置快照为纯内存小对象，驻留成本可忽略

## Migration Plan

1. 实现指纹计算与规范化（纯函数，可独立测试）
2. 在配置加载路径接入指纹比对 + 缓存实例复用（行为等价改造：首次加载 = 指纹 miss → 构建）
3. 接入运行期变更检测（事件驱动或定时）触发惰性重建
4. 坏配置降级与可观测性补齐
5. 回滚策略：热重载入口整体关闭（配置开关或 revert），回退到启动期一次性构建；指纹机制保留（纯函数无副作用）

## Open Questions

- [已由评审闭环] 依赖归属（D2 矩阵）与指纹子集（D3 白名单）已在对抗评审后定义；`model.api_endpoint` 归属白名单是本设计的裁定（判据：可重建生效且改变行为即属组织配置），若评审方认为 provider 连接信息属进程级配置，请在此登记后移入黑名单——不影响其余结构。
