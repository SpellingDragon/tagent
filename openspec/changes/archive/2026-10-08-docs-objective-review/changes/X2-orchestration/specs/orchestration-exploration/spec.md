# orchestration-exploration Specification

## ADDED Requirements

### Requirement: 动态编排能力矩阵交付

报告 SHALL 交付"编排调整能力矩阵"：调整维度（模型/工具/MCP/子 Agent 拓扑/任务依赖/优先级/暂停恢复）× 现行支持度（支持/部分/无）× 生效时点（即时/下一回合/需重启）× 改造面定位（文件:符号）。E2 的"最小语义"问题 SHALL 以此矩阵作答而非泛论。

#### Scenario: 矩阵可稽

- **WHEN** 编排者四查本报告
- **THEN** 矩阵 ≥7 行且"现行支持度=支持"的行均有代码佐证

### Requirement: 队列语义补白

报告 SHALL 补白事件队列语义（容量/满时行为/顺序保证/有无优先级与抢占/取消传播），此项为第一阶段未覆盖面，无论结论如何均为交付物。

#### Scenario: 补白有据

- **WHEN** 队列语义表被引用
- **THEN** 每行语义有 event_bus.go 或其测试的代码证据
