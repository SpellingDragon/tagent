## Purpose

wechat-bot 转世（重启）通报注入能力：进程重启后，REINCARNATION_NOTICE 等待 WAL 恢复重放/投影 rebuild 完成信号后，以独立 source 的尾部 external input 注入旧进程中断现场通报，保证重启前后送入 LLM 的消息序列语义连续。

## ADDED Requirements

### Requirement: 通报注入等待投影就绪信号

转世通报 SHALL 在启动注入流程前等待「WAL 恢复重放/投影 rebuild 完成」信号，而非固定时间延迟；信号未就绪时 MUST NOT 注入通报，避免通报与恢复重放竞速导致通报迟到于重启后首个 LLM 调用（agent 在关键窗口不知自己在转世现场；三探针取证确认位置现状正确，缺陷是迟到而非位置错误）。

#### Scenario: 长重放场景下通报不抢跑

- **WHEN** 进程启动后 WAL 恢复重放耗时超过原固定 5 秒延迟（长 WAL 重放可达数分钟）且尚未完成
- **THEN** 通报注入流程保持等待，不发生注入；重放/投影 rebuild 完成信号到达后才注入通报

#### Scenario: 短重放场景下行为不劣化

- **WHEN** 进程启动后 WAL 恢重放很快完成（信号早于原 5 秒延迟到达）
- **THEN** 通报在信号到达后注入，注入不早于投影就绪

### Requirement: 通报以尾部 external input 注入且 source 独立

转世通报 SHALL 作为 external input（role=user）注入且注入 source MUST 为 "reincarnation"（不得复用 "meditation"）：位置上落在恢复现场尾部（恢复历史之后、system 头之后），语义上消除与冥想通道的混用，不误触发 meditation 投递门禁。

#### Scenario: 注入后消息序列尾部追加

- **WHEN** 重放/投影就绪信号到达且通报完成注入
- **THEN** 送入 LLM 的 messages 序列中，通报位于全部恢复历史之后（尾部），system 头保持冻结（不多出通报 system 消息、历史段不后移）

#### Scenario: source 独立不触发冥想门禁

- **WHEN** 通报以 source="reincarnation" 注入并经事件循环消费
- **THEN** 事件中记录的 source 为 "reincarnation"，meditation 投递门禁（分批/静默不投递逻辑）不被该通报触发

### Requirement: 通报持久化语义不变

通报注入后 SHALL 经 external input 事件持久化进 WAL 与投影（事件溯源不变），重启后的新进程 MUST 能在重放中看到该通报事件。

#### Scenario: 注入事件落投影可回放

- **WHEN** 通报以尾部 external input 注入且持久化成功
- **THEN** WAL/投影中存在该通报事件记录（role=user、source=reincarnation），后续重启重放可恢复该事件

### Requirement: 投递分发兼容

main.go 事件分发 SHALL 对 source="reincarnation" 的事件定义明确行为（投递或静默消化均可，但 MUST 显式处理而非落 default 静默吞掉）：若选择投递，投递内容 MUST NOT 含泄漏敏感字段（与既有 T-G 门禁一致）；若选择静默消化，日志 MUST 记录吞掉原因。

#### Scenario: 转世通报事件分发行为明确

- **WHEN** source="reincarnation" 的事件进入 main.go 分发 switch
- **THEN** 命中显式定义的 case（投递或静默消化二选一），且行为经测试锁定

### Requirement: 通报连续性回归不回归

通报注入时机与 source 变更后，既有转世连续性行为（rebuild/fallback/orphan 场景）MUST 保持全绿：冷启动无通报、stale 通报不注入、检测/降级路径不变。

#### Scenario: 既有转世连续性测试全绿

- **WHEN** 运行 examples/wechat-bot 包既有转世相关单测（detect/metadata/WAL tail/notice text/breakpoint 判定）
- **THEN** 全部通过，无行为回归
