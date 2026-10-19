# 归档清单：docs/wiki 压缩重写前的完整版（2026-10-19）

本目录是 **压缩重写的归档指针**，不是副本。原版的完整字节保存在本文件记录的提交里，逐篇可按下方命令取回；
`docs/wiki/` 里的同名文件是同内容的压缩版（去冗余、保锚点与判据）。

## 一、为什么是"指针归档"而不是 `git mv` 出来的副本

- 仓库的 `doc_budget` 棘轮按 **git 索引内全部 `*.md` 行数**计量，副本归档会让总量等于「原版 + 压缩版」（净增约 1600 行），
  直接判红，与"总量只降不升"的门冲突；而 `scripts/check_doc_budget.py` 不在本次变更的写入白名单内，不能扩排除面。
- `docs/wiki` 本就是 `historical-verbatim` 树，`doc-refs` 对"目标文件在今日树中不存在"的引用按**引入行所在提交**解析
  （`scripts/codetools/check.go` 的 `citationBasis`），因此原版只需在 git 历史中可定位，不需要在工作树里再存一份。

取回任意一篇的完整原版：

```
git show e4e884a48c6b9a5a70fcd13265e28d3aa55a67c8:docs/wiki/agent/agent-architecture.md
```

## 二、基线（改写前，提交 e4e884a48c6b9a5a70fcd13265e28d3aa55a67c8）

| 篇 | 行数 | 被 Go `// 契约:` 引用的锚点数 |
|---|---|---|
| agent/agent-architecture.md | 590 | 7（module-position/core-components/meditation-curator/package-layout/subagent-loop/test-support/framework-boundary） |
| agent/event-flow.md | 232 | 5（event-stream-overview 之外：projection-lifecycle/e2e-turn-sequence/watchdog-final-round/multi-user-isolation） |
| agent/compression-and-telemetry.md | 85 | 3（output-overflow/telemetry-ladder/condensed-card-guard） |
| agent/execution-generations.md | 139 | 4（turn-outcome/lifecycle-convergence/reentry-resolution/turn-local-execution-face） |
| agent/governance-enforcement.md | 56 | 5（enforcement-modes/disposition-and-risk/approval-channel/budget-epoch/denial-ledger/goal-registry） |
| agent/prototype-skeleton.md | 45 | 2（six-pieces/inherited-invariants） |
| agent/task-lifecycle.md | 94 | 5（status-machine/spawn-dedup-origin/restore-rebuild/ttl-reclaim/detach-suppression） |
| platform/platform-subsystems.md | 233 | 4（composition-root/workspace-scratch/config-surface/strict-decode） |
| platform/org-hot-reload.md | 302 | 10（overview/fingerprint/generations/apply-record/candidate-refusal/close-drain/owner-retirement 等） |
| platform/agent-behavior-matrix.md | 290 | 0 |
| platform/cognitive-asset-guard.md | 92 | 0 |
| platform/evaluation-suites.md | 53 | 1（design-notes） |
| platform/reincarnation-notice.md | 56 | 2（overview/breakpoint） |
| platform/resource-ownership.md | 106 | 0 |
| prompt/prompt-architecture.md | 591 | 0（由平台篇以文件链接引用） |
| reliability/durable-delivery.md | 168 | 0（被 agent 篇按锚点链接：lineage-visibility） |
| rl/rl-architecture.md | 220 | 0 |
| memory/memory-architecture.md（W2） | 1662 | 12（hard-contracts/read-paths/event-shape/overview/inmemory-retrieval/embedder-contract/partitionidfromname/file-segment-store/causal-chain/store-instance-sharing/rv-cli/local-file-kv；原版 33 个锚点全保留，另补 `partitionidfromname` 锚——该锚在原版就不存在（既有悬空，2 处 Go 契约引用落空），新篇补上后转绿） |
| event/event-architecture.md（W2） | 581 | 2（overview/const-list；文档 8 个锚点全保留） |
| evolution/evolution-architecture.md（W2） | 117 | 4（verdict-states/evidence-window/refine/git-safety；文档 5 个锚点全保留） |
| **合计** | **5712** | — |

## 三、改写约束（对压缩版仍然成立）

1. 上表列出的 `<a id="...">` 锚点**一个不删**：Go 注释与跨篇链接按锚名寻址，删掉即断链。
2. 被 `// 契约:` 指向的章节里的**具名语义句**逐字保留（判定句、常量、字段名、拒绝形态）。
3. 判据表、决策（"为什么"）与「已知缺口」章保留；删的是重复叙述、过程叙事、代码清单与过时实现细节。
4. 原版行的可核对性以第二节的提交为基线：任何"这句原来有没有"的争议，用第一节的命令取回原版逐字比对。

## 四、W2 保全清单（memory / event / evolution）

W2 改写前先枚举引用面：

```sh
grep -rn "docs/wiki/memory\|docs/wiki/event\|docs/wiki/evolution" --include="*.go" .
```

结果：108 处 Go 契约注释指向 18 个锚名，另有跨篇 markdown 锚点链接（当时快照 19 处）。
随后把具名语义句、契约行、常量与数字口径锁成 **131 条双向清单**：每条必须同时逐字命中「原版（`git show <commit>:<path>`）」与「压缩版」，任一侧缺字符即判违例。

| 面 | 条目 | 覆盖的不可让渡资产 |
|---|---|---|
| memory | 74 | 九条硬契约原行、事实链不修改红线、三个原语与压缩固化语义、正负 key/bit63 恒 0 与 10 位分区、计数未知/墓碑/收据/TTL/压实完整性、因果链六读数与四类类型化错误、Embedder 契约与 RRF 同分决胜、降级归因矩阵 |
| event | 32 | `event_summary` 退位语义整段（unified-memory-curation 命名沿革）、严格拒绝非设计折损、EventTypeSpec 唯一权威源、非投影单入口、时间线前缀写读同包、`wf.*`/`inbox_receipt` 保留不得缩短、内置类型存在理由全表、元数据键归属 |
| evolution | 25 | 四条设计前提、refine 三操作、git 四条安全约束、窗口起点与倒序、指标阈值与保守原则、结论四态、三条测试事实、两条 fail-closed 不变量 |

终态：`131 entries, 0 violation(s)`；锚点面 `108 go refs, 0 violation(s)`（md 跨篇链接随并行波次页面变动，最终快照 12 处，0 violation）。

## 五、W1 压缩结果对照（agent · platform · prompt · reliability · rl，终态）

17 篇合计 **3352 → 1667 行（压掉 1685 行，50.3%）**，`<a id>` 锚点 **131 → 131（一个未删）**。逐篇：

| 篇 | 原行数 | 现行数 | 压缩率 |
|---|---|---|---|
| agent/agent-architecture.md | 590 | 248 | 58.0% |
| agent/event-flow.md | 232 | 103 | 55.6% |
| agent/execution-generations.md | 139 | 106 | 23.7% |
| agent/task-lifecycle.md | 94 | 60 | 36.2% |
| agent/governance-enforcement.md | 56 | 39 | 30.4% |
| agent/prototype-skeleton.md | 45 | 33 | 26.7% |
| agent/compression-and-telemetry.md | 85 | 76 | 10.6% |
| platform/org-hot-reload.md | 302 | 169 | 44.0% |
| platform/agent-behavior-matrix.md | 290 | 171 | 41.0% |
| platform/platform-subsystems.md | 233 | 117 | 49.8% |
| platform/resource-ownership.md | 106 | 68 | 35.8% |
| platform/cognitive-asset-guard.md | 92 | 45 | 51.1% |
| platform/reincarnation-notice.md | 56 | 45 | 19.6% |
| platform/evaluation-suites.md | 53 | 40 | 24.5% |
| prompt/prompt-architecture.md | 591 | 105 | 82.2% |
| reliability/durable-delivery.md | 168 | 102 | 39.3% |
| rl/rl-architecture.md | 220 | 140 | 36.4% |
| **合计** | **3352** | **1667** | **50.3%** |

复跑：

```sh
git show e4e884a48c6b9a5a70fcd13265e28d3aa55a67c8:docs/wiki/agent/agent-architecture.md | wc -l
wc -l docs/wiki/agent/*.md docs/wiki/platform/*.md docs/wiki/prompt/*.md docs/wiki/reliability/*.md docs/wiki/rl/*.md
go run ./scripts/codetools doc-refs docs/wiki docs/*.md
```

# 归档清单：W3（plugin / tool / examples）压缩重写前的保全面

本清单在改写**之前**写成，钉住被保全的对象；原版由 `git mv` 归档到本目录同名路径，
`docs/wiki/` 下的同名文件是压缩重写版（保锚点、保具名语义句、保判据与缺口），
两版都可逐字比对。基线提交：`e4e884a48c6b9a5a70fcd13265e28d3aa55a67c8`。

## 一、基线（改写前）

| 篇 | 行数 | 锚点数 | 被 Go `// 契约:` 引用 |
|---|---|---|---|
| plugin/plugin-architecture.md | 991 | 9 | 14（`#overview`） |
| tool/tool-architecture.md | 1281 | 19 | 37（`mcp-gateway-injection` 12、`delegation-retry` 12、`mcp-live-registry` 6、`十七、委派调用的取消归属与重试形状` 5、`tmux-monitor` 1、`action-tool` 1） |
| tool/tmux-action.md | 89 | 12 | 8（`#quiet-vs-dead`） |
| examples/wechat-bot-runtime.md | 108 | 8 | 32（`outbound-delivery` 12、`startup-and-routing` 7、`delivery-receipts` 5、`inbound-intake` 3、`inbound-dedup` 3、`real-send-gate` 1、`large-file-acceptance` 1） |
| **合计** | **2469** | **48** | — |

取回任意一篇的完整原版：

```
git show e4e884a48c6b9a5a70fcd13265e28d3aa55a67c8:docs/wiki/tool/tool-architecture.md
```

## 二、锚点清单（一个不删）

- plugin：`overview` `memory-plugin` `causal-mech` `commit-gate` `summary-plugin` `skip-set` `echo-credential` `attribution-carrier` `causal-chain`
- tool：`tool-registry` `extra-params` `govx-entry-only` `recall-unified-entry` `tool-accessor` `declaration-stability` `recall-agent` `action-tool` `tmux-monitor` `resident-continuity` `recall-contract` `recall-subtools` `knowledge-agent` `mcp-live-registry` `mcp-gateway-injection` `memory-query-hard` `websearch-backend` `delegation-retry` `tool-agent-factory`
- tmux：`quiet-vs-dead` `liveness-first` `notify-gates` `fake-alive-restart` `truthful-tail` `hit-counting` `named-session-singleton` `restart-takeover` `pipe-log` `error-visibility` `failure-polarity` `known-gaps`
- examples：`module-layout` `inbound-dedup` `inbound-intake` `real-send-gate` `outbound-delivery` `startup-and-routing` `delivery-receipts` `large-file-acceptance`

额外硬约束：`tool/tool-architecture.md#十七、委派调用的取消归属与重试形状` 被 5 处 Go 注释按**中文标题**寻址，
压缩版必须保留同文标题或显式 `<a id="十七、委派调用的取消归属与重试形状"></a>`。

## 三、逐字保全的具名语义句（判定句／常量／拒绝形态）

- plugin：四道跳过闸「无 `Response`/无 `Choices`、`IsPartial` 流式分片、退化空 `agent_output` 终答、精确回显」且「全部发生在分配 EventKey 之前」；「**不猜父**」；「关系失败**不回滚内容**」；「投影与存储同点，绝不先投影后存储」；提交闸判据「该字段是否主张『已持久』」；`Verified()` 门禁 fail-closed；「查找一个因果键，只能返回该键最后一次写入的 key，或者 0（不存在）——绝不返回别的会话的 key」；剥离伪造的 `[evt_...]` 前缀（只作用于 assistant 正文）。
- tool：`action: tmux not available (install: brew install tmux)`（tmux 是硬依赖）；「**不完整不等于空**」「**不推断原因**」「**字段是加法**」；声明区「**没有任何向量或嵌入参数**」；召回路径只看 `MemoryEngine().Capabilities().Vector`；MCP 失败「**以结果形态返回，而不是 Go error**」＋自纠清单；typed nil 不得注入；`memory_query` 的 `query_error`；`web_search` 与 `duckduckgo_search` 互补；「**一次 REMOTE 尝试至多重试一次**」；`cancel` 在 channel 关闭之后调用；工厂「只装配执行配置，不构造 agent」。
- tmux：静默≠假死（阈值 opt-in）；判活先看进程、不注入按键；通知三门 AND；假活重启保持同一 ID；真实尾帧；命中计数只增不减；命名会话不得静默双开；重挂 best-effort 且元数据目录必须私有；pipe 日志归档而非删除；错误必须可见且拒绝要可行动。
- examples：去重键分层（`ClientID` → `uid:FromUserID#sha256(text)[:16]`）；`sanitizeName` 白名单 `[A-Za-z0-9._-]`；`ExtractFilePaths` 六条规则；`selectSendFn` 按扩展名分派；「**声明过的，完成即达；未声明的，等用户在场**」；「**回执不递归**」。

## 四、改写约束

1. 上节锚点全部保留；被 `// 契约:` 指向的章节里，判定句、常量、字段名、拒绝形态逐字保留。
2. 判据表、设计决策（「为什么」）与「已知缺口」章保留；删的是过程叙事、重复解释、大段代码清单与过时实现细节。
3. 当期契约（`api/plugin` 四相位、`tool/{exec,mcp,remote,subagent}`、`runtime/task/approval`）以码面 doc 为准；
   历史机制压成「历史机制对照」并标注已被替代的边界，不与当期契约混述。
4. 结构性变更（原版归档、原位重写）不改变 `docs/wiki` 的历史快照语义：当期入口仍是
   [docs/wiki/README.md](../../wiki/README.md#当前架构速览) 与 [docs/api/](../../api/index.md)。
