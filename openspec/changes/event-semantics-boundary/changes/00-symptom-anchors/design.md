# Design — 00-symptom-anchors（症状直译锚）

## D0 结论

本域的完成标准不是"三条测试写出来了"，而是：**三条测试在修复前的代码上确定性失败，且失败点正好落在用户原话的那一层**。因此每叶的红证必须是"命令 + 失败断言原文"，不是"跑过了"。用户世界（outbound）能红就红在用户世界；只有当脚本模型无法诚实地产生该行为时，才把红证收敛到模型世界（模型真实收到的请求），并**白纸黑字记录 fake 模型下不红**（R33/R34：规模与自洽性都不替代症状）。

## D1 两个观测面（症状测试只允许在这两处取数）

| 面 | 取数口 | 证明什么 | 不证明什么 |
|---|---|---|---|
| 用户世界 | `rig.recorded()` → `outboundEvent{Kind,Target,Body}`，按 `Target` 过滤到本测试的会话 | 用户实际收到几条、什么话术 | 系统内部为什么发 |
| 模型世界 | `provider.recorded()` → `smokeModelRequest.Messages`（脚本模型**真实收到**的请求） | 模型被喂了什么素材 | 用户收到什么 |

补充：`provider.count()` 只作"请求发生过"的计数证据；`installSmokeLog` 的日志只作失败定位的辅助，**不得**作为任何一条断言的主证据（日志是旁路，`consumerDebug`/`assemblyDebug` 的沉默不代表行为正确）。

**禁止取数**：产品内部结构体字段、golden 文件、测试手工拼装的中间态。三条测试跑的是 `newWechatApp → Boot → bindWechat → bindChannel → registerHandlers` 的正式装配。

## D2 三条症状的红证形态（含"不红"的诚实边界）

### G1 受理静默（症状 2 原话："进入初始派发状态应该静默"）

- **用户世界断言**：委派被受理后、任务结算前，本会话 outbound 事件数 = 0。脚本模型在收到受理回执后回**空正文**（空正文不进 `freshAssistantOf`，不产生投递），因此该断言测的是系统而不是模型。
- **模型世界断言**：受理后的下一次请求里，本次调用的 tool 结果 MUST 恰为 `ok`；MUST NOT 含 task id、`(running)`、`background task`、`will arrive as a notification`。
- **当前红证形态**：用户世界**预期绿**（现状系统不为受理回执单独发用户消息）；模型世界**确定性红**——`runtime/agent/loop.go` 的 `admissionText` 返回 `accepted <taskID> (running): the call runs as a background task and its result will arrive as a notification when it settles`。
- **诚实边界**：本叶的红证由模型世界承担；用户世界若绿，`t.Logf` 如实记录"fake 模型下用户世界不红，真机形态见一级 proposal 症状 1/2"，不得为了让用户世界红而把脚本改成复述回执（那是伪造红）。

### G2 完成恰一条（症状 1 原话："后台任务摘要后又发多余确认"）

- **用户世界断言**：从受理到结算的整轮里，本会话 outbound 文本数 = 1，且该条 MUST NOT 命中确认词表（`accepted` / `(running)` / `background task` / `will arrive as a notification` / `reports progress` / `[send_ok]` / `delivered to`），MUST 等于委派任务的结算正文。
- **当前红证形态**：2 条——脚本模型对受理状态素材做出了应答（`delegate ack`），随后结算跟进轮再发 `settle-ack`。首条正是"系统把内部确认当素材诱导模型复述"的机械复刻（一级 proposal 2026-10-24 微信实录）。
- **诚实边界（必须写进测试注释）**：脚本"见到状态素材就应答"是**行为仿真**，不是产品缺陷本身。若评审否决该仿真，本叶的诚实结论是"fake 模型下用户世界不红"，红证退回 G1 的模型世界断言，并在叶内记录真机形态与时间戳；**两条出口只能选一条，不得同时省略**。

### G3 失败必告知（症状 3 原话："模型失败需要通报用户感知"）

- **用户世界断言**：provider 对指定标记返回 500 后，本会话 outbound **恰好一条**失败告知，且正文 MUST 含原始错误标识（`500` 与注入文案里的 `smoke` 片段之一），MUST NOT 只含柔化系统话术。
- **当前红证形态（两条真实路径，均记录）**：
  1. **smoke 路径（本叶实际跑法）**：outbound 收到的是示例宿主的固定降级文案 `执行出错：本轮模型回合失败，未产出交付内容。`（`examples/wechat-bot/wechat.go` 的 `hostRuntime.Drive`），**不含任何原始错误** → 断言"含原始错误"确定性红；断言"恰好一条"当前为绿（一条罐头）。
  2. **真机路径（一级 proposal 20:15:55 证据）**：`Turn.Terminal` 为失败终态且无可交付正文，`Host.deliverFresh` 只写 `assemblyDebug` → **用户零消息**。
- **为什么两条路径不同**：`/task` 注入面直接调 `host.Invoke`，不经过 `Drive` 的降级文案；生产入站路径（`rig.drive`）才经过。G3 必须走 `rig.drive`（生产入站），否则会漏掉罐头文案这条现实。
- **后续约束（上报项 P-02）**：G3 的"恰好一条"在 02.1 落地框架直投后会变成 **2 条**（框架原文 + 示例罐头）。所以 02 域必须同步收敛示例层文案，否则本叶由红转绿会卡在"两条"。本域只把这条约束写进 G3 的断言与注释，不改示例代码。

## D3 测试设计（三条，一个文件，一次写入）

| 叶 | 父测试 | 注入方式 | 关键脚本/注入 |
|---|---|---|---|
| 00.1 | `TestLocalSmokeSymptomAcceptedSilent` | `postTaskSession(text, "symptom.g1/one")` 独立会话 | 入口模型：调用 worker → 见受理回执后回空正文；脚本 provider 的 `hook` 用 channel 闸住 worker 模型的请求，保持“受理已回、结算未到”的窗口 |
| 00.2 | `TestLocalSmokeSymptomSettledOnce` | `postTaskSession(text, "symptom.g2/one")` 独立会话 | 同一链路不放闸前先断言受理窗口；放闸后等到结算，数本轮文本 |
| 00.3 | `TestLocalSmokeSymptomFailureNotice` | `rig.drive(chat, ...)` 生产入站 | `smokeProvider.hook` 对标记返回 500（仿 `smoke_errors_test.go` 写法）；断言正文含原始错误标识 |

**共同约定**：

- 三条各自 `bootSmokeRig` 独立宿主（互不影响），各自 `defer rig.close()`；不共用 `lastChat` 之外的状态。
- 会话标签固定：`symptom.g1/one`、`symptom.g2/one`；G3 用 `smokeChats` 既有会话（生产入站路径要求），只读该会话 outbound，不改既有三会话的期望轮次。
- 确认词表与失败话术词表**分开**定义（前者是"不该说的话"，后者是"该说而未说的话"），不共用一个正则。
- 每条测试的失败信息必须打出**收到的实际文本**（`recentOutbound`/`recentRequests`），使红证可判读而非只有 `false != true`。
- 脚本模型的仿真规则写进测试注释；任何"为了让测试红"的脚本改动，视为伪造红。

## D4 写集、串行与红窗

| 叶 | 生产写集 | 测试写集 | 只读 | 串行/锁 |
|---|---|---|---|---|
| 00.1 | 无 | `examples/wechat-bot/smoke_test.go`（新增函数 + 词表常量） | `smoke_harness_test.go`、`wechat_test.go` | `wechat-smoke`（本域三叶同文件 → 合单一次写入） |
| 00.2 | 无 | 同上（同文件不同函数） | 同上 | 同上；与 00.1 合单 |
| 00.3 | 无 | 同上（同文件不同函数） | `smoke_errors_test.go`（注入写法） | 同上；与 00.1/00.2 合单 |

- **本域三叶触碰集完全相交**（同一文件），按 D4 一级合并规则**合单为"00 域症状单"一次写入**；叶 checkbox 仍三条分列，便于逐条对账。
- **与 02 域相交**：`smoke_test.go` 里既有的 500 注入等待（`smokeErrorsFailureText`）会被 02.1 修订 → 02 必须排在 00 之后（本域先落，02 再改同文件）。
- **红窗（必须上报）**：函数名以 `TestLocalSmoke` 开头 → `scripts/preflight.sh` 的 `-run TestLocalSmoke` 会包含它们。00 域落地后 preflight 变红，直到 01.1 与 02.3 转绿。这是本变更的**有意红窗**：F2 全量门禁的绿证必须在 F3 之前取得，W0/W1 期间的 preflight 红不记为回归。
- 本域**不改** `smoke_harness_test.go`（只读）；若实施期发现必须新增台架辅助（如放行闸），先上报，由编排者判是否扩写集——不得顺手改。

## D5 勾选判据（红证域的特例，写清以免被误判为"没跑绿"）

本域三叶是**红证交付**，验证命令**预期非 0 退出**：

1. 命令可复跑，退出码非 0；
2. 失败信息命中**约定的那条断言**（D2 的"当前红证形态"），而不是编译错误、台架错误、超时或 `t.Fatal` 在别的子步骤；
3. 同一命令连跑 2 次，失败点一致（确定性红，不是 flake）；
4. 首次运行的 `-json` 日志落仓库外 `/tmp/event-semantics-boundary/<叶>/run-<i>.json`，把失败断言行与退出码摘入叶内（不新增证据文件到仓内）；
5. 若退出码为 0（不红）：**不得勾选**，按 D2 的诚实边界记录"fake 模型下不红"的形态并上报编排者裁定（此时本叶的验收面改为"该断言的模型世界替代锚"，且必须在叶内写明替代理由）。

> 勾选的含义是"红证成立且可复跑"，不是"行为已修好"。转绿由 01.1（G1/G2）与 02.1/02.3（G3）负责，F3 收口时以回滚即红复核。

## D6 风险与回退

| 风险 | 影响 | 缓解 / 回退 |
|---|---|---|
| 脚本模型无法仿真"复述确认" | G2 用户世界不红 | 红证退回模型世界（G1），如实记录；不得伪造 |
| 三测试进 preflight 造成"全局变红"误解 | 被误判为回归、被要求回退 | proposal/design 明写红窗与解除条件；F2 只在 F3 前要求全绿 |
| 独立会话的 outbound `Target` 与 chat 不一致 | 数错会话 | 断言按注入会话标签过滤，并在失败信息里打印实际 `Target` 全集 |
| G3 走 `/task` 注入而漏掉罐头文案 | 红证取错面、02 漏改示例层 | G3 强制走 `rig.drive`（生产入站）；D2 记录两条路径差异 |
| 测试复用既有三会话、改动其期望轮次 | 既有 smoke 用例连锁红 | 新增测量一律独立注入会话；G3 只读既有会话 outbound，不追加轮次 |
| 红证混入 flake（宿主起停竞态） | 红证不可判读 | 固定等待条件用 `provider.count`/`outbound` 轮询而非 `time.Sleep` 定点；连跑 2 次一致才计红 |

## D7 本域不做的（边界）

- 不修任何产品行为（修在 01/02）。
- 不写能力 delta（无新能力）；不改主规格与 golden（04 域）。
- 不把三测试登记进 CI 选择器（04 域），但**函数名已按 `TestLocalSmoke` 前缀落位**，登记只是显式化。
- 不宣称真机 provider 抖动可控：G3 是 smoke 台架的确定性 500 注入，真机证据只作症状来源引用（诚实边界写进一级 proposal「必答一问」）。
