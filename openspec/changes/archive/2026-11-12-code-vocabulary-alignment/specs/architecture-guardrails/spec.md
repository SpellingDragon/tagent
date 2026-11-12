# architecture-guardrails Specification

## ADDED Requirements

### Requirement: 代码词汇对齐 GLOSSARY 规范词（机械门）

代码标识符与注释 SHALL 以词汇表登记的规范词指称机制，MUST NOT 用已退役词或规范词的同义旧词。carrier 系多义退役词，MUST NOT 在任何义项用作项目词汇：检查点机制 SHALL 称 checkpoint，提交承载 SHALL 称 commit／DurableCommit，凭据 SHALL 称 credential，URL 载荷 SHALL 称 URL，查询载荷 SHALL 称 projection／payload，各按词汇表既有规范词指称，不造新概念；压缩材料晋升 SHALL 称 band 晋升，MUST NOT 称 generational／fold generation／folding generation（记录世代 instance_id 与折叠行 gen: 锚以规范词表达，合法保留）；插件材料声明面 SHALL 以统一 Material 契约表达，MUST NOT 复活已删除的 Material×Product 矩阵（含复合物标识符）；退役词 读透视图／链权威答案 MUST NOT 用作项目词汇。

词汇对齐 SHALL 以机械门承载而非纪律：门为纯短语禁，命中即红，无基线／分类／豁免／棘轮／镜像。carrier SHALL 以大小写不敏感子串匹配（覆盖驼峰复合物标识符），动词 carries／carried／carrying 不含该子串自然不拒；禁用短语 SHALL 逐行扫标识符与字符串字面量（含驼峰形 foldGeneration／generationalBand），注释短语跨续行 SHALL 先归并散文再匹配且单行命中只计一次（不重复计数），MUST NOT 因跨行或标识符形漏检；插件 Product SHALL 以大小写敏感子串匹配复合物标识符，范围限插件材料声明面，小写 product 散文不拒。门 SHALL 有负例自测证明真拒植入违规（只打印不拒判空转），SHALL 接进 lint 与 CI 作正式消费者（门红则 CI 红）。冻结契约拼写与冻结归档 MUST NOT 随更名改写（归档只附勘误）。

#### Scenario: carrier 全义退役被拒

- **WHEN** 代码注释或标识符在任何义项用 carrier（检查点义「framework carrier」、提交承载 commitCarrier／newCarrier、凭据「credential carrier」、URL「URL carrier」、查询载荷「query carriers」）
- **THEN** 每处出现即门红（exit 1）具名 file:line，无豁免；动词 carries／carried／carrying 不含 carrier 子串不拒

#### Scenario: 压缩 fold generation 被拒、合法世代不触禁式

- **WHEN** 压缩晋升叙述称 generational／fold generation／folding generation（含标识符驼峰形 foldGeneration／generationalBand、字符串字面量、跨注释续行），或折叠行以 gen: 锚／instance_id／recorder generation 规范词携带记录世代
- **THEN** 压缩世代短语门红；记录世代以规范词（gen: 锚／instance_id／recorder generation）表达者不触禁用短语检测（非禁用形，自然不拒，无需基线豁免）

#### Scenario: 负例自测真拒

- **WHEN** 运行 `python3 scripts/gates/test_check_vocabulary_alignment.py`，自测向临时 fixture 植入 carrier 标识符 commitCarrier、carrier 散文、跨行 folding generation、单行 folding generation、标识符 foldGeneration／generationalBand、字符串字面量 folding generation、api/plugin 的 Product 与复合物 MaterialProduct／pluginProduct、退役词
- **THEN** 门对每类植入违规 exit 1（单行短语只计一次，不重复计数）；对 gen: 锚／instance_id／recorder generation 标识符／compress.product 事实族／api/plugin 小写 product 散文／动词 carries/carried/carrying 每类合法不拒；自测断言拒收逻辑，不勾选产品验收

#### Scenario: 正式消费者接 lint 与 CI

- **WHEN** 运行 `bash scripts/gates/lint.sh --only policy` 或 CI 的 Lint step
- **THEN** 词汇门 `check_vocabulary_alignment.py` 实跑（test+check 对）；门红则 lint 与 CI 红，非仅本地脚本

#### Scenario: 纯短语禁无基线

- **WHEN** 某变更引入任何 carrier 子串或禁用短语，或修复一处既有命中
- **THEN** 新命中即红（exit 1），无基线登记／豁免／棘轮；修复后命中即绿（exit 0），无 stale 条目／--write-baseline；门不读不写任何分类基线文件

#### Scenario: 冻结拼写与归档不改写

- **WHEN** 词汇更名涉及冻结契约拼写（reincarnation 族、两个冻结测试名）或冻结归档引用的旧测试名
- **THEN** 冻结拼写逐字不变（冻结契约面与冻结文件对基线 git diff 为空）；冻结归档原文不改写，仅追加勘误行指向新真名＋改名提交