# 设计决策（grill 第一轮修订版）

D1 崩溃窗口全覆盖（grill#2 修订）：生成义务销账与交付义务登记**同批原子**不现实（跨层），采用**补交接规则**：落地即销路径（driveSeat 读到 completed attempt）在销生成义务前**先为已生成结果补登记 owed 义务**（逐条正文，grill#12），登记成功才 consume。生产路径顺序：invoke 落结果 → 逐条登记 owed → 发送 → 销账。崩溃窗口任何点重启：owed 在账或补登记挽回 → 发送。
D2 义务状态词汇（grill#1/#4）：新增 `owed` 态（已生成待投递义务，恢复自动经 deliverOne 投递、保留 ruler 裁决）；`held` 语义不动（等审批）；**单一义务身份贯穿**（同 ID 状态迁移 owed→held/retry/sent，销账幂等），恢复 switch 显式接住 owed（default 丢弃是雷）。
D3 at-least-once 边界（grill#5）：Approve/发送路径不可承诺恰一次——发送前义务保持未销（owed/sending 态），回执落盘才销账；回执失败返回具名错误（"已送达但回执未落"）防盲目重试；spec 明示容忍度=发送后崩溃允许恰好重投一次。Refuse 可回滚（checked 成立）。
D4 checked 范围（grill#6/#7/#9）：recordDeliveryState 失败具名上抛——原 4 个调用点（Approve/Refuse/hold/restore），71efb740 后共 6 个（新增 redrive 补投与 headless 销账两点），均已 checked；noteDeliveryRefusal 审计提交失败同样返回错误；无 tier 装配=不承诺跨重启（spec 降级声明）。
D5 格式拒旧判据（grill#10）：**顶层无 `message` 键即具名拒绝**（原始 JSON 键存在性，覆盖全部旧形态含 key/content/summary 与 event_key 形态）；禁止结构体零值判据（会误拒合法空 Message 审计事实）；既有 LegacyRefused 锚（event_key 形态）不动，新测试字节样例=`{"key":…,"content":…,"summary":…}`（grill#11）。
D6 残余窗口明示（grill#3）：用户 turn 的「invoke 返回→登记」微窗口（同一调用栈内）无可恢复源——spec 明示残余窗口与理由，不作无条件承诺；强杀锚判据（grill#8）=kill 前投递 0、重启后恰 1、持久账 owed 归零（Scan delivery: 键）、生成义务 consume 恰一次。
D7 final 归属裁定（grill 次要项判定）：执行最终输出 final 表**跨重启不承诺**（进程内运行时），重启后 ErrFinalOutputUnrecorded 属可解释错误；归属=不扩展持久面（登记下轮实现）。
D8 次要项：WithCallPass 依赖隔离原登记下轮，**已由 D11 并入本轮实施**（4501ee3a 迁 api/plugin）。

# 责任范围收缩决策（五项并入，grill 一轮修订版）

D9 去写权三分（grill A-1）：Count=登记已成立（manager.go:67 已现算）；Open=以 restore 为 oracle 现算；**Blocked=不能从权威记录算回且改变允许操作**（拒收原因 err/answered 均无事实载体，manager.go:165-177 纯内存）——二选一裁决：投递拒收落为事实（新增权威写）或保留写权写明依据（默认后者，D1 判据表明其不满足删除条件）。
D10 独立写权状态全枚举表（grill A-2）：pendingDeliveries（host.go:1510 证明可从持久账重建→派生索引）、entry.answered（同 Blocked 裁决）、exitReason、state.Occupancy、delegate.go:327 投影——逐行裁决入表；D1 补第三格「不能算回且不改操作→补权威记录或登记派生索引，二选一写明」。
D11 训练外移幸存面（N3 裁定：tool/ 属装配面，import 白名单化；核心=runtime/ 运行时核心，编译级装配依赖不在约束，运行时 nil 路径有 TestNoCaptureZeroRecords 锚）（grill B-1/B-2）：**Capture 三方法全保**（Snapshot/Record/Seal=执行证据面，Snapshot 调用时固定输入）；Recorder 三方法逐个判——Episode 删（episodeID+起始时间可从 ready 事件算回）、Decision 删（Snapshot+链上因果覆盖，inputs 本是核心因果链（4501ee3a 删除前的 loop.go:282-301，见该提交 diff））、Attempt 并入 Capture.Record（两面逐次重复调用，见 4501ee3a 删除前 loop.go:1471-1480；现合并点 loop.go:686）；**黑白名单**：黑=Recorder 组织+runtime 核心对 features/* 的 import；白=event 包身份类型（EpisodeID/DecisionID/AttemptID 是事件身份非训练形态）；WithCallPass/CallPassFrom 缝合迁 api/plugin（CallPass 的家，值类型已在 api/plugin/phases.go:255），runtime/agent 去 features/evolution import。
D12 端口压平裁定（grill C-1）：组合推导判据=「材料集合级统一路由或组合合法性计算」——按证据（phases.go:197-253 六对 switch 等价具名端口常量、material.go:82-96 闭表查（叶9 前为 :86-108 三查）、无集合路由/代数）裁**无**→删九词汇常量，assemble 改具名端口常量派发；EXTENSIONS.md:11/:28 两锚改写、skeleton_assert.go:104-113 常量钉、误声明用例（policy_test.go:458-464）同步；六端口闭集不合并。
D13 热更新三档字段映射（grill D-1）：字段级映射表（hostDocument+hostAgent 全字段每行一档）；**tools/plugins 声明裁二档具名拒绝**（现静默换入 doc 且永不复用是最差态——host_reload.go:140-147 全量复用 Boot natives）；档三测试锚=PublishUnchanged（publish.go:300-305 同指纹不开新代）；storage.root/type、ReadRoot/Exec 等二档字段由 compatibleWith 具名拒绝（现静默放行 host_reload.go:199-220）。
D14 验收设施（grill E-1）：复用 tests/integration/execution rig（delegation_completion_test.go:514-527+close_drains_obligations 全套）+ NativeAgent.Tools 注册面（run.go:64-73）；清点表=runtime/task 全目录 grep（tmux|subagent|远程）产出入库清单。

# host 簇文件重组裁定（D15，2026-10-28 用户指令「分包或职责优化」）

D15 不分包、文件级职责重组：host 簇是组合根（Host 横跨驱动/交付/装配/生命周期），分包要求导出化大量内部类型（heldDelivery/hostDeliveryRecord/settleBarrier），制造导出面膨胀——不分包（先例：能力从属才移包）。实测域块（host.go 3215 行）：交付域 host.go:1141-1935（~800 行，本案 D1-D6 全部改动面）、文档装配域 2036-2634（~600 行）、宿主工具与端口域 2635-2994（send 工具+meditation+injection ~360 行）、HTTP face 2995-3215（~220 行）。重组（同包纯移动零语义变更）：
- host_delivery.go ← 交付域全族（deliverFresh→freshObligationOf）；
- host_document.go ← hostDocument 装配域；
- host_tools.go ← send 工具+meditation+injection；
- host_face.go ← host.go 尾部真 HTTP face（现 host_face.go 实为诊断面，更名 host_diagnostics.go——名实相符）；
- host.go 剩 Boot/options/驱动/settle/生命周期/地址锁（~1100 行）。
门禁随迁：comment_policy 文件头、package-layout 镜像、gen_godoc 重生成；host_test.go 仍镜像 host.go。

D16 根目录整体迁移 assembly/（2026-10-28 用户裁定，上位 D15）：根 23 个 .go（host 簇 10+会话/事实面 13）同包整体 git mv 进 assembly/，目录=包名=assembly（D15 的同包文件级拆分全部随迁保留）；包内互引零改动，外部限定符 tagent.→assembly. 全改（仓内 import 枚举重写）；根目录只剩 README/go.mod/子目录。门禁随迁：package-layout 清单/comment_policy 扫描集/gen_godoc/concept-budget baseline 键/docs 引用路径。D15「不分包」的导出化顾虑由同包整体迁移消解（无跨包边界产生）。
