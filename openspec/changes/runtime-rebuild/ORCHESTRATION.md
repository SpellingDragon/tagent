# ORCHESTRATION.md — runtime-rebuild 编排文档（计划完成的强制产物，派发唯一依据）

> 三者边界（各自职责，不互抄）：`specs/orchestration/spec.md` = 可验收的**编排契约**（条款/场景）；`design.md` = **静态决策**（D1 责任、D2 子域表、D4 复用、D7 并发写域、D8 三级验收）；本文件 = **怎么驱动 + 当场账**。
> 本文件由集成者单写；每次派发前重读对齐，单完/波后回填台账。无凭据摘要不得在台账写「完成」。
> 本文件**不是** openspec artifact，openspec CLI 不跟踪它；它的存在不改变 CLI 的 4/4 判定。

## 授权边界（本轮，先于一切推断）

- 本轮用户授权为 **B**：只写 OpenSpec 多级规划产物 + CI/lint/golden 检查器资产；**绝非产品 apply**。
- 允许写：`openspec/changes/runtime-rebuild/**`（现有 md/json 及缺失的 `ORCHESTRATION.md`）；`golden/runtime/{architecture.json,scenarios.json}`；`scripts/{runtime_checklib.py,check_runtime_plan.py,check_runtime_architecture.py,check_runtime_structure.py,check_runtime_evidence.py,test_check_runtime_*.py}`；`.github/workflows/runtime-contracts.yml`。除此不得写。
- 禁止：产品源码/产品测试/残件清理/主 `openspec/specs/`/历史 wiki/prototype/用户文件/依赖改动；不跑产品 Go build/test/vet 或实际 `go list`；不安装依赖、不提交/推送/归档、不付费/微信调用。
- 允许运行：检查器自身自测、契约模式（`--check-contract`/`--check-policy`）、OpenSpec 校验（`validate`）。**允许预制检查工装，但不因此越过 00.3 前置**。
- 外部批准计划 `/Users/pengweiye/Library/Application Support/Qoder/SharedClientCache/cache/plans/运行时_重设_实施计划_304e54c3.md` **只读**；外部计划原编号/行为/范围不得改。

## 0、结论（先行）

- **抵达 DoD 的唯一路径**：G0 现场/资源盘点 → G1 共享签名与原生接口探针 → G2 正常/失败/并发/重启纵向推演 → G3 逐单精确文件授权 → **阶段一 P0 骨架波（24 骨架，零逻辑）→ 00.P0.3 骨架评审门（认真评审＋用户确认）→ 阶段二 65 项 `NN.M` 实施** → 按**就绪叶任务集合**派发产品实现（W 两两不相交且 W 交对方 R 为空）→ 每个域全场景 + 跨域接线 → 09.1 正式入口真实观测对 golden → 09.3 两 module 全量门禁 → 09.5 真实资源逐项 → 09.6 闭合。任一环缺凭据即不勾选。
- **本轮最大风险/瓶颈**：G1 尚未有最小编译探针（无产品实施授权），G2 **设计推演尚未落书面结论**（G2 = 实施前正常/失败/并发/重启桌面推演，**非**产品观测；产品观测属 09，另行说明）；因此**产品完成度=0**：阶段一 24 个 P0 骨架中仅 00.P0.1/00.P0.2（规划/工装，非产品）随本轮完成（凭据 `rev2phase-20261013`），其余骨架与阶段二 65 项 `NN.M` 均 planned。第二大风险是「规划格式全绿」被误读为「产品通过」——本文件的每处完成声明都必须能落到**可复跑命令 + 原始退出码**。
- **本轮实际完成的只是规划与检查工装**，且**规划/检查工装完成记录与产品完成记录严格分开**：检查器自测通过只证明检查器，不证明产品、不勾选任何叶任务。
- **对照索引**：目标—KR 见 [expectations.md](expectations.md)；锁定框架原生复用依据见 [trpc-reuse.md](trpc-reuse.md)（单点，不互抄）；二者随 00.3 审定接入 G1。

## 1、编排四问

### 次序（就绪队列到孙任务粒度）

- **派发原子 = 就绪叶任务集合**（§design D7 / orchestration spec「隔离并发与新语义优先」）；域只是**边界与账目单位**，不是调度单位，不整域派发。
- **就绪判据**：该叶任务全部 `depends_on` 已在集成树落地（非「同波」）；设计闸 G1/G2/G3 覆盖该项；本单资源锁未被占用。
- **本轮（规划波）就绪面**：**规划/检查工装单**（批次 1–7）已完成；**阶段一 P0 骨架波与阶段二产品单均未派发（当前产品单 = 0）**。
- **阶段一 P0 骨架波就绪队列**：`00.1→00.2→00.3` 设计闸就绪后，**层 A 可并行**：`01.P0.1`、`02.P0.1`、`02.P0.3`、`04.P0.2`、`09.P0.1`（仅依赖 `00.3`/`00.P0.1`）；随后按 **import 方向分层推进**（如 `02.P0.2`/`03.P0.1`/`04.P0.3`/`05.P0.2` → `01.P0.2`/`03.P0.2`/`04.P0.1`/`04.P0.4`/`06.P0.1`/`06.P0.2` → … → **`08.P0.1` 汇合** → `08.P0.2`；具体分层以 `task-graph.json` 依赖为准），全部域 P0 done 后由 `00.P0.3` 骨架评审门解锁阶段二。
- **阶段二产品首批就绪候选（待 G3，且须 `00.P0.3` 评审门已过）**：`01.1`、`02.1`、`02.4`（三者仅依赖 `00.3`；阶段二一律以 `00.P0.3` 为额外前置）。
- **关键路径与汇合链（红标；各边均取自 `task-graph.json` 实测边，不暗造整域/波次等待）**：
  - **就绪主干（最长链，20 叶）**：`00.1→00.2→00.3→02.1→01.2→01.3→02.5→02.6→02.7→02.8→02.9`。
  - **持久提交链（可早于 02.9 独立推进）**：`02.1→02.2` 与 `02.1→03.1→03.2` 两条支线在 `03.3` 汇合；`03.3` **只等 `02.2`/`03.2`**（`depends_on=[02.2,03.2]`，**不接 `02.9`**）。旧稿把它写成 `02.9 → 03.3` 的单一链属误写——本仓 JSON 无此边，且外部计划明示「持久提交/恢复不能拖到大部分功能已实现后首次接线」。
  - **恢复/外置链**：`03.3→03.4`、`03.3→03.5` 汇合于 `03.6←03.4/03.5`，再 `03.6→03.7`；与主干 `02.9→04.2` 两条支线在 `03.8` 汇合（`03.8←03.7/04.2`）。
  - **交付/宿主链**：`02.9→04.4`、`03.8→04.4`；`02.9→08.1`、`05.2→08.1`；`08.2←08.1/04.4/04.5`；`08.3←08.2/03.8`；`08.4←06.8/07.1/07.3/07.4/08.3`。
  - **收尾链**：`09.1`（← 01.1–08.4 共 50 项确定性叶）`→09.3→09.6`；`09.5←03.2/04.3/04.4/04.9/05.3/06.6/08.5`。
  - **叶级前置一律以 `task-graph.json` 为准**；`tasks.md` 的 mermaid 只画汇合（其 `01.4→01.5` 等**节点摘要不作派发图**，`01.4` 就绪即可派发，`01.5` 不构成 `01.4` 前置）。
- **本轮做不了什么**：任何产品叶任务（无 G3 写权限、无产品 build/test 授权、无外部资源）；G1 签名探针与 G2 设计推演签证；残件清理与主 spec 变更；提交/归档。

### 授权（每单白名单精确到文件 / 共享面归属 / 预授权与上报边界）

- 每单写入**白名单精确到文件**且**只含本单孙任务所需**；账目**单点归属**（一个文件在某一时刻只有一个写者）。
- **共享面默认集成者单写**：`openspec/changes/runtime-rebuild/` 顶层（`proposal.md`/`design.md`/`contracts.md`/`coverage.md`/`baseline.md`/`tasks.md`/`task-graph.json`/`specs/orchestration/spec.md`/本文件）、`golden/runtime/*`、`scripts/runtime_check*.py`、`.github/workflows/runtime-contracts.yml`。产物级可独立派发时，先把跨包接线契约冻结为**签名级启动常数**再拆（本波不适用）。
- **预授权（本波内自决）**：批次 1/2 白名单内的规划/检查工装修补、覆盖表与 task-graph 的命令对齐、检查器负控补漏、CI 的 go 版本与 `--plan-graph` 校正。**必须上报待裁**：任何越过白名单文件的改动、任何产品单、任何「为目录绿而预建空壳包」、任何疑似新增重大架构选择。
- **域 task 候选写集现状（诚实登记）**：部分域候选仍写作**目录或通配**（如 `exec/`、`mcp/`、`state/` 绑定消费点），本波已按 architecture policy 与计划包职责**收敛为候选文件**或显式标「**尚未可派发**」；**不因收敛动作宣称 G3 闭合**（见 §2 明细）。
- **P0 骨架单白名单（阶段一，未来波；本波不派发）**：每个 P0 单只写**本包文件组 + 该包 `skeleton_assert.go`**（逐包收敛，见各域 `tasks.md`「P0 骨架任务」节）；**不含**任何阶段二 `NN.M` 文件、**不新建未登记包**（O1-4）；P0 文件组仍为**候选拟写文件**，需 G3 逐单授权，本波不据此宣称 G3 闭合。

### 凭据（四查落点按单核 / 复跑命令 / 必须亲自复验的数字）

- **完成度四查**（勾选真实性 / 产物盘点 / 数字溯源 / 遗漏检测）**按派发单核**；未完成者归因后修计划再重派。
- 每条「完成」必须给出**可复跑命令 + 原始退出码 + 产物引用**；数字必须能溯源到命令输出。
- **本波必须亲自复验的数字清单**：自测数（unittest `Ran N tests`）、`check_runtime_plan.py` 计数（`graph_tasks=65`/`checkboxes=65`/`covered_main_capabilities=120`/`spec_files`/`scenarios`）、`check_runtime_architecture.py --check-policy` 的 `policy_packages`/`policy_edges`、`check_runtime_evidence.py --check-contract` 的 `scenario_count=10`、`openspec validate runtime-rebuild --strict` 的判定。
- **每条判据的复跑命令**：见 §3 台账每行；未附命令与退出码的结果不写「完成」。

### 释放（资源锁 ↔ 等待表 / 释放即补派 / 熔断线与分段记账）

- **锁必携等待表**；释放 = 触发，当场消费等待面，不等待整域。本波**无物理锁**（不占设备/端口/配额，不跑长跑）；产品波的资源锁见下表。
- **本轮资源锁（无）**：不运行 `go list`/Go 测试/付费/微信，无设备与配额占用。
- **后续资源锁 ↔ 等待表**（产品波，登记用；本波不消费）：
  | 资源 | 持有单 | 等待者（释放即补派） |
  |---|---|---|
  | rustviking 存储档 | `03.2` 资源门 | `09.5` 存储档子项 |
  | 真实模型端点 | `07.4` 资源门 | `09.5` 真实模型子项 |
  | MCP/A2A 服务 | `04.3`/`04.4` | `09.5` MCP/A2A 子项 |
  | embedder 索引 | `04.7`/`04.9` | `09.5` 检索子项 |
  | collector/OTel | `05.3`（点名用例 `TestRealCollectorDelivery`，非 06.3 的 RL 队列封账） | `09.5` collector 子项 |
  | SFT 制品/tokenizer | `06.6` | `09.5` SFT 子项 |
  | 微信发送通道 | `08.5` | `09.5` 微信子项 |
- **熔断线与分段记账**：单执行失败即停下归因（不静默绕行）；长跑类分段记账（每段一条台账行 + 退出码），不用汇总「PASS」覆盖。

## 2、明细层

### 派发单-孙任务矩阵（本波）

| 单号 | 类型 | 覆盖叶任务 | 状态 | 触碰集 W（精确文件，见 §2.1 独立清单） | 构建/验证读集 R | 证据落点 |
|---|---|---|---|---|---|---|
| B1 | 规划（主代理） | 无（不勾选叶任务） | 已完成 | §2.1「B1」**精确文件清单**（非 `{...}` 通配授权） | 只读 `design.md`/`contracts.md`/`coverage.md` | 台账批次 1 行 |
| B2 | 规划+检查工装（本代理） | 无（不勾选叶任务） | 完成 | §2.1「B2」**精确文件清单**（非 `0{1,2,4,5,9}-*/**md` 通配授权） | 只读 `openspec/specs/**`、既有叶任务候选 | 台账批次 2 行 + 复跑退出码 |
| B3 | 规划+检查工装（本代理） | 无（不勾选叶任务） | 完成 | §2.1「B3」**精确文件清单**（非 `0{5,7,9}-*/**md` 通配授权） | 只读 `openspec/specs/**`、`changes/07-evolution/specs/**` | 台账批次 3 行 + 复跑退出码 |

> 本波最多 **3 产品写单 + 1 集成验证** 的准入上限**未启用**（当前产品单 = 0）；产品派发须 G3 逐单授权，且 W 交 W、W 交对方 R 均为空才准入（§design D7）。

#### 2.1 已执行 B 批次登记写集（精确文件清单，替代原通配授权）

> 本仓 `openspec/changes/runtime-rebuild/**`、`golden/`、`scripts/check_runtime_*.py` 等在**批次 1–3 当时**为未纳入 git 的规划资产（`git status` 显示 `??`，无历史可比对）——此为**批次 1–3 历史语境**；当前基线 `a9381ce` 已将该规划基线与检查工装入库，可 diff。下列清单按台账描述**收敛为文件级**作为该批次登记写集。批次 2/3 在 `task-graph.json`、`09-acceptance/*` 等文件上有**串行重叠**（属收尾），不表示并发；如需 git 级精确写集，须待资产入库后以 diff 回填。

**B1（顶层四件套 + 00 四件套 + 顶层编排 spec + task-graph）**
- `openspec/changes/runtime-rebuild/proposal.md`
- `openspec/changes/runtime-rebuild/design.md`
- `openspec/changes/runtime-rebuild/specs/orchestration/spec.md`
- `openspec/changes/runtime-rebuild/tasks.md`
- `openspec/changes/runtime-rebuild/task-graph.json`
- `openspec/changes/runtime-rebuild/changes/00-foundation/proposal.md`
- `openspec/changes/runtime-rebuild/changes/00-foundation/design.md`
- `openspec/changes/runtime-rebuild/changes/00-foundation/tasks.md`
- `openspec/changes/runtime-rebuild/changes/00-foundation/specs/architecture-guardrails/spec.md`

**B2（跨域规划矛盾修复 + 检查器/CI 补漏；在 B1 之上串行）**
- `openspec/changes/runtime-rebuild/ORCHESTRATION.md`
- `openspec/changes/runtime-rebuild/task-graph.json`
- `openspec/changes/runtime-rebuild/coverage.md`
- `openspec/changes/runtime-rebuild/changes/00-foundation/proposal.md`
- `openspec/changes/runtime-rebuild/changes/00-foundation/design.md`
- `openspec/changes/runtime-rebuild/changes/00-foundation/tasks.md`
- `openspec/changes/runtime-rebuild/changes/00-foundation/specs/architecture-guardrails/spec.md`
- `openspec/changes/runtime-rebuild/changes/01-state-binding/tasks.md`
- `openspec/changes/runtime-rebuild/changes/02-execution/tasks.md`
- `openspec/changes/runtime-rebuild/changes/04-capabilities/tasks.md`
- `openspec/changes/runtime-rebuild/changes/05-inspection/tasks.md`
- `openspec/changes/runtime-rebuild/changes/09-acceptance/proposal.md`
- `openspec/changes/runtime-rebuild/changes/09-acceptance/design.md`
- `golden/runtime/architecture.json`
- `golden/runtime/scenarios.json`
- `scripts/runtime_checklib.py`
- `scripts/check_runtime_plan.py`
- `scripts/check_runtime_architecture.py`
- `scripts/check_runtime_structure.py`
- `scripts/check_runtime_evidence.py`
- `scripts/test_check_runtime_plan.py`
- `scripts/test_check_runtime_architecture.py`
- `scripts/test_check_runtime_structure.py`
- `scripts/test_check_runtime_evidence.py`
- `.github/workflows/runtime-contracts.yml`

**B3（P2-1..P2-4 修复 + 证据状态机/07 激活窗/验收集分层/资源归属）**
- `openspec/changes/runtime-rebuild/ORCHESTRATION.md`
- `openspec/changes/runtime-rebuild/task-graph.json`
- `openspec/changes/runtime-rebuild/changes/05-inspection/tasks.md`
- `openspec/changes/runtime-rebuild/changes/07-evolution/proposal.md`
- `openspec/changes/runtime-rebuild/changes/07-evolution/design.md`
- `openspec/changes/runtime-rebuild/changes/07-evolution/tasks.md`
- `openspec/changes/runtime-rebuild/changes/07-evolution/specs/evolution-evaluation/spec.md`
- `openspec/changes/runtime-rebuild/changes/09-acceptance/design.md`
- `openspec/changes/runtime-rebuild/changes/09-acceptance/tasks.md`
- `golden/runtime/scenarios.json`
- `scripts/check_runtime_evidence.py`
- `scripts/test_check_runtime_evidence.py`

#### 2.2 叶任务派发索引（阶段二 65 叶；全 `planned`、未授权；阶段一 24 个 P0 另见各域 `tasks.md`「P0 骨架任务」节与 `task-graph.json`）

> **本表不是授权**：65 项 `status=planned` 且**无任何产品写权限**；**G3（逐单精确文件 + 实测读闭包）全部未闭合**。W 列**引用各域 tasks.md 的具体编号**（完整候选文件清单在该行）：标「**文件级候选**」= 该域已把 W 收敛为具体文件路径；标「**未收敛**」= 域白名单仍是目录/通配（07/08/09 及 00.6 产品门），**显式 G3 blocked，不得视为精确已齐**。
> 域引用缩写：`[00]`=`changes/00-foundation/tasks.md` … `[09]`=`changes/09-acceptance/tasks.md`。
> `R0`（所有行统一，**保守读集**）= 只读 `openspec/changes/runtime-rebuild/**`（顶层 + 相关域 proposal/design/specs）+ 该单前置产物 + 锁定框架原生接口（trpc-agent-go `v1.11.2-tagent.1` / provider `v1.10.0`）。**本波未跑 `go list`，故读闭包为保守声明、非实测**；G3 需以实际构建系统传递依赖闭包复测，复测前一律 blocked。
> `deps` 列 = `task-graph.json` 的权威 `depends_on`（与本索引逐条一致）。

| id | deps（JSON 权威） | W 候选（域 tasks 引用 + 收敛） | R | G3 | 状态 |
|---|---|---|---|---|---|
| 00.1 | 无 | [00] 00.1 文件级候选（`baseline.md`） | R0 | blocked | planned |
| 00.2 | 00.1 | [00] 00.2 文件级候选（`coverage.md`） | R0 | blocked | planned |
| 00.3 | 00.2 | [00] 00.3 文件级候选（`contracts.md`、`design.md`） | R0 | blocked | planned |
| 00.4 | 00.3 | [00] 00.4 文件级候选（`golden/runtime/architecture.json`、`golden/runtime/scenarios.json`） | R0 | blocked | planned |
| 00.5 | 00.4 | [00] 00.5 文件级候选（`scripts/runtime_checklib.py` + 4×`check_runtime_*.py` + 4×`test_check_runtime_*.py`） | R0 | blocked | planned |
| 00.6 | 00.5 | [00] 00.6 本轮 W 文件级（`runtime-contracts.yml`、`test_check_runtime_workflow.py`）；**后续产品门候选仍为待收窄清单**（`ci.yml`/`lint.sh`/`race_check.sh`/`gen_godoc.sh`）→ **未收敛** | R0 | blocked | planned |
| 01.1 | 00.3 | [01] 01.1 文件级候选 | R0 | blocked | planned |
| 01.2 | 02.1 | [01] 01.2 文件级候选 | R0 | blocked | planned |
| 01.3 | 01.2,02.1 | [01] 01.3 文件级候选 | R0 | blocked | planned |
| 01.4 | 01.1,02.1 | [01] 01.4 文件级候选 | R0 | blocked | planned |
| 01.5 | 01.4 | [01] 01.5 文件级候选 | R0 | blocked | planned |
| 01.6 | 01.5 | [01] 01.6 文件级候选 | R0 | blocked | planned |
| 02.1 | 00.3 | [02] 02.1 文件级候选 | R0 | blocked | planned |
| 02.2 | 02.1 | [02] 02.2 文件级候选 | R0 | blocked | planned |
| 02.3 | 02.2 | [02] 02.3 文件级候选 | R0 | blocked | planned |
| 02.4 | 00.3 | [02] 02.4 文件级候选 | R0 | blocked | planned |
| 02.5 | 01.3,01.4,02.3,02.4 | [02] 02.5 文件级候选 | R0 | blocked | planned |
| 02.6 | 02.5 | [02] 02.6 文件级候选 | R0 | blocked | planned |
| 02.7 | 02.6 | [02] 02.7 文件级候选 | R0 | blocked | planned |
| 02.8 | 02.7 | [02] 02.8 文件级候选 | R0 | blocked | planned |
| 02.9 | 02.8 | [02] 02.9 文件级候选 | R0 | blocked | planned |
| 03.1 | 02.1 | [03] 03.1 文件级候选 | R0 | blocked | planned |
| 03.2 | 03.1 | [03] 03.2 文件级候选 | R0 | blocked | planned |
| 03.3 | 02.2,03.2 | [03] 03.3 文件级候选 | R0 | blocked | planned |
| 03.4 | 02.5,03.3 | [03] 03.4 文件级候选 | R0 | blocked | planned |
| 03.5 | 03.3 | [03] 03.5 文件级候选 | R0 | blocked | planned |
| 03.6 | 03.4,03.5 | [03] 03.6 文件级候选 | R0 | blocked | planned |
| 03.7 | 03.6 | [03] 03.7 文件级候选 | R0 | blocked | planned |
| 03.8 | 03.7,04.2 | [03] 03.8 文件级候选 | R0 | blocked | planned |
| 04.1 | 02.6 | [04] 04.1 文件级候选 | R0 | blocked | planned |
| 04.2 | 02.9,01.5 | [04] 04.2 文件级候选 | R0 | blocked | planned |
| 04.3 | 04.1 | [04] 04.3 文件级候选 | R0 | blocked | planned |
| 04.4 | 02.9,03.8 | [04] 04.4 文件级候选 | R0 | blocked | planned |
| 04.5 | 02.6 | [04] 04.5 文件级候选 | R0 | blocked | planned |
| 04.6 | 04.5,03.3 | [04] 04.6 文件级候选（design-blocked：goal 词待核） | R0 | blocked | planned |
| 04.7 | 03.4,03.5,04.5 | [04] 04.7 文件级候选（design-blocked：接口必要性待验） | R0 | blocked | planned |
| 04.8 | 04.7 | [04] 04.8 文件级候选 | R0 | blocked | planned |
| 04.9 | 04.7 | [04] 04.9 文件级候选（design-blocked：依赖授权资源） | R0 | blocked | planned |
| 05.1 | 01.3,01.5,02.2 | [05] 05.1 文件级候选 | R0 | blocked | planned |
| 05.2 | 05.1 | [05] 05.2 文件级候选 | R0 | blocked | planned |
| 05.3 | 02.6,05.2 | [05] 05.3 文件级候选 | R0 | blocked | planned |
| 05.4 | 03.3,04.2,05.3 | [05] 05.4 文件级候选 | R0 | blocked | planned |
| 06.1 | 02.5 | [06] 06.1 文件级候选 | R0 | blocked | planned |
| 06.2 | 02.4,06.1 | [06] 06.2 文件级候选 | R0 | blocked | planned |
| 06.3 | 06.2 | [06] 06.3 文件级候选 | R0 | blocked | planned |
| 06.4 | 03.3,06.3 | [06] 06.4 文件级候选 | R0 | blocked | planned |
| 06.5 | 06.4 | [06] 06.5 文件级候选 | R0 | blocked | planned |
| 06.6 | 06.5 | [06] 06.6 文件级候选（design-blocked：真实制品/入口待授权） | R0 | blocked | planned |
| 06.7 | 06.4 | [06] 06.7 文件级候选 | R0 | blocked | planned |
| 06.8 | 01.5,06.7 | [06] 06.8 文件级候选 | R0 | blocked | planned |
| 07.1 | 04.7,05.4 | ⚠ [07] **未收敛**：域白名单为目录/通配（`evolution/**`、`plugin/**`、`resources/prompts/**`、`tests/integration/evolution/**`） | R0 | **blocked（未收敛）** | planned |
| 07.2 | 03.3,01.6 | ⚠ [07] **未收敛**（同 07.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 07.3 | 07.2,05.3 | ⚠ [07] **未收敛**（同 07.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 07.4 | 01.5,02.4 | ⚠ [07] **未收敛**（同 07.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 08.1 | 02.9,05.2 | ⚠ [08] **未收敛**：域白名单为通配（module 根公共入口文件、`examples/wechat-bot/**`、`tests/integration/host/**`） | R0 | **blocked（未收敛）** | planned |
| 08.2 | 08.1,04.4,04.5 | ⚠ [08] **未收敛**（同 08.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 08.3 | 08.2,03.8 | ⚠ [08] **未收敛**（同 08.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 08.4 | 06.8,07.1,07.3,07.4,08.3 | ⚠ [08] **未收敛**（同 08.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 08.5 | 08.4 | ⚠ [08] **未收敛**（同 08.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 09.1 | 01.1–01.6,02.1–02.9,03.1–03.8,04.1–04.8,05.1–05.4,06.1–06.5,06.7–06.8,07.1–07.4,08.1–08.4（50 项确定性叶） | ⚠ [09] **未收敛**：域白名单为通配（tooling 单检查资产、`tests/integration/acceptance/**`） | R0 | **blocked（未收敛）** | planned |
| 09.2 | 00.1,03.3,03.5 | ⚠ [09] **未收敛**（同 09.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 09.3 | 00.6,08.1,09.1 | ⚠ [09] **未收敛**（同 09.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 09.4 | 09.1,09.2 | ⚠ [09] **未收敛**（同 09.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 09.5 | 03.2,04.3,04.4,04.9,05.3,06.6,08.5 | ⚠ [09] **未收敛**（同 09.1 白名单） | R0 | **blocked（未收敛）** | planned |
| 09.6 | 09.1,09.2,09.3,09.4,09.5 | ⚠ [09] **未收敛**（同 09.1 白名单） | R0 | **blocked（未收敛）** | planned |

> **收敛口径（诚实登记）**：01–06 域已在各自 `tasks.md` 把候选写域**收敛为文件级**（见对应行）；**07/08/09 三域仍为目录/通配**，00.6 的**产品门候选**同样未收窄——这些一律 **G3 blocked，不因本表或收敛动作宣称 G3 闭合**。无 `go list` 实测，**不伪造读闭包**；G3 需实际闭包复测时维持 blocked。

### 合单判据

- 两候选单的 W 不相交，且各自 W 不与对方 R 相交（测试包全部源文件计入 R）→ 可并发。
- 同文件簇（含测试包）→ **合单或串行**，不以「文件名不同/追加兼容」放行。
- 本波 B1/B2 同属规划面，**串行执行**（B2 在 B1 产物之上收尾），避免同一规划文书的并发写。

### 预算上限

- 本波：**无网络预算**（不安装、不下载、不付费）；无长跑；单命令均为本地 Python/OpenSpec 校验。
- 产品波：逐单在 G3 登记命令集与资源预算后再开放（本波不预设）。

### 档位与真实路径欠账

- 档位：`memory`/`localfile`/`file(rustviking)` 三档中，**只有非资源档可在无授权下作编译级证明**；`file(rustviking)` 与一切真实档为资源欠账。
- 真实路径欠账（未验证，不得称通过）：`04.9` 召回、`06.6` SFT、`08.5` 微信、`07.4` 真实模型、`09.4` soak/崩溃恢复、`09.5` 各资源子项。
- **验收集分层（本轮 P2-3）**：golden 合同**始终含全 10 场景**，唯一 scope 登记（S01–S09=`deterministic`、S10=`resource`）。`09.1` 确定性 runner 用 `--scope deterministic` 只验确定性集并输出 `scope` 与 `remaining_required`（S10 为**显式欠账**，不报全通过、不得合成 SFT）；完整 golden（含 S10 真实 `06.6` 及其他真实组件）由 `09.5` 以默认 `--scope complete` 验收。
- 候选写集收敛（01/02/04/05 域 tasks）：已按 architecture policy 收敛为候选文件；**仍无 G3 逐文件授权，相关叶任务为「尚未可派发」**。

## 3、台账层

> 规则：每行必须有**真实命令与原始退出码**；无凭据摘要不得写「完成」。`productVerified` 恒为 `null`。

### 批次 1（主代理，已完成基础文档/修补）

| 项 | 产物 | 状态 | 凭据 |
|---|---|---|---|
| 顶层四件套 | `proposal.md`/`design.md`/`specs/orchestration/spec.md`/`tasks.md` | 完成 | `openspec validate runtime-rebuild --strict --no-interactive` → 见下 |
| 00-foundation 四件套 | `changes/00-foundation/**` | 完成 | 同上 |
| 01–09 四件套 | `changes/0{1..9}-*/**` | 完成（待本波修矛盾） | `check_runtime_plan.py` 计数（见下） |
| manifests | `task-graph.json`/`contracts.md`/`coverage.md`/`baseline.md` | 完成 | 同上 |

### 批次 2（本代理收尾，本轮；下列数字为批次 2 完成时快照，批次 3 四项修复后已重跑，见下）

| 项 | 产物 | 状态 | 凭据（原始命令 → 退出码） |
|---|---|---|---|
| 编排文档 | `ORCHESTRATION.md`（本文件） | 完成 | `check_runtime_plan.py --json` → `ok=true`、`orchestration_layers=4`、EXIT=0 |
| 跨域规划矛盾修复 | 00/09 guardrails 收口（09 重复 delta 用 DeleteFile 删除，proposal/design 引用 00）；04 轮询改单次回执；01/02 依赖分级（签名 vs 实现）；02/04 候选路径收敛；coverage 写者登记；task-graph 命令对齐 | 完成 | `check_runtime_plan.py --json` → EXIT=0；见下方逐条复跑块 |
| 检查器/CI 补漏 | `check_runtime_evidence.py` 增 `evidence_contract` 自洽校验；`scenarios.json` 同步 `execution` 字段与 test_log_ref/runner 语义；CI go `>=1.24.1` 与场景合同 `--plan-graph`；新增 6 条负控/正控自测 | 完成 | `python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'` → `Ran 117 tests ... OK`, EXIT=0（**批次 3 修复后为 `Ran 130 tests ... OK`**） |
| 自测 | `scripts/test_check_runtime_*.py` | 完成 | unittest `Ran 117 tests in 3.194s OK`, EXIT=0（批次 2 快照；**批次 3 后 `Ran 130 tests ... OK`**） |
| 契约/政策/场景校验 | plan / architecture policy / evidence contract | 完成 | 见下方复跑块（各原样退出码） |
| OpenSpec 校验 | `openspec validate runtime-rebuild --strict --no-interactive` | 完成 | `Change 'runtime-rebuild' is valid`, EXIT=0 |

#### 批次 2 复跑凭据（原始退出码，非汇总）

| 命令 | 退出码 | 关键输出 |
|---|---|---|
| `python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'` | 0 | `Ran 117 tests ... OK`（批次 3 后 **130**） |
| `python3 scripts/check_runtime_plan.py --json` | 0 | `ok=true`；graph_tasks=65/checkboxes=65/covered_main_capabilities=120/spec_files=62/scenarios=429/requirements=124/modified=55/added=69/removed=0/domains_with_spec=10/graph_edges=162/orchestration_layers=4（批次 3 后 **scenarios=433/requirements=126/modified=57**） |
| `python3 scripts/check_runtime_architecture.py --check-policy --json` | 0 | `ok=true`；policy_packages=19/policy_edges=19 |
| `python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json` | 0 | `ok=true`；scenario_count=10/observation_values=51/evidence_contract_fields=18 |
| `openspec status --change runtime-rebuild --json` | 0 | `isComplete=true`，4/4 artifacts done |
| `openspec validate runtime-rebuild --strict --no-interactive` | 0 | `Change 'runtime-rebuild' is valid` |
| `python3 -c "import yaml;yaml.safe_load(open('.github/workflows/runtime-contracts.yml'))"` | 0 | `YAML ok` |
| `python3 -m py_compile scripts/check_runtime_*.py scripts/test_check_runtime_*.py` | 0 | `py_compile ok` |

> 批内仍**无产品验证**：无 Go build/test/vet、无 `go list`、无安装/下载/付费/微信、无提交/归档。

### 批次 3（本代理，本轮四项问题修复，P2-1..P2-4）

| 项 | 定位 | 产物 | 状态 | 凭据 |
|---|---|---|---|---|
| P2-1 证据状态机包窗口 | `scripts/check_runtime_evidence.py` `index_test_log` | 拒包启动前/结束后测试事件、包终局不得有未终结用例、子测试须在父测试窗内；允许合法多包并发与并行子测试 run/pause/cont/pass | 完成 | 见下复跑块（unittest 130 OK）；正反例见 `test_check_runtime_evidence.py` |
| P2-1 夹具真实顺序 | `scripts/test_check_runtime_evidence.py` | `full_test_log` 子测试嵌套于父 `run..pass`；不再用 `rows.pop()` 造合法假象，改以负例证明其被拒 | 完成 | 负例 `test_package_terminal_moved_before_its_tests_is_rejected`/`test_subtest_after_parent_pass_is_rejected`；正例 `test_multi_package_concurrency_is_valid`/`test_parallel_subtest_lifecycle_is_valid` |
| P2-2 07 激活时刻窗 | `changes/07-evolution/{design,proposal,tasks}.md` + `specs/evolution-evaluation/spec.md` | 窗口锚由 commit 时刻改为**激活证据时刻**、真实消费版本归因、GitSHA 仅关联；两条既有 Requirement 以 **MODIFIED**（完整主源 + 修订）语义覆盖，旧矛盾条文不再并存 | 完成 | `check_runtime_plan.py` → modified=57（+2）、requirements=126（+2）、scenarios=433（+4），EXIT=0 |
| P2-3 验收集分层 | `golden/runtime/scenarios.json` + `scripts/check_runtime_evidence.py`（`--scope`）+ `changes/09-acceptance/{tasks,design}.md` + `task-graph.json` 09.1 | 合同始终全 10，唯一 scope 登记（S01–S09=`deterministic`、S10=`resource`）；默认 `complete` 严格，`--scope deterministic` 只验子集并输出 `scope`/`remaining_required`，对缺 S10 不报全通过、拒绝 scope 外记录；S10 `task_ids` 补 `06.6` | 完成 | 新自测 8 条（默认缺 S10 失败、子集通过且显式欠账、拒绝合成 S10、完整仍需 S10、未知/未登记/缺块/重复 scope） |
| P2-4 资源归属 | `changes/09-acceptance/{tasks,design}.md` + `changes/05-inspection/tasks.md` + 本文件资源表 + `task-graph.json` 05.3/09.5 | collector/OTel 归属由 `06.3` 改 **`05.3`**，点名用例 `TestRealCollectorDelivery`（真实 collector 收 span/metric）；关闭 OTel 仍 Query 可用=确定性 `TestObservationDoesNotOwnState`；不以 fixture 封账冒充 collector 收到 | 完成 | 见下复跑块 |

#### 批次 3 复跑凭据（原始退出码，非汇总）

| 命令 | 退出码 | 关键输出 |
|---|---|---|
| `python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'` | 0 | `Ran 130 tests ... OK`（批次 2 为 117，+13） |
| `python3 scripts/check_runtime_plan.py --json` | 0 | `ok=true`；graph_tasks=65/checkboxes=65/covered_main_capabilities=120/spec_files=62/scenarios=433/requirements=126/modified=57/added=69/removed=0/domains_with_spec=10/graph_edges=162/orchestration_layers=4 |
| `python3 scripts/check_runtime_architecture.py --check-policy --json` | 0 | `ok=true`；policy_packages=19/policy_edges=19 |
| `python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json` | 0 | `ok=true`；scenario_count=10/observation_values=51/evidence_contract_fields=18/scenarios_required=10/scope_is_complete=1（scope=`complete`，remaining=[]） |
| `openspec validate runtime-rebuild --strict --no-interactive` | 0 | `Change 'runtime-rebuild' is valid` |
| `openspec status --change runtime-rebuild --json` | 0 | `isComplete=true`，4/4 artifacts done |
| `python3 -c "import yaml;yaml.safe_load(open('.github/workflows/runtime-contracts.yml'))"` | 0 | `YAML ok`（本批**未改 CI**：scope 登记已由“Scenarios contract”步骤的 `validate_contract` 覆盖，无需子集接线） |
| `python3 -m py_compile scripts/check_runtime_*.py scripts/test_check_runtime_*.py` | 0 | `py_compile ok` |

> **层次 scope 与剩余真实资源欠账**（不得称通过）：先分清两个不同陈述，不得混读——
> 1. **合同差集（假设性）**：`remaining_required=[S10]` 是**仅当确定性子集（S01–S09）实际跑绿时**，`--scope complete` 相对合同会列出的**差集**；它**不表示当前只有 S10 未验**。
> 2. **当前实况（零证据）**：scenario runner **尚未实现**、**无任何产品运行**，故 **S01–S10 全部无产品证据**；contract 模式（`--check-contract`）下 `remaining=[]` **只表示合同自洽/完整（10 场景齐备、scope 登记唯一）**，**绝不表示运行完成**。
>
> 因此：**09 全部真实项**（09.1–09.6）与 **G1/G2 取证**均**未验**；`09.5` 全部真实资源子项（真实模型/MCP/A2A/embedding/collector/SFT/微信/存储档）与 `09.4` soak 仍欠账；65 叶任务**全部 planned**；**无产品 build/test/vet**。

### 批次 4（本代理，规划一致性修正；只写 5 个规划文件，不碰产品/测试/脚本/CI/提交）

| 项 | 定位 | 产物 | 状态 | 凭据 |
|---|---|---|---|---|
| C1 真实汇合链 | 本文件 §1 次序「关键路径」 | 删除虚构边 `02.9→03.3`，改为**从 `task-graph.json` 实测的多条汇合链**（主干 20 叶、持久链 `03.3←02.2/03.2` 且不接 02.9、恢复链 `03.8←03.7/04.2`、交付/宿主链、收尾链） | 完成 | 边集与 `python3` 最长链/汇合节点复算逐条一致（见下复跑） |
| C2 叶级派发索引 | 本文件 §2 明细层 | 新增 §2.1（B1/B2/B3 精确文件写集，替代 `0{...}**md` 通配）与 §2.2（**65 叶逐 id** 的 deps/W 引用+G3 收敛/R 保守范围/状态）；**07/08/09 及 00.6 产品门**显式 **G3 blocked（未收敛）** | 完成 | `check_runtime_plan.py` → `graph_edges=162`（未改图边）、`orchestration_layers=4`、EXIT=0 |
| C3 09.4 新锚 | `task-graph.json` 09.4 + `changes/09-acceptance/{design,tasks}.md` | 恢复新计划用例名 `TestSoakContinuity`（`-race ./tests/integration/host`），**不以旧 CI 名 `TestSoak_Continuity` 替代**；旧 30×30/45m 降为**待核参数参考**；macOS/Linux 实际崩溃恢复/热更/泄漏仍必验 | 完成 | 三处命令一致（`grep -c TestSoakContinuity`=3、`grep -c TestSoak_Continuity`=0） |
| C4 09.3 产品 race | `task-graph.json` 09.3 + `changes/09-acceptance/{design,tasks}.md` | 补**产品 race 入口**：根 `go test -race ./... -short -count=1 -json` + 微信 `go -C examples/wechat-bot test -race ./... -short -count=1 -json`（全第一方子包递归、存原始退出码）；保留 `test_race_check.sh` 自测并标注**不等价于产品 race**；prototype 连带编译=额外只读、本轮不执行 | 完成 | 见 `changes/09-acceptance/tasks.md` 09.3 命令块（两行） |
| C5 scope 语义澄清 | 本文件 §3 层次 scope 段 | 区分「合同差集 `remaining_required=[S10]`（假设性）」与「当前实况：S01–S10 全无产品证据、`remaining=[]` 只表示合同自洽」；明确 09 全部真实项与 G1/G2 未验 | 完成 | `check_runtime_evidence.py --check-contract` → `scope=complete`、`remaining_required=[]`、`scenario_count=10`、EXIT=0 |

#### 批次 4 复跑凭据（原始退出码，非汇总）

| 命令 | 退出码 | 关键输出 |
|---|---|---|
| `python3 -c "import json;json.load(open('openspec/changes/runtime-rebuild/task-graph.json'))"` | 0 | `task-graph.json OK` |
| `python3 scripts/check_runtime_plan.py --json` | 0 | `ok=true`；graph_tasks=65/checkboxes=65/graph_edges=162/orchestration_layers=4/scenarios=433/requirements=126/modified=57/added=69/removed=0/domains_with_spec=10（**与批次 3 数值一致，未见回归**） |
| `python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json` | 0 | `ok=true`；scenario_count=10/scenarios_required=10/scope=complete/remaining_required=[]（contract-only，**无运行证据**） |
| `python3 scripts/check_runtime_architecture.py --check-policy --json` | 0 | `ok=true`；policy_packages=19/policy_edges=19 |
| `openspec validate runtime-rebuild --strict --no-interactive` | 0 | `Change 'runtime-rebuild' is valid` |
| `openspec status --change runtime-rebuild --json` | 0 | `isComplete=true`，4/4 artifacts done |

> **自测数来源说明**：本批**未改**任何 `scripts/**` 或 `test_check_runtime_*.py`，故**不重复同一自测**；台账引用的 `Ran 130 tests ... OK`（EXIT=0）来自**主代理本会话刚亲验**（批次 3 基线），**不冒称本批新测**。
> **本批无产品验证**：无 Go build/test/vet、无 `go list`、无安装/下载/付费/微信、无提交/归档；物理写面仅下方 5 个规划文件。
> **本批写面（精确 5 文件）**：`openspec/changes/runtime-rebuild/ORCHESTRATION.md`、`openspec/changes/runtime-rebuild/tasks.md`、`openspec/changes/runtime-rebuild/task-graph.json`、`openspec/changes/runtime-rebuild/changes/09-acceptance/design.md`、`openspec/changes/runtime-rebuild/changes/09-acceptance/tasks.md`。
> **未通过的 G3 项（维持 blocked）**：07/08/09 三域候选写域仍为目录/通配、00.6 产品门候选未收窄、全部 65 叶的**实测读闭包**未跑——均**G3 blocked**，本批**未**声称 G3 闭合。

### 批次 5（清理 + 提交；用户 2026-10-13 明确授权本批删除与本地 commit）

> 用户授权：本批可删除旧代残件并做**本地** `git commit`（**不 push**，不 force，不改 git config，不跳钩子）。本批**只写 2 个规划 md**（`baseline.md`、本文件）；不改外部批准计划（仓库外）。

| 项 | 定位/内容 | 状态 | 凭据 |
|---|---|---|---|
| 文档落账 | `baseline.md` §2.1.1 + §4.1「处置更新（2026-10-13 用户授权清理）」；本行 | 完成 | `git diff --stat -- openspec/changes/runtime-rebuild/baseline.md` |
| 残件清理 | 整目录删除 6 项（见下清单）= **28 文件** + 1 空目录；删除前 `find` 对账=28（派单 30），删除后 `find`=0 | 完成 | 删除后六项均 `GONE`（见复跑块） |
| 本地提交 | 本地 commit（不 push）：`chore(plan): land runtime-rebuild planning baseline and contract checkers; remove stale wave remnants` | 完成 | `git show --stat HEAD` → `114 files changed, 10212 insertions(+)`、`0 deletions` |

**删除清单摘要（仅 6 项整目录，未扩大）**：`model/`(2) · `state/`(4) · `tagent/`(2) · `tests/`(2) · `openspec/changes/rebuild-formal-runtime/`(18) · `.tmp-proto-a/`(0) = **28 文件**（派单预期 30，差 2 已在 `baseline.md` §2.1.1 对账说明）。

**提交范围（逐路径 add，13 路径；禁 `git add .`/`-A`/`-f`）**：`.github/workflows/runtime-contracts.yml` · `golden` · `openspec/changes/runtime-rebuild` · `scripts/runtime_checklib.py` · `scripts/check_runtime_{plan,architecture,structure,evidence}.py` · `scripts/test_check_runtime_{plan,architecture,structure,evidence,workflow}.py`。**不含** `examples/`、`.tagent-workspace/`、`.pre-isolation-backup/`、用户 interview skill。

**保护面（本批不动，点名）**：用户 interview skill `examples/wechat-bot/skills/interview/agent-interview-helper/**`、`prototype/**`、`docs/**`、`openspec/specs/**`、`openspec/changes/archive/**`、`LEDGER.md`、`.tagent-workspace/**`、`.pre-isolation-backup/**`、`resources/**`、`scripts/` 既有文件、`.github/workflows/ci.yml`、`go.mod`/`go.sum`。

#### 批次 5 复跑凭据（原始退出码，非汇总）

| 命令 | 退出码 | 关键输出 |
|---|---|---|
| 删除前 `find model state tagent tests openspec/changes/rebuild-formal-runtime -type f \| wc -l` | 0 | **28**（=2+4+2+2+18；派单预期 30，差 2 见 §对账说明） |
| 删除后 六路径 `[ -e ]` 检查 + `find … -type f \| wc -l` | 0 | 六项均 `GONE`；残留文件数 **0** |
| `python3 scripts/check_runtime_plan.py --change-dir openspec/changes/runtime-rebuild --json` | 0 | `ok=true`；graph_tasks=65/checkboxes=65/covered_main_capabilities=120/scenarios=433/spec_files=62 |
| `python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'` | 0 | `Ran 130 tests in 5.333s` / `OK` |
| `python3 -c "import yaml;yaml.safe_load(open('.github/workflows/runtime-contracts.yml'))"` | 0 | `YAML ok` |
| `openspec list --json` | 0 | 仅 `runtime-rebuild`（`totalTasks=14`）；`rebuild-formal-runtime` 已消失 |
| `openspec validate runtime-rebuild --strict --no-interactive` | 0 | `Change 'runtime-rebuild' is valid` |
| `git diff --cached --name-only \| wc -l`（提交前） | 0 | **114**，根集恰为 13 规定路径；无 `examples/`、`.tagent-workspace`、`.pre-isolation-backup`、interview skill |
| `git status --short`（提交后） | 0 | 仅 `?? examples/wechat-bot/skills/interview/agent-interview-helper/` |
| `git show --stat HEAD`（提交后） | 0 | `114 files changed, 10212 insertions(+)`（0 deletions，残件均未跟踪故不入索引） |

> 提交为**本地单一提交**（无 push/force、未改 config、未跳钩子）；message 见上行。
> 本批**无产品验证**：无 Go build/test/vet、无 `go list`、无安装/下载/付费/微信、无归档。

### 批次 6（本代理，CodeReview 7 项确认问题修复 + 预期档/复用导航 + golden 关联 + HEAD 基线表述；只写规划 md/json 与 `golden/runtime/scenarios.json`）

> 用户授权：本轮为**规划补充批次**（**非产品 apply**）；**唯一可写** = `openspec/changes/runtime-rebuild/**` 现有/新增规划 md/json + `golden/runtime/scenarios.json`；不碰产品/测试/主 spec/历史 wiki/prototype/旧 CI 与 scripts/go.mod/go.sum/interview skill；外部批准计划**只读**。**不安装/网络/Go build·test·list/提交/推送/归档/清理**；仅跑检查器自测、plan/policy/contract 模式、OpenSpec strict。

| 项 | 定位 | 产物 | 状态 | 凭据 |
|---|---|---|---|---|
| Q1 Receipt/异步义务 | `02-execution/specs/subagent-turn-execution/spec.md` + 02 proposal/tasks | 成功受理 Receipt **≠** 义务结清（义务保持至结清）；仅未新增义务的 dedup/rejected 撤本次预登记、旧义务不动；统一事件调度（同 invocation 可多 decision）、受理≠运行即成功、无第二循环；不冻结旧类名/500ms | 完成 | `openspec validate` EXIT=0（见下） |
| Q2 空 Governance 恒真 | `01-state-binding/specs/config-state-twinship/spec.md` + 01 proposal/design/tasks | 缺必需检查**拒绝**、不适用/禁用**显式记录**（非证据通过）；候选拒绝**释放候选独占资源**、旧资源不受影响 | 完成 | 同上 |
| Q3 Apply prompt 固定 vs 热读 | 同 spec | state 持**结构绑定+文件来源**，请求边界读取并固化**实际内容版本**；历史快照重放不阻新请求文件更新；**文件内容版本 ≠ 结构 generation** | 完成 | 同上 |
| Q4 07 commit 时刻开窗 | `07-evolution/specs/{git-native-refine,evolution-evaluation}/spec.md` + 07 级联 | 窗口锚改**真实请求消费版本 + 首消费激活证据**；register 补 Git 关联**不重贴**旧请求/反馈；缺关联标 unknown/insufficient；保全 Git 安全/四态；不引入新版本平台 | 完成 | `modified=57`（Q4 为 MODIFIED 替换，未增计数） |
| Q5 三次 unknown 判死 | `04-capabilities/specs/resident-session-continuity/spec.md` + 04 design/tasks + 05 级联 | 连续 unknown **只升级诊断或显式停止**，**次数阈值不作退出依据**；未止义务/租约保留且可解释 | 完成 | 同上 |
| Q6 用途矩阵 + 中性 SFT | `06-learning/specs/{offline-training-export,trajectory-recording}/spec.md` + 06 级联 | 补 DPO/采样 RL 用途矩阵（用途/证据/qualified·ineligible·unknown/缺失原因）；中性 SFT 消费合同（旧 AReaL 仅 v2 只读导入）；SFT 资格不推出其它用途；episode≠session 不强制等同/拆分 | 完成 | 同上 |
| Q7 03.8 外置归 workspace | `03-history/{proposal,design,specs/tool-output-interception/spec.md}` | 不预建未登记根 `tool` 包；输出外置归 `workspace/externalize.go`（原生 wrapper 注入端口）；不放宽 architecture policy | 完成 | `check_runtime_architecture --check-policy` → policy_packages=19/policy_edges=19，EXIT=0 |
| E1 预期档（新） | `expectations.md`（154 行） | 5 O + 可判定 KR（结果/真实消费者/任务IDs/场景证据/待核）；10 金样逐条表；non-goal 不含真实微信必验；不自报完成率 | 完成 | 链接见 E5；本文件 |
| E2 复用导航（新） | `trpc-reuse.md`（104 行） | 8 类能力逐行 module/签名/差异/接线/状态；core fork `v1.11.2-tagent.1` / provider `v1.10.0`；evaluator 独立 module 标 **candidate**（不自动升级/安装） | 完成 | §§1–9 |
| E3 golden 关联 | `golden/runtime/scenarios.json` | S03 call/task/decision + receipt/正文请求索引；S05 drain 两阶段；S07 去重→**引用互不覆盖/内容相等**（不强制优化）；S08 派生数量/归属阻塞；S10 工件链/替换绑定；SFT 非空/形状对齐（不预设批次/制品）；保留 10 ID/scope、S10 资源门 | 完成 | `check_runtime_evidence --check-contract` → scenario_count=10、observation_values=63，EXIT=0 |
| E4 HEAD 基线表述 | 顶层 `design.md` + `04-capabilities/design.md` + `baseline.md` | 56c58eb 标**清理前历史**、a9381ce 为**当前新基线**；保留 baseline 历史记录只附更新（本单无清理/提交） | 完成 | 见下 |
| E5 单点链接 | 顶层 `proposal/design/tasks` + 本文件 §0 + `00-foundation/tasks.md` 00.3 + `09-acceptance/tasks.md` 09.6 | 预期档/复用导航单点链接，不全文互抄 | 完成 | 链接可解析 |

#### 批次 6 复跑凭据（原始退出码，非汇总）

| 命令 | 退出码 | 关键输出 |
|---|---|---|
| `python3 -B -m unittest discover -s scripts -p 'test_check_runtime_*.py'` | 0 | `Ran 130 tests ... OK` |
| `python3 scripts/check_runtime_plan.py --json` | 0 | `ok=true`；graph_tasks=65/checkboxes=65/covered_main_capabilities=120/spec_files=62/**scenarios=442/requirements=127/added=70**/modified=57/removed=0/domains_with_spec=10/graph_edges=162/orchestration_layers=4（相对批次 3–5 的 requirements=126→127、scenarios=433→442、added=69→70，系本批 spec 增补，非回归） |
| `python3 scripts/check_runtime_architecture.py --check-policy --json` | 0 | `ok=true`；policy_packages=19/policy_edges=19 |
| `python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json` | 0 | `ok=true`；scenario_count=10/scenarios_required=10/scope=complete/remaining_required=[]/observation_values=63/evidence_contract_fields=18 |
| `openspec validate runtime-rebuild --strict --no-interactive` | 0 | `Change 'runtime-rebuild' is valid` |

> 本批**无任何产品验证**：无 Go build/test/vet、无 `go list`、无安装/下载/付费/微信、无提交/推送/归档/清理；65 叶任务**全 planned**、checkbox **全未勾**、DAG 无环（`check_runtime_plan` 含 cycle 检测，`ok=true`）。G1 **未运行**；G2 为**桌面推演**（实施验收是未来 09 证据，两者分别说清）。

### 批次 7（本代理，两阶段修订落盘；只写规划与检查器资产，**不写任何产品 Go 代码**）

> 用户授权：本批为**单一批次的两阶段修订**——只改规划 md/json 与检查器脚本（`scripts/runtime_checklib.py`/`check_runtime_plan.py`/`test_check_runtime_plan.py`）；**不写任何产品 Go 代码**；外部批准计划只读；不跑产品 `go build/test/vet`、不安装依赖、不提交/推送/归档、不付费/微信。

| 项 | 定位/内容 | 状态 | 凭据 |
|---|---|---|---|
| 阶段一骨架约定冻结 | `design.md` 新增决策 **D9 两阶段交付与骨架先行**（stub 形态/编译断言/英文 Godoc/包注释/≤500 行/同文件内聚；阶段门 `00.P0.3`；被否方案） | 完成 | `openspec validate` EXIT=0（见下） |
| task-graph 两阶段 | `task-graph.json` 增 24 个 P0 骨架任务（`phase=skeleton`，`00.P0.2=tooling`）；59 个阶段二 `NN.M` 追加阶段门边 `00.P0.3` | 完成 | `check_runtime_plan.py` → graph_tasks=89/checkboxes=89、errors=[]、EXIT=0 |
| 检查器两阶段 | `runtime_checklib.py`（PHASES 加 `skeleton`、TASK_ID_RE 支持 `NN.P0.M`）；`check_runtime_plan.py`（P0_RANGES＋阶段门/P0 phase 硬校验）；`test_check_runtime_plan.py`（fixture 支持 P0＋4 负控） | 完成 | `python3 -B -m unittest discover -s scripts -p 'test_check_runtime_*.py'` → `Ran 134 tests ... OK`、EXIT=0 |
| 00.P0.1/00.P0.2 完成 | 骨架约定冻结（D9）＋检查器/自测扩展；**工装判据，非产品** | 完成 | evidence run_id=**rev2phase-20261013**；exit_codes=[0,0,0] |
| 规划文档两阶段 | `design.md` D9、`proposal.md` What Changes、`expectations.md`（§0 分母→89/§8 G1 实体化）、顶层 `tasks.md`（头部＋0.0 行）、`specs/orchestration/spec.md`（ADDED `两阶段交付与骨架评审门`）、各域 `tasks.md` P0 节 | 完成 | `check_runtime_plan.py` → `orchestration_layers=4`、`ok=true`、EXIT=0 |

#### 批次 7 复跑凭据（原始退出码，非汇总）

| 命令 | 退出码 | 关键输出 |
|---|---|---|
| `python3 -B -m unittest discover -s scripts -p 'test_check_runtime_*.py'` | 0 | `Ran 134 tests ... OK`（批次 6 为 130，+4 P0 负控） |
| `python3 scripts/check_runtime_plan.py --json` | 0 | `ok=true`；graph_tasks=89/checkboxes=89/graph_edges=318/covered_main_capabilities=120/spec_files=62/scenarios=447/requirements=128/added=71/modified=57/removed=0/domains_with_spec=10/orchestration_layers=4 |
| `python3 scripts/check_runtime_architecture.py --check-policy --json` | 0 | `ok=true`；policy_packages=19/policy_edges=19 |
| `python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/runtime-rebuild/task-graph.json --json` | 0 | `ok=true`；scenario_count=10/scenarios_required=10/scope=complete（`scenario_count=10` 不变） |
| `openspec validate runtime-rebuild --strict --no-interactive` | 0 | `Change 'runtime-rebuild' is valid` |
| `git status --short` | 0 | 仅白名单 20 文件（+ 本轮新改 `ORCHESTRATION.md`）；`?? examples/wechat-bot/skills/interview/agent-interview-helper/`（用户未跟踪，未触碰） |

> **本批无产品验证**：无 Go build/test/vet、无 `go list`、无安装/下载/付费/微信、无提交/推送/归档；**产品完成度仍=0**（阶段一 24 P0 中仅 00.P0.1/00.P0.2 工装完成，阶段二 65 项全 `planned`）。G1 骨架落码级探针**未执行**；G2 为**桌面推演**。

### 明确未完成（不得勾选）

- 阶段二 **65 个 `NN.M` 叶任务全部 `planned`**；阶段一 24 个 P0 骨架中，**仅 00.P0.1/00.P0.2（规划/工装，非产品）随本轮完成**（evidence run_id=`rev2phase-20261013`），其余 22 个 P0 与全部阶段二为 `planned`。G1 骨架落码级探针、G2 设计推演（正常/失败/并发/重启）**未执行**（产品观测属 09 证据，**不属 G2**）；无产品 build/test/vet；无 `go list`；无提交/归档。
- 外部计划文件**未改**；主 `openspec/specs/`、prototype、历史 wiki、用户文件**未改**。
