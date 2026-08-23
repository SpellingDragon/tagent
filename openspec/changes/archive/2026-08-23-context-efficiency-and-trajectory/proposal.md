# Proposal: context-efficiency-and-trajectory

## Why

2026-08-23 wechat-bot 实机日志暴露一个行为缺陷与一个结构性病灶的叠加：

1. **自旋等待（行为）**：模型等待后台 plan 任务时发明 `exec(sleep N)` 定时器——6 轮、150s、每轮 ~86K token。直接诱因是 exec ack 中某次为"教任务工具"而**加上**的轮询暗示（"可用任务工具查询状态/结果"），叠加 AGENTS.md 只教 busy 场景（"立即转去推进其他 track"）留下的 idle 盲区。**加法造的 bug。**
2. **项目熵增（结构）**：双压缩管线（骨架+legacy）、11 个压缩旋钮（唯一真实用户的配置里 9 个只以注释存在）、speak/draw stub 占据内置名保护位、wiki 宣称已被删除的兼容桥、空规格目录、零实现却完整存在的 value-driven-compression 规格。单人 + AI 高速迭代把加法成本压到近零、把减法成本（判断力）留在原地——熵增速率高于常规项目。

**方法论文述**：本变更以减法为主要工具。判定标准：一句话/一个路径/一个旋钮删掉后**行为信息是否变少**——不变则为冗余该删；变少则为缺失该补（缺失只能补不能删）。

## What Changes

### A. 行为修正（减法版）

- **删除** exec ack 中的轮询暗示（"可用任务工具查询状态/结果"），不新增框架层教学文案——保留的"回写结果"语义本就正确；子 agent ack 不动
- **新增一条防回归约束**：后台 ack SHALL NOT 引导轮询式查询/等待（防止未来再加回去）
- 任务看板尾部**一行**固定等待指引（copy，非结构）
- wechat-bot AGENTS.md：**替换**只教 busy 场景的不完整规则为覆盖 busy/idle 双场景的一条（应用层，非框架）
- task_settled 组装为**紧凑单行轨迹形态**：状态标记 + desc 截断 + 短 id + 结果（编译期常量内联上限，超限走既有转储管线+尾部预览）；同时**删除** `MaxTokens/2*4` 派生的内联上限公式（256K 字符不是任何人的决策，是公式事故）

### B. 结构熵减

- **文档失真清理**：删除 wiki 中对已删除兼容桥（task_alias/compress_alias）的引用等"描述不存在之物"的内容
- **残留清理**：空规格目录（specs/ttl-cursor-scan/）；tool-lifecycle-management 规格中的 read/write 幽灵 agent 名
- **speak/draw stub 删除**：两个 stub 包、内置名保护位、4 个 prompt 文件、保护测试用例（愿景不进主干）
- **legacy 压缩管线删除**：`compressLegacy`（skeleton_segmentation:false 回退路径）、其 L3 LLM 段摘要与归档缓存、连带 3 个旋钮（skeleton_segmentation / archive_cache_cap / max_summary_input_chars）——骨架管线成为唯一管线
- **旋钮节食 11→4**：从未被真实调过的 4 个（max_tool_result_chars / max_exec_state_chars / chunk_summary_len / max_notice_chars）定常化；保留有使用证据的 4 个（card_max_chars / compact_keys_listed / recent_full_count / summary_max_tokens）+ summary_model/provider
- **规格清偿**：撤回零实现的 value-driven-compression；并清除因 legacy 管线删除而失效的 batched-summarization / l3-archive-summarization（整体撤回）与 compress-quality-fix 中的 2 个 legacy 需求（防 I6 重演；git 历史可找回）
- **docs/.dev 过程日志移出仓库**（历史在 git log 中）

### 明确不做

- 不新增 sleep/wait 拦截代码（降级为观察后备：实机若仍有自旋再独立立项）
- 不新增 wait 独立工具（用户决策）
- 不新增 `settle_inline_chars` 配置旋钮与"0=回退旧格式"逃生门
- 不实现 value-driven 压缩（撤回而非兑现）

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `async-task-execution`: ack 防轮询约束（SHALL NOT 引导轮询式查询/等待）
- `task-registry-and-board`: task_settled 紧凑单行轨迹形态（常量内联上限，向既有"事件仅放摘要"要求收敛）；看板尾部等待指引行
- `task-skeleton-compression`: 删除 legacy 回退路径条款——骨架管线为唯一管线，legacy 专属要求与场景移除
- `tool-lifecycle-management`: 内置 agent 保护名单去除 speak/draw，修正 read/write 幽灵名
- `compress-quality-fix`: 仅移除 2 个依赖已删 legacy 摘要管线的需求（generateSummary 重摘 / summarizeBatch 截断）；投影解析类需求（resolveReferenceToMessage 等）描述现役代码，保留

### Removed Capabilities

> 以下三个能力已整体撤回：其主 spec 文件（`openspec/specs/<name>/spec.md`）**直接删除**（避免留下零需求空 spec 触发 openspec 校验），撤回理由与 Migration 见本 proposal 与 design D6；git 历史可找回。

- `value-driven-compression`: 零实现规格（I6 存量样本），主 spec 已删
- `batched-summarization`: legacy 分批 LLM 摘要能力随管线移除，主 spec 已删
- `l3-archive-summarization`: legacy L3 逐类型摘要/归档能力随管线移除，主 spec 已删

## Impact

| 位置 | 变更 |
|------|------|
| `tool/action/action_tool.go` | 删 ack 轮询句 |
| `agent/task/task_board.go` | 尾部指引行 |
| `agent/event_bus.go` | settle 单行组装 + 常量上限（删派生公式） |
| `agent/tool_agent.go` | 不动（ack 已正确） |
| `agent/compress/smart_compress.go` | 删 compressLegacy 路径与归档缓存 |
| `config.go` / `tagent.go` | 删 7 个配置项；builtinAgentNames 减 2 |
| `tool/speak/` `tool/draw/` + prompts + 保护测试 | 删除 |
| `examples/wechat-bot/resources/prompts/AGENTS.md` | busy→busy/idle 规则替换 |
| `openspec/specs/` | 4 个 delta + 1 个撤回 |
| `docs/wiki/` + `docs/.dev/` | 失真修正 + 移出 |

净效果目标：**新增代码路径 0、新增配置 0、净行数为负**。无外部消费者（单人项目），已删配置键经非严格 YAML 解析自然忽略，文档记录迁移即可。
