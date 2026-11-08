# orchestration Specification（restart-recovery-optimization 一级编排契约）

## ADDED Requirements

### Requirement: 阶段门禁与拷问闭环

一级变更 SHALL 把实施划分为七个阶段（W0–W6）与一个收尾（W7），每一阶段完成后 SHALL 依次执行「派独立只读子代理拷问 → 主代理逐项回答并附证据 → 再派子代理依回答完善 → frontier 空」，MUST NOT 在 frontier 非空时进入下一阶段。子代理拷问 SHALL 对照原任务行的验收面，而非追认缩水后的面。

#### Scenario: 阶段推进凭据

- **WHEN** 某阶段声称完成
- **THEN** ORCHESTRATION 台账 SHALL 含该阶段拷问子代理 id、frontier 项清单、主代理回答、完善凭据与复跑命令，缺一不得推进

### Requirement: 三挂点边界机械守护

全部改动 SHALL 落在「消费 g(x)（读链）」内部，事实链保持唯一真相；MUST NOT 引入链外可写第二真相。外部状态 blob SHALL 由链上票据事实按内容寻址引用、带完整性校验、损坏即退回全量事件流。F0 SHALL 新增边界规则：任何链外持久物未经 R3 登记即红。

#### Scenario: 全量等价

- **WHEN** 存在一条恢复态票据
- **THEN** 「全量重放」与「加载 dump + 其后增量」SHALL 逐位一致；票据缺失/截断/校验不过/版本不认识时 SHALL 退回全量且结果不丢义务

### Requirement: 词汇一致

项目术语 `转世` SHALL 统一为 `重启恢复` 并同步简化代码标识符；MUST NOT 使用 `读透视图` / `链权威答案` 作为项目词汇；契约值（事件元数据、事实键、冻结类型）MUST NOT 随改名变更。

#### Scenario: 契约零改动

- **WHEN** 阶段 1 与收尾分别运行 F1
- **THEN** `python3 openspec/changes/archive/2026-11-08-restart-recovery-optimization/changes/01-vocabulary/verify_contracts.py` SHALL 输出 `VERDICT: PASS (line sets identical)` 与 `MISMATCH_FILES= 0`，即六个冻结字符串（四个契约拼写 + 两个测试名）在「基线 `1c4e165f` 的 10 个持有文件」与「工作树」中含该串的整行序列一致。MUST NOT 用 `git diff <基线>..HEAD`：阶段 1 时 HEAD 恰等于基线，该命令恒为空（vacuous），且契约串不在该 pathspec 内（见 r6）
