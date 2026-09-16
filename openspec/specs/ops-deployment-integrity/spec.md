# ops-deployment-integrity Specification

## Purpose
TBD - created by archiving change hardening-review-batch2. Update Purpose after archive.
## Requirements
### Requirement: 运维探针认证集成
restart / maintenance 健康探针与 mail poller MUST 从受控凭证源（环境变量或 rl 配置文件）读取认证 token 并随请求发送；`/healthz` 保持鉴权不豁免。探针 MUST 区分响应类别：401 → AUTH_FAIL（告警、不触发杀进程）、连接拒绝 → 等待窗口内正常状态、200 → 健康判定继续。

#### Scenario: 启用 token 后例行健康检查
- **WHEN** 配置了 TAGENT_RL_AUTH_TOKEN 且服务正常
- **THEN** 探针带 Authorization 头获得 200，保险脚本 MUST NOT 因 401 触发重启

#### Scenario: 邮件投递
- **WHEN** mail poller 向 /task 提交任务且 token 已启用
- **THEN** 请求 MUST 携带认证头并被接受，MUST NOT 进入无限 401 重试

### Requirement: 部署状态链完整
重启保险 MUST 维持可区分的状态标记：成功 / 失名失败 / 超时各自独立 marker，失败路径 MUST NOT 复用成功 marker 导致后续跳过；健康门 MUST 校验运行进程的 revision 指纹与预期一致；pidfile MUST 记录真实子进程 PID。

#### Scenario: 部署失败后再次执行
- **WHEN** 上次换装失败（新进程未起）后再次运行重启脚本
- **THEN** 脚本 MUST NOT 因遗留成功 marker 跳过，且 MUST 以 expected revision 校验健康门

### Requirement: HTTP 服务宿主持有
wechat-bot 的 HTTP API server MUST 由宿主代码持有（http.Server）：重试循环受主 ctx 取消，SIGTERM 走 Shutdown 优雅等待；MUST NOT 在 goroutine 内无控制永久循环。

#### Scenario: 进程退出
- **WHEN** 进程收到 SIGTERM 且 HTTP 正在重试等待
- **THEN** 重试循环 MUST 立即取消并走 Shutdown，进程 MUST NOT 因 HTTP goroutine 阻塞退出

