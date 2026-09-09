## Purpose

让本地运维方（agent）可以在不持有 QQ 身份的前提下，通过 127.0.0.1 上的受限 HTTP 端点向机器人下发既有业务指令，并查询可用指令清单。

## ADDED Requirements

### Requirement: 本地指令通道端点
机器人 SHALL 在 127.0.0.1:9601 上提供 `POST /cmd` 与 `GET /cmd` 两个端点；两个端点 MUST 仅绑定回环地址，MUST NOT 监听任何外部接口。

#### Scenario: 列出可用指令
- **WHEN** 客户端向 `GET /cmd` 发起请求
- **THEN** 系统返回 JSON 数组，包含注册表内全部指令的 keyword、aliases 与 description

#### Scenario: 下发已知指令
- **WHEN** 客户端向 `POST /cmd` 提交 `{"command":"<keyword>","args":[...]}`
- **THEN** 系统经指令注册表分发执行，回复走配置的默认消息频道，author 标记为 `local-admin`

#### Scenario: 下发未知指令
- **WHEN** `POST /cmd` 的 command 不在注册表内
- **THEN** 系统返回 404 与错误 JSON，不执行任何指令

#### Scenario: 请求体非法
- **WHEN** body 非合法 JSON 或缺 command 字段
- **THEN** 系统返回 400 与错误 JSON

#### Scenario: 指令执行异常
- **WHEN** 指令执行返回错误或 panic
- **THEN** 端点返回 500 并包含错误摘要，进程 MUST NOT 因指令失败退出
