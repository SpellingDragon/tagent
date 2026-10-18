# delta → 主规格合成清单（delta-sync-manifest）

> 派发单：**delta-spec 同步**（runtime-rebuild archive 前置；用户明示归档授权）。
> 语义：按 delta 条款头合成 —— ADDED 追加条款、MODIFIED 以 **delta 正文**替换同名条款（过往主规格不作金标准）、
> REMOVED 删除条款、RENAMED 改标题；同 capability 无主规格则**新建**。delta 原文只读，本单未改一字。
> 白名单：`openspec/specs/**`（主规格）＋ 本清单。未改产品代码、`changes/` 内 delta、`golden/`；**未 commit**。

## 1. 总量与守恒（可复跑）

| 项 | 数 | 取法 |
|---|---|---|
| delta 源文件 | **62** | `ls openspec/changes/runtime-rebuild/changes/*/specs/*/spec.md` (61) + `…/runtime-rebuild/specs/orchestration/spec.md` (1) |
| capability 数 | **62** | 一一对应（0 个 capability 有多个 delta writer） |
| ADDED 条款 | **71** | 全部追加成功（0 跳过、0 同名冲突） |
| MODIFIED 条款 | **57** | 全部按 delta 正文原位替换（0 缺失、0 歧义） |
| REMOVED 条款 | **0** | 62 份 delta 内无 `## REMOVED Requirements` 分节 |
| RENAMED 条款 | **0** | 无 `## RENAMED Requirements` 分节 |
| 主规格新建 | **1** | `runtime-inspection`（05-inspection，主规格原不存在） |
| 主规格改写 | **61** | 其余 61 个 capability 均在既有 `openspec/specs/<cap>/spec.md` 上原位合成 |
| 条款总数（本单 62 capability 子集） 前→后 | **322 → 393** | 守恒：322 + 71 − 0 = **393** |
| 条款总数（全库 `openspec/specs`） 前→后 | **601 → 672** | `git archive HEAD openspec/specs | tar -xO 2>/dev/null | grep -c '^### Requirement:'` → 601；`cat openspec/specs/*/spec.md | grep -c '^### Requirement:'` → 672（差 71，与 ADDED 71 相等） |

复跑命令（三个数字各自可独立复现）：

```sh
# 1) delta 文件数 = 62
ls openspec/changes/runtime-rebuild/changes/*/specs/*/spec.md \
   openspec/changes/runtime-rebuild/specs/*/spec.md | wc -l
# 2) delta 条款头分布 = 71 ADDED / 57 MODIFIED（无 REMOVED/RENAMED）
for f in openspec/changes/runtime-rebuild/changes/*/specs/*/spec.md \
         openspec/changes/runtime-rebuild/specs/*/spec.md; do
  awk '/^## (ADDED|MODIFIED|REMOVED|RENAMED) Requirements/{k=$2} /^### Requirement:/{print k}' "$f"
done | sort | uniq -c
# 3) 全库条款总数 前→后 = 601 → 672
git archive HEAD openspec/specs | tar -xO 2>/dev/null | grep -c '^### Requirement:'
cat openspec/specs/*/spec.md | grep -c '^### Requirement:'
```

第 4 项（逐 capability `before`/`after` 与逐条正文同文对账）需按标题分块比对，用一次性脚本执行：
对每个 capability 取 `git show HEAD:openspec/specs/<cap>/spec.md` 与现值，各自按 `### Requirement:` 分块（块边界为下一条 `###` 或下一个 `##` 头）：
`before_i + added_i − removed_i == after_i` 逐行成立，且每条 ADDED/MODIFIED 块正文与 delta 块逐字符相等（除 §5-1 声明的 1 条）。
本次运行结果：`before 总和 322`、`after 总和 393`、`problems=1`（即唯一已声明偏离）；逐块比对未发现其他差异。

## 2. 逐 capability 合成表（62 行）

`Δ` = ADDED / `M` = MODIFIED 命中数；`跳过` 列只登记**未写入主规格**的项（本单**无跳过**；唯一编辑性重排见 §5-1）。

| # | 域 | capability | 主规格 | 条款 前→后 | Δ added | M modified | 跳过 |
|---|---|---|---|---|---|---|---|
| 1 | 00-foundation | `architecture-guardrails` | 原位 | 10 → 12 | 2 | 2 | — |
| 2 | 01-state-binding | `config-hot-reload` | 原位 | 9 → 10 | 1 | 2 | — |
| 3 | 01-state-binding | `config-state-twinship` | 原位 | 2 → 5 | 3 | 2 | — |
| 4 | 01-state-binding | `per-call-subagent-overrides` | 原位 | 3 → 3 | 0 | 1 | — |
| 5 | 01-state-binding | `workflow-config-compilation` | 原位 | 6 → 7 | 1 | 2 | — |
| 6 | 02-execution | `async-task-execution` | 原位 | 11 → 12 | 1 | 5 | — |
| 7 | 02-execution | `event-metadata-contract` | 原位 | 6 → 7 | 1 | 0 | — |
| 8 | 02-execution | `event-type-unification` | 原位 | 4 → 4 | 0 | 1 | — |
| 9 | 02-execution | `failure-polarity` | 原位 | 6 → 7 | 1 | 0 | — |
| 10 | 02-execution | `persistent-event-loop` | 原位 | 20 → 21 | 1 | 4 | — |
| 11 | 02-execution | `request-budget-accounting` | 原位 | 4 → 5 | 1 | 0 | — |
| 12 | 02-execution | `subagent-turn-execution` | 原位 | 8 → 8 | 0 | 4 | — |
| 13 | 02-execution | `thinking-plan-event-classification` | 原位 | 3 → 3 | 0 | 1 | — |
| 14 | 02-execution | `unified-task-semantics` | 原位 | 4 → 4 | 0 | 2 | — |
| 15 | 03-history | `causal-chain-isolation` | 原位 | 1 → 1 | 0 | 1 | — |
| 16 | 03-history | `event-lifecycle` | 原位 | 5 → 5 | 0 | 1 | — |
| 17 | 03-history | `event-segment-store` | 原位 | 22 → 22 | 0 | 3 | — |
| 18 | 03-history | `event-sourced-projection` | 原位 | 10 → 10 | 0 | 2 | — |
| 19 | 03-history | `recall-protocol` | 原位 | 6 → 7 | 1 | 1 | — |
| 20 | 03-history | `rustviking-config` | 原位 | 2 → 2 | 0 | 1 | — |
| 21 | 03-history | `storage-lifecycle-completion` | 原位 | 4 → 5 | 1 | 0 | — |
| 22 | 03-history | `task-registry-rebuild` | 原位 | 4 → 4 | 0 | 1 | — |
| 23 | 03-history | `tool-output-interception` | 原位 | 3 → 5 | 2 | 1 | — |
| 24 | 04-capabilities | `action-tool-config` | 原位 | 3 → 3 | 0 | 3 | — |
| 25 | 04-capabilities | `adaptive-poll-scheduling` | 原位 | 6 → 8 | 2 | 0 | — |
| 26 | 04-capabilities | `approval-channels` | 原位 | 3 → 4 | 1 | 0 | — |
| 27 | 04-capabilities | `async-task-lifetime` | 原位 | 7 → 9 | 2 | 0 | — |
| 28 | 04-capabilities | `cross-session-delivery` | 原位 | 4 → 5 | 1 | 0 | — |
| 29 | 04-capabilities | `goal-tools` | 原位 | 2 → 3 | 1 | 0 | — |
| 30 | 04-capabilities | `mcp-call-tool` | 原位 | 4 → 5 | 1 | 0 | — |
| 31 | 04-capabilities | `mcp-discovery` | 原位 | 3 → 4 | 1 | 0 | — |
| 32 | 04-capabilities | `mcp-server-registry` | 原位 | 6 → 7 | 1 | 1 | — |
| 33 | 04-capabilities | `memory-curation` | 原位 | 3 → 4 | 1 | 0 | — |
| 34 | 04-capabilities | `recall-hybrid-fusion` | 原位 | 3 → 4 | 1 | 0 | — |
| 35 | 04-capabilities | `remote-agent-communication` | 原位 | 8 → 10 | 2 | 0 | — |
| 36 | 04-capabilities | `resident-session-continuity` | 原位 | 6 → 8 | 2 | 0 | — |
| 37 | 04-capabilities | `semantic-search` | 原位 | 5 → 6 | 1 | 0 | — |
| 38 | 04-capabilities | `tool-lifecycle-management` | 原位 | 2 → 3 | 1 | 1 | — |
| 39 | 04-capabilities | `tool-security-alignment` | 原位 | 3 → 4 | 1 | 0 | — |
| 40 | 04-capabilities | `unified-tool-registry` | 原位 | 6 → 7 | 1 | 1 | — |
| 41 | 05-inspection | `committed-event-attribution` | 原位 | 3 → 4 | 1 | 0 | — |
| 42 | 05-inspection | `degradation-behaviors` | 原位 | 1 → 2 | 1 | 0 | — |
| 43 | 05-inspection | `runtime-inspection` | 新建（Purpose 取自 delta，171 字） | 0 → 4 | 4 | 0 | — |
| 44 | 05-inspection | `self-telemetry-audit` | 原位 | 3 → 4 | 1 | 0 | — |
| 45 | 05-inspection | `trajectory-trace-correlation` | 原位 | 2 → 3 | 1 | 0 | — |
| 46 | 05-inspection | `turn-tracing` | 原位 | 4 → 5 | 1 | 1 | — |
| 47 | 06-learning | `decision-capture` | 原位 | 5 → 8 | 3 | 1 | — |
| 48 | 06-learning | `feedback-binding` | 原位 | 3 → 5 | 2 | 1 | — |
| 49 | 06-learning | `offline-training-export` | 原位 | 5 → 8 | 3 | 1 | — |
| 50 | 06-learning | `rl-feedback` | 原位 | 7 → 9 | 2 | 1 | — |
| 51 | 06-learning | `trajectory-recording` | 原位 | 6 → 8 | 2 | 2 | — |
| 52 | 06-learning | `trajectory-verification` | 原位 | 4 → 5 | 1 | 0 | — |
| 53 | 07-evolution | `evolution-evaluation` | 原位 | 5 → 7 | 2 | 2 | — |
| 54 | 07-evolution | `git-native-refine` | 原位 | 6 → 6 | 0 | 1 | — |
| 55 | 07-evolution | `meditation-agent-partition` | 原位 | 5 → 6 | 1 | 0 | — |
| 56 | 08-host | `example-local-file-delivery` | 原位 | 7 → 8 | 1 | 0 | — |
| 57 | 08-host | `example-rl-visibility` | 原位 | 3 → 5 | 2 | 0 | — |
| 58 | 08-host | `production-wiring` | 原位 | 5 → 6 | 1 | 0 | — |
| 59 | 09-acceptance | `race-gate-coverage` | 原位 | 1 → 1 | 0 | 1 | — |
| 60 | 09-acceptance | `resident-release-evidence` | 原位 | 8 → 10 | 2 | 0 | — |
| 61 | 09-acceptance | `wiki-code-sync` | 原位 | 10 → 10 | 0 | 1 | — |
| 62 | top | `orchestration` | 原位 | 2 → 6 | 4 | 2 | — |
| **合计** | — | **62** | 61 原位 + 1 新建 | **322 → 393** | **71** | **57** | **0** |

> 数字来源：每行 `after − before = added − removed` 逐行可验；合计行与 `verify.py` 的 `totals` 逐字段对齐。

## 3. REMOVED / 已删名处理（09.6 §3.3 + §6.6 口径）

### 3.1 REMOVED：0 条（与 09.6 §3.3 `removed_requirements=0` 一致）

62 份 delta 只有 `## ADDED Requirements`（47 份含）与 `## MODIFIED Requirements`（34 份含）两种分节，
**没有任何 `## REMOVED Requirements` / `## RENAMED Requirements`** 分节（复跑：
`grep -rh '^## ' --include=spec.md openspec/changes/runtime-rebuild/changes openspec/changes/runtime-rebuild/specs | sort | uniq -c`）
→ 主规格**无条款被删除**，故不存在“已删名条款按 REMOVED 处理”的适用对象。

### 3.2 09.6 §6.6 的 24 个已删/不存在名：**不删条款、不改原文，具名登记为已知残留**

这 24 个名字是**条款正文里引用的 API 符号名**（非条款标题），随 delta 正文合成进主规格；
按红线“已删名不凭空造条款”，本单**不改写 delta 正文、不新造条款、也不静默删条**，逐条具名登记如下（下表 `处数` 为对 delta 文件的独立复算，与 09.6 §6.6 逐行比对）：

| # | spec | 09.6 记 处数 | 本单复算 处数 | 一次性核对 | 名字（本单复算次数） |
|---|---|---|---|---|---|
| 1 | `action-tool-config` | 13 | 13 | 一致 | `TmuxMonitor`×3, `ActionProperties`×9, `NewTmuxMonitor`×1 |
| 2 | `async-task-execution` | 4 | 4 | 一致 | `Lifetime`×1, `LifetimeOf`×1, `TaskSpec`×1, `TmuxMonitor`×1 |
| 3 | `causal-chain-isolation` | 2 | 2 | 一致 | `FullEvent`×2 |
| 4 | `event-segment-store` | 1 | 1 | 一致 | `InMemoryStore`×1 |
| 5 | `goal-tools` | 1 | 1 | 一致 | `GoalRequiredFor`×1 |
| 6 | `mcp-discovery` | 1 | 1 | 一致 | `MCPRegistry`×1 |
| 7 | `mcp-server-registry` | 1 | 1 | 一致 | `MCPRegistry`×1 |
| 8 | `rustviking-config` | 3 | 3 | 一致 | `MemoryConfig`×2, `MockRustVikingClient`×1 |
| 9 | `subagent-turn-execution` | 1 | 1 | 一致 | `AgentToolWrapper`×1 |
| 10 | `thinking-plan-event-classification` | 3 | 3 | 一致 | `ExtractEventType`×3 |
| 11 | `tool-lifecycle-management` | 11 | 11 | 一致 | `GetPlainToolFactory`×1, `GetToolAgentFactory`×2, `RegisterBuiltinTools`×5, `RegisterPlainTool`×1, `RegisterToolAgent`×2 |
| 12 | `tool-output-interception` | 4 | 4 | 一致 | `AgentToolWrapper`×1, `NewTagentAgent`×1, `OutputLimitTool`×2 |
| 13 | `tool-security-alignment` | 2 | 2 | 一致 | `RestartSession`×2 |
| 14 | `unified-tool-registry` | 7 | 7 | 一致 | `GetPlainToolFactory`×1, `RegisterBuiltinTools`×3, `GetRegistry`×2, `ValidateToolAccess`×1 |
| **合计** | **14 份 spec** | **54** | **54** | 全部一致 | **24 个名字** |

处置口径（登记而非改写）：条款照 delta 正文入主规格 → 主规格里这 24 名的“代码侧是否已删”属 **09.6 §6.6 未裁项**，
需在规格正文修订（另单/另授权）时统一改名，**不在本单白名单内**（本单禁改 delta，且改写会偏离“以 delta 为准”）。

## 4. 与 source of truth 的对账（锚4）

| 判据 | 结果 |
|---|---|
| 62 delta 文件 ↔ 62 capability，无一对多 | PASS |
| ADDED 71 = 逐 capability `Δ` 列求和 = 71 | PASS |
| MODIFIED 57 = 逐 capability `M` 列求和 = 57 | PASS |
| 条款总数（62 capability 子集） 322 + 71 − 0 = 393 = 合成后实测 | PASS |
| 全库条款总数 601 + 71 = 672 = 合成后实测 | PASS |
| 每条 ADDED/MODIFIED 条款正文与 delta **逐字符相同**（除 §5 声明的 1 条重排） | 71/71 + 56/57（`problems=1`） |

## 5. 偏离与自行决策（申报）

1. **`trajectory-recording` / 「Conversion to AReaL SFT dataset」正文重排（唯一内容级偏离）**：
   delta 以引用块 `> 标题沿用旧名（delta 寻址）：语义已改为中性 SFT 消费合同。` 开头，
   `openspec validate --strict` 对主规格只取条款正文**首个非空行**判 SHALL/MUST（`dist/core/parsers/markdown-parser.js:parseRequirements`），
   引用块开头导致 `Requirement must contain SHALL or MUST keyword`（ERROR）。
   处置：把该引用块移到首个规范性段（含 `系统 SHALL 提供…`）之后，**文字一字未改、语义未变**，仅段序调整。
   依据：锚1 为主规格合法性硬门；不改 delta 原文（红线），故在主规格侧做等价重排。
2. **`runtime-inspection`（新建）Purpose 直接取自 delta 的 `## Purpose`（171 字）**：
   若留 `TBD` 会触发 `--strict` 的 `Purpose section is too brief (<50)`（WARNING→失败）。取 delta 自身 Purpose，不新造内容。
3. **29 份 delta 的 `## Purpose` 未并入既有主规格**：其文本是 delta 域内说明（形如“本 delta 要求…”），
   并进主规格 Purpose 会污染主规格语义；既有主规格 Purpose 原样保留。
4. **ADDED 追加位置 = `## Requirements` 分节末尾**（不重排既有条款），MODIFIED 原位替换（保序）。
5. **`check_runtime_plan.py` 在合成后必然红**（详见 §6）：该脚本是 **archive 前**的门（要求 ADDED 尚未进主规格、
   `coverage.md` 能力集 == 主规格能力集）。本单按任务要求“不破计划契约”，用 `--main-specs-root` 指向 HEAD 快照复跑证明 **delta 侧契约 0 变化**。

## 6. 四条锚的机械结果

| 锚 | 命令 | exit | 关键输出 | 判定 |
|---|---|---|---|---|
| 锚1 | `openspec validate --all --strict --no-interactive` | **0** | `Totals: 122 passed, 0 failed (122 items)` | PASS |
| 锚2 | `python3 scripts/check_runtime_plan.py` | **1** | `added_requirements=71 / modified_requirements=57 / removed_requirements=0 / spec_files=62 / requirements=128`；72 ERROR = 71×`ADDED … already exists in main spec (collision; use MODIFIED)` + 1×`coverage capability set mismatch: missing=['runtime-inspection']` | **结构性红（非回归）**：见下 |
| 锚3 | `go run ./scripts/codetools doc-refs docs/wiki docs/*.md` | **0** | `43 citation(s) resolve at their document's snapshot commit`；`no dangling file citations` | PASS（未引入新 dangling） |
| 锚4 | 本清单 §4 对账 | — | 71/57/0 守恒、322+71=393 | PASS |

**锚2 定向说明（必须由上级裁定口径）**：`check_runtime_plan.py:645-660` 规定 “ADDED requirement 一旦同名存在于主规格即报错”，
`check_coverage()`（:679-700）规定 `coverage.md` 能力集必须等于主规格能力集。二者都是 **archive 前置门**的语义：
合成（archive 实质动作）一完成，这两条判据必然同时变红。机械证明本单未破契约：

```sh
git archive HEAD openspec/specs | tar -x -C /tmp/deltasync/presync
python3 scripts/check_runtime_plan.py --main-specs-root /tmp/deltasync/presync/openspec/specs
# → exit 0, RESULT: ok（delta 侧 / 任务图 / 复选框 / 编排面与 HEAD 完全一致）
```

即：72 条 ERROR 的**每一条**都是“合成已发生”的指纹，而非 delta 或产品面被改动。

## 7. 本单**未**闭合的事项（如实声明）

- 本单只做 **delta → 主规格合成**（archive 的实质内容）。`openspec archive` 命令**未执行**（另行授权/另单）。
- 09.6 §6 的未裁项**一律未动**：golden 4 drift、Rollback 根面出口缺失、资源门 09.5、结构 16 文件超 500、
  11 个 `M` 能力零 delta、24 个已删/不存在名、消费者口数 69→74/75 漂移。
- 09.6 的 **B 轴（docs/wiki 全历史篇）** 与 **C 轴（`docs/api` 27 stale + 13 分歧 + 33 缺页）** 不在本单白名单，未触碰。
- 因此：**主规格已与 delta 对齐，但不等于 09.6 已闭合**；09.6 的勾选仍应由其自身判据决定。

