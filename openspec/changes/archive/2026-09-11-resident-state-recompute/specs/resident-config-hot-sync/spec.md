## ADDED Requirements

### Requirement: 单一 config-substrate 观察者

系统 SHALL 提供唯一一个 config-substrate 观察者，泛化 `tool/mcp/registry.go:maybeSyncLocked` 的范式（惰性查 tagent.yaml mtime → 重解析 → 逐段 diff → 分段 apply），作为全部运行时段热更新的单一入口。SHALL NOT 存在并行的第二/第三套 mtime 热重载机制（如独立的 org 热重载闭包或死代码 coordinator）。

#### Scenario: mtime 变更触发逐段 diff-apply
- **WHEN** tagent.yaml 被修改且 mtime 变化，下一次惰性检查点到达
- **THEN** 观察者 SHALL 重解析、与上次快照逐段 diff、只对变化的段调用其 apply 处理器

#### Scenario: 单一机制无并行实现
- **WHEN** 审查热更新代码路径
- **THEN** MCP servers、prompt、tool 描述、compress_threshold、tools、subagent 的热更新 SHALL 共用同一观察者，无重复的 mtime 检查实现

### Requirement: 段级热应用与需重启分类

每个配置段 SHALL 被分类为「可热应用」或「需重启」。可热应用段（mcp_servers、prompt、tool 描述、compress_threshold、tools 增删）SHALL 免重启生效；需重启段（无法中途安全替换者，如 memory backend、subagent 组合在阶段二就绪前）SHALL 高可见日志提示需重启并 fail-closed 保留旧组合。

#### Scenario: compress_threshold 热应用
- **WHEN** 仅 compress_threshold 段变化
- **THEN** 观察者 SHALL 原子热应用新阈值到运行中的压缩器，无需重启

#### Scenario: 结构变更 fail-closed 提示重启
- **WHEN** 需重启段（如 agent 拓扑结构）变化
- **THEN** 观察者 SHALL 记录高可见日志（需重启）并保留旧组合继续服务，SHALL NOT 半应用

### Requirement: 解析失败 fail-closed

重解析或 diff 失败时，观察者 SHALL 保留当前组合并 WARN，SHALL NOT 使运行中的 agent 崩溃或降级到空组合。

#### Scenario: 坏配置不搞崩运行中服务
- **WHEN** tagent.yaml 被改成非法 YAML 或字段错误
- **THEN** 观察者 SHALL 捕获错误、WARN、保留上一次有效组合继续服务

### Requirement: 安全换点在 turn 边界

组合类变更（尤其 subagent 重组、tools 增删）SHALL 在 turn 边界应用，SHALL NOT 在 BeforeModel 执行中途换。in-flight 引用（正在执行的子 agent 工具调用）SHALL 被妥善处理（drain 或保留旧定义至该 turn 结束）。

#### Scenario: subagent 热重组在 turn 边界
- **WHEN** subagent 定义被热改且当前有 in-flight 子 agent 调用
- **THEN** 新组合 SHALL 在下一个 turn 边界生效，in-flight 调用 SHALL 以旧定义完成，不中断

### Requirement: 热应用值单一原子权威

热应用的运行期值 SHALL 有单一原子权威（如压缩阈值的 `atomic` 存储）；SHALL NOT 存在未同步的旁路字段。运维/探针读取 SHALL 直接读权威值。

#### Scenario: 探针读权威值
- **WHEN** 运维探针查询当前生效的 compress_threshold
- **THEN** SHALL 返回压缩器原子权威值，而非可能与权威分叉的旁路字段
