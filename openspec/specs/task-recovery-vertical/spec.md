# task-recovery-vertical Specification

## Purpose

任务在线执行与重启恢复共用一份有类型的 apply 解释，纵向样板覆盖 checkpoint 与尾部事实；审批裁决写失败不改变可见状态；损坏或不可读切片具名拒绝而非按空状态启动。

## Requirements

### Requirement: 任务在线与恢复共用一份解释

任务的状态、修订、轮次、通知义务与收据 SHALL 由同一有类型应用规则解释；在线提交与恢复回放 MUST NOT 各自维护一份业务推导。进程内移动时间戳与唤醒 SHALL 留在应用规则之外，恢复态按“未知时间”语义表达。

#### Scenario: 同一提交序列两种路径

- **WHEN** 同一任务的提交序列分别经在线路径与重启恢复
- **THEN** 状态、修订、轮次、义务与收据 SHALL 逐项一致
- **AND** 未持久化的移动时间戳 SHALL 不参与相等判定

### Requirement: 纵向样板覆盖 checkpoint 与尾部

任务—通知—checkpoint—邮箱 SHALL 有一个真实纵向样板：产生任务与待投递通知，切面后再提交新事实，重启后未决工作与事实一致，已完成工作不重新打开。任务账户 SHALL 在邮箱恢复前初始化；该依赖 SHALL 由拥有者关系表达，不由协调者解析业务载荷。

#### Scenario: 切面后尾部改变未决

- **WHEN** checkpoint 之后提交了新的消费收据或任务状态
- **THEN** 重启后的未决集合 SHALL 按尾部事实修正
- **AND** 已确认完成的投递 SHALL NOT 再次欠账

#### Scenario: 缺任务切片

- **WHEN** 最近 checkpoint 没有任务切片
- **THEN** 恢复 SHALL 读全任务链或按尾增量授权重建
- **AND** MUST NOT 以空账户启动

### Requirement: 审批裁决失败不改变可见状态

审批裁决 SHALL 先提交裁决事实，成功后再更新内存状态。提交未成功时 SHALL 区分“未提交”“结果不确定”“事实已提交但镜像失败”三态；任一态 MUST NOT 释放闸门，MUST NOT 在快照或重启后丢失未决项。快照筛选与运行期查询 SHALL 读取同一份已提交裁决。

#### Scenario: 回答写失败

- **WHEN** 审批回答的写入或屏障失败
- **THEN** 运行期查询与快照 SHALL 仍把该请求视为未决且未放行
- **AND** 重启后该请求 SHALL 仍可见，除非裁决事实确认已提交

#### Scenario: 存储结果不确定

- **WHEN** 存储返回结果不确定
- **THEN** 系统 SHALL 重新读取判定，MUST NOT 直接按“无请求”或“已裁决”处理

### Requirement: 损坏或不可读切片具名拒绝

可恢复消费者遇到损坏或不可读切片 SHALL 具名拒绝 boot，MUST NOT 静默按空状态继续。历史裁决与收据 SHALL 仍按身份可读；读失败 SHALL 具名上返。

#### Scenario: 坏切片

- **WHEN** 切片载荷无法解析
- **THEN** boot SHALL 拒绝并指出切片名与消费者
