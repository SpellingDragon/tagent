# task-skeleton-compression Delta

## ADDED Requirements

### Requirement: 压缩触发器多维化（token 阈值或完整段超龄）

`ContextCompressor.Compress` SHALL 在 token 阈值之外，增加**完整任务段超龄**作为独立触发维度：当 refs 中 `agent_output` 段边界计数（完整任务回合数）超过 `keepRecent` 时，即使 `usedTokens <= threshold` SHALL 调用 `SmartCompressor.Compress`。完整段计数 SHALL 复用现有的 ref 遍历统计（零额外扫描成本），SHALL NOT 通过每轮全量分段来获得。

#### Scenario: 完整段超龄时即使 under budget 也触发

- **GIVEN** 会话有 5 个完整任务回合（keepRecent=2），`usedTokens` 低于阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL 调用 `SmartCompressor.Compress`（此前因 under budget 直接返回，压缩从未运行）
- **AND** 老回合 SHALL 按段龄被 L1 丢弃 tool 事件、L3 折叠进滚动摘要

#### Scenario: 完整段未超龄时短路返回

- **GIVEN** 会话仅有 1 个完整任务回合，且 `usedTokens` 低于阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL NOT 调用 `SmartCompressor.Compress`，直接返回原始 refs

#### Scenario: token 阈值触发保持有效（回归）

- **GIVEN** `usedTokens` 超过阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL 调用 `SmartCompressor.Compress`（与既有行为一致）

### Requirement: LLM 文摘作为卡片之上的可选叠加层

骨架压缩（`compressSkeleton`）是纯工程、零 LLM：L3 = 整段离场→`buildRetainedRefs` 折叠成滚动摘要卡片，**不做 LLM 段摘要**。骨架管线的 LLM 文摘是 `condenseCardLines`（`curateCards` 内）：卡片超 `cardMaxChars` 时 SHALL 用 summary 模型浓缩较旧一半卡片、保留最新卡片原文；无模型时 SHALL 将最旧行沉底为计数（不报错）。LLM 段摘要 + `segmentContentHash` 归档缓存（同内容复用、落库 `context_compress_summary` 固化物、TTL 豁免）是 `compressLegacy`（`skeleton_segmentation:false` 回退路径）独有。所有 LLM 生成的文摘/浓缩内容 SHALL 保留 `[evt_key]` 召回票据，使卡片始终是召回锚点。

#### Scenario: condenseCardLines 浓缩旧卡且保留票据

- **GIVEN** 滚动摘要卡片超过 `cardMaxChars` 且配置了 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 浓缩较旧一半卡片、保留最新卡片原文与 `[evt_key]` 票据

#### Scenario: 无模型时沉底计数不报错

- **GIVEN** 卡片超 `cardMaxChars` 但无 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 将最旧行沉底为 `(earlier n items)` 计数，SHALL NOT 报错

#### Scenario: legacy L3 摘要经归档缓存复用（素材律）

- **GIVEN** `skeleton_segmentation:false` 回退路径下，一个 L3 段内容未变
- **WHEN** 再次执行压缩
- **THEN** SHALL 复用 `segmentContentHash` 归档缓存的摘要，SHALL NOT 重复调用 summary 模型

## MODIFIED Requirements

### Requirement: 骨架仍超预算时触发多段压缩归档

当骨架（L2 仅保留边界事件）仍超预算时，SHALL 触发多段压缩归档（L3）：最老段整段离场并由 `buildRetainedRefs` 折叠进滚动摘要。L3 段摘要 SHALL 优先复用 `segmentContentHash` 归档缓存，缓存未命中且配置了 summary 模型时调用模型生成，模型缺失时标记 `level3Failed` 并回退为纯骨架折叠（不视为错误降级）。

#### Scenario: L3 优先归档缓存，模型缺失回退骨架

- **GIVEN** 一个 L3 段，归档缓存未命中且无 summary 模型
- **WHEN** 执行多段压缩
- **THEN** SHALL 以纯骨架方式折叠该段进滚动摘要，标记 `level3Failed`，SHALL NOT 报错或中断压缩
