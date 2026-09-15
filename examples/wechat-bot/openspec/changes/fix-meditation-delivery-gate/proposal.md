# fix-meditation-delivery-gate

## Why

wechat-bot 生产多次泄漏冥想输出到用户微信（03:20 冥想世系 task settle、04:52 纯冥想批 batch 9）。**根因已实证（2026-09-15 用户两次拍板澄清）**：看板快照以 user source 伪装注入（task_board.go 注释自供 "delivered as a standalone user-level input"，InjectBoard 以 RoleUser 追加）→ extractTriggerSource 优先级 1"真实 user 必胜"（event_loop.go:215 起）被合法压过 → main.go:374 读 StateDelta["trigger_source"]（context_manager.go RunFlow 执行期盖章）读到 "user" → 投递。e2195fc（读侧世系）/8bab6a4（写侧 Origin 盖章）机制本身成立，但被同批次伪装的 user source 压制。

**泄漏特征佐证**：泄漏全部发生在"内部回合恰有后台任务在跑"时（看板每轮注入→user source 必在批内→世系被压）；任务清闲时门禁正常——与"世系键缺失"假设不吻合、与"看板伪装"完全吻合。

**D0 原则（用户拍板）**：事件分类与投递路由是框架职责，在事件层闭合，与 LLM 收到什么 role 无关；禁止把"渲染为 user role"当门禁判定依据（那是被否掉的"由上下文形态判断"的变体）。

## What Changes

- 根因取证收尾（主体已实证）：钉死看板 user source 进入事件批的精确回流环节；提取泄漏 turn 日志佐证"看板在跑"前置；盘点同类系统注入点
- 框架层修复三件套：① 事件层为看板等系统注入建立独立类别，不冒充 user；② extractTriggerSource 按类别确定性导出——内部观察不参与"真实用户"判定、不覆盖世系；③ 回流事件携带内部观察类别（按取证结论落管道）
- 消息层 role 形态不动：看板继续以 RoleUser 呈现给 LLM（role 是模型侧形态，非门禁依据）；消费端 main.go switch 机制不变
- 覆盖两类泄漏输入：纯冥想批次与 meditation 世系 spawn 的 task settle——含"看板在跑"场景（三次泄漏共同前置）
- 回归验证三用例：纯冥想批+看板在跑不投递、冥想世系 task settle+看板在跑不投递、用户消息正常投递（防过修）
- 生产验证：修复部署后下一次"后台任务在跑"的冥想 turn 输出不再到达用户微信（轨迹+投递日志双重证据）

## Impact

- 代码：`tagent/agent/task/task_board.go`（RenderBoard/InjectBoard 注入语义或类别标注）、`tagent/agent/context_manager.go`（injectLiveTaskBoard:1211-1227、事件回流/入批管道、RunFlow trigger_source 盖章）、`tagent/agent/event_loop.go`（extractTriggerSource:215 起优先级链）、`tagent/agent/agent.go`（SessionHook:422-428 若涉回流）、`examples/wechat-bot/main.go`（消费端，预期不改但需确认兼容）
- 测试：`tagent/agent/meditation_test.go`、`tagent/agent/spawn_lineage_write_test.go`、`tagent/agent/metadata_propagation_test.go`、`tagent/agent/task/task_board_test.go` 及新增回归用例
- 运维：部署/重启序列（restart-tagent.sh / hotswap 路径）
- 不涉及仓库外文件；与并行计划 wechat-bot-reincarnation-notice（同动 main.go/注入链）需顺序协调
