# oom-bounded-capture-and-storm-fuse 编排文档

## 一、结论

抵达 DoD 的唯一路径：波 1 四域并发（capture 设界 / 熔断执行面 / facts 清理 / fork 限长）→ 波 2 编排者单写 assembly 接线（三配置键→各域消费面 + pprof 挂载）→ 波 3 M7 注入对比锚与 M8 长跑回归验收。当前最大风险：熔断面与消费循环/直驱两路径的交点（dispatch 共用面）行为分歧。

## 二、编排四问

### 2.1 次序

| 单 | 所辖孙任务 | 触碰集 | 前置 | 状态 |
|---|---|---|---|---|
| 编排者 | M1 红锚（已红：5.4MB>1MB 实测）、配置契约冻结（AgentSpec 三字段+解码，编译绿） | features/rl/capture_bounded_test.go、api/config/{config,decode}.go | — | 完成 |
| A | M2 capture 设界（字节预算 LRU + MarshalSnapshot 落盘换出 + scoped/order 读回；M1 红锚转绿；导出消费者锚） | features/rl/capture.go、capture_wiring.go、capture_bounded_test.go（如需微调阈值不改语义） | M1 红锚 | 就绪 |
| B | M4 本仓熔断执行面（单响应调用上限截断+超限回执；长上下文防线 fold 判定+串行受理；两路径共用；TestResponseCallCapFuse/TestLongContextFuse 新锚 + K=0 兼容负锚） | runtime/agent/*.go、tests/integration/execution/ 新测试文件 | 配置契约（Loop 参数面 B 自定） | 就绪 |
| D | M5 facts 清理（fold 同步清 originals/seen/consumed；Run.facts 会话热窗口 2048+未闭合段全保；重启恢复负锚） | memory/replay/*.go、assembly/facts.go、memory/replay 新测试 | 无 | 就绪 |
| E | M4 fork 限长（sanitize 降级 payload 4KB 截断+标记；fork 单测） | trpc-agent-go internal/toolcall/sanitize.go + 其单测（**另一仓**） | 无 | 就绪 |
| F(波2) | 编排者：host.go 接线（三键→各域）+ C/M6 pprof 挂载 + go.mod 指向 fork 新版 | assembly/host.go、hostFace、go.mod | A/B/E 交付 | 排队 |
| G(波3) | M7 注入对比锚（双形态封死）+ M3 记录 + M8 长跑/文档/门 | tests/integration/execution、docs | F | 排队 |

并发准入：A(features/rl) × B(runtime/agent+execution) × D(memory+facts.go) × E(另一仓) 触碰集两两不相交。assembly/host.go 归编排者波 2 单写。

### 2.2 授权

- A 白名单：features/rl/{capture,capture_wiring,capture_bounded_test}.go（不得动 capture_test.go 既有断言）。
- B 白名单：runtime/agent/*.go、tests/integration/execution/{response_call_cap,long_context}_fuse_test.go（新文件）。禁动 assembly/、features/。
- D 白名单：memory/replay/*.go、assembly/facts.go、memory/replay 新测试文件。禁动 runtime/agent/。
- E 白名单：trpc-agent-go 仓 internal/toolcall/sanitize.go 及其测试。禁动本仓。
- 共享面（api/config 已冻结、assembly 接线、tasks.md、ORCHESTRATION.md）归编排者。各单禁 git commit。
- 预授权：域内内部结构（B 的参数面形态、A 的 LRU 实现、D 的淘汰数据结构）；偏离超授权上报。

### 2.3 凭据

- A 正锚：`go test ./features/rl -run '^TestTrajectoryCaptureStaysBounded$' -count=3` exit 0（红转绿）；负锚：预算 1MB 下 50×100KB 快照常驻 ≤1MB 且 SnapshotOf 全部读回逐字段等价；消费者锚：`go test ./tests/integration/learning -run TestEventIntegrityCapture -count=1` exit 0。
- B 正锚：两个新 Fuse 测试 -count=3 绿；负锚：K=0/阈值=0 时 `go test ./tests/integration/execution -count=1` exit 0（兼容）。
- D 正锚：`go test ./memory/replay -count=1` + fold 后 originals 无被折叠 key 新负锚；重启恢复：`go test ./tests/integration/history -count=1`。
- E 正锚：fork `go test ./internal/toolcall/ -count=1`；限长负锚：>4KB payload 截断+标记。
- 五查按派发单核；汇报附逐条输出摘要。

### 2.4 释放

A/B/D/E 全回 → 波 2 接线（host.go+pprof+go.mod fork bump）；波 2 完 → G。等待表：{F: 等 A/B/E}、{G: 等 F}。

## 三、明细

- 合单判据：本波四单两两触碰集不相交，各自成单；B 含两道熔断（同域同文件簇）合一单。
- 配置契约（已冻结，波 2 接线消费）：AgentSpec.{MaxCallsPerResponse,LongContextTokens,CaptureBudgetMB}；yaml 键 max_calls_per_response/long_context_tokens/capture_budget_mb；0 语义=关/缺省（capture 0=64MB）。
- 设计偏差记录：design.md D3/D5 原写 governance.* 全局键——实施修正为 per-agent 声明（与 max_tool_iterations 同模式）；设计文档随 G 波更新。

## 四、台账

| 波次 | 单 | 五查结论 | 集成验证 | 备注 |
|---|---|---|---|---|
| 编排者 | M1+契约 | 红锚红着（5,412,544 > 1,048,576 实测）；config 编译+api/config 测试绿 | go build ./... exit 0 | 症状锚第一天红 ✓ |
| 波1 | A/B/D/E | — | — | 派发中 |

### 波 1+2 台账（追加）

| 单 | 五查结论 | 备注 |
|---|---|---|
| A | M1 红锚转绿（811KB≤1MB，-count=10+race）；读回 50/50 字节等价；消费者锚绿 | seal 口径裁定：heap 释放+保留索引与盘上 swap |
| B→B' | 用户裁定改审批形态：B 截断交付后重做；B' 整批挂审批三态全绿（cap 7 子+long-context 5 子+消费者锚真 host）；无簿 fail-closed | B' 探明审批面五缺口（G1-G5）→ 裁定选项 A：架构边开通（golden 具名）+ seam 接线 |
| D | fold 四面统一清理+3 负锚红转绿；facts 热窗口被红线拦（位置型投递基线错位）→ 顺延波 3 方案 A（身份型基线先行） | originals 无被折叠回捞消费（实证）；幂等兜底三洞记录 |
| E | fork 4KB 限长红→绿两态、消费者锚 llmflow/graph、race/bench 不恶化 | 用户裁定扩面：result 降级同限长（编排者补，fork commit c10033a7f + tag v1.11.2-tagent.3 本地） |
| 编排者波2 | host 接线三件：capture 预算（entry capture_budget_mb→swap 目录）、统一审批簿（宿主>Default>boot-local）、熔断三键→Loop（零声明不覆盖测试注入）；AgentSpec ratchet 21→24 具名；ApprovalBook S4 范例+红线；gen_godoc 重生成；go.mod 本地 replace（fork tag 推送后切回） | 集成验证：go test ./... exit 0 |

### 波 3 + 收口台账（追加）

| 单 | 五查结论 | 备注 |
|---|---|---|
| G1 | pprof 三端点（同 face 凭据门）+ gctrace 边界实测（库内 Setenv 不生效→合并+提示）；TestDebugPprofServesHeap/TestGCTraceSwitchStatesItsBoundary -count=3 绿 | M3 采样：单快照瞬时峰值 ≈1.03MB（S4 长跑复验） |
| G2 | 身份型投递基线（三处闭环+负控红证）；facts 热窗口 2048+未闭合段全保（5 子测试，无 output 链整条保留的 spec 违例自查修复）；approval tmp 竞态（先写盘后发布+同锁，64 轮负控） | 副作用上报：restoreCallPairing 窗口外配对→编排者修（factOf→CommittedEvent tier 回退，已落） |
| G3 | TestStormPropagationFuse 三场景 -count=3 绿；旧基线 worktree 实测对照表（t.Logf 承载）；文档三面落盘 | doc budget 落锁 22162→22166（+3 G3、+1 OPERATION 观测面） |
| 编排者收口 | spec storm-fuse/gctrace 措辞对齐审批形态；config 注释漂移修正+gen_godoc；Baseline 死读清理（segment/SegmentAnswer/segmentResult 整链）；restoreCallPairing tier 回退；OPERATION 观测面段 | W-final：comment policy 35→0；两条裁决落地（29 条前序删除入索引、fact_window_test→facts_test 改名）→ lint --only policy ok |
| 终验 | `go build ./...` + `go test ./... -count=1` **EXIT=0** | 法定停点：提交/归档需明示 |

**剩余未勾项（非阻塞，属运行期/长跑面）**：M1 取样锚（victim attempts 分布，随长跑）；M6 后半（victim 实例 heap 采样）；M8 长跑回归与 event-short 十轮门（CI 面）。

### 终态验收（R35 环）与修复闭环（追加）

**独立只读审者结论**：无 Critical（死锁/假绿/数据错逐项排除——锁序环核查、awaitBatch 挂起影响面、三 Fuse 测试判别力抽查、mutation 证据溯源）；3 Major + 4 Minor。

| # | 级别 | 问题 | 处置 |
|---|---|---|---|
| 1 | Major | design.md D1「0=纯落盘」与实现相反（0=64MB、1MB 钳制封死纯落盘；表格又裁弃） | **修**：契约文本对齐「0=缺省 64MB；纯落盘已裁弃不可达」 |
| 2 | Major | swap 目录跨 boot 只写不删（磁盘单调增长） | **修**：openHostCapture boot 时 RemoveAll(swap)（换出/读回义务均在本 boot 生命周期内，注释写明依据） |
| 3 | Major | trimFactsLocked 每次 commit 全局锁内无条件 O(全 factOrder) 扫描 | **修**：sessionFactCount 守卫（O(1) 至链可达窗口；trim 后同步递减；trimSeededFacts 校准计数） |
| 4 | Minor | awaitBatchRequest 注释描述修复前竞态行为 | **修**：改为现行不变式 |
| 5 | Minor | ExampleApprovalBook nil loop 演示（照抄即 panic） | **修**：assembledLoop 替身（nil 不再可达） |
| 6 | Minor | Consumer.read 去重表只增不删（量级有限） | 备案不改（前序变更边角） |
| 7 | Minor | approval rebuild 短暂回退窗口（修复前已存在、方向 fail-closed） | 备案不改 |

**修复后终验**：`go build ./...` + `go vet ./examples/...` + `go test ./... -count=1` **EXIT=0（0 FAIL）**；`lint --only policy` ok；热窗口/投递基线锚 -count=3 绿。修复注释两处 free-standing 回归（Major 2/3 的行内说明上移为 doc）后 lint 复绿；六锚（热窗口/投递恢复/capture 有界/三 Fuse）-count=1 复跑全绿。**审查-修复闭环完成，可放行。**
