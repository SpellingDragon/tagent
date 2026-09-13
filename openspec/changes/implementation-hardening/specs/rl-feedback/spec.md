## ADDED Requirements

### Requirement: RL HTTP API 认证与监听约束

RL HTTP API SHALL 支持 token 认证（API 层）：宿主经 `SetAuthToken`（或等价构造选项）启用后，ServeHTTP 顶部单一强制点 SHALL 验证 `Authorization: Bearer <token>`，不匹配返回 401 且不执行任何端点副作用； SHALL 提供 `TAGENT_RL_AUTH_TOKEN` 环境读取助手。监听守卫 SHALL 以导出助手（如 `ValidateListenAddr(addr, token)`）提供：token 未设且地址非 loopback 时返回错误并列明出路（配置 token / 改 loopback / 明确风险后启用不安全开关）；随载宿主（wechat-bot）启动前 MUST 调用该守卫。

#### Scenario: 配置 token 后未带凭证的请求

- **WHEN** SetAuthToken 已启用，客户端不带或带错误 Authorization 头调用任意端点
- **THEN** 服务返回 401，不执行 InjectMessage / feedback / llm_base_url 变更等任何副作用

#### Scenario: 无 token 且监听非 loopback

- **WHEN** token 未设置且宿主以非 loopback 地址（如 0.0.0.0）调用监听守卫
- **THEN** 守卫返回错误拒绝启动监听，错误信息列明三条出路；随载宿主默认遵守（未设 token 时绑 127.0.0.1 并留指引日志）
