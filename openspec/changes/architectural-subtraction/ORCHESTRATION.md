# architectural-subtraction 编排文档

> 基线 HEAD `8919acf`；启动门已满足（fidelity-and-recovery 归档 `3c2cf89`）。派发前重读本档二节；单完/波后回填四节台账。
> **派发模型（用户裁定 2026-10-27：充分并发）**：三实施轨并发 + 编排者集成验证轨；R21 缓解三约见 2.3。

## 一、结论（先行）

- **抵达 DoD 的唯一路径**：波1（A1: 1.1+1.3 根包链 ∥ B: 1.2 memory ∥ C1: 2.3 分层）→ 集成验证（host 级锚全量+2.1 核验）→ 波2（A2: 2.2 ∥ C2: 3.1）→ 集成验证 → 波3（A3: 3.2 → 3.3 四锚）→ F1–F4。
- **当前最大风险**：并发轨共享编译面（host 级测试编译根包+memory+api/plugin 全链）——窗口内互踩靠「包内锚先验＋异文件编译错轮询重跑＋集成面统一复跑」兜底；1.3（历史路径统一）仍是语义最大单，负锚先行。

## 二、编排四问

### 2.1 次序 —— 谁先谁后

就绪队列（派发原子=文件簇零相交的孙任务集；同文件必同轨串行）：

| 轨/单 | 孙任务 | 前置 | 状态 | 文件簇（独占） |
|---|---|---|---|---|
| **波1-A1** | 1.1→1.3（同簇串行） | 启动门✅ | **ready** | facts.go/tools.go/age_fold.go/run.go/request.go + tests/integration/host 新建两测 |
| **波1-B** | 1.2 | 启动门✅ | **ready** | memory/event_body.go + memory 新建测试 |
| **波1-C1** | 2.3 | 启动门✅（与批1无文件交） | **ready** | api/plugin/*、telemetry/*、settle_turn.go（仅增量标记）、api/plugin 新建测试 |
| 集成-1 | 编排者：host 级锚全量 + 2.1 核验 + 五查/勾选 | 波1 三轨落 | blocked | 只读 + 账目 |
| **波2-A2** | 2.2 | 集成-1 放行 | blocked | run.go/request.go/rl/*/host_face.go + rl 新建测试 |
| **波2-C2** | 3.1 | 集成-1 放行（C 轨串行） | blocked | api/plugin/present.go·phases.go·material.go、examples/contract/api_plugin/* |
| 集成-2 | 编排者：host 级锚 + 2.1 若未毕 | 波2 落 | blocked | 只读 + 账目 |
| **波3-A3** | 3.2（前置=2.2） | 集成-2 放行 | blocked | memory/replay/* + request.go + memory/replay 新建测试 |
| 3.3 | 编排者：D5 四锚全量+定点破坏 | 波3 落 | blocked | 只读 + evidence |
| F1–F4 | 编排者 | 3.3 | blocked | specs/golden/evidence/归档 |

- 并发准入=文件簇零相交（上表逐单核过）；**跨轨编译闭包相交为既知事实**（根包 import memory/api/plugin/telemetry），按 2.3 三约缓解，不作为禁并依据（用户裁定充分并发）。
- 关键路径：A1→集成1→A2→集成2→A3→3.3→F（C 轨傍行）。
- 本轮做不了：F 任务（三波全毕）；2.1 并行化改造（出案另行）。

### 2.2 授权 —— 谁准写什么

| 单 | 白名单（精确到文件） | 独占说明 |
|---|---|---|
| A1 | facts.go、tools.go、age_fold.go、run.go、request.go、tests/integration/host/*_test.go（新建 TestFoldCommitFailureSurfaces/TestSingleHistoryPath；既有锚测试文件禁改断言） | 根包核心五文件 |
| B | memory/event_body.go、memory/*_test.go（新建 TestDecodeEventBodyLegacyRefused） | memory 包 |
| C1 | api/plugin/*、telemetry/*、settle_turn.go（**仅增量**：标记/方法声明，禁改既有逻辑）、api/plugin/*_test.go（新建 TestObserverLayering） | settle_turn.go 增量-only |
| A2 | run.go、request.go、rl/*、host_face.go、rl/*_test.go（新建 TestRequestSnapshotsSingleSource） | 根包+rl |
| C2 | api/plugin/present.go·phases.go·material.go、examples/contract/api_plugin/* | api/plugin（C 轨内接 C1） |
| A3 | memory/replay/*、request.go、memory/replay/*_test.go（新建两负锚测试） | memory/replay+根包 |
| **编排者** | openspec/changes/architectural-subtraction/**（tasks/findings/ORCHESTRATION/账目勾选）、git 提交、specs delta、evidence | **账目单点归属：代理一律不写 tasks.md/findings.md/ORCHESTRATION.md，只报凭据** |

- 偏离分级：**预授权自决**——测试放置包位微调（负锚具名与 `-run` 正则逐字符一致并回报）、注释措辞、Go 风格化、白名单内机制设计；**必须上报**——验收面变动、白名单外文件、主规格条款失真（D3.7）、改语义断言（D3.4 批准权）、需删/改他轨文件（含编译错诱惑——**禁修异文件**，走轮询协议）。
- 概念预算门（lint S1–S4）：删=债减；新增导出名/概念须登记 BASELINE+canonical example（先例 da2c8d5）并回报——2.3 优先无exported机制。

### 2.3 凭据 —— 什么才算完成

- **分层验证约**：①**包内锚**（只编译本轨包及其上游者：`go test ./memory/...`、`./api/plugin`、`./telemetry`、`./rl`、`./runtime/agent`——注意 runtime/agent 为根包上游、不编译根包）窗口内由代理跑至绿；②**host 级锚**（tests/integration/host 全家：TestFactFidelity/RequestFidelity/TestHistoryRestoresWithoutBudget/TestSettleWakeSingleDrive/冷启动/新建两测——编译根包+memory+api/plugin+telemetry 全链）由**编排者集成时统一跑**，代理窗口内可试跑但不作完成依据；③**勾选**在集成验证全绿后由编排者执行（五查按单核）。
- **R21 轮询协议**：验证遇**白名单外文件**的编译错/测试红 → `sleep 45` 重跑，≤5 次；仍红则如实归因上报（「异轨半成品波及」），不修不绕不猜。
- 非空转证明：passed>0；退出码为唯一判定。
- 必亲验数字（编排者）：各锚 passed 数、grep 结构断言值（history[/snapshots/retained=0）、F2 逐门退出码、每波 diff --stat。
- 批收尾门禁（F2 全套逐批，用户裁定）：lint/selftests/check-openspec/三模块 short/short 十轮/long 六片（后台）/mutations --all/症状 job——长跑后台 shell（R32b）。

### 2.4 释放 —— 锁与等待表

| 资源 | 占用者 | 释放条件 | 等待表（释放后消费） |
|---|---|---|---|
| 根包核心文件 | A1→A2→A3 依次 | 各波集成放行 | 下一单派发 |
| memory 包 | B（波1）→A3（波3 memory/replay，子目录不相交但同包测试面——串行接 A3） | 集成-1/集成-2 | A3 前置检查 |
| api/plugin | C1→C2 | 集成-1 放行 | C2 |
| host 级验证面 | 编排者 | 各波三/两单齐落 | 集成锚全量+勾选 |
| git 提交权 | 编排者 | 波集成绿+验证记录 | 波提交（用户已授权） |

- 释放=触发：集成绿即当场派下一波（R31：连续推进至法定停点——DoD/提交归档明示/重大取向/blocked）。

## 三、明细层

- 派发矩阵：波1{A1(1.1,1.3) B(1.2) C1(2.3)} → 集成1{host 锚全量+2.1} → 波2{A2(2.2) C2(3.1)} → 集成2 → 波3{A3(3.2)}+3.3 → F。
- 2.1 于集成-1 执行（核验单：`go test ./tests/integration/host -run 'TestSettleWakeSingleDrive' -count=3` + `grep -cE 'retained|firing|rearm' settle_turn.go`=0 + findings 登记）——避免与 C1 的 settle_turn.go 增量并发互踩。
- 档位：无外部模型依赖；真实 tmux/real-smoke 不在面内。
- R10–R14 核对：无性能主张/无新档位/消费者锚已点名/冷启动真实路径入 1.2/1.3 面。
- 每波三问模板：①`git diff --stat <波前>..HEAD`②波报告（责任归谁）③锚复跑+定点破坏。

## 四、波次台账（边打边回填）

| 波次/单 | 五查结论 | 集成验证（命令+退出码） | 跨域裁决 | 账目勾选凭据 | 下轮情报 | 锁释放与队列消费 |
|---|---|---|---|---|---|---|
| 波1 派发（A1/B/C1 三轨并发，2026-10-27） | — | — | — | — | R21 三约随令下发 | — |
| 波1 集成-1（2026-10-27） | 五查过：勾选真实（锚复跑全 RC=0）/产物盘点（白名单内+编排者修复 5 处越轨引用）/数字溯源（38 包/settle×3 10.1s/双 grep=0）/遗漏检测（2.2 留波2）/消费者实测（冷启动+全 host） | build 0·锚电池 7 组全 0·settle×3 0·全 host 29.8s 0·全仓 short 38 包 0 | 裁定①-④+read.go 缺口修复+词法清零（findings 波1 节） | 1.1/1.2/1.3/2.1/2.3 勾选附凭据 | rl LeaseObserver 归属→A2；F2 门禁进行中 | 根包/memory 放行→波2 派发 |
