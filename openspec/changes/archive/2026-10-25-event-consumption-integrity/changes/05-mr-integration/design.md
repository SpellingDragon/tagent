# 05 MR 集成：事件双档必跑、required 汇总与发布证据

> 本设计只描述未来实施的接线与验收取舍；`ci.yml` 作业、preflight 事件入口、required 汇总与文档更新均为**拟新增/拟扩展**，当前不存在；文中命令均未运行。

## D0 依赖图与本地/远端分层

```text
05.1 生产三作业 + preflight 默认入口
  └─→ 05.2 required 汇总 + workflow 自测
        ├─→ 05.3 文档终验（草稿可先写，完成必须等 05.2）
        └─→ 05.4 最终集成（另依赖 05.3）
```

- 05.3 的**开发前置**是 01.5、04.5；**最终绿证前置**是 05.2。05.1/05.2 落地前只能写“拟新增/未启用”的草稿，不能勾选。
- 05.1/05.2 的本地完成门不是“远端 CI 绿”，而是：与 CI **完全相同的命令**在本地真实跑通 + `CiWorkflowTest` 的结构验收 + `preflight.sh` 同一入口。远端 required 真实生效属于 L2/F4，另行授权与验证。
- 05.4 的最终集成依赖 05.2、05.3；依赖无环。

## D1 每 PR 事件作业与稳定名称

新增四个 job，名称即分支保护的稳定检查名：

| job | 触发 | 命令核心 | 产物（`runner.temp` 下） |
|---|---|---|---|
| `event-integrity-short` | 每次 `pull_request` 与 `push`（main/dev） | `run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10 --out "${{ runner.temp }}/event-short"` | `event-short/repeat-{1..10}/**` |
| `event-integrity-long` | 同上；matrix `seed [1,7,42] × window [256,32768]`，`fail-fast: false` | `run_runtime_scenarios.py --contract … --scope event-long --seed <s> --window <w> --repeat 1 --out "${{ runner.temp }}/event-long/seed-<s>-window-<w>"` | 每个分片独立目录 |
| `event-integrity-mutations` | 同上 | `check_event_integrity_mutations.py --all --json --out "${{ runner.temp }}/event-integrity-mutations"` | `event-integrity-mutations/**` |
| `event-integrity-required` | `if: always()`；`needs` 上述三者 | 见 D2 | required 报告与 bundle |

- 触发沿用 workflow 级 `pull_request` 与 `push: branches [main, dev]`；事件 job MUST NOT 加 `paths`/`paths-ignore`、MUST NOT 加 job 级 `if:`、MUST NOT 加 `continue-on-error`、MUST NOT 以 secret 缺失为跳过条件。`workflow_dispatch` 触发时四个 job 同样执行，不构成“只有手动才跑”。
- 十次 short 由 runner 发起十次独立 `go test … -count=1 -race -json`，每次独立日志、UUID run-id、`repeat_index` 与证据；`ci.yml` 中 MUST NOT 出现单次 count 放大。长程六分片各一次 `-count=1 -race -json`；`--repeat` 保持为 1，不用 count 放大总量。
- **单 long 分片口径是 1200 输入/120 重启**（4 会话 × 300 输入；每会话 30 个重启触发点）；六个分片合计 **7200 输入/720 重启**。任一分片 MUST NOT 被写成 7200/720，MUST NOT 用一次重启按四会话计数。
- 显式模块与 tags 由合同声明，job 不写死模块路径；`examples/wechat-bot` 的 smoke 场景用合同里的 `tags: ["smoke"]` 与模块目录，runner 负责 `go -C` 切换。所有事件 job 安装 `tmux`（exec-tool/异步层测试依赖），`setup-go 1.24`、`setup-python 3.11`、`checkout fetch-depth: 0` 与现 `test` job 对齐。
- `timeout-minutes` 只是熔断，不是预算结论。short 20m / long 45m / mutations 30m 是附稿对整个作业的提议值；实施时用 03.10 校准的六项（冷构建、热运行、120 次重启、race、存储增长、峰值资源）最大观察值加明确余量重算并记录，未校准时标注“待校准”，不把提议值写成已测。
- 每个生产 job 的 artifact 上传用 `if: always()`；上传失败本身使 job 失败，不吞。artifact 名固定，供 required 按名下载与缺项判定；上传路径一律 `${{ runner.temp }}/…`，MUST NOT 依赖仓库内未核验目录（其忽略状态未核验，MUST NOT 宣称为已忽略）。
- 既有 `test`/`race`/`validators`/`openspec`/`soak` 全部保留。**`required` 只是新增事件三档的汇总检查，不是全库唯一必跑，不替代旧检查**；分支保护可同时保留旧检查名，旧检查不得被删除或降级。
- `race` job 的包集合由**全部修改的产品文件与全部正式测试包**映射，至少包含 `./tests/integration/execution/...`、`./tests/integration/history/...`、`./tests/integration/host/...`、`./tests/integration/capabilities/...`、`./tests/integration/evolution/...`、`./tests/integration/learning/...`，以及被 00—03 改动的根包（含 `./runtime/agent/...`、`./runtime/task/...`、`./runtime/state/...`、`./memory/...`、`./evolution/...`、`./rl/...`、`./tool/...`、`./api/event/...`、`./api/plugin/...`、`.`）；不得只补 host/history。`scripts/race_check.sh` 的判据与 `-p 1` 纪律不改；其上游 waiver 只对非 tagent frame 生效，**不豁免新事件套件**，新测试不得以 `-skip`/build tag 逃避 race。微信/cli-host 的 race 由事件 short/long 自身的 `-race` 承担，不塞进根 race job。
- 被否方案：把 long 六组合塞进一次 `go test`（无法分片取证、超时即全红）；用单次 count 放大冒充十次独立执行；给事件 job 加路径过滤；只补 host/history 两个包就宣称 race 覆盖受影响集；把 `required` 说成“唯一必跑”并删旧检查。

## D2 required 汇总：只比对事实，不重判语义

- `event-integrity-required` 用 `if: always()`，`needs: [event-integrity-short, event-integrity-long, event-integrity-mutations]`；这是**新增事件汇总检查名**，不是全库唯一必跑，也不替代 `test`/`race`/`validators`/`openspec`。matrix 分片不单独 required。
- 第一步把上游结果经 `env` 传入 shell（`SHORT_RESULT: ${{ needs.event-integrity-short.result }}` 等），逐个 `test "$X_RESULT" = "success"`；MUST NOT 把 `${{ … }}` 直接插进带引号的 shell 参数（沿用现有 workflow 自测的注入防线）。`cancelled`、`failure`、`skipped` 均非 `success`，一律失败。
- 第二步 `actions/download-artifact@v4` 以 `pattern: event-integrity-*` 下载到 `${{ runner.temp }}/bundle`，不合并目录，保留分片来源。
- 第三步调用同一引擎的汇总模式：`python3 scripts/check_runtime_evidence.py --bundle "${{ runner.temp }}/bundle" --contract golden/runtime/event_integrity_scenarios.json --json`。该模式由 04.5 提供，只消费 runner/checker 已产出的 JSON 事实并做集合与一致性比对：
  - short 恰好 10 份独立证据，`run_id`/日志真实路径互不相同，`repeat_index` 覆盖 1..10，全部终局通过；
  - long 的 `(seed, window)` 集合恰为 `{1,7,42} × {256,32768}` 六项，每项一份证据，缺一、重复、额外均失败；每份证据的 `per_combo` 为 1200/120，汇总为 7200/720；
  - mutations 恰一份，控制树绿、每个登记变异由 `expect_assertion` 具名断言杀死、无 `not_evidence_backed`/“验证失败”项；
  - 所有证据的基线源树摘要一致（受影响第一方源码、必要未跟踪测试、合成配置/fixture、合同、各模块 `go.mod/go.sum`），运行配置摘要按 seed/window/repeat index 区分；
  - 同一执行凭据（run_id/日志真实路径/artifact）不得冒充不同独立轮次或不同 matrix 组合；同一轮内多个 case 可共享同一条日志，但每个 case 必须按具名 selector 独立核验，不按三锚乘三；
  - 缺 artifact、空目录、截断、skip、重复 run、后补 pass 覆盖先 fail 均失败。
- 若 04.5 未暴露 `--bundle`，05.2 **停止并上报**，不得用 `jq`/shell 重写一套矩阵与指纹规则（那就是第二套解析器）。
- 第四步 `if: always()` 上传 required 报告与下载到的证据；失败也上传，便于定位“哪一片缺失/哪一次 short 红”。上传路径用 `runner.temp`，不写仓库。
- 跳过语义：GitHub 对“被跳过的 required check”存在按成功呈现的陷阱，因此本设计不从 required 里补救，而是**在源头禁止跳过**（生产 job 无 `paths`/无 `if`/无凭据条件），再加 `always()` 与缺件失败；workflow 自测把这两侧都钉住。
- 本地完成口径：05.2 不要求远端 CI 绿；本地用 05.1 的真实证据目录运行同一 `--bundle` 命令，并通过 `CiWorkflowTest` 的结构断言即可勾选。远端 required 在 PR 上真实生效属于 L2/F4，另验，不冒充本地叶完成。
- 被否方案：只检查 `needs.*.result` 不看证据（分片成功但 artifact 为空仍会假绿）；缺 artifact 时“没有对象=通过”；把 required 做成 `if: success()`（上游失败时它自己 skipped）；把 required 写成全库唯一必跑并删旧检查；以远端 CI 绿作为 05.1/05.2 本地勾选条件。

## D3 preflight：默认完整事件入口，不静默缩水

- `scripts/preflight.sh` 在现有 lint/deterministic/三模块测试/smoke 之后，**默认**增加同一 runner/checker 入口：
  - `event integrity short`：`python3 scripts/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10 --out "${TMPDIR:-/tmp}/event-consumption-integrity/preflight/event-short"`
  - `event integrity long`：六个 `(seed, window)` 组合逐个独立执行，每个 `--scope event-long --seed … --window … --repeat 1 --out "${TMPDIR:-/tmp}/…/event-long/seed-…-window-…"`；默认不跳过，不新增以成本为由默认关闭 long 的 opt-in 开关。
  - `event integrity mutations`：`python3 scripts/check_event_integrity_mutations.py --all --json --out "${TMPDIR:-/tmp}/…/mutations"`
- `--skip-smoke` 保留原语义：只跳过微信 smoke，**事件 short/long/mutations 仍默认运行**；脚本打印“smoke 未运行，本次 preflight 不具备完整 MR 等价证据”，但退出码不被该标注改变。MUST NOT 因为 `--skip-smoke` 取消 event-long，MUST NOT 新增任何默认跳过或默认关闭事件 long 的开关。
- preflight MUST 调同一 CLI，不得内联复制 runner 的场景循环、矩阵或证据规则；`set -euo pipefail` 与逐步原始退出码保留。默认完整 long 的本地成本是明确代价；如确实需要快速迭代，只能由开发者显式选择不运行 preflight，不得由脚本自行猜一个“成本高所以不跑 long”的默认。
- 被否方案：新增默认关闭 long 的 opt-in 开关（默认仍不完整）；默认跳过 long 并打印一行“未运行”；把 preflight 的绿当作 CI 绿；用 `--skip-smoke` 顺手跳过事件 long。

## D4 workflow 自测：一个解析器，两个目标

- 保留 `scripts/test_check_runtime_workflow.py` 现有 `WorkflowTest` 对 `runtime-contracts.yml` 的全部断言与负控，不改旧目标、不降低旧门槛。
- 新增同一文件的 `CiWorkflowTest`，复用现有 `on_block`/`step_body`/断言辅助，只增加一个 `CI_WORKFLOW` 路径常量；MUST NOT 新建第二个 YAML 解析脚本或第二套 gate 表。
- 正向断言：
  - 四个事件 job 存在，名称逐字匹配；`event-integrity-required` 的 `needs` 恰为三者且 `if: always()`；旧 `test`/`race`/`validators`/`openspec`/`soak` 仍在；
  - short 命令含新合同与 `--repeat 10`，事件 job 全文无单次 count 放大；
  - long 的 matrix 恰为 `seed [1,7,42]`、`window [256,32768]`，`fail-fast: false`，命令含 `--seed`/`--window`/`--repeat 1`；每片 `per_combo` 为 1200/120，汇总 7200/720；
  - mutations 命令为 `check_event_integrity_mutations.py --all --json --out "${{ runner.temp }}/…"`；
  - required 下载 artifact 到 `runner.temp` 并调用 `check_runtime_evidence.py --bundle`；
  - 事件 job 无 `paths`/`paths-ignore`、无 job 级 `if`、无 `continue-on-error`、无 `secrets.*` 跳过；
  - 所有事件 artifact 上传步骤 `if: always()`，上传路径在 `runner.temp` 下；
  - `race` job 命令含全部映射的正式测试包（至少 execution/history/host/capabilities/evolution/learning），不只 host/history；
  - `preflight.sh` 默认含 short/long/mutations 三个入口，不存在默认关闭或跳过事件 long 的开关；`--skip-smoke` 只跳过 smoke 且打印“不具备完整 MR 等价证据”；
  - required 不被描述为全库唯一必跑：自测只断言它 `needs` 三个事件 job，并断言旧 job 仍存在，不断言“唯一”。
- 负控（只改解析后的副本，不写文件）：删一个事件 job、少一个 seed、把 `fail-fast` 改 true、删一个 `need`、删 `always()`、加 `paths` 过滤、加 `continue-on-error`、加凭据跳过、删 `--repeat 10`、加单次 count 放大、删 bundle 步骤、删 artifact 上传、给生产 job 加 `if`、给 preflight 加默认关闭 long 的 opt-in 开关、从 preflight 删 event-long、把 race 集合缩回 host/history——每种都必须让 `CiWorkflowTest` 变红。
- `lint.sh` 已有的 `unittest discover -s scripts -p "test_check_runtime_*.py"` 自动覆盖该文件，无需新增 lint 行。
- 被否方案：为 `ci.yml` 另写 `test_check_ci_workflow.py`；把断言写成“只要 job 名出现就算过”；只测 required 不测 preflight 默认入口。

## D5 文档：只写已落地能力与可证明边界

- `README.md`：能力矩阵加事件门一行；补“每 MR 事件门”短节，列四个 job、artifact 名、short 十次/long 六组合（单分片 1200/120，汇总 7200/720）/mutations 的一句话口径，并明说“本地绿 ≠ 远端 required 通过”“required 只是新增事件汇总，不替代旧检查”。
- `docs/OPERATION.md`：§四测试加事件双档与变异的本地命令（short/long/mutations 默认入口）；§五门禁加事件门表（job/命令/artifact/失败处置）；§七风险加“缺 artifact、跳过、长程抖动、预算不足”四类处置，强调不得靠重试或降覆盖变绿。
- `docs/FACT-VOCABULARY.md`：只登记 00/02 已确认的事实族与事件→事实归属；若 00.3 未新增族，则只加指向 GLOSSARY 的事件机制索引行，不凭空造族、不改“事实七族”口径。
- `docs/GLOSSARY.md`：登记事件完整性、消费确认、发送确认、真重启、short/long 档、killed/uncovered、required 汇总、证据档位八个词；每词给判据，不写实现细节。
- 命令措辞：04.5/05.1/05.2 未落地前，文档只能写“拟新增/未启用”；MUST NOT 让 `check_runtime_evidence.py` 或旧 `scenarios.json` 被描述为已支持新合同/新图。落地后才改为“可用”，并附真实命令与退出码口径。05.3 的终验必须等 05.2：草稿可先写，但 CI 段落不能在 05.2 之前从“拟新增”改成“可用”。
- 历史：既有 README/OPERATION/归档说明中的旧结论只做就地勘误（标注日期与原因），不改写历史结论、不追溯修改归档 change。
- 文档预算：`scripts/doc_budget.json` 现为 206 篇/21934 行且只降不升。不新增 `.md` 篇目；四篇的净增行必须用同篇删减等量置换，跑 `python3 scripts/check_doc_budget.py --json` 实测；只有净减少时才 `--tighten`，MUST NOT 上调基线。若确无可删减内容，停止并上报待裁，不靠“先加后放宽”过关。

## D6 soak 手动作业：先验证归属，再最小纠正

- 现命令 `go test ./tests/ -tags soak -run TestSoak_Continuity …` 与树不符：`./tests/` 不是包路径，`TestSoak_Continuity` 不存在，实际测试是 `tests/integration/host/soak_continuity_test.go` 的 `TestSoakContinuity`，且无 `soak` build tag。
- 实施 05.1 时先用 `go test -list '^TestSoak' ./tests/integration/host` 验证归属；确认后把命令改为 `go test ./tests/integration/host -run '^TestSoakContinuity$' -count=1 -v -timeout 45m -args -rounds=30 -events-per-round=30`，保留 `if: github.event_name == 'workflow_dispatch'` 与 job 本身。
- 该 job 继续是手动作业，不计入每 MR long、不作为 required 依赖、不替代六组合；若实际归属与静态线索不同，按实测改并记录偏差，不猜。

## D7 三层证据与独立偏差验收

| 层 | 内容 | 可勾选条件 | 未授权时 |
|---|---|---|---|
| L1 本地确定性完成 | 05.4 在最终集成树运行 MR 等价命令；05.1/05.2 本地用同一命令 + `CiWorkflowTest` 结构验收 | 全部命令 exit 0 且证据可复跑；本地 consumer 与 CI 同命令同结构 | 可完成 |
| L2 远端 MR 与 required | 推送分支、创建 MR、required 保护规则实际生效（F4） | 另获提交/推送/建 MR/改保护授权后，PR 上 required 真实通过 | BLOCKED/待验，不勾选，不作为 05.1/05.2 本地完成门 |
| L3 72h 与真实服务 | 真实模型/微信/掉电/长期运行 | 主规格既有条款，另立授权与预算 | 不宣称、不并入本 change |

- 05.4 的“独立偏差验收”要求验收者与实施者分离：逐条对照计划/合同/证据，保存偏差表（计划原文、实际做法、原因、影响）；验收者不可用时如实标“待验”，不以实施者自查冒充。
- 05.4 的本地绿证只写 L1；L2（含 F4 远端 CI/required）与 L3 分别登记，MUST NOT 用 L1 结果推断 L2/L3 通过。
- 未授权动作清单：`git commit`、`git push`、创建/更新 MR、修改分支保护、打 tag、真实服务调用。任一被执行即越权。

## D8 精确文件与风险

拟新增：无（不新建 workflow、不新建解析器、不新建文档篇目）。

拟扩展：

- `.github/workflows/ci.yml`（四个事件 job、race 受影响包集合、soak 命令纠正）
- `scripts/preflight.sh`（默认 short/long/mutations 入口与跳过标注）
- `scripts/test_check_runtime_workflow.py`（`CiWorkflowTest` 与负控）
- `README.md`、`docs/OPERATION.md`、`docs/FACT-VOCABULARY.md`、`docs/GLOSSARY.md`

零改动：产品源码、`tests/**`、`golden/**`、`scripts/race_check.sh`、`scripts/lint.sh` 判据、其他检查器、`openspec/specs/**`、`openspec/changes/archive/**`。

| 风险 | 处置 |
|---|---|
| long 六分片成本/时长超预算 | 分片并行、`fail-fast: false`；先校准再冻结熔断；优化编译与证据收集，不降覆盖、不削 fsync、不关冥想 |
| 长程抖动导致每 MR 红 | 定因修复；禁止用重试、`-count>1`、缩矩阵或放宽断言变绿 |
| 单分片被误写成 7200/720 | 合同与自测钉住 per_combo=1200/120、total=7200/720；任务与文档逐处写清单分片口径 |
| artifact 缺失被当“无对象通过” | required 的 `--bundle` 对缺件/空目录直接失败；负控钉住 |
| required 被跳过仍显示成功 | 生产 job 无 `paths`/无 `if`/无凭据条件；required `always()`；自测负控加 `paths`/`if` 必红 |
| required 被误当全库唯一必跑 | 文档与自测明确它只汇总新增三档；旧 job 仍存在；分支保护可保留旧检查 |
| race 只补 host/history 漏项 | 按全部修改/正式测试映射包集合；自测负控缩回 host/history 必红 |
| 旧 race waiver 被用来豁免新事件套件 | waiver 仅对非 tagent frame 生效；新套件不得 `-skip`/build tag 逃避；出现 tagent race 必须修产品 |
| 远端未授权却宣称 05.1/05.2 完成 | 本地门 = 同命令 + workflow 结构；L2/F4 单列，未授权保持待验 |
| 05.3 抢跑 | 草稿可写，完成必须等 05.2；依赖图表与任务表同步 |
| 文档预算不够 | 同篇等量删减置换；不上调基线；无内容可删则停止上报 |
| soak 归属再次猜错 | 先用 `go test -list` 验证；按实测改并记偏差 |
| 04.5 未提供 `--bundle` | 05.2 停止上报，不用 jq/shell 另写规则 |
| 原始运行产物写入仓库 | 原始日志/证据一律 `--out` 到 `runner.temp`/`TMPDIR`；证据摘要与 `log_ref` 只记入根 `ORCHESTRATION.md` 台账，不另建每叶 summary 文件；不宣称未核验目录已被忽略 |

回退：本域只改 workflow、preflight、workflow 自测与文档；回退即还原这些文件，产品行为与既有 CI 判据不受影响。
