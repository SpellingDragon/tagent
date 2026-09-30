# spec ↔ 门 追溯表（28.7）

规范里每条 SHALL/MUST NOT 必须映射到一个**可运行的检查**；映射不到的，要么写成明确的人工判据（并说明为何不可自动化），要么就是**门缺口**，必须先补再清包。本表是 `restrict-comments-to-godoc-and-index` 的对照结果，后续新增规则时同步更新。

## 一、`code-documentation`

| # | 规范要求 | 检查 | 状态 |
|---|---|---|---|
| 1 | 注释形态白名单（仅 go doc ＋ 文档索引） | `comment_policy: free-standing` | 机器 ✓ |
| 2 | 机械语法行豁免（`//go:*`、`//nolint`、cgo） | `comment_policy: isExempt` | 机器 ✓ |
| 3 | 实现叙述被识别并迁出 | `mechanism-narrative`、`rationale` | 机器 ✓ |
| 4 | 章节横幅不属于任何声明 | `free-standing`（横幅在 doc 槽外） | 机器 ✓ |
| 5 | **契约信息不得因清理而丢失** | 无 | **人工**：等价门只能证明"只改了注释"，不能证明"信息未丢"。约束方式＝删除前逐条三判（已覆盖→删／有价值→先入文档→再删）＋**每批打印全部被删行并逐条复核**（24.6、28.5）；快照＋`comment-check` 保证不夹带改码 |
| 6 | doc 注释以所依附标识符名开头 | `doc-not-name-prefixed` | 机器 ✓ |
| 7 | 每个**包**有 package 注释 | `missing-package-doc`（按包判定，非按文件） | 机器 ✓ |
| 8 | 每个**导出符号**有 doc | `missing-symbol-doc`：导出函数/方法/类型/常量/变量 | 机器（部分，见 G-1） |
| 9 | 索引固定语法 `// 契约: <路径>[#锚点]`，一行一个、不承载解释 | `index-root`、`index-line` 形状 | 机器 ✓ |
| 10 | 目标只允许 `docs/**`（终裁剔除 `openspec/specs/**`，与门 `indexTargetRoots` 一致） | `index-root` | 机器 ✓ |
| 11 | MUST NOT 指向 `openspec/changes/**` 或过程工件 | `process-artifact-ref`、`unindexed-path-ref` | 机器 ✓ |
| 12 | 索引必须落到具体章节（大文档带锚点） | `index-anchor-required`（>200 行）、`index-anchor-unknown`（锚点不解析） | 机器 ✓ |
| 13 | 测试文件 doc 槽＝一行意图＋可选要点列表＋索引，单行有长度上限 | `test-doc-not-one-sentence`、`test-doc-line-too-long`、`missing-test-responsibility` | 机器 ✓ |
| 14 | **用例论证移入断言消息**（期望 X 进消息、判据 Y 入 wiki） | 仅强制"doc 不得多行" | **人工**：无法机器判断"断言消息是否承载了期望"。约束方式＝压缩测试 doc 前逐条核对断言消息，且改断言属第三类变换，须单独一批并证断言数不变（21.9、24.8） |
| 15 | 注释剥离等价证明（只改注释） | `codetools comment-check`（词元流判等） | 机器 ✓ |
| 16 | 借道改码被拦截 | 同上（任一 `CODE-CHANGED` 即整批退回） | 机器 ✓ |
| 17 | 政策扫描 ＋ 豁免台账（新增豁免需显式登记） | `baseline.json` 棘轮（只准降）＋ `explain-*.tsv` 入库 | 机器（棘轮）＋**人工**（豁免条目的理由是否成立、是否最小：目前靠一次性三连验，非持续门） |
| 18 | **生成式 API 文档与新鲜度**（`docs/api/` ＋ 漂移检出） | `scripts/gen_godoc.sh`（生成 37 包＋索引）＋ `--check` 已接入 `scripts/lint.sh` | 机器 ✓（G-2 已闭合） |

## 二、`architecture-guardrails`

| # | 规范要求 | 检查 | 状态 |
|---|---|---|---|
| 19 | 测试文件族与职责同位（同职责多工况并入一文件） | 拆分决策＝人工；`missing-test-responsibility` 保证每文件声明职责 | 人工＋机器 |
| 20 | 合并不改变测试覆盖 | `codetools merge-check`：missing/extra-test、missing/extra-helper、`body-changed`、`assert-count` 单调、`parallel-count`、`test-name-mangled` | 机器 ✓ |
| 21 | 端到端链不因收敛而拆散 | 无（哪些算"一条链"是判断） | **人工**：以 hold 清单＋`e2e`/`soak` 门控文件独占为约束 |
| 22 | **测试标识不承载迭代编号** | 无持续门（一次性扫描脚本 `demap_*.py` ＋ 改名映射表） | **门缺口 G-3** |
| 23 | 期望移入可执行位置（表驱动用例名/断言消息） | 无 | **人工**（同 #14） |

## 三、门缺口清单（按处置优先级）

| ID | 缺口 | 影响 | 处置 |
|---|---|---|---|
| ~~G-2~~ **已闭合** | `docs/api/` 生成器与新鲜度门 | 生成器早已存在但从未运行、也未接入 lint ⇒ 整条规范实际无检查 | 已生成 38 份（37 包＋索引）入库路径 `docs/api/`，`--check` 接入 `scripts/lint.sh`；`bash scripts/lint.sh` 现打印 `lint: ok` |
| **G-3** | 无持续的"标识不承载迭代编号"检查 | 新写代码可再次引入 `TestI1_*`、`wp4*` 一类名字 | 加 `codetools name-check`：对 `Test*`/包级标识符跑编号模式匹配，白名单放领域词汇（`Int64`、`e2e`、`L1..L3`、`V1/V2`、`MD5`、模型名等）；并入 `scripts/lint.sh` |
| **G-1** | `missing-symbol-doc` 的范围是否含**导出结构体的导出字段** | 现只覆盖函数/方法/类型/常量/变量；若按字面解释 spec"每个导出符号"，字段也算，则全仓会有数百条既有欠账 | **需裁决**：扩范围（并把既有量登记为棘轮起点）或把范围在 spec 里明确为"符号＝包级可见声明，字段除外"。我倾向后者（字段文档以 `doc-not-name-prefixed` 约束形态即可），但这是规范口径变更，不替你定 |

## 四、维护约定

- 新增/修改 `specs/code-documentation`、`specs/architecture-guardrails` 的 SHALL/MUST NOT 时，**同一批内**在本表登记对应检查名；写不出检查名的，明确标"人工"并说明为什么不能自动化。
- 人工项不是免责：它要求批次证据（被删行清单、断言数不变证明、豁免理由），缺证据即视为未做。


## 55 轮补条：内容规则不因排版位置豁免（门缺口 G-4 → 已闭合）

| 项 | 原状态 | 处置 |
|---|---|---|
| G-4：`process-artifact-ref`／`rationale`／`audit-marker`／`mechanism-narrative`／`unindexed-path-ref` 只作用于 doc 槽 | 注释一旦被判定 free-standing 就 `continue`，其内容不再检查 ⇒ 一条 `// Design: <变更目录>/design.md` 只要排版错位即**完全隐形**（实测：`org_hotreload.go:19`、`examples/wechat-bot/main.go:274`、`reincarnation_notice.go:3` 三处未被报） | 红测 `TestFreeStandingCommentsAreContentChecked` 先失败（0 < 2）→ 修补 free-standing 分支同时跑内容检查 → 转绿；规则加强后存量从 5 暴露为 9，当场修掉 6 处（`tool/plan` 4 处系把**运行域路径**写成引用、`verify_large_file.go` 把"结果记进过程工件"当指令、`codetools/check.go` 自身注释含字面模式），余 3 处需新增长期文档承载 ⇒ 记 D-18，归各自包批次 |
