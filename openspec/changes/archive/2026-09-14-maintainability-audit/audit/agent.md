# agent 族评分卡（task / compress / governance / reliability / agent 本体）

base: `cf006e1` | 取证: task_manager/session/context_manager/event_loop/projection_rebuild/build_agent 交互面全文级（多轮 review 积累）+ compress/governance/reliability 结构读与定向核对

## agent/task（3 文件 1098 行 / 测试 1217）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | B+ | task_manager.go 集中 Spawn/watch/resume/RestoreTask/三对账（detached/zombie/liveness）/pruneTerminal 多职责，方法划分清晰但单文件近 1100 行 |
| 耦合 | S | 叶子包零引擎依赖（design 明示）；detector 契约接口窄 |
| 测试 | S | 测试行超源码；fail-before 回归纪律（zombie 三态/n- 枚举/pruneTerminal nil detector 皆 fail-before） |
| 文档一致 | A | 注释带出处与生产教训；诚实边界（subagent 跨重启 Resume❌ 承诺表） |
| 演进风险 | A- | pruneTerminal nil 守卫刚修（cf006e1）；RebuildTask 语义与 zombie 对账的交互是新的耦合面（本次生产 panic 即其相遇）——建议守护测试扩至「重建→退役→prune 全链」 |

发现: 见 F-4/F-5（agent 包侧）；本包生产 panic 已修（cf006e1）

## agent/compress（7 文件 1925 行 / 测试 2538）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | B | 骨架模型（L0-L3 定级）+ 卡片序列 + compaction 事件 + token 计量——概念密度全项目最高，但骨架定级为段龄纯函数（deterministicLevel）控住了分支 |
| 耦合 | A | 依赖 memory/event 叶子；与 agent 本体经 ContextManager 单点衔接 |
| 测试 | S | 2538 测试行；ByteIdentical 重建等强不变量测试 |
| 文档一致 | A | wiki/memory §压缩策展与实现同步（compaction 事件化后已反转更新） |
| 演进风险 | B+ | 压缩视图 path-dependent（resident-state-recompute 复审结论）——改动折叠/渲染顺序须过字节级回归；新comer 理解成本高 |

发现: 无新（path-dependent 约束为已知设计决策）

## agent/governance（8 文件 1705 行 / 测试 933）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | A- | 决策管线 classify→approve→goal→budget→ledger 阶段清晰；classifier 纯函数 |
| 耦合 | S | 依赖倒置：Goal/Ledger BindStore 延迟绑定；ApprovalChannel 送达抽象 |
| 测试 | B+ | 933 测试行对 1705 源码；审批 TOCTOU 面的并发测试可再加（当前靠值拷贝纪律，approval.go:116 注释自证） |
| 文档一致 | A | 「闸不是墙」理念贯穿；与 specs/approval-channels 一致 |
| 演进风险 | A- | 审批文件目录为进程外协议（人工写文件即生效）——目录格式演进需兼容旧 pending 文件 |

发现: 无

## agent/reliability（4 文件 537 行 / 测试 452）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | S | 五依赖状态机 + spill + anchor 各一文件 |
| 耦合 | S | MemorySink 适配器反向满足 memory.DegradationSink（sink.go）——双向依赖倒置判例 |
| 测试 | A- | 故障注入式测试（degradation 三段式） |
| 文档一致 | S | 头注释即设计摘要（at-least-once/三段式/失败一等资产） |
| 演进风险 | A | 新依赖接入点明确（ReportFailure/ReportSuccess + sink 适配） |

发现: 无

## agent 本体（19 文件 5310 行 / 测试 6762）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | B | context_manager.go 千行级多职责（装配+压缩编排+看板注入+热更缝+SwapExecutor+persistBusEvent）；19 文件总体职责划分清楚但粘合层集中 |
| 耦合 | B+ | 下游依赖方向正确；上游 session.go 与框架 Session 的并发边界含混（F-4 race 根因） |
| 测试 | A- | 6762 测试行；7 处 DATA RACE 未清（F-4）+ 1 flaky（F-5） |
| 文档一致 | A | wiki/agent 已同步 R2-R4；buildMode 三谓词文档化（§2.13） |
| 演进风险 | B+ | ownership 规则已类型化但 build 侧 late-bind 接线（resident sink/redispatch 表/回填）仍多散点——新入口接入需对照 checklist |

发现: F-4、F-5
