# 任务（只编排；孙任务见 changes/<域>/tasks.md）

```mermaid
graph LR
  A1A[symptom anchor RED] --> A2[loop ready increment]
  A2 --> A3[assembly wiring check]
  A3 --> A4[host Invoke semantics]
  A4 --> A5[spec delta + anchor migration]
  A5 --> B1[meditation anchors]
  B1 --> B2[driver activity gate]
  B2 --> B3[meditation spec delta]
  B3 --> F[F closeout]
```

头部三行：只编排 / 启动门=本域无跨 change 前置（规格现条款在 main）/ 波次仅为汇报分组，解锁=依赖入 main。

- [x] 01 域 decision-boundary —— changes/01-decision-boundary/（叶 A1–A5）
- [x] 02 域 meditation-activity —— changes/02-meditation-activity/（叶 B1–B3）
- [x] 03 域 environment-block —— changes/03-environment-block/（叶 E1–E4）
- [x] F1 契约零改动断言（**F4 归档前**）—— 验证：`git diff <base>..HEAD --stat -- openspec/changes/unified-intake/changes/*/specs` 输出**恰好三份 delta**（01 域、02 域、orchestration）；**F4 归档后**另跑 `git diff --stat -- openspec/specs` 核对恰好三个既有 capability 各一处落地，其余主规格零改动（S-1：delta 在归档前不并入 specs/）
- [x] F2 全量门禁 —— 验证：`bash scripts/lint.sh` exit 0；`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 1 --out $TMP/eis` verdict=match；`python3 scripts/gates/check_event_integrity_mutations.py --all --json --out $TMP/mut` gate.ok=true；双示例 module `go test ./... -count=1` 全 ok
- [x] F3 DoD 核验 —— 验证：`go test ./tests/integration/execution -run '^TestMessageReachesTheNextDecisionOfAInFlightTurn$' -count=1` 绿；`go test ./tests/integration/evolution -run '^TestMeditationYieldsBesideAnActiveSession$' -count=1` 绿；完整回滚两域改动后同两条命令**必红**（两次证据入 notes）
- [x] F4 archive+导览 —— 归档于 `openspec/changes/archive/2026-10-29-unified-intake/`（CLI `openspec archive` 落地根 specs/ 的 orchestration「三段验收」，三份域 delta 因 CLI 只读 change 根 specs/ 而手工并入权威规格）；域→证据：01 域 A1–A5 → [run-notes](run-notes.md) 的「F3 DoD 核验」（A 锚绿原文 + `41580639` 红锚原文 + 行为回滚探针 FAIL），02 域 B1–B3 → 同节（B 锚绿原文 + `bdb88238` 红锚原文 + 删判据①后的 FAIL），03 域 E1–E4 → 同文件「F2 全量门禁」（场景契约 `verdict=match` run_id `93e3413f-c74c-4aa7-b2a6-21d32a38c3c8`、变异门 `gate.ok=true`、`examples/*` 双 module 全绿），门禁与残余见同文件「F2 全量门禁」与「残余」两节
