## Purpose

用群内相对频率（相对群活跃中位数）识别值得激活的低频群友，替代绝对阈值，且死群停用、新号防御、频率自限。

## ADDED Requirements

### Requirement: 相对频率触发条件

用户尾随 24h 发言数 u MUST 满足 u ≤ 0.5×群中位数 M，且 M MUST ≥3（死群停用）；观察期 MUST 满 24h（first_seen 持久化），lifetime MUST ≥3；四条件全部满足才允许激活。

#### Scenario: 低频群友激活
- **WHEN** 群中位数 M=6，用户 u=2（2 ≤ 3 = 0.5×6），且观察满 24h、lifetime≥3
- **THEN** 该用户发言触发低频激活

#### Scenario: 死群停用
- **WHEN** 群中位数 M=2（< 3）
- **THEN** 该群所有用户都不满足相对频率触发，低频激活停用

#### Scenario: 新号不激活
- **WHEN** 用户 lifetime 计数 < 3
- **THEN** 该用户不满足触发条件，即使其 24h 频率足够低

#### Scenario: 观察期不足不激活
- **WHEN** 用户 first_seen 距今 < 24h
- **THEN** 该用户不满足触发条件（统计未积累）

#### Scenario: 激活自增发言数自然退出
- **WHEN** 用户被激活一次后，机器人回复使该用户后续发言，24h 窗口内计数增长至超过 0.5×M
- **THEN** 该用户自动退出低频激活状态，不再触发

### Requirement: 群级令牌桶兜底

每个群 MUST 配置独立的令牌桶（容量 2、每 4h 回填 1）对激活类 LLM 调用兜底限流；@bot 直接响应不受令牌桶约束。

#### Scenario: 令牌耗尽拒绝激活
- **WHEN** 群 G 在短时间内已消耗 2 个令牌，第 3 次激活请求到达
- **THEN** 该次激活被拒绝，日志记录原因 group token bucket empty

#### Scenario: 令牌回填
- **WHEN** 群 G 令牌耗尽后经过 4h 无消耗
- **THEN** 令牌回填 1，下一次激活请求可被放行

### Requirement: 统计留内存不落盘

滑窗计数、中位数、令牌桶、first_seen 之外的统计性字段 MUST 留在内存，不落盘；重启后统计从零积累。

#### Scenario: 重启后统计重置
- **WHEN** 进程重启
- **THEN** 滑窗/中位数/令牌桶等内存统计清零重新积累，仅持久化字段（白名单、开关、画像、first_seen）从磁盘恢复
