## Why

R1（对话上下文跨重启连续）/R2（任务重启接续）/R3（超长运行+异步/交互命令）/R4（配置描述的全运行时**非重启**热更新）不是四个特性，而是**一个架构的两面**。经代码验证（2026-09-12）收敛为一条统一原则：

> **持久事件溯源状态 ⊥ 可热换无状态执行器。** 状态（投影/任务/异步常驻）由长生命周期持有者（cm/TagentAgent）**事件溯源**持久、置于执行器**之外**；执行器（fwAgent+runner+tools）是「配置→turn 行为」的**无状态函数**，可从配置重导出并**原子热换**。两种扰动一套地基：崩溃/升级（重启）→ 状态从事实链重建（R1/R2/R3）；配置演进（热更）→ 换执行器、状态原封流过（R4）。

验证锚点：文档铁律「事实链只在 KV 里」(:1069)、「同点投影」(:1060)；R1 缺口=折叠产物负 key 不落库（:983）+ path-dependent 不可重算；R2 缺口=任务板「never persisted」（task_board.go:21）+ TaskSpec 闭包不可序列化 + TaskManager 困在 ActionTool（执行器内）；R3 缺口=resident_recovery 生产死代码（n-* 会话被 prefix 过滤）+ ResidentMeta 缺 command/origin/task_id；R4 缺口=cm.runner 普通字段（:47）不可换、fwAgent 未持有（:386）、WithTools 构造期烘死（:359）无 mutator、incremental B 画饼未建。**雏形已有**：SwappableModel（原子换内芯、in-flight 用旧）、MCP registry（稳定 meta-tool+热同步间接缝）、prompt.Source、incremental A。

业界对齐（Cordis 不重启自改装 / RHI Harness 热换自进化 / Reef 灰度回滚 / Teamwork 动态组队）但**不搬插件框架**——tagent 的状态/执行器天然分离 + 逐 turn 执行器调用 + SwappableModel 模式已够，R4 = 把该模式**上提到执行器层**（一个缝足矣），且事件溯源史解除 Cordis「必须无产出才能换」约束（中途换工具安全：历史是不可变事实）。前置事实：R2/R3 把状态搬出执行器是 R4 无损热换的前提（换执行器≠丢任务板/在途命令）。

## What Changes

本变更自身不修改运行时代码，交付伞形路线图（承接 2026-09-06-tagent-evolution-roadmap 的治理形态）：

- **统一架构原则与不变量**（specs/resident-continuity）：状态与执行器分离不变量、状态层事件溯源模式（R1 立范、R2/R3 沿用）、执行器可换语义（R4）。
- **三阶段路线图（依赖排序）**，每阶段派生独立子变更执行：
  - **R1 上下文连续**（进行中）：`event-sourced-projection`（已 apply-ready）——压缩折叠=事实链 compaction 事件、投影=事实链纯回放（snapshot+tail）。**模式立范者**。
  - **R2 任务连续 + R3 异步/常驻连续**（R1 后并行）：任务生命周期事件溯源 + 声明式 TaskSpec + TaskManager 外提到 cm；常驻会话态事件溯源（ResidentMeta 补全）+ 修 resident_recovery 枚举死代码 + 重挂存活 tmux + monitor fail-dead 修复。
  - **R4 非重启全量热更**（R2/R3 后）：SwappableExecutor——cm.runner 原子换缝（SwappableModel 上提）+ config diff 二分（行为变更走既有细粒度热同步/结构变更重建执行器）+ build-validate-then-swap（失败不换、可逆）+ turn 边界换入（in-flight 用旧）。分 A（行为热同步既有面固化）/B（结构换缝）/C（可逆治理+回滚）增量子变更。
- **共享约定**（R1 确立、R2/R3/R4 沿用）：状态=事实链旁路产物、重建=**事件回放进空态**（snapshot+尾部回放是 R1 特例——其状态层有折叠压缩；无压缩语义的状态层如 R2 任务板为纯全量回放，勿预设须有 snapshot 事件）、状态外置于执行器、可换缝=SwappableModel 模式。
- **预留确认项**（执行到对应阶段重新核对，不阻塞批准）：见 design.md。

## Capabilities

### New Capabilities
- `resident-continuity`: 常驻连续性架构不变量与路线图治理——状态/执行器分离不变量、状态层事件溯源模式、执行器可换语义、阶段依赖与准出门禁（fresh-eyes 前置、fail-before 回归）。

### Modified Capabilities
<!-- 无：各阶段子变更自行修改对应能力（如 task-skeleton-compression 由 R1 修改；R2/R3/R4 各自的新能力），本伞形变更只立不变量与治理 -->
