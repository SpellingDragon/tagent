## MODIFIED Requirements

### Requirement: LLM 文摘作为工程层之上的可选叠加层

骨架压缩（`compressSkeleton`）的定级与丢弃是纯工程、零 LLM；L3 折叠的工程票据层亦恒在。管线中的 LLM 文摘恰有两处，均为可选叠加层，失败或无模型时 SHALL 降级为工程形态：

1. **L3 滚动综述 `synthesizeRollingNarrative`**：折叠点的增量单行综述（见"多段压缩归档"需求）；
2. **卡片浓缩 `condenseCardLines`**（`curateCards` 内）：卡片超 `cardMaxChars` 时 SHALL 用 summary 模型浓缩较旧一半卡片、保留最新卡片原文；无模型时 SHALL 将最旧行沉底为计数（不报错）。

所有 LLM 生成的文摘/浓缩内容 SHALL 保留 `[evt_key]` 召回票据，使卡片始终是召回锚点。

**折叠即事实链 compaction 事件（本变更新增）**：折叠产出的 composed 综述 ref（负 key、仅投影）含**两处 LLM 不可重算产物**——滚动综述 narrative 与浓缩后卡片文本——且折叠 path-dependent（`deterministicLevel` 的 age 依赖累积投影段数）、并存负 key `tool_chain` 合成 ref（store 无对应事件），故整条折叠态**不可从原始事件重算**。为兑现「投影 = 事实链纯回放」（见 `event-sourced-projection` 能力）并支持重启逐字节重建，折叠点 SHALL 向事实链**追加一条 `context_compress_summary` 正 key compaction 事件**（载综述正文逐字节 + 综述 ref 身份 + 有序 retained-ref 列表含 tool_chain 全身份+EventSummary + fullBoundary），并滚动 supersede 上一条。此为对 `context-efficiency-and-trajectory`「`context_compress_summary` 固化物产生源移除」的**定向逆转**——按减法判定标准（删除后行为信息变少 = 缺失补最小量）：累积综述不可重算、prefix-cache 重启要求逐字节 → 属「缺失」而非冗余副本。**scope**：持久化的是折叠态（composed 综述含浓缩卡片 + retained-ref 列表含 tool_chain 合成 ref）；被折叠的**原始事件不删**（冷存，`[evt_key]` 票据仍 recall 回补），不因 compaction 事件而移除原文。

（`compressLegacy` 管线、其 LLM 段摘要、`segmentContentHash` 归档缓存仍随 legacy 移除；`context_compress_summary` 的产生源由本变更**为 compaction 事件**恢复，载体是折叠态（composed 综述 + retained-ref 列表），非 legacy 逐段原文副本。存量固化物读路径容错、TTL 自然清退不变。）

#### Scenario: condenseCardLines 浓缩旧卡且保留票据

- **GIVEN** 滚动摘要卡片超过 `cardMaxChars` 且配置了 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 浓缩较旧一半卡片、保留最新卡片原文与 `[evt_key]` 票据

#### Scenario: 无模型时沉底计数不报错

- **GIVEN** 卡片超 `cardMaxChars` 但无 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 将最旧行沉底为 `(earlier n items)` 计数，SHALL NOT 报错

#### Scenario: 折叠点追加 compaction 事件

- **GIVEN** 一次压缩触发真折叠、产出新 composed 综述 ref
- **WHEN** 折叠完成
- **THEN** SHALL StoreEvent 一条 `context_compress_summary` 正 key compaction 事件，载综述正文逐字节 + 综述 ref 身份 + 有序 retained-ref 列表（含负 key tool_chain 合成 ref 全身份+EventSummary）+ fullBoundary
- **AND** SHALL DeleteEvent 上一条 compaction 事件（滚动 supersede）
- **AND** 被折叠的原始事件 SHALL NOT 被删除（冷存供 recall 票据回补）

#### Scenario: 无 summary_model 时折叠态仍持久化

- **GIVEN** 未配置 summary_model（综述层降级为纯工程，无 LLM narrative）
- **WHEN** 折叠产出 composed 综述 ref（纯工程卡片 + 计数）
- **THEN** compaction 事件 SHALL 仍追加（composed 正文 + retained-ref 列表），使重启逐字节重建不依赖是否有 LLM（工程折叠态同样 path-dependent 不可重折，须持久化）
