# remote-agent-communication Specification

## Purpose

本 delta 要求 A2A 与内部子委派使用**显式目标与调用归属**：目标由应用/调用方指定，不猜；重试不改变归属；协议原样复用。

## ADDED Requirements

### Requirement: 显式目标与调用归属

A2A 远程子代理与内部子委派 SHALL 以**显式目标**（目标 agent/session 地址）发起，MUST NOT 由最近会话或启发式猜测目标；每次委派 SHALL 记录调用归属（所属 invocation/decision 与目标），使数量、归属、阻塞与退出原因可解释。

#### Scenario: 显式目标发起

- **WHEN** 一个配置了 `remote` URL 的子代理被调用
- **THEN** 系统按该显式 URL 创建/路由远程调用，不猜测其它目标
- **AND** 调用归属记录到调用方 invocation

#### Scenario: 无显式目标即拒绝（失败）

- **WHEN** 委派缺少可解析的显式目标
- **THEN** 返回具名错误，MUST NOT 回退到"最近会话"或默认目标

#### Scenario: 重试不改变归属（并发）

- **WHEN** 一次 A2A 调用因网络错误重试
- **THEN** 重试沿用同一调用归属与目标
- **AND** 不产生第二份事实

### Requirement: 协议原样复用不与 state 双写

A2A 线协议 SHALL 复用 `trpc-a2a-go` 原生实现，MUST NOT 重写协议；外部上下文传递 SHALL 经既有 `RuntimeState`/`external_context` 通道，MUST NOT 在本地再维护第二份可写历史。

#### Scenario: 复用原生协议

- **WHEN** TagentAgent 作为 A2A server 暴露
- **THEN** 使用原生 `a2a.New(...)`/`server.Start(host)`，agent card 发布于 `/.well-known/agent.json`
- **AND** 不新增自造线协议

#### Scenario: 不双写历史（失败）

- **WHEN** 远程请求被处理
- **THEN** 结果只经单一权威历史路径提交
- **AND** 不存在第二份可写会话历史被维护
