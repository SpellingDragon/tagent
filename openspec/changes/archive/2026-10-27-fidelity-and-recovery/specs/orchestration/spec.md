# Orchestration 契约

## ADDED Requirements

### Requirement: 协议保真麻雀先行

G1 协议保真红转绿 SHALL 先于其余实施；G10 测试去假设同批完成（根治 CI flaky）。

#### Scenario: 麻雀未闭环不扩面

- **WHEN** G1 三场景未红转绿
- **THEN** 2.1/2.2/3.x/4.x SHALL NOT 派发

### Requirement: 十锚回滚即红

F3 SHALL 含 G1–G10 各自的定点破坏红证（还原对应修复点→该 G 测试必红）。

#### Scenario: 缺回滚证据不得收口

- **WHEN** F3 只有修复后绿证
- **THEN** F3 SHALL NOT 勾选

### Requirement: 协议保真热点串行

`request.go` 触碰的叶（1.0、2.1、4.x）SHALL 串行；`facts.go`/`loop.go` 簇（2.2）与 `host.go` 簇（3.1/3.2）合单。

#### Scenario: 同文件并发被拒

- **WHEN** 两单写集同时含 request.go
- **THEN** 两单 SHALL NOT 并发
