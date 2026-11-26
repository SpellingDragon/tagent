# ci-throughput 规格（delta）

> 04 域验收契约：缓存只加速不判定、race 分片并集保真、证明面零收缩、consumer/restart/mailbox 稳定性前置、真 CI 读数前后对比。吞吐优化本身不改产品行为、测试断言、重复轮次与变异矩阵；稳定性前置修复按各自锚变更实现与夹具（见本文件「稳定性前置属本域且保留原判据」），本域 MUST NOT 被宣称只改 workflow。

## ADDED Requirements

### Requirement: 缓存只加速不判定

编译缓存（module 与 build）SHALL 只影响构建耗时；任何门与锚的判定 SHALL 只看命令退出码。冷缓存与热缓存两态 SHALL 各取一轮证据且均绿；缓存键 SHALL 绑定各模块的依赖锁文件，键源缺失 SHALL 使缓存失效而非使判定改变。缓存 MUST NOT 改写测试命令、选择集或阈值。

#### Scenario: 冷热两态同判

- **WHEN** 同一提交在冷缓存与热缓存各跑一轮
- **THEN** 全部门的结论 SHALL 相同，两轮的缓存命中读数 SHALL 分别入档
- **AND** 缓存未命中只 SHALL 体现为耗时，MUST NOT 改变退出码

#### Scenario: 键源剥离即红

- **WHEN** 从缓存键源中剥离某一模块的依赖锁文件
- **THEN** 工作流静态断言 SHALL 红，拒绝以残缺键源上线

### Requirement: race 全覆盖由分片并集保真

race 门 SHALL 可分散在多个分片执行；分片包集定义 SHALL 单点维护在 `scripts/gates/race_check.sh`，脚本 SHALL 静态断言全部分片的并集等于原全包集，缺包、重包或空分片即红。分片内允许按顶层测试名把单包拆为互补选择器，选择器 SHALL 成对声明（奇偶互补），缺失或不互补即红。

聚合门 SHALL fail-closed：任一分片非 success（含 skipped、cancelled）SHALL 使聚合红；分片 MUST NOT 改变 race 检查的退出码保真与豁免分类器语义。

#### Scenario: 分片缺包

- **WHEN** 从分片定义删去一个包
- **THEN** 并集断言 SHALL 红，聚合门 MUST NOT 报绿

#### Scenario: 选择器不成对

- **WHEN** 上传的分片产物里单包选择器缺失或不再是奇偶互补
- **THEN** 并集检查 SHALL 红

#### Scenario: 分片被跳过

- **WHEN** 任一分片以 skipped 或 cancelled 结束
- **THEN** 聚合门 SHALL 红，MUST NOT 以其余分片通过放行

### Requirement: 证明面不因吞吐优化而收缩

重复轮次矩阵与变异矩阵 SHALL 保持原样；MUST NOT 以删测试、缩包集、跳过或用例改写换取墙钟。触发策略（push 与 PR 双跑）SHALL NOT 在本能力内改变，去重属独立裁定。慢路径 SHALL 以隔离执行面承接（独立步骤或独立分片），MUST NOT 以扩大超时或放宽断言承接。

#### Scenario: 优化越界

- **WHEN** 任一优化提议需要减少轮次、缩小 race 包集或放宽阈值
- **THEN** 该提议 SHALL 被拒绝并回设计门

#### Scenario: 以隔离代替放宽

- **WHEN** 单个重路径用例长期贴超时边界
- **THEN** 处置 SHALL 是把该路径移入独立执行面并保留原断言与超时
- **AND** MUST NOT 以扩窗、删除断言或标记跳过收场

### Requirement: 稳定性前置属本域且保留原判据

真 CI 读数 SHALL 以可复现执行为前提：consumer 风暴路径、restart 恢复通知等待与完成通知接受/认领的竞态修复 SHALL 登记在本域，与吞吐优化同批验收。consumer 重路径 SHALL 只移入独立执行面（同 job 独立 step 或独立分片），restart 夹具 SHALL 在恢复通知模型请求与答案完成后再发显式 invocation，完成通知的接受与认领 SHALL 以原子 handover 收敛。

稳定性修复 MUST NOT 以扩大超时或窗口、放宽或删除断言、标记跳过承接。本域写面 MUST NOT 被宣称只含 workflow 与 race_check：稳定性前置的写面 SHALL 逐个列出（workflow 独立 step、consumer/restart 测试夹具、`assembly` 与 `runtime/state` 的完成通知面）。

#### Scenario: 稳定性修复不下调判据

- **WHEN** consumer 风暴或 restart 等待在共享 runner 上贴边
- **THEN** 处置 SHALL 是隔离执行面或补有界等待，原断言值、窗口与超时 SHALL 保留
- **AND** MUST NOT 以扩窗、删断言或标记跳过收场

#### Scenario: 写面声明完整

- **WHEN** 审计本域的写面与影响面
- **THEN** 声明 SHALL 同时列出吞吐优化面与稳定性前置面，MUST NOT 写成只改 workflow 与 race_check
- **AND** 完成通知的接受与认领 SHALL 显示为同一次原子 handover

### Requirement: 吞吐读数以真 CI 为权威面

吞吐验收 SHALL 以真 CI 为权威面：记录变更前后同一工作流的作业分解与墙钟，关键路径 SHALL 定义为 `race-aggregate.completedAt - run.startedAt`，目标 SHALL 低于原单 race 作业墙钟的既定比例（见 Scenario），run 墙钟 SHALL 同步下降。读数 SHALL 以运行接口的原始作业数据入档，MUST NOT 以本地近似值、记忆值或剔除排队/聚合的有利口径替代；同一分片的多次尝试 SHALL 按尝试归属，MUST NOT 把继承自上一尝试的作业计作本轮执行。

#### Scenario: 关键路径读数

- **WHEN** 分片与缓存上线后的第一轮真 CI 完成
- **THEN** 作业分解表 SHALL 列出分片墙钟、排队与聚合门状态，前后对比 SHALL 按同一口径列出
- **AND** 关键路径 SHALL 低于原单 race 作业墙钟的 60% 才算达标，未达标 SHALL 记录并继续调整配比；聚合完成前的等待 SHALL 计入关键路径

#### Scenario: 尝试归属错误

- **WHEN** 一处读数把重跑尝试继承的作业计入本轮执行
- **THEN** 该读数 SHALL 判无效，须按尝试边界重算
