# trajectory-verification Specification

## Purpose
TBD - created by archiving change hardening-review-batch2. Update Purpose after archive.
## Requirements
### Requirement: 重启点身份配对
前缀验证器 MUST 以 (agent, session) 身份配对重启前后记录，并以同身份死亡前**最后一条**请求作为 Before 锚点；MUST NOT 向前搜索更早的请求来获得匹配。

#### Scenario: 存在更早的易匹配记录
- **WHEN** 同会话中较早存在一条 10 条消息的请求、死亡前最后一条为 100 条消息的请求，恢复后仅有 30 条
- **THEN** 验证器 MUST 以 100 条请求为锚判定 CRITICAL（丢失 70 条），MUST NOT 配对早前 10 条记录判为 EXACT

### Requirement: 截尾即失败
恢复请求是死亡前请求的真前缀但更短时，MUST 判定 CRITICAL 并记录丢失条数；任何 CRITICAL 或无法配对（UNPAIRED）非零时，验证器退出码 MUST 非零。

#### Scenario: 截尾输入
- **WHEN** 死亡前 100 条、恢复后为前 90 条
- **THEN** 判定 MUST 为 CRITICAL（丢失 10 条）且退出码为 1，MUST NOT 崩溃或输出 EXACT

### Requirement: 动态系统段单列
看板行等系统注入段 MUST 单列为 BOARD_DIFF 类别比较：不计为 EXACT、不计为 CRITICAL、MUST NOT 通过删除该段后仍宣称「全上下文逐字节相等」。

#### Scenario: 仅看板段差异
- **WHEN** 前缀完整、差异仅出现在系统注入的看板行
- **THEN** 判定 MUST 为 BOARD_DIFF（带差异坐标），总结论 MUST 明示「排除动态系统段后的相等」

### Requirement: 验收失败必须可见
验证器 MUST 在无法配对、缺记录、截尾时输出明确失败分类与非零退出码；MUST NOT 把未分类记录默认计入通过。

#### Scenario: 无可配对 Before 记录
- **WHEN** 重启点前找不到同身份的死亡前请求
- **THEN** 判定 MUST 为 UNPAIRED 并计入失败汇总，退出码非零

