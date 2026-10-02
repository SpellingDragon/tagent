## Why

用户指令确立注释哲学：**注释尽量薄、文档尽量厚，每段注释核心是文档引用，go doc 仅简要解释职责**。现状与之相悖且已有实测：174 个非测试源文件中 **113 个无任何 `契约:/规格:` 索引**；**99 个声明的 doc 含三个及以上散文段落**（最厚 `config.go:Config` 41 行、`LocalFileKV` 26 行、`New` 22 行——机制叙述堆在注释里，文档反而薄）。既有规则只禁"机制叙述/取舍理由"等特定形态（mechanism-narrative/rationale），不约束叙述深度，也不要求生产文件声明文档归属——注释薄化的倒逼闭环缺了半边。

## What Changes

- `comment_policy` 新增规则 **`missing-file-responsibility`**：每个非测试 `.go` 文件（`scripts/` 豁免，沿 `check_test_merge.sh` 先例）必须承载一行文件级 `契约:/规格:` 索引，指向 `docs/` 下真实小节（既有 `index-target-*`/`index-anchor-*` 硬门继续兜底：锚点不实即红，倒逼先补文档）。
- `comment_policy` 新增规则 **`doc-not-brief`**（形态规则，镜像测试侧已立形态）：声明 doc = 至多两段职责散文 + 穿插的 `- ` 要点行与索引行；**第三个叙述段落即违规**（物理换行不计深度，叙述段落才是）——段落式机制叙述无家可归，只能进 wiki。
- **脚手架棘轮，本变更内出生、归档前死亡**：两规则带实测基线上岗挡增量（113 / 99），战役内逐域清零，P4 删双槽转零容忍硬门——终态 strict-0，非长期预算。
- 分域战役：被削薄的注释散文**迁移成 wiki 正文**（"文档厚"由"注释薄"的迁移供料）；每域一批（写/扩 wiki 小节 → 削薄声明 doc → 补文件指针 → 验证 → 基线降 N 对账）。
- **counts 归空收口**：顺手清 `missing-test-responsibility:9` 存量（9 个测试文件补索引），归档时 `baseline.json` counts 为空对象 = comment_policy **全部规则零容忍**——用户指令"所有规则最后通过 CI 固化"的终态断言。

## Capabilities

### New Capabilities

（无——两规则均落于既有 `code-documentation` 能力域。）

### Modified Capabilities

- `code-documentation`：①MODIFIED「go doc 注释的契约边界」——契约定义收窄为"简要职责 + 细节归文档"，新增 doc 形态机检（职责句 ≤2 行 + 仅 bullet/索引续行）；②ADDED「源文件职责索引」——非测试源文件必须声明所辖文档小节，与既有测试侧声明义务（architecture-guardrails）对称闭合。

## Impact

- **门禁本体**：`scripts/comment_policy/main.go`（两规则）+ `main_test.go` + `baseline.json`（双槽起 = 实测值，终 = 移除）。
- **存量战役面**：113 文件补指针、99 声明削薄（P2 逐声明冻结清单）、`docs/wiki/` 约 12 域小节增扩（迁文供料）、9 测试文件补声明。
- **接线零改动**：经 `lint.sh → comment_policy` 既有链路自动进 CI；`gen_godoc` 与文件级索引行的共存（过滤或链接化）在 P1 定案并落地。
- **协作纪律**：pathspec 提交、每批降幅恰等于 N、并行热区文件批前检查——沿用 test-colocation-gate 已验证的批规程。
