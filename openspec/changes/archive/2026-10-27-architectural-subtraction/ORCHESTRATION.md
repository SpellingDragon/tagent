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
| **波1-A1** | 1.1→1.3 | 启动门✅ | done | 根包五文件 + host 两新测 |
| **波1-B** | 1.2 | 启动门✅ | done | memory/event_body.go + 新测 |
| **波1-C1** | 2.3 | 启动门✅ | done | api/plugin、telemetry、settle_turn.go（增量）+新测 |
| 集成-1 | host 锚全量+2.1 核验+勾选 | 波1 落 | done | 只读+账目 |
| **波2-A2(A2b)** | 2.2 | 集成-1 ✅ | done | run.go/request.go/rl/host_face.go+新测 |
| **波2-C2** | 3.1 | 集成-1 ✅ | done | api/plugin 三文件、examples/contract/api_plugin |
| 集成-2 | host 锚+收尾 | 波2 落 | in-flight | 只读+账目 |
| **波3-A3** | 3.2（前置 2.2✅） | 集成-2 | ready | memory/replay + request.go + 新测 |
| 3.3 | D5 四锚+定点破坏 | 波3 落 | blocked | 只读+evidence |
| F1–F4 | 编排者 | 3.3 | blocked | specs/golden/evidence |

- 并发准入=文件簇零相交；跨轨闭包相交按 2.3 三约缓解（用户裁定充分并发）。关键路径 A1→集成1→A2→集成2→A3→3.3→F。本轮做不了：F（三波毕）；pump 并行化（另行）。

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

- **分层验证约**：①包内锚（只编译本轨包及上游：memory/api-plugin/telemetry/rl/runtime-agent）代理窗口内验；②host 级锚（编译全链）编排者集成统一验；③勾选在集成绿后由编排者执行（五查按单核）。
- **R21 轮询协议**：遇白名单外文件编译错/红 → `sleep 45` 重跑 ≤5 次，仍红归因上报（禁修异文件）。非空转：passed>0，退出码唯一判定。
- 必亲验数字：锚 passed 数、结构 grep 值、F2 逐门退出码、每波 diff --stat。批收尾门禁=F2 全套（用户裁定），长跑后台（R32b）。

### 2.4 释放 —— 锁与等待表

| 资源 | 占用者 | 释放条件 | 等待表（释放后消费） |
|---|---|---|---|
| 根包核心文件 | A1→A2→A3 依次 | 各波集成放行 | 下一单派发 |
| memory 包 | B（波1）→A3（波3 memory/replay，子目录不相交但同包测试面——串行接 A3） | 集成-1/集成-2 | A3 前置检查 |
| api/plugin | C1→C2 | 集成-1 放行 | C2 |
| host 级验证面 | 编排者 | 各波三/两单齐落 | 集成锚全量+勾选 |
| git 提交权 | 编排者 | 波集成绿+验证记录 | 波提交（用户已授权） |

- 释放=触发：集成绿即派下一波（R31 连续推进，法定停点=DoD/提交明示/取向/blocked）。

## 三、明细层

- 派发矩阵：波1{A1(1.1,1.3) B(1.2) C1(2.3)} → 集成1{host 锚全量+2.1} → 波2{A2(2.2) C2(3.1)} → 集成2 → 波3{A3(3.2)}+3.3 → F。
- 2.1 于集成-1 执行（与 C1 的 settle_turn.go 增量避开并发）。无外部模型依赖；R10–R14 核对无欠账。每波三问：diff --stat/责任归谁/锚复跑+破坏。

## 四、波次台账（边打边回填）

| 波次/单 | 五查结论 | 集成验证（命令+退出码） | 跨域裁决 | 账目勾选凭据 | 下轮情报 | 锁释放与队列消费 |
|---|---|---|---|---|---|---|
| 波1 派发（A1/B/C1 三轨并发，2026-10-27） | — | — | — | — | R21 三约随令下发 | — |
| 波1 集成-1（2026-10-27） | 五查过：勾选/产物/数字/遗漏/消费者实测 | build 0·锚电池 7 组 0·settle×3 0·全 host 0·short 38 包 0 | 裁定①-④+read.go 修复+词法清零 | 1.1/1.2/1.3/2.1/2.3 勾选 | LeaseObserver→A2 | 波2 派发 |
| 波2 集成-2（2026-10-27） | 五查过：A2b 补差（P1 夹具/P2 链/死面）纳入核验 | build 0·snapshots/Phase/死面 grep=0·formal 红→绿·short 39 包 0·全 host 0·变异 11/11·doc-budget ok | 裁定⑤⑥+C2 三处落地+变异伴锚迁移+doc-budget 重锁（findings 波2 节） | 2.2/3.1 勾选附凭据 | tmux 结构门=继承红登记 | 波3-A3 派发 |
| 波3 集成-3+3.3（2026-10-27） | 五查过：A3 负锚基线红证+导出面−10 | 四锚全量 0·四破坏红→绿（evidence/F3）·history/acceptance 迁移 ok·build 0 | 裁定⑦（五件跨白名单迁移+delta+词表）；D2 锚选窄归因留痕 | 3.2/3.3/F1/F3 勾选 | — | F2 收尾门禁→F4 归档 |
