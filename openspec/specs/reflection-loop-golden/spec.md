# reflection-loop-golden Specification

## Purpose
长时反思循环的持续正确性契约：30+ 轮反思循环的水位、消费、资源、投递与重启续接行为由 golden 门保护。
## Requirements
### Requirement: 长时反思循环 golden 门

CI 常规测试面 SHALL 含长时反思循环 golden（脚本模型驱动 30–50 轮，墙钟分钟级）；断言面 SHALL 至少覆盖：反思水位单调不回退、零重复消费、零资源泄漏、跨会话投递逐目标恰一次、重启后水位续接。断言 SHALL 基于事件计数与状态读数（不基于墙钟），红即阻 CI。

#### Scenario: 水位单调与零重复

- **WHEN** 30+ 轮反思循环跑完
- **THEN** 水位读数单调不回退且每条反思产物恰被消费一次

#### Scenario: 重启后续接

- **WHEN** 循环中途进程重启
- **THEN** 水位从持久记录续接，不重复消费重启前已消费的产物

#### Scenario: 逐目标恰一次

- **WHEN** 反思产物投给多个目标会话
- **THEN** 逐目标恰一次（账目或事实计数），重复投递不产生第二条有效记录
