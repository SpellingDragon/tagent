# Orchestration 契约（一级仅留编排面）

## ADDED Requirements

### Requirement: 症状锚启动门

本变更 SHALL 以三条症状直译测试的**先红**作为其余一切编码工作的启动门：任何一域的行为修改在 00 域红证产出之前不得开始。

#### Scenario: 未出红证即改行为被拒

- **WHEN** 00 域三测试任一尚未在基线树上产出红证
- **THEN** 01/02/03/04 域的行为修改 SHALL NOT 进入实施；交付物缺红证视为启动门未过

### Requirement: 麻雀闭环

01.1（受理回执→`ok`）SHALL 在 W0 内完成，使症状 2 的直译测试由红转绿，作为其余域的机制样板。

#### Scenario: W0 未闭环不进 W1

- **WHEN** 01.1 未使 00.2 转绿
- **THEN** W1 三单 SHALL NOT 派发

### Requirement: 触碰集不相交与热点合单

同文件簇的孙任务 SHALL 合单或严格串行（01 与 02 同动 host.go 即串行）；仅文件簇零相交的任务可并发。

#### Scenario: 同文件并发被拒

- **WHEN** 两笔派发单的写集同时含 host.go
- **THEN** 两单 SHALL NOT 并发；编排文档记录合单或串行理由

### Requirement: 症状红转绿双证据（F3）

F3 SHALL 含症状直译锚的回滚即红与修复后绿两次证据；工程化测试全绿不替代症状证据。

#### Scenario: 缺回滚证据不得勾选 F3

- **WHEN** F3 只有修复后绿证、无回滚即红证
- **THEN** F3 SHALL NOT 勾选，收口未完成

### Requirement: 契约偏移检查

实施期任何对骨架/契约（主规格、golden 合同）的修改 SHALL 有评审记录；本合同条款修订须显式声明。

#### Scenario: 无评审的契约改动被拒

- **WHEN** 实施单修改 `openspec/specs/**` 或 `golden/runtime/event_integrity_scenarios.json` 而无评审记录
- **THEN** 该单 SHALL 回退并补评审
