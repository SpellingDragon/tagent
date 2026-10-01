## Why

dev（43 提交）合入 origin/main 前的分主题并行评审发现 2 项 P0（主规格与实现相反）与 14 项 P1、29 项 P2。合入目标不是"修到可接受"，而是**让 main 达到干净的理想形态**：全部已知发现一次清零、specs 与实现零矛盾、谱系判定与通知识别收敛为单一真源。项目处于 pre-release（无外部消费者），允许破坏性变更；不承诺对旧盘上数据的兼容迁移——新代码冷启动从事实链干净重建即视为合格。

## What Changes

**规格对齐（P0×2）**
- `event-sourced-projection` 主 spec 无锚恢复条款按现行实现（fallbackCap=0，b871d30 裁决）重写为全量复原语义。
- `event-segment-store` 主 spec 移除两条已被归档 delta 声明 REMOVED 的 WAL 系条款，落"KV Sync=原子快照屏障"真契约（含分区快照语义）。

**实现修复——机制完善型（P1 主体 + P2 补齐）**
- agent 核心运行时：SetAuditLine 死接线归位；execution gate 三个失败点以失败 Response 呈现；subagent 拒绝路径补 liveCM 注销；settle 路由 teardown 窗口补终态排空；QuarantineEnvelope 材料读失败补租约释放兜底；构造失败路径补 CloseDurable。
- 常驻可靠性：退役谱系改挂结算信号（Spec.Origin 恢复 spawn 后不可变）；批量折叠改为**产生侧分流**（有 invocation 归属的条目不进批，批内天然只剩无主结算，宿主回调零改动）；quarantine rename 补 dirsync 与错误传播、容量扣减与隔离成败绑定；ClaimNext 补锁内 closed 复查；DefaultTTL 注释对齐"永开无禁用"哲学；reconcileTTL 对恢复任务（detector=nil）补会话回收路径。
- compress：折叠豁免推广为 run 级（run 含 Active 成员整 run 原样保留）；单条票据分支补 Timestamp==0 防护。
- memory：spill 租约释放绑定"行已落盘移除"；finalizeTombstones 在 idx 删除失败时保留墓碑；deleteSegments 清理失败聚合上报；locateOrphanEvtSlot 对 ListSegments 失败 fail-loud；store 构建失败路径补 relation store 释放。
- rl/周边：swappable sweepRetired 陈旧读归位（current 读入写锁内）；trajectory 通道路径补 (nil,nil) 防护；resume 新建 detector 重注册 monitor 回调；restart 脚本归档截断移至新旧进程交接窗口。

**实现修复——语义收敛型（BREAKING，方向=更彻底的既有哲学）**
- **谱系判定白名单同源化**：投递门扣留判定与 compress 外显判定收敛为同一 deliverable 白名单（白名单外一律内部，fail-closed），消除双负清单手工同步（`internalLineageValues` 与投递门清单）这一熵增源。
- **结算通知结构化识别**：事件源头写入 `settle_notice` 结构化标记，投影侧识别只认标记；无标记的旧事件不折叠（原样保留，方向安全）。正文前缀 `[task settled` 启发式退役。
- **LocalFileKV 分区快照**：快照按分区分片，Sync 屏障只重写 dirty 分区——事件级屏障语义不变，写放大从 O(全库) 降为 O(分区)。旧单文件 kv.json 不迁移，冷启动空库重建。
- **死面清理**：`DiagnosticsSnapshot.WALQuarantined` 字段及其测试桩、`memory.fsync` 配置键与 `WithFSync` 选项（恒为 no-op 的死旋钮）直接删除——配置面不留谎言实体。

**工程化与文档**
- comment-only wrapper 放行删除侧给 codetools 硬拒（防线回工具层单点）；lint.sh 成功打印移到全部门后；soak 父进程显式 timeout。
- 文档全量对齐：README"零必填配置"纠正、rl-architecture 自相矛盾段、compression-and-telemetry 截断表述、storage-durability-positioning"码面事实"、归档 evidence.md 个人绝对路径脱敏、wiki 索引重复段清除、全部 TBD Purpose 回填。
- 注释卫生：7+ 处批量重写错乱前缀人工校正、SessionError Godoc 对齐失败极性主载体语义。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `event-sourced-projection`: 无锚恢复 Requirement 从"保留最新 500 条（partial/truncated_events）"改为"过滤后全量复原、无截断上限"。
- `event-segment-store`: REMOVED 两条 WAL 系条款；ADDED"LocalFileKV Sync 为原子快照屏障"条款（含分区分片语义）。
- `async-task-lifetime`: 批量退役 Requirement 显性化——折叠域为产生侧定义（有 invocation 归属的退役条目不进批、仍走逐条路由与记账释放）；谱系戳挂结算信号、不写任务 Origin；Resume 恢复轮按恢复语境取谱系。
- `persistent-event-loop`: ADDED 模型入口错误极性条款（迭代器/通道创建失败、verify 拒绝必须以失败响应呈现并归约 failed turn）。
- `telemetry-channel`: ADDED 两条——结算通知识别 SHALL 依据结构化标记（正文前缀启发式退役）；外显判定 SHALL 与宿主投递门同源（单一 deliverable 白名单，白名单外一律内部）。

## Impact

- 代码：`agent/`（agent.go、execution_gate_model.go、session.go、settle_routing.go、event_bus.go、recovery.go、telemetry_audit.go）、`agent/task/`、`agent/compress/`、`agent/reliability/`、`memory/`（mem_spill、retention_lease、compaction、segment_store、kv/local_file_kv、engine/diagnostics）、`rl/`、`tool/action/`、`scripts/`（check_comment_only.sh、lint.sh）、`.github/workflows/ci.yml`、`examples/wechat-bot/restart-tagent.sh`
- 文档：README、docs/storage-durability-positioning.md、docs/wiki/**、openspec/specs/**（Purpose 回填）、openspec/changes/archive/**（路径脱敏）
- **BREAKING**：诊断字段删除、谱系白名单化、通知识别标记化、KV 快照分片（旧 kv.json 不迁移）——均为 pre-release 定位下的有意变更，CHANGELOG 显式标注
- 唯一外部依赖项（非本地可消除）：go.mod replace 指向个人 fork（上游 PR #2637 未合）——保留 + CI `go mod verify` 防护 + 合入后摘除的跟踪任务
- 评审全量证据：`.git/review-notes/01-findings.md`（45 项逐项闭环为验收标准）
