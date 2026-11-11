# 配置活线（config-liveness）——链驱动

父变更：boundary-closure（W2；2026-11-05 用户报告立项，r18 终裁链驱动；前置=W1 入 main）。

## Why

探查实证（HEAD 48d8eb93；ORCHESTRATION 台账 r17）：配置真相目前是双轨——源文件一份、内存代一份，链上没有可回读的配置账。四个具体缺口：

1. **声明族（族 4）有登记无生产点**：`config.declaration` 的身份（规范指纹）与类型已存在（`api/config/fact.go`），Boot/Reload 却从不提交声明事实；链上只有逐维 `config_change` 流水，没有"这次发布的完整声明是什么"。
2. **`config_change` 无载荷**：事实只带维度与描述，正文为空；读者无法从链上判定声明内容，只能回读源文件或内存。
3. **发布记录内存化**：`state.PublicationRecorder` 端口在，`assembly/commit.go` 的 `memoryPublications` 自注"durable ledger 未接"——发布记录随进程消失，重启后无法对账。
4. **安装失败 /reload 仍回成功**（探针 `TestReloadReportsSuccessWhenTheModelRebuildIsRefused` 实证：新代已发布＋模型仍旧＋回执 200）：违反"写不进=明确失败告知"，是本轮必修的结构性缺陷（与路线选择无关）。

用户终裁 r18：**链驱动**——与交付义务上链（r1）、冥想状态机（r11）同一判据：**一条链一本账**。文件降格为编辑界面（单向流入），配置真相＝链上声明事实；发布入口消费声明事实；回执读账如实。

## What Changes

1. **声明事实获生产点与载荷**：Boot/Reload 装载后提交声明事件——Type 常量 `config.declaration` **驻 assembly**（循 01 的 `delivery.settlement` 先例，不动 `api/**`；`api/config` 既有 `Declaration.FactType()` 的冻结面保持只读复用），身份＝指纹（族 4 既有），Content＝规范声明序列化；大正文按既有内容寻址文件事实模式引用，不复制进事件。
2. **发布入口以声明事实为输入**：Reload 从链上最新声明事实构造候选；治理/基线/发布记录/Hold 流程原样保留；发布记录落为链上事实（接 `memoryPublications` 的 durable ledger 缺口）。
3. **失败语义上链**：安装/发布失败落带 result 的事实；`/reload` 回执读账如实——结构性修复"安装失败仍回成功"。
4. **watcher**：文件内容变更（hash 对比，mtime-only 不触发；防抖）→装载→提交声明→发布（同一唯一入口）。
5. **跨维顺序**：声明事实与发布记录带代号/前后关系；绑定代与模型版本两切换点的窗口内决策组合**账面可查**（最小实现：窗口事件可审计）。
6. **文件＝编辑界面（单向流入）**；重启＝Boot 读文件后与链对账（文件新于链→自动走发布；一致→静默；链新于文件→以链为准并记录文件滞后）。
7. **契约不变量（对外五条，r12 式）**：文件是界面链是账；变更必经唯一发布入口（治理保留）；回执如实（失败必达账面）；声明事实带载荷可审计；重启对账不漂移。

## 边界与依赖

- **前置**：W1 入 main（避开 01 的 assembly 窗口）。
- **接口面**：不动 `api/**`（载荷经既有事件 Content 字段；类型常量驻 assembly）；不动交付/冥想域文件。
- **规格面**：本域含两个 delta——`specs/config-liveness/`（ADDED 五不变量）与 `specs/config-hot-reload/`（MODIFIED「配置热更维度矩阵」，头逐字匹配主 spec）。
- **禁止事项**：不动 `assembly/host_delivery.go`、`request.go`、`loop.go`（01/02 域）；不改世代机制本身（发布入口的 Hold/换代语义原样保留）；不引入 fsnotify 依赖（hash 轮询优先，go.mod 零新增）；本域不设实验开关（按新设计恒开）。
