# 成功语义一致化：交付义务预登记、状态提交 checked、格式识别拒旧

## 症状（验收矩阵第一行——用户可见语义直译）

| 评审场景（用户可见语义） | 直译测试（第一天红着） |
|---|---|
| 「已生成的结果在发送中途崩溃后永久漏送」 | 真子进程强杀（Sender 未确认时）→ 重启 → **用户收到已生成结果**（结果事实在、实际交付>0、待交付账本非空销账） |
| 「拒绝后重启回到待审核」 | 拒绝时持久化失败 → **接口返回错误且内存态不发布**；持久化成功 → 重启后仍拒绝态 |
| 「旧格式数据静默丢正文」 | 旧格式字节（key/content/summary 形态）→ 解码**具名拒绝**（ErrEventBody），不再出现"解码成功正文空" |

## Why（评审 P1/P2，降格为候选修法后逐项溯源）

1. **P1 漏送**：host.go settleTerminal（先记完成+consume 唤醒义务）→ deliverFresh（后发送）——崩溃窗口内结果永不送达。修法=结果先交接给既有交付账本（hostDeliveryRecord 持久义务），再关生成义务；恢复时发送已生成结果。
2. **P2 吞错**：recordDeliveryState 只 WARN 不返回；RefuseDelivery 等在持久化失败时仍返回 nil、内存态已变。修法=复用事实提交的 checked 语义：持久化成功后才发布状态变化。
3. **P2 格式静默**：event_body.go:145 只查 key 存在；上一版 key/content/summary 字节解码成功但正文空。修法=显式识别旧格式并具名拒绝（单一格式、冷重建裁定的自然延伸）。

## What Changes

- (a) 交付义务预登记：发送前持久登记交付义务（复用 hostDeliveryRecord 账本），生成义务在交接后才消费；重启恢复=发送已生成未销账结果。
- (b) 交付状态提交 checked：recordDeliveryState 返回错误；RefuseDelivery/ApproveDelivery 持久化失败→错误返回且内存态不发布。
- (c) 格式识别：DecodeEventBody 对旧格式字节（无 message 而有 content/summary/event_key）具名拒绝。

## 范围并入登记（D9-D16）

本节登记本轮**并入**的责任范围收缩项与结构重组项，逐条一行、决策原文在 `design.md`（D9-D14 见「责任范围收缩决策」节，D15/D16 见「host 簇文件重组裁定」节）：

- **D9 去写权三分**：Count=登记已成立（现算）、Open=以 restore 为 oracle 现算、Blocked=保留写权并写明依据（拒收原因无事实载体）。
- **D10 独立写权状态全枚举表**：pendingDeliveries（派生索引）/answered/exitReason/Occupancy/delegate 投影逐行裁决入库 → `state-inventory.md`。
- **D11 训练外移幸存面**：Capture 三方法全保、Recorder Episode/Decision 删、Attempt 并入 Capture.Record、WithCallPass 迁 api/plugin。
- **D12 端口压平**：六端口闭集保留不合并、删九个组合词汇常量、assemble 改具名端口派发。
- **D13 热更新三档字段映射**：工具/插件（本方言落点）与 storage/read_root/exec 等裁二档具名拒绝、同指纹不开新代 → `reload-tiers.md`。
- **D14 验收设施**：复用 tests/integration execution rig + `NativeAgent.Tools` 注册面，`runtime/task` 越界清点表 → `boundary-audit.md`。
- **D15 host 簇文件级职责重组**（不分包）：host_delivery/host_document/host_tools/host_face 四域纯移动 + host_diagnostics 更名。
- **D16 根包整体迁移 assembly/**（上位 D15）：23 个 .go 同包 git mv、限定符 `tagent.`→`assembly.`、门禁随迁。

## 次要项（grill 判定是否纳入本轮）

- features 依赖隔离：runtime/agent→features/evolution/governance（WithCallPass）——评审「继续向已有所有者收拢」。（已裁定 D8/D11 并入本轮：4501ee3a 迁 api/plugin）；
- 执行最终输出 final 内存表跨重启归属——评审「需要明确归属」。（已裁定 D7：跨重启不承诺，ErrFinalOutputUnrecorded 属可解释错误；持久化登记下轮）

## 必答一问

「本案完成后，强杀崩溃还会漏送吗？」——已登记义务不漏（交付义务在发送前已持久登记，恢复路径发送已生成结果）；invoke 返回→登记微窗口仍属 spec 声明的残余窗口（不作恢复承诺）。红转绿证据=强杀场景测试第一天红着、修复后绿、回滚即红。
