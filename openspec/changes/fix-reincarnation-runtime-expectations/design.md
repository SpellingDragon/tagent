# Design

## 缺陷 A：pruneTerminal nil detector

**根因链**（实证）：
`RebuildTaskRegistry`（`agent/task_record_sink.go:275`）→ `RestoreTask`（`task_manager.go:587`）
构造 Task 时**不设 detector**（detector 全工程仅赋值于 `Spawn` L420 与 `Resume` L898）→
该任务经 `reconcileDetached`/`reconcileZombies` 探针裁决置终态 + `settledAt` →
超 `defaultTerminalTTL`（L339 = 2min）→ `pruneTerminal` victims 循环 L769
`t.detector.Cancel()` 无判空 → panic。

**爆炸半径**：`pruneTerminal` 由 `List()`（`injectLiveTaskBoard`，**每轮 before-model 回调**）
与 `Spawn()`（**每次 ActionTool 调用**）调用。

**修法**：持 `t.mu` 读 detector（同时守 nil 与 `Resume` swap 竞态），nil 时跳过 `Cancel`，
条目仍照常回收。与既有 `Cancel()` L795 写法对齐。

## 缺陷 B：plan provider 回退

`resolveAgentModel`（`wiring.go:23-40`）：agent 的 `Provider` 为空即回退 `cfg.Provider`（全局
`deepseek`），而 model 用的是该 agent 覆盖的 `glm-5.3` → 端点/模型不匹配 → 400。
`AgentConfig.Provider`（`config.go:276`）本就是为此设计的 per-agent override，plan 段漏写。

## 缺陷 C：转世运行预期（设计缺口）

**三条恢复路径的实测结果**：

| 路径 | 机制 | 2026-09-13 实测 |
|---|---|---|
| 转世通报 | 直读 WAL 尾部 N 条事件 + 元数据，独立于 projection | ✅ 正常（12 条事件注入） |
| projection 重建 | 以 **compaction 快照事件**为锚点 | ❌ no-op：`latestCompactionKey()==0` |
| 任务看板重建 | 事实链 spawn − settle 折叠 | ✅ 恢复出 suspect 任务（但 detector=nil → 触发缺陷 A） |

**预期缺口**：上一世未触发压缩时无 fallback 锚点 → 空投影 → agent 感觉"没有过去的记忆"。
设计上应提供降级路径：无快照时按最近 N 条事件重建最小投影。

**任务看板回收缺口**：nil-probe（plan/subagent）任务在两处判据中都被豁免
（`reconcileZombies`「Nil-probe tasks (subagents) are never touched」+ `isTerminalExpired`
对 running/suspect 恒 false）→ 永不回收、永久挂看板、按 dedup key 挡住同名 re-spawn。
实测：5 个 suspect plan 任务最久已挂 1h41m。

## 验收

- 缺陷 A：`go test ./agent/task/` 含回归用例；换装后连续消息+工具调用无 panic。
- 缺陷 B：plan 工具调用返回规划结果而非 400。
- 缺陷 C：成文预期文档 + fallback 重建设计 + 回收策略设计。
