# 03 history — 二级子变更提案

> 一级变更：`runtime-rebuild`（schema `spec-driven`）。本文件是二级域 `03-history` 的提案。
> 二级嵌套不被 CLI 跟踪是**有意设计**：本域可整体提升为顶层 change 而结构无损；进度由一级 `tasks.md` 的域 checkbox 人工记账。
> 领域 = 责任与验收单位（计划 §6），不是代码包数量，也不是整域派发单位；派发原子下沉到就绪孙任务（计划 §8）。
> 本轮只产出 OpenSpec 规划产物；**不写产品代码/测试、不改残件、不跑产品 build/test、不安装/提交/归档**。

## Why

历史域承载「原文可准确召回、投影可按记录重建、超大结果先存原文再给有界副本」这条链，也最容易出现「投影变成第二真源」「原文与副本寿命混淆」「召回退化为概率路径」「保留保护与回收互相踩踏」等隐藏失效。清理后的工作树没有可运行的 memory/workspace 现场（计划 §1），旧包的类名与子包划分不可作为基线（计划 §1、§3）。本域把「原文引用 → 存储屏障 → 持久提交 → 投影回放 → 保留/回收 → 副本物化/清理 → 统一外置路径」定成可复跑的一链，使执行域（02）与能力域（04）只消费而不复制历史真源。

## What Changes

- 原文引用与引用元数据分离：owner/format/保留策略不同的引用互不覆盖；原文按字节精确召回与分段读取；不复活多套同义召回接口。
- 后端适配与**实际提交屏障**：localfile/file 档各自证明屏障；内存档复用同一业务合同但**明确不耐久**（不承诺进程恢复）；不新增数据库逃避合同设计。
- task/state 正式接入**持久提交与幂等接纳**，重放未确认投递并定位所属 owner；无需更换数据库。
- 原文、呈现决策、预算组装与**投影回放**进入实际 Agent 请求；投影恒等于事实链回放，无游离 checkpoint；因果链按 session 隔离。
- 保留/墓碑/恢复清点与回收协调在同一存储生命周期内；多持有者引用计数保护；保留端口按真实 consumer 收敛，持久档缺能力 fail-closed、非耐久档显式 capability。
- workspace **副本物化引用稳定**（跨 owner/tool/restart 不互覆盖、显式允许安全复用、多副本合法）、授权分段读与缺失重物化；原文有效性与副本寿命分开。
- 临时副本启动/周期清理与活跃读写保护：不删用户文件、不跨链接越界。
- 通用工具/任务结果/tmux/overflow 接入**同一外置路径**，并验证清副本后的独立进程恢复。

## Capabilities

### New Capabilities

本域不新增产品能力。新增义务以既有能力的 **ADDED Requirement** 登记：`storage-lifecycle-completion` 增「保留保护与回收协调」；`tool-output-interception` 增「工作区副本物化与引用稳定」「临时副本清理与活跃读写保护」；`recall-protocol` 增「原文引用与分段读取保真」。

### Modified Capabilities

- `event-sourced-projection`：修改「投影是事实链的纯回放（一等不变量）」「冷启动回放逐字节重建投影」。
- `recall-protocol`：修改「索引卡为召回标准协议（输入形态分流）」。
- `event-segment-store`：修改「LocalFileKV Sync 为原子快照屏障」「未确认恢复材料的有限保留租约」。
- `event-lifecycle`：修改「生命周期策略经声明式配置注入」。
- `causal-chain-isolation`：修改「Causal chain is isolated by session」。
- `task-registry-rebuild`：修改「恢复与显式重投按当前编排绑定」。
- `tool-output-interception`：修改「工具输出大小拦截包装器」。
- `rustviking-config`：修改「Production MemoryStore wiring uses real RustVikingClient」。

本域**无 REMOVED**。其余历史域相关能力（`event-relation-store`、`async-task-lifetime`、`memory-*`、`ttl-cursor-scan` 等）由主 spec 继承或他域落地，仅作**证据映射**，不出 delta、不声称已认证。

## Impact

- 实施候选写面（本轮不写）：`memory/*.go`（含 localfile/file 后端；包划分按 G1，不预建 `memory/kv` 子包）、`workspace/*.go`、`state/*.go`（持久提交接线）、`task/*.go`（恢复/重投）、`tool/*.go`（输出外置）、`tests/integration/history/**`。
- 只读消费：event 身份/原生载荷（02.1）、task 提交消费端口（02.2）、state 同代绑定（01.4）、config 声明档位（01.1）。
- 对外符号 `package tagent` 位于 **module 根**，不 nested 到 `tagent/tagent`。
- 组合根、主 spec、旧 change、外部计划、依赖、`scripts/` 均不在本轮写域。

## 边界与依赖

### 依赖（接口前置 vs 实现前置）

- **接口前置**（签名冻结即可开工）：00.3 G1 共享合同；02.1 事件身份/原生载荷与 task 提交消费端口**签名**；01.4 `StatePlan`/`AgentBinding` 签名。
- **实现前置**：03.2 ← 03.1；03.3 ← 02.2、03.2；03.4 ← 02.5、03.3；03.5 ← 03.3；03.6 ← 03.4/03.5；03.7 ← 03.6；03.8 ← 03.7、04.2。
- 03.1 仅依赖 02.1。
- 计划 §7 注：03.3 与 02.2 的跨包接线由同一集成单或串行实施，不由两个代理各造一个提交协议。

### 被依赖

- 04.4 接外置依赖 03.8；04.7 检索消费 03.4/03.5；06.4 授权导出依赖 03.3；07.2 文件直接生效与 03.3 的提交/恢复相关；05.4 解释阻塞消费 03.3。

### 接口面（本域最小冻结面，只登记不实现）

- 原文引用：`Ref{Key, Owner, Format, RetentionPolicy}`（owner/format 参与去重作用域）；`Recall(items:[{key,hint?}])`；分段读取。
- 存储屏障：`Sync() error`（按分区分片原子快照）、`Close() error`。
- 保留租约（保护/回收协调）：端口按**真实 consumer 所需**表达，具体名称/签名不冻结（DESIGN-GATE）；多持有者引用计数归零才解除、幂等；持久档缺能力 fail-closed、非耐久档显式 capability。
- 副本：workspace 物化引用稳定 + 安全复用/多副本合法 + 活跃读写保护（MUST NOT 强制全局唯一副本；DESIGN-GATE：与 02.1 CallID 语义、04.x 工具外置对齐）。
- 阈值：`MaxTokens/2*4` 字符（与 `tool-output-interception` 既有公式一致）。

### 禁止事项

- 不建第二可写历史真源；不把投影态持久化到事实链之外的独立 checkpoint。
- 不新增数据库/WAL 逃避「不假设批量 API 天然原子」的合同；不为形式复用另造事实总线。
- 不为保留旧接口发明消费者；不按旧类名/子包数恢复旧代码。
- 不删除用户文件、不跨链接越界清理。
- 不提交/推送/归档；不跑产品 build/test；不触碰 Git index。
