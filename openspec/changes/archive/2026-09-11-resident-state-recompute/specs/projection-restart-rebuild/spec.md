## ADDED Requirements

### Requirement: 启动期从完备 WAL 重建投影

系统 SHALL 在 agent 启动期提供一个投影重建原语 `RebuildProjectionFromWAL`，与 `ReattachResidentSessions` 结构同构：以 WAL（未墓碑的正 key 事件 + 持久化的 `context_compress_summary` 综述事件）为唯一真相源，按 EventKey 升序逐条经**幂等 Append** 路径重建 SessionProjection，随后由首个 BeforeModel 的确定性 `Compress` 重折叠出压缩视图。重建 SHALL 只在启动期对**空投影**执行一次。

#### Scenario: 冷启动重建投影
- **WHEN** agent 进程重启，投影为空，WAL 含历史正 key 事件与 narrative 综述事件
- **THEN** `RebuildProjectionFromWAL` SHALL 按 key 升序重放，将每条事件 Append 进投影（综述事件重建为负 key 综述 ref）
- **AND** 首个 BeforeModel 的确定性压缩 SHALL 重折叠出与重启前一致的卡片层与综述层

#### Scenario: 重建仅启动期一次、只对空投影
- **WHEN** 投影已含运行期事件（非空）
- **THEN** 重建 SHALL NOT 在其上执行（避免覆盖活投影）；运行期的 spill 恢复路径 SHALL 保持 append-only

### Requirement: 重建只 Append 不 Replace 活投影

重建与任何 WAL 重放路径 SHALL 只经幂等 `AppendProjectionRef`（按 EventKey 去重）修改投影，SHALL NOT 以快照 `Replace` 整体覆盖投影。压缩器状态（fullBoundary 等）SHALL 由确定性重折叠在 BeforeModel 单 goroutine 内重算得出，SHALL NOT 由跨 goroutine 回灌写入。

#### Scenario: spill 恢复重放不抹活投影
- **WHEN** memory 依赖 degraded→normal 触发 `ReplaySpilled`，重放窗口内事件，而活投影已含比重放事件更新的条目
- **THEN** 重放 SHALL 仅 Append（幂等，已在投影的条目被去重跳过），SHALL NOT Replace 掉更新的活条目

#### Scenario: 无跨 goroutine 压缩态写入
- **WHEN** 重建或重放与 BeforeModel 压缩并发
- **THEN** fullBoundary/threshold 等压缩态 SHALL 仅在 BeforeModel goroutine 内变更，无数据竞争

### Requirement: 重建后上下文逐字节一致以复用前缀缓存

给定相同 WAL 与配置，重建后的 `render(projection)` SHALL 与重启前逐字节一致，使 LLM provider 的前缀缓存命中。不可重算的 LLM 滚动综述 SHALL 从持久化的 `context_compress_summary` 事件逐字节复现，SHALL NOT 在重启时重新生成。

#### Scenario: 逐字节装配一致
- **WHEN** 进程 A 压缩若干轮后落 WAL，进程 B 冷启动重建
- **THEN** B 的 `render(projection)` SHALL 与 A 重启前逐字节相同（含综述文本与卡片行）

#### Scenario: 综述逐字节复现而非重生成
- **WHEN** 重建遇到持久化的 narrative 综述事件
- **THEN** SHALL 直接采用其存储字节，SHALL NOT 重新调用 LLM 合成（避免非确定性破坏前缀）

### Requirement: 压缩阈值取自配置而非持久态

重建 SHALL 从配置（`config.go` CompressThreshold，含 R4 热更新权威）取 compress threshold，SHALL NOT 从任何持久化快照回灌 threshold。

#### Scenario: 重启反映最新配置阈值
- **WHEN** 运维在重启前改了 tagent.yaml 的 compress_threshold
- **THEN** 重建后的压缩 SHALL 采用配置中的新阈值，而非任何历史持久值
