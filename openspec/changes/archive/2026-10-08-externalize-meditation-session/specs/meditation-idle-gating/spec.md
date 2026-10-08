# meditation-idle-gating Specification（MODIFIED delta）

## MODIFIED Requirements

### Requirement: 新颖性锚点锚定输入侧

`lastUserInput` SHALL 仅在消息注入点（`InjectMessageWithSource` / `InjectMessageWithMetadata`）且 `source == "user"` 时更新——该更新规则在两种形态下不变。判据取用分两形态：**in-loop 形态**（未配置观察面）novelty 判定 SHALL 沿用 `lastUserInput > lastMeditation`；**外部观察形态**（`meditation.observed_namespaces` 非空）novelty 判定 SHALL 切换为跨分区谱系判据（定义见 meditation-agent-partition），`lastUserInput` 锚 SHALL NOT 参与该形态的判定，但仍按本条规则持续更新（保留理由：移除观察面回切 in-loop 形态时输入侧语义连续；该理由 SHALL 以代码注释固化，防后人当死代码删除）。两形态均 SHALL NOT 依据任何输出侧事件更新新颖性锚点。

#### Scenario: 用户注入更新新颖性锚点

- **WHEN** 以 `source="user"` 注入消息
- **THEN** `lastUserInput` SHALL 更新为注入时刻（两形态同）

#### Scenario: 非用户 source 注入不更新

- **WHEN** 以 `source="meditation"` 或 `source="task"` 注入消息
- **THEN** `lastUserInput` SHALL 保持不变（两形态同）

#### Scenario: 外部形态判据不读注入锚

- **WHEN** 已配置观察面，且仅有本 agent 的用户注入而无被观察分区的非自管新事件
- **THEN** novelty 门保持关闭——外部形态下用户注入本身不再解锁冥想

### Requirement: 门控不依赖输出侧血统追踪

冥想门控 SHALL NOT 依赖输出事件、任务层 `Origin` 行李或 task_settled 事件的血统标记；事件回调（`makeOnEventCallback`）SHALL NOT 包含冥想锚点更新逻辑；`checkAndMeditate` SHALL NOT 在触发时重置空闲锚点。**唯一例外**：外部观察形态的谱系判据 SHALL 读事实链的**持久化归因**（`FullEvent.Metadata[trigger_source]`，入库时由装配路径盖章——既有事实的一部分，非输出侧追踪）；该判据 SHALL NOT 引入任何回调侧读取或新的输出侧血统标注。

#### Scenario: 事件回调与冥想状态解耦

- **WHEN** 任意 trigger_source 的 final response 经过事件回调
- **THEN** 冥想管理器的任何锚点 SHALL NOT 因该回调而变化（两形态同）

#### Scenario: 外部判据读持久归因非回调

- **WHEN** 外部观察形态执行 novelty 判定
- **THEN** 判定路径只含事实链查询与元数据过滤，不含事件回调或输出侧状态的读取
