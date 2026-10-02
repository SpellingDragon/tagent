## Why

「测试文件族与职责同位」(architecture-guardrails)已立法,但 enforcement 只有半边:每个测试文件必须声明职责(`missing-test-responsibility` 棘轮,基线 9),而「同一职责的多个工况 MUST 收敛在同一测试文件内」这半句零机检。实测全仓 153 个已声明测试文件中,同 (package, `契约:` 锚点) 散在多文件者 23 组;去掉合法形态(build-tag 异组、桩/基座、生产文件镜像)后仍有 **17 组 / 26 文件**真碎片。

若按朴素「同锚点必须一文件」立门,会与同一需求的另一条款「测试文件族与所辖生产文件族一一对应」正面相撞——同锚点的多个文件常常各自精确镜像一个生产文件(如 `tool/action` 的 `settle_test.go`/`tmux_executor_test.go`),错并它们反而消灭镜像对应;且 build-tag(`soak`/`integration`)与 test-support 文件(桩/基座)在 Go 语义上不可合并。需要一个**镜像感知**的机检门禁,把立法变成 CI 事实而不制造错并。

## What Changes

- `comment_policy` 新增关系型规则 **`responsibility-fragmentation`**(包级聚合,超越现有 per-file 模型):同 (目录, build-tag 集, `契约:` 锚点) 的参与者(含 ≥1 个 `func Test` 的 `_test.go`)≥2 个,且某文件**无镜像**(同目录不存在 `<去_test后缀>.go`;`_real` 等既有变体后缀视为家族镜像)→ 该文件计一条 finding。
- 棘轮接入:基线槽 `responsibility-fragmentation`(按文件计,单调可降;shell 预演 25,未含 `_real` 宽容的口径为 26,以 P1 门禁实测落定),只降不升;归零后移出基线,升级为与 `mechanism-narrative` 同级的零容忍硬门。
- **三条合法出口**(门禁是粒度对齐压力机,不是合并机器):①工况并入镜像文件;②测试文件改名对齐生产镜像(如 `action_test.go` → `action_tool_test.go`,经 `--map` 登记改名);③文档侧收敛锚点(wiki 小节上收/细化/重挂)——代码与文档哪边粒度错了修哪边。
- 存量 17 组按四病型分域收敛:**命名错位型**(改名)、**族内多锚型**(锚点上收)、**e2e 挂单元锚型**(锚点重挂)、**真碎片型**(合并);每批经 `check_test_merge.sh` 无损校验。
- `check_test_merge.sh` 接线 pre-commit:收敛批的测试面零丢失安全网(生产码零改、函数体逐一对应、断言数不降)。
- `lint.sh` 零改动:新规则经既有 `comment_policy` 链路自动进 CI Lint 步。

## Capabilities

### New Capabilities

(无——本变更是对既有立法的 enforcement 补全,不引入新能力域。)

### Modified Capabilities

- `architecture-guardrails`:「测试文件族与职责同位」需求从纯立法升级为可机检——钉定「同职责」的判定单位(目录 × build-tag × 锚点 × 生产镜像)、钉定不可合并的合法形态(镜像文件、build-tag 异组、test-support)、钉定三条收敛出口与棘轮→硬门的升级路径。新增机检场景;既有三场景(分散合并/合并不改覆盖/端到端不拆散)语义不变。

## Impact

- **门禁本体**:`scripts/comment_policy/main.go`(关系规则 + 包级聚合 pass)、`scripts/comment_policy/baseline.json`(新槽,实测计数)、`scripts/comment_policy/main_test.go`(规则单测)。
- **接线**:`scripts/hooks/pre-commit`(收敛批调 `check_test_merge.sh`,含 base-ref 解析);`lint.sh` 与 `.github/workflows/ci.yml` 零改动。
- **存量收敛面**:26 个测试文件(纯测试侧移动/改名/锚点调整,生产码零改);`docs/wiki` 若干小节(B 型锚点上收、C 型重挂)。`tests/`(e2e,零生产文件)4 文件的处置属 C 型锚点重挂,不引入 tests/ 专属规则。
- **协作纪律**:并行会话热区文件(`agent/task` 两件)在对应收敛批动手前须确认工作树干净;棘轮 `--update-baseline` 每降必提交,防两条会话互相洗基线。
- **不改动**:生产代码、CI workflow 结构、`missing-test-responsibility` 既有基线(9)、上游 trpc-agent-go 依赖。
