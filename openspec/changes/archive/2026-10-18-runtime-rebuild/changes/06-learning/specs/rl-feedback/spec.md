# rl-feedback（06-learning 子变更 delta）

> 本文件为二级子变更 `06-learning` 对既有主 spec `openspec/specs/rl-feedback/spec.md` 的 delta（主 spec **只读**）。
> `MODIFIED` 段为**完整源 Requirement 全量拷贝 + 本波新增约束**；未列出者视为继承主 spec 现行有效约束，**不默认「无 delta = 继承」**。

## Purpose

RL 反馈通道：诊断消费面、long-poll 拉取、HTTP 鉴权与动态端点授权。本波在既有「显式授权管理能力」语义上补充**逐跳重定向复核**、响应体大小限制，以及**反馈归属到 decision/attempt** 与**真实端点单次门控**。

## MODIFIED Requirements

### Requirement: 动态端点是显式授权管理能力

默认宿主 SHALL 使用可返回 error 的 endpoint 更新接口；未开启能力而请求 llm_base_url SHALL 拒绝。启用时 URL MUST 为 http/https、无 userinfo/fragment 且 host 属显式 allowlist，端口可动态；重定向 SHALL 同受限制。更新错误 SHALL 不注入消息、不报告更新成功。更新与本批接收 SHALL 串行化；日志 SHALL 脱敏，不写凭据或完整敏感 URL。此能力 SHALL 明确为全局管理面，不宣称多租户 task 级隔离。**本波新增**：重定向 MUST **逐跳**按同一 allowlist 重新校验（MUST NOT 仅校验首跳）；HTTP 响应体 SHALL 有大小限制，超限 SHALL 拒绝并保留原因。

#### Scenario: 非许可目的地
- **WHEN** 合法 token 请求将模型切到 allowlist 之外的 host
- **THEN** 在更新模型和注入消息前拒绝，无 prompt 发往该目的地

#### Scenario: 更新失败
- **WHEN** endpoint callback 返回 error
- **THEN** 保持原模型且请求失败，不返回 accepted

#### Scenario: 逐跳重定向越权（失败，本波新增）
- **WHEN** 首跳 host 合法但重定向到 allowlist 之外的 host
- **THEN** 该跳 MUST 被拒绝，无 prompt 发往越权目的地，MUST NOT 仅凭首跳放行。

#### Scenario: 响应体超限（失败，本波新增）
- **WHEN** HTTP 响应体超过配置大小上限
- **THEN** MUST 拒绝并保留原因，MUST NOT 无量读取。

## ADDED Requirements

### Requirement: 反馈归属到 decision/attempt
RL HTTP 接入的反馈 SHALL 归属到对应 **decision/attempt**；有证据才绑定，无证据 MUST 标 `unbound`，冲突 MUST 标 `ambiguous`；MUST NOT 以 trace 存在与否决定业务身份。

#### Scenario: 精确归属（正常）
- **WHEN** 反馈携带可定位的 event_key/decision 引用
- **THEN** MUST 绑定到对应 decision/attempt，保留来源与时间。

#### Scenario: 无证据 unbound（失败）
- **WHEN** 反馈无可定位引用
- **THEN** MUST 标 `unbound`，MUST NOT 继承最近父调用或任意猜测。

#### Scenario: 并发反馈各自归属（并发）
- **WHEN** 两个 decision 并发收到反馈
- **THEN** 各自归属 MUST 独立，MUST NOT 相互串写。

#### Scenario: 重启后归属保持（重启）
- **WHEN** 服务重启后重新读取反馈归属
- **THEN** 归属结论 MUST 与写入时一致。

### Requirement: 真实端点单次门控
真实 RL 端点/allowlist 资源验收 MUST 独立单次门控（`-count=1`）；并发族用例走 `-race -count=10`；缺凭据或不可达 MUST NOT 计为完成。

#### Scenario: 真实端点单次验收（正常）
- **WHEN** 真实端点与凭据可用
- **THEN** 单次验收 MUST 通过并保留可复跑命令与输出。

#### Scenario: 缺凭据不计完成（失败）
- **WHEN** 真实端点凭据缺失或被 `SKIP`
- **THEN** MUST NOT 计为完成，汇总标注未验证。
