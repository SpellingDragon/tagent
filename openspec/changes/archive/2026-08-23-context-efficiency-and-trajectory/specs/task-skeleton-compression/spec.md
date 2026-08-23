# task-skeleton-compression Delta

## MODIFIED Requirements

### Requirement: 骨架仍超预算时触发多段压缩归档

当骨架（L2 仅保留边界事件）仍超预算时，SHALL 触发多段压缩归档（L3）：最老段整段离场并由 `buildRetainedRefs` 折叠进滚动摘要卡片。骨架管线为**唯一压缩管线**：L3 是纯工程折叠（零 LLM、零归档缓存），SHALL NOT 存在 legacy 回退路径（`skeleton_segmentation` 配置及其 L3 LLM 段摘要、`segmentContentHash` 归档缓存、`level3Failed` 降级标记随 legacy 管线一并移除）。

#### Scenario: L3 纯工程折叠进滚动摘要

- **GIVEN** 骨架化后仍超预算，最老段被定为 L3
- **WHEN** 执行多段压缩
- **THEN** 该段 SHALL 整段离场并由 `buildRetainedRefs` 折叠为滚动摘要卡片行
- **AND** 全程 SHALL NOT 调用 LLM，SHALL NOT 读写归档缓存

### Requirement: LLM 文摘作为卡片之上的可选叠加层

骨架压缩（`compressSkeleton`）是纯工程、零 LLM：L3 = 整段离场→`buildRetainedRefs` 折叠成滚动摘要卡片，**不做 LLM 段摘要**。管线中唯一的 LLM 文摘是 `condenseCardLines`（`curateCards` 内）：卡片超 `cardMaxChars` 时 SHALL 用 summary 模型浓缩较旧一半卡片、保留最新卡片原文；无模型时 SHALL 将最旧行沉底为计数（不报错）。所有 LLM 生成的文摘/浓缩内容 SHALL 保留 `[evt_key]` 召回票据，使卡片始终是召回锚点。

（`compressLegacy` 管线、其 LLM 段摘要、`segmentContentHash` 归档缓存与 `context_compress_summary` 固化物产生源已随本变更移除；存量固化物读路径容错、TTL 自然清退。）

#### Scenario: condenseCardLines 浓缩旧卡且保留票据

- **GIVEN** 滚动摘要卡片超过 `cardMaxChars` 且配置了 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 浓缩较旧一半卡片、保留最新卡片原文与 `[evt_key]` 票据

#### Scenario: 无模型时沉底计数不报错

- **GIVEN** 卡片超 `cardMaxChars` 但无 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 将最旧行沉底为 `(earlier n items)` 计数，SHALL NOT 报错
