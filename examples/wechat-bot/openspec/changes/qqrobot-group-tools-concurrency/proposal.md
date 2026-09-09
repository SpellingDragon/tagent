## Why

群 @ 管线已修复（GROUP_MESSAGE_CREATE + is_you 门控，robot.go:219-287，pid 3739707 验证通过），现在要在此基础上补齐群管理能力、聊天触发与并发处理：用户要求把"有害信息→禁言1分钟+说明安抚"工具化（mute 注册为 LLM 可调用工具，由 LLM 在聊天管线自主判断调用，不做硬编码审核流），扩大群聊触发面并防止无效 LLM 调用，同时将消息处理并发化。

**2026-09-08 范围调整**：SDK fork 升级（botgo v0.0.9→v0.1.0）因 GitHub 网络四路全败（git@ SSH 挂起、HTTPS clone/tarball 超时，仅 ssh -T 认证通）DEFERRED，用户指示"SDK 拉不下来就先不优化 SDK"，网络恢复后另立 change。mute 实现路径从"fork 补 v2 封装"切换为**主工程内 v2 REST 直调**（net/http 按 `kb/api/qq/group_restrict_chat_setting.md` 契约，鉴权复用 robot.go:90-96 既有 token.QQBotTokenSource），不动 SDK。

## What Changes

- **Capabilities**（对应 specs/ 下四个能力，其中 botgo-sdk-v2 已转 DEFERRED 记录）：
  - `llm-group-tools`：mute 注册为 eino 工具，底层 v2 REST 直调 `POST /v2/groups/{group_openid}/restrict_chat_setting`（action_type add/del + member_openid），description 承载"说明与安抚"指引（description 即 prompt 载荷），LLM 自主判断与调用；角色查询工具随 SDK defer
  - `botgo-sdk-v2`：**DEFERRED**——fork 升级 v0.0.9→v0.1.0 + v2 封装 + tag，网络恢复后另立 change（本 change 内保留 defer 记录与 D1-D6 队列）
  - `group-chat-gating`：群主 @ / 昵称触发聊天 + 防无效 LLM 调用（活跃窗口 / 冷却 / 预算）
  - `message-concurrency`：消息处理并发化——per-group 串行 + 跨群并行 + 有界 worker 池
- **契约修正**（依据 kb/api/qq/group_restrict_chat_setting.md 字段级检索）：正确路径为 `POST /v2/groups/{group_openid}/restrict_chat_setting`（网传 `PUT .../member_mute` 不存在）；接口为**白名单能力**（未开通报 11253），实弹前须小流量验证；v2 契约**无时长字段**（原"默认 60s / 上限 300s"语义不适用于直调路径，到期解除由平台语义决定）
- 构建 `bash build.sh`（go1.18 冻结工具链）；config.yaml 不入 git，敏感字段（cookie/token）注意脱敏
- 产物归档 kb/：kb/INDEX 与 kb/timeline 更新（含 SDK defer 记录）

## Impact

- 范围：QQchannelRobot 主工程（robot.go / service/react_tool.go / 新增 v2 直调客户端 / 新增并发分发器）；**不含 src/botgo fork**（DEFERRED）
- 最终产物：可构建二进制、kb/ 归档文档；验收含测试群禁言实弹一次（前提：v2 白名单验证通过；若 11253 未开通则记录结论、停用实弹项，工具层降级为口头警告优先）
