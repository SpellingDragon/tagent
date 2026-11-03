# 消息语义技术决策

## D1 回执分层

send ok=目标输入事实已提交；call_subagent ok=任务已受理。目标之后的 failed/cancelled 不 retroactively 改回执。被否：ok=目标回答完成，因为它把异步消息重新耦合为同步调用并引入回调死锁。

## D2 通知唯一结清语义

Accept/Claim 仅表示接纳；目标地址持久记录消费结果和连续位置后才 Consume。failed/cancelled 若已形成持久消费结果，也属于已处理，不默认无限重放；无结果的进程停止/写失败才保持 owed。显式拒收落 feedback 后结清。外部发送改为独立派生义务。

## D3 已提交重入不可反向否认

只有输入提交前才可回滚为未发生的尝试。提交后任务/委派/事实链必须沿同一 Round 记录真实 completed/failed/cancelled/uncertain 并恢复；同一 send identity 重放不得另开 Round 或复制正文。Broken/denied 不得成为事后第二答案。

## D4 恢复不依赖进程内表

Continuation 的 served/running/owed 只能由任务、地址、Round 和输入事实推导，不能持有唯一正文或合法性权威。**准入判定只用 task.Status，不用 LogicalStatus**：`stable`/`suspect`/`alive` 是观测词而非状态（终态任务的 Logical 恒为 Active，见 `runtime/task/logical.go:154-158`），不得作为重入分支。
- 同进程、continuation entry 存在、task=StatusSettled：Claim→Serve（现路径）。
- 重启后、task=StatusSettled、delegation entry 与 declaration 可由持久账重建：Reactivate 开下一 round→Claim→Serve。
- failed/cancelled/running/accepted/unknown（含逻辑 suspect 的非终态任务）：具名拒绝，零状态变更、零 round 消费。

boot 只重建 metadata（assembly/site/faces），**不 Ensure/mount Consumer、不动 Loop.current**；`addr` 由 `{AgentID: assembly.Name, SessionID: Spec.Source.SessionID+"#sub:"+CallID}` 推得；重建 entry 的 `running=false/owed=nil/pin=nil`（旧 generation pin 已死）；`entry.round` 取持久 high-water。

## D4a 声明持久化与重建时机（待 lead 裁定 D-W5-A/D-W5-B）

重启后下一轮要 `builder.Bind(assembly)`，而 resolved Assembly（Name/Model/Instruction/Skills/Tools/MCP/sampling）当前无持久源：Spec/TaskRecord/delegated input meta 都没有，tool-call 参数只经 requestSnapshots（capture 面）读取，不在任务/事件恢复账内。候选：(A) delegated input fact meta 增 `delegation`（1 键，无新公共类型；需 api/event 词汇+04 元数据 spec delta）；(B) 任务 admission 侧按 taskID 的 durable declaration 内部记录。否决：给 `task.Spec`/`agent.Delegation` 加导出字段（各 9 字段，S2 棘轮 10>9，scripts/gates/check_concept_budget.py:704）；boot 预挂 Consumer。重建时机：(E) boot 后 metadata seed；(L) 首次 re-entry 懒重建（推荐，boot 零动作，天然满足无任务不建槽/tombstone 相容）。裁定前 2.5c 不派发。

## D5 事实链是唯一投递账

send 与 settle 在原事实提交时写同构元数据。旧事实 fallback 只是迁移读法；hostMeditationPort.receipts 等缓存不得改变同身份重放的答案。

## 风险与回退

- failed 通知由重试变已处理是明确行为变化 → 主 spec 与故障点阵共同验收；未形成消费结果的失败仍恢复。
- uncertain 提交不能猜测 → 按稳定身份重读，禁止重写事实；具体恢复协议复用现有 durable commit。
- 改动横跨 Consumer/Host/Task → 与 01/03 串行，不增加跨域补偿面。

## D6 委派输入：岗位说明/材料/任务三事实（W1；待 lead 裁 L-W1-*）

**术语**
- **委派输入批次**：一次委派 round 在子站链上提交的事实组——岗位说明事实（system）、材料事实（每个已解析源一条）、任务事实（user，最后提交）。它不是被 r69/r103 删除的“输入批次”护栏：没有 barrier/guard/私有窗口，只是可重读的事实组；“批”是本设计对事实组的称呼，不是新机制。
- **材料事实 / 源事件**：材料事实是子链上承载材料正文的事实；源事件是父/调用方链上被引用的原事实。两者是否同一身份（同键）是 L-W1-1，不得混称。
- **岗位说明事实**：Assembly.Instruction 的 system 消息事实；不是冻结 generation 的 prompt，也不是任务正文。
- **命名驱动**：DelegatedInput 提交完成后，以 Consumer.Drive(instructionKey, materialKeys..., taskKey) 命名并由 Wake 触发一次拉取；Drive 会丢弃已 taken 的键，重入轮的旧 instruction/material 键不应再驱动第二段。

**不变量（无论 L-W1-* 怎么裁都必须成立）**
1. **子链可见且重启可读**：子请求需要的岗位说明与材料，必须能在重启后仅凭子链事实（或子链可推导的键）重读；命名 wake 的键只在进程内，不能是唯一来源。
2. **批不可分**：一个 segment 只在整批（instruction/material/task）可见后开；只看到 instruction/material 时不得开段；task 提交失败时零段，已落的前置事实保持惰性（重启也不得单独驱动它们）。
3. **键稳定**：岗位说明键按执行站点稳定，不带 CallID/DecisionID/AttemptID/EpisodeID 等每轮变化字段（SameFact 比较这些字段，memory/event_body.go:233-313）；任务键区分“同一轮重放”与“下一轮”（L-W1-3）。
4. **谱系一致**：三事实同带 LineageDelegation（自管谱系），不得成为 meditation 的 host-facing 交付/新鲜度材料；否则 system 岗位说明会被当作外部输入被观察到。
5. **身份唯一**：材料事实与源键的关系只能有一句真话（L-W1-1）；“材料键=源键”与“子链独立/重启可读”不能同时声称而不说明拷贝还是投影。
6. **顺序固定**：system 岗位说明 → 材料 → task user 最后；任务事实是命名驱动的主要键，不是三事实中唯一的 range starter（当前 startsRange 对三者皆真，runtime/agent/loop.go:476-481）。

**L-W1-1 材料身份（真分叉，推荐 A）**
- A 子域投影键：子链提交 delegated-<site>-material-<源键>（源键入 meta），材料事实子链可见、重启可读、同库不撞源键；代价=改 r103“材料键=源键”的字面，材料事实与源事件不同身份。
- B 源键+按键读：同库不落子链事实，命名拉取经 SetFactReader 按键读全局；重启需引用清单（r103 已删 input_refs）或子站可推导的键，否则材料丢失。
- C 源键+子链视图扩展：给子站链视图加按键投影/索引；新投影面，概念门风险最高。
- 证据：assembly/facts.go:69-85,230-236,309-318（全键判重/冲突）、assembly/run.go:1283-1305 + assembly/facts.go:377-384,571-578（子链按 Owner/Session 过滤）、runtime/agent/consumer.go:607-655（命名拉取只读 sessionFacts）、runtime/agent/loop.go:770-846（reader 唯一消费者）。
- 后果：选 B 而不同时恢复引用清单 = 材料重启丢失；选 C = 新投影机制（红线/概念门）。

**L-W1-2 批次可见性（真分叉，推荐 A）**
- 现状：submitDelegated 先 mount（loop.go:1310-1321），mount 立即跑首轮 consume（consumer.go:1627,264-284）；三事实都是 range starter，首读落在两次提交之间即半批开段。
- A 空链 mount 跳过首轮 consume，首个显式 wake 才拉（私有条件，无新公共面；生产路径都是 commit 后 Wake：assembly/host.go:847-851、host_tools.go:456-461、invoke.go:72-78、consumer.go:355-357）。推荐。
- B 私有 hold-first-pull 门（新私有机制，按红线报用户）。
- C 接受半批（S11 child_agent_model_requests 与 Unsettled/重启语义须改；不推荐）。
- **L-W1-2b 失败/重启面（与 L-W1-2 同裁）**：A 只解 fresh mount 首读；task 提交失败留下的 instruction/material 事实在重启时因链非空、首读照跑，仍会被 PendingInputs 单独驱动。要满足不变量 2，还需 (i) 三事实带 delegated_batch 标记且非 task 事实不 startsRange（重启只由 task 驱动、同批事实随拉；须同改 runtime/agent/loop.go:476-481 与 assembly/facts.go:367-372 两处同判，assembly/facts.go 进 W1-01/02 写集）｜(ii) 接受孤儿事实可被驱动/或另设清理（不推荐）。未裁前 TestDelegatedTaskCommitFailureLeavesNoSegment 只跑“失败后立即”半边，重启半边挂 L-W1-2b。

**L-W1-3 轮次身份（真分叉，推荐“同体重入=重放”）**
- 现状 task 键=delegated-<site.SessionID>-<sha256(Request)[:8]>（loop.go:1367-1375）；continuation 重入复用同一 site（continuation.go:254）且当前传递 refs=nil。同 body 重入→键命中→返回上一段答案，不开新 round。
- 推荐：先承认“同体重入=重放”并加锚；若要“新 round”，须给 continuation 加 round 号并入 task 键，写集扩 continuation.go。
- 岗位说明键不能跟着 Request 走（否则每轮重复 system 消息）；site-only 键的代价是“同 site 不同 instruction”冲突，须由“一个 site 的 assembly 固定”保证（continuation 已固定 assembly）。

**L-W1-4 净删门**：现“净删为正”不可判定且估算不成立（删 ~83 行/增 ~90 行）；建议改为“三旧符号 grep=0+无新增公共类型+逐文件 git diff --numstat 留证”。概念门现树 new=1 来自 W2 在途 ConfigChange，不能作 W1 叶门。

**L-W1-5 reader 去留**：随 L-W1-1。选 A 则 SetFactReader/factReader 与展开器同删（r104/r105“保留仅喂命名拉取按键读取”前提失效，需同裁）；选 B 则把 reader 接进命名拉取并保留。

**测试保真**：tests/integration/execution/delegation_pipeline_test.go 的 delegatedFactLedger.commit 不去重、不判冲突，不能证明同库“源键幂等命中/冲突”；同库材料锚必须走真 assembly.Assemble（tests/integration/capabilities/recall_delegation_test.go 的真实 run.Invoke 面）。
