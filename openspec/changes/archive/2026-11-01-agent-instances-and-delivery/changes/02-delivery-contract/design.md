# 02 技术要点

## 要点

1. **事分立账对齐**（D7）：两类事实类型不动（`TypeExternalInput`=agent 主动投递、`TypeFeedback`=框架状态变更）；新增/对齐的投递元数据键：目标、投递身份（投递方调用帧/Notice 身份各自稳定）、回执键、来源分账键——两路同构且走 `MetaKey*` 单点词表。
2. **删 `NewSendFace`**（D8）：`runtime/state/send.go` 的第三套门+拒绝账本整体删除；若其"拒绝留账"概念可取，以注释/小步并入 `hostMeditationPort` 四门的拒绝路径（拒绝不静默，具名+留痕已是第一套语义）。
3. **插件路径范围门**（D10）：`fireNoticeTurn` 前置 deliver_to 类声明式白名单（按投递方 agent 声明的目标集），fail-closed；与 send 四门白名单档共用同一判定函数（结构性重复才收口）。
4. **投递账目**（D9）：源事件 ID → 投递记录（目标、投递身份、回执状态）；账目查询面（按源事件/目标查），持久随事实层；对齐 9.2 表"事件投递"核对点。
5. **I2 锚的关键断言**：同一条事件两路投到同一目标，账目读出的元数据字段集合同构、去重键规则一致（事不同账同）。

## 文件清单

- 改：`runtime/state/send.go`（删）、`assembly/host_tools.go`（元数据同构+账目）、`assembly/settle_turn.go`（范围门）、`api/event/meta.go`（词表）。
- 测：I2/I3 锚、范围门负锚、账目查询测试。

## 风险与回退

- 风险：`fireNoticeTurn` 补门可能拦住既有 settle 续跑路径（范围声明缺失时 fail-closed 会误拒）——迁移期给既有调用方补声明，负锚钉"空表拒绝"不变。
- 回退：单域可独立回滚；`NewSendFace` 删除为无消费者删除，回滚零影响。
