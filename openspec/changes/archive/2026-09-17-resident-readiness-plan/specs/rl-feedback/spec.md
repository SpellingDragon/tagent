## ADDED Requirements

### Requirement: HTTP 输入与通知资源有界

HTTPAPI SHALL 默认限制 body=1 MiB、messages=32、单条 content=256 KiB、feedback 通知队列=1024；宿主可设置正上限，零采用默认，负值拒绝。请求 SHALL 完整验证后才产生副作用，超限返回 413，格式/role 非法返回 400。合法输入 role 为 user/system，空 role 归 user。feedback 队列溢出 SHALL 仅淘汰最旧通知，不删除事实，并在后续 wait/diagnostics 提供 dropped_count、partial 与补查提示。

#### Scenario: 认证后超大请求
- **WHEN** 合法 token 请求超过 body 或消息限制
- **THEN** 返回 413，不调用模型更新或 InjectMessage

#### Scenario: 通知溢出
- **WHEN** 已持久化反馈多于队列上限且无人消费
- **THEN** 内存队列保持有界，事实可查，下一次 wait 显式报告通知不完整

### Requirement: 请求取消与接收回执

long-poll SHALL 响应请求取消及 server shutdown。POST /task SHALL 使用可判定接收接口，以单 envelope 提交整个 messages 列表；成功返回 202 + request_id + durability，失败返回结构化错误；SHALL NOT 把丢弃或只收到部分消息表示为整批 accepted。原 status 字段 SHALL 保留兼容。

#### Scenario: 客户端取消 long-poll
- **WHEN** 客户端断连或服务 shutdown
- **THEN** 等待立即结束，不等满 30s，不泄漏等待者

#### Scenario: 队列拒绝
- **WHEN** agent 已关闭、队列已满或可靠持久化失败
- **THEN** 返回非 202 的明确错误，客户端可区分不可用、背压与存储失败

### Requirement: 动态端点是显式授权管理能力

默认宿主 SHALL 使用可返回 error 的 endpoint 更新接口；未开启能力而请求 llm_base_url SHALL 拒绝。启用时 URL MUST 为 http/https、无 userinfo/fragment 且 host 属显式 allowlist，端口可动态；重定向 SHALL 同受限制。更新错误 SHALL 不注入消息、不报告更新成功。更新与本批接收 SHALL 串行化；日志 SHALL 脱敏，不写凭据或完整敏感 URL。此能力 SHALL 明确为全局管理面，不宣称多租户 task 级隔离。

#### Scenario: 非许可目的地
- **WHEN** 合法 token 请求将模型切到 allowlist 之外的 host
- **THEN** 在更新模型和注入消息前拒绝，无 prompt 发往该目的地

#### Scenario: 更新失败
- **WHEN** endpoint callback 返回 error
- **THEN** 保持原模型且请求失败，不返回 accepted

### Requirement: 认证与运维同源

随载宿主和探针 SHALL 使用同一 token 契约，所有端点包括 healthz 均遵守认证；无 token 仅允许 loopback。401 SHALL 被判为认证失败而非进程死亡，不能触发误杀/假成功 marker。server SHALL 设置有限读写/空闲超时并由宿主关闭。

#### Scenario: 重启探针收到 401
- **WHEN** 服务存活但探针凭据错误
- **THEN** 记录 AUTH_FAIL、非零退出，不杀服务，不写成功 marker，不记录 token
