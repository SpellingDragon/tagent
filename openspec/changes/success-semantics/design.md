# 设计决策（grill 第一轮修订版）

D1 崩溃窗口全覆盖（grill#2 修订）：生成义务销账与交付义务登记**同批原子**不现实（跨层），采用**补交接规则**：落地即销路径（driveSeat 读到 completed attempt）在销生成义务前**先为已生成结果补登记 owed 义务**（逐条正文，grill#12），登记成功才 consume。生产路径顺序：invoke 落结果 → 逐条登记 owed → 发送 → 销账。崩溃窗口任何点重启：owed 在账或补登记挽回 → 发送。
D2 义务状态词汇（grill#1/#4）：新增 `owed` 态（已生成待投递义务，恢复自动经 deliverOne 投递、保留 ruler 裁决）；`held` 语义不动（等审批）；**单一义务身份贯穿**（同 ID 状态迁移 owed→held/retry/sent，销账幂等），恢复 switch 显式接住 owed（default 丢弃是雷）。
D3 at-least-once 边界（grill#5）：Approve/发送路径不可承诺恰一次——发送前义务保持未销（owed/sending 态），回执落盘才销账；回执失败返回具名错误（"已送达但回执未落"）防盲目重试；spec 明示容忍度=发送后崩溃允许恰好重投一次。Refuse 可回滚（checked 成立）。
D4 checked 范围（grill#6/#7/#9）：recordDeliveryState **全部 4 个调用点**（Approve/Refuse/hold/restore）失败具名上抛；noteDeliveryRefusal 审计提交失败同样返回错误；无 tier 装配=不承诺跨重启（spec 降级声明）。
D5 格式拒旧判据（grill#10）：**顶层无 `message` 键即具名拒绝**（原始 JSON 键存在性，覆盖全部旧形态含 key/content/summary 与 event_key 形态）；禁止结构体零值判据（会误拒合法空 Message 审计事实）；既有 LegacyRefused 锚（event_key 形态）不动，新测试字节样例=`{"key":…,"content":…,"summary":…}`（grill#11）。
D6 残余窗口明示（grill#3）：用户 turn 的「invoke 返回→登记」微窗口（同一调用栈内）无可恢复源——spec 明示残余窗口与理由，不作无条件承诺；强杀锚判据（grill#8）=kill 前投递 0、重启后恰 1、持久账 owed 归零（Scan delivery: 键）、生成义务 consume 恰一次。
D7 final 归属裁定（grill 次要项判定）：执行最终输出 final 表**跨重启不承诺**（进程内运行时），重启后 ErrFinalOutputUnrecorded 属可解释错误；归属=不扩展持久面（登记下轮实现）。
D8 次要项：WithCallPass 依赖隔离**登记下轮**（与成功语义不同源）。

# 责任范围收缩决策（五项并入，grill 一轮修订版）

D9 去写权三分（grill A-1）：Count=登记已成立（manager.go:67 已现算）；Open=以 restore 为 oracle 现算；**Blocked=不能从权威记录算回且改变允许操作**（拒收原因 err/answered 均无事实载体，manager.go:165-177 纯内存）——二选一裁决：投递拒收落为事实（新增权威写）或保留写权写明依据（默认后者，D1 判据表明其不满足删除条件）。
D10 独立写权状态全枚举表（grill A-2）：pendingDeliveries（host.go:1510 证明可从持久账重建→派生索引）、entry.answered（同 Blocked 裁决）、exitReason、state.Occupancy、delegate.go:327 投影——逐行裁决入表；D1 补第三格「不能算回且不改操作→补权威记录或登记派生索引，二选一写明」。
D11 训练外移幸存面（grill B-1/B-2）：**Capture 三方法全保**（Snapshot/Record/Seal=执行证据面，Snapshot 调用时固定输入）；Recorder 三方法逐个判——Episode 删（episodeID+起始时间可从 ready 事件算回）、Decision 删（Snapshot+链上因果覆盖，inputs 本是核心因果链 loop.go:282-301）、Attempt 并入 Capture.Record（逐次重复调用 loop.go:1471-1480）；**黑白名单**：黑=Recorder 组织+runtime 核心对 features/* 的 import；白=event 包身份类型（EpisodeID/DecisionID/AttemptID 是事件身份非训练形态）；WithCallPass/CallPassFrom 缝合迁 api/plugin（CallPass 的家，值类型已在 api/plugin/phases.go:255），runtime/agent 去 features/evolution import。
D12 端口压平裁定（grill C-1）：组合推导判据=「材料集合级统一路由或组合合法性计算」——按证据（phases.go:197-253 六对 switch 等价具名端口常量、material.go:86-108 闭表三查、无集合路由/代数）裁**无**→删九词汇常量，assemble 改具名端口常量派发；EXTENSIONS.md:11/:28 两锚改写、skeleton_assert.go:104-113 常量钉、误声明用例（policy_test.go:458-464）同步；六端口闭集不合并。
D13 热更新三档字段映射（grill D-1）：字段级映射表（hostDocument+hostAgent 全字段每行一档）；**tools/plugins 声明裁二档具名拒绝**（现静默换入 doc 且永不复用是最差态——host_reload.go:140-147 全量复用 Boot natives）；档三测试锚=PublishUnchanged（publish.go:300-305 同指纹不开新代）；storage.root/type、ReadRoot/Exec 等二档字段由 compatibleWith 具名拒绝（现静默放行 host_reload.go:199-220）。
D14 验收设施（grill E-1）：复用 tests/integration/execution rig（delegation_completion_test.go:514-527+close_drains_obligations 全套）+ NativeAgent.Tools 注册面（run.go:64-73）；清点表=runtime/task 全目录 grep（tmux|subagent|远程）产出入库清单。
