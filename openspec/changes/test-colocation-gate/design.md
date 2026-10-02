## Context

`architecture-guardrails` 的「测试文件族与职责同位」已立法双条款:同职责工况收敛一文件 + 测试文件族与生产文件族一一对应。enforcement 现状只有声明侧(`missing-test-responsibility` 棘轮,基线 9),收敛侧零机检。全仓实测:153 个已声明 `_test.go`,同 (目录, 锚点) 多文件 23 组;剔除合法形态后真碎片 **17 组 / 26 文件**。

朴素「同锚点必须一文件」门禁有三个已证实的失败模式:①镜像相撞(同锚点文件各自镜像生产文件,错并消灭对应关系,如 `tool/action` 三组);②物理不可并(`//go:build soak|integration` 异组、Test=0 桩/基座);③逼错动作(26 文件中仅约 10 个的解是合并,其余是改名或文档侧锚点修正)。本设计以**镜像感知**化解三者。

`comment_policy` 现为 per-file 模型(`checkFile` 逐文件出 finding,`finding.Key()=Rule` 做按规则总量棘轮);本规则是关系型(跨文件聚合),需在其文件遍历后加一道包级 pass。镜像判定是文件系统事实(`os.Stat`),与既有 `index-target-missing` 同级,不引入新依赖形态。

## Goals / Non-Goals

**Goals:**

- 「同职责收敛」从立法变成 CI 机检事实:新增 `responsibility-fragmentation` 规则,经 `lint.sh → comment_policy` 既有链路自动进 CI,`lint.sh`/`ci.yml` 零改动。
- 判定不制造错并:镜像文件、build-tag 异组、test-support 三类合法形态零误报。
- 收敛压力双向传导:代码侧(合并/改名)与文档侧(锚点上收/细化/重挂)都是合法出口。
- 存量 17 组以棘轮消化(26 起步,只降不升),归零后升级零容忍硬门。

**Non-Goals:**

- 不改动生产文件划分(既有条款明令:MUST NOT 借测试合并之名拆生产文件)。
- 不为 `tests/`(e2e,零生产文件)立专属规则——其处置走 C 型锚点重挂。
- 不引入「测试文件族」的模糊族匹配器(前缀/包含匹配)——镜像判定只认精确文件名 + 既定变体后缀。
- 不动 `missing-test-responsibility` 既有基线与棘轮语义。

## Decisions

### D1 规则归属:`comment_policy` 而非 codetools

`契约:` 锚点语义、棘轮机制、CI 接线三者都在 `comment_policy`;codetools 虽有 `merge-check`(测试面分析基因)但无棘轮、无 lint 链路,且锚点语义不属于它。代价是给 `comment_policy` 加第一个**关系型** pass:主遍历收集每个测试文件的 (目录, build-tag 集, 锚点, Test 函数数),遍历后按组聚合出 finding——per-file 聚合器天然可容纳,`Key()=Rule` 棘轮槽位无需改。**备选否决**:codetools 落地(需再造棘轮+接线,双倍门禁基础设施);独立脚本(第三个门禁入口,违背 lint.sh 单一入口注释的自我声明)。

### D2 判定单位与参与者

- **分组键**:`(目录, 包名, build-tag 集, 契约锚点)` 四元组。build-tag 进键,`soak`/`integration`/`race_enabled` 异组自动不可比,免显式豁免表。包名进键是 P2 实施揭示的修正:一个目录可同时持有内部测试包(`package memory`)与外部测试包(`package memory_test`),二者是独立编译单元,函数体跨包平移必改限定符、与无损前提冲突——原三元键在这类目录产生假阳性(实测 `memory/#tombstone`、`#error-tracking` 两组共 4 文件,修正后 −2)。
- **参与者**:含 ≥1 个 `func Test` 声明的 `_test.go`。Test=0 者无论 Bench 有无(桩 `mock_model_test.go`、基座 `testbase_test.go`、纯 bench `segment_store_bench_test.go`)一律非参与者——它们声明锚点是标明支撑关系,不是重复测试面。
- **违规**:同键参与者 ≥2 时,组内**无镜像**的每个文件计一条 finding。

### D3 镜像判定:精确文件名 + 既定变体后缀

镜像 = 同目录存在 `<去 _test 后缀>.go`。`zhipu_real_test.go` 对 `zhipu.go` 这类**既存变体后缀**(仅 `_real`)按家族镜像宽容。**不做前缀/包含模糊匹配**:`action_test.go` 对 `action_tool.go` 判不中,是特性不是缺陷——精确匹配同时对齐镜像**命名**,逼出的正解是改名(出口②),模糊匹配会把这个压力泄掉。备选否决:前缀匹配(`action` ⊂ `action_tool` 即认镜像)——开口即不可收(`s` 前缀?`a` 前缀?),且重新引入锚点通胀式的博弈面。

### D4 三出口等价合法,finding 消息同时给出

| 病型 | 出口 | 判例 |
|---|---|---|
| D 真碎片 | ① 工况并入镜像文件 | `meditation_audit_test.go` → `telemetry_audit_test.go` |
| A 命名错位 | ② 改名对齐镜像(`--map` 登记) | `action_test.go` → `action_tool_test.go` |
| B 族内多锚 / C e2e 挂单元锚 | ③ 文档侧锚点收敛(上收/细化/重挂) | `segment_store_{barrier,recovery}_test` 上收族锚;`tests/integration_test` 重挂 e2e 级锚 |

finding 消息必须并列三条出口——门禁是**粒度对齐压力机**,不是合并机器;与「注释只指向文档、倒逼文档更新」总纲同构。

### D5 棘轮计数按文件,不按组

组计数在部分收敛时不降(3 文件组并掉 1 个仍算 1 组),进度不可见、协作挫败;文件计数单调可降 26→25→…→0,每并一个文件棘轮立即可 `--update-baseline`。基线写入走既有 `lint.sh --update-baseline`(继承 scan-set 一致性防局部洗基线)。

### D6 升级路径:棘轮 → 硬门

26 起步只降不升(shell 预演口径;含 `_real` 变体宽容后为 25,包名入键修正后 P1 实测 25→逐步收敛)。归零后从 `baseline.json` 移除该槽——按既有判定 `counts[k] > base.Counts[k]`,未登记规则基线视为 0,自动升级为与 `mechanism-narrative` 同级的零容忍,无需代码改动。

### D7 `check_test_merge.sh` 接 pre-commit,不进 CI 常驻

它需要 `<base-ref>` 且语义是「校验一次收敛批」,对非收敛 PR 是噪声。接线形态:pre-commit 检测暂存区含 `_test.go` 且存在收敛批标记(如 `--map` 表文件在暂存区)时,以 `HEAD` 为 base-ref 调用;纯新增/常规改动不触发。**备选否决**:CI 常驻 job(merge-base 做 base-ref 技术可行,但每个 PR 都跑测试面全比对,信号噪声比恶劣)。

### D8 收敛编排:四病型分域,并行热区最后

`evolution`(1)→`tool/action`(1)→`rl`(2)→`memory`(8)→`agent`(5)→`tests`(4)。排序原则:文件数升序建立流程肌肉;`agent/task` 两件是并行会话热区,压后到 `agent` 批并前置「工作树干净」检查。每批单一病型、单一出口型,批内 `check_test_merge.sh --map` 无损校验 + 全量 `-race` 绿后提交并 `--update-baseline`。

## Risks / Trade-offs

- [锚点通胀逃逸:滥用出口③铸造细锚逃避合并] → 既有 `index-anchor-required`/`index-target-missing` 硬门保证锚点必须指向真实文档小节;锚点细化本身受 doc-refs 与评审约束;四病型分类在 tasks 中逐文件预裁决,收敛批不接受临场发明锚点。
- [并行会话互相洗基线] → 棘轮每降必提交;`agent` 批前置工作树干净检查;`checkScanSet` 既有防局部扫描机制继承。
- [新变体后缀蔓延(`_mock`/`_fake` 要求同等待遇)] → 变体白名单冻结为仅 `_real`(仓库既存惯例);新增后缀必须走本变更同等评审,不开口子。
- [镜像以文件名为代理,漏「族」概念(`segment_store` 族三测试文件挂两锚)] → 有意为之:B 型的正解是锚点上收(出口③)而非引入族匹配器;若将来族匹配成为主流需求,另立变更评审。
- [`tests/` 无镜像语义,C 型重挂可能把 e2e 锚挂歪] → 重挂目标锚点须在收敛批内列出并说明落点语义;`tests/` 锚点族化是否单独立法列为 Open Question,不在本变更内裁决。
- [26 文件收敛批与并行 merge-review 后续工作冲突] → D8 排序把热区压后 + 每批独立提交;冲突发生时该文件退出当批、单独重排。

## Migration Plan

```
P0 接线 check_test_merge.sh → pre-commit(纯工具,先行,可独立回滚)
P1 门禁上线:规则 + 单测 + 基线 26(挡增量,存量不阻塞;CI 即刻生效)
P2 分域收敛批 ×6(evolution→tool/action→rl→memory→agent→tests),
   每批:无损校验 → -race 绿 → 提交 → --update-baseline 降槽
P3 基线归零:移除槽位 → 零容忍硬门 → 主 specs 已含升级条款,无需二次立法
```

回滚:P0/P1 各自独立提交,可单独 revert;P2 每批独立,revert 后棘轮槽回升须显式 `-force-raise`(既有防误升机制);P3 是删一行基线,revert 即回棘轮。

## Open Questions

1. **单文件多锚是否解禁**:B 型收敛后可能出现「一个测试文件覆盖族内多个小节」;现条款要求一行索引。本变更维持一锚(逼锚点粒度=族粒度),若实践中确需主/副锚,另立变更。
2. **`tests/` 锚点族语义**:e2e 是否应只允许 `#e2e-*` 族级锚(消灭 C 型温床)。本变更以重挂处置存量,族化立法留待 `tests/` 再出现新 C 型时裁决。
3. **变体后缀白名单的治理位**:冻结在 design 还是写进 spec 场景。当前取 design(实现细节);若 P2 期间出现第 2 个后缀诉求,升级进 spec。
