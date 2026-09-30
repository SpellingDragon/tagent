
# `tests`/`tool`/`examples` 隔离清单（122 轮 sweep 删 865 行中命中判据词的 103 行）

- `examples/wechat-bot/dedup.go:61` // post-mortem instead of silently overwriting it.
- `examples/wechat-bot/file_delivery_test.go:12` // 路径注入错误，用于验证"单文件失败不阻断其余文件"的隔离行为。
- `examples/wechat-bot/file_delivery_test.go:30` failPath string // 若某 Send* 命中此 path，返回错误
- `examples/wechat-bot/file_delivery_test.go:308` // DeliverFiles 不返回错误（单文件失败仅记日志），故此处不应 panic/返回错误
- `examples/wechat-bot/file_intake.go:61` Rejects    []string // 面向用户的拒绝/失败原因（不注入 agent）
- `examples/wechat-bot/main.go:143` // (redirect-disabled semantics, cold-eyes W-2: the guard must fail CLOSED
- `examples/wechat-bot/main.go:148` // tagent resolves provider endpoints/API keys from Config. The application only
- `examples/wechat-bot/main.go:172` // 2a. Global fallback model (for sub-agents without explicit model/provider).
- `examples/wechat-bot/main.go:222` // and yaml-only model changes silently require a manual restart.
- `examples/wechat-bot/main.go:222` // cc21241): YAML compress.summary is the only summary path.
- `examples/wechat-bot/main.go:227` // watcher is never installed (tagent.go: `if rc.configPath != ""`)
- `examples/wechat-bot/main.go:245` // compose / D5 consume-marker). One-shot goroutine, never crashes the bot.
- `examples/wechat-bot/main.go:254` // about failed restart attempts instead of staying blind.
- `examples/wechat-bot/main.go:258` // 5. Start persistent event loop (the only execution mode for top-level tagent)
- `examples/wechat-bot/main.go:268` // 30x 的每一跳目标 host 必须在 allowlist 内，越界跳转以明确错误终止。
- `examples/wechat-bot/main.go:269` // 初始 URL——LLM client 携带逐跳 CheckRedirect（guardedClient），端点自身
- `examples/wechat-bot/main.go:278` // rl.NewHTTPServer 硬化构造——Addr 显式携带（空 Addr 静默绑 :80 事故）+
- `examples/wechat-bot/main.go:301` // endpoint — without a token it must not be reachable from off-host.
- `examples/wechat-bot/main.go:316` // resident-readiness-plan 5.3：动态端点重定向默认禁用；RL 训练部署显式开启
- `examples/wechat-bot/main.go:320` // — the library never registers global signals itself).
- `examples/wechat-bot/main.go:341` // Authentication + loopback fail-closed guard (implementation-hardening 3.3):
- `examples/wechat-bot/main.go:343` //    Without this, the tracer is noop (zero overhead).
- `examples/wechat-bot/main.go:348` srv.Addr = listenAddr // :80 事故教训：fallback 必须同步显式 Addr，绝不留空
- `examples/wechat-bot/main.go:352` // S1 fix (systemic-α): a Warn-and-exit goroutine is a silent death —
- `examples/wechat-bot/main.go:358` return // graceful shutdown or clean stop: don't retry
- `examples/wechat-bot/main.go:387` //     dispatching by event type. This ensures outputCh never fills up
- `examples/wechat-bot/main.go:421` // 回合的事件可能不带 meta_chat_id，若无此回退锚点，其用户可见回复会被静默
- `examples/wechat-bot/main.go:423` // consumers never read raw StateDelta keys. (unified-event-projection D4)
- `examples/wechat-bot/main.go:477` // output events are internal and must never reach the user chat.
- `examples/wechat-bot/main.go:489` // handled above; DeliverFiles only sends files. Per-file failures
- `examples/wechat-bot/main.go:495` // most recent active user session instead of dropping the reply.
- `examples/wechat-bot/main.go:501` // Surface the real execution error instead of the framework's
- `examples/wechat-bot/main.go:510` // Degenerate empty final: nothing to deliver. Drop instead of
- `examples/wechat-bot/main.go:533` // Error: log only, don't send to user.
- `examples/wechat-bot/main.go:602` // Unknown trigger source: log only
- `examples/wechat-bot/main.go:61` // task 结算降级的 "task-unstamped"（及其它未识别值）一律扣留：未知不得
- `examples/wechat-bot/main.go:61` // 仍在 dispatch switch（log-only 扣留）——既有双层语义不变。
- `examples/wechat-bot/main_test.go:136` // still deliver (anti-overreach guard).
- `examples/wechat-bot/main_test.go:14` // restart. No test here may touch a real channel (guard below enforces it).
- `examples/wechat-bot/main_test.go:140` // Real user turns are stamped at the RunFlow forwarding block — must
- `examples/wechat-bot/main_test.go:146` // stays with the dispatch switch (meditation/error are log-only there).
- `examples/wechat-bot/main_test.go:86` // Fresh process: a brand-new map seeded only from the durable file.
- `examples/wechat-bot/reincarnation_notice_test.go:101` // nil for a query without PartitionIDs -> zero partitions scanned -> empty
- `examples/wechat-bot/scripts/verify_large_file.go:73` // ---- 新流式 API：边下边解密边写盘 + MaxSize 防线 ----
- `examples/wechat-bot/system_alert.go:14` // agent completely blind because the verdict only landed in restart.log.
- `examples/wechat-bot/system_alert.go:14` // gate now withholds meditation-stamped output, which would silently eat
- `examples/wechat-bot/system_alert.go:14` // is only seen at next boot; in-process hot-reload failures are covered by
- `examples/wechat-bot/system_alert.go:43` return // no pending alert: silent no-op
- `tests/compression_test.go:104` // On the second turn, the request messages must be fewer than the raw
- `tests/integration_test.go:601` // Real LLM integration tests for edge cases found in production traces.
- `tests/integration_test.go:61` // Context cancelled — drain any remaining events with a grace period
- `tests/invariants_test.go:168` // must always be reduced.
- `tests/invariants_test.go:169` // Use ContextCompressor instead of the deleted Compactor.
- `tests/invariants_test.go:173` // that existing MemoryStore events are never modified, not that refs
- `tests/invariants_test.go:185` // must not be modified.
- `tests/llm_contract_test.go:253` // 期待：发起真实 ToolCall（首选）或纯文本回复；文本部分绝不含伪调用语法。
- `tests/llm_contract_test.go:26` // contracts_llm_test.go — 真实 LLM 契约守护套件。
- `tests/llm_contract_test.go:26` // 往返一致性。单元/契约测试只能锁定工程侧（见 agent/event_keys_contract_test.go），
- `tests/llm_contract_test.go:26` // 模型侧的抄写行为只能用真实 LLM 验证——这正是 event_keys hex 断裂静默存活
- `tests/llm_contract_test.go:303` // 迷航特征：用 ../ 补偿"还在 articles/2026"的错误认知。
- `tests/llm_contract_test.go:377` // 反自旋核心断言：模型不得发起 sleep/wait 式等待命令。结束回合（无工具
- `tests/llm_contract_test.go:481` // Deployment-related keys must be selected; the weather key must not.
- `tests/llm_contract_test.go:486` //      发现指引如实、直调返回真实搜索结果、错误自纠携带正确清单。
- `tests/llm_contract_test.go:620` // --- 3. 自纠:错误工具名(文档风格驼峰)应返回正确清单 ---
- `tests/llm_contract_test.go:796` // plan must ask clarifying questions, not fabricate a finished plan.
- `tests/llm_contract_test.go:820` // must refine using EXACTLY these specifics (distinctive markers below).
- `tests/llm_contract_test.go:823` // Round 1 assertion: the clarification must (a) explicitly enumerate WHICH
- `tests/llm_contract_test.go:864` // plan incorporated the supplied specifics (distinctive markers that only
- `tests/llm_contract_test.go:975` // 结论断言：hy3 必须至少有一种配置能触发思考，否则说明思考模式完全不可用。
- `tests/offline_bench/offline_bench_test.go:313` // residue (dirty tmp orphans must be zero after clean commits).
- `tests/offline_bench/offline_bench_test.go:397` "sync_barriers":  after[6] - before[6], // §9.3: probes must show whether reads re-flushed
- `tests/offline_bench/offline_bench_test.go:568` // (1) Fail-before discriminator: the wrapper must have been reached.
- `tests/offline_bench/offline_bench_test.go:60` // facts plus the effective bench configuration, so a report can never be
- `tests/plan_agent_test.go:324` // A 级以 spec(op="status") 结构自检收尾，禁止 validate（无 specs deltas 必然失败）。
- `tests/plan_agent_test.go:331` // 核心断言：create 必须产出合规 openspec change（proposal.md + tasks.md），
- `tests/plan_agent_test.go:372` // 关键：proposal.md 必须创建（不再是裸 tasks.md 目录）
- `tests/resident_e2e_test.go:22` // keeps only the first line — the tail survives ONLY through store recall),
- `tests/resident_e2e_test.go:221` // partial must self-report WHERE it lost ground — never claim full.
- `tests/resident_e2e_test.go:323` // ---- acceptances: batch A+B as ONE unit; C arrives only AFTER the A+B
- `tests/resident_e2e_test.go:372` // claim: an envelope that never drains still fails the test.
- `tests/resident_e2e_test.go:373` // unlinked") at its own completion point instead of racing it — not a weaker
- `tests/resident_e2e_test.go:386` // item ended its lifecycle as processed-cleaned, nothing silently lost).
- `tests/soak_test.go:194` // active segment, retune thresholds (harness hook — a soak run never
- `tests/soak_test.go:23` // artifacts the next verify process must read through.
- `tests/soak_test.go:242` // The reopen must ALSO read through the compaction artifacts the previous
- `tests/upgrade_rollback_drill_test.go:144` // Read-only: the diagnosis never re-keys — every pid mapping is unchanged.
- `tests/upgrade_rollback_drill_test.go:15` //     (registerStoreOwner fails closed at build; rename is the only fix)
- `tests/upgrade_rollback_drill_test.go:71` // a receipt without one, and without the credential matching the reservation).
- `tests/upgrade_rollback_drill_test.go:94` // Crash mid-processing: claim one, never receipt/ack, then close.
- `tests/upgrade_rollback_drill_test.go:96` // as outstanding — the rollback gate must therefore refuse a downgrade.
- `tool/mcp/call.go:165` // （避免名称错误误标 server 退化）。ctx 取消不计（与 M1 一致）。
- `tool/mcp/call.go:166` // 信号）→ 上报 DepMCP 退化；toolNames 非空则是 tool 名错误（server 健康），不上报
- `tool/mcp/call.go:181` // 传输/连接失败上报（区分需 trpc MCP 层错误类型）为进阶方向，见 review M1。
- `tool/mcp/call.go:183` // DepModel 一致，M1）。注：err 含传输错误 + tool 级业务错误，MVP 均计入 DepMCP；仅对
- `tool/mcp/call_test.go:114` // Nil registry falls back to the empty stub (factory must not fail).
- `tool/mcp/call_test.go:152` // must fire BEFORE the lookup, so an empty registry still proves the path.
- `tool/mcp/registry_test.go:205` // Config change without the manual entry → manual survives.
- `tool/mcp/registry_test.go:235` // Access right after Seed must not rebuild from the just-seeded file.
- `tool/spec/openspec.go:147` // closing step instead of letting it retry into a dead end
- `tool/spec/openspec.go:150` // A-level plans (proposal+tasks only) can NEVER pass validate — it
- `tool/spec/openspec.go:17` dir string // working directory (must contain openspec/)
- `tool/spec/openspec.go:93` // --tools none: only create core dirs (tagent isn't in openspec's
- `tool/task/task_tools.go:185` // 未纳管（detector 已被 Spawn 取消），绝不能报「已重跑」。

## 定级结论（123 轮，探针与 85 同一实现）

本次对这两份账共 **212 条**跑探针：**203 条原文件仍留痕**（判据由代码或现存注释承载），**9 条无文本痕迹**。9 条逐条人工判定，依据是**读周边上下文是否连贯**＋判据是否落在代码里，而不是只看计数：

- 全部 9 条均为**被删自由注释块里的句子碎片**，删除后周围注释与代码**读起来连贯、无悬空句**（已抽查 `examples/wechat-bot/dedup.go`、`tests/invariants_test.go` 等处）。
- 其中判据并未丢失：`dedup.go` 的"不静默覆盖、留作事后分析"落在 `os.Rename(s.path, s.path+".corrupt")` 这行代码上；`tui_timeout_test.go` 的"非交互会话静默≠假死"已在 `docs/wiki/tool/tmux-action.md#quiet-vs-dead`；其余为分节横幅（`==== Task 5.5 ====`）与测试内说明碎片，无契约内容。
- 结论：**本两份账无待还原的真缺口**。

⚠️ 同时纠正我自己的一个检测缺陷：第一版"孤儿 doc"探针检查的是**注释组内任意一行**，因而误报 505 条（多行 doc 的续行本就可能以 the/and 开头）；改为只看**组首行**后为 **0**。⇒ 教训：报出任何"损伤计数"之前，先确认探针的判据位置正确；否则会把检测器缺陷说成代码缺陷。

## 124 轮 tdoc 摘除的碎片（命中判据词 0 行，与上批合并统计）

