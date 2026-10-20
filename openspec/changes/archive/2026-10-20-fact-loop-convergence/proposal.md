# Proposal：事实-消费-时态 化归收敛

## Why

用户判据（2026-10-19）：框架全部能力应化归为围绕 **event（事实）/ event loop（消费）/ plugin（相位扩展）** 的实现——其余包是事实的三种时态（存续/在途/回放）、执行器（model/tool）、平行事实流（rl/telemetry/workspace/evolution）、边界（config/host）。同时：**事件不可变、产出即锁定；进入管线后都是投影；plugin 只操作投影**。

审计（实证）确认现实现**部分**按化归组织，四处结构级偏差：

1. **事实词汇分裂**：event/task/rl/config/workspace 五包各持事实类型，无统一"事实类别"入口；
2. **消费器多形未裁决**：Loop.Drive（回合驱动）与 Reflection.Tick（时间驱动）并列，拓扑未声明；
3. **呈现视图住扩展包**：Presentation/Gap/View（时态视图产物）类型在 plugin 包，与相位扩展同居；
4. 写路径 DTO（CommitRecord）在 task——端口模式可接受，登记即可。

## What Changes

- **01 事实词汇统一**：event 包声明事实类别契约（实现者可自证"我是事实"），五个事实族（Event/Notice/训练记录/Declaration/Ticket）在各包内实现之；`docs/FACT-VOCABULARY.md` 词汇表（化归判据的落地载体）。
- **02 消费器拓扑裁决**：裁决=**多实例同构合法**（Drive=回合驱动、Tick=时间驱动、host=外部驱动，同读事实→消费→产事实）；补"消费器协议"声明＋Tick 协议测试（不改 Tick 结构）。
- **03 呈现视图归位**：新建 `view` 包承载 Presentation/Gap/View（时态视图词汇）；plugin 保留扩展接口并引用 view；run 族引用更新。
- 不搬包（域边界=语义单元）；不重写行为（纯概念归属与契约）。

## Capabilities

### New
- `fact-vocabulary`：事实类别契约（自证接口）与五族登记。
- `consumer-topology`：消费器协议与三实例拓扑声明。
- `presentation-view`：时态视图词汇归属（view 包）。

### Modified
- `call-subagent-config`（呈现面引用）、`action-tool-config`（无行为变化，仅类型归属）。

## Impact

event/task/rl/config/workspace（各加 1 方法）、evolution/agent（文档+协议测试）、plugin（present 类型迁出）、run 族（引用）、新 view 包。风险：类型迁移触碰 plugin/run 消费面——以 compile 门兜底；语义零变化（纯归属）。