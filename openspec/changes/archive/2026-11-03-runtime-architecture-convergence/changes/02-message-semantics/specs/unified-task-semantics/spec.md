# unified-task-semantics Delta

## MODIFIED Requirements

### Requirement: 通知接纳与消费确认分离

通知 SHALL 先进入稳定身份的持久接纳记录；Accept/Claim、运输接收或发送动作 MUST NOT 被当作逻辑消费完成。只有目标地址的消费结果与连续消费位置已持久记录后，通知才结清。completed/failed/cancelled 均可构成已处理但结果不同的消费结果；进程停止、存储失败或尚未形成消费结果时 SHALL 保持 owed 并可恢复。外发义务 SHALL 独立记录，其失败不得重开已消费通知。

#### Scenario: 无消费结果不结清
- **WHEN** 通知已接纳或材料已提交，但目标尚未持久记录消费结果
- **THEN** 通知保持 owed，恢复后可继续，且不得重复已提交材料

#### Scenario: 失败结果也是已处理
- **WHEN** 目标地址已持久记录该通知对应的 failed/cancelled 消费结果与连续位置
- **THEN** 通知按已处理结清，不默认无限重放；结果必须可查且不得冒充成功

#### Scenario: 重复恢复幂等
- **WHEN** 进程在消费结果或结清完成后再次重启
- **THEN** 同身份通知不产生第二次有效处理；只补齐尚未完成的结清或独立外发义务

### Requirement: 投递元数据契约同构

agent 主动投递与框架状态变更投递 SHALL 是两类不同事实（类型保留区分），但两路在原始事实提交时 SHALL 写同构的目标、投递身份、回执与来源键（键走 MetaKey* 单点定义），去重与回执规则一致。读投影 MUST NOT 靠只为其中一路补字段来掩盖新写事实的不同构。

#### Scenario: 两路投递一账可查
- **WHEN** 同一条源事件分别经 send 与经 settle 路径投到同一目标
- **THEN** 原始事实与账目的元数据字段集合均同构，事实类型各自保留，重复身份不产生第二条有效记录
