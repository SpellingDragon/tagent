# implementation-hardening — 提案

## Why

三份第二轮外部评审（docs/.dev/tagent深度分析报告(1).md、tagent重要特征深度调研(1).md、tagent多维度评价报告(1).md，2026-09-13，全量源码两轮审查 + 本地实证）给出加权评分：**设计 7.5 / 实现 6.6**。评审的核心论断：差值 0.9 本身就是最重要发现——设计自觉性顶尖（事件溯源投影、票据召回、buildMode 类型化均获正面确认），但实现存在系统性「收尾欠缺」，四个短板维度（安全 4.0、可靠性实现 5.5、复杂度 6.0、性能实现 6.0）全部属于收尾纪律而非能力缺陷。

本轮对三卷全部指控做了**逐条核验**（file:line 级），结果三分：

**证实（14 项，本轮修复对象）**：

| # | 发现 | 证据 |
|---|------|------|
| V1 | Resume 对重建任务 `close(nil watchDone)` panic——RestoreTask 不置 watchDone/firstSettle/detector，Resume 判 newWatch=true 即 close(nil)。与已修的 pruneTerminal 同型裂缝，评审「模式级审计缺失」判断成立 | task_manager.go:900（RestoreTask :577-606 无 watchDone） |
| V2 | LocalFileKV WAL 仅 bufio Flush 无 f.Sync——「已确认写入」掉电可丢，主打的「宿主机重启原地复活」恰在卖点场景失守；现有 durability 测试只覆盖优雅 Close | local_file_kv.go:210-233 |
| V3 | RL HTTP API 零认证——任何可达者可 InjectMessage 操纵 agent、经 llm_base_url 重定向 LLM 端点致全部后续 prompt 外泄（评审判为全项目最严重安全缺口） | rl/http_api.go（无任何 auth 字样） |
| V4 | TypeToolUse/NewToolUseEvent 幽灵抽象——构造器全库零调用方，注释宣称的「tool_calls 转换为 bus 事件」不存在；「事件驱动替代 ReAct」叙事与 turn 内仍是框架同步 ReAct 的现实有落差 | event_bus.go:58-91 |
| V5 | modelref.go BuildDirectRequest/CallDirectModel 死导出（judge.go:123 直接 GenerateContent；仅 FoldModelRefAliases 活于 config.go:718） | modelref.go:14/39 |
| V6 | IsTmuxAvailable 恒真——体为 `NewTmuxExecutor() != nil` 而后者永不返 nil；调用方 action_tool.go:176 的可用性降级分支形同虚设 | action_tool.go:751-754 |
| V7 | compaction.go 头注释宣称 L2 gzip / L3 gzip+summarization，全仓无 gzip 调用——设计意图化石 | compaction.go:19-24 |
| V8 | MemoryPlugin.lastEventKeys map 无界增长（键 "partitionID:sessionID"） | memory_plugin.go:36 |
| V9 | SwapExecutor 返回的旧 runner 被调用方丢弃（多次热更泄漏）；SwappableModel.Swap 不 Close 旧 model（同型）——drain-free 做了「切」没收「尾」 | tagent.go:396/425、swappable_model.go:35-39 |
| V10 | YAML 非 strict 解析（yaml.Unmarshal），拼错 key 静默忽略 | config.go:882 |
| V11 | buildAgent 递归无引用环检测（A↔B 构建期栈溢出） | build_agent.go（无 visited） |
| V12 | README「事件永久入库」vs 默认类型 TTL（event.DefaultTypeTTL 曲线）——头条承诺与默认行为脱节（评审「叙事信用是最易损耗资产」） | README.md:3/167/253 |
| V13 | recall items 路径无独立条数上限（limit 钳制只见 query/turn 路径），模型传大量票据可放大水合成本 | tool/recall/recall_subtools.go |
| V14 | MetricGuardrail 的 DenialCount/CriticalCount 依赖治理事件，治理关闭时两判据静默空转且未声明 | evolution/eval.go:53-54 |

**反证（3 项，评审亦有失准——采其可取，弃其失实）**：invBus/activeBus 非死码（Run() 子代理调用真实经其路由注入，session.go:85-166）；「namedStores+sync.Once 不可重入」组合根不存在于当前代码；behavior-matrix 已记载 bundle 退役（:89）而非残留旧叙述。（原列第四条「StopLoop 未 close outputCh」经深钻撤回——见 V15。）

**深钻补证（2026-09-14，探索轮定谳）**：

| # | 发现 | 证据 |
|---|------|------|
| V15 | **StopLoop→StartLoop 重启双重破坏**（前项反证撤回）：StartLoop 循环 goroutine 的 defer 里有 `close(ta.outputCh)`（lifecycle.go:180），而 StartLoop 复用 ta.outputCh 成员不重建——重启后消费者读到已关通道；二次 StopLoop 再次 close 已关通道 → panic 逃逸（recover 已过）→ 进程崩 | lifecycle.go:174-191 |
| V15a | meditationMgr 随每次 StartLoop 重启（:186-188）✓——冥想不受重启影响（探索前疑虑解除） | lifecycle.go:186 |
| V16 | spec 线性定级实锤：deterministic-compress-level:24-30 写 `age<k*2→L1、age<k*3→L2、其余 L3`，代码 smart_compress.go:148-153 为指数边界 {k,2k,4k}——文档漂移证实 | spec:24-30 |
| V17 | agent 包 -race 十案三分类：上游内部竞态为主（inmemory session service / steer 关闭路径，tagent 帧仅路过）＋测试 mock 自身竞态（loopMockTool）；**无本地可修案**，审计 F-4 的「Session.Clone」描述失准（真身为上游 inmemory service 并发读写） | /tmp/race 探针全栈分析 |
| V18 | buildAgent cache 于构建尾部填充（:259/:592）——agent 引用环是真栈溢出（评审定性正确） | build_agent.go:72/259/592 |

本变更承接既有工作：pruneTerminal 已修（cf006e1）、死代码一清已毕（848b412，staticcheck 全绿）、可维护性审计 F-1~F-11 在案（本变更闭合其中 F-4 的本地侧与 race 门禁部分）。评审「战略级」建议——打 v0.1.0 冻结承诺面——作为本变更终点。

## What Changes

六个工作包 + 战略收尾，全部以核验后事实为对象：

- **WP1 裂缝模式级收口**：V1 修复（fail-before 先行，构造路径已定谳：RestoreTask(status=Stable)+ResumeFn）+ Spawn/Resume/watch 的 nil detector 模式级审计；KeepRecentTasks 临时改写竞态（context_compressor.go:333-335）改参数传递；**V15 重启修复（StartLoop 每次重建 outputCh）+ e2e 行为锁定**。
- **WP2 耐久收口**：LocalFileKV WAL 每次 append 后 f.Sync + snapshot rename 后目录 fsync（对齐 RelationStore per-line Sync 先例），fsync 可配置默认开；冷分区遗忘经待深钻定谳后补分区发现扫描。
- **WP3 安全收口**：RL HTTP token 认证（`rl.auth_token`，Bearer）；未设 token 时仅允许 loopback 监听（非 loopback fail-closed 拒绝启动）；recall items 路径条数上限。
- **WP4 资源与触发面收口**：旧 runner/旧 model 换代后延迟 Close（in-flight 引用归零后，保留 ring-2 回滚所需的最近一代）；lastEventKeys 封顶淘汰；Rollback 手动触发面（信号或管理命令）。
- **WP5 死代码二清**：TypeToolUse/NewToolUseEvent 删除 + event_bus 头注释改写如实；modelref 死导出删除；IsTmuxAvailable 改真实探测（exec.LookPath）。
- **WP6 配置健壮性**：YAML KnownFields strict 解析（未知字段报错并列出）；agent 引用环检测（visited 集，构建期报配置错误）。
- **WP7 文档对齐**：README「永久入库」改按类型 TTL 实述（默认曲线 + 配置永久之法）；compaction 化石注释改真；guardrail↔governance 耦合在评估输出与文档显式声明；wiki 漂移逐条复核。
- **WP9 架构防呆立法**（v0.1.0 冻结前置；立法三件 + 一项软点补丁）：① 分层依赖方向可机械断言——已核验现状全绿（memory/plugin/event 无违规 import、agent 不引根包、event 纯叶子），以测试固化使未来违例即红；② **上游内部行为假设钉**——invariants_test.go 宣称 I1-I4 但 I2（BeforeModel 完备性，恰依赖「插件管线在 tool-result 事件上同步等待完成」这一上游内部行为）**有注无测**，补 TestI2 走真实管线钉死此假设（评审判定的「定时炸弹」拆引信）；③ 红色耦合台账——上游内部假设清单 + 隐式耦合清单入 LEDGER，逐项标注钉测/豁免状态；④ 资源回收软点补丁——retired 清单加定时兜底清扫（持续负载下全局 in-flight 可能永不归零）。另：两处 yaml 解析点拍板抽取共享 strict 解析实现（一实现两调用点）。
- **WP8 战略收尾（前七包通过后）**：V17 分类处置（上游案→豁免清单+上游 issue 证据；测试 mock 案→测试侧修复；不改生产码）+ agent 包纳入 CI race 门禁；soak 测试骨架（长跑压缩-召回-重启循环，CI 手动触发）；全部通过后打 **v0.1.0 tag** 冻结承诺面。

**Non-Goals**：R4 子 agent store 代际漂移（设计级，另立案）；治理 RiskClassifier 子串匹配强化（「闸不是墙」哲学定位内，文档声明即可）；token 计量启发式替换（可配置已足够，中文场景默认合理）；rustviking/git exec 超时（审计 F-6/F-8，backlog 另行）；任何新子系统——评审明确「停止新增子系统直到现有闭环跑过真实部署」。

**立法但缓行**（守则入册、动作入 backlog，不在本变更执行）：god file（context_manager 1078 行/tool_agent 991 行）按职责图解体——v0.1.0 冻结前夜不动大结构，验收准绳（变更局部性：一个意图一个落点）随本案立法，解体列为冻结后首变；detector/EventKey/PartitionID 类型化（L2 上梯）；「新增后端」注册表化（L3）——均见 design.md D11 路由表。

## Impact

- **Specs delta**：10 个既有 capability 增补 Requirement（task-reentry、event-segment-store、rl-feedback、recall-protocol、swappable-executor、persistent-event-loop、action-tool-config、production-wiring、evolution-evaluation、wiki-code-sync）+ 新增 capability `architecture-guardrails`（分层断言/假设钉/解析单点）。
- **代码**：约 20 文件；行为变化三处须明示——fsync 默认开（写路径性能代价，可关）、YAML strict（含未知字段的旧配置将启动失败，v0.1.0 前可接受）、RL 未配 token 且非 loopback 监听将拒绝启动（fail-closed）。
- **流程**：LEDGER 台账回写；v0.1.0 tag + CHANGELOG 定稿为终点门。
