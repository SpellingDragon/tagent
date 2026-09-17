# 常驻可靠性与实现收口计划

## Why

三份历史报告之后，tagent 已完成多轮加固，继续照旧清单施工会重复建设；当前仍存在事件提交耐久、恢复完整性、资源所有权和验收可信度之间的断点。以 `aeb273dee72f8fb72c581a35c31344d6d2db669b` 为基线，保留事件事实链与派生投影架构，围绕实际剩余风险建立分阶段、可验证的收口计划，评估证据见 `assessment.md`。

## What Changes

- 建立旧结论处置矩阵和当前发现 F01–F11；明确独立审计未完成、长跑/真实模型证据缺口，不重新给出缺乏标尺的数值评分。
- 让本地 durable 事件提交等待真实屏障，统一存储读取错误、重复键、隔离和生命周期计数契约。
- 可靠模式升级为有界 durable inbox，增加显式接收结果与 claim/commit/ack；无可靠配置仍保持轻量 volatile 模式，不默认开启平台子系统。
- 恢复输出结构化完整性结果：过滤后截断、请求键对账，统一传到 diagnostics 和模型可见提示。
- 收口跨 agent/跨执行代的资源租约、流式 model 退役、有效配置回执和回滚；冷启动/热更沿用各 agent 身份，不复用 entry 的全部资源。
- 在已有 RL 认证上补请求/队列上界、取消、端点更新错误契约；通过既有部署形态落实 OS 隔离，不自建命令安全解析器。
- 卡片浓缩增加机器票据校验；性能优化先建离线基准，保留 token 单维触发、冻结渲染与声明恒定 MCP。
- 修复 race 包装器假绿；补独立进程恢复、故障注入、长跑、双模块 CI 和规范语义一致性门禁。
- **BREAKING（行为收紧，签名尽量兼容）**：内存存储拒绝重复键/无分区查询；可靠接收失败不再假成功；RL 非法/超限请求拒绝；不兼容的共享资源配置拒绝。新增返回结果的 API 供新消费者使用，旧入口保留并显式告警。

## Capabilities

### New Capabilities

- `runtime-resource-ownership`：组合根资源租约、跨实例关闭、多 agent 隔离和冲突配置检测。
- `resident-release-evidence`：分阶段验收、负向验证器测试、独立进程/长跑证据与发布权限边界。

### Modified Capabilities

- `event-segment-store`：事件提交耐久、错误传播、后端契约一致性与重启计数恢复。
- `event-sourced-projection`：消除 no-op 冲突条款，结构化恢复结果与可见性、过滤后有界回放。
- `persistent-event-loop`：显式 volatile/durable 接收语义、有序 inbox 确认、终结态生命周期。
- `swappable-executor`：按 agent 身份绑定、流完成后退役、有效配置与配置快照回滚。
- `task-skeleton-compression`：浓缩票据校验及确定性失败回退。
- `rl-feedback`：有限控制面、请求取消、接收回执和安全端点更新。

## Impact

- 代码：组合根 `tagent.go/build_agent.go/wiring.go`，`agent/`、`memory/`、`plugin/`、`rl/`、压缩与召回消费者；测试同步覆盖 `examples/wechat-bot` 独立模块。
- 数据：事件原文保持唯一真源；inbox 仅保存待提交输入及处理状态，不能成为历史/投影第二真源；不自动更改 EventKey 布局、TTL 默认值或已有原文。
- 依赖：默认不新增数据库、消息中间件、tokenizer 或安全运行时；复用现有 KV、文件、OS 隔离与测试接口。
- 兼容：保留旧读取格式；可靠 inbox 采用独立版本目录，升级前排空旧 spill；存储/资源行为收紧提供明确迁移错误，禁止静默降级。
- 发布：本轮只交付方案；实施分 WP0–WP7，发布候选以功能门和长期证据门为准。真实部署、消耗模型额度、提交、推送、打 tag、发布和上游提单均需要另行授权。
