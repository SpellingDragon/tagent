# compression-and-budget-as-events 编排文档

## 一、结论

抵达 DoD 的唯一路径：先让消费循环路径（W1）与直驱路径（已恢复）在 execution 包全绿，再并行完成 D6/D7 事件化接线（W3/W4），最后 D5 收敛 gate/Occupancy 并过 F 门归档。当前最大风险：Host.Invoke 与 fireNoticeTurn 双入口的投递/settle 账目语义在消费循环下尚未闭合（notice 驱动段的回复无人投递、settle 账目 fire-wake 分支未结账）。

## 二、编排四问

### 2.1 次序（就绪孙任务表）

| 单 | 所辖孙任务 | 触碰集 | 前置 | 状态 |
|---|---|---|---|---|
| 编排者 | D1 骨架（consumer.go/loop.go/host.go/facts.go/request.go）、D3 dispatch、D2 Invoke/fireNoticeTurn wake 分支、投影推进规则（recordMapped→rangeOpen） | runtime/agent/{consumer,loop}.go、assembly/{host,facts,request}.go | — | 已落盘，双红锚绿，execution 16红→6红 |
| W1 | execution 剩余 6 红收敛：intake 系 4（unified_intake_test 断言语义迁移：收据→段闭合）+ event-integrity 系 2（notice 投递接线 + settle 账目 + 段 range 命名形态） | tests/integration/execution/{unified_intake_test.go,event_integrity_test.go}、assembly/host.go、runtime/agent/consumer.go | 编排者产物已就绪 | 就绪 |
| W3 | D6 压缩三段式：AgeFoldPlugin 只提交 compress.request；新增 CompressionConsumer；seedReplay 读 product 重建、删 restoreFoldForm 重折 | assembly/age_fold.go、assembly/run.go、memory/replay/、新文件 assembly/compress_consumer.go | 无（L1+L2a 已入 main） | 就绪 |
| W4 | D7 预算事件化：FinalRoundPlanner（PostCommit）提交 final_round.request | runtime/agent/final_round.go、assembly/run.go（挂载点） | W3 释放 run.go | 排队 |
| W5 | D7b 消费循环读 final_round.request（收尾轮）+ D5 gate/Occupancy 收敛 + 冥想让路改读消费状态 | runtime/agent/consumer.go、assembly/host.go | W1 释放 | 排队 |
| F | D8 迁移验收 + F1-F4 | 全仓 | 全部域清零 | 排队 |

并发准入：W1×W3 触碰集不相交（host.go/consumer.go/测试 vs age_fold.go/run.go/memory）。W4 与 W3 交（run.go 挂载点）→ 串行。W5 与 W1 交（consumer.go/host.go）→ 串行。

### 2.2 授权

- W1 白名单：上述 4 文件；禁止动 runtime/agent/loop.go 与 memory/；禁 git commit。
- W3 白名单：上述 4 路径；禁止动 runtime/agent/、assembly/host.go、tests/integration/execution/；禁 git commit。
- 共享面（loop.go、tasks.md、ORCHESTRATION.md）归编排者单写。
- 预授权：测试断言按 spec 场景语义改写（验收面以 spec.md 为准，测试初稿的旧语义断言可改，须在汇报中列明逐条改写理由）；偏离超出预授权必须上报。

### 2.3 凭据

- W1 正锚：`go test ./tests/integration/execution -count=1 -timeout 600s` exit 0（全包绿，非空转：用例数>0）；红锚 #1/#2 必须保持绿：`go test ./tests/integration/execution -run '^TestSlowToolDoesNotBlockNewInput$|^TestCrossSourceBatching$' -count=1`。
- W1 负锚：TestEventIntegrityNoticeIdentity 的 conflicting_body_same_identity 与 multiple_progress_ordered_by_seq（具名错误路径，必须从红转绿）。
- W3 正锚：`go test ./memory/... ./tests/integration/... -count=1`（新 CompressionConsumer 单测 + 现有 fold 测试不回归）；负锚：`restoreFoldForm` 重折路径删除后 `grep -rn restoreFoldForm --include='*.go'` 零残留（exit 1 即空，用 `! grep` 断言）。
- 消费者锚：W3 的 seedReplay 重建路径必须经真实投影入口（AssembleBudgetPrefix 读到合成条目），组件绿不算。

### 2.4 释放

- W3 完成 → run.go 挂载点空出 → 当场派 W4。
- W1 完成 → consumer.go/host.go 空出 → 当场派 W5。
- 等待表：{W4: 等 W3}、{W5: 等 W1}。

## 三、明细

- 派发单-孙任务矩阵见 2.1；合单判据：W1 六测试同根因（consumer/host 接线行为）触碰同文件簇 → 合一单。
- 执行期已修（编排者）：投影 ended 边界按 rangeInvocation 推进 → 在飞段材料泄漏进请求前缀（重复 second 实证）；改为 rangeOpen（agent_output 关段才推进）；Drive 直驱恢复 awaitSettled（dispatch await 下沉）；receipts-only 不驱动下轮（running 空才 dirty）；cursor 恢复位置（lastOutputOf）。
- 计划回写（§五）：design.md 的「Drive 保留结构但行为变更」与实际实现（Drive=直驱旧语义、Consumer=段语义双轨）有偏差——双轨是迁移期形态，D5 时统一；已记入 design 待办。

## 四、台账

| 波次 | 单 | 五查结论 | 集成验证 | 备注 |
|---|---|---|---|---|
| 编排者直改 | D1/D3/D2 部分 | 双红锚绿（2026-10-30 实测）；execution 16红→6红 | go build ./... exit 0 | 红锚#2 断言从 len==1 改为「同请求含两输入」——对齐 spec「允许进入同一次模型请求」措辞，非缩水 |
| 波1 | W1 | 全包绿连续 4 次（36 顶层/91 子 PASS）；红锚/直驱保持绿；负锚（conflicting_body/multiple_progress）红转绿；变异实测（删投递分支→红）；断言改写 4 项均有 spec 依据并逐条申报 | 待跨包复跑（host/history/learning） | 投递方案=消费端单一投递点（closeSegment 回调）；settle 账目=fireNoticeTurn 消费分支等段+记账 |
| 波1 | W3 | 8+3 条 fold/compress 锚全绿；restoreFoldForm 零残留（grep 硬门）；消费者锚=重启恢复经公共装配入口实测；幂等三重；回滚对照实验隔离归因 | memory 全绿；integration 全量 17 红经基线对照归因为 W1/编排者面（host 12/history 1/learning 1/execution flake 3） | product 五字段走 event.Meta（先例 model_replacement）；compress.request 不持久化（重启账目归 0，spec 未要求） |

### 待裁项（W1/W3 上报，编排者待裁）
1. 受理收据快路径：spec 允许第二 Invoke 返回收据，现一律等段闭合——建议保持现状（等待优于收据），spec 措辞已允许（「或返回受理收据」为许可非要求）。
2. bare-session product 不落 tier（SessionID-only 事实无 Agent 归属）——保守取舍已声明，待上层裁定。
3. product 走 Meta 私有键 vs 结构化字段——契约变更需另开单。
4. CompressionConsumer 外部宿主导出面——S4 范例门约束，待需时再开。
5. D3 补缺：快结果先到先消费红锚未写（tasks.md 已标【补缺】）——纳入 W5。

### 释放与补派
- 波 2：W4（D7 planner，全绿+变异实测）、W5a（13/14 收敛，R1/R2 待裁上报）已回；W5b（D7b+D5+快结果红锚，11 包两连绿）已回。
- 编排者直修：R1 投影边界（agent_output 含语义推进 + driving/material 分界，TaskContinuation 25/25 绿）+ R2 export 链序配对（learning 绿）+ the_same_text 断言语义修正（历史呈现不计重复读）。
- 波 3：W6（两 flake harness，50-60 次零失败）+ W7（D8 文档三处，doc budget 具名落锁 22032→22061）+ W8（comment policy 110→0）。
- 编排者收口：doc_budget 落锁、spec InjectEvent 措辞澄清、native_conversation_roundtrip 双路范围说明、race_check.sh 包清单修正（过期路径 ./evolution ./rl . 对齐 features/ 结构）。
- 门禁状态：根全量 ✅（./... exit 0）、lint comment_policy ✅（0 finding，仅余 tracked-hygiene 23 条=前序删除未入索引，待 git 收口）、race ✅、doc budget ✅、event-short 十轮终验执行中（W9 后 3/3 已 match）。
- 波 4：W9（wechat-bot smoke 域段语义迁移，10 项归因处置、红锚 0 真回归，-count=3 全绿，event-short 3/3 verdict=match）。HEAD 对照证明冥想检查红为既有夹具缺陷（旧语义下同样死法），非本变更回归。
- W9 待裁已裁：FoldVolume 排除口径照准（测量折叠产物而非其后到达的反思材料）；另 5 处只拉 interval 的钉死夹具不扩（未抖不动，记遗留）；LongRun 的 -race 守卫在 event-long 门自带 race 模式下自然满足。
- 法定停点：工作树收口（git add/commit）+ 归档需用户明示；event-long 六分片（约 2h 超长跑）待用户裁定是否等待或后台跑。

## 归档后追加（2026-10-31）

- 归档完成：delta 三份（consumer-loop-convergence/compression-pipeline/watchdog-final-round）并入 openspec/specs/** 新建 capability；doc budget 具名落锁 22061→22162（+101，口径边界迁移）；README 归档重写（脱节点等行替换，净 +3）。
- event-long 遗留（归档后发现，属 CI 面，用户裁定后续处理）：smokeLongWant 已迁移（delegate 形状只等 settle-ack，同 W9 形态）；但单分片（seed=1/window=256，-race）1500s 超时未完成——迁移前 82s 即失败、迁移后推进但比基线预算（1200s 内）慢，疑有慢等待或代次堆积，待排查。首次排查时注意清理残留 wechat-bot.test 进程（pkill 杀 runner 不杀 go test 子进程，实测互踩）。
