# Tasks — 02-event-consumers（业务消费者）

> **状态：恰 7 个叶 checkbox（02.1—02.7）；02.1、02.2、02.4—02.7 已勾选，02.3 未勾选（G4 已裁定：折叠=投影形式+WAL 重折能力，验收面已改写为 `rebuild_refolds_with_from_wal`，待实施验证）。** 下列实施项只有三锚、十轮独立证据与正式证据回验齐全后才可勾选。
>
> **派发单位**：孙任务级；域只作边界与账目单位。本节只保留 7 个叶 checkbox，叶内细节一律为普通子步骤。
>
> **前置**：00.3（契约冻结 + 缺陷分派）。依赖图、写集与未决门见 [design.md](design.md)；能力义务见 [specs/](specs/)。
>
> **无环口径**：01.5 的输入是七叶（02.1—02.5、02.7 根接线产物 + 02.6 经 01.6 回验）的**产物 ready**（实现、端口与域内证据可被消费），不是七叶 checkbox；02.1—02.5、02.7 的 checkbox 在 01.5 集成后的**正式证据回验**通过后才可勾选，02.6 的 checkbox 在 01.6 微信宿主装配回验通过后才可勾选。需要根接线的域产物在交接时只记 `ready`，不记完成；回验失败重开对应叶。该结构没有“01.5/01.6 等 02 全部勾选、02 等 01.5/01.6 派发”的互等：02.6 → 01.6 只是 `verify_after`，01.6 只依赖 01.4、不含 02.6，方向单向无环。
>
> **02 域已有正式入口先验**：每叶实施前先复跑本域现有正式入口测试（见各叶子步骤），先验只作基线，不替代 01.5/01.6 后的正式证据回验；测试里手工补接线不算生产消费者。
>
> **证据**：每叶 10 轮**独立进程** `-count=1 -race -json`，日志分开落 `/tmp/event-consumption-integrity/<叶>/run-<i>.json`；禁止单轮 `-count=10`，禁止复用同一份日志。每轮原始退出码单列 `exits.txt`，**不得用 `echo`/`tee` 包裹 `go test` 后取管道退出码**；每轮父测试与全部具名子测试的 `run`/`pass` 由既有 checker 扩展逐项核验，**不用 `grep -c` 计数**。原始日志保留在仓库外；命令、退出码、结论与 `log_ref` 摘要由编排者统一回填根 `ORCHESTRATION.md` 台账，不新增每叶摘要文件。
>
> **写集口径**：各叶「实施期写集（候选）」是**实施期**白名单，须由 00.3 按证实路径裁剪；**本轮七叶的实际写集只有本目录文档**，不触任何产品文件、测试或 CI。
>
> **静态线索口径**：本文件与 design 中的静态线索一律称**待证**，不构成缺陷结论，也不声称“全仓无生产路径”的绝对结论；旧符号/旧类型不是恢复指令。

## 公共门（非 checkbox；条件不成立时受影响叶保持未勾选）

- 确认 00.3 已冻结五层对账口径（责任身份/事实引用/运输重试/逻辑消费/外部发送）与叶级缺陷分派；未冻结的叶保持未勾选。
- 按 00.3 证实路径裁剪每叶写集；越出 D4 候选白名单的产品 API、持久格式、依赖或文件先上报，不得顺手改。
- 确认测试落位：`tests/integration/` 是 test-mirror 豁免树；02.5 根审批测试落 `tests/integration/capabilities/event_integrity_test.go`；02.6 微信审批测试**扩展现有 `examples/wechat-bot/wechat_test.go`**，不新增 `wechat_approval_test.go`，不为放测试扩大 `mirrorExemptFiles`。
- 十轮证据脚本与仓库外证据目录就位；每轮原始退出码单列 `exits.txt`，不以 `&&` 串联吞掉中间失败，不以 `echo`/`tee` 包裹测试命令。
- 静态线索先取证：G1（driver 忙早退）、G5（`ErrLeaseVersionHeld` 已存在）、G7（审批过期已实现）、G8（`RecordingLosses` 已存在）均只作待证线索，不得直接定性为缺陷；无复现只补回归。
- 口径不另立：四共享会话 × 六组合（seed 1/7/42 × window 256/32768）属 03.x 矩阵；本域只用域内最小场景，不宣称矩阵覆盖。
- 发送/导出边界：发送结果未知时保留动作身份与未确认状态，**不自动重发、不伪报成功**；导出只作显式离线消费，不新增也不要求 Boot 自动导出。

## 02.1 冥想节奏与水位（`meditation-idle-gating`）

- [x] 02.1 冥想节奏与水位：正锚 `TestEventIntegrityMeditationSchedule/new_material_on_cadence`、`TestEventIntegrityMeditationSchedule/cold_start_passes_through`；负锚 `TestEventIntegrityMeditationSchedule/non_completed_terminal_defers`、`TestEventIntegrityMeditationSchedule/busy_observation_holds_watermark`、`TestEventIntegrityMeditationSchedule/own_product_no_reentry`；消费者锚 `TestEventIntegrityMeditationSchedule/formal_reflection_consumer`（`tagent.Boot` 装配 `meditation.Driver`/`meditation.NewReflection`/`Run.ReflectionInput` 的正式反思回合，material 未被记成已消化）；父 selector `go test ./tests/integration/evolution -run '^TestEventIntegrityMeditationSchedule$' -count=1 -race -json`；十轮独立原始日志与 `exits.txt` 经既有 checker 扩展逐子测试核验；01.5 后正式证据回验通过且三锚齐全后才勾选，任一负锚缺失/跳过/零匹配即保持未勾选。

- 前置：00.3（终态/水位口径）。
- 实施期写集（候选）：`evolution/meditation/reflection.go`、`evolution/meditation/driver.go`、`evolution/meditation/watermark.go`。
- 测试落位：`tests/integration/evolution/event_integrity_test.go`（拟新增，属 test-mirror 豁免树）。
- 先验（已有正式入口，实施前先复跑）：`TestHostBootWiresTheMeditationDriver`（`tagent.Boot` 装配驱动）、`TestReflectionScopeAndWatermark`（既有水位/注入面）、`TestHostBootRefusesMisplacedMeditationTools`（错误授予负向）。先验只作基线；最终消费者锚必须经公开装配入口驱动，不手工补 `Driver`/`Reflection` 接线。
- 消费者入口（符号级）：`tagent.Boot` → `meditation.NewDriver` / `meditation.NewReflection` → `Driver.ObserveCommit` → `Run.ReflectionInput().InjectReflection` → 反思回合；不引用行号。
- 父测试—子测试—验收面（子测试名与 `-run` 父 selector 逐字符一致）：

| 子测试全名 | 验收面 | 类型 |
|---|---|---|
| `TestEventIntegrityMeditationSchedule/new_material_on_cadence` | delta「让位不烧窗口」+ 既有节奏门 | 正锚 |
| `TestEventIntegrityMeditationSchedule/cold_start_passes_through` | delta「冷启动直通」（既有水位条款） | 正锚 |
| `TestEventIntegrityMeditationSchedule/non_completed_terminal_defers` | delta「未完成终态记为推迟（失败）」 | 负锚 |
| `TestEventIntegrityMeditationSchedule/busy_observation_holds_watermark` | delta「忙时观察不推进水位（并发）」 | 负锚 |
| `TestEventIntegrityMeditationSchedule/own_product_no_reentry` | 既有「冥想触发采用双闸门判定」（不新增 delta 条款） | 负锚 |
| `TestEventIntegrityMeditationSchedule/formal_reflection_consumer` | 正式装配下反思回合实际执行，material 未被记成已消化 | 消费者锚 |

- 子步骤：先写并运行全部具名子测试，记录基线；未复现缺陷前不改产品。证实缺陷时仅在写集内最小修复；无复现则只保留可证伪回归。
- 子步骤：三条负向子测试必须独立具名通过（未完成终态/忙时观察/自身产物三条错误路径各自走不通），不得以同一断言分支冒充三条。
- 子步骤：消费者锚以 `tagent.Boot` 的正式装配为准；测试内手工创建 `Driver`/`Reflection` 只算先验，不算消费者锚。
- 子步骤：若本叶产物需根接线，产物以 `ready` 交 01.5 集成，交接不勾选；01.5 后正式证据回验通过才回填本叶。
- 十轮证据（原始命令，禁止 `echo`/`tee` 包裹后假绿）：

```sh
EVID=/tmp/event-consumption-integrity/02.1
mkdir -p "$EVID"
for i in $(seq 1 10); do
  go test ./tests/integration/evolution -run '^TestEventIntegrityMeditationSchedule$' -count=1 -race -json > "$EVID/run-$i.json" 2>&1
  rc=$?
  printf 'run-%d exit=%d\n' "$i" "$rc" >> "$EVID/exits.txt"
  if [ "$rc" -ne 0 ]; then exit "$rc"; fi
done
```

- 判定：扩展后的既有 `scripts/check_runtime_evidence.py`（复用其 test-log 状态机）逐轮核验父测试与上表全部子测试各恰有一次 `run` 和终局 `pass`，拒绝 `skip`、`fail`、零匹配、重复 run、截断 JSON、错误模块与包级失败；`exits.txt` 的 10 行原始退出码逐行与 `run-i.json` 对应。不得用 `grep -c` 通过数代替。
- 勾选条件：三锚齐全 + 十轮每轮全部子测试 `pass` + 原始退出码全 0 + 01.5 后正式证据回验通过。缺一不勾。

## 02.2 反思投递（`meditation-delivery-tool`，引用 `cross-session-delivery`）

- [x] 02.2 反思投递：正锚 `TestEventIntegrityReflectionDelivery/explicit_call_reaches_target_session`、`TestEventIntegrityReflectionDelivery/same_batch_yield_is_legal_success`；负锚 `TestEventIntegrityReflectionDelivery/zero_live_target_not_success`、`TestEventIntegrityReflectionDelivery/partial_commit_failure_not_success`、`TestEventIntegrityReflectionDelivery/wrong_target_no_extra_write`、`TestEventIntegrityReflectionDelivery/empty_whitelist_fail_closed`、`TestEventIntegrityReflectionDelivery/unknown_target_named_refusal`、`TestEventIntegrityReflectionDelivery/target_not_running_named_refusal`、`TestEventIntegrityReflectionDelivery/delivery_does_not_rearm_novelty`、`TestEventIntegrityReflectionDelivery/replay_identity`；消费者锚 `TestEventIntegrityReflectionDelivery/boot_deliver_to_target_request`（`tagent.Boot` + `meditation.enabled`/`deliver_to` 装配 → 模型显式调用 `deliver_summary` → 目标会话下一次真实请求可见内容）；父 selector `go test ./tests/integration/host -run '^TestEventIntegrityReflectionDelivery$' -count=1 -race -json`；G2 replay 身份未验或任一负锚缺失/跳过/零匹配，叶保持未勾选，不以 `design-blocked` 或部分正负通过勾整叶。

- 前置：00.3（含 G2 参数/动作身份、G3 入口命名、G9 拒绝前缀的裁定）。
- 实施期写集（候选）：`evolution/meditation/deliver.go`。
- 测试落位：`tests/integration/host/event_integrity_test.go`（与 00.2/01.3/01.4/01.5/02.4 **同文件单写**；同刻仅一个写者，后写者先读回；不新增 `event_integrity_meditation_delivery_test.go`）。
- 先验（已有正式入口，实施前先复跑）：`TestDeliverSummaryLandsOnTargetChain`、`TestDeliverSummaryRefusals`、`TestMeditationDeliversNothingWithoutModelCall`、`TestSummaryStampNoNovelty`。这些测试里的手工 `meditation.NewDeliverSummaryTool` 只算先验，不算最终消费者锚。
- 消费者入口（符号级）：`tagent.Boot` → `hostDocument.toolsFor` 授予 `deliver_summary` → `meditation.NewDeliverSummaryTool` → `hostMeditationPort.CommitToSession` → `Run.CommitToSession` → 目标会话下一次真实请求；不引用行号。
- 父测试—子测试—验收面（完整名；`cross-session-delivery` 为纯引用，按下表逐条保留既有验收面）：

| 子测试全名 | 验收面 | 类型 |
|---|---|---|
| `TestEventIntegrityReflectionDelivery/explicit_call_reaches_target_session` | delta「授权目标全覆盖才报成功」+ 既有「目标正常消费投递」 | 正锚 |
| `TestEventIntegrityReflectionDelivery/zero_live_target_not_success` | delta「目标零存活会话不得报成功（失败）」 | 负锚 |
| `TestEventIntegrityReflectionDelivery/partial_commit_failure_not_success` | delta「部分会话提交失败不得报成功（失败）」 | 负锚 |
| `TestEventIntegrityReflectionDelivery/wrong_target_no_extra_write` | 既有「白名单外目标被拒」/「白名单外目标具名拒绝」+ 拒绝账本 | 负锚 |
| `TestEventIntegrityReflectionDelivery/empty_whitelist_fail_closed` | 既有「未配置白名单」/「未配置白名单 fail-closed（失败）」 | 负锚 |
| `TestEventIntegrityReflectionDelivery/unknown_target_named_refusal` | 既有「目标名不存在」具名拒绝 | 负锚 |
| `TestEventIntegrityReflectionDelivery/target_not_running_named_refusal` | 既有「目标未启动」/「目标 loop 未运行具名拒绝（失败）」+「不走 one-shot 回退」 | 负锚 |
| `TestEventIntegrityReflectionDelivery/delivery_does_not_rearm_novelty` | 既有「投递不喂肥目标新鲜度」 | 负锚 |
| `TestEventIntegrityReflectionDelivery/same_batch_yield_is_legal_success` | 既有「与用户输入同批让位」/「成功语义为进入 mailbox（并发）」 | 正锚 |
| `TestEventIntegrityReflectionDelivery/replay_identity` | delta「重放不多写」；动作身份由 00.3 冻结 | 负锚（G2 硬门） |
| `TestEventIntegrityReflectionDelivery/boot_deliver_to_target_request` | Boot 装配下工具声明与目标会话真实请求双向可见 | 消费者锚 |

- 子步骤：先写并运行可实施子测试，记录基线；四道门结论按现行入口取证。
- 子步骤：如证实缺陷，仅在写集内最小修复；不得新开注入路径或改 `CommitToSession` 签名。
- 子步骤：G2 未裁定前，`replay_identity` **不得**以内容摘要、时间窗或“同一正文”代替动作身份；该子测试未真实验证则本叶保持未勾选。G3 只回填四道门结论口径，不阻塞其余子场景；G9 未裁定前 delta 只写「既有具名拒绝文本」，不写死前缀。
- 子步骤：`cross-session-delivery` 不写 delta；上表把该能力既有场景逐条映射到子测试，不得以“纯引用”为由省略任一验收面。
- 子步骤：若本叶产物需根接线，产物以 `ready` 交 01.5 集成，交接不勾选；01.5 后正式证据回验通过才回填本叶。
- 十轮证据（原始命令，禁止 `echo`/`tee` 包裹后假绿）：

```sh
EVID=/tmp/event-consumption-integrity/02.2
mkdir -p "$EVID"
for i in $(seq 1 10); do
  go test ./tests/integration/host -run '^TestEventIntegrityReflectionDelivery$' -count=1 -race -json > "$EVID/run-$i.json" 2>&1
  rc=$?
  printf 'run-%d exit=%d\n' "$i" "$rc" >> "$EVID/exits.txt"
  if [ "$rc" -ne 0 ]; then exit "$rc"; fi
done
```

- 判定：扩展后的既有 checker 逐轮核验父测试与上表全部子测试各恰有一次 `run` 和终局 `pass`，拒绝 `skip`/`fail`/零匹配/重复 run/截断/错误模块；`exits.txt` 10 行原始退出码逐行对应。不得用 `grep -c`。
- 勾选条件：三锚齐全 + 十轮全子测试 `pass` + 原始退出码全 0 + G2 replay 身份已验证 + 01.5 后正式证据回验通过。缺一不勾；G2 未验时不得用 `design-blocked` 代替。

## 02.3 投影折叠与恢复（`event-sourced-projection`）

- [x] 02.3 投影折叠与恢复：正锚 `TestEventIntegrityProjection/small_window_fold_and_restore`、`TestEventIntegrityProjection/rebuild_refolds_from_wal`（G4 用户裁定：折叠为事件投影形式——CompactionFact 可纯内存，但 MUST 能通过重放事件 WAL 完成重新折叠：重建路径按同一规则恢复折叠形态，重建内重折属投影派生不算自激，重建后不得再触发新折叠写入）；负锚 `TestEventIntegrityProjection/unconsumed_not_lost`、`TestEventIntegrityProjection/rebuild_does_not_refold`、`TestEventIntegrityProjection/fold_product_no_self_trigger`；消费者锚 `TestEventIntegrityProjection/actual_request_assembly`（`age-fold` 插件 `ObserveCommit` → `ReplayProjection.FoldByAge` → 实际请求装配可见综述且原文可 recall）；父 selector `go test ./tests/integration/history -run '^TestEventIntegrityProjection$' -count=1 -race -json`；WAL 重折能力未验或任一负锚缺失/跳过/零匹配，叶保持未勾选，不以 `design-blocked` 或部分正负通过勾整叶。

- 前置：00.3（含 G4 compaction 写入点裁定）。
- 实施期写集（候选）：`age_fold.go`、`memory/replay/age_segments.go`、`memory/replay/replay_budget.go`、`memory/replay/fold.go`、`memory/replay/projection.go`、`memory/replay/replay_settle.go`。
- 测试落位：`tests/integration/history/event_integrity_test.go`（拟新增，与 01.2、03.9 **同文件单写**；同刻仅一个写者）。
- 先验（已有正式入口，实施前先复跑）：`TestAgeFoldFiresFromCommits`、`TestUnderThresholdUntouched`、`TestRebuildRefoldsIdempotent`、`TestProjectionReplayAndBudget`（既有逐字节重建/有界 fallback/实际请求装配套件）。先验只作基线，不替代 01.5 后正式回验。
- 消费者入口（符号级）：`Run.AttachReplay` / `AgeFoldPlugin.ObserveCommit` → `ReplayProjection.FoldByAge` → `request.projectedSegment` 实际请求装配；冷启动 `RebuildProjectionFromWAL` 只复原投影；不引用行号。
- 父测试—子测试—验收面：

| 子测试全名 | 验收面 | 类型 |
|---|---|---|
| `TestEventIntegrityProjection/small_window_fold_and_restore` | delta「重复折叠不追加第二份产物」+ 既有「冷启动回放逐字节重建投影」 | 正锚 |
| `TestEventIntegrityProjection/unconsumed_not_lost` | 既有不变量「未消费输入不被折叠吞掉」 | 负锚 |
| `TestEventIntegrityProjection/rebuild_does_not_refold` | delta「重建不触发折叠（失败）」 | 负锚 |
| `TestEventIntegrityProjection/fold_product_no_self_trigger` | delta「折叠产物不触发自身（失败）」 | 负锚 |
| `TestEventIntegrityProjection/rebuild_refolds_from_wal` | G4 用户裁定：重放事件 WAL 后按同一规则恢复折叠形态（综述/带结构与在线折叠一致）；重建后不得触发新折叠写入；不要求 CompactionFact 落事实链 | 正锚（G4 用户裁定） |
| `TestEventIntegrityProjection/actual_request_assembly` | 实际请求装配可见综述，原文经既有 `[evt_key]` 票据 recall 回补 | 消费者锚 |

- 子步骤：先写并运行可实施子测试，记录基线与折叠键集合；未复现缺陷前不改产品。
- 子步骤：如证实缺陷，仅在写集内最小修复；不得引入第二真相源或游离快照。
- 子步骤：G4 已裁定（纯内存折叠+WAL 重折能力）；实施 `rebuild_refolds_from_wal`：验证（或最小实现后验证）重放 WAL 能恢复折叠形态；重建路径零生产调用时，验证能力层面（WAL 信息充分性+同规则重折）即可，不要求新增 Boot 接线；重建内重折不算自激。
- 子步骤：三条负向子测试必须独立具名通过（未消费不丢/重建不折/产物不自激）；不得以同一断言分支冒充三条。
- 十轮证据（原始命令，禁止 `echo`/`tee` 包裹后假绿）：

```sh
EVID=/tmp/event-consumption-integrity/02.3
mkdir -p "$EVID"
for i in $(seq 1 10); do
  go test ./tests/integration/history -run '^TestEventIntegrityProjection$' -count=1 -race -json > "$EVID/run-$i.json" 2>&1
  rc=$?
  printf 'run-%d exit=%d\n' "$i" "$rc" >> "$EVID/exits.txt"
  if [ "$rc" -ne 0 ]; then exit "$rc"; fi
done
```

- 判定：扩展后的既有 checker 逐轮核验父测试与上表全部子测试各恰有一次 `run` 和终局 `pass`，拒绝 `skip`/`fail`/零匹配/重复 run/截断/错误模块；`exits.txt` 10 行原始退出码逐行对应。不得用 `grep -c`。
- 勾选条件：三锚齐全 + 十轮全子测试 `pass` + 原始退出码全 0 + G4 生产恢复已验 + 01.5 后正式证据回验通过。缺一不勾；G4 未验时不得用 `design-blocked` 代替。

## 02.4 模型替换（`config-hot-reload`）

- [x] 02.4 模型替换：正锚 `TestEventIntegrityReplacement/new_request_gets_new_version`、`TestEventIntegrityReplacement/inflight_stream_keeps_start_version`；负锚 `TestEventIntegrityReplacement/duplicate_same_version_not_reinstalled`、`TestEventIntegrityReplacement/failed_replacement_not_installed`；消费者锚 `TestEventIntegrityReplacement/formal_reload_entry`（`Host.Reload` / `POST /reload` 后请求侧可见新生效版本）；父 selector `go test ./tests/integration/host -run '^TestEventIntegrityReplacement$' -count=1 -race -json`；十轮独立原始日志与 `exits.txt` 经既有 checker 扩展逐子测试核验；01.5 后正式证据回验通过且三锚齐全后才勾选。重复替换只要求「不重复发布/安装、结果可判定」，**不要求公共 `Reload` 必须报错**。

- 前置：00.3。
- 实施期写集（候选）：`host_reload.go`、`host_model.go`、`rl/replacement.go`、`rl/lease_model.go`、`rl/lease_observer.go`。
- 测试落位：`tests/integration/host/event_integrity_test.go`（与 00.2/01.3/01.4/01.5/02.2 **同文件单写**；不新增 `event_integrity_model_replacement_test.go`）。
- 先验（已有正式入口，实施前先复跑）：`TestReloadEndpointSwapsModel`、`TestReloadEndpointRefusesStructuralChange`、`TestModelReplacementLifecycle`。先验只作基线；最终消费者锚必须经 `Host.Reload`/`POST /reload`。
- 消费者入口（符号级）：`Host.Reload` / `POST /reload` → `Run.NoteModelChange` → `rl.NewLeaseModel` / `ModelLease.Replace` → 新请求取新生效版本；`Run.LeasePublications`/`ModelLease.Replacements` 为只读证据面；不引用行号。
- 父测试—子测试—验收面：

| 子测试全名 | 验收面 | 类型 |
|---|---|---|
| `TestEventIntegrityReplacement/new_request_gets_new_version` | delta「替换后新请求取新版本」 | 正锚 |
| `TestEventIntegrityReplacement/inflight_stream_keeps_start_version` | delta「在途流保持开始版本」 | 正锚 |
| `TestEventIntegrityReplacement/duplicate_same_version_not_reinstalled` | delta「同版本重复替换不重复发布/安装（失败）」：不二次重建/安装/发布，generation 不变，结果可判定（no-op 或具名结果） | 负锚 |
| `TestEventIntegrityReplacement/failed_replacement_not_installed` | delta「替换失败不安装（失败）」 | 负锚 |
| `TestEventIntegrityReplacement/formal_reload_entry` | 正式 reload 入口后新请求版本与在途流版本分别可证 | 消费者锚 |

- 子步骤：先写并运行全部具名子测试，记录基线（generation/生效绑定/发布记录）。
- 子步骤：如证实缺陷，仅在写集内最小修复；不得改变失败原子性。
- 子步骤：`duplicate_same_version_not_reinstalled` 的验收面是「不重复发布/安装、结果可判定」，不得从 `rl.ErrLeaseVersionHeld` 的现状反推公共 `Reload` 必须返回错误；no-op 成功与具名拒绝都可接受，但必须可判定且无第二次发布/安装。
- 子步骤：两条负向子测试必须独立具名通过（重复不重装/失败不安装）；在途流与新请求的证据必须来自请求侧，不以替换回执自证。
- 子步骤：若本叶产物需根接线，产物以 `ready` 交 01.5 集成，交接不勾选；01.5 后正式证据回验通过才回填本叶。
- 十轮证据（原始命令，禁止 `echo`/`tee` 包裹后假绿）：

```sh
EVID=/tmp/event-consumption-integrity/02.4
mkdir -p "$EVID"
for i in $(seq 1 10); do
  go test ./tests/integration/host -run '^TestEventIntegrityReplacement$' -count=1 -race -json > "$EVID/run-$i.json" 2>&1
  rc=$?
  printf 'run-%d exit=%d\n' "$i" "$rc" >> "$EVID/exits.txt"
  if [ "$rc" -ne 0 ]; then exit "$rc"; fi
done
```

- 判定：扩展后的既有 checker 逐轮核验父测试与上表全部子测试各恰有一次 `run` 和终局 `pass`，拒绝 `skip`/`fail`/零匹配/重复 run/截断/错误模块；`exits.txt` 10 行原始退出码逐行对应。不得用 `grep -c`。
- 勾选条件：三锚齐全 + 十轮全子测试 `pass` + 原始退出码全 0 + 01.5 后正式证据回验通过。缺一不勾。

## 02.5 根审批（`approval-channels`）

- [x] 02.5 根审批：正锚 `TestEventIntegrityApproval/approved_then_executes_once`；负锚 `TestEventIntegrityApproval/unapproved_never_executes`、`TestEventIntegrityApproval/rejected_never_executes`、`TestEventIntegrityApproval/expired_response_refused`、`TestEventIntegrityApproval/changed_args_requires_new_approval`、`TestEventIntegrityApproval/duplicate_response_no_second_execution`、`TestEventIntegrityApproval/wrong_target_no_overreach`、`TestEventIntegrityApproval/channel_delivery_failure_does_not_block`；消费者锚 `TestEventIntegrityApproval/formal_governance_task_wrapper`（正式治理+任务包装下执行一次）；父 selector `go test ./tests/integration/capabilities -run '^TestEventIntegrityApproval$' -count=1 -race -json`；单目录十轮独立日志与 `exits.txt`，经既有 checker 扩展逐子测试核验；`verify_after=01.5`，根正式装配回验通过且三锚齐全后才勾选。微信审批归 02.6；根 `tagent approve <digest>` 保留为独立待验项。

- 前置：00.3（含 G7 取证口径）。
- 实施期写集（候选）：`evolution/governance/policy.go`、`runtime/task/approval/approval.go`、`runtime/task/approval/approval_store.go`。
- 测试落位：`tests/integration/capabilities/event_integrity_test.go`（拟新增，属 test-mirror 豁免树）。
- 先验（已有正式入口，实施前先复跑）：`TestGovernanceBeforeExecution`。先验只作基线；最终消费者锚必须经正式治理+任务包装。
- 消费者入口（符号级）：治理策略 → `runtime/task/wrapper.go`（P1，禁改）→ 执行；不引用行号。
- 父测试—子测试—验收面（root）：

| 子测试全名 | 验收面 | 类型 |
|---|---|---|
| `TestEventIntegrityApproval/approved_then_executes_once` | delta「批准后只执行一次（并发）」+ 既有「状态机流转与幂等」 | 正锚 |
| `TestEventIntegrityApproval/unapproved_never_executes` | 既有「Hold 记录为等待而非拒绝」 | 负锚 |
| `TestEventIntegrityApproval/rejected_never_executes` | 既有「Denied 产生原生配对具名结果（失败）」 | 负锚 |
| `TestEventIntegrityApproval/expired_response_refused` | delta「过期回应不得执行（失败）」 | 负锚 |
| `TestEventIntegrityApproval/changed_args_requires_new_approval` | delta「改参不得沿用旧批准（失败）」 | 负锚 |
| `TestEventIntegrityApproval/duplicate_response_no_second_execution` | 既有状态机幂等 | 负锚 |
| `TestEventIntegrityApproval/wrong_target_no_overreach` | 既有 Hold/Denied 边界 | 负锚 |
| `TestEventIntegrityApproval/channel_delivery_failure_does_not_block` | 既有「通道投递失败不阻塞门（失败）」：通道投递失败后审批门仍可经其它入口回应 | 负锚 |
| `TestEventIntegrityApproval/formal_governance_task_wrapper` | 正式治理 + 任务包装下执行一次 | 消费者锚 |

- 子步骤：先写并运行可实施子测试，记录基线；过期/改参结论以实测为准。
- 子步骤：如证实缺陷，仅在写集内最小修复；不改 `runtime/task/wrapper.go`。
- 子步骤：七条负向子测试必须独立具名通过（未批/拒绝/过期/改参/重复/错目标/通道投递失败）；不得以部分正负通过勾整叶。
- 范围边界：主规格 `approval-channels` 的「CLI 批准入口」指根 `tagent approve <digest>`，是既有独立要求；本叶不新增或改写该命令。微信宿主 `runApprovalCLI` 归 02.6，不得冒充根 CLI。
- 子步骤：本叶产物需根接线，产物以 `ready` 交 01.5 集成，交接不勾选；01.5 后正式证据回验通过才回填本叶。
- 十轮证据（原始命令，禁止 `echo`/`tee` 包裹后假绿）：

```sh
EVID=/tmp/event-consumption-integrity/02.5
mkdir -p "$EVID"
for i in $(seq 1 10); do
  go test ./tests/integration/capabilities -run '^TestEventIntegrityApproval$' -count=1 -race -json > "$EVID/run-$i.json" 2>&1
  rc=$?
  printf 'run-%d exit=%d\n' "$i" "$rc" >> "$EVID/exits.txt"
  if [ "$rc" -ne 0 ]; then exit "$rc"; fi
done
```

- 判定：扩展后的既有 checker 逐轮核验父测试与上表全部子测试各恰有一次 `run` 和终局 `pass`，拒绝 `skip`/`fail`/零匹配/重复 run/截断/错误模块；`exits.txt` 10 行原始退出码逐行对应。不得用 `grep -c`。
- 勾选条件：三锚齐全 + 十轮全子测试 `pass` + 原始退出码全 0 + 01.5 后正式证据回验通过。缺一不勾。

## 02.6 微信审批（`approval-channels`）

- [x] 02.6 微信审批：正锚 `TestEventIntegrityApprovalWeChat/parse_approve_reject`、`TestEventIntegrityApprovalWeChat/approval_request_fanout`；负锚 `TestEventIntegrityApprovalWeChat/ambiguous_prefix_refused`、`TestEventIntegrityApprovalWeChat/unknown_digest_refused`、`TestEventIntegrityApprovalWeChat/non_approver_refused`；消费者锚 `TestEventIntegrityApprovalWeChat/wechat_cli_entry`（`runApprovalCLI` 写回应文件）；父 selector `go -C examples/wechat-bot test -run '^TestEventIntegrityApprovalWeChat$' -count=1 -race -json`；单目录十轮独立日志与 `exits.txt`，经既有 checker 扩展逐子测试核验；`verify_after=01.6`，微信宿主装配正式证据回验通过且三锚齐全后才勾选。端到端微信往返归 01.6/03.4 复验；根 `tagent approve <digest>` 仍为独立待验项。

- 前置：00.3（含 G7 取证口径）。
- 实施期写集（候选）：`examples/wechat-bot/wechat_approval.go`。
- 测试落位：**扩展现有 `examples/wechat-bot/wechat_test.go`**（新增父测试 `TestEventIntegrityApprovalWeChat`，不新增 `wechat_approval_test.go`，不为放测试扩大 `mirrorExemptFiles`）。
- 先验（已有正式入口，实施前先复跑）：`TestApprovalGateway`、`TestApprovalRuling`。先验只作基线；最终消费者锚必须经 `runApprovalCLI`。
- 消费者入口（符号级）：`runApprovalCLI` 与 `approvalGateway.Resolve` → `ApprovalManager`；不引用行号。
- 父测试—子测试—验收面（wechat，扩展 `wechat_test.go`）：

| 子测试全名 | 验收面 | 类型 |
|---|---|---|
| `TestEventIntegrityApprovalWeChat/parse_approve_reject` | 既有微信回应解析/批准/拒绝 | 正锚 |
| `TestEventIntegrityApprovalWeChat/approval_request_fanout` | 既有「审批请求送达」：pending 审批广播到每个已配置审批人 | 正锚 |
| `TestEventIntegrityApprovalWeChat/ambiguous_prefix_refused` | 既有 `match` 前缀歧义具名拒绝 | 负锚 |
| `TestEventIntegrityApprovalWeChat/unknown_digest_refused` | 既有未知 digest 拒绝 | 负锚 |
| `TestEventIntegrityApprovalWeChat/non_approver_refused` | 既有审批人白名单：非审批人回应被具名拒绝且不改状态 | 负锚 |
| `TestEventIntegrityApprovalWeChat/wechat_cli_entry` | `runApprovalCLI` 写回应文件；端到端微信往返归 01.6/03.4 复验 | 消费者锚 |

- 子步骤：先写并运行可实施子测试，记录基线。
- 子步骤：如证实缺陷，仅在写集内最小修复；不改根审批面。
- 子步骤：三条负向子测试必须独立具名通过（歧义前缀/未知 digest/非审批人）；不得以部分正负通过勾整叶。
- 子步骤：微信审批测试只扩展既有 `wechat_test.go`；不为新测试文件放行 `mirrorExemptFiles`，也不新增 `wechat_approval_test.go`。
- 子步骤：本叶 `verify_after=01.6`；01.6 微信宿主装配回验通过才回填本叶，01.6 不依赖本叶，方向单向无环。
- 十轮证据（原始命令，禁止 `echo`/`tee` 包裹后假绿）：

```sh
EVID=/tmp/event-consumption-integrity/02.6
mkdir -p "$EVID"
for i in $(seq 1 10); do
  go -C examples/wechat-bot test -run '^TestEventIntegrityApprovalWeChat$' -count=1 -race -json > "$EVID/run-$i.json" 2>&1
  rc=$?
  printf 'run-%d exit=%d\n' "$i" "$rc" >> "$EVID/exits.txt"
  if [ "$rc" -ne 0 ]; then exit "$rc"; fi
done
```

- 判定：扩展后的既有 checker 逐轮核验父测试与上表全部子测试各恰有一次 `run` 和终局 `pass`，拒绝 `skip`/`fail`/零匹配/重复 run/截断/错误模块；`exits.txt` 10 行原始退出码逐行对应。不得用 `grep -c`。
- 勾选条件：三锚齐全 + 十轮全子测试 `pass` + 原始退出码全 0 + 01.6 后正式证据回验通过。缺一不勾。

## 02.7 决策采集与离线导出（`decision-capture`，引用 `offline-training-export`）

- [x] 02.7 决策采集与离线导出：正锚 `TestEventIntegrityCapture/requests_and_attempts_match_records`；负锚 `TestEventIntegrityCapture/failed_attempt_recorded_as_failure`、`TestEventIntegrityCapture/rejected_admission_in_loss_ledger`、`TestEventIntegrityCapture/no_request_no_record`、`TestEventIntegrityCapture/export_correlation_is_not_reward`；消费者锚 `TestEventIntegrityCapture/formal_capture_then_offline_export`（`tagent.Boot` 产生实际 capture → 公开 `rl.NewChainExporter`/`ChainExporter.Export` 读取有权限的 `ExportOptions.SessionIDs` → capture manifest/attempt 记录与导出样本逐项关联；不新增 CLI、不自动接 Boot 导出）；父 selector `go test ./tests/integration/learning -run '^TestEventIntegrityCapture$' -count=1 -race -json`；G6 导出关联未验或任一负锚缺失/跳过/零匹配，叶保持未勾选，不以 `design-blocked` 或部分正负通过勾整叶。

- 前置：00.3（含 G6 导出消费者裁定、G8 取证口径）。
- 实施期写集（候选）：`host_capture.go`、`rl/capture_wiring.go`、`rl/sink.go`、`rl/produce.go`、`rl/export_chain.go`、`tool/subagent/capture.go`、`tool/subagent/builder.go`。
- 测试落位：`tests/integration/learning/event_integrity_test.go`（拟新增）。
- 先验（已有正式入口，实施前先复跑）：`TestBootMountsCapture`（`tagent.Boot` 实际 capture）、`TestChainExporterProducesSamples`（公开 `rl.NewChainExporter` 显式导出）、`TestCaptureAdmissionAndSeal`、`TestCaptureFidelityAndRedaction`。先验只作基线；`TestChainExporterProducesSamples` 使用 `tagent.Assemble` 的路径不等于 Boot 采集消费者锚。
- 消费者入口（符号级）：`tagent.Boot` → `openHostCapture`/`hostRunCapture.Record` → `Run.SessionFacts` → `rl.NewChainExporter`/`ChainExporter.Export`；`rl.ReadCaptureSeal` 读 manifest；`tool/subagent.Builder.Capture` 只作子代理采集候选面；不引用行号。
- 父测试—子测试—验收面：

| 子测试全名 | 验收面 | 类型 |
|---|---|---|
| `TestEventIntegrityCapture/requests_and_attempts_match_records` | delta「真实请求逐项有记录」 | 正锚 |
| `TestEventIntegrityCapture/failed_attempt_recorded_as_failure` | delta「失败尝试如实记录（失败）」 | 负锚 |
| `TestEventIntegrityCapture/rejected_admission_in_loss_ledger` | delta「准入被拒进入损失账目（失败）」 | 负锚 |
| `TestEventIntegrityCapture/no_request_no_record` | delta「未发生请求不产生记录（失败）」 | 负锚 |
| `TestEventIntegrityCapture/export_correlation_is_not_reward` | 纯引用「精确关联不等同自动奖励」：导出只读关联，不因关联自动生成 reward | 负锚 |
| `TestEventIntegrityCapture/formal_capture_then_offline_export` | Boot 实际 capture + 公开 export API 读取授权数据 + manifest/attempt/sample 逐项关联 | 消费者锚（G6 硬门） |

- 子步骤：先写并运行可实施子测试，记录基线（请求数/记录数/损失数）。
- 子步骤：如证实缺陷，仅在写集内最小修复；不新建第二账本。
- 子步骤：G6 的静态线索只是「当前静态检索未见 `rl.NewChainExporter` 的生产调用点（待证）」；**零生产引用不等于必须新增 CLI**。显式离线 API 本身就是真实消费者：`tagent.Boot` 产实际 capture，公开 `Export` 读取有权限的 `ExportOptions.SessionIDs`，manifest/attempt/sample 逐项关联即可。不得为凑消费者自动接 Boot 导出或新增 CLI。
- 子步骤：G6 导出关联未验时本叶保持未勾选；不得用 `design-blocked`、测试调用或“结构存在”代替关联证据。G6 若需根接线，产物以 `ready` 交 01.5 集成，交接不勾选；01.5 后正式证据回验通过才回填本叶。
- 子步骤：`offline-training-export` 为纯引用，只验来源关联；关联不等于自动奖励，导出 MUST NOT 因 session/episode/turn 关联而生成 reward 或修改事实链。真实 tokenizer/chat template/collator 与训练资格仍是既有独立待验项，不得由本域缩水替代。静态未引用/旧符号不构成恢复指令。
- 十轮证据（原始命令，禁止 `echo`/`tee` 包裹后假绿）：

```sh
EVID=/tmp/event-consumption-integrity/02.7
mkdir -p "$EVID"
for i in $(seq 1 10); do
  go test ./tests/integration/learning -run '^TestEventIntegrityCapture$' -count=1 -race -json > "$EVID/run-$i.json" 2>&1
  rc=$?
  printf 'run-%d exit=%d\n' "$i" "$rc" >> "$EVID/exits.txt"
  if [ "$rc" -ne 0 ]; then exit "$rc"; fi
done
```

- 判定：扩展后的既有 checker 逐轮核验父测试与上表全部子测试各恰有一次 `run` 和终局 `pass`，拒绝 `skip`/`fail`/零匹配/重复 run/截断/错误模块；`exits.txt` 10 行原始退出码逐行对应。不得用 `grep -c`。
- 勾选条件：三锚齐全 + 十轮全子测试 `pass` + 原始退出码全 0 + G6 导出关联已验 + 01.5 后正式证据回验通过。缺一不勾；G6 未验时不得用 `design-blocked` 代替。

## 未决门（阻塞对应叶，不以猜测绕过）

| 门 | 静态线索（待证） | 阻塞 | 处置 |
|---|---|---|---|
| G2 | 主规格声明参数 `{target, content}`；现行 `DeliverSummaryTool.Declaration`/`Call` 只接受 `content`，目标由 `deliver_to` 装配期固定；重放动作身份来源待证 | 02.2 的 `replay_identity` 与整叶勾选 | 00.3 裁定；不得以内容摘要/时间窗代替身份；未验则叶不勾选 |
| G3 | 主规格以 `InjectMessageWithSource("meditation", …)` 命名入口；现行实现经 `Run.CommitToSession` 落目标链 | 02.2 的四道门结论口径 | 00.3 裁定；02.2 按既有场景取证，不改写条款；G3 不阻塞其余子场景 |
| G4 | `memory/replay/fold.go` 在内存构造 `CompactionFact`；当前静态检索未见 `context_compress_summary` 写入点（待证，不排除动态装配/测试外路径） | 02.3 的 `compaction_event_writer` 与整叶勾选 | 02.3 取证后交 00.3；可能需 01.5 接线，交接不记完成；未验则叶不勾选 |
| G6 | 当前静态检索未见 `rl.NewChainExporter` 的生产调用点（待证）；显式离线 API 本身是合法消费者 | 02.7 的消费者锚与整叶勾选 | 不新增 CLI、不自动接 Boot 导出；Boot capture → 公开 export API → manifest/attempt/sample 逐项关联；未验则叶不勾选 |
| G9 | 主规格用 `[delivery_denied]`；现行 `deliver.go` 用 `[deliver_summary_denied]`（待证） | 02.2 拒绝文本断言 | 00.3 裁定；delta 只写「既有具名拒绝文本」，不写死前缀 |

## 证据检查器扩展契约（实施期落地，禁止另写 grep 计数）

- 输入：每叶证据目录（10 份 `run-i.json` + `exits.txt`）、父测试名、该叶全部具名子测试名（与任务行逐字符一致）。
- 逐轮核验：父测试与每个子测试各恰有一次 `run` 与终局 `pass`；父测试 `pass` 必须晚于其全部子测试终局；拒绝 `skip`、`fail`、零匹配、重复 `run`、截断 JSON、包级失败、错误模块与跨轮复用同一日志。
- 退出码：`exits.txt` 必须有 10 行，逐行是该轮 `run-i.json` 对应的原始 `go test` 退出码；任一行非 0 即整叶失败，不以其余轮次覆盖。
- 输出：逐叶列出缺失子测试、非 pass 子测试与首个失败原文；不使用 `grep -c` 统计 `"Action":"pass"` 数量。

## 本轮文档交付（非 checkbox，只读核对）

- 已写 `proposal.md` / `design.md` / `tasks.md`。
- 已写七项能力 delta：`meditation-idle-gating`（完整 MODIFIED）、`meditation-delivery-tool`（ADDED）、`event-sourced-projection`（ADDED）、`config-hot-reload`（ADDED）、`approval-channels`（完整 MODIFIED，02.5 根 + 02.6 微信两叶共同引用）、`decision-capture`（ADDED，02.7）。
- `cross-session-delivery`、`offline-training-export` 登记为纯引用，不写 delta（理由见 proposal/design）。
- 主代理独立执行 `openspec validate` / `openspec status`（本域不自行宣称 CLI 校验通过）。
