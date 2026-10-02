## Context

用户指令确立终态哲学：注释薄、文档厚、注释核心是文档引用、go doc 仅简要职责。实测基线：174 非测试源文件（`scripts/` 除外）中 112 无 `契约:/规格:` 索引；591 导出声明全有 doc，但散文 ≥3 行者上界约 200（含 bullet 计入的保守口径；形态规则精确值待 P2 冻结），最厚 `Config` 41 行。既有规则（mechanism-narrative/rationale/audit-marker/free-standing）只打击特定形态，不约束总形态与文件归属；`missing-test-responsibility` 只覆盖测试侧声明义务。`comment_policy` 已具备：索引行语法与目标校验（`index-root`/`index-target-missing`/`index-anchor-*` 零容忍硬门）、按规则总量棘轮、`<a id>` 显式锚惯例、pre-commit 收敛批触发。test-colocation-gate 战役（25→0 切硬）刚验证了"脚手架棘轮 + 分域批 + 对账锚 + pathspec 提交"的完整打法。

## Goals / Non-Goals

**Goals:**

- 两规则落地并进 CI：`missing-file-responsibility`（文件级指针强制）+ `doc-not-brief`（声明 doc 形态：职责句 ≤2 行 + 仅 bullet/索引续行）。
- 终态 strict-0：归档前双槽删除、`missing-test-responsibility:9` 一并清零，`baseline.json` counts 归空 = comment_policy 全规则零容忍。
- 迁移供料闭环：削薄出的机制散文成为 wiki 正文，文档在战役中变厚。

**Non-Goals:**

- 不重构生产文件划分（沿用既有准绳：不为注释治理拆生产文件）。
- 不为声明级强制铺索引（文件级必有、声明级可选——591 处锚点即噪声，与"简要"自相矛盾）。
- 不动 `indexTargetRoots=docs/` 既有裁决、不引入告警级规则（两规则都走棘轮到硬门，无只报不拦形态）。

## Decisions

### D1 形态规则取代行数预算

行数预算治标（5 行段落照样厚）；形态规则（首 1–2 行职责句 + 其后仅 `- ` 要点/索引行）是"仅简要解释职责"的机检等价物，且与测试侧既有形态立法对称。上界约 200 声明需削薄，bullet 不计入散文（可扫读不算厚）。备选否决：行数预算（>5 行=63 起步）——拦不住 4 行段落式叙述，且与"简要"无同构性。

### D2 指针粒度：文件级强制，声明级可选

"每段注释核心=引用"若按声明级铺设=557 锚点噪声。文件级指针（112 缺）同时是 colocation 镜像闭环的另一半：测试文件镜像生产文件，生产文件镜像 wiki 小节。豁免：`scripts/`（沿 `check_test_merge.sh` 的 `^scripts/` 先例——门禁自用工具无 wiki 家园）、测试文件（已有 `missing-test-responsibility`）、`_test.go` 之外的全部 `.go`。

### D3 脚手架棘轮：本变更内出生、归档前死亡

纯"先清理后开门"在有活跃并行会话的仓库不安全（c561672 索引互卷实证）：清理窗口期并行提交新增违规，落地日集中爆红。两规则带基线上岗即刻挡增量；P4 战役归零后删槽切硬。终态仍是 strict-0——基线是变更内脚手架，不是长期预算，与"一步到位"不冲突。

### D4 迁移供料与批次编排

每域一批：先写/扩 wiki 小节（承接被削薄的散文，含 `<a id>` 锚），再削声明 doc 至形态，再补文件指针，再验证（`go test <pkg>` + `-race` + `gen_godoc --check`），`--update-baseline` 降幅恰等于该批 N，pathspec 提交。域序：小域先行（evolution/tool/spec 级 3–5 文件）练流程，`agent`(20) 拆 2–3 批，`examples/wechat-bot`(5) 独立批，热区压后。对账锚双冻结：`pointer-map.txt`（文件→目标小节裁决表）+ `beyond-brief.txt`（削薄对象清单），偏差即停。

### D5 gen_godoc 共存于 P1 定案

文件级索引行进入生产文件 doc 区后，`docs/api` 生成物是否渲染该行：过滤（保持 API 文档纯净）或链接化（docs/api 直达 wiki）。P1 实测定案并同批落地，不留给战役期。

## Risks / Trade-offs

- [削薄丢失契约细节（默认值/失败语义被删而非迁移）] → 战役批内动作定式=「迁文优先，削薄在后」：细节先落 wiki 才允许删注释；`wiki-code-sync` 各门（标识符一致/默认值一致/示例一致）逐批把关。
- [112 文件的 wiki 小节不存在，指针无家可归] → 正是倒逼本意：先补文档再补指针；既有 `index-target-missing` 零容忍硬门强制。
- [形态规则误伤合法长契约（如 stdlib 风格多段说明）] → 职责句 ≤2 行 + 无限 bullet 的形态已容纳绝大多数；真超载契约本就该进 wiki——判例在战役中逐个裁决并入 D4 对账锚记录。
- [并行会话窗口期增量] → 脚手架基线上岗即挡（REGRESSION 当场红）。
- [gen_godoc 渲染污染] → D5 前置定案。
- [`agent/` 热区冲突] → 压后 + pathspec + 批前工作树检查。

## Migration Plan

```
P0 工件(本批):proposal/design/specs/tasks 落地
P1 规则+单测+脚手架基线:missing-file-responsibility(实测≈112)/doc-not-brief(上界≈200);
   gen_godoc 共存定案;负路径双探针留痕
P2 权威测量:双对账锚冻结(pointer-map.txt 每文件裁决目标/beyond-brief.txt 削薄清单)
P3 分域战役:小域→memory→根→governance/compress→agent(拆批)→bot;每批迁文→削薄→补指针→验证→降 N
P4 归零切硬:双槽删除+missing-test-responsibility:9 清理→counts 归空断言
   →全规则负路径抽验→归档→终验(lint/build/test/race/bot/CI 四 job)
```

回滚：P1/P3 每批独立提交可单独 revert（revert 后棘轮回升须显式 `-force-raise`）；P4 删槽即终态，revert 基线文件即回棘轮。

## Open Questions

1. `doc-not-brief` 的职责句上限 2 行是否对 package doc（`doc.go`）放宽至 4 行——倾向不放宽（包概述同样该进 wiki），战役中若出现强例再裁决。
2. 文件级索引行的放置位（package 子句前的文件 doc 区 vs 文件内任一 doc 槽）——倾向前者（与测试侧同构、读者第一眼），P1 单测钉死。
