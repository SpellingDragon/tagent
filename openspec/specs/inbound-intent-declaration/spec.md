# inbound-intent-declaration Specification

## Purpose
TBD - created by archiving change delivery-intent-and-receipts. Update Purpose after archive.
## Requirements
### Requirement: /task 注入可选声明意图血统

`POST /task` 请求体 SHALL 支持可选 `trigger_source` 字段。受理时声明值 SHALL 写入注入事件的 `Metadata[trigger_source]`，事件 `Source` SHALL 保留机械通道标签（`http`）供遥测；未声明时行为 SHALL 与现状逐位相同。

#### Scenario: 受信集成声明 user

- **WHEN** 端点 auth 已启用，且请求体携带 `trigger_source: "user"`
- **THEN** 注入事件 `Metadata[trigger_source]=="user"`、`Source=="http"`，批血统推导（`extractTriggerSource`）判为 `user`，该轮产出按可投递血统分发

#### Scenario: 缺省不声明

- **WHEN** 请求体不含 `trigger_source`
- **THEN** 事件不携带声明血统，`Source=="http"`，批血统推导与投递行为与现状逐位相同

### Requirement: 声明受理 fail-closed 三重校验

声明 SHALL 满足三点，任一不满足即 400 拒绝整请求（fail-closed）：值域当前仅 `"user"`（扩域需显式裁决并修订本条）；端点 auth 未启用时拒绝受理任何声明（`declaration_requires_auth`）；缺省为不声明。

#### Scenario: 无 auth 拒绝声明

- **WHEN** 端点未配置 auth token，请求携带 `trigger_source`
- **THEN** 返回 400 `declaration_requires_auth`，事件不注入

#### Scenario: 非法值拒绝

- **WHEN** 声明值非 `"user"`（如 `"meditation"`、空串、任意字符串）
- **THEN** 返回 400，事件不注入

### Requirement: 白名单语义与声明能力隔离

`DeliverableLineage` 白名单与宿主分发分支 SHALL 不因声明能力而改动；声明的唯一效果是把已知意图代入现行血统推导路径。通道标签 SHALL 始终保留于 `Source` 与遥测面。

#### Scenario: 声明不扩白名单

- **WHEN** 任意未声明或声明非 user 的 /task 请求
- **THEN** 白名单判定结果与改动前逐位相同，产出仍按原终态处理（含 delivery-dispatch-receipts 的回执矩阵）

