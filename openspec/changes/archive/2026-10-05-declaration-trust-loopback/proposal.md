# Proposal: 回环源视为已鉴权，受理入站意图声明（declaration-trust-loopback）

## Why

上一批的意图声明能力带一条受理前置：端点配了 auth token 才受理。这漏掉了最常见的默认部署——**服务只监听 127.0.0.1 且无 token**（外部本就不可达）。后果是双面的：同机 mail-poller 不声明（无凭）⇒ 邮件轮血统留在 `http` ⇒ ① 合法用户的邮件轮产出永久不可投递（复现案 2 病灶），② 每轮触发 `undigested-lineage` 回执（远端已报告噪声）。噪声与静默是同一根因的两张脸，必须修根因而非压症状。

## What Changes

- **受理条件扩一条**：`authToken != ""` **或** 请求源地址为回环（127.0.0.0/8、::1）。非回环且无 token 的形态照旧 400 拒声明，行为逐位不变。
- **可归因**：经回环豁免受理时打一行 INFO（`declaration-source=loopback`），滥用可见可查。
- **poller 同步**：声明条件由「携凭」改为「携凭 或 目标 URL host 为回环」，默认部署零配置即通；跨机部署仍要求配 token（避免 400 与注入重试循环互锁）。
- **明确不做**（理由见 design 否决区）：回执聚合去重/“内部播报不回执”、退役任务结果机械补发、新增信任配置开关。

无 BREAKING：默认（有 token）路径不变，仅放宽本机源；`DeliverableLineage` 白名单与投递分支零改动。

## Capabilities

### Modified Capabilities

- `inbound-intent-declaration`: 「声明受理 fail-closed 三重校验」需求修订为两条受理前提（auth 或回环源），+2 scenario（回环受理、非回环无凭仍拒）。
