# Design

## D1 被否：排空告警轮（计数阈值不可靠）

原 D1（等 MAIN 计数 +2 后注入）被实现期证伪：本用例注入前有两次语义发布（结构热加 + 数值更新），每次都 `EmitSystemAlert`（`tagent.go:684`）⇒ 告警轮 2 枚；`EventBus.Pull`（`agent/event_bus.go:787`）排干合批 ⇒ 2 枚告警可合为 1 turn（MAIN 记 2 条）或分作 2 turn（MAIN 记 4 条）。任何绝对计数阈值在一种形态下漏排、在另一种形态下永不可达（20s 挂死）。证据：`fail-before.log` 结论 3。

## D1' 亦被否：基线计数锚（两种取法各有纳秒窗）

- base 取在 armGate **前**：record 与 gate 检查之间存在窗口——一条告警轮 B 调用可在 (base, armGate) 内落 record 且其 gate 检查先于 armGate ⇒ 未 park 的 record 也满足 `> base` ⇒ 空转通道残留。
- base 取在 armGate **后**：一条 armGate 前开始、gate 检查在 armGate 后的 B 调用会 park，但其 record 已计入 base ⇒ 谓词等待的下一条 record 因 loop 阻塞在 park 中永不再来 ⇒ 20s 假红（死锁形态）。

## D1'' 采用：park 直接观测

delegModel 增 per-label 的 parked 计数（parkEnter/parkExit/parkedNow，`delegation_test.go`）：调用进入 gate 等待即 +1，离开（放行或 ctx 取消）即 -1。谓词 `parkedNow("SUB-B") >= 1` 即「此刻确有一条 B 调用被 gate 拦住」——零时序推理：record 先于 gate 检查（GenerateContent 程序序）⇒ 计数 record 无法证明 park，而 parked 计数本身就是 park。armGate 之后被消费的任何轮（in-flight 或残留告警轮）的 B 调用必然 park ⇒ 谓词必可达（不依赖 FIFO/合批形态）；被 park 的调用无论属于哪个轮，在被测契约「进行中结构绑定不被回滚迁移」上是同等合法的在途见证。

## D2 断言①锚完成事件增量（串行 loop 保证归属）

`countMainCompletedByB`（MAIN record 且 `ToolResults` 含 `served:SUB-B`）。Rollback 后、disarm 前取 completedBase；① 等 `> completedBase`。processTurn 串行 ⇒ park 释放后的第一个完成事件必属被 park 的那个 turn（唯一在途）；若回滚真拆掉在途调用，其 ctx 取消 ⇒ 该 turn 无 MAIN 收尾 ⇒ ① 超时红——fail-before 敏感度不降反升。

## D3 断言②维持计数增量（活性语义，接受替代）

`> servedNow`（① 之后取基线，注入 `"after"` 后等待）。残余替代面：回滚自身也是一次发布（告警轮入队），② 可能被该告警轮的 serve 先满足——但「回滚后 loop 仍能消费并服务事件」本身就是②要保护的活性；`"after"` 输入最终必然被服务（无阻塞源）。不为消灭该替代面引入输入标记字段（超最小改动）。

## D4 fail-before 与验证口径

- fail-before：`GOMAXPROCS=1 -count=8` ×3 批 = 3/24 红（batch0 1/8、batch1 0/8、batch2 2/8），全部为断言① 20s 烧尽；`GOMAXPROCS=2 -count=4` 全绿 0.610s（与「空转通过」诊断同形）。见 `fail-before.log`。
- 修复后须 `GOMAXPROCS=1` ×24 全 PASS、`GOMAXPROCS=2` PASS、`go test . -short` 与根包 `-race` 绿。若修复后 P=1 仍出现断言①红，则「测试归因」诊断不完整——park 观测谓词下的红只能来自 ctx 取消/turn 终止，不再有编排解释，届时按产品面在途拆除缺陷立案（该可能性在此预告）。
- 被否方案：调大 20s（被等待事件不存在时无有限上界）；排空计数（D1）；基线计数（D1'）。
