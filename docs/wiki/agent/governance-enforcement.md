> **本篇是当期契约**：代码注释 `// 契约: docs/wiki/agent/governance-enforcement.md#<锚>` 指向本页各节。实现面在 `features/evolution/governance/`（`policy.go`/`risk_rules.go`/`budget.go`/`denial.go`/`goal.go`）与 `runtime/task/approval/`（`approval.go`/`approval_store.go`），抽象面在 `api/plugin/governance.go`；装配见 [docs/EXTENSIONS.md](../../EXTENSIONS.md)。

# 治理：一次执行前裁定，三种处置，两本账

治理只做一件事：在工具副作用之前把一次调用**裁决一次**。这一次裁定同时是执行决定与记账决定——不留第二次打分，也不在执行后补判。它是可选层：`Open()` 才把门装起来，零值与 nil 是「没开」，每个 call 一律放行且无裁定义务。

<a id="enforcement-modes"></a>
## 一、两种执行档：违规是否真被拒，只由这一档决定
`Enforcement` 只有两个值：`warn`（缺省，违规被记账并暴露、**不阻断**）与 `strict`（违规判为拒绝）；`Policy.Mode()` 对没装的门答空串，对装了而未选档的门答 `warn`。**「是不是真拒绝」由这一档单独决定**，规则只回答「这是不是违规」。三处拒绝不是违规，因此与档无关、各自独立成立：预算窗口耗尽（`WithBudget`）、关键档取不到批准或批准被拒（`WithApprovals`）、要求目标的触发源下没有在途目标（`WithGoals`+`WithGoalRequired`）。

<a id="disposition-and-risk"></a>
## 二、风险四档与处置三选一
风险是 `low`/`medium`/`high`/`critical` 四档；处置是 `allow`（放行，无额外留痕义务）/`record`（放行，把这次执行记进审计账本）/`hold`（副作用前拦下，交批准通道，获批后才跑内层）。缺省映射在 `policy.go:dispositionFor`：`low`→`allow`、`medium`/`high`→`record`、`critical`→`hold`——**关键档恒走批准**，放行策略跳不过它。分级输入是纯数据（工具名、参数 JSON 字节、触发源），规则是纯函数匹配；一次 pass 没带 grade 时读作 `RiskMedium`，声明表 `RiskRules` 再兜底，选择序为**精确名 > 最长前缀 > 策略缺省档**——空档不是一种档。

<a id="approval-channel"></a>
## 三、批准通道：外部裁决，一个请求一个身份
状态四态 `pending`（等待裁决，被治理的操作不执行）/`approved`/`denied`/`expired`（**超期未裁决按未获批准处理，迟到的裁决不得让它重新可跑**）。请求经 `ApprovalChannel.Deliver` 交外部审批者，三条红线：一 call 一请求、以请求自带的批准身份为键；**投递失败不得读成裁决**（门继续等）；不得改写交给它的请求。失败落成可见的 `DeliveryGap`，既不吞掉也不当放行。请求与参数用 `ArgsDigest`（SHA-256）绑定：裁决绑在**被拦下的那份参数**上，换参数再批即拒。任务层的那份等待凭据是 `task.Hold{TaskID,ApprovalID,ExpectedDigest}`——回执只报「在等」，不读成终结。

<a id="budget-epoch"></a>
## 四、预算与 epoch：快照带 epoch 一起持久化
窗口缺省一小时切六桶，每窗放行 20 次高档、200 次中档（`BudgetConfig`）。给了目录就把快照写 `<dir>/budget.json` 并从中恢复 epoch 与桶；读回时快照缺失、解析失败、或其中 epoch 为 0 一律**不覆盖**当前 epoch——反之让缺失值把 epoch 归零，累计消耗会被读成「新开始」，预算门形同虚设。

<a id="denial-ledger"></a>
## 五、拒绝账本：拒绝是一等资产
一次拒绝留两样东西：账本记录（模型据此自纠）与一条治理事件（宿主与观测面据此看见）。事件按 `subtype` 分 `denial`（被拒）/`approval`（被拦待批）/`audit`（放行但留痕），每个族一条有序流（`tagent/governance/v1/denial:`、`.../goal:`），键的字典序即发生序，重放还原同一序。存储面**迟绑定且 nil 安全**：没挂存储时退化为内存态而失败不了，挂上时把已存事件回放进索引，重启不丢记录；写入三红线——按族键、`Sync` 返回 nil 前不得报持久、不得改写或丢弃已存事件。

<a id="goal-registry"></a>
## 六、目标登记：四态可判定，只写两态
四态 `active`（在途，占预算并参与门禁）/`achieved`（达成，不占预算、不参与裁定）/`abandoned`（主动放弃）/`expired`（超期未达成）。**只有两个可写**：目标显式解析为 achieved 或 abandoned，而 `expired` 由 `ExpiresMs` 派生——过期只是不再算在途，登记表从不改它的状态，统计与预算归因因此分得开「超期」与「放弃」。登记表可从事件重放重建、并发安全、无存储时退化为内存态；要求目标的调用在无在途目标时按名拒绝，并把当时 `GoalID` 渗透进账本记录，审计据此读出这次越权落在哪个自治范围里。

## 已知缺口与演进方向
| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| `warn` 是缺省档 | 「接了治理但没配 strict」的系统实际不阻断任何事，这一事实只能从配置读出，运行期没有可见信号 | 启动期一行 INFO 报出有效档与已装门清单 |
| 批准依赖外部通道与轮询 | 有 `DeliveryGap` 与 `ArgsDigest` 绑定，「谁批的、依据什么」仍散在请求文件的自由文本里 | 把审批者的裁决理由做成结构化字段并随治理事件落链 |
| epoch 语义只由测试钉住 | 缺一次跨重启实测报告（进程重启 vs 整机重启各一腿） | 按 `docs/upgrade-rollback-drill.md` 的口径补一次演练记录 |
