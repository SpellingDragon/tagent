## MODIFIED Requirements

### Requirement: 结算通知以有界票据进入投影并可被折叠回收

批量退役 SHALL 把一轮回收折叠为单条汇总通知，MUST NOT 为每个退役任务各发一条独立通知（嵌套回收不得重复投递）。`[task settled]` 类结算通知 MUST 以有界票据形式进入投影：正文携带可召回的事件键票据与受限预览，其全量体在事实链按票据可取。投影压缩 SHALL 能像对待其它内容一样折叠这些结算票据，消除"折叠不覆盖 external_input 因而其正文结构性不可回收"的死重。本要求只界定新通知的产生形态与可折叠性，MUST NOT 改写通用压缩与回放算法本身。回收／退役外发的结算信号 MUST NOT 继承任务原有的触发谱系，且该谱系戳 MUST 挂在结算信号上传递，MUST NOT 原地改写任务的 Origin（消除并发写与恢复轮污染）。批量退役折叠 MUST NOT 丢弃有 invocation 绑定条目的结算路由与记账释放：被父调用循环 await 的退役条目 SHALL 仍按 per-invocation 路由递减记账屏障，汇总通知仅覆盖无绑定条目。经 Resume 从退役终态恢复的任务，其恢复轮外发的结算信号 SHALL 按恢复语境取谱系，MUST NOT 继承退役戳记。

#### Scenario: 批量退役单条汇总

- **WHEN** 一次唤醒同时回收多个任务
- **THEN** 仅产生一条合并汇总通知，而非逐任务多条

#### Scenario: 积累的结算票据可被折叠

- **WHEN** 历史中积累多条结算通知且上下文逼近预算
- **THEN** 压缩将其折叠为票据引用并回收正文，仍可按票据回补全量

#### Scenario: 被父循环 await 的退役条目仍完成记账释放

- **WHEN** 一个由父调用循环 await（有 invocation 绑定）的任务被批量退役
- **THEN** 其结算仍按 per-invocation 路由递减记账屏障并唤醒父循环，父循环可正常静止退出，不依赖调用方硬超时

#### Scenario: Resume 恢复轮结果按原谱系外投

- **WHEN** 一个被退役（终态 failed）的任务经 Resume 恢复并再次结算
- **THEN** 恢复轮结算信号携带恢复语境谱系（非 task-retired），宿主投递门按原谱系正常外投，用户可拿到恢复轮结果

#### Scenario: 退役谱系不写任务 Origin

- **WHEN** 任意路径退役一个任务
- **THEN** 任务 Spec.Origin 不被改写，谱系戳仅存在于外发结算信号上
