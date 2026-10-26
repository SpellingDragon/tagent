## MODIFIED Requirements

### Requirement: 水位执行语义与让位补位

冥想水位 SHALL 在冥想 turn 被消费时推进（注入时刻为水位值），MUST NOT 在注入时推进；混合批让位 SHALL 记为推迟（水位不动、pending 清零、下个 interval 重试）；pending SHALL 防重入，长期未决（超 3×interval）只 SHALL 出 WARN 观察线而 MUST NOT 自动复位重投（回合时长无上界，时间复位=重投踩踏；兜底取向 fail-safe 停摆）；lastMeditation 为零（首次/锚缺失）时节奏门 SHALL 直通。

「被消费」MUST 仅指冥想回合进入成功完成终态后回写 `ReflectionConsumed`：回合以失败、取消、错误或任何未达 `completed` 的终态结束时，该次注入 SHALL 记为推迟（`ReflectionDeferred` 语义），水位 MUST NOT 推进、pending SHALL 清零，下一次可执行 tick SHALL 对同一事实面重新评估。驱动在上一 tick 尚未结束时收到的观察 SHALL 保留给下一次评估：MUST NOT 推进水位、MUST NOT 计为已消费，且下一次可执行 tick SHALL 仍能覆盖该观察所属事实；MUST NOT 以「驱动忙」为由丢弃该材料或让其老于水位。推迟与失败 MUST NOT 被日志、回执或状态记成已消化。

#### Scenario: 让位不烧窗口

- **WHEN** 注入的冥想事件与真实事件同批被让位丢弃，随后观察面安静
- **THEN** 水位未推进，下个 tick 对同一事实面再次评估并可触发，覆盖完整

#### Scenario: 冷启动直通

- **WHEN** 属主从未执行过冥想且观察面有非自管新事实
- **THEN** 首个 tick 触发一次通读，水位自锁

#### Scenario: 未完成终态记为推迟（失败）

- **WHEN** 冥想回合已注入并启动，随后以失败、取消或任何未达 `completed` 的终态结束
- **THEN** 水位 MUST 保持注入前的值，pending SHALL 清零
- **AND** 下一次可执行 tick MUST 重新评估同一事实面，该批材料 MUST NOT 被视为已消化

#### Scenario: 忙时观察不推进水位（并发）

- **WHEN** 驱动上一 tick 仍在执行时到达一条新材料观察
- **THEN** 该观察 MUST NOT 推进水位、MUST NOT 计为已消费
- **AND** 下一次可执行 tick MUST 仍在扫描窗口内看到该材料，不得因忙而漏评估
