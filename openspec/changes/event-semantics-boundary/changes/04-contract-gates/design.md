# Design — 04-contract-gates（契约与门禁收口）

## D0 结论

本域的价值不在"再写三条场景"，而在**三件容易做错的事上做对**：

1. **把"改契约"和"改证据"分开**：`golden/runtime/event_integrity_scenarios.json` 被一级 F1 冻结（零 diff），而它唯一的 progress 子场景恰好是 **fact 层**断言，所以"核对"的诚实结论是**保留**；真正的假话在 **v1 金样 S15** 与两条 acceptance/execution 测试里。混淆这两个文件会让 4.2 要么违反 F1，要么漏改 S15。
2. **不给症状锚造第二套合同引擎**：三条 smoke 测试在独立 module、需要 `-tags smoke`、且不在归档 `task-graph.json` 的 `test_registry` 里——把它登记进 v2 合同会同时撞上四道硬约束（D3）。正解是**一条独立 CI job**，它和 event 三 job 共享"无 skip hatch"纪律，但不共享合同。
3. **给 profile 留证据而不是留口号**：`check_runtime_plan.py` 的六域常量写死在七处，参数化时要保证六域报错文案**逐字不变**——那句 `"is not one of the six event domains"` 既是既有测试的断言对象，也是"六域规则没被放宽"的唯一机读证据。

## D1 4.1 三场景的归属与全文替换纪律

OpenSpec 的 `MODIFIED Requirement` 是**全文替换**：delta 里必须复述该 requirement 的**全部**既有正文段与**全部**既有 Scenario，否则落地时会把未复述的部分删掉。本域 delta 已按此写好（两条 requirement，分别 6 段/8 个 Scenario 与 3 段/6 个 Scenario），4.1 的实施动作只有一条：**逐字替换主规格中对应两条 requirement 的整块文本**。

| 归属 | 追加内容 | 为什么落在这条 requirement |
|---|---|---|
| task_settled 作为一等事件触发回收 turn | 第 6 段（三族分类 + 受理最小确认 + progress 只落 fact + MUST NOT 增加模型请求消息 + MUST NOT 丢弃）与 2 个 Scenario（受理回执不构成模型材料 / progress 只落 fact 不进 intake） | 该 requirement 已经定义了"事件如何驱动下一步 decision 与 intake 待办"，"哪些事件不得成为模型材料"是它的边界条件，不是新语义 |
| 模型入口错误以失败极性呈现 | 第 3 段（失败极性达至用户面、恰一条、原始错误、MUST NOT 经模型改写、fact + 日志留痕、成功回合无失败话术）与 1 个 Scenario（失败回合必达用户告知） | 该 requirement 已经定义"失败必须可见（事件流与日志）"；02 域把可见性推到用户面，是同一条义务的延伸 |

**不新增 requirement** 是硬约束：新增一条「静默语义」requirement 会让 `quiet-receipts` 与主规格出现两份同义条款（一级 D1 的"单一事实源"被破坏），所以两个追加点都必须挂在既有 requirement 上。三族的完整语义引用 `quiet-receipts`，失败告知的触发条件引用 `failure-notice`，主规格只固定**循环面**的呈现边界。

## D2 4.2 progress-intake 锚清点表（九条，逐条给决定）

判定口径：**只有"断言 progress 进入模型材料"的锚会与 01.3 冲突**；断言"事实已落、Seq 有序、幂等"的锚一律保留。

| # | 锚（文件:行） | 现状断言 | 01.3 之后 | 决定 | 执行者 |
|---|---|---|---|---|---|
| A1 | `golden/runtime/event_integrity_scenarios.json` → `01.1/TestEventIntegrityNoticeIdentity::multiple_progress_ordered_by_seq` | fact 层：`runNoticeProgressOrder`（`tests/integration/execution/event_integrity_test.go:517`）只断 `Seq` 稳定有序，并在 :527 记录"progress notifications unreachable" | 不变（01.3 不动 `commitNotice` 的事实/Seq/幂等） | **保留，零改动**（F1 由此可满足） | 无（本域只记录理由） |
| A2 | `golden/runtime/scenarios.json:475` S15 `title` | "…＋progress 回调抵达下一次决策且不答调用" | 标题成假话 | **修订**为"…＋progress 只落 fact、不进下一次决策且不答调用" | 4.2 |
| A3 | 同文件 `:484-487` `anchor_tests` 第三项 `tests/integration/execution:TestProgressNotificationReachesTheAgent` | 指向将被改名的父测试 | 悬空引用（`anchor_tests` 不被 checker 校验，属文档债） | **修订**为 `TestProgressNotificationStaysOutOfTheDecision`（与 A5 同批，避免中间态出现悬空名） | 4.2 |
| A4 | 同文件 `:495` `progress_reaches_decision: true` | 期望 progress 抵达决策 | 实测 false | **修订为 `false`**；键名保留——问句是"是否抵达"，答 false 语义精确，且不动契约键集 | 4.2 |
| A5 | `tests/integration/execution/progress_notification_test.go:22,25` | 父测试 `TestProgressNotificationReachesTheAgent`，子测试 "…enters the decision…"，在模型请求里找 progress 正文 | **必红**（CI `go test ./... -short` 覆盖，无 `testing.Short` 守卫） | **修订**：父测试改名 `TestProgressNotificationStaysOutOfTheDecision`；子测试改 `progress_lands_as_a_fact_without_entering_the_decision`（下划线形态便于 `-run` 精确选中）；断言翻转为"下一请求不含 progress 文本 + 事实已按 `NoticeFactKey` 落链 + 不答调用" | 4.2 |
| A6 | 同文件 `:50` "a notification that is not progress carries nothing into the decision" | 非 progress 通知不得改变 decision | 不变 | **保留**（反向边界哨兵：静默不得外溢到终态） | 无 |
| A7 | `tests/integration/acceptance/scenario_s15_test.go:52-53`（注释）与 `:103-107`（`carried` 计算） | 注释声明"reaches"；`carried` 只被记录，不断言 | 记录值恒 false | **修订**：注释改为新语义；补 `if carried { t.Errorf(...) }` 见证断言，把 S15 从"记录者"变"见证者"（不改投递路径、不重复 01.3 的集成断言） | 4.2 |
| A8 | `runtime/agent/loop_test.go:710` `TestBackflowNotificationLanes` progress 子测试 | 断 progress 进下一 decision | 必红 | **不在本域写集**：01.3 已反向改写为 `progress_commits_fact_without_model_material` / `unaddressed_progress_commits_nothing`；本域只核对其绿 | 01.3（4.2 核对） |
| A9 | `openspec/specs/persistent-event-loop/spec.md:433`「多 progress 稳定 seq 有序」 | fact 层 | 不变 | **保留**（4.1 的全文替换只针对另外两条 requirement，不触碰 `Handoff 未来通知的目标、实例与稳定序号`） | 无 |

**决策规则（供实施期遇到表中未列锚时使用）**：① 断言"事实存在/有序/幂等/不丢"→ 保留；② 断言"正文进入模型请求"或"进入 intake 呈现面"→ 修订并记录原因；③ 断言"通知驱动下一 decision"→ 先分辨驱动者是**终态**还是 **progress**：终态保留，progress 修订；④ 拿不准 → 停下来上报，不得"顺手改绿"。

## D3 4.3 三种形态与被否理由

| 形态 | 做法 | 判定 |
|---|---|---|
| A | 把三测试登记进 `golden/runtime/event_integrity_scenarios.json`，新增 `symptom` role | **否**，四道硬阻塞（下） |
| B | 独立 CI job `symptom-anchors`（无合同、无 scope 字段） | **选它** |
| C | 新建 `golden/runtime/symptom_scenarios.json` + 扩 checker 的 roles/scopes + 扩 runner 的 `-tags` | **否**（过度设计，下） |

**A 的四道硬阻塞（全部实测）**：

1. `EVENT_ROLES = ("positive","negative","consumer","control")`（`scripts/check_runtime_evidence.py:80`）是封闭枚举，`roles` 里出现 `symptom` 直接报错；加值就要改校验器，而校验器是三条 event job 的唯一判定引擎。
2. 合同与归档 graph **双向严格互校**（`:1638-1643`）：`graph test missing from event contract` 与 `event contract test is not present in task-graph test_registry` 都会红。三条 smoke 测试不在归档 graph 的 `test_registry` 里，登记即红；把它们补进归档 graph 等于改历史证据（禁止）。
3. 合同条目的 `EVENT_REQUIRED_TEST_FIELDS` 含 `leaf`/`selector`/`scope`/`file`，全部要与归档 graph 的叶与 registry 对上；`run_runtime_scenarios.py` 虽已支持按 `tests[].tags` 分组传 `-tags`（`:665-666`），但 `index_event_tests` 硬等 `contract["profile"] == "event-integrity"`（`:478`）——**tags 不是障碍，graph 归属才是**。
4. 与 F1（`golden/runtime/event_integrity_scenarios.json` 零 diff）**直接冲突**。

**C 被否**：三条测试是**单发症状见证**，与 matrix/seed/window/repeat 全无关；为它们造第二套 v2 合同 + 新 role + 新 scope + 新 profile 分支，成本是 checker/runner/lint 三处新分支与一个新冻结文件，收益只是"名字进了一个 json"。

**B 的 job 形状**（与 event 三 job 同纪律：无 paths filter、无 job 级 `if`、无 `continue-on-error`）：

```yaml
  symptom-anchors:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      # 与 test job 同因：exec/async 用例在缺 tmux 时 skip，症状锚不能落在 skip 上。
      - name: Install tmux
        run: sudo apt-get update && sudo apt-get install -y tmux
      - name: Symptom anchors (three user-voice tests)
        run: |
          E="$RUNNER_TEMP/symptom"
          mkdir -p "$E"
          go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' \
            -count=1 -race -json -timeout 30m > "$E/symptom.json" 2>&1
      - name: Upload symptom evidence
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: symptom-anchors
          path: ${{ runner.temp }}/symptom
          if-no-files-found: error
```

- `-run '^TestLocalSmokeSymptom'` 是**前缀**而非三个全名：00 域自检已确认"不存在 `TestLocalSmokeSymptom` 之外的前缀"，因此它精确选中三条、又不会因为将来症状锚增加而漏跑。
- job 名即 required check 名：GitHub 允许把 job 直接列进 branch protection。**外部动作**：仓库设置里把 `symptom-anchors` 加入必需检查——这一步在本仓里不可验证，4.3 只登记"仓库侧的 job 已无条件存在"，不得宣称"已是必需检查"。
- shape 守卫进 `scripts/test_check_runtime_workflow.py`（它解析真实 `ci.yml`）：`test_symptom_anchor_job_has_no_skip_hatch` 断言 job 存在、无 job 级 `if`、无 `paths`、无 `continue-on-error`、命令含 `-tags smoke` 与前缀 `-run`、`timeout-minutes <= 30`。这是**负锚**——删掉 job 或加回 skip hatch 都会红。

## D4 4.4 五域 profile 的机制与真实对象缺位

**机制**：profile 从"一个常量"变成"一张注册表"，域元组与文案随 profile 下传。

```python
_PROFILES = {
    EVENT_PROFILE:      {"domains": EVENT_DOMAINS,      "word": "six",  "label": "event"},
    SEMANTICS_PROFILE:  {"domains": SEMANTICS_DOMAINS,  "word": "five", "label": "semantics"},
}
SEMANTICS_DOMAINS = ("00-symptom-anchors", "01-silent-receipts", "02-failure-delivery",
                     "03-tool-schema", "04-contract-gates")
```

**参数化清单（七处硬编码 → 全部改为传入）**：`check_event_graph` 的 profile 校验与域归属（`:758`、`:777`，含报错文案的 `word`/`label`）、域齐全与未知域检查（`:842`、`:845`）、`extra["leaves_by_domain"]` 的键集（`:873`）、`check_event_checkboxes` 扫描的 `changes/<domain>/tasks.md`（`:932`）、`run_event_profile` 的 `check_artifacts`/`check_specs` 域列表与 note 文案（`:1019`、`:1022`、`:1024`）、`run()` 的 dispatch（`:1064-1066`）。函数名 `run_event_profile` → `run_domain_profile`（它现在服务两个 profile，旧名会变成谎）。

**文案保真**：`"{} domain {!r} is not one of the {} {} domains"` 里 event profile 传 `("six","event")`，因此既有断言串 `"is not one of the six event domains"` **逐字不变**；semantics profile 得 `"is not one of the five semantics domains"`。这是"六域不放松"的机读证明。

**fixture 策略**：把 `EventProfileFixture` 的域元组与叶清单参数化为 `DomainProfileFixture(profile, domains, leaves)`；六域实例保持 38 叶、76 个 phase 节点与既有断言**零改动**（否则负锚失去意义），五域实例每域一叶（`00.1/01.1/02.1/03.1/04.1`）、每叶 ready/verified 两节点、`counts.leaves_by_domain` 全 1。五域实例的叶数**故意取最小**：profile 规则本来就是"叶数从图读"，钉死真实叶数会把 4.4 变成 02 域的镜像依赖。

**真实对象缺位（如实登记）**：`ls openspec/changes/*/task-graph.json` → 无；`scripts/lint.sh:102-112` 的 `active_graphs` 为空，`check_runtime_plan.py --change-dir` 对本变更**根本不跑**。因此 4.4 的消费者锚只能到"fixture 驱动的 checker 进程级调用"，真实图校验要等一个按五域形状产出的 `task-graph.json`。**待裁 P-05**：是否为本案补图（建议不补：F1 冻结事件合同、F2 门禁不依赖它、补图要另写叶状态与证据口径，属新工作量）。

## D5 数字溯源（四个数字，全部可复跑）

```bash
python3 -c "import json;d=json.load(open('golden/runtime/event_integrity_scenarios.json'));print(len(d['tests']),sum(len(t['subtests']) for t in d['tests']))"
# 29 143
grep -c '^#### Scenario:' openspec/specs/persistent-event-loop/spec.md   # 69
grep -c '^- \[ \] ' openspec/changes/event-semantics-boundary/changes/0*/tasks.md
```

| 数字 | 来源 | 实测 | 处置 |
|---|---|---|---|
| 合同父测试 / 子场景 | `notes[0]` 写"137 + 4 = 141" | **29 / 143**，全部 `mandatory: true`；roles 引用计 48 positive + 66 negative + 18 consumer + 11 dual = 143 | **不改合同**（F1）。差额 2 与 `notes[5]` 登记的两条补差锚（`01.2/handover_before_confirmation`、`01.3/notice_input_without_projection`）一致——`notes[0]` 未随补差登记更新。4.2 在叶下记录正确数字与复跑命令；是否允许 notes-only 修订见 **P-06** |
| 主规格 scenario 总数 | 本域落地前 | 69（落地后应为 72） | 4.1 的正锚 |
| 五域叶数 | 一级 `tasks.md`「粒度统计」写"16 叶" | 可见 checkbox **15**：00(3)+01(3)+02(3)+03(2)+04(4) | 以实际 checkbox 为准，**不把 16 写进任何 fixture**；差额见 **P-07** |
| 六域叶数 | 归档 event graph | 38（`test_domain_profile_accepts_six_domains_and_38_leaves`） | 六域 fixture 保持 38，负锚逐字不变 |

## D6 风险与回退

| 风险 | 影响 | 缓解 / 回退 |
|---|---|---|
| 4.1 全文替换时漏复述既有段落 | 主规格丢条款 | delta 已逐字复述并经脚本校验；落地后跑 `grep -c '^#### Scenario:'`（69→72）与 `bash scripts/check-openspec.sh`（退出 0，基线 127 项通过） |
| 4.2 误改 event_integrity 合同 | 破 F1、破归档互校 | 写集显式排除该文件；4.2 验收含 `git status --porcelain golden/runtime/event_integrity_scenarios.json` 必须 0 行 |
| 4.2 改名后遗留悬空引用 | S15 `anchor_tests` 指向不存在的测试 | A3 与 A5 同批改；改前 `grep -rn 'TestProgressNotificationReachesTheAgent'` 必须只剩 S15 与测试自身 |
| 4.3 新 job 被后续"优化"加 skip hatch | 症状锚重新变成可静默删除 | shape 守卫是最小防火墙；另在 job 上方留一行注释说明"无 paths filter 是刻意的" |
| 4.3 在 CI 上是 smoke 首跑，可能有台架竞态 | job flake 被当回归 | 沿用 `provider.count`/outbound 轮询（00 域 D6），不用 `time.Sleep` 定点；若首跑 flake，先连跑 3 次归因，不得直接放宽断言 |
| 4.4 改坏六域报错文案 | 既有六域测试连锁红 | 文案由 profile 元组生成，event 侧传 `("six","event")`；负锚就是既有测试逐字断言 |
| 4.4 五域 fixture 钉死真实叶数 | 与 02 域进度耦合 | 五域 fixture 每域一叶，叶数从图读（D4） |

## D7 本域不做（边界）

- 不实现任何产品行为（01/02 域）；不改 `loop.go`/`host.go`/`wechat.go`/`tool/exec`。
- 不改 `golden/runtime/event_integrity_scenarios.json`（F1）、不改归档 `task-graph.json`、不改 S01–S14。
- 不新增 `EVENT_ROLES`/`EVENT_SCOPES` 的取值，不新建 v2 合同、不改 `run_runtime_scenarios.py`。
- 不为本案新建 `task-graph.json`、不写 `coverage.md`、不宣称 `check_runtime_plan.py` 在本变更上跑过。
- 不改 00 域三条症状测试的测试体与断言面（那是验收面冻结区）。

## D8 实施期裁定回写（D6，2026-10-25）

本节记录实施时对前文的覆盖，读取时以本节为准；逐条证据在 `tasks.md` 的「九锚裁决」与「实施回写」。

1. **D1 被一级 D8-5 取代**：4.1 由「MODIFIED 两条既有 requirement、不新增 requirement」改为 **`## ADDED Requirements` 一条新 requirement「循环面的模型感知边界」+ 三场景**。改动动因：挂在既有两条 requirement 下会让主规格在两处各写半份同义条款；ADDED 一条边界 requirement 才让「循环面只看边界、语义看 `quiet-receipts`/`failure-notice`」的单一事实源成立。落地后主规格 69→72 scenario，新 requirement 恰 1 处。
2. **D3 的 job 名与命令以派发单为准**：`symptom-anchors` → **`event-semantics-symptoms`**；命令 `-race -json -timeout 30m` → `-count=1 -json -timeout 600s`（job 预算 20m）。理由与实测见 `tasks.md` §2。
3. **4.4 已由 P-05 裁删**：本变更无 `task-graph.json`，plan 门无判定对象；本域因此只有 3 叶，D4/D5 中 4.4 相关条目仅作历史记录。
4. **D5 的数字**：主规格 69→72（兑现）；合同 29/143（未动，F1）；v1 金样 S03/S11 在 HEAD `9cc4b20` 即漂移，属既有过期、不在本域写集（新登记 P-09）。
5. **新增残余 P-08**：主规格 +28 行撞满 `scripts/doc_budget.json` 基线（余量 0），`scripts/lint.sh` 在此门红；基线不在本单白名单，处置选项见 `tasks.md` §5.1。
6. **写集**：除本域声明的文件外，实际还改了 4.2 A5/A7 点名的两条测试（`tests/integration/execution/progress_notification_test.go`、`tests/integration/acceptance/scenario_s15_test.go`）与 `scripts/test_check_runtime_workflow.py`；均在派发单/P-04 的授权面内，未触碰产品代码。
