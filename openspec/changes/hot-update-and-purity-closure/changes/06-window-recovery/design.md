# 04-window-recovery 设计（裁定集 r45 定稿）

> **r46 勘误（用户裁决，domain 12 取代）**：D3 的「D 快照装全状态否决」、D5 的「不接收任意外部状态/不提供通用注册钩子」、D6/D7 的 RecoveryTicket 票据路线，均由 domain 12-checkpoint-state 取代——框架级统一 checkpoint 事实族承载全部可恢复状态（投影+送达账+邮箱+任务终态），消费者经框架状态接缝注册 snapshot/restore。本域完成的投影折叠锚（D4）作为一致性切面定理被 domain 12 继承，投影幸存者成为 checkpoint 的一个切片；独立锚 boot 回退与票据路线由 checkpoint 恢复收编。下文 D3/D5/D6/D7 已按此改写；已完成任务证据（tasks [x]）只附勘误不改写。

## D1 机制界不可配，策略可配（4.0 已放行）

- **机制界**（内存、缓存、页界、捕获预算、检索腿界）：由回放窗派生或降为不可配常量。
- **策略**（压缩时机与保留深度、预算面、长上下文）：`compress_threshold`/`keep_recent_tasks`/`provider_context_window`/`output_reserve`/`long_context_tokens` 保留可调。
- 四类登记于 `golden/runtime/window_bounds.json`，`check_window_bounds.py` 机械守护：新增上限类可配项即红、机制常量不得当声明键、声明键必须登记。

## D2 常驻界由投影派生（4.0b 已放行）

带窗会话链以投影最老原文为界；无窗链兜底 `noWindowResidentFacts=2048`。折叠规则由 `fold_contract.json` + `fold_plan_golden.json` + 四条 mutation 钉死。判断事实带属主落链；`EachSessionFact` 按 (partition, seq) 提交序读。

## D3 被证伪的路（如实保留，防止再走）

「窗口内重建」三次翻车（r43）：提交序上**不存在无损早停**——存活原文（新带、滚入输入、折后新事实）可比任何旧产物事件更深；盲走不停、停点漏滚入、跳过漏替换三方向全部失败。结论：`seedReplay` 单遍全量走链；C（外部存活位索引）冗余且属持续维护类，**终局否决**。**D（快照装全状态）原否决已由用户裁决取代（r46，domain 12）**：框架级统一 checkpoint 事实族承载全部可恢复状态，以分区 Seq 水位 + 偏移后回放保证一致性，boot 成本由 checkpoint + 增量决定而非全史重放；「无损早停不存在」针对的是无快照的纯走链，快照+回放不在此列。

## D4 折叠锚定理（核心，先红后绿）

折叠瞬间是每会话**同步、一致性安全**的检查点时刻（判卷→落判断→执行→落产物→替换投影，全在当次提交内串行完成）。产物载荷扩展为完整锚：

```
锚 = 摘要正文（已有） + 死亡名单 Covered（已有） + 幸存者名单（新增）
     + seq 位点（自带）
```

- **幸存者名单**：批次 `ApplyProducts` 之后按**投影序**记录键列表（含头部 hist 键与段产物的合成键，合成键带 `compress_key` 指向事实键）。重建=按序查表，**不重算放置**——绕开 r43 全部三个死因。
- **锚发现**：按冻结事件类型 `event.CompressProductType` 判别（解码后的 `evt.Type`），不是按事实键的拼写；倒序走链时每会话第一条携带锚载荷的产物事实即锚，全部会话都锚定后停止枚举；从未折叠的会话退全量（天然短）。事实键的 ID 拼写只是身份文案，MUST NOT 作为机制判别依据或写进模型上下文。
- **恢复公式**：锚载荷 + 回放本链中 seq 大于幸存者边界的全部事件（覆盖快照与产物提交之间插入的事实）。**r46：本投影锚恢复由 domain 12 统一 checkpoint 收编**——投影幸存者成为 checkpoint 的 Projection 切片，独立「锚坏/缺/版本不符 → 全量走链」回退被 checkpoint 恢复取代（坏 checkpoint 具名拒 boot，见 12/design.md D4）；折叠锚的一致性切面定理（折叠瞬间同步安全）保留并被继承。
- **golden**：折叠契约向量随载荷扩展重生成（钉住有意的变更），四条 mutation 复验 killed。
- **红锚** `TestAnchoredRebuildEqualsFullWalk`：12 回合折叠链，锚重建 ≡ 全量走查逐条一致、解码数更少、锚数 > 0；编译红中。

## D5 持久化与派生态（#19 审计的判据）

链事件是语义真相，既有主人日志承担机械重做。**r46：原「锚不接收任意外部状态、不提供通用注册钩子」由用户裁决取代（domain 12）**——框架提供状态接缝 `Stateful`{SnapshotState/RestoreState}，消费者（送达账/邮箱/任务注册表/投影）注册其可恢复状态切片，框架在一致性切面聚合为统一 checkpoint 事实。checkpoint 仍是可由链重建的派生快照（回放偏移后事实经同一 observe/commit 管线重新派生），不是第三真相；「真正不可推导的状态必须先建模为事件」不变——快照只是加速恢复，语义真相仍在链。审计对象仍是 Run/Host 全部内存态字段，修复优先删除冗余状态或按链重建。

relations 不新增在线父图写入。召回是独立读侧消费者，父边在 4.c1 随查询从持久链按需推导，既有 `MemoryRelations` journal 与运行期接线随读侧退役一并清理。memory tier 与空 Root 不创建磁盘 journal。

## D6 boot 只做自己的消费（读侧退役）

boot 事件 = checkpoint 恢复（读最近 checkpoint 事实 + 回放偏移后事实）+ 完成点收敛。**r46：原「②锚/快照装载 + 完成点票据」由 domain 12 收编**——独立锚 boot 路径与 RecoveryTicket 完成点票据退役，恢复统一经 checkpoint 事实族（12/design.md D4/D5）。查询/召回是独立消费者，按需读链：

- 观察表整体退役：`TierQuerySource` 查询直读持久层；`StoreQuerySource`/`ObserveStore`/运行期 land 钩子连同接线足迹**原子删除**（删除原子化裁定）。
- 向量缝整体退役：检索向量腿改查询时候选现嵌入；`VectorSeam` 整型删除。
- landedSeq 已并入走链（r44 ✅）；tier 打开改懒校验。
- 召回父边按需从持久链推导，不新增在线关系写入或第二份父图状态；`MemoryRelations` journal 与 `New/OpenMemoryRelations` 已删除，RecallPort 以晚绑定包装 Trace，工具构造顺序不再依赖 Host 预先开图。
- boot 从四遍全量解码收敛为自己的消费一遍。

## D7 载体（不变 + 已知缺口）

- **r46：载体收编为统一 checkpoint 事实族（domain 12）**。原「快照保留、面封顶八表、票据门禁」中的 RecoveryTicket 票据路线退役；有界化由 checkpoint 事实族承担（分区 Seq 水位 + 开账清单，O(开账数)）。一致性机器（等价锚 + 退回）由 checkpoint 恢复 + 偏移后回放继承；「七类退回/票据门禁」随票据退役移除。
- **时间纯度审计**：terminalTTL 等时间参数是否冻进快照面；不纯则移出快照面、按 boot 时刻现算。
- **游标缺口（r46 已解）**：原「mergeDelta 全量重扫 ⇒ 无真游标 boot 不平坦」由 domain 12 的分区 Seq 水位 checkpoint 解决——boot 读最近 checkpoint + 回放偏移后增量，成本平坦（合并门：LongRun 每代 boot 时长不随代数增长）。

## D8 不改的东西

事件契约、事实族、插件管线、交付/冥想语义、压缩策略（何时折/折什么/保留几段/分龄推断/身份语义）、预算面与策略面保留键；「分区位图/Init 游标」预案删除。
