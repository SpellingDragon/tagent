# 设计决策（grill 第一轮修订版）

D1 崩溃窗口全覆盖（grill#2 修订）：生成义务销账与交付义务登记**同批原子**不现实（跨层），采用**补交接规则**：落地即销路径（driveSeat 读到 completed attempt）在销生成义务前**先为已生成结果补登记 owed 义务**（逐条正文，grill#12），登记成功才 consume。生产路径顺序：invoke 落结果 → 逐条登记 owed → 发送 → 销账。崩溃窗口任何点重启：owed 在账或补登记挽回 → 发送。
D2 义务状态词汇（grill#1/#4）：新增 `owed` 态（已生成待投递义务，恢复自动经 deliverOne 投递、保留 ruler 裁决）；`held` 语义不动（等审批）；**单一义务身份贯穿**（同 ID 状态迁移 owed→held/retry/sent，销账幂等），恢复 switch 显式接住 owed（default 丢弃是雷）。
D3 at-least-once 边界（grill#5）：Approve/发送路径不可承诺恰一次——发送前义务保持未销（owed/sending 态），回执落盘才销账；回执失败返回具名错误（"已送达但回执未落"）防盲目重试；spec 明示容忍度=发送后崩溃允许恰好重投一次。Refuse 可回滚（checked 成立）。
D4 checked 范围（grill#6/#7/#9）：recordDeliveryState **全部 4 个调用点**（Approve/Refuse/hold/restore）失败具名上抛；noteDeliveryRefusal 审计提交失败同样返回错误；无 tier 装配=不承诺跨重启（spec 降级声明）。
D5 格式拒旧判据（grill#10）：**顶层无 `message` 键即具名拒绝**（原始 JSON 键存在性，覆盖全部旧形态含 key/content/summary 与 event_key 形态）；禁止结构体零值判据（会误拒合法空 Message 审计事实）；既有 LegacyRefused 锚（event_key 形态）不动，新测试字节样例=`{"key":…,"content":…,"summary":…}`（grill#11）。
D6 残余窗口明示（grill#3）：用户 turn 的「invoke 返回→登记」微窗口（同一调用栈内）无可恢复源——spec 明示残余窗口与理由，不作无条件承诺；强杀锚判据（grill#8）=kill 前投递 0、重启后恰 1、持久账 owed 归零（Scan delivery: 键）、生成义务 consume 恰一次。
D7 final 归属裁定（grill 次要项判定）：执行最终输出 final 表**跨重启不承诺**（进程内运行时），重启后 ErrFinalOutputUnrecorded 属可解释错误；归属=不扩展持久面（登记下轮实现）。
D8 次要项：WithCallPass 依赖隔离**登记下轮**（与成功语义不同源）。
