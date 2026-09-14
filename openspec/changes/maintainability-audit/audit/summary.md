# 发现汇总台账（summary）

- base commit: `cf006e1`
- 审计范围: 29 Go 包逐包逐文件
- 状态: 定稿（2026-09-13，共 11 发现：🔴0 / 🟠3 / 🟡8）

## 编号规则

`F-xx` 全局唯一；三级：🔴 致错/致损、🟠 漂移温床、🟡 卫生债；每条必含 file:line 证据、来源（存量/引入）、影响路径、建议立案形态。反证条目保留并标「已反证」。

## 发现清单

（待批次审计填充——按 🔴 → 🟠 → 🟡 排序定稿）

| 编号 | 级 | 包 | 发现 | 证据 | 来源 | 影响 | 立案形态 |
|------|----|----|------|------|------|------|---------|
| F-1 | 🟡 | event | 包注释残留他项目名 `trpcclaw`（拷贝史残留，误导读者对代码来源的判断） | event/types.go:3 | 存量 | 读者困惑；暴露代码移植史 | 文档（一行注释修正） |
| F-2 | 🟡 | event | 常青生产注释引用已归档变更内文件 `execution-dag.md §4.2 ESCALATE`——归档目录属临时 artifact，清理即断链 | event/registry.go（「冻结纪律」注释段，grep execution-dag 即达；初记行号 29-30 已漂移——spec 符号引用设计的活例）→ 文件现仅存于 openspec/changes/archive/2026-09-06-tagent-evolution-roadmap/ | 存量 | 引用断链后冻结纪律失去可达依据 | 文档（把 ESCALATE 规则内联或迁入常青文档） |
| F-3 | 🟡 | event | 核心契约包覆盖率 49.7%（包内最低），`summarizeToolResult` JSON 摘要多分支（types.go:236-316）覆盖薄弱 | `go test ./event/ -cover` | 存量 | 摘要分支回归无守护——它是卡片行/召回列表的文本来源 | 守护测试 |
| F-4 | 🟠 | agent | 7 处 DATA RACE：`TagentAgent.Run.func1`（agent/session.go:122）与上游 `session.Session.Clone`（trpc-agent-go session.go:95）并发读写——历史 race_check 已知存量（LEDGER 曾记 3 处，本次实测警告点 7 处、根因同源） | `go test ./agent/ -race`（WARN 堆栈 session.go:122 ←→ 上游 Clone） | 存量 | 竞态窗口在 turn 运行路径上；上游 Clone 读 session.Events 与本地写入并发——量变到质变即数据损坏 | 修复（需与上游协作或本地 Clone 前加锁快照） |
| F-5 | 🟠 | agent | `TestSubAgentRun_ToolResultStopsPrematurely` 在 -race 下断言失败（evt[1] done=true tool_calls=1 与期望次序不符）——race 时序敏感 flaky，或暴露工具结果提前终止的真语义边界 | `go test ./agent/ -race` 输出 session_subagent_toolstop_test.go:197 | 存量（疑似） | CI -race 门不稳定；若为真语义缺陷则影响子 Agent 工具循环正确性 | 待深钻（先定性 flaky 或真缺陷） |
| F-6 | 🟠 | memory/kv | rustviking CLI fork 无超时：`exec.Command`+`cmd.Output()` 裸调（rustviking_client.go:83/108），fork 挂起→同步写路径（StoreEvent）永久阻塞→ErrorTrackingStore 退化防线失效（其依赖错误返回，挂起不返回）——设计报告 R2 已预感「fork 延迟不可控」但未落地超时 | rustviking_client.go:81-116（无 CommandContext/Deadline） | 存量 | 常驻进程单次 CLI 挂起即可冻结事件管线（MemoryPlugin 同步点），且无降级路径可达 | 修复（exec.CommandContext + 预算超时 + 挂起转错） |
| F-7 | 🟡 | memory | MemoryStore 接口宽：可选能力（向量路）靠 stub 方法 + `SupportsVectorSearch()` 探测（segment_store.go:804-815 / in_memory_store.go:311-324），新后端必须复刻 stub 语义而非只实现能力 | 两 store 的 stub 对比 + types.go:93 接口注释 | 存量 | 新后端漏复刻 stub → 编译错（尚可）；但能力探测语义靠约定，误实现 SupportsVectorSearch=true 而未实现向量路 → 静默错误结果 | 重构（接口隔离：能力拆为可选接口+类型断言，如 agent.Closer 判例） |
| F-8 | 🟡 | evolution | git exec 裸调无超时（gitrefine.go:28，同 F-6 类；git 本地操作多为快速失败，但 hooks/credential helper 场景可挂起——refine 在治理闸内属高权限通道，挂起即占住 turn） | evolution/gitrefine.go:28 | 存量 | refine register/rollback 卡死回合 | 修复（exec.CommandContext，与 F-6 同一变更顺手） |
| F-9 | 🟡 | tool/spec | 测试密度全包最低（294 源码行 / 71 测试行） | wc 口径见 baseline.md | 存量 | openspec 工具面回归无守护 | 守护测试 |
| F-10 | 🟡 | memory/根包 | 生产注释系统性引用设计报告行号（memory/error_tracking.go:18「报告 line 1861」、mem_spill.go:6「line 2229/2392」等）——报告为一次性 artifact，行号非稳定锚点 | 多处（grep「报告 line\|报告 D3」） | 存量 | 引用漂移后设计语境不可回溯 | 文档（出处改引 spec 名/设计节名，去行号） |
| F-11 | 🟡 | agent/governance | DenialLedger 审计事件 StoreEvent 失败被静默忽略（`_ = store.StoreEvent`）——审计账本完整性缺口：拒绝/放行记录可能无痕丢失 | agent/governance/ledger.go:146 | 存量 | 治理审计断档不可知；合规场景是硬伤 | 修复（失败降级为本地文件追加或告警日志） |

## 开放项 / 待深钻

| 项 | 说明 |
|----|------|
| O-1 | F-5 定性：TestSubAgentRun_ToolResultStopsPrematurely race 下失败——需单独会话复现判定 flaky 或真缺陷 |
| O-2 | c5399e2 的 12 条 spec 状态注记本身时效复验（本审计抽核未发现新漂移，未逐条全验） |
| O-3 | F-4 修复方案需上游 trpc-agent-go 协作（Session.Clone 并发契约）——建议先以本地快照锁缓解 |

## 横切结论（第二遍交叉）

| 主题 | 结论 |
|------|------|
| 错误处理一致性 | 良好：错误包装 `%w` 规范、MemoryPlugin 存储失败有 stored-gate 承接、日志级别纪律（大写开头）一致；仅 F-11 一处静默吞 |
| 锁与并发 | 判例良好（approval 值拷贝写外锁、rl 三锁分离、mcp 懒同步单锁）；残余集中两处——F-4（agent-session 边界）、pruneTerminal 无锁读（cf006e1 已修） |
| TODO/FIXME/死代码 | **TODO/FIXME/XXX/HACK 零残留**（生产代码 grep 实证）；悬空引用见 F-1/F-2/F-10 |
| specs 一致性 | R1-R4/buildMode/LoadFiles 语义本会话已同步 wiki+specs；抽核未见新 🔴 漂移（全量逐条复验归 O-2） |

## 已反证

| 疑似 | 包 | 反证理由 |
|------|----|---------|
| `Source.Get` 源文件被删时行为不明，恐报错中断热重载 | prompt | prompt/source.go:60-84：`checkModTimes` err → 回退缓存内容（graceful degradation），仅当缓存为空才返错；优雅降级成立 |
