# 注释 / 文档 / 实现 不一致台账

清注释时反复出现同一类问题：**注释（或文档）说的与代码做的不一致**。这类比"该删的废话"更危险——它会误导下一个改代码的人，而且**任何等价门都查不出来**（门只证明"没改代码"）。因此每批必须把撞见的记到本表，并三选一处置：改文档／改代码／记为待裁决。

| # | 位置 | 注释/文档声称 | 实际 | 证据 | 处置 |
|---|---|---|---|---|---|
| D-1 | `memory/engine/engine_inmemory.go` 头横幅 | 「MVP 局限（有意）：向量索引内存态、**不持久化**，重启后旧事件向量丢失」 | 同包 `EngineConfig.KV` ＋ `engine_persist.go` 已实现「KV 序列化 ＋ 启动异步重建 ＋ 模型指纹跳旧」 | 读 `engine_persist.go` 全文；`TestInMemoryEngine_KVPersistenceRebuild` 通过 | 注释删除；准确形式（"持久性取决于是否配置 KV"）写入 `memory-architecture.md` 十七节（组 27） |
| D-2 | `memory/kv/local_file_kv_test.go` 体内注 | 「小写入量合法地只存在于 `kv.wal.jsonl`，属 snapshot＋WAL 布局」 | 全包 grep 无任何代码写 `kv.wal.jsonl`，`LocalFileKV` 只有单张 `kv.json` 快照、**无 WAL** | `grep -rn "wal.jsonl" --include=*.go` 只命中这条注释与它的断言备选 | 注释删除；文档十八节明确"单快照、无 fsync"；断言保留 `snapshot 或 WAL 之一存在`（为将来后端留口），已在文档中标注 |
| D-3 | `memory/engine/diagnostics.go` 字段注 ＋ 三层透传注释 | `WALQuarantined` 是「F3 可观测**闭环**——此前只定义无消费方」 | 消费方确实存在（诊断快照），但**没有任何 KV 后端产生该计数**：`func .*) WalQuarantined` 只有三个转发者 ⇒ 字段恒为 0 | `grep func .*) WalQuarantined` ＝ ErrorTrackingStore／FileSegmentStore／engineBridge 三处，全是转发；`memory/kv` 内无实现 | 未擅自"修代码"。文档十八节末节改为如实标注「未接通管道，恒为 0」；补实现还是删整链**待你裁决**（组 25.6） |
| D-4 | `rl/trajectory_recorder.go` `Close` doc | 「Close 会 drain ＋ **sync** on close」 | 写协程退出路径此前只 `Close` 文件不 `Sync`；且 flush 哨兵非阻塞投递，通道满时被丢弃 ⇒ 承诺落空 | 读 `writeLoop` 退出分支；注释自述为「四审发现的 C1 文档谎言实例」 | 代码已补退出前**最终 Sync 兜底**（既成事实），doc 侧删除该自述并把兜底写成设计（`rl-architecture.md` 三节，组 33） |
| D-5 | `rl/http_api.go` package 注释 | 「**Package agent** provides an optional HTTP API…」 | 文件属 `rl` 包；且同包 `agent_loop.go` 已有另一份 package 注释 ⇒ **一个包两份 package doc、其中一份包名写错** | `go doc ./rl` 显示的是另一份；两份并存 | 合并为一份（`agent_loop.go`）并加 `// 契约:` 索引；HTTP 面契约索引挂到 `HTTPAPI`（组 34.3） |
| D-6 | `docs/wiki/evolution/…`（原无此篇） | 注释里成体系的自改进契约 | `docs/wiki/evolution/` 目录**根本不存在** ⇒ 契约只活在注释里 | 组 27 核查 | 先建篇再删注释（`evolution-architecture.md` 九节） |
| D-7 | 代码用词 ↔ 文档用词 | 代码写 `MetaKeyEventKey`、"摘要命名" | `event-architecture.md` 12.6/12.8 确有对应内容，但**只写字符串值、不点常量名** ⇒ 从代码出发检索不到 | 关键词反查 0 命中 | 12.6 表内点名 Go 常量（组 29.7）；并升为规范「索引双向承诺」（29.8） |
| D-8 | `docs/upgrade-rollback-drill.md` 佐证行 | 引用 `rl/endpoint_redirect_test.go` 的五个用例 | 该文件已在本变更 W2 结构收敛中并入 `rl/http_api_closeout_test.go` ⇒ **文档引用不存在的文件** | Grep 核实五个用例现居何处（覆盖未丢）后改写引用（组 32.2） | 已修；同类漂移改由机器检查兜（见下 `doc-refs`） |
| D-9 | 文档标题残留 | `memory-architecture.md` 十五、记忆策展**（unified-memory-curation）** | 标题带已归档变更名，属迭代标记 | 组 22 核查 | 去掉归档名，章节重新编号 |
| D-11 | `docs/wiki/platform/platform-subsystems.md` | 佐证引用 `openspec/changes/tagent-evolution-roadmap/execution-dag.md` | 该变更早已归档为 `archive/2026-09-06-tagent-evolution-roadmap/` ⇒ 引用停在归档前的路径 | `codetools doc-refs` 报出；归档目录内确有 `execution-dag.md` | 改指归档路径；并把该类检查做成门（见下） |
| D-12 | 白名单口径教训 | —（工具设计问题） | 首版 `doc-refs` 把上游仓路径（`trpc-agent-go/runner/runner.go`）与占位符（`PromptDir/knowledge_agent.md`）报成漂移；且 `docs/.dev/*` 历史纪要引用**当时**的文件名，本就不该被"修正确" | 命中分类核对 | 收紧为「首段必须是本仓已知顶层目录」；**`docs/.dev/` 按设计排除在门外**（篡改历史记录比留悬空更糟），门只扫 `docs/wiki` ＋ 顶层 md |
| D-13 | `rl/trajectory_recorder_test.go` 停用块 | `// TODO: Re-enable after moving SwappableModel to rl package` | `SwappableModel` **现在就在 `rl/swappable_model.go`** ⇒ 前提已满足，TODO 早已过期；而整块被 `/* */` 包住的用例既不被运行也无标记为待办 | 读文件＋确认同包存在 `NewSwappableModel`；机械pass 还会把这条 TODO 当旁白删掉（信息从此消失） | **待你裁决**：复活该用例（属行为/覆盖变更，且 `/* */` 块内的代码需重新接线）还是删除。清理前该文件在策略上剩 1 条无法归零——注释类门对"整块停用的代码"没有正当形态。已在块首写明现状与去处（D-13），不再用 TODO 伪装成待办 |
| D-14 | `internal/strictyaml` 的契约归属 | 包注释自称"唯一严格解码实现"，规则也要求测试有一行文档索引 | `docs/**` 与 `openspec/specs/**` 下**没有任何一节**承载该严格度契约 ⇒ 索引无处可指 | `codetools doc-refs` 与人工反查均无落点 | 待裁决：在配置文档补一节（倾向）或给"契约全由 package doc 承载的小包"开豁免。暂不放宽规则、也不硬凑锚点 |
| D-14 处置 | `internal/strictyaml` | 见上（索引无处可指） | — | — | **已闭合**：按"补一节、不放宽规则"方案，在 `platform-subsystems.md` 新增「六·B、配置解码的严格度契约」（`#strict-decode`，含未知字段/尾随文档/尾随内容/空文档零值/`0x` 宽容五行表与"不得另立第二套严格度"），包注释与测试文件索引均指向它 ⇒ 该包 4 → **0 发现** |
| D-15 | `rl/swappable_model_test.go` | 注释称 `Swap(当前实例)` 时"old==new 仍会退役一次"（即会被关闭） | 紧接断言要求 `second.closed == 0`——**当前实例永不作为回收候选**，实现亦如此 | 读该用例＋比对 `rl/swappable_model.go` 的 sweep 条件 | 判注释错：删除该注释；正确语义由用例名、断言消息与 `rl-architecture.md` 二节承载 |
| D-16 | 生成物抽查（`docs/api/`，覆盖未清包） | 若干 doc 仍以"此前/曾"叙述变更经过，并残留 `N4`/`K5` 类条目编号 | 生成物把它们集中呈现（人工读源码时易漏） | `gen_godoc --check` 首跑后 grep 线索词 | 归各包 G 批处置；生成器同时成为"叙述残留"的复检面（每包清完重跑即验证） |
| D-17 | `memory/embedder/*` 断言消息 | 三条 `t.Fatalf/t.Errorf/t.Logf` 消息以 `5.1`/`5.3` 条目号开头作为判据标识 | 编号属过程工件，读者无法从消息判断契约；且它长在**代码**里（字符串），注释门看不到 | `grep '"5\.[0-9'` | 归第三类批次改写为自解释判据（49.4），与 21.9/24.8 同一处置方式 |
| D-17 状态 | `memory/embedder/zhipu_real_test.go` | 三条断言消息以 `5.1`/`5.3` 编号作判据 | 判据应自解释且编号属过程工件 | 改后 `t.Fatal/t.Error/require` 计数 13 → 13、残留字面量 0 | **已闭合**（组 49.4，第三类批） |
| D-18 | `org_hotreload.go`、`examples/wechat-bot/main.go`、`reincarnation_notice.go` | 注释以 `// Design:`／破折号形式指向**变更文档**的设计条目（D1–D4 等） | 该设计内容目前**只有变更文档一份**，`docs/wiki` 与 README 均无（已 grep 证实） | 门闭合 G-4 后新暴露 | 先读实现写出 `docs/wiki` 长期篇（org 级热重载四决策；转世通告机制），代码再改指锚点。不得只删引用不留真源，也不得把设计照抄进注释 |
| D-19 | `examples/wechat-bot/reincarnation_notice.go` | 两条读者可见文本以 `D1 命中` 作为判据标识（`log.Infof` 与注入给模型的通报正文） | 编号属过程工件；且它是代码（字符串），注释门看不见 | `grep -c "D[0-9]"` 该文件仍 2 处 | 归第三类小批改写为自解释文本并证断言计数不变（58.2 已把混批的改动撤回） |
| D-10 | doc 挂错声明（同族三例） | 说明文字与所描述的函数不对应 | `ContextCompressor.MarkMeditationKey`、`NewGitEvolution`/`SetGovernanceSignalsAvailable`/`BindRuntime` 三句错位、`EndpointRedirectPolicy` 的契约挂在 const 上 | `go doc` 输出张冠李戴 | 逐处复位（组 27.4、34.3、33）；`agent/compress` 那处随该包批次修 |

## 判据

注释与文档不一致时，**先判定哪个是对的**（以代码与测试事实为准），再决定改哪一边；绝不允许"为了让注释消失而把注释删掉、留下一个没人纠正的错误说法"。
