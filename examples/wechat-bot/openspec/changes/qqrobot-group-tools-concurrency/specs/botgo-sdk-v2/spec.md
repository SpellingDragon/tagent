## Purpose

> **DEFERRED（2026-09-08）**：botgo SDK fork（v0.0.9→v0.1.0，补 v2 群管理封装）因 GitHub 网络不可达（git@ SSH 挂起、HTTPS clone/tarball 超时，仅 ssh -T 认证通）整体延期，用户指示"SDK 拉不下来就先不优化 SDK"。网络恢复后另立 change 执行；本 spec 保留为 defer 记录，**不计入本期验收**。

botgo SDK fork 升级目的：补齐 QQ 开放平台 v2 群管理 OpenAPI 封装（群成员禁言、群成员角色），核对群富媒体 v1 封装，使主工程经 fork 使用群管理能力。

## ADDED Requirements

> 以下 requirement 本期 DEFERRED（网络恢复后另立 change 实施），此 spec 保留为 defer 记录。

### Requirement: fork 版本与封装完整性

fork SHALL 重新克隆至 `src/botgo`（上次克隆静默失败，目录缺失），版本从 v0.0.9 升至 v0.1.0 并发布 tag；v0.1.0 MUST 包含：群成员禁言封装（对应 `POST /v2/groups/{group_openid}/restrict_chat_setting`，body: `action_type` add/del + `member_openid`——⚠️ 网传 `PUT .../member_mute` 路径不存在，以官方 autogen 文档为准）、群成员角色查询封装（v2 接口）；群富媒体（v1）既有封装 MUST 核对可用、无行为回归。

#### Scenario: 主工程引用 fork v0.1.0

- **WHEN** 主工程 go.mod 以 replace 指向本地 `src/botgo`，模块版本 tag v0.1.0
- **THEN** `bash build.sh` 构建通过，群成员禁言能力可经 SDK 调用

#### Scenario: 禁言 API 错误语义保留

- **WHEN** 服务端返回错误（如无权限 / 用户不在群）
- **THEN** SDK 返回可区分的错误（非 2xx 状态码与错误体透传），主工程工具层可据此降级

### Requirement: 群成员禁言封装行为

群成员禁言封装 SHALL 支持 add（禁言）/ del（解除）两态语义，幂等调用 MUST 安全（重复禁言同用户不报错）；到期解除由平台语义决定（v2 契约无时长字段）。

#### Scenario: 禁言后到期自动解除

- **WHEN** 调用 restrict_chat_setting 设置 add
- **THEN** 平台按自身语义到期自动解除禁言（GET 查询 mute_expire_at 可复核）；del 立即解除
