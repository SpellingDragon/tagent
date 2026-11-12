# Proposal: code-vocabulary-alignment

> **甲 修订（取代旧「指纹棘轮＋泛义保留」方案）。** 勘误：旧 proposal 把「仅检查点义 carrier 退役、泛义经基线豁免保留」当立案前提，据此建 2377 行分类基线＋550 行指纹棘轮门。该前提系执行期 agent 决定被误标为 user_ruling（W8-VOCAB）；用户明确「我没有裁定过泛义保留」。GLOSSARY 行 3「描述机制一律用代码实名」未登记 carrier 为规范词。甲＝**carrier 全退役**（每处改既有精确实名，不造新概念）＋**纯短语禁门**（无基线/分类/棘轮/镜像豁免），净负代码。旧已落地的检查点义 carrier→checkpoint 改名与 folding generation→band promotion 经复核映射正确，保留。详见 design.md D6。

## Why

词汇审计实证：纯化已把架构脊柱与文档词汇统一，但**代码侧仍残留多义旧词 `carrier`**——事实载体／提交承载／凭据／URL／查询载荷／检查点诸义混用同一词，与 GLOSSARY「描述机制一律用代码实名」脱节；`age_segments.go` 注释用「folding generation」描述 band 晋升（GLOSSARY 行 41 明文「不用 generational／fold generation 说法」）。用户原症状＝「代码词汇违反维护规范、与文档脱节、维护熵增」。

只清存量不够：没有机械门，旧词会随后续变更静默回流（熵增）。既有门各管一轴（concept_budget＝新增导出类型名绑定、name-check＝迭代编号测试名、comment_policy＝注释形态、spec_hygiene＝spec 散文形态），**无一覆盖「注释＋标识符用词对齐 GLOSSARY 规范词」**。故须立门把「设计意图」量化为机械判定，未评审漂移必红。

## What Changes

- **01 residual-cleanup**（实现子域）：清存量违规——`carrier` **全退役**（泛义→既有精确实名：commit／DurableCommit／credential／URL／projection／envelope／fact／record，见 design D6 映射）；旧检查点义 carrier→checkpoint 改名保留；age_segments「folding generation」→band promotion 保留；测试 helper/夹具/局部变量改名；两处冻结归档附勘误；godoc 重生成。
- **02 vocabulary-gate**（唯一 spec delta）：立 `check_vocabulary_alignment.py` **纯短语禁**机械门（carrier 大小写不敏感子串／generational|fold generation|folding generation 短语／api/plugin 内 Product／退役词；**无基线/分类/棘轮/镜像豁免**，命中即红）＋负例自测＋**删除旧 2377 行分类基线**＋lint/CI 接线；architecture-guardrails 新增「代码词汇对齐 GLOSSARY 规范词（机械门）」requirement。
- **03 structural-golden**（实现子域，已完成）：结构/golden 对齐——checkpointSliceMailbox 重复常量删除（级联验证零生产消费者）、golden window_bounds 检查点义「载体」→checkpoint、SnapshotCheckpoint/RestoreCheckpoint 方法名评估保留。

## Capabilities

spec 已下沉至二级 `changes/<NN-domain>/specs/`；一级仅留 `specs/orchestration/`。**全变更只一处能力 delta：`architecture-guardrails` MODIFIED**（02 持有）。**不新建 `code-vocabulary` 能力**——规范词汇已各有其主（session-checkpoint-material／deterministic-compress-level／fold-provenance／restart-recovery-vocabulary／fact-vocabulary），新建即重复＝用户所要防止的词汇增殖。01/03 为纯实现子域，满足该 requirement＋既有机制 spec，无 spec 文本。甲把 carrier 统一到**既有**精确词，是统一相似概念词汇，非造新概念。

## Impact

- 基线：main HEAD `a0ff631b`（文档修复已推、CI 绿）。
- 症状锚：词汇门第一天红着（当前树 **390 命中**＝341 .go＋49 docs/api 镜像，全 carrier），全退役＋godoc 重生成后绿（0）；F3 验红转绿＋**仅回退清理**门复红（因果）；负例自测证明门真拒（非空转）。
- 门宿主：新建检查器成立（无既有门覆盖此轴），**纯短语禁**（carrier 全退役后无合法子串命中，门简单恒判定），归 architecture-guardrails「以结构而非纪律承载正确性」。
- 检测精度（D6）：纯短语禁——carrier 大小写不敏感子串（动词 carries/carried/carrying 不含子串故放行）、generational/fold generation/folding generation 短语禁（gen:/instance_id/recorder generation 合法形不匹配禁式）、Product 范围限 api/plugin（compress.product 事实族不扫、小写 product 散文不拒）、退役词短语禁。注释短语跨行→门先归并注释散文再匹配，单行只计一次。**无豁免/分类**。
- 规模：3 子域；执行轨 C（门重写）→红锚捕获→A（生产 .go 97 命中/24 文件）/B1（测试 113/4 文件）/B2（测试 131/17 文件）并发→D（golden/wiki/GLOSSARY 漂移）→E（godoc 重生成）。**净负代码**（门 550→266、自测 480→215、基线 2377→0，改名不增行）。
- 约束：外来未跟踪文件零触碰；冻结契约拼写不更名；冻结归档只附勘误；doc-budget 只降不升；**不提交/不推送/不归档**（A3 发布授权为法定停点，待用户明示）。