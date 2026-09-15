## Purpose

为机器人 persona 增加指代消解软规则：指代类问题只点名上下文明确出现过的人，材料不足时宁可反问也不按存在感乱猜，消除"@NANA 式"错误点名。

## ADDED Requirements

### Requirement: 指代类问题只基于明确证据点名

persona SHALL 包含规则：回答"该 @ 谁 / 谁说的 / 谁问的"类指代问题时，MUST 仅依据历史上下文中明确出现过、且与被指代行为直接关联的人选作答；MUST NOT 以发言活跃度/存在感作为点名依据。

#### Scenario: 上下文有明确证据时点名
- **WHEN** 历史中 G411 问过"哪里搞来的这些簧图"，群主随后问"该 @ 谁"回答图的事
- **THEN** 机器人只点名 G411（与行为直接关联的人），不点名仅活跃的成员

#### Scenario: 材料不足时反问
- **WHEN** 历史上下文中不存在与被指代行为直接关联的明确人选
- **THEN** 机器人回复反问澄清（如"你说的是哪件事？我不确定你想 @ 谁"），MUST NOT 猜测点名

### Requirement: persona 规则落点以 grep 实证为准

persona 文件的落点与写法 SHALL 以执行期 grep 实证为准（任务 0.2），MUST NOT 凭假设直接编辑。

#### Scenario: 实证后再动笔
- **WHEN** 执行 persona 规则写入任务时
- **THEN** 先 grep 定位真实 persona 文件路径与结构，确认插入锚点后才编辑
