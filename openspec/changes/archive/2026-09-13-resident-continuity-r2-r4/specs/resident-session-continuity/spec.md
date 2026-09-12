## ADDED Requirements

### Requirement: 常驻会话元数据完备并入事实链

常驻/交互 tmux 会话（named `n-*`、resident/interactive 模式）的 spawn 参数 SHALL 完备持久化：Name/Mode/Watch/Probe/ProbeIntervalSec 之外**新增 Command（原始命令）/Origin（来源上下文）/TaskID（关联任务）**，并经 `resident_session` 生命周期事件入事实链（spawn 带 full 元数据、终态带结局）——旁路同点，与 R1/R2 同模式。

#### Scenario: 会话 spawn 事件携带完整参数

- **WHEN** 启动一个 resident/interactive 会话
- **THEN** SHALL StoreEvent 一条 `resident_session` spawn 事件，载 Name/Mode/Command/Origin/TaskID/Watch/Probe 参数
- **AND** 运行期跟踪（monitor）照旧即时建立，事件为旁路

#### Scenario: 重启后会话参数可从事实链回读

- **GIVEN** 重启前有存活 named 会话
- **WHEN** 冷启动重挂
- **THEN** 会话参数（含原命令与关联任务）SHALL 可从事实链/元数据回读，SHALL NOT 因元数据缺字段而无法重建探测与 watch

### Requirement: 会话枚举修复（n- 与 prefix 不相交死代码）

`ListSessions` SHALL 同时收录默认 prefix（`tagent`）前缀**与** named 会话（`n-` 前缀）的会话（双条件过滤）；**orphan 语义重定义**：`CleanupOrphanSessions` SHALL 排除 `n-` 前缀会话（orphan=仅无主生成名会话）——否则 cleanup 先于 reattach 执行时会屠杀全部常驻会话。重挂 SHALL 仅在 entry agent 装配时执行一次（唯一挂载点，非 per-agent）；ResidentMeta 目录 SHALL 可配（`resident_meta_dir`，默认 /tmp）。

#### Scenario: 重挂枚举命中 named 会话

- **GIVEN** 存活 named 会话 `n-<logical>`（带元数据）
- **WHEN** 启动 `ReattachResidentSessions`
- **THEN** 该会话 SHALL 被枚举到并重建 monitor 跟踪（watch/probe 按元数据恢复），返回计数 ≥1

#### Scenario: fail-before——修复前枚举恒空

- **GIVEN** 同上场景但 ListSessions 仅按默认 prefix 过滤
- **THEN** 枚举结果 SHALL 为空（n- 不带默认 prefix）、重挂计数=0——证明修复承重

#### Scenario: cleanup 先行 n- 会话仍存活

- **GIVEN** 存活 named 会话，且 CleanupOrphanSessions 在 reattach 之前执行（装配时序）
- **WHEN** 构建 ActionTool（cleanup→reattach 序）
- **THEN** n- 会话 SHALL 存活（cleanup 排除 n-），重挂计数 ≥1

### Requirement: 存活探测三态化（dead/unknown/dead 可辨）

会话存活判定 SHALL 三态：**alive**（list-sessions 列出该会话）/ **dead**（命令成功且会话不在列表）/ **unknown**（命令 err）。探测 SHALL 以 `list-sessions` 为准（`has-session` 的 exit 1 不可辨 dead 与 unknown，实测 tmux 3.6a 同码）。monitor 层 SHALL NOT 将 unknown 直接当 dead 处理（err→assume-dead 屠杀路径加闸）：unknown SHALL 保留会话并计数，**连续 N 次**（可配，默认 3）unknown 才按 dead 处理并通知。

#### Scenario: 探测命令 err 不屠杀

- **GIVEN** 一个 running 任务的会话，探测命令连续 1-2 次返回 err（tmux server 抖动）
- **WHEN** monitor 周期探测
- **THEN** 会话 SHALL 保留（unknown 态计数），任务 SHALL NOT 被判 dead/屠杀
- **AND** 连续第 3 次（默认 N）unknown 才按 dead 走终态与通知

#### Scenario: 会话真死可辨

- **GIVEN** 会话已被外部 kill
- **WHEN** list-sessions 成功且不含该会话
- **THEN** 判定 SHALL 为 dead（非 unknown），立即走终态路径

### Requirement: 启动重挂与任务重关联

冷启动 SHALL 在投影重建（R1）与任务注册表重建（R2）之后调用 `ReattachResidentSessions`：存活 named 会话重建 monitor 跟踪；其关联任务（TaskID 桥）在重建 registry 中仍 active → 会话与任务重关联（探测恢复、watch 续期）；会话已死 → 任务走 settle 补偿。重挂 SHALL best-effort（单个会话失败不阻断启动）。

#### Scenario: 存活会话+active 任务重关联

- **GIVEN** 重启前任务 T（running）绑定会话 S（n-x，存活）
- **WHEN** 冷启动：registry 重建（T→suspect）→ 重挂（S 恢复跟踪）
- **THEN** T 与 S SHALL 重关联，T 经探测裁决恢复 running（或 S 死→T 走 settle 补偿）
- **AND** 后续 S 的输出/结算继续路由到 T

#### Scenario: 在途交互命令跨重启续用

- **GIVEN** 用户在 interactive 会话中的长任务跨重启存活
- **WHEN** 重挂完成
- **THEN** LLM SHALL 可经 resume（R2 声明式 spec）继续向该会话发送输入，SHALL NOT 因重启而失联
