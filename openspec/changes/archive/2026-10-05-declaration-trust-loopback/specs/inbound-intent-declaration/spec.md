## MODIFIED Requirements

### Requirement: 声明受理 fail-closed 三重校验

声明 SHALL 满足三点，任一不满足即 400 拒绝整请求（fail-closed）：值域当前仅 `"user"`（扩域需显式裁决并修订本条）；**受理前提为端点已鉴权（配置了 auth token）或请求源为回环地址**（127.0.0.0/8、::1；源地址解析失败一律按非回环保守拒）；缺省为不声明。经回环前提受理时 SHALL 记一行可归因日志（无 token 而获豁免这一事实必须可见）。

#### Scenario: 本机回环源无 token 仍受理声明

- **WHEN** 端点未配置 auth token，请求源地址为 127.0.0.1，携带 `trigger_source: "user"`
- **THEN** 请求受理（202），注入事件携带声明血统，且日志记录该声明经回环源获豁免

#### Scenario: 非回环源且无 token 拒绝声明

- **WHEN** 端点未配置 auth token，请求源地址非回环（外部 IP 或容器网桥地址），请求携带 `trigger_source`
- **THEN** 返回 400 `declaration_requires_auth`，事件不注入

#### Scenario: 已鉴权端点不受源地址影响

- **WHEN** 端点已配置 auth token 且请求凭 token 通过鉴权，源地址任意
- **THEN** 声明按值域规则受理（与回环前提无关）

#### Scenario: 非法值拒绝

- **WHEN** 声明值非 `"user"`（如 `"meditation"`、空串、任意字符串）
- **THEN** 返回 400，事件不注入（值域不因源为回环而放宽）
