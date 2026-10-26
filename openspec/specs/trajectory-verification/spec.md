# trajectory-verification Specification

## Purpose
轨迹重启验证器：以 (agent, session) 身份配对重启前后记录，锚定死亡前最后一条请求做保真核对。
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

### Requirement: 真实消费验收失败可见
真实 SFT 消费验收 MUST 在 **labels/mask 不一致、目标样本为空、应拒样本未入拒绝账** 时输出明确分类与非零退出码；缺凭据、模板不支持或 `SKIP` MUST NOT 计为完成，MUST NOT 声称权重训练收益。

#### Scenario: 目标样本为空（失败）
- **WHEN** 导出数据中目标 assistant 既无正文又无 `tool_calls`
- **THEN** 验收 MUST 失败并分类为 `EMPTY_TARGET`，退出码非零。

#### Scenario: mask 不一致（失败）
- **WHEN** labels 与动作/观察 mask 不匹配
- **THEN** MUST 判 `MASK_MISMATCH` 并计入失败汇总，退出码非零。

#### Scenario: 拒绝账遗漏（失败）
- **WHEN** 存在模板不支持或多模态未支持样本，但拒绝账为空
- **THEN** MUST 失败并报告遗漏的拒绝分类，MUST NOT 静默降级为纯文本。

#### Scenario: SKIP 不算通过（失败）
- **WHEN** 消费用例因缺凭据被 `t.Skip`
- **THEN** MUST NOT 计为完成，汇总 SHALL 标注「未验证」并保持非通过。

#### Scenario: 重启后复跑一致（重启）
- **WHEN** 验收器在同一输入上重启后复跑
- **THEN** 分类与退出码 MUST 与首次一致，MUST NOT 因重启翻转结论。

#### Scenario: 并发消费不互相掩盖（并发）
- **WHEN** 多个消费批次并发校验
- **THEN** 各批次分类 MUST 独立汇总，MUST NOT 因并发丢失失败计数。
