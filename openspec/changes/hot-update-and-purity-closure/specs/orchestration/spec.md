# orchestration Specification

## Purpose

约束 hot-update-and-purity-closure 的编排行为：启动门、波次解锁、共享面契约守护、三级验收与放行，使多域并发不互踩、完成以复跑凭据为准、症状锚不被工程目标替换。

## ADDED Requirements

### Requirement: 启动门与波次解锁

后置域 SHALL 仅在其前置依赖入 main 后派发；波次 SHALL 仅作汇报分组，不得作解锁栅栏。

#### Scenario: 03/05 等待 restart-recovery 归档

- WHEN `restart-recovery-optimization` 未归档
- THEN 03/05 中触碰 recovery.go/run.go/host_reload.go 的孙任务不派发，01/02/04 照常派发

#### Scenario: 05 等待契约与 hygiene

- WHEN 探针 5.2 或契约冻结 5.3 或 03 未入 main
- THEN 05 消费者孙任务 5.4-5.12 不派发

### Requirement: 共享面契约守护

跨域共享面（run.go 装配缝、host_reload.go、api/event、api/config）SHALL 由编排者单写或经 D6 冻结签名级契约后独立派发；收尾 SHALL 以 F1 diff 断言契约零未授权改动。

#### Scenario: 消费者域不改共享缝

- WHEN 05 某消费者域 agent 试图修改 run.go 装配缝签名
- THEN 该改动被拒，须回编排者按 D6 冻结契约统一落

#### Scenario: F1 契约断言

- WHEN 收尾跑契约目录对基线的 diff 断言
- THEN 除与 design D6 逐字一致的新增外 diff 为空，否则 F1 红

### Requirement: 症状锚优先与放行

验收矩阵第一行 SHALL 为症状锚 A/B 直译测试；工程目标 SHALL 只能追加；放行 SHALL 经独立只读审者末轮显式 frontier 空，且证据取自提交树。

#### Scenario: 工程绿而锚红

- WHEN 某域工程测试全绿但锚 A 仍红（锚 B 已由 restart-recovery 兑现基线绿）
- THEN 以锚为准判该域未完成，不得归档

#### Scenario: 回滚必红

- WHEN 完整回滚本变更
- THEN 锚 A 测试必红；修复后必绿；两次证据入 F3 run notes（锚 B 基线已绿，不作回滚红锚）