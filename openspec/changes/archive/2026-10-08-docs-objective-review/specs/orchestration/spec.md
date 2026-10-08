# docs-review-orchestration Specification

本规格仅定义过程编排。下述“评审/探索”条款限定第一/二阶段；第三阶段产品行为以O1–O6下沉spec为准。当前修订不代表已实施或已通过真实验收。

## ADDED Requirements

### Requirement: 第二阶段探索期权限边界

代码探索代理 SHALL 遵守深读+现有验证档：允许 Read/Grep/Glob 与现有只读验证命令（go build / go vet / go test 指定包指定测试），SHALL NOT 写/改任何源码、测试、配置与既有文档，SHALL NOT 新建 spike/benchmark 代码，SHALL NOT 执行任何 git 写操作。探索代理写权限 SHALL 限定为其报告文件。

#### Scenario: 命令账可稽

- **WHEN** 探索代理运行任何验证命令
- **THEN** 报告〇节记录命令全文与退出码
- **AND** 命令失败不阻断探索，失败本身作为证据入账

### Requirement: 假设核验契约

每份探索报告 SHALL 含假设核验表（≥5 行），逐条给出第一阶段报告的结论、本次代码证据、判定（成立/不成立/证据不足）；判定为不成立或证据不足时 SHALL 说明以何推翻、还缺何证据。探索报告对第一阶段结论有推翻权，但 SHALL NOT 修改第一阶段任何报告文件。

#### Scenario: 推翻可溯源

- **WHEN** 探索汇总报告引用某条被推翻的第一阶段结论
- **THEN** 可回溯到 X 域报告的核验行（双方证据均在）

### Requirement: 探索波次与前置

X1-X5 五域 SHALL 在 W3a 并发执行（互无产物依赖）；X6 收敛探索 SHALL 前置于其孙任务中标注对 X1-X5 报告文件的依赖（孙任务级前置，非整波栅栏）；编排者 SHALL 在 X6 派发前完成对 X1-X5 报告的四查。

#### Scenario: 前置不满足即缓派

- **WHEN** 任一 X1-X5 报告未落盘或四查不过
- **THEN** X6 不派发，先按重派协议补齐

### Requirement: 只读边界守护（仅第一与第二阶段）

评审全程 SHALL NOT 修改以下路径的任何文件：源码（`*.go`）、`docs/wiki/**`、`docs/api/**`、`README.md`、`README_EN.md`、`openspec/specs/**`、`examples/**`。评审代理的写权限 SHALL 限定为：本 change 目录内文件与 `docs/.dev/20261007-wiki-review-*.md`。

#### Scenario: 收尾契约断言

- **WHEN** 全部域评审完成
- **THEN** `git status --porcelain -- '*.go' docs/wiki docs/api README.md README_EN.md openspec/specs examples` 输出为空
- **AND** 任何域代理试图写上述路径时必须被其任务指令中的写白名单阻止

### Requirement: 六维评审契约

每份域报告 SHALL 按统一框架包含六个评价维度的独立章节（预期特性 / 架构设计 / 过度设计嫌疑 / 缺陷设计嫌疑 / 推理友好性 / 训练友好性），每维 SHALL 给出 A/B/C/D 档评分与置信度（高/中/低），低置信度 SHALL 说明缺失的证据。每份域报告 SHALL 含"本域最尖锐的三个问题"小节与至少 2 个带 `文件:符号` 级佐证的代码抽查断言。

#### Scenario: 域报告完整性核验

- **WHEN** 编排者对任一域报告执行四查
- **THEN** 六维章节锚点、评分行、尖锐问题小节、代码佐证引用均可 grep 命中
- **AND** 缺任一要素即判定该域评审不通过、重派补齐

### Requirement: 派发前置资源审计

编排者在派发每个并发评审代理前 SHALL 完成资源划拨审计并随任务声明：写文件路径唯一归属（各域报告文件互不相同）、只读范围（源码抽查不锁文件）、无账号/设备/网络竞争。并发规模 SHALL 声明为 6。

#### Scenario: 并发互不踩踏

- **WHEN** 六个域代理并发执行
- **THEN** 各代理只写各自的 `docs/.dev/20261007-wiki-review-D<N>-*.md`
- **AND** 不存在两个代理写同一文件的路径冲突

### Requirement: 子代理完成协议

域代理返回时 SHALL 提供：报告绝对路径、六维评分一览（每维一档+置信度）、本域最尖锐三个问题的一句话版、README 特性承诺在本域的兑现判定。编排者 SHALL 对返回执行四查（勾选真实性 / 产物盘点 / 数字与断言溯源 / 遗漏检测），空返或四查不过 SHALL 重派而非由编排者代写。

#### Scenario: 空返处置

- **WHEN** 域代理返回体不含报告路径或报告文件不存在/为占位
- **THEN** 判定未完成，重派时指令改为"核查既有工作区产物并补跑缺失步骤"

### Requirement: 汇总与 DoD

编排者 SHALL 交叉引用六份域报告产出总报告 `docs/.dev/20261007-wiki-review-summary.md`，总报告 SHALL 覆盖：预期特性全貌、架构设计总评、过度设计清单、缺陷设计清单、推理友好性结论、训练友好性结论，每个跨域结论 SHALL 标注来源域报告。完成对话呈现后 SHALL 执行归档流程。

#### Scenario: DoD 逐项可勾选

- **WHEN** 变更收尾
- **THEN** F1–F4 每项均有可核验记录；归档未实际执行不能冒称archive完成

### Requirement: 第三阶段授权及哲学启动门
第三阶段 SHALL 由后续用户apply启动；当前修订仅改计划。实施 MUST 保持同构自治、唯一发布权、FP/SRC/FILE/RESTART、提交后投影及可选采集；MUST NOT 将旧报告“无索引即无法join、默认noop缺陷、随机性不可训练”等判断直接转为需求。

#### Scenario: 计划存在但未实施
- **WHEN** O1–O6四件套已生成而用户尚未启动apply
- **THEN** 全部第三阶段checkbox保持未勾，不运行模型测试或修改产品代码

### Requirement: 三级任务与依赖解锁
一级 SHALL 仅维护六域及集成验收；O1–O6各自tasks承载单机制叶任务。O1.6 MUST 等O4.5；O5.2 MUST 等O3.2；O5.5 MUST 等O1.6/O2.6/O3.6及真实Runner钉测；O6.6 MUST 等O5.6。fixture只能解锁独立开发，不能替代真实集成。

#### Scenario: 单个依赖尚未进入集成树
- **WHEN** 某域声明已完成但消费者所需接口未集成或未验证
- **THEN** 相应消费者叶任务保持阻塞，其他独立任务可以继续

### Requirement: 单写归属与资源等待表
执行 SHALL 遵守design D15文件归属；root/config/context/metadata/长期文档由编排者单写。性能基准和真实模型分别占用独占资源槽，释放立即消费等待表。MUST NOT 并发操作用户默认tmux服务器。

#### Scenario: 插件归因与事实闸共享文件
- **WHEN** O1和O5同时需要修改MemoryPlugin
- **THEN** O1先交付事实闸，O5仅提供接线清单，编排者在冻结版本上集成，不出现并发覆盖

### Requirement: 必需的本地真实验收
第三阶段完成 SHALL 包含真实模型的RuntimeOverrides/RequestBudget/DecisionCapture/OfflineSFT用例以及本地真实tokenizer样本验证；认证、端点、模型、tokenizer缺失或用例Skip/零匹配/零调用 MUST 判未完成。默认CI可跳过无凭据live组，但不能替本阶段F11达标。

#### Scenario: CI绿而本地live组未跑
- **WHEN** short/mock全过但真实模型测试被Skip
- **THEN** F11未通过，第三阶段不能宣称完成

### Requirement: 性能主张与归因证据
性能主张 SHALL 使用同条件before/after、真实库存规模、冷热口径和完整退出码；不能用历史报告、标题计数或静态调用数冒充测速。负结果 MUST 保留；不达编码缓存收益门时撤候选，不降低耐久保证。

#### Scenario: 快照缓存负收益
- **WHEN** O4基准未达规定改善/退化/内存边界
- **THEN** 保留定向扫描及回归，缓存候选退出并记录理由，不宣称全路径加速

### Requirement: 真实产物与规格收口
F9–F13 SHALL 以实际diff、测试、run manifest、样本与文档一致性核验；第三阶段产品delta SHALL 经用户授权后无损提升为独立change并正常archive。MUST NOT 删除delta或使用skip-specs/no-validate绕过同步，父过程spec不得污染产品规格。

#### Scenario: 归档未授权
- **WHEN** 实施验收已通过而用户未批准归档
- **THEN** 交付状态注明验收完成、F14待授权，不伪勾或擅自修改主规格
