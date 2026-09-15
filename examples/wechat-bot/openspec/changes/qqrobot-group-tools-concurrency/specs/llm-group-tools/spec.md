## Purpose

将 QQ 群管理能力（禁言）注册为 LLM 聊天管线中的可调用工具，工具说明本身承载"说明与安抚"的处置指引，由 LLM 自主判断与调用，替代硬编码审核流。

> 2026-09-08 范围调整：botgo SDK fork 升级 DEFERRED（GitHub 网络不可达，用户指示"SDK 拉不下来就先不优化 SDK"）。本能力改为主工程内 **v2 REST 直调**（kb/api/qq/group_restrict_chat_setting.md 契约），不依赖 SDK 新封装；角色查询工具随 SDK 一并 defer。

## ADDED Requirements

### Requirement: mute 注册为 LLM 工具（v2 REST 直调）

系统 SHALL 提供 group_member_mute 工具，注册进聊天管线（service 层 eino ToolsNode），可由 LLM 自主调用；工具底层 SHALL 经主工程 v2 直调客户端调用 `POST /v2/groups/{group_openid}/restrict_chat_setting`（body: `action_type` add/del + `member_openid`，鉴权复用 token.QQBotTokenSource 体系）。工具 description SHALL 包含：适用情形（有害信息处置）、处置方式、以及"说明与安抚"话术指引（禁言后需向群内说明原因、安抚被处置用户），description 即 prompt 载荷。⚠️ v2 契约**无时长字段**：平台到期自动解除语义，工具不暴露时长参数。

#### Scenario: LLM 判断有害信息后自主调用 mute

- **WHEN** 群消息中有害内容被 LLM 判定为需要处置，且机器人具备群管理权限（v2 白名单已开通）
- **THEN** LLM 通过 ReAct 循环调用 group_member_mute 工具（add），目标为发言用户；工具执行成功后，后续回复 SHALL 包含面向群说明与安抚的话术

#### Scenario: 工具执行失败时优雅降级

- **WHEN** mute API 调用失败（白名单未开通 11253 / 权限不足 / openid 无效）
- **THEN** 工具返回结构化错误信息（不 panic），LLM 据此调整策略（如仅口头警告）；该失败 MUST 记录日志

#### Scenario: 非 @ 机器人消息同样可触发工具

- **WHEN** 机器人因活跃窗口 / 昵称触发而进入聊天管线（未被 @），且消息含需处置内容
- **THEN** LLM 仍可调用 group_member_mute，处置逻辑不依赖 @ 触发

### Requirement: v2 直调客户端错误语义可区分

v2 直调客户端 SHALL 将非 2xx 响应解析为可区分的错误（11253 白名单未开通 / 权限不足 / 非群成员等），错误体透传给工具层；token 过期 SHALL 经 tokenSource 自动重取后重试一次。

#### Scenario: 白名单未开通时降级

- **WHEN** 直调返回 11253（应用无接口访问权限）
- **THEN** 错误透传至工具层与 LLM，LLM 降级为口头警告处置；结论记录至 kb（是否开通白名单）供后续决策

### Requirement: 群管理工具集可扩展

未来群管理能力（角色查询、移出群等，随 SDK defer 项 D2/D6）MAY 按同一模式扩展，注册入口 MUST 收敛到统一的绑定函数（BindReactTools 模式）。

#### Scenario: 新增群管理工具按同一模式注册

- **WHEN** 开发者按现有模式新增一个群管理工具（实现 eino tool.BaseTool，description 含处置指引）
- **THEN** 该工具经统一绑定函数注册后即可被 LLM 调用，无需改动聊天管线主流程
