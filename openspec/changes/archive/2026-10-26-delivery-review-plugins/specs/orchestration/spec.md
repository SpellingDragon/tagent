# Orchestration 契约

## ADDED Requirements

### Requirement: 麻雀先行

投递 hold→批准补投→恰一条 的短闭环 SHALL 在其余实现之前完成，作为发送/审核衔接的机制样板。

#### Scenario: 麻雀未闭环不扩面

- **WHEN** 1.0 的短闭环未红转绿
- **THEN** 1.1/1.2/1.3 SHALL NOT 派发

### Requirement: 症状回滚即红

F3 SHALL 含 G-A/G-B 的回滚即红双证据：还原 entry 的 send 授权→G-A 必红；禁用投递审核→G-B 必红。

#### Scenario: 缺回滚证据不得收口

- **WHEN** F3 只有修复后绿证
- **THEN** F3 SHALL NOT 勾选

### Requirement: 同文件簇串行

`host.go` 与 `api/plugin/governance.go` 由 1.1/1.2 同时触碰的，SHALL 串行或合单；entry 配置与台架文档（1.3）在其后。

#### Scenario: 同文件并发被拒

- **WHEN** 两单写集同时含 host.go
- **THEN** 两单 SHALL NOT 并发
