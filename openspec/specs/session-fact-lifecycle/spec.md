# session-fact-lifecycle Specification

## Purpose

会话事实的提交即持久化、当次召回、索引失败欠账与新事实可达性：正式回合写下的对话与工具结果必须同根落盘、当次经声明式召回入口读回原文，冷启动后原文与归属仍可恢复。

## Requirements

### Requirement: 会话事实提交即持久化且当次可召回

正式回合产生的对话与工具结果 SHALL 在提交成功时即由同一 durable 根持久保存，并在当次回合经正式召回入口（`Run.RecallReads()` 暴露的 `Table`/`Originals`/`Embedder` → 声明式 recall 工具）查到原文；召回返回的原文 MUST 与已提交事实的字节一致。持久化 MUST NOT 只停留在进程内内存，MUST NOT 以测试内手工 `commitFact` 或测试内存 store 冒充持久化。提交路径失败（写入失败、同步失败、存储不可用）SHALL 向调用方可见（具名错误或可判定失败状态），MUST NOT 返回伪成功，MUST NOT 把未落盘的事实计为已提交。

#### Scenario: 新事实当次可召回

- **WHEN** 一次正式 `Boot`/`Host.Invoke` 回合产生新的对话与工具结果事实并提交成功
- **THEN** 同一回合内 SHALL 能经正式召回路径查到该事实原文
- **AND** MUST NOT 依赖测试内存 store 或测试旁路读取

#### Scenario: 召回原文与提交字节一致

- **WHEN** 正式召回返回一条本回合新提交的事实
- **THEN** 其原文 SHALL 与已提交事实的字节一致
- **AND** MUST NOT 返回截断、重写或仅摘要形态的内容

#### Scenario: 存储失败不返回伪成功

- **WHEN** 事实提交路径遭遇写入失败、同步失败或存储不可用
- **THEN** 失败 SHALL 向调用方可见（具名错误或可判定失败状态）
- **AND** MUST NOT 返回成功，MUST NOT 把未落盘事实计为已提交

### Requirement: 索引失败保留可恢复欠账

事实已持久化但检索索引写入失败时，系统 SHALL 保留可恢复欠账（可重试的待索引记录或等价可判定状态），MUST NOT 静默遗漏该事实；后续恢复或重建 SHALL 能把该欠账补入索引。新事实进入索引 MUST NOT 使既有可达记录变得不可达。本要求只规定可达性底线；「旧记忆被新无关记录挤掉」的完整排序语义归另立变更，不在本能力内展开。

#### Scenario: 索引失败留可恢复欠账

- **WHEN** 事实已持久化而检索索引写入失败
- **THEN** 系统 SHALL 保留可判定的待索引欠账
- **AND** 后续恢复或重建 SHALL 能补齐该欠账，MUST NOT 静默遗漏

#### Scenario: 新事实不使既有记录不可达

- **WHEN** 一条新事实成功进入索引
- **THEN** 既有可达记录 SHALL 保持可达
- **AND** 本要求不改变既有排序语义，完整排序语义归另立变更

### Requirement: 正式召回工具返回新事实

声明式 recall 工具 SHALL 读取同一 durable 根经正式索引产生的结果；对本回合刚提交的事实，工具返回 MUST 包含其原文与来源引用，MUST NOT 依赖测试注入的旁路读取。召回路径 SHALL 保留既有权限与时间过滤语义，本要求不改写过滤规则。

#### Scenario: 正式召回工具返回新事实

- **WHEN** 模型在正式回合内调用声明式 recall 工具查询本回合新提交的事实
- **THEN** 工具 SHALL 返回该事实原文与来源引用
- **AND** 调用链 SHALL 为 `host.go` 的 `recall.New` → 声明式 recall 工具 → `Run.RecallReads()`，MUST NOT 为测试旁路

### Requirement: 冷启动恢复原文、归属与后续使用

进程正常关闭或进程中断后，以同一 durable 根重启 SHALL 恢复：事实原文、invocation/task 因果归属、工具调用与结果配对。恢复后下一个实际请求 SHALL 使用恢复材料（进入请求装配、召回或历史呈现），MUST NOT 仅恢复账本而下一请求不携带恢复材料。恢复 MUST NOT 依赖测试内存 store 冒充 durable 根，MUST NOT 建立第二套恢复状态仓。

#### Scenario: 正常关闭后恢复原文、归属与工具配对

- **WHEN** 进程正常关闭后以同一 durable 根重启
- **THEN** 系统 SHALL 恢复事实原文、invocation/task 因果归属与工具 call/result 配对
- **AND** 恢复材料 SHALL 来自该 durable 根

#### Scenario: 进程中断后恢复原文、归属与工具配对

- **WHEN** 进程被中断后以同一 durable 根重启
- **THEN** 系统 SHALL 恢复事实原文、invocation/task 因果归属与工具 call/result 配对
- **AND** MUST NOT 以测试内存 store 冒充 durable 根

#### Scenario: 下一请求使用恢复材料

- **WHEN** 重启完成且宿主收到下一个实际请求
- **THEN** 该请求 SHALL 携带恢复材料（进入请求装配、召回或历史呈现）
- **AND** 证据 SHALL 显示材料来自恢复而非仅账本可读

#### Scenario: 仅恢复账本不算完成

- **WHEN** 重启后账本可读但下一请求未携带恢复材料
- **THEN** 恢复 MUST NOT 计为完成
- **AND** 该状态 SHALL 以使下一请求缺材料的具名失败呈现
