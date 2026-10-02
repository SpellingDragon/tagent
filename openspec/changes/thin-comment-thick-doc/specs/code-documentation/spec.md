## ADDED Requirements

### Requirement: 源文件职责索引

每个非测试 Go 源文件 SHALL 在其文件级 doc 槽位（package 子句之前）以一行索引声明所辖职责（`// 契约: <docs/** 路径>` 或 `// 规格: <docs/** 路径>`），索引目标 MUST 满足既有「文档索引语法与目标限制」「索引必须落到具体章节」的全部约束。`scripts/` 下的门禁自用工具与 `_test.go`（由测试侧声明义务管辖）SHALL NOT 适用本要求。一个文件声明多行索引时，MUST 各自指向真实小节；无索引的生产文件即 `missing-file-responsibility` finding。

#### Scenario: 无索引的生产文件被拦截

- **WHEN** 扫描器遍历两模块全部非测试 `.go` 文件（`scripts/` 除外），某文件不含任何 `契约:/规格:` 索引行
- **THEN** 该文件 SHALL 计一条 `missing-file-responsibility` finding，消息指明「先在 docs/wiki 建立或找到所辖小节，再落一行索引」

#### Scenario: 索引指向不存在的小节

- **WHEN** 文件补了索引但目标文件或锚点不存在
- **THEN** 既有 `index-target-missing`/`index-anchor-unknown` 零容忍硬门 SHALL 直接失败——文档先行的倒逼闭环不得被空指针绕过

#### Scenario: 测试文件与门禁脚本不适用

- **WHEN** 文件是 `_test.go` 或位于 `scripts/`
- **THEN** 本要求 SHALL NOT 计 finding（测试侧由 `missing-test-responsibility` 管辖，门禁自用工具无 wiki 家园）

## MODIFIED Requirements

### Requirement: go doc 注释的契约边界

doc 注释 SHALL 以所依附标识符名开头，形态限于**简要职责**：至多两个散文段落（是什么、调用方可见的核心语义），段落之间可穿插 `- ` 要点行与 `契约:/规格:` 索引行；第三个散文段落即 `doc-not-brief` finding——输入输出细节、前置/后置条件、失败与幂等语义、并发安全性、默认值与不保证事项 SHALL 迁入 `docs/wiki` 对应小节，doc 内以要点行或索引行指向。物理换行不计深度（Go 作者硬换行成句），叙述段落数才是深度度量。判定准则：换一种实现方式该句仍成立即为职责句；依赖实现步骤或配置细节的句子属于文档，不属于 doc。

#### Scenario: 每个包与导出符号有文档

- **WHEN** 扫描器遍历两模块全部包与导出符号
- **THEN** 每个包 SHALL 有 package 注释、每个导出符号 SHALL 有 doc 注释；缺失即门禁失败

#### Scenario: 第三个散文段落被判厚

- **WHEN** 某声明的 doc 含三个及以上散文段落（物理换行合并计数，bullet 与索引行除外）
- **THEN** 该声明 SHALL 计一条 `doc-not-brief` finding，消息指明「迁文优先：细节先落 docs/wiki 对应小节，再削薄 doc 至职责段落 + 要点」

#### Scenario: 要点行与索引行不受限

- **WHEN** doc 由至多两段职责散文加任意数量 `- ` 要点行与索引行构成，或单句因硬换行跨越多个物理行
- **THEN** 门禁 SHALL 不计厚度 finding——可扫读的要点列表与换行不是厚注释，第三个叙述段落才是

#### Scenario: 削薄不得丢契约

- **WHEN** 一次削薄动作删除了默认值、失败语义或并发安全性的叙述
- **THEN** 该内容 SHALL 已先迁入 wiki 对应小节（`wiki-code-sync` 各门把关）；只删不迁的批次 SHALL 被评审拒绝
