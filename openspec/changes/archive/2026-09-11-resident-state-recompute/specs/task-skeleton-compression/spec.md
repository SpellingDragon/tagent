## MODIFIED Requirements

### Requirement: LLM 文摘作为工程层之上的可选叠加层

骨架压缩（`compressSkeleton`）的定级与丢弃是纯工程、零 LLM；L3 折叠的工程票据层亦恒在。管线中的 LLM 文摘恰有两处，均为可选叠加层，失败或无模型时 SHALL 降级为工程形态：

1. **L3 滚动综述 `synthesizeRollingNarrative`**：折叠点的增量单行综述（见"多段压缩归档"需求）；
2. **卡片浓缩 `condenseCardLines`**（`curateCards` 内）：卡片超 `cardMaxChars` 时 SHALL 用 summary 模型浓缩较旧一半卡片、保留最新卡片原文；无模型时 SHALL 将最旧行沉底为计数（不报错）。

所有 LLM 生成的文摘/浓缩内容 SHALL 保留 `[evt_key]` 召回票据，使卡片始终是召回锚点。

**滚动综述持久化（本变更新增）**：L3 滚动综述是**不可重算**的 LLM 增量合成（旧综述 + 新折叠素材 → 新综述，非确定性），是重启后逐字节重建上下文时唯一无法从 WAL 重算的块。故 `synthesizeRollingNarrative` 产出新综述时 SHALL 将其持久化为一条正 key `context_compress_summary` 事件（TTL 豁免、可召回），载综述逐字节 + 折叠 key 区间；滚动 SHALL supersede（墓碑上一条综述事件，只留最新活）。此为对 `context-efficiency-and-trajectory`「`context_compress_summary` 固化物产生源移除」的**定向逆转**——按减法判定标准（删除后行为信息变少 = 缺失补最小量），narrative 属「缺失」而非冗余副本。**scope 严格限于 narrative**：卡片行等可重算工程产物 SHALL NOT 落库（仍走 `[evt_key]` 票据 recall 回补），不重蹈 legacy 固化物冗余覆辙。

（`compressLegacy` 管线、其 LLM 段摘要、`segmentContentHash` 归档缓存仍随 legacy 移除；`context_compress_summary` 的产生源由本变更**仅为滚动综述**恢复。存量固化物读路径容错、TTL 自然清退不变。）

#### Scenario: condenseCardLines 浓缩旧卡且保留票据

- **GIVEN** 滚动摘要卡片超过 `cardMaxChars` 且配置了 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 浓缩较旧一半卡片、保留最新卡片原文与 `[evt_key]` 票据

#### Scenario: 无模型时沉底计数不报错

- **GIVEN** 卡片超 `cardMaxChars` 但无 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 将最旧行沉底为 `(earlier n items)` 计数，SHALL NOT 报错

#### Scenario: 滚动综述持久化为 context_compress_summary 事件

- **GIVEN** 配置了 summary_model 且 L3 折叠触发 `synthesizeRollingNarrative` 产出新综述
- **WHEN** 新综述合成完成
- **THEN** SHALL `StoreEvent` 一条正 key `context_compress_summary` 事件（TTL 豁免），载综述逐字节 + 折叠 key 区间
- **AND** SHALL 墓碑上一条综述事件（滚动 supersede，只留最新活）
- **AND** 卡片行等可重算产物 SHALL NOT 落库

#### Scenario: 无 summary_model 时不产生综述固化物

- **GIVEN** 未配置 summary_model（综述层降级为纯工程，无 narrative）
- **WHEN** L3 折叠
- **THEN** SHALL NOT 产生 `context_compress_summary` 事件（无可持久化的不可重算块）
