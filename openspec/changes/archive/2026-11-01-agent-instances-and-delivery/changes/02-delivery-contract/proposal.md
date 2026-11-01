# 02 delivery-contract：投递契约对齐与账目（G2+G3）

父变更：agent-instances-and-delivery（依赖：01 的账目消费面——实现依赖；2.1/2.3 可先行）。

## Why

9.1 预期"插件派发与模型工具共用目标解析/范围校验/身份去重/落账/唤醒"，实现只共用落账：两路投递元数据不同构（`TypeExternalInput`+`trigger_source:"send"` vs `TypeFeedback`+`trigger_source:"task"`）、插件路径无范围门；且存在第三套投递门 `NewSendFace`（无生产调用方，实现偏差产物）与第一套冲突。多目标投递无逐目标账目。裁定（Q4=b/Q10=a/Q11=b/Q12=b）：**事分立、账对齐**——事实类型保留区分，投递元数据契约同构；删 `NewSendFace`；单目标调用保持，补投递账目。

## What Changes

- **投递元数据契约同构**：两路投递事实的目标/投递身份/回执键/来源分账键同构，去重与回执规则一致（事分立账对齐）。
- **插件路径补范围门**：SettleDriver→fireNoticeTurn 路径补 deliver_to 类声明式白名单（fail-closed、具名拒绝），与 send 四门白名单档同构。
- **删 `NewSendFace`**（DEL：`runtime/state/send.go` 第三套门）；其拒绝账本概念若可取则小步并入第一套。
- **投递账目**（G3）：一条源事件 → 多次单目标投递 → 逐目标投递身份与回执，可查可对账。

## 边界与依赖

- **依赖**：01（账目消费面挂常驻实例）；`assembly/host_tools.go`/`runtime/state/send.go` 独占。
- **被依赖方**：03（golden 的逐目标恰一次断言读账目）。
- **接口面**：投递元数据键（`MetaKey*` 词表）、`hostMeditationPort` 四门、账目查询面。
- **禁止事项**：不合并两类事实类型（Q11=b）；不扩 send 参数（Q12=b）；不动 settle 恰好一次；不改四门的 fail-closed 语义。
