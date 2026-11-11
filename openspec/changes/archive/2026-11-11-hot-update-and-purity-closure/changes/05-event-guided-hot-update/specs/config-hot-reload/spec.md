# config-hot-reload Specification（MODIFIED delta）

## Purpose

把 Boot 一次消费者纳入事件指引整代装配：运行中替换执行/记忆/反思/存储/治理/观测/宿主工具等不重启，在途保旧、排空释放一次、整代原子可见。

## MODIFIED Requirements

### Requirement: 全清单热更不重启

须重启清单 SHALL 清空（除具名裁决保留项）；移动这些字段 SHALL 发布新代生效而非具名拒绝。

#### Scenario: exec 热更（锚 A）

- WHEN 运行中 Reload 移动某 agent exec 段不重启
- THEN published=true、新回合走新执行器、旧在途跑完旧代、旧资源排空释放一次

#### Scenario: 清单清空

- WHEN 审查须重启清单
- THEN 除具名裁决保留项外清单空

### Requirement: 在途保旧与排空释放

在途回合 SHALL 持有旧代跑完；旧代资源 SHALL 在排空后精确释放一次；候选构建失败 SHALL fail-closed 保旧代。

#### Scenario: 在途不被抽走

- WHEN 换代时仍有在途回合持旧代
- THEN 旧代资源不释放直至排空

#### Scenario: 构建失败保旧

- WHEN 候选构建/校验失败
- THEN 旧代原样服务、候选资源 Close

### Requirement: 整代原子可见

任何时刻读者 SHALL 看到一整代一致组织，SHALL NOT 半新半旧。

#### Scenario: 无撕裂读

- WHEN 多消费者同时换代
- THEN 请求不见新 exec+旧 memory 组合