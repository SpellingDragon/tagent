# 任务

## M1 症状锚（红先行）

- [x] 红锚：跑 M 个 decision 后 TrajectoryCapture 常驻快照集合有界（字节 ≤ 预算）——TestTrajectoryCaptureStaysBounded 红着立案（5,412,544 > 1,048,576 实测）→ A 单转绿（811,360 ≤ 预算，-count=10 + race 稳）
- [ ] 取样锚：attempts.jsonl（victim 3,779 条）单响应调用量分布取证，校准 K 值——随 G 波长跑回归一并取样

## M2 capture 设界（D1）

- [x] 实现：`fixed`/`snapshots` 字节预算 LRU + MarshalSnapshot 落盘换出（SwapDir 文件）+ scoped/order 读回；`NewTrajectoryCaptureWithBudget` + `CaptureBudget{Bytes,SwapDir}`（旧构造不破坏，零 SwapDir 不淘汰）；低水位 budget/2——A 单
- [x] 负锚：预算 1MB + 50×100KB 快照常驻 ≤1MB 且换出件在盘、读回逐字段等价（digest 复核 + 50/50 字节相等）——M1 红锚转绿
- [x] 消费者锚：learning Capture 系 + assembly/subagent 的 seal 后读面全绿
- [x] 生产接线：host_capture.go 接 entry `capture_budget_mb`（0=缺省 64MB）、swap 落 `<root>/trajectories/swap`——编排者波 2
- [x] seal 口径裁定：释放 heap 常驻、保留 fixed 索引与盘上 swap（spec 只要求可读回+heap 有界——接受）

## M3 深拷贝收窄确认（D2）

- [x] 记录性任务：D1 换出后单快照常驻峰值确认——G1 采样：8×500KB 快照在 1MB 预算下单快照边际瞬时峰值 ≈1.03MB（含编码瞬时），稳态按记账 ≤1MB；真实 heap 略高已申报（S4 长跑复验）

## M4 风暴熔断（D3/D4/D5——用户裁定改审批挂起形态）

- [x] 本仓：单响应调用数上限（`max_calls_per_response`，0=关）——超限时**整批挂审批**（恰 1 项批次请求含 N 调用清单；call ID 不进 digest）；同步有界等待（ExpiresMs 界+250ms 轮询+ctx 取消）：批准=整批受理执行（N 回执+1 批次通知）、拒绝=N 条拒绝回执零执行、过期=N 条未执行回执；无簿=fail-closed 全拒（不回退截断）——B' 单（TestResponseCallCapFuse 7 子测试）
- [x] fork：sanitize 降级消息 payload 限长 4KB（截断+标记、rune 边界、代码块闭合）——E 单（红→绿两态、消费者锚 llmflow/graph、race/bench）；**result 降级路径同限长**（用户裁定扩面）——编排者波 2 补
- [x] 长上下文防线：`long_context_tokens`（0=关）超阈值整批挂审批（同形态同代码点）——B' 单（TestLongContextFuse 5 子测试）；fold 触发半边经查执行面无干净入口（ReplayProjection 归 PostCommit 域），不硬接
- [x] 负锚：K=0/阈值=0 全量兼容（execution 包 -count=1 绿）；架构边 runtime/agent→runtime/task/approval 具名开通（golden 政策表）
- [x] 生产接线：host Boot 统一审批簿（宿主挂的 > 进程 Default > boot-local `<root>/approvals` 兜底）+ entry 三键→Loop（零声明不覆盖已注入限值）——编排者波 2

## M5 facts 清理（D6）

- [x] fold 同步清理 `originals/seen/consumed` 被折叠 key（四个重写面统一 forgetDropped；「表=entries」不变式；3 负锚红转绿）——D 单
- [x] `Run.facts` 每会话热窗口 2,048 + 未闭合段全保（含无 agent_output 的整条在飞段）+ factOrder 同步裁 + seedReplay 末一次套窗——G2 单；身份型投递基线（deliverFresh/generatedBodiesOf 按 turn.InvocationID 取材、位置型 baseline 参数删除）先行落地；负锚 TestFactsStayInTheirSessionWindow 5 子测试 + TestDeliveryAfterRestartKeepsTheTurnAnswer，各 -count=3 绿
- [x] 幂等兜底链实证：tier 命中→SameFact/ErrFactKeyConflict 兜底成立；三洞记录（无 tier 无去重/factOrder 需同步裁/settle 二次观察）——台账

## M6 pprof 观测面（D7）

- [x] host face 三端点（同 face 凭据门、仅回环）+ `TAGENT_GCTRACE` 合并 GODEBUG（库内 Setenv 不生效已实测并打边界提示）——G1：TestDebugPprofServesHeap（含凭据负锚）+ TestGCTraceSwitchStatesItsBoundary -count=3 绿
- [ ] S4 钉死：victim 复现形态 heap 采样证据入 run notes——波 3

## M7 风暴传播对比锚（双形态封死）

- [x] 注入锚（**新语义**）：TestStormPropagationFuse 三场景（单响应 500 调用挂审/20 输入风暴 2 请求/链增量=受理数非 2N）——G3：-count=3 绿；消费者锚走真 Boot
- [x] 对比记录：旧基线 c111d7a2 worktree 实测对照表（无闸 500 全派发/新基线 0 执行挂审；victim 形态旧基线 500 条降级回链/新基线不可触达）——G3 以 t.Logf 承载（-v 可复跑，复现配方在测试注释）

## M8 验收与文档

- [ ] 长跑回归：-race 下模拟会话（含风暴注入段），RSS 稳态有界——波 3
- [x] 文档：README 能力矩阵（捕获预算/审批熔断两行）、OPERATION 观测面、GLOSSARY；design.md D3/D5 措辞对齐审批形态（截断→审批挂起）——波 3
- [ ] 门：根全量 + lint + race + event-short 十轮——波 3
