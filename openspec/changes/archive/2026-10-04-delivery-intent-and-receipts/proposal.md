# Proposal: 投递意图声明与统一回执（delivery-intent-and-receipts）

## Why

2026-10-04 两起同族事故（邮件定谳在案）：① 冥想轮派生的交付型任务结算被 meditation 分支静默扣留（感知成功/发送未发生，18 小时无人知）；② email-inbound 轮产出被 deliverable 白名单静默消化（"http" 系框架 HTTP 适配层硬编码的传输标签，`rl/http_api.go:539`），用户双通道状态同步丢失。共同本质：**血统是"注入时刻的环境侧写"而非"声明的意图"**（案 1 侧写执行环境、案 2 侧写传输通道），且**一切扣留终态静默终结**——唯一能在运行时自救的一方（agent）拿不到投递结局，事后取证层面：探针实测（入 change 目录 `fail-before.log`）表明结算事件自身血统并未丢失——`source_snapshot` 全量无损入链（部署版同然）——但一级键 `trigger_source` 写入的是回合级空串（`buildBusFact` 无条件写 `cm.triggerSource`），直接读一级键会被误导为“无血统”，真实值必须知道快照机制并解码才能拿到；事故当时的取证失败正是踩中此陷阱。

## What Changes

- **P1 注入声明**：`POST /task` 请求体增加可选 `trigger_source` 字段——受信入站集成（mail-poller）声明 `"user"`；仅当端点 auth 已启用时受理，无 auth 或非法值一律 400 fail-closed；缺省行为不变（`http` 机械标签 → 白名单外消化）。声明写入事件 `Metadata[trigger_source]`（`extractTriggerSource` 现行 `firstLineage=="user"` 路径自动生效），`Source` 保留通道值供遥测；`DeliverableLineage` 白名单一字不动。mail-poller（本仓 `examples/wechat-bot/mail-poller/`）同步声明。
- **K2 统一投递回执**：宿主分发层（`examples/wechat-bot/main.go`）一切预期外静默终态回流 agent 语境——已送达不回执（agent 自证可观察）；SendText 失败 ERROR 回执（新覆盖的第四裂缝）；未知/未声明血统消化、冥想·交付特征扣留、error 分支、无目标扣留四类 WARN 回执；冥想纯叙事维持契约内静默（不给 agent 刷屏）。回执经持久总线注入（转生后仍在账上），`delivery_receipt` 血统天然非投递（回执轮自身输出静默）、不武装冥想新颖门；**回执不递归**（防自激红线）。
- **K3 结算血统一级化**：① `persistBusEvent` 持久化 `SourceTask` 事件时，把事件自带 `Metadata[trigger_source]` 以独立键 `settle_trigger_source` 提升为事实链一级可读键（快照原样保留）；② 修 `buildBusFact` 空串陷阱：回合级 `trigger_source` 为空时 SHALL NOT 写入空串一级键。
- **明确不做**（否决理由见 design）：白名单扩通道（通道≠意图，/task 是通用面）；机械补发 pending（辩证结论无安全域——声明过的直达、未声明的恰是不能自动发的）；投递门内容推断（内容特征只参与回执分级，永不参与投递裁决）；A2 引用式承接（待办记录形态需独立设计，后续另立）。

## Capabilities

### New Capabilities

- `inbound-intent-declaration`: 入站集成在注入边界声明意图血统的 API 契约——受理条件（端点 auth）、合法值域（当前仅 `user`）、fail-closed 缺省、声明与通道标签并存。
- `delivery-dispatch-receipts`: 宿主分发层投递终态的回执矩阵——七终态分级表、回执的持久化与内部血统、防自激、交付特征仅用于分级。

### Modified Capabilities

- `event-metadata-contract`: 事实链持久化新增结算事件自身血统（`settle_trigger_source`），溯源不再依赖代码反推。
