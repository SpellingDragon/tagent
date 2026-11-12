# Design: 02-vocabulary-gate

> **甲 修订（取代旧「指纹棘轮＋泛义保留」方案）。** 勘误：本文旧版据伪前提「user_ruling 泛义保留」（执行期 agent 决定被误标为用户裁定）以 check_spec_hygiene.py 指纹棘轮为逐字模板（DEFAULT_REGISTRY 基线＋指纹 sha256＋stale 不报错＋--write-baseline），四规则带「基线豁免」列逐处分类豁免泛义 carrier。用户明确「我没有裁定过泛义保留」。甲＝carrier 全退役＋**纯短语禁门**（命中即红，无基线/分类/棘轮/豁免/镜像）＋删 2377 行基线，净负代码（门 550→266、自测 480→215）。权威门形态见上级 design.md D6。

决策权威见一级 `design.md` D6（门宿主裁定＋纯短语禁门形态表）。本域只落实现细节。

## 实现参照（纯短语禁，非指纹棘轮）

甲门为**纯短语禁**：`guard_checklib as lib`＋`RULES` 编译正则字典＋`scan_files`/`joined_pieces`/`piece_line`/`go_prose_paragraphs`/`doc_paragraphs`/`scan_line`/`scan_prose`/`scan_tree`＋exit 0（净）/1（命中）。**删除**旧指纹棘轮骨架：`load_registry`/`hit_key_order`/`baseline_entry`/`write_baseline`/`verdict`/`EXEMPT_CLASSES`/`CLASSIFICATIONS`/`DEFAULT_REGISTRY`/fingerprint sha256/`--registry`/`--write-baseline`/镜像逻辑（`prosaic` 随镜像移除成死代码亦删）。check_spec_hygiene.py 不再是模板（其指纹棘轮＋基线正是甲所否）；仅共享 guard_checklib。

## 四规则（纯短语禁，GLOSSARY 锚定，无基线豁免）

| watch | 规则形态 | 范围 | 合法形（不匹配禁式，自然不拒） |
|---|---|---|---|
| carrier | 大小写不敏感**子串禁** `carrier`（含驼峰标识符如 commitCarrier） | `*.go`＋`docs/api/*.md` | 动词 carries/carried/carrying 不含 carrier 子串；carrier 全退役后无合法子串命中 |
| compression-generation | **短语禁** `generational\|fold\s*generation\|folding\s*generation`（大小写不敏感；逐行扫标识符/字符串＋注释续行先归并散文再匹配，跨行不漏；单行只计一次不重复） | `*.go`＋`docs/api` | gen: 锚／instance_id／recorder generation（非禁用形） |
| plugin-product | 大小写敏感**子串** `Product`（含复合物 MaterialProduct/pluginProduct） | 仅 `api/plugin/` | compress.product 事实族在 api/event/，不在扫描面；api/plugin 小写 product 散文不命中 |
| retired-word | **短语禁** `读透视图\|链权威答案` | `*.go`＋`docs/api` | —（GLOSSARY/退役声明本身在门扫描面之外） |

**无豁免/分类/棘轮/镜像**：命中即红（exit 1），净即绿（exit 0）。门不读不写任何分类基线文件。

## 关键实现点

1. **短语逐行＋注释跨行归并（保留）**：禁用短语逐行扫全部文本（标识符、字符串字面量、单行注释），驼峰形 foldGeneration 因 `fold\s*generation` 允许零空格而命中。注释跨续行（「…by its folding」＋「// generation.」）另把连续 `//` 行剥前缀归并为散文段匹配；归并只记跨行命中（spans_pieces：match 起止落在不同注释行），单行命中已由逐行扫描记录，避免重复计数。否则标识符形或跨行短语漏检＝症状锚失效（age_segments 实证漂移形，删归并＝缩水验收）。
2. **标识符子串匹配（carrier 与 Product）**：`\bcarrier\b`／`\bProduct\b` 词边界不命中驼峰子串——carrier 用子串（大小写不敏感）覆盖 commitCarrier 等，Product 用子串（大小写敏感）覆盖 MaterialProduct／pluginProduct 等复合物；小写 product 散文不含大写 Product，天然不命中。**carrier 全退役后无合法子串命中，无需任何豁免**（旧「靠基线分类豁免泛义标识符」被甲推翻）。
3. **纯短语禁命中即红（取代指纹棘轮）**：每处命中即 exit 1 具名 file:line，无指纹登记/无基线/无 stale/无 --write-baseline；修复一处命中即绿（exit 0）。旧 (file, rule, text_sha256, count) 指纹棘轮＋stale warn＋--write-baseline 全部删除。
4. **无基线（删 vocabulary_alignment.json）**：旧 2377 行分类基线（含 ADR/user_ruling/review_by/reason 元数据、逐处豁免分类）随伪前提「泛义保留」一并删除；门不读不写任何基线文件。净负代码（基线 2377→0）。
5. **lint 接线（wiring 字节不变）**：lint.sh policy 块（spec_hygiene 行旁）`run python3 scripts/gates/test_check_vocabulary_alignment.py`＋`run python3 scripts/gates/check_vocabulary_alignment.py`（test+check 对，无参）；注释改纯短语禁（无基线/豁免）。ci.yml:109 Lint step 已跑 `bash scripts/lint.sh`→gates/lint.sh，无需改 ci.yml（grep 实证）。门红则 CI 红。

## 负例自测（test_check_vocabulary_alignment.py，17 例 52 断言）

tempfile+subprocess 真退出码（非空转）。**植入违规→断言门 exit 1**：①carrier 标识符 commitCarrier②carrier 散文③跨行 folding generation④单行 folding generation（只计一次，compression-generation=2 不重复）⑤标识符 foldGeneration 驼峰⑥generationalBand⑦字符串字面量 folding generation⑧api/plugin Product＋复合物 MaterialProduct⑨pluginProduct⑩退役词 读透视图／链权威答案；缺 docs/api 亦红。**植入合法形→断言不误拒**：gen: 锚／instance_id／recorder generation 标识符／api/event compress.product 事实族／api/plugin 小写 product 散文／动词 carries/carried/carrying（不含 carrier 子串）。自测证明拒收逻辑，不勾选产品验收。（旧「⑧泛义 fact carrier 断言不拒」被甲推翻——泛义 carrier 全退役，命中即拒。）

## 风险与回退

- 误改义类（非误拒泛义）：carrier 全退役，每处按上级 design D6 映射改既有精确实名；映射未覆盖的歧义义类上报编排者裁，不自行豁免任何命中换绿（纯短语禁无豁免）。回退＝门未接线前删检查器即可（lint.sh 单行）。
- 漏检跨行/标识符形：归并＋逐行短语扫描须有自测③④⑤⑥⑦钉住；小写 product 散文不误拒须有合法形自测钉住；动词 carries/carried/carrying 放行须有自测钉住。