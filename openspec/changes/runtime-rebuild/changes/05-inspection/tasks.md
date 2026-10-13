# 05-inspection — 任务清单（tasks.md）

> 二级子变更 `05-inspection` 的三级孙任务。**全部未勾选**（`- [ ]`），**不继承**旧 change 完成数。
> 状态仅 `planned` / `design-blocked`；**本轮不声称 Code 实际 PASS**。
> 编号与只读计划 §7「05-inspection」行一一对应，每原编号一次。
>
> **验证命令基线**（仓库根执行）：
> - 确定性（并发族带 `-race`，重复 10 次 + JSON）：
>   `go test -race ./tests/integration/inspection -run '^<Test名>$' -count=10 -json`
> - 真实网络/付费资源：独立门控、单次 `-count=1`；未启用可合法 Skip（**不以 Skip 结项**）。
> - 判定口径：点名用例真实 `pass`；零匹配/`Skip`/包级 `Fail` 不算通过；不用 `grep/head/tail` 退出码替代原命令。
> - 产品实现单共同前置 G0–G3 不逐行重复。

## 前置与边界

- **上游**：01-state-binding（01.3/01.5）、02-execution（02.2）、03-history（03.3）、04-capabilities（04.2 拒绝账本/阻塞源）。本域只读消费。
- **写白名单（未来，本轮不写）**：Query/观测相关产品包与 `tests/integration/inspection/**`。**本轮只写本目录 md**。
- **禁止**：修改授权事实；跨 store 原子快照；把多维状态相加成"运行数"；观测失败回滚事实。
- 状态标签：`planned` / `design-blocked`。

## 任务

- [ ] 05.1 实现 agent/session/invocation/task/notice/租约的**只读运行总图**；统计从明细派生（不把多维状态相加成"运行数"） —— 状态：planned —— 前置：01.3、01.5、02.2 —— 候选文件：`query/inventory.go`、`tests/integration/inspection/runtime_inventory_test.go` —— 验证：`go test -race ./tests/integration/inspection -run '^TestRuntimeInventory$' -count=10 -json`

- [ ] 05.2 实现授权过滤、**不可变快照**、稳定分页、详情及订阅 **gap/reset**；游标绑定 revision 与过滤条件 —— 状态：planned —— 前置：05.1 —— 候选文件：`query/snapshot.go`、`query/cursor.go`、`query/subscribe.go`、`tests/integration/inspection/query_auth_cursor_test.go` —— 验证：`go test -race ./tests/integration/inspection -run '^TestQueryAuthorizationAndCursor$' -count=10 -json`

- [ ] 05.3 提交后插件接 **OTel/审计**，SDK 尝试观察分层；**关闭 OTel 不影响 Query** —— 状态：planned —— 前置：02.6、05.2 —— 候选文件：`telemetry/otel.go`、`plugin/observe.go`、`tests/integration/inspection/observation_test.go` —— 验证：`go test -race ./tests/integration/inspection -run '^TestObservationDoesNotOwnState$' -count=10 -json` —— **资源门（09.5 消费，归属本任务 05.3）**：真实 collector 接收 span/metric 由**点名用例** `TestRealCollectorDelivery`（同包 `tests/integration/inspection`，05.3 实现 + `09.5` 验收）在启用门+授权+`-count=1` 单次运行；确定性 `TestObservationDoesNotOwnState`（关闭 OTel 仍 Query 可用）不以真实 collector 为前提。**资源归属 05.3**——MUST NOT 以 `06.3` 的 RL 队列封账 `TestCaptureAdmissionAndSeal` 冒充 collector 接收。

- [ ] 05.4 在**重启/取消/退役/存储失败**下解释每条**阻塞及下一推进者**（含连续 `unknown` 仅升级诊断/显式停止处理、不作判死退出依据；未止义务/租约保留且可解释） —— 状态：planned —— 前置：03.3、04.2、05.3 —— 候选文件：`query/explain.go`、`tests/integration/inspection/blocker_explain_test.go` —— 验证：`go test -race ./tests/integration/inspection -run '^TestExplainRuntimeBlockers$' -count=10 -json`

## 缺口修复

（执行期缺口写在此节：场景 → 根源 → 影响面。契约变化先回写本域 design 与顶层计划。本轮无。）
