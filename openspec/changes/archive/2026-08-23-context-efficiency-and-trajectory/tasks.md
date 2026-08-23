# Tasks: context-efficiency-and-trajectory

> 执行顺序 = 风险递增（design D7）。每节可独立提交、独立 revert。

## 1. 失真清理（零风险）

- [x] 1.1 删除 wiki 中对已删除兼容桥的引用（`docs/wiki/agent/agent-architecture.md` §三包结构与文件清单两处的 `task_alias.go`/`compress_alias.go` 行），并以"描述不存在之物"为判据全文扫描 wiki 补充清理
- [x] 1.2 删除空规格目录 `openspec/specs/ttl-cursor-scan/`；扫描其余空目录一并清理
- [x] 1.3 `docs/.dev/` 过程日志移出仓库（git rm；历史由 git log 承载）——实际仅 2 个文件被跟踪已移除，其余为未跟踪本地文件（已 gitignore）

## 2. 行为修正 · copy 层

- [x] 2.1 删除 exec 后台 ack 中"可用任务工具查询状态/结果"轮询暗示（`tool/action/action_tool.go` `buildAckResult`），保留"回写结果"语义；子 agent ack（`agent/tool_agent.go`）不动
- [x] 2.2 `RenderBoard` 尾部追加一行固定等待指引（`agent/task/task_board.go`）：有活跃任务时输出，文本不随任务数/年龄变化
- [x] 2.3 wechat-bot AGENTS.md：将只教 busy 场景的规则（"立即转去推进其他 track"条）**替换**为 busy/idle 双场景一条——idle 侧语义：所有可推进工作均在后台时，简短回复并结束回合，结算自动唤醒，禁止 sleep 类等待命令（`examples/wechat-bot/resources/prompts/AGENTS.md`）
- [x] 2.4 单测：ack 文案不含轮询表述且保留回写语义；看板指引行有/无活跃任务两态与固定性

## 3. task_settled 紧凑单行组装

- [x] 3.1 重写 `newTaskSettledEvent`（`agent/event_bus.go`）：单行轨迹形态（✓/✗/∞/⚠ + desc 截断 60 字符 + 8 位短 id + 结果段），换行转义 `␤`；failed 附错误截断
- [x] 3.2 内联上限为编译期命名常量（初始 600 字符）：≤ 常量单行内联；> 常量走既有 tool-output 转储 + `settleInlineTail` 尾部预览；**删除** `maxOutputChars = MaxTokens/2*4` 派生公式在本路径的使用
- [x] 3.3 单测：trivial 结果单行内联（无空行/独立 UUID 行）；5000 字符结果转储+路径+预览；信息无损字段断言（desc/状态/id/错误/结果或路径齐备）；failed/alive-detached/suspect 三态
- [x] 3.4 回归：`tests/` 中依赖 settle 文本格式的既有用例更新断言（contracts_llm_test 夹具同步为单行格式）

## 4. speak/draw stub 删除

- [x] 4.1 删除 `tool/speak/`、`tool/draw/` 两包与 `resources/prompts/{speak,draw}_{agent,tool_desc}.md` 四个 prompt 文件
- [x] 4.2 `builtinAgentNames` 移除 speak/draw 两个保护位（`tagent.go`）；`builtin_agent_protection_test.go` 移除对应用例
- [x] 4.3 核查注册表/文档残留引用（另发现并清理：示例 tagent.yaml 两处 tool-ref+两个 agent 定义、`action_agent.md` 路由表、`tool-architecture.md` 包结构、`prompts_embed_test.go` 枚举）；愿景建议开 issue 留痕
- [x] 4.4 验证：`go build ./...` + `go vet ./...` + 相关包测试通过；`builtinAgentNames` 恰为 {knowledge, recall, action}

## 5. 旋钮节食（定常化 4 个）

- [x] 5.1 `max_tool_result_chars` / `max_exec_state_chars` / `chunk_summary_len` / `max_notice_chars` 四个配置项删除（`config.go`），默认值转为 `agent/compress` 包级命名常量；`WithMaxToolResultChars` 等 option 与 `CompressConfig` 对应字段删除（另清理死代码 `WithMaxToolArgsChars`）
- [x] 5.2 yaml/README/wiki 中对应配置说明清理；wechat-bot tagent.yaml 注释行清理
- [x] 5.3 验证：compress 包测试改用常量断言；全量测试通过

## 6. legacy 压缩管线删除（最大回归面）

> **【已完成】** 采用“整文件重构”策略（仅保留 KEEP 区段，现役骨架管线逐字不动）一次性清除 legacy，编译器+全量测试把关。实测规模与结果：
> - **smart_compress.go** 从 1440 行精简至 ~380 行；删除 `compressLegacy` + 15 个 legacy helper + 字段 `skeletonSegmentation`/`archiveCache*`/`maxSummaryInputChars`/`memStore`/`projection` + 死权重截断字段 `maxToolResultChars`/`maxExecStateChars`/`maxToolArgsChars`/`maxNoticeChars`/`chunkSummaryLen` + option `WithSkeletonSegmentation`/`WithArchiveCacheCap`/`WithMaxSummaryInputChars`/`WithMemStore`/`WithProjection`。
> - **保留共享 helper**（现役 `condenseCardLines` 依赖）：`generatePlainSummary`、`effectiveSummaryMaxTokens`、`WithSummaryMaxTokens`、`SplitSystemMessage`、`splitRollingSummaryMessage`。
> - **`Compress` 签名简化**：去掉未用的 `inv` 参数，调用方 `context_compressor.go` 同步。
> - **legacy 测试删除**：`archive_curation_test.go`（整文件）、`smart_compress_test.go` 重构为仅保留有效用例（SplitSystemMessage/ParseEventKeyAndType/EventTypeToRole + 共享 mock）、`task_segmenter_test.go` 删 legacy 用例 + `segmentMessagesByUser` 函数。
> - **配置级联**：删 `skeleton_segmentation`/`archive_cache_cap`/`max_summary_input_chars` 三配置项 + `agent.CompressConfig` 对应字段 + `newContextManagerFromConfig` 的 `WithMemStore`/`WithProjection` 接线；`defaults.go` 清理 8 个孤儿常量。
> - **回归**：`go test ./... -short` 全包绿。

- [x] 6.1 删除 `compressLegacy` 路径与 `skeletonSegmentation` 分支（`agent/compress/smart_compress.go`）：`Compress` 直接走骨架管线；删除 `WithSkeletonSegmentation` option
- [x] 6.2 级联删除：`segmentContentHash` 归档缓存（archiveCache/archiveCacheCap）与 legacy L3 LLM 段摘要路径、`WithArchiveCacheCap`/`WithMaxSummaryInputChars` option、`skeleton_segmentation`/`archive_cache_cap`/`max_summary_input_chars` 三个配置项（`config.go`）；确认 `context_compress_summary` 固化物无新产生源
- [x] 6.3 残留符号清查：`grep -rn "compressLegacy\|skeletonSegmentation\|archiveCache\|segmentContentHash\|level3Failed"` 于非测试代码应为空；legacy 专属测试删除
- [x] 6.4 回归：`go test ./agent/compress/ ./agent/ ./tests/ -short`；`context_simulation_test` 全生命周期仿真通过（确认模型可见结构不因管线删除而劣化）
- [x] 6.5 变更说明记录：已删配置键经非严格 YAML 解析自然忽略；曾用 `skeleton_segmentation: false` 的部署静默落到骨架管线

## 7. 规格清偿与验证

- [x] 7.1 归档时将本 change 五个 delta 并入主 specs：`value-driven-compression` 主规格文件删除（delta 已含 REMOVED 理由）——由 `openspec archive` 自动完成
- [x] 7.2 新增真实 LLM 契约测试（`tests/`，-short 门控）：复现"仅剩一个后台任务在跑、无其他独立事项"场景，断言模型不调用 exec sleep 类命令、以最终回复结束回合（`TestContract_WaitScenario_NoSleepSpin` / C10）
- [ ] 7.3 实机验证（wechat-bot 部署观察）：自旋消失、settle 通知单行化；记录 settle 通知平均长度与 read_file 频率（内联常量 600 的校准输入）——需部署环境，本会话不可达
- [x] 7.4 文档同步：README/README_EN（compress 配置表去 skeleton_segmentation/archive_cache_cap，补 summary_max_tokens）、wiki agent/event-flow/plugin 章节 legacy 管线说明更新；归档前核对 delta 全部兑现且主 specs 与实现一致（防 I6 重演）
