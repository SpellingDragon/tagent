# 04-contract-gates：契约与门禁的收口（无新能力）

## Why

01/02 域改了行为之后，仓库里会同时留下**两套说法**，而下一个人读到的往往是旧的那一套：

1. **主规格缺条款**。`openspec/specs/persistent-event-loop/spec.md` 现在只写到"通知驱动原 invocation 的下一步 decision"，没有任何一句说"哪些事件不得成为说话素材""失败必须达至用户面"。01 域让受理回执只剩 `ok`、让 progress 不进 intake（`runtime/agent/loop.go`）；02 域把真失败的原始错误直投用户（`host.go`）。行为改了、契约没改，等于把"为什么不能把回执喂给模型"这条知识留在实现里。
2. **既有场景断言旧语义**。`golden/runtime/scenarios.json` 的 S15 写着 `progress_reaches_decision: true`，标题写"progress 回调抵达下一次决策"；`tests/integration/acceptance/scenario_s15_test.go` 按"progress 作为 user 消息被带进下一次请求"计算该观察值；`tests/integration/execution/progress_notification_test.go` 的子测试名直接叫 "a progress notification enters the decision that issued the call"，并在模型请求里找 progress 正文。01.3 落地后，后两者会**在 CI 的 `go test ./... -short` 里变红**，前者变成一份说假话的金样（它仍把旧父测试名放进 `anchor_tests`）。
3. **症状锚没有护栏**。00 域的三条症状测试只在 `scripts/preflight.sh` 的 `-run TestLocalSmoke` 集合里跑（本地），CI **完全不执行**：`examples/wechat-bot` 是独立 module，而 `-tags smoke` 在 `.github/workflows/ci.yml` 里一次都没出现。下一次重构可以静默删掉三条测试而全绿——这正是 R33 的另一面。
4. **plan 门禁不认本变更的形状**。`scripts/check_runtime_plan.py` 把 `EVENT_DOMAINS`（六域）写死在七处；本变更是五域，任何按本变更形状产出的 `task-graph.json` 都会被判成"不是六域之一"，而改写成十域旧名又会被 `test_legacy_ten_domain_rules_are_not_relaxed` 拒绝。

本域是 W2 的收口：**只改契约、金样与门禁，不改产品行为**（行为在 01/02，本域一行生产代码都不碰）。

## What Changes

- **4.1 主规格三场景**：以 MODIFIED delta（`specs/persistent-event-loop/spec.md`）全文替换两条既有 requirement——「task_settled 作为一等事件触发回收 turn」追加"受理回执不构成模型材料""progress 只落 fact 不进 intake"两个场景，「模型入口错误以失败极性呈现」追加"失败回合必达用户告知"；既有正文段与既有 11 个 Scenario 逐字保留，**不新增 requirement**。
- **4.2 progress-intake 锚逐条裁决**：对九条涉 progress 呈现的锚（合同 / 金样 / 三条测试 / 主规格）各给"保留 / 修订"决定与理由（design D2）。`golden/runtime/event_integrity_scenarios.json` **零改动**——它唯一的 progress 子场景是 fact 层断言（A1）；真正过期的是 v1 金样 S15 与两条测试。
- **4.3 症状测试入 CI**：新增独立 job `symptom-anchors`，在每次 PR 与 main/dev push 上跑 `go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom'`，无 paths filter、无 job 级 `if`、无 `continue-on-error`；并在 `scripts/test_check_runtime_workflow.py` 加一条 shape 守卫，防止这三样被后续"优化"加回来。
- **4.4 domain_profile 适配五域**：新增 `event-semantics` profile（五域元组），把域元组与 profile 名从 `run_event_profile` 的七处硬编码参数化；六域常量与旧十域规则**一个字符不放宽**（既有六域文案逐字保留，作为负锚守卫）。

## Capabilities

### New Capabilities

无。本域不引入新语义，只把 01/02 已交付的语义写进既有能力，并把守护它们的门禁接上。

### Modified Capabilities

- `persistent-event-loop`：见 `specs/persistent-event-loop/spec.md`（两条 MODIFIED：各追加一段正文与场景，不新增 requirement）。
- `event-integrity`（golden 合同）：**核对后确认零改动**（design D2 的 A1），原因写入 4.2 的执行记录——这是核对结论，不是跳过核对。

## 边界与依赖

- **前置（硬）**：4.1/4.2 的验收对象是"01/02 行为落地后的语义"，4.1/4.2/4.3 MUST 在 01 域三叶与 02 域三叶勾选**之后**启动；4.4 是 scripts 独立簇，可提前。
- **依赖 00 域（测试名与文件）**：`TestLocalSmokeSymptomAcceptedSilent` / `TestLocalSmokeSymptomSettledOnce` / `TestLocalSmokeSymptomFailureNotice`，落 `examples/wechat-bot/smoke_test.go`（`//go:build smoke`）。本域只引用函数名与文件，**不改测试体**；测试体任何改动都要回 00 域。
- **依赖 01 域**：1.1（`admissionText`→`ok`）、1.3（progress 既不 park 也不进 transcript，并已把 `TestBackflowNotificationLanes` 两个 progress 子测试反向改写）。
- **依赖 02 域**：2.1（`deliverFresh` 直投原始错误）、2.3（示例层文案收敛，使 G3 的"恰一条"成立）。
- **上报项 P-04（本域对派发单的扩面，需编排者确认）**：一级 proposal 的 Impact 只列了 `golden/runtime/event_integrity_scenarios.json`；但该文件被 F1 冻结且实测无需改动，真正过期的是 **v1 金样 `golden/runtime/scenarios.json` 的 S15 段**。4.2 的写集因此含 `golden/runtime/scenarios.json`（S15 的 `title` / `anchor_tests` / `observations` 三处）与两个既有测试文件。若不批，4.2 只能停在"清点表 + 结论"，S15 的假话与两条测试的红由 F2 全量门禁暴露，本域不得宣称收口。
- **禁止事项**：① 不改 `golden/runtime/event_integrity_scenarios.json`（F1）；② 不改归档 `openspec/changes/archive/**/task-graph.json`（合同与它双向严格互校，动它就是改历史证据）；③ 不放松 `EVENT_ROLES`（`positive/negative/consumer/control`）与 `EVENT_SCOPES`（`event-short/event-long`）的既有值；④ 不动 S01–S14 与既有 `TestLocalSmoke*` 用例的期望；⑤ 不改 01/02 已交付的行为代码（`runtime/agent/loop.go`、`host.go`、`examples/wechat-bot/wechat.go`）；⑥ 不新增 CI paths filter、job 级 `if`、`continue-on-error`；⑦ 不为本案新建 `task-graph.json`（本变更无图，见 design D4）。

## Impact

- 规格：`openspec/specs/persistent-event-loop/spec.md`（+2 正文段、+3 scenario，不新增 requirement）。
- 金样：`golden/runtime/scenarios.json`（S15 三处，P-04）。
- 测试：`tests/integration/acceptance/scenario_s15_test.go`（注释 + 见证断言）、`tests/integration/execution/progress_notification_test.go`（父测试改名 + 反向改写）。
- CI / 脚本：`.github/workflows/ci.yml`（+1 job）、`scripts/test_check_runtime_workflow.py`（+1 shape 守卫）、`scripts/check_runtime_plan.py`（+1 profile、参数化七处）、`scripts/test_check_runtime_plan.py`（+1 fixture、+2 测试）。
- 运行成本：新 job 跑三条 smoke 宿主（00 域估单条 30–60s；`-race` 下 job 量级 3–5min，`timeout-minutes: 20` 留约 4× 余量），不与长跑矩阵同波（R32）。
- 本轮仅落 OpenSpec 文档：不改产品代码、测试、CI、主规格与金样，不提交。

## 规划入口

- 决策、清点表、形态选择与数字溯源：[design.md](design.md)
- 四叶账目与三锚：[tasks.md](tasks.md)
- 主规格 delta：[specs/persistent-event-loop/spec.md](specs/persistent-event-loop/spec.md)

## 落定回写（D6 实施，2026-10-25）

上文「What Changes」是规划时形态，实施按一级 D8-5 与一级「待裁项裁定」落定，差异如下（读取时以后者为准）：

- **4.1**：MODIFIED 两条既有 requirement → **`## ADDED Requirements` 一条新 requirement「循环面的模型感知边界」+ 三场景**（D8-5）。主规格 69→72 scenario，新 requirement 恰 1 处；细节引用 `quiet-receipts`/`failure-notice`，不复述。
- **4.3**：job 名 `symptom-anchors` → **`event-semantics-symptoms`**；命令去掉 `-race`，保留 `-json`，`-timeout 600s`（job 预算 20m）。
- **4.4**：由 P-05 裁删（本变更无 `task-graph.json`，plan 门无对象）。域叶数 4→3。
- **写集**：4.2 的 A5/A7 实际改了 `tests/integration/execution/progress_notification_test.go` 与 `tests/integration/acceptance/scenario_s15_test.go`（P-04 批准的冲突锚修订），并同步 `scripts/test_check_runtime_workflow.py`。
- **残余**：P-08 主规格 +28 行撞满 `scripts/doc_budget.json` 基线（基线不在本单白名单，处置选项见 tasks §5.1）；P-09 v1 金样 S03/S11 在 HEAD 即漂移（既有，待 F2 前裁定）。
