# evals-harness Delta

## MODIFIED Requirements

### Requirement: 共享 Host 多会话真实重启长程矩阵

长程模拟 SHALL 在同一个 Host 上运行 4 个会话，每会话 300 个不同业务输入（1200 输入/组合）；每会话 SHALL 轮流安排 30 个重启触发点（合计 120 次真实子进程重启/组合），每次重启 MUST 恢复全部四会话。矩阵 SHALL 为 seed 1/7/42 × 回放窗口 256/32768 共 6 组合，合计 7200 个业务输入与 720 次真实重启。初次启动 MUST NOT 计为重启，一次重启 MUST NOT 按四会话计数。窗口语义 SHALL 为回放 MaxTokens（按每 token 四字符、阈值 0.3），断言 MUST 取实际触发结果。冥想消费者 MUST 保持启用；持久化强度与 fsync MUST NOT 被削弱。完整矩阵 SHALL 在 merge_group 的 merge ref 上运行并通过后方可进入 main；在外部 merge queue/required checks 尚未启用时，既有 PR 全量 MUST 保持不变。MUST NOT 用 PR head 证据、main push 或 nightly 替代 merge-ref 证明。

#### Scenario: 四会话同一 Host 轮转重启
- **WHEN** 一个组合运行到结束
- **THEN** 真实子进程重启计数为 120，每次重启后四个会话均被恢复，输入身份总数为 1200

#### Scenario: 六组合 merge 前证明
- **WHEN** 完整长程矩阵在 merge_group 运行
- **THEN** 六个组合全部出现且各自 count=1，输入 7200、重启 720；缺任一组合 MUST 阻断 merge

#### Scenario: 外部门禁未启用
- **WHEN** 仓库尚无已启用的 merge queue 或 required checks
- **THEN** 不得从 PR 移除完整矩阵，也不得宣称 merge 前证明已经迁移

### Requirement: 事件证据分片完整性

short 的每份证据 SHALL 与合同 event-short 的父测试集合完全相等；bundle SHALL 恰含 10 个唯一 repeat index、run_id 和日志。long 与 mutation SHALL 与 short 使用同一受测基线；缺号、重号、缺片、共同缩片或跨基线证据 MUST 具名拒收。

#### Scenario: 所有分片一致缩片
- **WHEN** 十个 short 分片都合法地只含同一个父测试
- **THEN** bundle MUST 拒绝，即使各分片 test_ids 彼此相同

#### Scenario: mutation 来自旧基线
- **WHEN** runner 与 mutation 证据的源码或合同指纹不同
- **THEN** bundle MUST 拒绝，不得把历史 mutation 结果当作当前 merge 证明
