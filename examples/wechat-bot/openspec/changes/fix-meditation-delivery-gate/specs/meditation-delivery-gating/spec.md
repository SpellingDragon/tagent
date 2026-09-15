## ADDED Requirements

### Requirement: 输出投递门禁的冥想血统闭环

任何从框架输出通道（outputCh）投递给消费端的携带 Response 的事件，其 trigger_source SHALL 在投递克隆/传播发生前已确定且可被消费端解析到——消费端 SHALL NOT 因元数据缺失而将冥想系输出按兜底来源（`user`）投递给用户。门禁判定 SHALL 同时覆盖两类输入：(a) 纯冥想批次（批次内触发事件全部为 meditation 来源）；(b) 冥想血统派生的事件（如冥想 turn spawn 的后台任务 settle 后回流的事件），两类输入的输出 SHALL NOT 投递给用户会话。

门禁判定 SHALL 依据**事件层类别**（external_input 事件的 Source / 类别字段与世系元数据）闭合于框架层，SHALL NOT 依据消息层 role 形态或上下文内容形态（"渲染为 user role"不构成用户触发证据）。

#### Scenario: 纯冥想批次的输出不投递给用户

- **WHEN** 事件循环拉取的批次中仅含 meditation 来源的 external_input 事件，agent 正常消费并产出 final response
- **THEN** 该输出事件的 trigger_source SHALL 被解析为 `meditation`
- **AND** 消费端 SHALL 扣留该输出，不投递给用户微信

#### Scenario: 冥想血统 task settle 的输出不投递给用户

- **WHEN** 冥想 turn spawn 的后台任务 settle，其回流事件被消费并产出 final response
- **THEN** 该输出事件 SHALL 携带冥想血统（trigger_source 解析为 `meditation`），无论其到达输出通道的具体路径
- **AND** 消费端 SHALL 扣留该输出，不投递给用户微信

#### Scenario: 看板在跑不破坏冥想门禁（防御性约束）

> 动机修正注记（2026-09-15 实证）：本场景原动机「看板以 user source 注入事件批、压过冥想/任务世系」**已证伪**——看板已实证不走事件流，系 BeforeModel 请求期注入（context_manager.go:406 注册回调 → :1221-1227 直接改 args.Request.Messages，request-only、never projected/compressed；轨迹 3977 条 board 内容 0 命中双重验证）。断言由此降格为**防御性约束**：约束未来任何看板注入形态不得影响事件层 trigger_source 判定，不再描述当前泄漏成因（泄漏真因由 §1 取证两问另行钉死）。

- **WHEN** 内部回合（纯冥想批次或冥想血统 task settle 回流）执行时存在活跃后台任务，后台任务看板被注入到本轮消息列表（以 user role 形态呈现给 LLM；当前实证为请求期注入，本约束不限于该形态）
- **THEN** 看板注入 SHALL NOT 使该回合的 trigger_source 被解析为 `user`——看板属系统内部观察，SHALL NOT 参与真实用户判定、SHALL NOT 压过冥想/任务血统
- **AND** 该回合输出 trigger_source SHALL 保持 `meditation`（或任务血统），消费端 SHALL 扣留，不投递给用户微信

#### Scenario: 兜底来源不豁免冥想输出

- **WHEN** 任一投递事件的 trigger_source 元数据缺失或不可解析
- **THEN** 消费端 SHALL NOT 因此将该事件默认为用户会话并投递——对携带 Response 的冥想系输出，缺失 SHALL 视为门禁失败并扣留（可经告警路径上报），SHALL NOT 走用户投递分支
- **AND** 用户消息触发的正常输出不受影响（见下条场景）

#### Scenario: 用户消息输出正常投递（防过修）

- **WHEN** 普通用户消息触发 agent 产出 final response（批内可含后台任务看板注入）
- **THEN** 输出事件 trigger_source SHALL 解析为 `user`，并正常投递到该消息 meta_chat_id 对应会话（含既有回退路径）

### Requirement: 系统内部观察注入的事件类别独立性

§1.3 通道盘点圈定的**真正入批**系统注入（非用户产生却以 Source="user" 形态进入事件批的事件）SHALL 拥有独立的事件类别（internal-observation 类），SHALL NOT 以用户来源（Source="user"）形态进入事件批。已实证**不入批**的 BeforeModel 请求期看板注入不属本条适用对象（其约束见「看板在跑不破坏冥想门禁」场景的防御性降格注记）。该类别的事件 SHALL NOT 参与 trigger_source 判定的"真实用户必胜"分支，SHALL NOT 覆盖冥想/任务血统的世系判定。消息层 role 形态（注入内容以 RoleUser 呈现给 LLM）与事件层类别相互独立，前者不影响后者。若 §1.3 盘点结论为无任何真正入批的伪装注入，本条降级为防御性类别储备（适用于未来新增入批注入点），不驱动当前修复。

#### Scenario: 真正入批的系统注入归类为内部观察

- **WHEN** §1.3 盘点圈定的真正入批系统注入（非用户事件以 Source="user" 形态进入事件批）被注入到某回合
- **THEN** 其事件层类别 SHALL 为 internal-observation（而非 user），事件批的 trigger_source 导出 SHALL 跳过该事件的用户判定分支
- **AND** 该注入内容呈现给 LLM 的 role 形态不受此约束影响（可保持 RoleUser）

#### Scenario: 真实用户输入不受内部观察类别影响

- **WHEN** 批内同时存在真实用户消息事件与入批的内部观察注入
- **THEN** trigger_source SHALL 解析为 `user`（真实用户判定仅对真实用户事件生效，内部观察不参与该分支）
- **AND** 正常用户投递路径不受影响

### Requirement: 冥想输出不构成对用户新颖性闸门的武装

冥想系输出的投递扣留 SHALL NOT 影响冥想触发的双闸门判定锚点：扣留事件 SHALL NOT 更新 `lastUserInput`，SHALL NOT 重置空闲锚点。门禁失败时被扣留的输出 SHALL 保留在框架侧可观测（日志/轨迹属性），供泄漏排查。

#### Scenario: 扣留不武装冥想

- **WHEN** 一次冥想输出被门禁扣留
- **THEN** 冥想管理器的新颖性锚点与空闲锚点 SHALL 保持不变（仅正常 turn 结束刷新空闲锚点）

#### Scenario: 扣留事件可观测

- **WHEN** 冥想输出被扣留
- **THEN** 日志与轨迹 SHALL 记录该扣留（事件标识 + trigger_source + 原因），不产生投递副作用
