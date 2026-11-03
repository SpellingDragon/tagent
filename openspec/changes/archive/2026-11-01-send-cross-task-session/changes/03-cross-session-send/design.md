# 03 技术要点

## 要点

1. **分派**：`hostSendTool.Call` 在 target 判定为 `{agent,session}` 后走对外投递缝（`WithDeliverTo` 语义的四道门），而非 `hostDeliverFunc` 出网。
2. **入链+驱动一轮**：四门通过 → `CommitToSession`（事件入目标链，携来源分账 Metadata 若 06 已合）→ `Host.Invoke`（复用驱动面，TriggerSource 记 `send`）；目标忙碌时沿 one-slot gate 边界（既有 gates 语义），不排队堆积。
3. **幂等**：投递身份=发送方调用帧（`CallIdentity`）+ 目标会址；重复投递以事实身份去重（复用 `Notice`/事实 Key 幂等思路）。
4. **deliver_summary 归并**：`DeliverSummaryTool` 删除；meditation 装配改发 `send` 工具（deliver_to 白名单解析保留 `hostMeditationTargets`）；扇出语义（deliver_to 全部活跃会话）由冥想 prompt 指导逐目标调用 send，或装配面保留批量解析——**取逐目标调用**（工具面唯一、语义最小）。目标会址的来源见下节补记。
5. **治理**：send 跨会话形态接 risk_rules（`governance.ToolRiskRule` 按 tool 名已有），critical→RulingHold→批准补投复用 02 的补投通道。

## 编排者裁决补记（W2-B 落地）：目标会址由装配面注入

**缺口**（W4-D 上报）：要点 4 定「逐目标调用 `send{target:{agent,session}}`」，但反思输入只带 deliver_to 的 agent 名与 digest，**没有目标会址**。模型因此只能猜 session，而四道门的白名单/绑定门会把猜错的会址拒掉——逐目标调用在材料面上不成立。

**裁决**：目标会址**由装配面注入反思输入材料**，模型逐目标调用，扇出仍不由装配面代劳。

- `meditation.ReflectionDelivery`（`reflection_config.go`）承载两面：声明的 `Targets` 与解析活跃绑定的 `Sessions`（`SessionQuery.ActiveSessions`）。
- `Reflection.deliveryText()` 把每个目标**当前绑定**的会址逐行渲染进 `reflectionText`（`- agent "<agent>", session "<session>"`），并附「per live session 发一条 send」的指令；声明的目标**一个会话也没绑**时具名报告 `no live session, so a body sent there would be refused`——不静默省略（省略会被读成"这个目标安静"），**不编造地址**。
- 接线：`assembly/host.go` 装配 reflection 时传 `Delivery: meditation.ReflectionDelivery{Targets: meditationPlan.targets, Sessions: meditationPort}`，即 `hostMeditationTargets` 的解析结果 + 运行期活跃绑定面，逐轮读取而非构造期快照。

**边界**：注入只改「材料」，不改「门」。会址列出来不等于获准——白名单、盲投、绑定、未运行四道门仍 fail-closed，被列出的会话若在调用时已解绑，回执仍是 `[send_denied]`。未声明 deliver_to 的冥想会话渲染不出该节（`deliveryText` 返回空），工具面也没有第二个通信工具可退。

**锚**：`TestMeditationSendsViaSendTool` —— 正锚 `delivery_targets_name_live_sessions`（多目标多会址逐行列出）、正锚 `target_beside_no_live_session_is_reported`（无活跃会话具名报告、不出现 `session ""`）、负锚 `tool_face_has_no_deliver_summary`。

## 文件清单

- 改：`assembly/host_tools.go`（分派）、`features/evolution/meditation/deliver.go`（删除）、`assembly/host_document.go`（装配面）。
- 测：`tests/integration/execution/`、`tests/integration/host/`、`features/evolution/meditation/`。

## 风险与回退

- 风险：`Host.Invoke` 在目标会话忙碌时的等待语义（不得死锁在 send 调用内）——用 one-slot gate 非阻塞语义，失败回 `[send_denied]`。
- 回退：deliver_summary 删除是 BREAKING（D1 用户明示）；回退恢复旧工具面即可，白名单语义未动。
