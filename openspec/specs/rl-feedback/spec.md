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

### Requirement: RL HTTP API 认证与监听约束

RL HTTP API SHALL 支持 token 认证（API 层）：宿主经 `SetAuthToken`（或等价构造选项）启用后，ServeHTTP 顶部单一强制点 SHALL 验证 `Authorization: Bearer <token>`，不匹配返回 401 且不执行任何端点副作用； SHALL 提供 `TAGENT_RL_AUTH_TOKEN` 环境读取助手。监听守卫 SHALL 以导出助手（如 `ValidateListenAddr(addr, token)`）提供：token 未设且地址非 loopback 时返回错误并列明出路（配置 token / 改 loopback / 明确风险后启用不安全开关）；随载宿主（wechat-bot）启动前 MUST 调用该守卫。

#### Scenario: 配置 token 后未带凭证的请求

- **WHEN** SetAuthToken 已启用，客户端不带或带错误 Authorization 头调用任意端点
- **THEN** 服务返回 401，不执行 InjectMessage / feedback / llm_base_url 变更等任何副作用

#### Scenario: 无 token 且监听非 loopback

- **WHEN** token 未设置且宿主以非 loopback 地址（如 0.0.0.0）调用监听守卫
- **THEN** 守卫返回错误拒绝启动监听，错误信息列明三条出路；随载宿主默认遵守（未设 token 时绑 127.0.0.1 并留指引日志）
