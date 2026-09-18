## ADDED Requirements

### Requirement: LLM 端点重定向逐跳受 allowlist 约束

动态端点重定向启用时，LLM HTTP client SHALL 安装 CheckRedirect 钩子：30x 跳转的每一跳目标 host MUST ∈ endpoint allowlist，越界跳转 SHALL 被拒绝且请求以明确错误终止；allowlist 未配置（重定向禁用语义）时任何跳转 SHALL 被拒绝。初始 URL 的既有校验（scheme/userinfo/fragment/host）不变。

#### Scenario: 端点 302 跳出 allowlist 被拒

- **WHEN** allowlist 为 `proxy.allowed.example` 且该端点 302 跳转到 `internal.metadata.host`
- **THEN** 第二跳被 CheckRedirect 拒绝，请求失败并返回明确的越界 host 信息，不发生任何对越界主机的请求

#### Scenario: 未启用重定向时跳转全拒

- **WHEN** 部署未启用动态端点（allowlist 空）且某响应携带 30x
- **THEN** 所有跳转被拒绝，行为与重定向禁用语义一致
