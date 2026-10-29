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

- [ ] 01 域 decision-boundary —— changes/01-decision-boundary/（叶 A1–A5）
- [ ] 02 域 meditation-activity —— changes/02-meditation-activity/（叶 B1–B3）
- [ ] 03 域 environment-block —— changes/03-environment-block/（叶 E1–E4）
- [ ] F1 契约零改动断言（**F4 归档前**）—— 验证：`git diff <base>..HEAD --stat -- openspec/changes/unified-intake/changes/*/specs` 输出**恰好三份 delta**（01 域、02 域、orchestration）；**F4 归档后**另跑 `git diff --stat -- openspec/specs` 核对恰好三个既有 capability 各一处落地，其余主规格零改动（S-1：delta 在归档前不并入 specs/）
- [ ] F2 全量门禁 —— 验证：`bash scripts/lint.sh` exit 0；`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 1 --out $TMP/eis` verdict=match；`python3 scripts/gates/check_event_integrity_mutations.py --all --json --out $TMP/mut` gate.ok=true；双示例 module `go test ./... -count=1` 全 ok
- [ ] F3 DoD 核验 —— 验证：`go test ./tests/integration/execution -run '^TestMessageReachesTheNextDecisionOfAInFlightTurn$' -count=1` 绿；`go test ./tests/integration/evolution -run '^TestMeditationYieldsBesideAnActiveSession$' -count=1` 绿；完整回滚两域改动后同两条命令**必红**（两次证据入 notes）
- [ ] F4 archive+导览
