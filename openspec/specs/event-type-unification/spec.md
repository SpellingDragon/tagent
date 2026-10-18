# event-type-unification Specification

## Purpose
事件类型常量单一来源：全部事件类型只在 event 包定义并被跨包引用，memory 包不再持有平行常量，杜绝两套字面量随演化分叉。
## Requirements
### Requirement: 事件类型常量单一来源

事件类型常量 SHALL 只在 `event` 包中定义（`event.TypeExternalInput` 等）。`memory/types.go` 中的 `EventType*` 常量 SHALL 被删除。所有引用 `memory.EventType*` 的代码 SHALL 改为引用 `event.Type*`。

本域新增的事件类型常量（如受理回执、审批等待、拒绝终局、后台结果观察、转交关联）——**若**确需新增——SHALL 同样只在 `event` 包单点定义，SHALL NOT 在 `task`/`agent`/`plugin` 包各持平行字面量。事件类型分类 SHALL 依据原生 `Role`/`ToolCalls`，MUST NOT 依据被重构或归一后的字段推导。

#### Scenario: memory 包不再定义事件类型常量

- **WHEN** 在 memory 包中搜索 `EventType` 常量定义
- **THEN** 不存在 `EventTypeExternalInput`、`EventTypeAgentOutput`、`EventTypeActionCommand`、`EventTypeThinkingPlan`、`EventTypeContextCompress` 等常量定义

#### Scenario: 所有引用使用 event.Type*

- **WHEN** 在整个代码库中搜索 `memory.EventType`
- **THEN** 返回零结果
- **AND** 所有原来引用 `memory.EventTypeExternalInput` 的代码已改为 `event.TypeExternalInput`

#### Scenario: event 包常量值不变

- **WHEN** 对比 event.TypeExternalInput 的值
- **THEN** 值仍为 "external_input"（与原 memory.EventTypeExternalInput 相同）
- **AND** 所有事件类型的字符串值保持不变（向后兼容已存储的事件）

#### Scenario: 新增事件类型单点定义

- **WHEN** 在 `task`/`agent`/`plugin` 包中搜索本域新增事件类型的字面量或常量定义
- **THEN** 返回零结果，全部引用 `event.Type*`

#### Scenario: 分类基于原生角色

- **WHEN** 对同一事件重复分类
- **THEN** 分类结果 SHALL 只依赖原生 `Role`/`ToolCalls`，与被后续变换的视图/摘要无关

### Requirement: memory 包可引用 event 包无循环依赖

memory 包 SHALL 可以 import event 包。event 包 SHALL 不 import memory 包。依赖方向为 memory → event → model，无循环。

#### Scenario: 编译无循环依赖

- **WHEN** 执行 `go build ./...`
- **THEN** 编译成功，无 "import cycle" 错误
- **AND** memory 包的文件可以引用 `event.TypeExternalInput` 等常量


### Requirement: feedback 类型注册

系统 MUST 经 EventTypeSpec 注册 `feedback`（见 feedback-binding spec），类型元数据单点声明。

#### Scenario: 注册即生效
- **WHEN** `feedback` 在注册表 init() 注册
- **THEN** 类型元数据全链路一致生效

### Requirement: governance 事件非骨架

governance 治理事件的 EventTypeSpec `Skeleton` 标记 MUST 为 **false**——治理记录（否决/批准/goal/退化）不参与骨架压缩，全文保留供审计与 goal 重建回放。

#### Scenario: 治理事件不被骨架化
- **WHEN** 压缩定级处理含 governance 事件的段
- **THEN** governance 事件内容完整保留（不折叠为骨架行），历史审计可全文追溯
