# orchestration（一级编排契约）

## ADDED Requirements

### Requirement: 启动门

本变更的 W1 SHALL 满足三项启动门后才可派发：

1. 症状直译锚测试存在且当前为红（慢工具未返回时第二条消息得到回答；第一条回答闭合后第二条不在已完成历史）；
2. `ORCHESTRATION.md` 金字塔四层齐备（结论 / 编排四问 / 明细 / 台账）；
3. 跨域签名已冻结并写入 `design.md#D6`（消费批次、消费关系、会话消费者启动签名）。

#### Scenario: 红锚缺失不得开工
- **WHEN** 症状直译锚不存在或当前为绿
- **THEN** W1 SHALL NOT 派发，先补锚并取得红证据

#### Scenario: 签名未冻结不得开工
- **WHEN** `design.md#D6` 的结构未冻结
- **THEN** 任何消费循环改动 SHALL NOT 派发

### Requirement: 波次与解锁

波次 SHALL 只是汇报分组；域解锁 SHALL 以依赖已入 main 为准，MUST NOT 以「上一波汇报完成」充当解锁。

#### Scenario: 依赖未入 main 不解锁
- **WHEN** 01 域仍在评审而非 main
- **THEN** 02/03/04/05 SHALL NOT 开工

#### Scenario: 可并行域
- **WHEN** 01 已入 main
- **THEN** 02 与 03 的触碰集不相交，SHALL 可并行派发；04 等待 01 与 03 同时入 main

### Requirement: 共享面与账目

`api/event`、`assembly/facts.go` 等共享面 SHALL 由编排者单写；每个域的写入白名单 SHALL 精确到文件；每单完成 SHALL 回填域 checkbox 与本级子变更 checkbox。

#### Scenario: 白名单外触碰即拒
- **WHEN** 某单改动落在其白名单之外的文件
- **THEN** 该单 SHALL 被拒绝并返工，MUST NOT 以「顺带修复」合并

#### Scenario: 账目落后即返工
- **WHEN** 代码已提交而对应任务行未勾选
- **THEN** 记账人 SHALL 在同一提交内同步勾选

### Requirement: 验收与放行

变更级 DoD SHALL 包含症状直译锚双证据（修复后绿 + 回滚必红）、全量门禁、契约清单、独立审者 frontier 空放行。

#### Scenario: 无回滚证据不放行
- **WHEN** 症状锚只有修复后绿、没有回滚必红证据
- **THEN** SHALL NOT 放行，补齐回滚证据

#### Scenario: 独立审者未空不放行
- **WHEN** 独立只读审者仍有未关闭项
- **THEN** SHALL NOT 归档
