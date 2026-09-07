# rl-feedback Specification

## Purpose

RL 反馈通道:GET /diagnostics 诊断消费面、GET /feedback/wait long-poll(AReaL 拉取)、TurnTracker 真实 turn 口径——评估判据可达性质变。
## Requirements

### Requirement: 诊断快照消费面
rl HTTPAPI SHALL 提供 `GET /diagnostics` 端点,输出 DiagnosticsSnapshot JSON(向量健康/存储规模/WAL quarantine 等);构造器经装配层注入(SetDiagnosticsFn 模式)。

#### Scenario: 运维拉取诊断
- **WHEN** GET /diagnostics
- **THEN** 200 + 快照 JSON(含 wal_quarantined 等维度)——诊断数据获得消费面

### Requirement: RL 反馈 long-poll 通道
HTTPAPI SHALL 提供 `GET /feedback/wait?timeout=N`(上限 30s):阻塞至超时或新 feedback 事件,返回最近待评分摘要列表(event_key+summary);TurnTracker 使评估窗口 Evidence 的 TurnCount 真实采集(TypeTurnStart 计数)。

#### Scenario: AReaL 拉取待评分事件
- **WHEN** AReaL 侧 GET /feedback/wait
- **THEN** 有新 feedback 即返列表;无则 30s 超时空列表——外部训练环拉取模型就绪
