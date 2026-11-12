# Proposal: 02-vocabulary-gate

> **甲 修订（取代旧「指纹棘轮＋泛义保留」方案）。** 勘误：本文旧版据伪前提「user_ruling 泛义保留」（执行期 agent 决定被误标为用户裁定）主张「仿 check_spec_hygiene.py 指纹棘轮＋人工分类基线」，逐处分类豁免 ~340 泛义 carrier、仅检查点义不豁免。用户明确「我没有裁定过泛义保留」。甲＝carrier 全退役（每处改既有精确实名）＋**纯短语禁门**（大小写不敏感子串 carrier，命中即红，无基线/分类/棘轮/豁免/镜像）＋**删 2377 行分类基线**，净负代码（门 550→266、自测 480→215、基线 2377→0）。权威门形态见上级 design.md D6。

## Why

词汇审计实证代码侧残留多义旧词 carrier 与 GLOSSARY 行 3「描述机制一律用代码实名」脱节，且无机械门防回流。既有门各管一轴（concept_budget D1＝新增导出类型名、name-check＝迭代编号测试名、comment_policy＝注释形态、spec_hygiene＝spec 散文形态），无一覆盖「注释＋标识符用词对齐 GLOSSARY 规范词」。本域立门把设计意图量化为机械判定，未评审漂移必红。carrier 全退役后无合法子串命中，纯短语禁即可恒判定，无需分类基线。

## What Changes

重写 `check_vocabulary_alignment.py` 为**纯短语禁**（carrier 大小写不敏感子串／generational|fold generation|folding generation 短语／api/plugin 内 Product／退役词；命中即红，**无基线/分类/棘轮/镜像豁免**）＋负例自测（17 例 52 断言）＋**删除旧 2377 行分类基线** `golden/runtime/vocabulary_alignment.json`＋lint/CI 接线；architecture-guardrails 新增「代码词汇对齐 GLOSSARY 规范词（机械门）」requirement（本变更唯一 spec delta）。

## 边界与依赖

- **依赖**：`guard_checklib`（接口常数，spec_hygiene 同款共享库）；GLOSSARY 行 3「描述机制一律用代码实名」＋行 27/41＋§五为规范词/禁词/冻结拼写的**接口常数**（carrier 未登记为规范词，故全退役）。无前置实现依赖——立案即跑（C 轨）。
- **被依赖方**：01 residual-cleanup（门红锚存在才能验清存量转绿）；03 structural-golden 的 golden 载体→checkpoint 复核（门入 main 后）。
- **接口面**：`python3 scripts/gates/check_vocabulary_alignment.py [--root] [--json]`，exit 0＝净（无命中）、1＝命中/输入不可用；**无基线**（`golden/runtime/vocabulary_alignment.json` 已删，门不读不写任何分类基线）；lint.sh policy 块 test+check 对。
- **禁止事项**：MUST NOT 新建 code-vocabulary 能力（归 architecture-guardrails）；MUST NOT 改 GLOSSARY 规范词/冻结拼写；MUST NOT 动 01/03 白名单文件；MUST NOT 改 .go 逻辑（本域只建门，不清存量——清存量是 01）；MUST NOT 引入任何基线/分类/豁免/棘轮/镜像（纯短语禁，命中即红）；映射未覆盖的歧义 carrier 义类上报编排者裁，MUST NOT 自行豁免任何命中换绿。

## Impact

- 症状锚：门对现状 exit 1（**390 命中**＝341 .go＋49 docs/api 镜像，全 carrier；已存证 red-anchor-390.log），01 全退役＋godoc 重生成后 exit 0（0 命中）。
- 最大风险：carrier 逐处精确词映射（上级 design D6，误改义类＝词汇失真）；跨行短语归并（漏检＝门无效，门保留注释续行归并提取器）。
- 检测精度：carrier 大小写不敏感子串（动词 carries/carried/carrying 不含子串故放行）；generational/fold generation/folding generation 短语禁（gen:/instance_id/recorder generation 合法形不触禁式）；Product 范围限 api/plugin（compress.product 事实族不扫、小写 product 散文不拒）；退役词短语禁。**无豁免/分类**。
- 净负代码：门 550→266、自测 480→215、基线 2377→0（改名不增行）。spec delta 散文只述不变量（spec_hygiene R4），门名/命令置 Scenario。