# 05-inspection — 设计（design.md）

> 二级子变更技术设计。只写"怎么做"的规划；本轮不写产品代码，签名未经探针不称冻结。上游只读计划 §4.2/§4.5/§7（05 行）。

## Context

- 现场：无 Query/观测产物；`go.mod` 已直接依赖 `go.opentelemetry.io/otel` + `.../trace` + `.../metric`（v1.38.0、v1.38.0、v1.38.0）。
- 设计闸 G0–G3 为实施共同前置；本域**只读消费** 01/02/03/04 事实，不复制其写入。
- 本域新增 `runtime-inspection`：现有 registry 无"授权过滤的只读运行总图"（非重合论证见 `proposal.md`）。

## Goals / Non-Goals

**Goals**

- 只读总图：agent/session/invocation/task/notice/租约；统计**从明细派生**。
- 授权过滤 + 不可变快照（有界 revision）+ 稳定分页 + 订阅 gap/reset。
- 观测旁路：提交后插件接 OTel/审计；**关闭 OTel 不影响 Query**。
- 阻塞解释：每条阻塞有**下一推进者**。

**Non-Goals**

- 不做统一大事务/万能 Store；不做跨后端原子快照；不修改授权事实。

## Decisions（含被否方案）

- **D05-1 只读投影，非写面**：Query 仅读；`GrantRead` 等授权事实**只读消费**。被否：Query 兼做授权变更。理由：计划 §4.5。
- **D05-2 统计派生自明细**：configured/resident/busy 等 SHALL 从明细计数派生，MUST NOT 把多维状态相加成"运行数"。被否：单一聚合计数。理由：计划 §4.5。
- **D05-3 有界快照 + 游标绑定**：Snapshot/List/Detail 共享**有界快照 revision**；游标 SHALL 绑定 revision 与过滤条件；旧窗口过期、订阅溢出、删除、同状态字段变化均可解释（具名）。被否：无版本游标（翻页期间语义漂移）。理由：计划 §4.5。
- **D05-4 报告水位与滞后，不夸跨 store atomic**：Query SHALL 报告**源水位与滞后**；MUST NOT 宣称跨所有后端原子。被否：声称全局一致快照。理由：计划 §4.5。
- **D05-5 观测旁路、不拥有状态**：OTel/审计在**提交后**接入；观测者失败记缺口、不回滚事实、不重跑工具；**关闭 OTel 仍可查询**。被否：观测者参与提交/回滚。理由：计划 §4.2/§4.5。
- **D05-6 阻塞可解释**：每条阻塞 SHALL 给出**下一推进者**（哪个执行者/通知/存储动作推进它）。连续 `unknown`/诊断升级 SHALL 可解释为**推进动作**，MUST NOT 以次数阈值（如 N=3、默认 3）判死；未止义务与资源租约 SHALL 保留且可解释。被否：只报"卡住"。理由：计划 §7（05.4）。

## 字段 / 端口所有权

| 面 | 所有者 | 本域角色 |
|---|---|---|
| session/mailbox/消费位置 | state（01） | 只读消费 |
| task/notice 阶段 | task（02） | 只读消费 |
| 提交/水位/墓碑 | history（03） | 只读消费 |
| 拒绝账本/阻塞源 | capabilities（04） | 只读消费 |
| Query 快照/游标/订阅 | 本域 `runtime-inspection` | 持有（只读投影） |
| OTel span/metric、审计 | 本域观测 | 持有（旁路、可 noop） |

## 并发 / 取消 / 生命周期 / 错误 / 配置（本域切片）

- **并发**：订阅多消费者独立游标；溢出具名 gap/reset；快照不可变（发布后不变）。
- **取消**：订阅取消/游标释放不泄漏等待者（对齐 long-poll 惯例）。
- **生命周期**：快照有界；过期窗口可解释；OTel 关闭为合法态。
- **错误**：源不可读返回具名错误/水位告警，不伪报空。
- **配置**：OTel 关闭 = noop；订阅溢出阈值可配。

## 风险 / Trade-offs

- [跨 store 非原子被误读] → D05-4 报告水位/滞后 + 场景断言。
- [观测者失败污染事实] → D05-5 缺口记录，不回滚。
- [订阅静默漏报] → D05-3 具名 gap/reset + 断言。

## 待核（Open Questions）

1. 一致 revision 的**水位来源**（05.2）。
2. 订阅 gap/reset 具名语义与溢出阈值（05.2/05.4）。
3. OTel 关闭下可查维度的明示边界（05.3）。

## 文件清单（未来候选写域，**未存在亦未获批**）

- 产品：`query/`（只读投影 inventory/snapshot/cursor/subscription/detail/explain；**已在 `golden/runtime/architecture.json` 登记为可选包 `query`（required=false）并含显式边**，G1 未冻结不强制目录存在，也不塞回组合根做业务大 Store）、`telemetry/`（OTel 旁路）、`plugin/`（提交后观测接点）。
- 测试：`tests/integration/inspection/{runtime_inventory,query_auth_cursor,observation,blocker_explain}_test.go`。
- 描述性主规格目录的 delta：`specs/{runtime-inspection,turn-tracing,trajectory-trace-correlation,self-telemetry-audit,committed-event-attribution,degradation-behaviors}/spec.md`。
- **本轮只写本目录 `changes/05-inspection/**` 的 md**。
