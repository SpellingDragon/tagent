# event-lifecycle delta（03-history）

> 复用登记（非规范，不改本条语义）：TTL/容量淘汰只写墓碑键、物理删除推迟到压实；生命周期策略经声明式配置注入，与保留租约协调。
> 本 delta 只修改「生命周期策略经声明式配置注入」；本能力其余 Requirement（Time-based TTL expiration、Capacity-based eviction、TTL 扫描为周期性全量扫描、容量淘汰基于进程内事件计数与最旧段优先）由主 spec `openspec/specs/event-lifecycle/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 生命周期策略经声明式配置注入

`LifecycleConfig` SHALL 可经 agent 的 memory 配置（YAML）注入，至少覆盖 `global_ttl_days`、`type_ttl`（按事件类型覆写，负值 = 豁免）、`check_interval`、`max_events_per_partition`。组合根 SHALL NOT 硬编码 `DefaultLifecycleConfig()`——未配置项回退默认值，显式配置优先。

生命周期策略 SHALL 与保留租约协调：淘汰/墓碑动作 MUST NOT 物理销毁受保留租约保护的原文（见 `event-segment-store`「未确认恢复材料的有限保留租约」与 `storage-lifecycle-completion`「保留保护与回收协调」）。声明式注入的档位 SHALL 与组合根的实际构造一致，MUST NOT 存在硬编码第二份默认值绕过配置。

#### Scenario: YAML 覆写全局 TTL

- **WHEN** agent 的 memory 配置声明 `global_ttl_days: 30`
- **THEN** LifecycleManager SHALL 以 30 天为全局 TTL，而非默认 7 天

#### Scenario: 未配置时沿用默认

- **WHEN** 配置中未声明任何生命周期字段
- **THEN** SHALL 沿用默认值（全局 7 天、类型 TTL 默认表、检查间隔 1 小时、容量不限）

#### Scenario: 可显式关闭 TTL

- **WHEN** 配置声明 `global_ttl_days: -1`（或等效的关闭语义）
- **THEN** TTL 扫描 SHALL NOT 标记任何事件为墓碑

#### Scenario: 策略不绕过保留保护

- **WHEN** 一个受保留租约保护的 key 到达 TTL/容量阈值
- **THEN** 生命周期策略 MUST NOT 物理销毁其原文，仅在其类型/年龄窗口内保持受保护
