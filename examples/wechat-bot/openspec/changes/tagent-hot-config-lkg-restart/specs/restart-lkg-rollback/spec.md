## Purpose

让 wechat-bot 的自替换换装流程具备 last-known-good（LKG）语义：换装前归档当前可用二进制，新二进制健康探测失败时自动回滚并重启，全程留档且与保险链 done-sentinel 握手兼容。

## ADDED Requirements

### Requirement: 换装前 LKG 归档

换装流程 SHALL 在替换二进制之前将当前二进制归档为 LKG（last-known-good）副本。当归档目标与当前二进制内容一致时 SHALL 跳过写入以避免无谓写放大。LKG SHALL 在换装成功后保留（作为下一轮换装的回滚目标）。

#### Scenario: 换装前归档当前二进制

- **WHEN** 换装流程进入二进制替换阶段且当前运行二进制与既有 LKG 副本内容不同
- **THEN** 当前二进制被完整复制为 LKG 副本，随后才执行替换

#### Scenario: LKG 与当前一致时跳过归档

- **WHEN** 既有 LKG 副本与当前二进制内容一致
- **THEN** 归档步骤跳过写入，流程继续

### Requirement: 换装后健康探测

换装流程 SHALL 在启动新二进制后对既有 healthz 端点做限期探测：在限定窗口（不超过 30 秒）内获得连续 3 次成功响应（HTTP 200 且响应体含正常状态标志）即判定换装成功；窗口耗尽或新进程死亡即判定失败并进入回滚。

#### Scenario: 新二进制健康驻留

- **WHEN** 换装启动的新二进制在探测窗口内返回连续 3 次健康响应
- **THEN** 换装判定成功，done-sentinel 按既有协议落档，LKG 保留

#### Scenario: 新二进制起不来触发回滚

- **WHEN** 换装启动的新二进制在探测期内死亡或始终未通过探测
- **THEN** 流程进入自动回滚

### Requirement: 探测失败自动回滚

探测失败时流程 SHALL 自动执行回滚：终止新进程（若存活）、将 LKG 恢复为运行二进制、以归档的环境快照重新拉起、并对回滚后的进程做确认探测。回滚成功 SHALL 写 done-sentinel（内容与成功路径同构）；回滚失败（LKG 也无法启动）SHALL 记 FATAL 且不写 done-sentinel，交由保险链或人工处置。

#### Scenario: 自动回滚到 LKG 并恢复服务

- **WHEN** 新二进制探测失败且回滚执行成功
- **THEN** 运行二进制恢复为 LKG 版本，服务经重新拉起后健康响应正常，done-sentinel 落档且指向回滚后进程

#### Scenario: LKG 也无法启动

- **WHEN** 回滚后 LKG 进程仍无法通过确认探测
- **THEN** 流程记录 FATAL 并终止，不写 done-sentinel，保险链或人工介入

### Requirement: 回滚事件通报

回滚发生时换装流程 SHALL 产出转世通报，其触发原因字段标注回滚语义（rollback 标记 + 具体原因），格式与既有通报消费方兼容；通报与回滚过程 SHALL 全程记录于换装日志。

#### Scenario: 回滚通报携带原因

- **WHEN** 因健康探测超时而执行回滚
- **THEN** 转世通报的原因字段含 rollback 标记与超时原因，通报文件按既有格式与路径落盘

#### Scenario: 日志可回放回滚全程

- **WHEN** 一次完整回滚发生
- **THEN** 换装日志依时间序呈现"探测失败 → 回滚开始 → LKG 恢复 → 重新拉起 → 回滚结果"的完整链路
