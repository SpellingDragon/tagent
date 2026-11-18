# 12 消费态生命周期 · 设计

> 静态决策见一级 [design.md](../../design.md) D1（P2/P3/P4）、D6.3/D6.4/D6.5、D8（G2、E6）、D9（12 的叶依赖）、D10（「单一状态消费、不丢义务」行）、§五（Stateful 接缝冻结签名）、§六（症状直译锚取向）。
> **当前状态（本轮勘误）**：12.R1/R2/R3 均未勾；已有部分乙案实现与历史红绿证据，尚未完整验收。撤销 R264「三项 P1 全修／零额外成本／注释门零 findings」作为当前结论；§十八原始记录不改写，以本轮勘误为准。本轮仅修改计划与静态核对，没有运行测试/构建/门禁。

## 当前设计与待办映射（行动单仅见 tasks.md）

| 静态事实 | 当前判定与验收修正 | 承接 |
|---|---|---|
| `assembly/run.go:2794` 先折 `seedCoveredRange` 非幸存事实，`:2797` 后折幸存者 | 两组各自有序不等于总提交序；恢复按同一提交序进入 `Reduce`，以顺序敏感的合法消费者核对运行/恢复逐条序列与最终态 | 12.R2.1 |
| `memory/replay/replay_budget.go:110` 以 `seen` 判 Append 重复，`:309` 清掉已移出投影的键 | 既有重试锚未覆盖已折叠旧输出；先证旧键确被移出，再重试，核呈现边界、轮次及生成/收据不重复，不以永久保留所有键换正确性 | 12.R2.2 |
| 同通知跨进程与异常保义务冻结锚尚缺；`runtime/agent/loop.go:373` 的送达事实失败后继续投递尚无完整后果证据 | 在事实提交、应用、结算、消费、投递/收据真实切点建立身份轨迹与红锚；两个进程同一通知至多一收据，失败义务仍欠。调查属于 R2，不能等依赖 R2 的 R3 | 12.R2.3 |
| `newestSurvivorSeq`（`assembly/run.go:3033-3044`）先逐键读取，之后才判覆盖区间是否为空 | 空区间不证明零额外读取；记录实际存储读、解码/分配，不仅 `Decoded` 或输出字节 | 12.R2.4 |
| `assembly/checkpoint_test.go:3432-3435` 的 S5 强制卸载后对象消失 | 与 §十已选「具名拒卸载」相反。正验：错误点名未完数，原对象/名字/义务仍在，后续 cut/重启不丢；负验：先删后报、静默删各失败，空/已结切片可正常卸载 | R1 纠正验收；12.R3 实施 |

**乙案方向保留，策略不绝对化。** 不变量是未应用事实不被成功 cut 覆盖；任一拒绝阻塞全部会话 cut 是目前保守选择，不是唯一可行设计，也不由“已有整份切面失败路径”推导为唯一。R2.4 固定负载，对比无错、瞬时错后恢复、长期错，量无关会话受阻范围/时长、热层增长与恢复成本；再按证据决定保留或另行比较安全的细分方案。告警不能替代容量/延迟影响评估，TTL 不销毁义务，不新增第二真相。

**完成边界**：保留部分修复，不把它们倒退为“未做”；同样不把旧锚绿当作新增场景已验证。所有新增测试标拟新增、未运行，命令与精确文件集集中于 `tasks.md`。每叶由非实施者独立审，核事实顺序、历史重试、异常义务与真实入口；当前不声称注释门已清零。下文各时点原文保留，旧“唯一缺口”“无需补充证据”不作为当前放行依据。

## 一、判据（先立，再收敛）

本域只为两条横切契约立判据，不重复他域的账：

- **J1 已提交 ≠ 已消费**：一条事实落链即「已提交」；「已消费」是该事实已被**每一个**挂载在状态接缝上的消费者按同一 `Reduce` 应用。切面水位 SHALL 只覆盖已消费的事实；被覆盖却未消费的事实是**运行期缺陷**，不得作为可接受偏差。
- **J2 义务随消费者生命周**：一个消费者持有的可恢复态与未完义务，在禁用/卸载/替换时 SHALL 有具名去向（交接给承接者、或落回持久权威、或具名拒绝该操作）。「disposer 一删了之」不是去向。

反事实检验（元规则一）：删掉 J1——恢复后消费者永远落后于链，模型侧表现为「同一通知被再派一次」或「该做的活没做」，用户可观察，故 J1 有现实载体（非自证）。**J1 已由 12.R1 原型实证为红**（§十三 S4）。删掉 J2——`Register` 的 disposer 存在（runtime/state/checkpoint.go:172-186），删掉 J2 后该 disposer 仍能静默丢义务，损失可观察（义务消失）。**J2 已由 12.R1 原型实证为红**（§十三 S5）。二者都不依赖本域自己造的结构成立。

**【12.R1 勘误】J2 的可达性须收窄**：全仓枚举 `StateRegistry.Register` / `Run.RegisterStateSlice` 的返回值，**五个生产调用点注册的七个切片全部丢弃 disposer**（assembly/run.go:577 的循环注册 projection/inputs/compress 三个、:652 mailbox、:655 tasks，assembly/governance_checkpoint.go:52 governance seat，assembly/host_delivery_checkpoint.go:152 delivery），唯一捕获并调用它的是 `runtime/state/checkpoint_test.go:45-49`（测试，按§五.2 不计入消费者）。故「卸载静默丢义务」今天是**潜在 API 能力**，不是线上可达路径。J2 的现实载体须改挂在**可达的三条生命周路径**上：①`plugins:` 声明禁用（只作用于 agent 级）；②`WithPlugins` 定制 run 级实例集；③代际切换重建五个 generation-scoped 消费者。S5 的红按 disposer 面取得，其生产可达性另记（§十）。

## 二、实证证据（✅ 读码；行号只作定位，不作契约）

| 编号 | 证据 | 含义 | 限制 |
|---|---|---|---|
| E12-1 | `assembly/facts.go:142-155`：`reduceCommitted` 把 `states.Reduce` 失败交 `assemblyError` 记录后返回 | 应用失败不使提交失败，也不阻塞切面 | 未测「失败后经重启是否真的丢」的端到端行为，R1 须补红锚 |
| E12-2 | `assembly/checkpoint.go:297-306`（水位＝分区落地序号）、`:355`（`flight.await` 等在途提交单元结束）、`:373`（写入 `r.checkpointOffset`） | 水位不看应用结果；`flight.await` 只保证「提交单元结束」，不保证「应用成功」 | 同上 |
| E12-3 | `assembly/checkpoint.go:1124-1157`＋`:1158-1166`：恢复只重放「水位之后」的事实 | 水位内未应用的事实此后无重放路径 | 会话切面路径（`sessionCut` :540-566）同构，R1 须两路都证 |
| E12-4 | `runtime/task/wrapper.go:309-311`（`Advance` 失败裸 `return`）、`:312-314`（信箱写入失败裸 `return`） | 结算事实已提交之后，消费失败被静默丢弃，无日志无返回 | 是否有第二个同类站点须 R1 穷举（按「提交成功后返回 error 被吞」的语义单元枚举，不按代码形态） |
| E12-5 | `runtime/state/checkpoint.go:171-188`：disposer 删 `holders[name]`/`names`；`:402-415` `PendingRestore` 只报告 | 运行期注销不检查未完成义务；boot 侧仅「无 holder 的切片」被具名拒绝（`assembly/host.go:777-779`） | 未测注销时有欠账的最小复现，R1 补 |
| E12-6 | `runtime/state/genregistry.go:121-137`（`Register`）、`:144-162`（`Build`）、`:71-77`（`GenInstances.Restore` 仅候选未生效时回滚） | 代际注册表只构造与回滚，无「把已陈述态下发新实例」的一步 | **【12.R1 实测结论】原「成功切换的新实例不继承旧实例已陈述的状态」须收窄**：五个 generation-scoped 消费者（`host_document.go:688/1409/1528/1589/1677`：hostFace/telemetryFace/governanceFace/memoryFace/meditationFace）**一个都不注册状态切片**；七个切片全在 run/host 级注册一次，代际切换不重建、不清空。故今天**不存在「随代际丢失的已陈述态」**。代际重建的是配置派生对象（governance 规则→新 policy，host_document.go:1546；meditation plan→新 loop，:1704），其**进度**经持久权威承接：meditation 水位住在 storage root 下的 anchor record（host_document.go:1661 注释＋既有绿锚 `assembly/hotupdate_generation_test.go:471 TestHotReloadMeditationAnchorSurvivesRestart`）。**这是 J2「落回持久权威」的既有先例，不是新通道**。3 代以上累积行为仍未实测（§十四） |
| E12-7 | `assembly/run.go:576-580`（三 run 级切片无条件注册）早于 `:615`（`mountBuiltinPlugins`）；`cfg.plugins` 在 `:608-614` 先应用 | 切片生命周与消费实例生命周未声明为同一件事 | **【12.R1 实测结论】原「`compressSlice` 对 `r.compress` 无 nil 保护 → 禁用后 panic」的推断不成立于声明路径**：`mountBuiltinPlugins`（run.go:615）无条件执行且必挂 `compress-consumer`（plugin_catalog.go:111 → compress_mount.go:22-27），故经 `Assemble` 构造的 Run 中 `r.compress` 恒非 nil；`plugins:` 声明**只能禁用 agent 级**插件，写 `-compress-consumer` 被具名拒绝（plugin_catalog.go:302-306），既有绿锚 `assembly/plugin_mount_test.go:200 TestPluginDeclarationRefusesARunLevelPluginName` 钉住该拒绝。nil 解引用只在绕过 `Assemble` 手工构造 Run 时可达，不是线上路径。本行结论收窄为「**无交接声明**」，不记「必 panic」 |
| E12-8 | `assembly/task_checkpoint.go:32-50`、`runtime/state/mailbox_checkpoint.go`：tasks/mailbox 切片无 `SnapshotSessionState` | 任务账与信箱账是 registry 全局账，**不进会话切面**；压缩/投影/输入切片才按会话陈述 | 反例：不要因「会话切面」是主流形态就要求全部状态按会话切 |

**未核事项（12.R1 逐项结账，详见§九）**：①注销时欠账是否真的丢失——**已实测：真丢**（S5 原型，§十三）；②3 代以上替换的累积态——**仍未实测**，但已结构性排除「切片随代际重建」（E12-6 勘误）；③外域（03/05/07）清减后本域负锚是否仍红转绿——**本轮不可测**（外域未实施），保持未核；④`plugins:` 禁用 compress 的实际运行表现——**已实测：既非 panic 也非静默，而是具名拒绝**（E12-7 勘误，既有绿锚复跑 exit 0）。

## 三、身份源与计费口径（不强迫每种状态归会话）

| 状态族 | 身份源 | 当前有效态 | 历史（按身份可查） | 是否进会话切面 |
|---|---|---|---|---|
| 任务提交账户（revisions/facts/mutations/specs/欠账） | task 身份（registry 全局账） | registry 现存任务 | 任务事件链（`fact:` 族）按 task 身份 | 否（E12-8） |
| 任务三族收据（consumed/attempts/rejected） | notice 四段身份（task+round+kind+seq） | 仍欠账／在飞的通知所需判重身份 | 已结收据的判重身份（保留到按身份重放仍可答的边界） | 否 |
| 信箱欠账/回执 | site（address+instance）+ accept seq | 未 `ClaimConsumed` 的条目 | 已消费条目留链 | 否 |
| 压缩账（judgments/answered） | 会话链 + request 身份 | 未答判决 + 已答身份 | 已结判决留链 | 是（`SnapshotSessionState`）——**清减归 03** |
| 治理账（两流尾/denial 索引/目标/审批证据） | 19 位零填充流序号 + 请求 ID | 两流尾 + 未结审批 | 治理事件链、attention 事实链 | 否——**清减归 07** |

**计费纪律**：本域只承诺「当前有效态按有效工作规模计费」，**不承诺每种状态 O(1)**；已结历史本身可增长（链是真相），其按身份读取成本单列在 06/09。任何以「删掉 `answered`/已结收据」换来的常数级成本都是伪造达成，直接判红。

## 四、候选设计（未冻结）

### 12.R2 已提交但未消费的处理边界

三条候选，R1 必须择一并给出负锚；均未冻结：

- **甲·应用水位随切面**（原推荐，**已被 12.R1 否决，见§十一**）：每个切片陈述 `AppliedThrough`（候选字段名），切面水位取「已应用位置」而非「已落地位置」；未应用的事实留在水位之后，由下次恢复重放。优点：不改提交路径，恢复侧天然正确。代价：切片必须自证应用位置，`SnapshotState` 契约不变但载荷增加。
- **乙·应用失败拒绝切面**（**12.R1 择定**）：`Reduce` 失败让切面失败（今天 `SnapshotState` 失败即整份拒绝，assembly/checkpoint.go 的 redline「MUST refuse the whole checkpoint」同构）。优点：最简单、最响亮。代价：一个消费者长期失败会让检查点永久停摆（欠账无界），须配具名告警门——**该告警门今天已存在**（checkpoint.go:680 的 ERROR＋`Close` 上抛，绿锚 `TestAFailedCheckpointSurfaces`），不需新造。
- **丙·恢复期按切片水位重放**：不切面侧改动，恢复时对每个切片额外重放一段「切面之后但未应用」的事实。缺点：要判「未应用」仍需一个位置，等价于甲但更绕。

**已否**：把失败事实从链上「跳过」（违反 P1 红线）；用第二份「应用日志」做真相（第二真相，违 P2）。

**消费失败上报**：`Wrapper.execute`（runtime/task/wrapper.go:301-316）的两处裸 `return` 改为具名错误上报（`HarnessError` 取向，遵循 `dsh-error-handling` 同源项目规范的本地既有做法：具名、可定位、不吞），并保留义务（信箱条目回落到欠账，`Release` 语义既有 runtime/state/mailbox.go）。**不**把多个外部动作塞进一个事务；**不**承诺无接收方支持的 exactly-once——恢复只恢复态，再继续未完义务。

### 12.R3 动态挂载/禁用/卸载/替换的义务交接

- **注册面不新建**：builtin 目录（`assembly/plugin_catalog.go`）、`plugins:` 声明、`Stateful` 注册**三者保持现状**；本叶只补「消费实例与切片的生命周对齐」这一件事。builtin 与扩展走同一份声明（禁用 builtin 与禁用扩展是同一条路径）。
- **候选交接声明**（未冻结）：切片侧声明「谁承接我的未完义务」；无承接者时禁用/卸载**具名拒绝**，拒绝理由指出该切片陈述了几条未完成项。
- **替换**：代际切换成功后，新实例必须绑定同一切片名与同一权威（切片不随代际重建）；候选做法是在切换后由切片侧把已陈述态重新下发新实例（复用 `RestoreState`），**不**新建「代际态迁移」通道。
- **已否**：为插件新增第四挂点；把义务塞进 `GenResources.Resources`（那是租约面，不是义务面）。

## 五、症状直译锚（用户视角，第一天红）

| 锚 | 用户视角直译 | 今天 | 修复后 | 回滚 |
|---|---|---|---|---|
| **S4** | 一个消费者的应用/消费失败之后重启，用户不应看到同一条通知被再派一次，也不应看到「该做的活不见了」；系统应给出具名失败 | 红（失败被 `assemblyError` 记录或裸 `return` 吞掉，重启后该消费者的状态落后于链） | 绿（具名失败 + 义务保留 + 重启补做恰一次） | 必红 |
| **S5** | 禁用一个带状态的插件后重启，用户不应看到它的未完义务凭空消失，也不应看到 boot 卡死而不说原因 | 红（无交接，boot 侧只有 pending 名拒绝） | 绿（具名交接或具名拒绝并指出未完成项数） | 必红 |

S4/S5 是验收矩阵第一行；工程化目标（切片载荷、交接声明）只能追加，不得替换这两句。

## 六、风险与回退

- **切面停摆**（乙案）：长期失败的消费者使检查点永不落盘 → 必须有具名告警与容量兜底（不是 TTL 删义务）。
- **义务重复**（甲案）：水位与重放边界判错 → 同一通知二次驱动；负锚必须钉「重启后同身份只结算一次」，而不是钉调用次数。
- **生命周期误伤**：把「每次 boot 都重新注册」读成「替换」→ 交接被误触发；判据必须是「同一切片名的持有者变更」，不是注册次数。
- **回退**：本域是增量契约——回退＝恢复「切面按落地序号推进 + disposer 直删」，症状回红（S4/S5 红转绿两次证据作 F3 材料）；不删任何已提交事实，切片载荷新增字段为追加键，回退后旧代码忽略未知键（`encoding/json` 既有行为），无需数据迁移。
- **不越界**：压缩账清减（03）、任务账清减（05）、治理账清减（07）不在本域；本域只提供 J1 边界与负锚。

---

## 七、12.R1 执行记录与源树绑定（本轮实测，非规划）

**源树绑定三件（ORCHESTRATION §②，R19 起强制）**：

1. **本叶读/量的是 HEAD `9547faca` 的隔离副本**：`git archive 9547faca | tar -x -C /tmp/recovery-g2consumer/tree`。全部 build/vet/test 与两枚红锚均在该隔离树执行。主树只读；`git diff --stat HEAD -- <18 个读取目标文件>` 输出为空，故主树读码与隔离树逐字节等价，行号可互引。
2. **本叶不引用任何 CI 记录**，不用工作树结果解释 CI 红。
3. **工作树候选未被本叶消费、未被删改**。另一单正在编辑的五个文件（`memory/ledger/segment_store.go`、`segment_store_test.go`、`cold_archive_test.go`、`journal_index_cut_test.go`、`assembly/symptom_anchor_recovery_test.go`）本叶**零读零写**；S2 量面归 09.R2，本叶不碰。

**写集**：主树只写本域 `design.md` 与 `tasks.md`；**主树零 `.go` 写入、零 `go build`/`go test`**。原型只写隔离树的 `assembly/consumer_lifecycle_anchor_test.go` 与 `runtime/task/consume_failure_anchor_test.go`，二者**未进主树**——故任务行「拟新增、尚未建立」在主树仍然成立，本叶不勾 12.R1（理由见§十五）。

**隔离树环境**：`go build ./...` exit 0；`go vet ./assembly/` exit 0；`go vet ./runtime/task/` exit 0。所有 `go test` 命令均带 `-count=1` 或 `-count=3`，命中用例数逐条记录于§九。

## 八、G2 判据表（本域部分，供编排者合并；不定义他域水位）

每条判据都附**可构造的失败例**（R24 N11 教训：不能失败的判据不是证据）。「今日」列全部为隔离 HEAD 树实测。

| 判据 | 可失败构造（删掉什么会真的坏） | 今日实测 | 结论 |
|---|---|---|---|
| **G2-C1** 切面水位不得覆盖任一挂载消费者未应用的事实（J1） | 令一个消费者的 `Reduce` 拒绝一条具名事实→提交→切面→重启。若水位覆盖该事实且重启不重放，判据失败 | **失败**：S4 原型 3/3 红。cut `checkpoint:16:16` 覆盖 `behind-1-2`，重启 fold 8 条、重放了 cut 后事实，唯独该事实既不在恢复载荷也不在重放集 | **FAIL** |
| **G2-C2** 消费完成失败须具名上报且义务保留 | 令信箱写入返回具名错误→观察是否有任何错误行点名 task 与拒绝原因 | **半失败**：义务**保留**（task 仍欠 1 条），但**零错误行**——`runtime/task` 全包无任何日志/错误上报通道 | **FAIL（上报面）** |
| **G2-C3** 同一状态应用跨重启幂等 | 同一 root 连启两次、期间零提交；若第二次派生态或欠账集与第一次不同（含翻倍），判据失败 | **成立**：`TestSameStateAppliesIdempotentlyAcrossTwoRestarts` 绿（派生态逐字节相等、欠账集同身份同序、非空守卫通过）；信箱两路恢复去重由既有绿锚钉住 | **PASS** |
| **G2-C4** 注销不得静默丢弃已陈述态与未完义务（J2） | 切片陈述一条未完项→切面→调 disposer→再切面→重启。若该项在任何切面与任何 pending 拒绝中都不出现，判据失败 | **失败**：S5 原型 3/3 红。首 cut 陈述该项，disposer 删持有者，次 cut 与 boot 均只名 `[compress inputs mailbox projection tasks]`，**无 pending 拒绝** | **FAIL** |
| **G2-C5** 挂载/禁用/卸载/替换经既有声明面，义务有具名去向 | ①`plugins: -compress-consumer`；②`-age-fold`；③代际切换。若任一路径让义务无去向且不具名，判据失败 | **部分成立**：①**具名拒绝**（绿锚复跑 exit 0）；②age-fold 无切片，禁用不产生无主切片；③七个切片不随代际重建，meditation 进度经 anchor record 承接（绿锚）。**缺口**：disposer 面（C4）与「无承接者时拒绝禁用」的运行期声明 | **PARTIAL** |
| **G2-C6** 恢复与运行期同一派生，**含失败语义** | 同一条被拒绝的事实，分别走运行期提交与恢复期重放，比较两路的失败处置 | **失败（不对称）**：运行期 `facts.go:152-154` 记 ERROR 后**吞掉**、提交成功；恢复期 `run.go:2995-2997` **上抛**、boot 失败。派生函数同一，失败语义不同 | **FAIL** |

**本域不认领**：任务账清减水位（05.R1）、治理账清减水位（07.R1）、压缩账清减（03）、boot 成本量名与阈值（09.R2）。三域共用一张 G2 表，由编排者合并；本表只填 C1–C6 六行。

## 九、E12-1..E12-8 复跑与补证

| 编号 | 复跑/读码方式 | 实测结果 | 结论 |
|---|---|---|---|
| E12-1 | 读 `assembly/facts.go:142-155`＋S4 原型日志 | 原型输出真实 ERROR 行：`assembly/facts.go:153 tagent: the committed fact behind-1-2 was not folded into the state of its consumers: state: fold the fact behind-1-2 into the "assembly.consumer-behind" state slice: …` | ✅ 成立，但**须勘误**：应用失败**有**具名 ERROR（点名事实与切片），缺的是重放路径，不是报告 |
| E12-2 | 读 `assembly/checkpoint.go:297-311`（水位＝`landedSeq`）、`:355`（`flight.await`）、`:371-376`（写 `checkpointOffset`） | 水位只读落地序号；`flight.await` 只等在途提交单元结束 | ✅ 成立 |
| E12-3 | 读 `assembly/checkpoint.go:1124-1152`＋`:1158-1164`；S4 原型实证 | 重放只取 `pastCut` 的事实；被覆盖的未应用事实无重放路径 | ✅ 成立，**端到端已证**（原限制「未测端到端」已消） |
| E12-4 | 读 `runtime/task/wrapper.go:301-316`＋负锚原型 | `:309-311` 与 `:312-314` 均裸 `return`；`:303-306` `envelopeOf` 不 ok 亦裸 `return`（**在提交之前**，不属「提交成功后吞错」）。`runtime/task` 全包 grep 无 logger/错误通道 | ✅ 成立，**穷举见§十二** |
| E12-5 | 读 `runtime/state/checkpoint.go:172-186`、`:405-417`；S5 原型 | disposer 只删 `holders`/`names`，无返回、无检查；`PendingRestore` 只报告 | ✅ 成立，**最小复现已取得**（原限制已消） |
| E12-6 | 读 `genregistry.go` 全文＋`host_document.go` 五个 registration＋绿锚复跑 | 见§二 E12-6 勘误：无切片随代际重建 | ✅ 成立但**结论收窄** |
| E12-7 | 读 `run.go:576-617`、`plugin_catalog.go:275-314`、`compress_mount.go`＋绿锚复跑 | 见§二 E12-7 勘误：声明路径具名拒绝，nil 不可达 | ✅ 成立但**推断被否** |
| E12-8 | 读 `assembly/task_checkpoint.go:32-50`、`runtime/state/mailbox_checkpoint.go` 全文＋两条跨会话绿锚复跑 | tasks/mailbox 无 `SnapshotSessionState`；会话切面不含全局账 | ✅ 成立，**反例保持绿** |

**既有锚复跑（隔离 HEAD 树，全部 `-count=1 -v`）**：

| 命令 | 命中 | 退出码 |
|---|---|---|
| `go test ./assembly/ -run '^(TestReducerDerivesEqualAtRuntimeAndRecovery\|TestNewConsumerRegistersSliceOnly\|TestNoPerConsumerDispatchPath)$'` | 3 RUN / 3 PASS | **0** |
| `go test ./assembly/ -run '^(TestCrossSessionObligationsStayInTheirTargetSlice\|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice\|TestBootSeatsTheMailboxAfterTheDurableAccount)$'` | 3 RUN / 3 PASS | **0** |
| `go test ./runtime/state/ -run '^TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState$'` | 1 RUN / 1 PASS | **0** |
| `go test ./assembly/ -run '^(TestPluginDeclarationDisablesABuiltinPlugin\|TestPluginDeclarationRefusesAnUnregisteredName\|TestPluginDeclarationRefusesARunLevelPluginName\|TestBootAdoptNewConsumers\|TestHotReloadMeditationAnchorSurvivesRestart\|TestHotReloadGovernanceNoRestart)$'` | 6 RUN / 6 PASS | **0** |
| `go test ./assembly/ -run '^TestSameStateAppliesIdempotentlyAcrossTwoRestarts$' -count=3`（**拟新增原型，只在隔离树**） | 3 RUN / 3 PASS（90.26/90.18/90.18s） | **0** |

**幂等原型的一次假红已归因（不冒充结论）**：首版夹具用 `declareObligationTargeting` 造欠账，那是**投递账**义务而非**信箱**欠账，故非空守卫具名报「the first boot owes nothing, so the idempotence comparison proves nothing」并红 3/3。改用 `state.NewDurableCommit` 直接提交一条带 Outbox 的 `task.CommitRecord`（沿用 `run_test.go:902` 既有夹具形态）后 3/3 绿。**守卫按设计拦住了空转，不是判据被放宽**。每次 boot 的 `Close` 因欠账未清而烧满 30s 预算（WARN「the close reached its budget before the final checkpoint was filed」），故两次 boot 恢复的是**同一个已落盘 cut**——这正是幂等比较想要的条件，不是缺陷。

**行号勘误（任务行 12.R3 消费者锚）**：`TestPluginDeclarationDisablesABuiltinPlugin` 与 `TestPluginDeclarationRefusesAnUnregisteredName` 实在 **`assembly/plugin_mount_test.go:150` 与 `:188`**，不在任务行所写的 `assembly/hotupdate_generation_test.go:622`（该行是 `TestBootAdoptNewConsumers`）。`-run` 是包级正则，命令本身不变、验收面未缩水；仅定位勘误。

## 十、生命周期四态定义表（12.R3 的实施契约，R1 定义）

「应交接什么」一律指**该消费者持有的未完义务＋已陈述派生态**，不是租约（`GenResources.Resources` 是租约面，已否）。

| 态 | 触发入口（今日真实存在） | 应交接什么 | 交给谁 | 无承接者时如何拒绝 | 今日实测 |
|---|---|---|---|---|---|
| **挂载** | `StateRegistry.Register`（checkpoint.go:131）；`Run.RegisterStateSlice`（assembly/checkpoint.go:284） | 无需交接：新切片从 `pending[name]` 取回已陈述载荷（:152-171），取不到则由 boot 的 `RestoreEmptyState`  seat | 框架自身（pending 队列） | 名字为空／重名→具名拒绝注册（:139-149）；restore 失败→回滚注册并具名拒绝（:157-169） | ✅ 已具名，绿锚 `TestNewConsumerRegistersSliceOnly` |
| **禁用** | `plugins: -<name>`（**仅 agent 级**，plugin_catalog.go:275-314）；`WithPlugins` 定制 run 级集（run.go:608-614） | 被禁用者若持有切片：其未完义务＋已陈述态 | 具名承接者，或**落回持久权威**（按身份可读的提交事实）并保持欠账 | **三条路径择一（R1 裁定：取「具名拒绝该禁用」）**：拒绝理由须指出该切片陈述了几条未完成项。boot 期读到无 holder 的切片名沿用既有 pending 拒绝（host.go:777-779） | ⚠️ agent 级可禁用的三个插件（lease-observer/age-fold/telemetry-observer）**均不持有切片**，故今天禁用不产生无主切片；run/host 级写 `-compress-consumer` **已被具名拒绝**（绿锚）。**缺口＝运行期禁用一个持切片者时无「未完成项数」拒绝** |
| **卸载** | `Register` 返回的 disposer（checkpoint.go:172-186） | 同上 | 同上 | **具名拒绝注销**，错误消息指出未完成项数；SHALL NOT 先删持有者再报告 | ❌ disposer 无返回值、无检查（S5 红）。**且七个生产注册点全部丢弃它**，唯一调用者是测试——故本态今天是**潜在能力**，12.R3 须先让它有生产调用方，否则「拒绝注销」无宿主 |
| **替换** | `GenRegistry.Build`（genregistry.go:144）＋发布路径（run.go:805、host_document.go:1150） | 旧实例的未完义务＋已陈述态 | **同一切片名下的同一权威**（切片不随代际重建）；新实例经既有 `RestoreState` 取得继续所需的态 | 候选生效前找不到承接者→**不发布该候选**，生效绑定与切片保持旧代（`GenInstances.Restore` 已是该回滚面） | ⚠️ 今天五个代际消费者**都不持有切片**，故无态可丢；meditation 进度经 anchor record 承接（绿锚）。**缺口＝无「新实例绑定同一权威」的显式一步**，一旦将来有代际消费者持切片即失守 |

**R1 裁定（三条路径择一）**：禁用/卸载一律取**「具名拒绝并指出未完成项数」**，不取「自动交接给承接者」。理由：①今天不存在任何具名承接者注册面，自动交接需要新造第二注册体系（proposal 明禁）；②「落回持久权威」对**账本型**切片（mailbox/tasks）已天然成立（欠账由 `DurableCommit.owed` 持有，`OwedNotices` 按身份可读），但对**派生型**切片（projection/inputs/compress）不成立——其态是 fold 的结果，链上无「该消费者已应用到哪」的记录；③具名拒绝是最小变更面且可回滚（回退＝恢复直删，S5 回红）。

## 十一、三候选择定：**取乙案**（推翻§四原「甲·推荐」）

**裁定：乙·应用失败拒绝切面。** §四原推荐甲，R1 据全局复杂度改判乙；原候选描述保全不改，只追加裁定与理由。

**哲学推导（非局部省事）**：检查点的既有红线是「一个切片拒绝陈述自己→整份切面拒绝」（assembly/checkpoint.go:325）。该红线的**本意**是「切面必须是 boot 能据以启动的态」。一条被消费者拒绝应用的事实，使该态**不是**boot 能据以启动的态——与「切片拒绝陈述」是**同一语义单元**的两种成因。故乙不是新机制，是把既有红线的判据从「陈述失败」扩到「应用失败」，**边界画在语义单元上**（元规则三）。

**全局复杂度对比**：

| 案 | 新增结构 | 新增契约面 | 复用既有失败路径 | 停摆风险 |
|---|---|---|---|---|
| 甲 | 每个 reducer 切片的**按分区应用位置映射**（7 切片×分区），＋切面取 min | `SnapshotState` 载荷增字段；会话切面同构改造 | 无（切面成功但水位回退，需**新造**「水位被压住」的可观测量） | 有（水位钉住） |
| **乙** | **一个「上次成功切面以来有无应用拒绝」的记录**（成功切面即清） | **零**（`Stateful` 三方法不变、载荷不变） | **有**：`checkpoint.go:680` 的 ERROR＋`Close` 上抛，既有绿锚 `TestAFailedCheckpointSurfaces`（checkpoint_test.go:979）钉住 | 有（切面钉住） |
| 丙 | 恢复期第二套「切面之后但未应用」边界计算 | 恢复期新增读路径 | 无 | 有 |

**否甲理由**：①它把「已应用到哪」做成**第二份表示**，而「有没有拒绝过」已足以回答同一问题（单一真相，P2）；②它同样钉住水位，**付了停摆代价还多付载荷与按分区映射的代价**；③它需要一个新的「水位被压住」观测量——按 R19 N2／R20 N6 的教训，新观测量必须有独立于它而存在的消费者，而该量的唯一消费者会是「解释水位为何不动」的日志行，形态与被删的 `LoadStats.Families` 同类，**本叶不提议**。

**否丙理由**：边界住在读者而非写者，恢复期多一条只在恢复期存在的读路径，与「恢复与运行期同一派生」的本意相违；且它仍需一个应用位置，等价甲而更绕。

**乙的代价须显式登记（不溶解）**：一个**长期或一次性**拒绝应用的消费者会把切面钉住，热层随之增长，直到重启。这是**正确信号**而非可接受偏差：规格禁止以 TTL 删义务换有界。缓解只能走**既有具名 ERROR 告警**，不得新造容量兜底删义务。**12.R2 实施约束**：拒绝记录须在「该事实其后被成功应用」时清除，否则一次瞬时失败即永久钉住切面；清除条件与记录粒度由 12.R2 定，本叶不冻结字段名。

**已否（保全原裁定）**：把失败事实从链上跳过（违 P1 红线）；用第二份「应用日志」做真相（违 P2）。

## 十二、「提交成功后返回 error 被吞」站点·语义单元枚举

按**结算／消费／投递完成**三类语义单元枚举（不按 `return` 形态扫）。每单元先定「提交成功」的权威判据，再枚举其后的完成动作。

### 单元一·结算（task 状态/修订提交成功后）

权威判据：`applyCommitLocked` 返回 `CommitCommitted`（runtime/task/lifecycle.go:385-396）。

| 站点 | 提交成功后的动作 | 处置 | 判定 |
|---|---|---|---|
| `runtime/task/wrapper.go:312-314` | 信箱写入 `w.mailbox` | **裸 `return`，零日志零返回** | ❌ **吞**（负锚红） |
| `runtime/task/wrapper.go:309-311` | `Advance` 自身失败 | 裸 `return` | ⚠️ 吞，但**提交未成功**，不属本单元判据；仍须具名化（12.R2） |
| `runtime/task/wrapper.go:303-306` | `envelopeOf` 不 ok | 裸 `return` | ⚠️ 在提交**之前**，不属本单元 |
| `runtime/task/lifecycle.go:250-262`（Register）、`:359-365`（Resume）、`cancel.go:100-110`、`reactivate.go:73-88`、`:154-165`、`manager.go:123-133`（Handoff） | 义务开闭 | 全部 `return err` | ✅ 不吞 |
| `runtime/agent/delegate.go:650-675` | `Flush`／`land` | `return notice, err` | ✅ 不吞 |
| `runtime/state/migrate.go:242-247` | 归档导入 | `return MigrationReport{}, err` | ✅ 不吞 |

### 单元二·消费（已提交事实的派生应用）

权威判据：事实已落链（`persistFact` 成功且进入 `r.facts`）。

| 站点 | 动作 | 处置 | 判定 |
|---|---|---|---|
| `assembly/facts.go:152-154` | 运行期 `states.Reduce` | `assemblyError` 具名记录后**返回 void**，提交成功、水位照推 | ❌ **吞**（S4 红；有报告但无重放） |
| `assembly/run.go:2995-2997` | 恢复期 `states.Reduce` | **上抛**，boot 失败 | ✅ 不吞——**与上一行不对称，即 G2-C6** |
| `assembly/run.go:1125-1155` | 任务账 restore | `assemblyError`＋`return err` | ✅ 不吞 |
| `runtime/state/durable.go:733-742` | 消费收据写＋Sync | `durableError`＋`return err`，`result=unconsumed` | ✅ 不吞 |

### 单元三·投递完成（送达/签收后的收据落链）

权威判据：外部动作已发生（`sender.Send` 返回 nil，或通知已被受理）。

| 站点 | 动作 | 处置 | 判定 |
|---|---|---|---|
| `assembly/host_delivery.go:990-993` | 送达后写 sent 收据 | `assemblyWarn`＋`return deliverySent, sentRecordNote(err)`，**义务不丢** | ✅ 具名＋保留（**本域应沿用的模板**） |
| `assembly/host_delivery.go:1513-1516` | 重投送达后写收据 | `assemblyWarn`＋return | ✅ 具名 |
| `assembly/host_delivery.go:512-515` | 审批送达后写收据 | `errors.Join(landed, restoreHeldDelivery(...))`→**义务回落欠账** | ✅ 具名＋保留 |
| `assembly/host_delivery.go:1661-1680`、`:1932` | 重试/耗尽状态写 | `assemblyWarn`＋`parkHeld` 保留义务 | ✅ 具名＋保留 |
| `assembly/host_delivery.go:868-871` | headless 退位写 | `assemblyWarn`＋return | ✅ 具名 |
| `assembly/host_tools.go:406-409` | 回卷失败 | `breakReentry`＋`assemblyError`（含「卡死round」后果） | ✅ 具名 |
| `assembly/host_tools.go:707-710` | 拒绝审计事实写 | `assemblyError` | ✅ 具名 |
| **`runtime/agent/loop.go:371-375`** | `Backflow` 写送达事实失败 | `consumerWarn` 后**继续 `in.deliver(...)`**：模型收到了通知，链上无该送达记录 | ⚠️ **具名但不停**——重启后链无收据，同一通知可被再派。**新发现，本域登记，归 12.R2 判定是否须停投递** |
| **`runtime/agent/loop.go:401-407`** | `commitProgressFact` 写进度事实失败 | `consumerWarn` 后返回 void | ⚠️ 具名但不停（进度事实是审计面，非结算收据，风险低于上一行） |
| `assembly/facts.go:936` | `store.Consume` | `return err`→**被单元一的 wrapper.go:312 吞掉** | ❌ 错误的真实来源在此，吞点在 wrapper |

**枚举结论**：三类单元共 **2 个真吞点**（`wrapper.go:312-314`、`facts.go:152-154`）＋**1 个具名但不停的新发现**（`loop.go:371-375`）。单元三整体已是「具名＋义务保留」的成熟形态，**12.R2 的上报改造应沿用单元三模板，不新造机制**。

## 十三、S4/S5 红锚实证（隔离 HEAD 树，拟新增、主树尚未建立）

| 锚 | 测试全名 | 命令 | 命中 | 退出码 | 红因（原文摘录） |
|---|---|---|---|---|---|
| **S4** | `TestConsumerBehindTheChainSurvivesARestart` | `go test ./assembly/ -run '^TestConsumerBehindTheChainSurvivesARestart$' -count=1` | 1 RUN / 1 FAIL | **1** | 「the restarted consumer never applied the committed fact behind-1-2: the cut checkpoint:16:16 states the watermark 16:16, the boot folded 8 fact(s) and replayed behind-past-the-cut, yet behind-1-2 is neither in the restored payload nor in the replay」 |
| **S5** | `TestUnmountKeepsTheOwedItemsOfItsSlice` | `go test ./assembly/ -run '^TestUnmountKeepsTheOwedItemsOfItsSlice$' -count=1` | 1 RUN / 1 FAIL | **1** | 「the open obligation the-notice-still-owed vanished with the unmount: the first cut checkpoint:16:4 stated it, the disposer deleted the holder without a named refusal or a named successor, the second cut checkpoint:16:5 states the slices [compress inputs mailbox projection tasks], and the boot restored checkpoint:16:6 … naming no pending slice」 |
| **负锚** | `TestConsumptionFailureIsReportedByName` | `go test ./runtime/task/ -run '^TestConsumptionFailureIsReportedByName$' -count=1` | 1 RUN / 1 FAIL | **1** | 「the consumption failure of task task-0d8d37f7… (notice round 1 kind settled) was reported by no line: the settlement committed, the mailbox refused with …, the task still owes 1 obligation(s), and Wrapper.execute returned bare — 0 error line(s) recorded」 |

**稳定性**：三锚各 `-count=3` 复跑，**3/3 同因红**（S4 30.32/30.25/30.34s，S5 0.17/0.13/0.15s，负锚 0.00s×3），无 flaky、无顺序敏感。

**非空转守卫（R24 N11 教训，逐条具名失败）**：S4 断言前依次核①拒绝确发生恰一次②该事实确已提交（`CommittedEvent` 无错）③运行期确未应用④其余事实确被 fold⑤切面确已落盘且点名该切片⑥载荷确不含该键⑦boot 确 fold 了事实⑧boot 确重放了 cut 后事实⑨重启后该事实仍可按身份读到。S5 核①首 cut 确陈述该未完项②disposer 后注册表确无该名③次 cut 确已落盘且键不同于首 cut④boot 确恢复了切面。负锚核①信箱 sink 确被调用恰一次②结算确已提交③义务确仍欠。**任一守卫不成立即具名失败，判据不会恒真**。

**S4 红因勘误（对§五原表述）**：原写「失败被 `assemblyError` 记录或裸 `return` 吞掉」把两类混为一谈。实测：**派生应用失败有具名 ERROR**（E12-1），红因是**切面覆盖＋无重放**；**消费完成失败才是零报告**（负锚）。两句须分开，不得合并成一个「无具名失败」的笼统归因。

**S5 的可达性限定**：S5 走 disposer 面取得红证，而该面**今天无生产调用方**（§一勘误）。故 S5 是**潜在能力红**，不是线上用户路径红。12.R3 须先给 disposer 一个生产宿主（或改以「禁用持切片者」为宿主），否则修好 disposer 也无从触发。**本叶不声称 S5 已覆盖线上禁用路径**。

## 十四、未证项（如实登记，不推断为结论）

1. **3 代以上替换的累积态**：未实测。已结构性排除「切片随代际重建」（E12-6 勘误），但「代际消费者将来持切片时是否失守」无实测。定它需：一个持有切片的 generation-scoped 消费者夹具＋≥3 次 `Reload`，比较每代切片字节与欠账集。
2. **外域清减后本域负锚是否仍红转绿**：本轮不可测（03/05/07 未实施）。保持未核，不得据本叶红证声称级联成立。
3. **乙案的停摆幅度**：未量。热层随钉住时长增长的速率归 09.R2 同条件校准，本叶不附虚构数字。
4. **`loop.go:371-375` 的重投后果**：已读码定性（具名但不停），**未构造重启后重复投递的最小复现**。定它需：令 `commitSettled` 失败→重启→观察同一通知身份是否产生第二条收据。归 12.R2。
5. **跨进程重试的「至多一条收据」**：任务行 12.R2 的 `TestRedeliveredNoticeAfterRestartSettlesOnce` 尚未建立，本叶未跑，不声称已证。
6. **本叶未检查**：`memory/ledger/**` 全部实现与测试、`assembly/symptom_anchor_recovery_test.go`（S2 量面）、`scripts/gates/**`、`golden/**`、`.github/**`、`examples/wechat-bot/**`、05/07 两域计划目录、任何真实用户数据根。G1/G3/G4 未判，G2 整体未判——本叶只交 C1–C6 六行。

## 十五、12.R1 结账判定

**本域 G2 部分＝FAIL（C1/C2/C4/C6 红，C3 绿，C5 部分）。** 这是有效判决，不是叶失败。

**不勾 12.R1 的理由（正确纪律，非遗漏）**：任务行正锚/负锚的文件白名单是 `assembly/consumer_lifecycle_anchor_test.go` 与 `runtime/task/consume_failure_anchor_test.go`，而本轮派发令**禁止在主树写任何 `.go`**（另一单正在编辑同包文件，构建闭包不可信）。两锚已在**隔离 HEAD 树**建立并 3/3 红，但**主树仍不存在该二文件**，故任务行「拟新增、尚未建立」的字面状态未变，勾选会把隔离树证据冒充主树落盘。待编排者释放 `assembly/`／`runtime/task/` 写面后，将隔离树原型按白名单落入主树并复跑同命令，方可勾。

**已交付（任务行①–⑥逐项）**：①J1/J2 判据＋反事实＋可达性勘误（§一）；②E12-1..E12-8 复跑与补证、未核四项逐项结账（§二/§九）；③三候选择定＝**乙**，甲丙被否理由（§十一）；④生命周期四态定义表＋三条路径择一裁定（§十）；⑤S4/S5 两枚红锚＋负锚，3/3 稳定红、守卫非空转（§十三）；⑥三类语义单元枚举，2 真吞点＋1 新发现（§十二）。

## 十六、锚落盘勘误（主树，2026-11-16 第二轮）

本节只登记落点与未落项；§九/§十三/§十五 的判定、实测数字与退出码**一字未改**。

- **已落盘一枚**：`TestConsumptionFailureIsReportedByName`（冻结名未改）从隔离树的独立文件改为并入 `runtime/task/unsettled_test.go`——该文件是 `unsettled.go` 的镜像且已拥有 `durable-delivery.md#envelope-states` 锚，独立新文件会触发 `responsibility-fragmentation`。主树实测红，失败原文与 §十三 表一致。
- **未落盘三枚**：S4 `TestConsumerBehindTheChainSurvivesARestart`、S5 `TestUnmountKeepsTheOwedItemsOfItsSlice`、`TestSameStateAppliesIdempotentlyAcrossTwoRestarts`。三者住 `assembly/`、引 `memory/ledger`，均在本轮写面外（并发单在写该包）。故 §十五「不勾 12.R1」的**实质理由只剩一条**：**本叶冻结正锚（S4）在主树不存在**，其余（S5/C3 与已落负锚）不改变该结论。
- **`assembly/` 两枚的归属**：须由编排者另派一单落盘并复跑 §十三 同命令；本域不改动它们的判据或守卫。
- **退役相关性（R51）**：本域四枚锚（含未落三枚）均不引用退位机制，无「任务五族留热」型判据，本轮无因 R51 排除者。

## 十七、12.R1 第三轮：两枚红锚落主树（2026-11-18 实测）

**源树绑定**：HEAD `9547faca` ＋工作树（`git status --short` 250 文件改动，属本 change 其他域在飞实施，非本叶写入）。本叶实测窗口 11:48–11:54。`go build ./...` **exit 0**；`go vet ./runtime/state/ ./runtime/task/ ./assembly/` **exit 0**（均在 11:51 复跑）。窗口内 `assembly/read.go`（11:50）、`assembly/checkpoint.go`（11:51 后）出现**在飞编译错**（`"errors" imported and not used`、`checkpoint.go:1081 cannot use "past" ... as int`），属并发单半成品，**未代改**，见 tasks.md 在飞记录；11:52 该单落定后，S4/S5 与既有锚**全部复跑一遍**，结果与下表逐条相同（红因原文一致）。

### 十七·1 落盘结果

| 冻结锚 | 落点 | 状态 |
|---|---|---|
| S4 `TestConsumerBehindTheChainSurvivesARestart` | `assembly/consumer_lifecycle_anchor_test.go`（**本轮新增**） | 主树红，1 RUN / 1 FAIL |
| S5 `TestUnmountKeepsTheOwedItemsOfItsSlice` | 同文件（随 S4 一并落） | 主树红，1 RUN / 1 FAIL |
| 负锚 `TestConsumptionFailureIsReportedByName` | `runtime/task/unsettled_test.go`（**第二轮已落，本轮未改**） | 主树红，1 RUN / 1 FAIL |
| C3 `TestSameStateAppliesIdempotentlyAcrossTwoRestarts` | **未落**（非红锚，90s/次；判据证据仍取第二轮的隔离树复跑） | 未建立 |

**负锚未按白名单新建文件的理由（须编排者知晓，非本叶自裁缩水）**：冻结名已存在于 `runtime/task/unsettled_test.go:292`（第二轮并入，因该文件是 `unsettled.go` 的镜像并拥有 `#envelope-states` 锚）。Go 不允许同包重复声明同名测试，再建 `runtime/task/consume_failure_anchor_test.go` 会使 `go test ./runtime/task/` **整体编译失败**（连既有的 400 余条用例一并打死）。故本轮**不新建该文件**，命令 `-run '^TestConsumptionFailureIsReportedByName$'` 与测试全名逐字未变、命中数 1、红因不变——**验收面未缩水**，只改落点归属。

### 十七·2 命令 / 命中 / 退出码 / 红因原文（主树实测）

| 类别 | 命令 | 命中 | exit | 红因原文（关键句） |
|---|---|---|---|---|
| 正锚 S4 | `go test ./assembly/ -run '^TestConsumerBehindTheChainSurvivesARestart$' -count=1` | 1 RUN / 1 FAIL | **1** | 「the restarted consumer never applied the committed fact `behind-1-2`: the cut `checkpoint:16:16` states the watermark 16:16, the boot folded 8 fact(s) and replayed `behind-past-the-cut`, yet `behind-1-2` is neither in the restored payload nor in the replay — committed, never applied, never replayed again」 |
| S5 | `go test ./assembly/ -run '^TestUnmountKeepsTheOwedItemsOfItsSlice$' -count=1` | 1 RUN / 1 FAIL | **1** | 「the open obligation `the-notice-still-owed` vanished with the unmount: the first cut `checkpoint:16:4` stated it, the disposer deleted the holder …, the second cut `checkpoint:16:5` states the slices [compress inputs mailbox projection tasks], and the boot restored `checkpoint:16:6` … naming no pending slice」 |
| 负锚 | `go test ./runtime/task/ -run '^TestConsumptionFailureIsReportedByName$' -count=1` | 1 RUN / 1 FAIL | **1** | 「the consumption failure of task `task-4fa8db…` (notice round 1 kind settled) was reported by no line: the settlement committed, the mailbox refused with "the mailbox of the target refused the notification", the task still owes 1 obligation(s), and `Wrapper.execute` returned bare — 0 error line(s) recorded」（`unsettled_test.go:362`） |
| 消费者锚（既有） | `go test ./assembly/ -run '^(TestReducerDerivesEqualAtRuntimeAndRecovery\|TestNewConsumerRegistersSliceOnly\|TestNoPerConsumerDispatchPath)$' -count=1` | 3 RUN / 3 PASS | **0** | — |
| 跨会话原验收（既有） | `go test ./assembly/ -run '^(TestCrossSessionObligationsStayInTheirTargetSlice\|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice)$' -count=1` | 2 RUN / 2 PASS | **0** | — |
| 信箱两路（既有） | `go test ./runtime/state/ -run '^TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState$' -count=1` | 1 RUN / 1 PASS | **0** | — |
| 声明面（既有，**位置已迁移**） | `go test ./assembly/ -run '^(TestPluginDeclarationDisablesABuiltinPlugin\|TestPluginDeclarationRefusesAnUnregisteredName\|TestPluginDeclarationRefusesARunLevelPluginName)$' -count=1` | 3 RUN / 3 PASS | **0** | — |

**非空转守卫照旧生效**：S4 九项、S5 四项、负锚三项（§十三）全部先于断言逐条成立；三者红因均**点名水位/切片/任务与未应用键**，无一是靠「断言消息为空」空转。

### 十七·3 两门红（**只由本叶新文件引起，读数如下，未自行处置**）

| 门 | 命令 | exit | 读数 |
|---|---|---|---|
| `package-layout` | `go run ./scripts/codetools package-layout . examples/wechat-bot examples/cli-host` | **1** | 全库唯一 finding：`assembly/consumer_lifecycle_anchor_test.go: test-mirror-missing (name a test file <production>_test.go beside the file it guards)` |
| `comment_policy` | `go run ./scripts/comment_policy . examples/cli-host examples/wechat-bot` | **1** | `REGRESSION responsibility-fragmentation: baseline 0 -> 1`，唯一条目同上文件；另有一行「1 git-ignored Go file(s) … examples/wechat-bot/knowledge_base」为**基线外环境提示**（`git check-ignore -v` → `examples/wechat-bot/.gitignore:5:*` 命中；非 finding，本叶未触该树） |

**根因同一**：本域两枚锚的契约锚点是 `platform-subsystems.md#framework-checkpoint`，而该锚点已被 `assembly/checkpoint_test.go`（`checkpoint.go` 的镜像）拥有；新建的非镜像文件既无生产词干可镜像（`checkpoint.go` 已被 `checkpoint_test.go` 占用），又与既有镜像文件争同一锚。**两条建议（须编排者裁定，禁写面内无法自处置）**：

1. **并入 `assembly/checkpoint_test.go`**（工具自报的 exit ①，与第二轮负锚并入 `unsettled_test.go` 同一模式）：两门同时转绿，零豁免、零基线调整；代价是该文件再增约 300 行，且 S4/S5 的归属从「生命周期锚」变成「检查点锚」。
2. **免登记**：`scripts/codetools/package_layout.go` 的 `mirrorExemptFiles` 加一行 `assembly/consumer_lifecycle_anchor_test.go`（附「跨 checkpoint/facts/run 三文件的消费者生命周锚，无单一生产词干」理由），并在 `comment_policy` 基线为该文件加一个 ratchet 槽位。**两处均在禁写面**，本叶不得自改。

`test-doc-not-one-sentence` 首轮 2 条 findings 已在本轮修好（意图行压成单行 ≤160 rune），复跑清零。

### 十七·4 E12 复跑补证（现行树，行号只作定位）

| 编号 | 断言 | 现行树证据 | 判定 |
|---|---|---|---|
| E12-1 | 应用失败被记录但不阻塞提交 | `assembly/facts.go:142-155` `reduceCommitted`：`assemblyError` 记录后**返回 void**；S4 日志现出真实 ERROR 行 `assembly/facts.go:153 … the committed fact behind-1-2 was not folded …` | ✅ 不变 |
| E12-2 | 水位取自分区落地序号，不看应用结果 | `assembly/checkpoint.go:298-310`（`checkpointWatermark` 复制 `landedSeq`）、`:353`（`offset := r.checkpointWatermark()` 在 `flight.await` 之前取）、`:374`（写 `r.checkpointOffset`） | ✅ 不变（原 §二 引 `:297-306/:355/:373`，差 1–2 行） |
| E12-3 | 恢复只重放水位之后的事实 | `assembly/checkpoint.go:1076-1104`（`eachFactPastCut`）、`:1110-1116`（`pastCut` 仅取 `ref.seq > covered`） | ✅ 不变（原引 `:1124-1157/:1158-1166`，漂移 −48 行） |
| E12-4 | 提交成功后消费失败被静默吞 | `runtime/task/wrapper.go:303-306`（`envelopeOf` 不 ok）、`:307-311`（`Advance` 失败）、`:312-314`（信箱写失败）**三处裸 `return`**，行号与 §二 引**逐字相同** | ✅ 不变；负锚主树红 |
| E12-5 | disposer 直删，无未完成项检查 | `runtime/state/checkpoint.go:131` `Register`，返回的闭包（`:171-188`）只 `delete(r.holders,name)` 并重建 `names`；`PendingRestore` `:383-395` 只报告 | ✅ 不变；S5 主树红 |
| E12-6 | 代际切换不下发已陈述态 | `runtime/state/genregistry.go:71-77` `GenInstances.Restore` 仍只回滚；**五个 generation-scoped 消费者无一注册状态切片**（全库 `Register`/`RegisterStateSlice` 生产调用点见下行） | ✅ 收窄结论不变 |
| E12-7 | 切片注册早于插件挂载 | `assembly/run.go:593` 应用 `cfg.plugins`、`:600` 调 `mountBuiltinPlugins`；切片注册在 `:562`（循环三个）与 `:634`/`:637` | ✅ 不变（原引 `:576-580/:608-614/:615`，漂移 −14 行）；**`run-level` 具名拒绝仍绿**（`TestPluginDeclarationRefusesARunLevelPluginName`，位置已迁移） |
| E12-8 | tasks/mailbox 无会话面 | `assembly/task_checkpoint.go:37`、`runtime/state/mailbox_checkpoint.go:40` 只有 `SnapshotState`；`SnapshotSessionState` 实现者全库仅 projection（`checkpoint.go:1151`）/delivery（`host_delivery_checkpoint.go:132`）/inputs（`checkpoint_inputs.go:62`）/compress（`compress_checkpoint.go:81`）四个 | ✅ 不变 |

**§一 勘误复跑**：`StateRegistry.Register` 的生产调用点仍是 **5 处／7 切片**，且**全部丢弃 disposer**——`assembly/run.go:562`（循环 projection/inputs/compress 三个）、`:634` mailbox、`:637` tasks、`assembly/governance_checkpoint.go:53` governance seat、`assembly/host_delivery_checkpoint.go:152` delivery。故 J2 的「卸载态」今天仍是**潜在能力**，结论不变。

**【本轮新增勘误·E12-7 测试位置】**：§九/§十三 引的 `assembly/plugin_mount_test.go:150/:188/:200` 在现行树**已不存在**（该文件被其他域删除），三个声明面测试现居 `assembly/plugin_catalog_test.go:106`（`DisablesABuiltinPlugin`）、`:144`（`RefusesAnUnregisteredName`）、`:156`（`RefusesARunLevelPluginName`）。包级 `-run` 命令不变、验收面不变，仅定位勘误。

### 十七·5 四类未核项的本轮状态

| 原未核项 | 本轮状态 |
|---|---|
| ①注销时欠账是否真丢 | **已在主树实测：真丢**（S5 红，原文见十七·2） |
| ②3 代以上替换的累积态 | **仍未实测**；结构性排除（E12-6）不变 |
| ③外域清减后负锚是否仍红转绿 | **本轮仍不可测**（03/05/07 未实施） |
| ④`plugins: -compress-consumer` 的真实表现 | **实测具名拒绝**（`TestPluginDeclarationRefusesARunLevelPluginName` 绿，位置已迁移） |

### 十七·6 §十二 语义单元枚举的现行树复跑（**不按代码形态扫**）

三类单元的判据与真吞点**未变**，逐条复核结果：单元一 2 个真吞点（`wrapper.go:312-314`，及其前置 `:303-306`/`:307-311` 两处裸 `return`）；单元二 1 个真吞点（`facts.go:152-154`，与恢复期 `run.go` 上抛构成 G2-C6 不对称）；单元三 0 真吞点（`assembly/host_delivery.go` 现树 20 处 `assemblyWarn` ＋ 2 处 `assemblyError`，全部「具名＋义务保留」，行号整体漂移）。

**本轮对新增面的枚举**（工作树新增/大改的代码，按同一三类单元判据逐处过）：

| 新增面 | 单元 | 结论 |
|---|---|---|
| `assembly/settle_turn.go:522-598` `driveSeat`（settle 驱动的落链后动作） | 三·投递完成 | ✅ 「具名＋保留」：每处失败 `assemblyWarn` 后 `releaseSeat` 保义务（`:544-548`、`:550-552`、`:594-596`），不吞 |
| `runtime/agent/loop.go:373`（`Backflow` 送达事实）、`:405`（进度事实） | 三 | ⚠️ **具名但不停**（`consumerWarn` 后继续），与 §十二 原登记同因，行号未变 |
| `runtime/agent/consumer.go`（26 处裸 `return`） | 一/二 | ✅ 全为 nil 守卫与循环退出（`c == nil`、`ctx.Done()`、`flyingSegment() == nil`），**无一在提交成功之后** |
| `features/evolution/governance/denial.go`（新增 355 行，16 处裸 `return`） | 二 | ✅ 无吞错分支；仅 `:495`/`:529` 两处 `defer func(){ _ = recover() }()` 恢复守卫，与提交后动作无关 |
| `memory/ledger/journal.go`（重写 1763 行） | 存储提交 | ✅ 屏障失败一律 `fmt.Errorf("%w: %w", ErrJournalBarrier, err)` 上抛（`:1611/:1643/:1694/:1697/:2234`），无「写成功但没落盘」的静默 |
| `runtime/state/durable.go:777-787`（消费收据写＋Sync） | 二 | ✅ `durableError` + `return err`，`result=unconsumed` |

**枚举结论（现行树）**：三类单元仍是 **2 个真吞点 ＋ 1 个具名但不停**，工作树 3 万行增量**未新增**第四类真吞点。新增的 `settle_turn.go` 恰好复用了单元三模板——这与 §十二「12.R2 应沿用单元三模板、不新造机制」的裁定互相印证。

### 十七·7 12.R1 结账（本轮仍**不勾**）

锚已全部落在主树并红（验收面第一行成立），共同门 `go build ./...` 与 `go vet` 均 exit 0。**仍不勾的唯一条理由**：本叶新文件单独把 `package-layout` 与 `comment_policy` 两门打红，处置权（豁免 or 并入 `checkpoint_test.go`）不在本叶写面内。编排者裁定后，**无需补充证据**即可勾：命令、测试全名、红因与命中数均已在此节冻结。

## 十八、12.R2 J1 的 G83 审后修复与两条裁定（2026-11-18，主树实测）

> **本轮勘误**：以下原始证据逐字保留；“全部修复”“零额外成本”“零 findings”不再是当前结论。总提交序、已折叠旧输出重试与实际边界读取尚待验，S5 判据亦须纠正；以本文头部及 tasks.md 当前行动单为准，不据本节放行或归档。

G83（只读独立审）对 J1 提出三项 P1，本轮全部在主树复现红锚后修复：

### 十八·1 P1#1 重试旧轮输出回退呈现边界（已修）

- **症状**：两轮完成后重交第一轮 `agent_output`，`foldMapped` 无条件回写 `endedAt/rangeOpen`，`SessionPresented` 缩回第一轮。红锚 `TestRetryLeavesPresentedRangeAtTheLatestRound` 双子测（拒绝/不拒绝）先红后绿。
- **修法**：`replay.ReplayProjection.Append` 返回「本条是否新携带」（同键重复→false）；`foldIntoProjection` 透传；`foldMapped` 仅在新携带时移动边界。签名冻结断言 `skeleton_assert.go` 同步。
- **理由**：重复折叠同一身份恰为重试路径（`commitFactNoCadence` 已交分支重折 reducer），投影按身份去重，边界移动必须与去重同一判据。

### 十八·2 P1#3 拒绝的会话归因不成立（裁定：拒绝不按链划界）

- **症状**：旧实现把未应用事实按 `factSession`（Owner→Target→Source）过滤，session cut 只拒同链；而投递切片按 Target 会话读事实，两者对同一事实可归不同链 ⇒ 非同链的 cut 可跨过拒绝。
- **裁定**：**框架不猜链**。`unappliedCutRefusal` 去掉会话过滤——任一未应用事实拒绝一切 cut（全局与每会话），同身份再次完整应用后解除；`session` 字段、`factSession`、以及中途试加的 `holder` 字段/`RetryUnapplied` 全部删除（生产可拒绝的 reducer——delivery/projection——皆带会话面，按 holder 精确划界无第二消费者，属自证结构）。
- **替代方案被否**：①按拒绝 holder 是否带会话面过滤——首错短路使拒绝者之后的 holder 根本未被问过，判据不充分；补尾集计算则只在「无会话面 reducer 可拒绝」时与甲案可区分，生产无可达实例。②`holder` 字段＋断点续折——重试时新挂载且序在拒绝者之前的 holder 会被跳过，且 Reduce 从头幂等已足够。
- **测试**：`TestSessionCutRefusesOnlyAnUnappliedFactOfItsChain` 重写为 `TestSessionCutRefusesOnAnUnappliedFactWhateverChainItNames`；assembly 侧 `TestCutNeverCoversAnUnappliedFact` 同步改钉「别链 cut 也拒绝」。

### 十八·3 P1#2 锚跨界：无 checkpoint 担保的覆盖区间整段漏折（已修，含裁定）

- **症状**：`seedAnchoredReplay` 锚分支用**锚幸存者边界**给 seam 折叠划界，但锚不是 cut、无应用保证；被拒事实使 checkpoint 停在锚之前时，（checkpoint 界，锚界] 的非幸存事实（含被拒者）重建时整段不折——义务跨进程静默蒸发。红锚 `TestAnchorRebuildFoldsTheRangeNoCheckpointVouches` 双子测（applies：seam 必须折到；refuses：重建按名拒绝）先红后绿。
- **裁定（两界分管）**：**锚界只界投影，cut 界才界 seam**。锚分支新增 `seamBoundary`＝恢复的 checkpoint 幸存者最大 seq（无恢复种子则 0＝全链），`seedCoveredRange` 在幸存者水化**之前**按 seq 序补折 (seamBoundary, anchorBoundary]（排除幸存者与锚键，经 `seedFactWithoutProjection`＝原 `seedAccountFact` 更名：入账+入 seam、不入投影）；`replayChainPastCut` 的 behind 分类界改用 seam 界防双折。稳态（产物落链即请求会话 cut）该区间为空，零额外成本。
- **断言重锚**：`TestAnchoredRebuildEqualsFullWalk` 与 `TestAnchoredBootMatchesFullBoot` 的「锚解码数 < 全走数」旧断言恰以不安全跳过为前提，改为「无担保时解码数＝全走数（seam 不跳也不多读）」；严格省读由真实 boot 流程（Close 落 cut）的 `TestDefaultBootRebuildsFromTheAnchor`、`TestSymptomAnchorSessionBootCostFlatAcrossGenerations` 担保。
- **替代方案被否**：锚不可用时整体回退全走——丢失锚在投影面的合法收益；per-holder 污染面标记——同十八·2，无生产第二消费者。

### 十八·4 验证面

- 红锚三处先红后绿（P1#1 双子测、P1#2 双子测、P1#3 改钉面）。
- `go test ./runtime/state/ -race -count=10` 绿；`go test ./memory/replay/` 绿；全量 `go test ./assembly/` 仅剩 `TestUnmountKeepsTheOwedItemsOfItsSlice`（12.R3 已知红）。
- `comment_policy` 对本轮触碰包零 findings（门整体 exit 1 为外来未跟踪 wechat-bot 文件的既有 scan-set 配置红，非本轮引入）。
- `docs/api` 经 `gen_godoc.sh` 重生成（Append 签名与 state 文档）。
