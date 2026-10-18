# ORCHESTRATION.md — runtime-rebuild 编排文档（计划完成的强制产物，派发唯一依据）

> 三者边界（各自职责，不互抄）：`specs/orchestration/spec.md` = 可验收的**编排契约**（条款/场景）；`design.md` = **静态决策**（D1 责任、D2 子域表、D4 复用、D7 并发写域、D8 三级验收）；本文件 = **怎么驱动 + 当场账**。
> 本文件由集成者单写；每次派发前重读对齐，单完/波后回填台账。无凭据摘要不得在台账写「完成」。
> 本文件**不是** openspec artifact，openspec CLI 不跟踪它；它的存在不改变 CLI 的 4/4 判定。

## 授权边界（当前波：阶段二产品实施；2026-10-13 用户确认 00.P0.3 门（/openspec-apply 继续推进）后解锁，取代骨架波口径）

- 当前授权：**阶段二 65 项 NN.M 产品实施**（真实逻辑＋集成测试，验证=点名用例 go test -count=10 -json 真实 pass）；00.P0.3 门已过（三道齐备，skeleton-review.md）。骨架签名是冻结起点，改动须回写 contracts 并闭合影响面。
- 允许写：各 P0 单声明的**本包文件组 + skeleton_assert.go**（白名单精确到文件，见各域 tasks）；`openspec/changes/runtime-rebuild/**`（规划维护）；检查器/CI 资产仅随 00.4–00.6 或主编排者裁决动。
- 持续禁止：主 `openspec/specs/`、历史 wiki、prototype、用户文件（含 interview skill）、依赖改动、付费/微信调用、提交/推送/归档（提交须用户明示）；骨架单不勾自己的任务（勾选权在主编排者，凭四查证据）。
- 允许运行：`go build/vet/gofmt -l`（骨架验证）、检查器自测/契约模式、OpenSpec 校验。
- 外部批准计划 `/Users/pengweiye/Library/Application Support/Qoder/SharedClientCache/cache/plans/运行时_重设_实施计划_304e54c3.md` **只读**；外部计划原编号/行为/范围不得改。

## 0、结论（先行）

- **抵达 DoD 的唯一路径**：阶段一 P0 骨架波→00.P0.3 评审门→阶段二 NN.M 实施（已完成一批域内机制）→ **M1–M5 纵向收拢（中期复审修订，先闭合正式入口链路再续扩展）** → 每个域全场景＋跨域接线 → 09.1 正式入口真实观测对 golden → 09.3 两 module 全量门禁 → 09.5 真实资源逐项 → 09.6 闭合。任一环缺凭据即不勾选。
- **当前最大风险/瓶颈（2026-10-15 中期复审裁定）**：**正式消费者断链**——域机制成立 ≠ 叶任务完成；已勾叶中 31 叶缺正式入口接线（23 补差＋8 M 回归，见 `midterm-review-response.md`），台账 done 52/103。就绪面＝{01.4, 02.3, 03.3}，正处 M1/M2 起点。次风险仍是「测试绿」被误读为「产品成立」——五查第五查（生产消费者实测）自本轮起为硬门。
- **本轮实际完成的只是规划与检查工装**，且**规划/检查工装完成记录与产品完成记录严格分开**：检查器自测通过只证明检查器，不证明产品、不勾选任何叶任务。
- **对照索引**：目标—KR 见 [expectations.md](expectations.md)；锁定框架原生复用依据见 [trpc-reuse.md](trpc-reuse.md)（单点，不互抄）；二者随 00.3 审定接入 G1。

## 1、编排四问

### 次序（就绪队列到孙任务粒度）

- **派发原子 = 就绪叶任务集合**（§design D7 / orchestration spec「隔离并发与新语义优先」）；域只是**边界与账目单位**，不是调度单位，不整域派发。
- **就绪判据**：该叶任务全部 `depends_on` 已在集成树落地（非「同波」）；设计闸 G1/G2/G3 覆盖该项；本单资源锁未被占用。
- **恢复实施优先序（M 波，2026-10-15 修订；图边仍为硬前置，M 波是就绪后的派发优先序）**：
  - **M1 安全与唯一入口**（01.4、01.5、02.3、02.6、04.5、04.1-准入、08.1-auth）：正式治理注入、统一工具准入、审批裁决接线、HTTP 认证；产出＝正式入口错误路径走不通。
  - **M2 执行与恢复闭环**（02.5、02.8、02.9、03.3）：Receipt/异步观察、Spec+Handoff 自包含提交、mailbox 同协议、真实排空与退役。
  - **M3 唯一请求边界**（01.6、03.4、06.2、04.1-正文）：prompt/skill/历史/预算/安全快照进同一实际请求；删 ProjectedEntry 镜像（D1）。
  - **M4 派生读面与观察链**（05.1、05.2、05.3、06.1、06.3、06.4、03.7、04.7、07.4）：Query/RL/OTel/后验/副本消费真实事实。
  - **M5 剩余功能与 09 验收**（03.8、04.2、04.4、04.8、04.9、05.4、06.5–06.8、07.1、08.2–08.5、09.1–09.6）。
  - **M 回归重验（8 叶）**：02.7←02.6、03.5/03.6←03.3/03.4、04.3←04.1、04.6←04.5/03.3、07.2←03.3/01.6、07.3←05.3、09.2←03.3——上游收口即跑 `midterm-review-response.md` §四 回归锚，绿后重新勾选。
  - **共享写簇约束**：`run.go`/`prepare.go`/`agent/loop.go`/`task/wrapper.go` 消费面在 M1/M2 内**单写者或串行**（外部计划 §8：共享接线优先一单纵向闭环，不按域强拆）；同波其余单按触碰集不相交准入。
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

- **完成度五查**（勾选真实性 / 产物盘点 / 数字溯源 / 遗漏检测 / **生产消费者实测**）**按派发单核**；未完成者归因后修计划再重派。
- **生产消费者实测（第五查，2026-10-15 增）**：完成证据必须含**正式公共入口**（Assemble/Invoke/Publish/Close/Query、SDK/CLI）的调用路径与**负向反例**（错误路径走不通）；测试手工补接线（Declare/Approver/Recorder/保护句柄等）不构成生产消费者；evidence 必附验收命令原文＋原始退出码＋正式入口路径。
- **机械拦截（2026-10-15 增，返工止于单内）**：`check_runtime_plan` 新增测试锚两规则——①点名测试必须在仓库 `*_test.go` 真实存在（拦空转/改名漂移，Roundtrip 事故类）；②verification 承诺的点名测试必须全部被 evidence.commands 点名（拦只跑一半）；与既有对齐规则（exit_codes↔commands）构成完成证据三规则。23 个 M 补差叶的负向锚已具名化并写入 verification（`midterm-review-response.md` §二·1），**负向测试不存在或未跑则检查器红、无法勾选**。CI 常驻 `check_runtime_architecture --implementation`。每单完成后必跑 `check_runtime_plan`，红即偏离、就地归因。
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
| **骨架层 A（apply 波，2026-10-13）** | 通过（5/5：01.P0.1/02.P0.1/02.P0.3/04.P0.2/09.P0.1，evidence: skeletonA-*） | build/vet=0、gofmt 空、comment_policy 0 findings、plan ok、strict=0（主编排者独立复跑） | ①设计闸 00.1–00.3 闭合（601 条归属核验＋103 空洞三类裁决清零＋G2 五链推演 15 回填 2 待核）；②02.P0.1 曾受阻（contracts 无常量枚举）→ 裁决 §1.1 常量集冻结口径（8 Type+7 MetaKey+meta_ 前缀，值沿用遗留，task_settled 升格）；③comment_policy 索引行补齐（6 文件，锚点 docs/wiki 既有章节）；④hygiene 登记 golden 顶层；⑤tests/acceptance 3 findings 登记 W02.1（锚点不硬凑） | 解锁层 B：02.P0.2/03.P0.1/04.P0.3/05.P0.2 | 00.P0.3 门依赖 21 项，剩 16 |
| **骨架层 B（apply 波，2026-10-13）** | 通过（5/5：02.P0.2/03.P0.1/04.P0.3/05.P0.2/06.P0.1，evidence: skeletonB-*） | build/vet/gofmt/comment_policy/hygiene/strict 全 0（主编排者独立复跑）；层B 新增 1612 行 | ①并发写竞态 3 起均按 R21 处置（包内隔离取证+终态复验）；②Write 工具尾部 package 注入 4 起（R15 实例，代理均已自修复）；③telemetry 锚点裁决：保留 #telemetry-ladder，语义 Godoc 自述，00.P0.3 评审门复核 | 解锁层 C：01.P0.2/02.P0.4/03.P0.2/04.P0.1/04.P0.4/06.P0.2/07.P0.1 | 门剩 11 |
| **骨架层 C（apply 波）** | 通过（5/5：01.P0.2/03.P0.2/04.P0.1/04.P0.4/06.P0.2，evidence: skeletonC-*） | 全套验证 0（主编排者独立复跑） | ①C1 受阻（golden 要求人工批准 state→task 边）→ 主编排者裁 (A) 批准并更新 golden（reason 注明批准链）＋NoticePort 不引入接口类型（元规则二）后重派通过；②memory/search 子包登记 golden（packages+edges+根 allow 三处） | 解锁层 D | 门剩 6 |
| **骨架层 D（apply 波）** | 通过（3/3：02.P0.4/05.P0.1/07.P0.1，evidence: skeletonD-*） | 全套验证 0 | agent/query/evolution 三包 golden 均已预登记无阻塞；query 包无端口接口（纯承载类型，方法形状断言） | 解锁层 E | 门剩 3 |
| **骨架层 E+F（apply 波）** | 通过（3/3：07.P0.2/08.P0.1/08.P0.2，evidence: skeletonE-*/skeletonF-*） | 全套验证 0；架构 **implementation 模式首次全绿**（violations=0） | ①Deps 5 字段版获主编排者裁定保留（4 跨边界端口=组合根唯一 DI 职责）；②golden 补 memory/search 三处登记 | **00.P0.3 门依赖 21/21 done** | 0 |
| **00.P0.3 骨架评审门** | 第一道 CodeReview=有条件通过（无 P1、4 P2）；第二道主编排者复核=P2 全处置（P2-1 改锚/P2-2 接线 RecallResult/P2-3 修订 D9 口径/P2-4 contracts §7 留痕），处置后 build/vet/policy/strict 全 0 | 评审记录=skeleton-review.md；风险预告 7 项归档为阶段二定向核对表 | **第三道用户确认待决——确认后勾选 00.P0.3 并解锁阶段二 65 项** | — | — |
| **风险修复轮（用户裁定"值得都修"）** | 7/7 修复（#1 ctx 全补/#2 plugin 六窄接口+注册单元/#3 Notice.Event/#4 Deps+3/#5 AdmissionKind/#6 BindingHandle/#7 词表）；批1 回传 forward 失败但按 R6 先验工作区确认全部落盘 | build/vet/gofmt/comment_policy 16包/arch impl/plan/strict 全 0（主编排者独立复跑） | contracts §7 销 2 条（plugin 缺口/Borrow 释放面）；skeleton-review.md 记修复轮 | — | — |
| **00.2 裁决登记 + 骨架符合预期确认** | 裁决登记落盘 adjudications-00.2.md（103 空洞三类裁决/9 张力/17 对位实证，独立存档）；骨架对照 expectations 五 O 载体逐项实证（O1 真别名18处/O2 三对象+BindingHandle+AcceptNotice/O3 AuthorizationScope+Snapshot/O4 logprobs+封账/O5 Deps+Runtime+HostBridge）——**骨架符合预期** | 同上全套 0 | 评审门第三道用户确认仍待决（本行确认=骨架载体符合预期，非门的第三道） | — | — |
| **阶段二波 1（00.P0.3 门确认后）** | 门三道齐备勾选（第三道=用户 /openspec-apply 继续推进）；00.4/00.5 实质达标核验勾选；01.1/02.1/02.4 产品实施（三单并发） | 各点名用例 -count=10 独立复跑：281/61/121 pass、0 fail；全套 0 | ①首派三单全 forward 失败（基础设施）→ R6 验工作区确认无产物→重派成功；②02.1 幂等键口径合流（身份=(TaskID,MutationID)，内容=rev/status/outbox——按合同"同身份异内容拒绝"采纳）；③00.5 自测断言过时（19→20 包，R23 修断言非产品） | 解锁波 2 | 34/89 |
| **阶段二波 2** | 01.2/02.2/03.1 产品实施（三单并发：state/task/memory 写域不相交） | TestSessionAuthority 61 / TestTaskLifecycle 81 / TestOriginalReferenceIsolation 71 pass、0 fail（主编排者独立复跑）；全套 0 | ①01.2 上报 Notice 无目标参数→TrackDelivery 显式登记方案采纳（登记即授权、fail-closed），"目标并入通知身份"留 contracts §7 待核；②02.2 Resume 幂等先于状态机拒绝（自修复）；③03.1 Guard holder=ref.Owner 口径采纳 | 解锁波 3：01.3/01.4/02.3/03.2 | 37/89 |
| **阶段二波 3** | 01.3+01.4 合单（同写 state/）∥ 02.3 ∥ 03.2 ∥ 00.6（四线并发） | 四点名用例独立复跑 41/41/51/51 pass、0 fail（-race）；全套 0；workflow 自测 15 tests OK 含双负控 | ①01.4 删 skeleton_assert 中 Release stub（真实实现替代）越白名单——主编排者追认；②00.6 上报 hygiene 漏登 5 顶层目录→主编排者补登+去重后 hygiene=0；③03.2 file 档具名 Skip（rustviking 缺，零 mock，归 09.5）；④02.3 Denied 配对结果走 ResultRecorder 事件回流（拒绝无 TaskID 不走 Notice）——采纳留痕；⑤bot 零步骤（08.2 后补，workflow 头注已记） | 解锁波 4 | 40/89 |
| **报告响应修订轮（2026-10-14）** | 用户提供 docs/.dev/REPORT.md 高质量骨架评估（26/89 时点）→ 主编排者逐条甄别（report-response.md：终局归约已被 02.4 解决、Handoff 部分解决，其余 9 项合同缺口+3 项规范矛盾成立）→ 计划调整落盘：task-graph +14 修订任务（00.7-00.20，[R1]×5/[R2]×6/[R3]×3，批次串行批内并行）；剩余 NN.M 三档加批次尾前置（01.5/02.x→00.11、03.4+/05.x/06.x→00.17、07+/08+/09+→00.20）；3 项规范矛盾裁决修正（persistent-event-loop 措辞/causal fallback fail-closed+迁移域限定/storage-lifecycle L0-L3 口径）＋adjudications 计数修正＋G1 编译门≠原生对齐口径 | plan ok（103/103）＋strict 0＋自测 OK（fixture 同步扩 00 域 20） | 波 4 三单（01.5/02.5/03.3）被用户取消派发——正确：先合同闭合再实施（报告 §七采纳） | **R1 批次（00.7-00.11）就绪为下一波** | 40/103 |

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


### 批次 8（R1 修订轮：报告响应合同缺口第一批；两波四单＋一次重派；主代理亲自复验全部凭据）

> 覆盖 00.7–00.11（[R1.1–R1.5]）。裁决依据：report-response.md §二/§三。进度 40→45/103。D2 空返留半成品（仅 sentinel）→重派闭合（R6 实证：先验工作区再定重做范围）；重派 forward 100400 但工作已落盘→四查全部由主编排者亲自执行（无代理战报）。

| 单 | 叶任务 | 写域（实证不相交） | 凭据摘要（复跑命令+exit 0） |
|---|---|---|---|
| D1 | 00.7 | model/terminal.go（纯 Godoc +94/−24，代码行零变化机器证明） | TestModelTerminalProtocol 11 子项 PASS 零改动；spec 措辞核验零残留 |
| D3（合单） | 00.9+00.10 | event/types.go、event/skeleton_assert.go、host_bridge.go、task/{spec,commit,hold,wrapper,lifecycle,cancel,skeleton_assert}.go＋execution 三测试级联 | 三测 22 PASS（含新增 approval resolves/mismatched refused/freezes the notification target 用例）；state 回归 ok |
| D2（重派） | 00.8 | state/binding.go、state/skeleton_assert.go | 三测 3 PASS 零改动；BindingRef×15/BorrowDerived×5；协议 Godoc 三条（排队保有/换代不移动/恢复具名拒绝） |
| D4 | 00.11 | plugin/phases.go、plugin/skeleton_assert.go | TestNativeToolAdmission 7 PASS 零改动；TransformCall×8；comment_policy 0 findings |

编排者裁决与修复（本批）：
- **ApprovalDecision 实位于根包 host_bridge.go 而非计划 W 行所写 plugin/governance.go**——按实际落点授权（W 行为候选写集，不改行文本）。
- **00.9 与 00.11 原判"五单互不相交"有误**（ApprovalDecision 若入 phases.go 则相撞）；实证定位后合 D3 消除 task 同文件簇（lifecycle.go 双触）。
- **D1 申报项 2 采纳并亲自修复**：aliases.go 包注释 "no behaviour of its own" 与 02.4 Reducer 行为矛盾→改写为"别名面零包装；包内唯一自有行为=请求级终局归约，见 terminal.go"（越单小修，TestModelTerminalProtocol 回归 ok）。
- **D4 申报项 1 裁决：采纳 TransformCall(ctx, *ToolCall) 签名**（不带 identity 形参）——身份约束（不得改 CallID/可信作用域）以 Godoc 固化；避免新增 plugin→task 包依赖边（分层：plugin 为被消费面）。留痕：若终审要求显式 identity，须先裁包依赖边。
- **勾选格式事故与修复**：主编排者误用 done:true 非标字段（检查器红、tasks.md 三行 checked-without-evidence）→按既有格式 status:'done'+evidence{run_id,commands,exit_codes,artifact_paths} 重写五单；既有 40 项 done 无损（虚惊源于错字段计数）。
- check_runtime_plan RESULT: ok（45/103）；go build ./... 全绿；openspec 工件未越权（四单代理零触碰 openspec）。


### 批次 9（R2/R3 修订轮：合同缺口第二三批；六单＋一补单；进度 45→54/103）

| 单 | 叶任务 | 写域 | 凭据摘要 |
|---|---|---|---|
| D5（重派） | 00.12+00.13+00.15 | memory/{reference,guard,guard_memory,segment_store,skeleton_assert}.go＋history 测试级联 | 三子用例 PASS（作用域外 Missing 不泄露/幂等 lease 迟到旧 lease 具名拒绝/字节分段精确重组）；冻结文件零字节改动（scoped-reader 并存迁移） |
| D6 | 00.14 | memory/search/*＋新测试 | TestSearchQueryContract 3 子项 PASS；空 Text=ErrQueryTextMissing（依据 spec recall-protocol）；DefaultLimit=20 无据注明 |
| D7 | 00.16 | state/{session,mailbox,skeleton_assert}.go＋测试 | 16 PASS 含新增回溯/前缀用例；Consumed 连续前缀行为修正=语义级联 |
| D8 | 00.17 | query/*.go | Cursor 编译级不透明/水位三态/Update 四操作/逐义务推进/NextAdvancer 删除（报告明示，包外零引用） |
| D9 | 00.18 | rl/*.go | 四项证据合同（SnapshotRef/逐文件封账/实例版本/精确来源）编译断言实存 |
| D10 | 00.19 | evolution/*.go | GateReport 四要素/后验四态独立/RefineRecord 接回编译断言实存 |
| D11 | 00.20 | 根包 tagent/host_bridge/build/skeleton_assert | Bridge() 候选（ErrBridgeNotAssembled）/待投递有界读面/ModelFactory 零实现；10 PASS 全回归 |
| D12（补单） | 00.15 workspace 同型 | workspace/{externalize,skeleton_assert}.go | 复用 memory.SegmentRequest/Result；行数形态零残留 |

编排者裁决与修复（本批）：
- **派发令白名单两处漏列定义文件**（00.16 的 session.go、00.12/00.15 的 original_store/localfile 冻结实现）→ 代理均按计划 W 列/并存迁移正确处置并申报，追认；教训：派发前以类型定义文件实位核白名单，不凭包名。
- **D5 首派被取消经 R6 验零半成品后重派**（refScope=既有去重作用域勿与读作用域混淆）；**D12 补单闭合 00.15 workspace 欠账**（R18 悬置即清）。
- **并行编译互视实证**：D12 两次撞 rl/evolution 中间态红→60s 重试转绿（R21 重试条款生效）；后续同包簇仍须合单或串行。
- **勾选格式沿用 status:'done'+evidence 四字段**（批次 8 事故教训固化）。
- check_runtime_plan RESULT: ok（54/103）；build/vet 0；集成测试 config/execution/history/state 全 ok。


### 批次 10（NN.M 纵向实施波；进度 54→64/103）

| 波 | 单 | 叶 | 写域 | 凭据摘要 |
|---|---|---|---|---|
| 波1 | D13 | 01.5 | state/{publish,baseline,retire}+测试 | TestGenerationPublication -race -count=10（80/0）；G2 六+G1 独立提交证据 |
| 波2 | D14∥D15 | 02.5∥03.3 | run.go+agent/loop ∥ task/restore+state/durable+memory/replay | RoundTrip 40/0（**测试名 Roundtrip→RoundTrip 由编排者对齐验收命令原文防空转**）；DurableTaskRoundTrip 60/0 |
| 波3 | D16∥D17∥D18 | 05.1∥06.1∥07.4 | query/inventory ∥ rl/+loop.go 生产点 ∥ evolution/structural+policy | 三单各 70/0；07.4 结构门本体（真模型评估留 09.5 资源门） |
| 波4 | D19∥D20∥D21 | 01.6∥02.6∥03.4+03.5 | state/{filesource,channels,errors} ∥ plugin/{phases,present}+loop.go 插线 ∥ memory/{projection,replay_budget,retention,lifecycle*}+agent/request_assembly | 80/80/140/0；G2 七+四+十二子用例全绿 |

编排者裁决与修复（本批）：
- **02.5 验收口径事故**：代理测试名驼峰化 Roundtrip 与验收命令 `^TestNativeConversationRoundTrip$` 零匹配（空转假绿）→编排者改测试名对齐计划原文（R24 反面）；此后验收以"验收命令原文跑通且非空转"为准。
- **02.6 TransformCall 相位归组**＝PhaseRequestEnrich 追认（两可，语义合同已在接口 Godoc；D 级裁决留 contracts 回写定稿）；**After 呈现分期上浮 05.3** 追认。
- **06.1 AttemptRecord 增 Usage/Error**＝落实 00.3 审定文"不遗漏实际生产字段"非演进，追认；contracts 回写入收尾账。
- **07.4 探针证据模型**（结构见证非真实评估）追认；ApplyGate 缺证据=reject 遵 gate.go 冻结面。
- **03.5 Retention 包装 Guard 不立第二真源**（元规则二）；03.4 boundary 回种＝R23 产品缺陷修复。
- 派发令白名单教训固化：以类型定义文件实位核白名单（01.5/01.6 均按计划 W 列执行零事故）。
- 全程 check_runtime_plan RESULT: ok；build/vet/gofmt 0；七集成包全 ok。


### 批次 11（波 5 四单派发遇余额阻塞；进度 64→66/103；**实施暂停待续**）

- 波 5（02.7∥04.1∥05.2∥07.2）派发回传报 **Insufficient account balance**——但四代理实际均已执行（同 D2 先例：回传失败≠未执行）。主编排者逐单独立验收：
| 单 | 状态 | 凭据 |
|---|---|---|
| 02.7 | **完成勾选** | TestDelegationCompletion -count=10 40/0；delegate.go +485 |
| 07.2 | **完成勾选** | TestGitNativeRefine -count=10 80/0；refine_git.go 509；临时 git 仓库不动本仓 |
| 04.1 | **半成品挂起** | native_tools_test 三子用例 FAIL（File tool 未真写/skill_load 占位返回"answered with string"疑 stub 未接真）——待续作修复单 |
| 05.2 | **半成品挂起** | query/snapshot+subscribe 修订已落（gap/reset 19 命中）但 05.2 验收测试未建——R24 有门缺凭据不勾，待续作补测试 |
- **阻塞线**：子代理派发需配额，余额耗尽。恢复后优先重派 04.1 修复单＋05.2 测试补单，再续波 5b（03.6/06.2/09.2）与 04.x/05.3/06.x/07.x/08.x 剩余。
- 工作区状态：build/vet 0、七集成包中六 ok（capabilities 红=04.1 半成品所致）、check_runtime_plan ok。


### 批次 12（余额恢复续派：两续作单＋四实施单＋一架构修复单；进度 66→72/103）

| 单 | 叶 | 凭据摘要 |
|---|---|---|
| D22 续作 | 04.1 修复 | TestNativeToolsAndSkillState 40/0；三断点归因（测试漏 prompt FileRef/断言笔误 5→4/回流 %T 占位）追认 |
| D23 续作 | 05.2 测试补单 | TestQueryAuthorizationAndCursor 130/0（13 子用例）；Follow 读面补齐=半成品缺口（reading.delta 死字段清理）追认 |
| D24 | 03.6 | TestReplicaMaterialization 70/0；blob 去重≠引用去重；segment_reader 并入 replica.go（白名单优先候选）追认 |
| D25 | 06.2 | TestCaptureFidelityAndRedaction 120/0（11 子用例）；secrets 不落盘/SnapshotFromOptions 恒拒/logprobs 三态 |
| D26 | 04.5 | TestGovernanceBeforeExecution 110/0（9 子用例+恰一次）；Hold≠Denied 恒 nil Denial；Check→Lookup 更名四态词未改 |
| D27 | 09.2 | TestOfflineMigration 51/0；源摘要不变/6 gaps 具名/中断幂等/回退不伪 restore；台账 blocked 引文系规划期旧登记，实施期已解锁（派发令优先）追认 |
| **D28 架构修复** | （R8） | **agent→rl 禁边消除**：agent.Recorder consumer 侧散参面＋rl.RecorderAdapter 零边适配＋根包跨包满足断言；check_runtime_architecture RESULT: ok；learning 两测试断言零改动（语义零变化回归证明）。裁决记录：rl→agent 同为禁边，独立小包被"不新建未登记包"封死，取 rl 侧零边适配器主路径 |

环境既有红（非单责，入收尾账）：lint.sh wechat-bot 子模块 build、comment_policy ratchet 354、gen_godoc docs/api 漂移。


### 批次 13（三实施单；进度 72→75/103）

| 单 | 叶 | 凭据摘要 |
|---|---|---|
| D29 | 02.8 | TestHandoffFutureNotifications 70/0（G2 六子用例）；双目标语义/关闭未交付保留/稳定 seq；Handoff 义务转移 vs 路由冻结裁决（golden S05） |
| D30 | 04.3 | TestMCPProtocolLifecycle 110/0（10 子用例）；框架 MCP 面 8 点复用零重写；固定网关不随注册表漂移；trpc-mcp-go 锁定间接依赖未动 go.mod |
| D31 | 05.3 | TestObservationDoesNotOwnState 60/0（S1-S5）；关闭 OTel 不影响 Query 行为+静态双证；TestRealCollectorDelivery 资源门合法 Skip 留 09.5 |


### 批次 14（四实施单＋两只读探针解阻；进度 75→79/103）

| 单 | 叶 | 凭据摘要 |
|---|---|---|
| D32 | 02.9 | TestCloseDrainsObligations 60/0（G2 五子用例）；三段序列/非合作 producer 不伪结束/重复 Close 幂等 |
| D33 | 03.7 | TestReplicaCleanup 60/0（G2 五子用例）；活跃保护/不删用户文件/不跨链接越界；暂存件跳过=产品缺陷修复 |
| D34 | 06.3 | TestCaptureAdmissionAndSeal 110/0（11 条目）；三上界准入/丢失账/同 protocol 封账/真 fsync；writer.go 拆分追认 |
| D35 | 07.3 | TestPostActivationEvidence 110/0（10 子测）；窗口锚=首次真实消费/judge 保守/四态；TestRealLLMJudgeArtifact 资源门留 09.5 |
| 探针×2 | 04.6/04.7 解阻 | 只读核实→裁决落盘 `changes/04-capabilities/unblock-04.6-04.7.md`。04.6：真源=agent/governance@git 214f5d9（prototype 零命中系指认更正）、状态词四态沿用+expired 派生态+epoch 防刷限、五冲突点裁决（事件 subtype 从新/其余沿用）。04.7：时间/类型薄映射做、关系轴自证不实施、融合限两值、embedder 留 09.5、**QueryEvents 承载断层归 04.7 实施面**、memory/recall.go 独占划界。04.6/04.7 状态 design-blocked→planned |


### 批次 15（04.6/04.7 解阻后实施＋06.4/08.1；进度 79→83/103）

| 单 | 叶 | 凭据摘要 |
|---|---|---|
| D36 | 04.6 | TestGovernanceRecovery 70/0（6 子用例）；状态词四态逐字沿用对照表/epoch 三守卫/拒绝双件+GoalID 填充/事件回放+seq 对齐；裁决序取旧序 A 追认 |
| D37 | 04.7（重派） | TestRecallConsumer 170/0（12 子用例）；QueryEvents 读面+薄映射不 widen+RRF 两值逐字节降级+embedder 缝；Relations 登记不实施；coverage 注记已同步 |
| D38 | 06.4 | TestAuthorizedTrainingExport 160/0（15 子用例）；授权先于读取/derive-on-change/两账分开/manifest 对齐 00.18 |
| D39 | 08.1 | TestPublicRuntimeEntry 50/0（4 子用例）；唯一 Assemble 四面/CLI 薄壳/Close 两段式；run.go 626 行超 500 纪律申报追认 |

- 04.7 首派 forward 100400 零产物→重派闭合（R6）；测试落点 tests/integration/host/ 新包（08.1）后全量=十包。
- 就绪面：04.2/04.8/04.9/06.5/06.7（下波）。

### 批次 16（中期复审修订轮；进度 83→52/103——31 叶重开：23 补差＋8 M 回归）

> 依据：用户指令「重新修订计划」＋单次全局 CodeReview（主编排者逐项核实：静态调用链＋五组现有测试 `-race -count=1` 复跑全绿）。裁决单点：`midterm-review-response.md`（补差合同/负向锚/M 波/待核项）。

| 项 | 内容 |
|---|---|
| 核心裁决 | 域机制成立≠叶任务完成；design D8/原计划 §10 的「生产消费者」口径自本轮起硬性执行（五查第五查） |
| 23 叶补差重开 | 01.4/01.5/01.6/02.3/02.5/02.6/02.8/02.9/03.3/03.4/03.7/04.1/04.5/04.7/05.1/05.2/05.3/06.1/06.2/06.3/06.4/07.4/08.1——缺正式入口接线（standGovernance 固定通过/Receipt 丢弃/Spec 不自包含/镜像接缝等，逐条见响应文档 §二） |
| 8 叶 M 回归重验 | 02.7/03.5/03.6/04.3/04.6/07.2/07.3/09.2——实现不判错；前置重开致 done-with-unfinished-dep 硬拦，上游收口后重跑 §四 回归锚再勾选 |
| 实施优先序 | M1 安全与唯一入口→M2 执行与恢复闭环→M3 唯一请求边界→M4 派生读面与观察链→M5 剩余功能与 09 验收；M1/M2 共享写簇（run.go/prepare.go/agent/loop.go/task/wrapper.go）单写者或串行 |
| 域内实现 | 保留不推倒（单归约/scoped 读取/冻结路由/缺口语义等复审认定的实质改进），补差只做正式入口接线与负向反例 |
| 校验 | check_runtime_plan ok（103/103 计数不变）；done 52；就绪面 {01.4, 02.3, 03.3}=M1/M2 起点；勾选↔status 一致性由检查器核 |

### 批次 17（D-M1 波：01.4+02.3+03.3 共享写簇合单＋2 M 回归重验；进度 52→57/103）

> 合单依据：三叶皆触 run.go 正式根接线簇（R28 触碰同一文件簇→合单；外部计划 §8 共享接线优先一单纵向闭环）。**首次执行 R30 波次收尾偏差评估**（独立验证子代理，与实施者分离）。

| 项 | 内容 |
|---|---|
| D-M1 实施 | 01.4 治理注入（standGovernance 清除/ErrGovernanceMissing）＋Publisher.Borrow 退役推进＋Query 读源随 r.current；02.3 三族统一 task.Wrap 受理＋WithApprover（strict 无 approver=拒）；03.3 Register 自包含 Spec（specDeclarer 结构口，冻结 Commit 端口未扩）＋Restore 缺 Spec 具名拒绝＋WithStorage 持久档装配 |
| 六冻结命令 | 40/70/60/40/30/30 全绿（-count=10，编排者亲跑与战报逐一对上）；全回归 10 包 ok |
| M 回归重验 | 03.5（TestRecoveryBeforeReclamation 70/0）、09.2（TestOfflineMigration 50/0）——上游 03.3 收口后锚绿，零适配，实现不判错即勾 |
| R30 偏差评估 | 五面核查：1-4 达标（计数结构交叉核对佐证）；偏差 4 条——①M 回归命令包错位（TestOfflineMigration 在 acceptance 非 history，评估指令的合并命令会零匹配假绿，评估代理抓出）→修正分包实跑；②responsibility-fragmentation +4 **本波引入**（负锚与既有文件共享契约锚）→如实记账，随 comment_policy 收尾账（预存 ~613）统一收敛裁决，**不做基线上调**；③Assemble Option 签名契约回写→已入 contracts.md（编排者写面）；④deny 子测注释"every file, skill and function"声明过宽（实驱动 save_file）→收窄注释/子测名（deny 路径三族共享，负锚合同本质已覆盖） |
| 过程教训（R19 变体） | 编排者勾选脚本语法错（02.3 条目缺 `]`）＋heredoc 多脚本以换行串联（非 &&）→tasks.md 已勾而 graph 未标的半程状态照走检查器——**勾选↔status 一致门当场拦红**，补跑修正后转绿；教训：批量账目脚本单条命令内跑完（&& 串联或单脚本），半程状态必被门抓 |
| 就绪面 | 01.5（M1 收尾）+ 02.5（M2 主干）；03.4←02.5、04.6←04.5、07.2←01.6 等待中 |

### 批次 18（D-M2 波：01.5+02.5 补差＋D-C1 跨叶级联；进度 57→59/103）

| 项 | 内容 |
|---|---|
| 01.5 补差 | TestFormalPublishRefusesMissingEvidence 10/10（缺 eval/零裁决均 ErrRequiredCheckMissing 拒、旧代不变、代记账证无新记录、变异负控必红）；state/publish.go 零改动（无缺口，evaluate 先于 commitRecord） |
| 02.5 补差（M2 主干） | 六项全落：Receipt 入 transcript／后台结果独立观察回流（role=user+ToolID 保真，不冒充第二次同步）／受理循环不被 await 阻塞／回流走 mailbox 接纳（DetailRetired 记账）／HistoryFace 共享会话历史／answerText 原样透传。正锚 41 pass 级联增强＋负锚 TestReceiptAndBackgroundStaySeparate 11 pass |
| D-C1 跨叶级联（R8 裁决） | 02.5 合同闭环后 4 组他叶测试锁旧配对语义（02.3 负锚/04.1/02.6/08.1）→纯测试断言改形（receipt+observation 范式），强度不降；native_tools 子测 3 场景重排（save 移独立 decision——同 decision 后台并发使旧串行假设不可确定，断言意图满强度保留）；全回归十包转绿 |
| R30 偏差评估（第二轮） | 有条件勾选→两条件均已 discharge：①编排者按清单复跑 4 组全绿＋git 白名单零越界（go.mod/state/publish.go 零 diff）；②计划回写登记 ×2——**golden S03 model_message_roles 期望值过时**（与双 call receipts 及新 observation 范式不自洽，S03 验收前必须修订）入 09.1 notes；**roleOf/observationOf 角色双轨**（TypeTaskSettled→RoleTool vs settled→RoleUser，现无实路径触发）随 03.4 transcript 装配收口统一，入 03.4 notes。裁断记录：role=user 观察（ToolID 保真）符合 contracts"不冒充第二次同步 tool response"；01.5 代记账证明可接受；D-C1 场景重排未偷换 04.1 G2 |
| 过程教训 | 波 18 首次勾选脚本半程态（02.3 条目缺 `]`＋heredoc 换行串联）被勾选↔status 门拦红——批次 17 已记；本次勾选改单脚本一次跑完，零复发 |
| 就绪面 | 01.6/02.6/03.4/05.1/06.1/07.4（六叶）；派发分组：07.4（evolution 独立簇）∥ 05.1（query+host 测试）∥ 02.6（agent/loop+execution 测试）先行，01.6/03.4/06.1（loop/run 共享簇）下波串行 |

### 批次 19（D-M3 波：02.6+05.1+07.4 三单并行＋02.7 M 回归；进度 59→63/103）

| 项 | 内容 |
|---|---|
| 02.6 补差 | pass.Risk/CallPass 经 WithPolicy→policyRuling 接正式裁决（占 WithApprover 座位，无 policy 行为逐字节不变）；After 产物经 PresentationSink→Run.Presentations() 正式读出。负锚 TestFormalLoopEnforcesPolicy 30/0（变异负控：摘 CallPass 必红）；正锚 80/0 |
| 05.1 补差 | query 六维保真（AgentState 三读数/SessionState 四读数/Decision/原生态 TaskStatus/NoticePhase 四相/Holder），DetailState 可比结构体，DetailRetired 留 consumed 锚。负锚 TestQueryShowsPublishedAgentState 40/0；正锚 80/0 |
| 07.4 补差 | 判据载体=Tools 的 EvaluationCriterionRef（Eval 比 expect/Benchmark 比 baseline，TrimSpace 真机判）；缺判据=insufficient、不符=reject（内容错误但 Completed 不过）；ApplyGate 消费 evidenceRecord 零驱动。负锚 TestEvalRejectsWrongContentCompletion 40/0；正锚 80/0 |
| 02.7 M 回归 | 上游 02.6 收口后 TestDelegationCompletion 40/0 零适配即勾 |
| R30 偏差评估（第三轮） | 有条件勾选→条件全 discharge：W1 勾选锚名勘误（级联实锚=TestQueryAuthorizationAndCursor S5，已按实写 evidence）；W2 集成提示"组合根解析 Tools SHALL 跳过 EvaluationCriterionRef"已登记 contracts.md；S1 待核 §五.2（Query lease 授权归属）已随 scopeCovers 授权过滤收口、随 05.1 勾选注明；补证=编排者复跑六锚 30/80/80/40/80/40＋白名单核（openspec/go.mod 未动） |
| fragmentation 定性 | 21 条 responsibility-fragmentation＝规则-结构错配（集成测试目录无同目录生产镜像，规则三出口结构性不可用）——按外部计划 §9"评估会驱动碎片化的旧规则"归 00.6/09.3 lint 迁移以正反样例改规则；**不做 baseline 上调**（05.1 待裁以选项 D 结案） |
| 就绪面 | 01.6/02.8/03.4/04.1/04.5/05.2/06.1（七叶）；本波派发＝请求边界合单（01.6+03.4+04.1+06.1，run/loop 簇单写者）∥ 02.8（task 簇）；04.5/05.2 与合单同读 capabilities/inspection 测试包，下波 |

### 批次 20（D-M4 波：请求边界合单 4 叶＋02.8 补差＋D-C2 级联；进度 63→68/103）

| 项 | 内容 |
|---|---|
| D-M4 合单（01.6+04.1+03.4+06.1） | Source A/B/C 三源进真实请求：fileFixer 固化正文（system 前缀）/SkillLoads 技能正文去重/AssembleBudget 预算投影（RequestFace 注入缝，真消费 memory）；ProjectedEntry 镜像删除未再造（D1）；WithRecorder→RecorderAdapter＋Invoke.Episode＋RecordingLoss 显式账。四负锚各 10/10＋正锚级联净增强 |
| 02.8 补差 | Handoff 经冻结 Commit 端口持久 mutation（handoff:<slot>:<json>，slot 递增防冲突）；deliveryContext 归属取持久 Spec（重启/异运输稳定）。负锚 TestHandoffRoutingSurvivesRestart 10/10（双进程链+幂等） |
| D-C2 级联 | capabilities 两负锚随 system 前缀布局平移（索引+2/计数 8→10）＋system 正文断言——强度增强；D-M4 白名单外停手上报→D-C2 授权适配（纪律闭环实证） |
| R30 偏差评估（第四轮） | 有条件勾选→条件全 discharge：W1 注释勘误双处（"moves with the handoff"→义务重分类/冻结路由不动——含跨行折断字样的修复）；W2 PartitionID 查键/S2 ChainKey 同构 D1 灰区→登记 03.4 回写项；S1 handoff 持久编码→登记 contracts.md；双轨回写项勘误（注释边界落定取代"统一"）；补证=十包 ok＋六负锚顶层 40/10/20/20＋go.mod 零变更（评估代理会话级核） |
| 就绪面 | 04.5/05.2（下波两单：plugin/capabilities ∥ query/inspection）；其后 04.7/05.3/06.2 等 M3/M4 收尾 |

### 批次 21（D-M5 波：02.9+04.5+05.2 三单并行＋三笔 M 回归；进度 68→74/103）

| 项 | 内容 |
|---|---|
| 三笔 M 回归 | 03.6（TestReplicaMaterialization 70/0）、04.3（TestMCPProtocolLifecycle 110/0）、07.2（TestGitNativeRefine 80/0）——上游 03.4/04.1/01.6 收口后锚绿零适配即勾 |
| 02.9 补差 | 四类 producer（Invoke/排队/直接工具/模型流）登记 Shutdown（SetWatch+Track/Watch）；cancel≠停止（WithoutCancel 分离执行+真实停止确认）。负锚 TestCloseRefusesWhileModelInFlight 50/0 |
| 04.5 补差 | 正式路径 critical 恒 Hold（HoldReceipt 非 Denied 零执行）/strict 拒绝事实与原生参数一致（ToolID+ArgsDigest）——全正式链负锚 TestCriticalCallsHoldThroughFormalPath；裁决链复用 02.6 policyRuling，未违令改 run.go |
| 05.2 补差 | Budget{MaxObjects,MaxBytes}+DefaultBudget{4096,4MiB}+NewBoundedProjection；小页按需物化（countingSource 证上界）/超界具名截断拒绝/分页不破。负锚 TestSnapshotPagesWithinByteBudget |
| R30 偏差评估（第五轮） | 有条件勾选→凭证回填解除（P1 evidence 命令原文+退出码、P2 go.mod 零 diff+编排者 -count=1 抽验 PASS）。**D1 流程缺陷**：02.9 回传失败零战报、04.5 战报截断——产物验收面经评估代理直核成立（三负锚非恒真/正式链路/cancel≠停止/digest 一致），战报残缺记流程缺陷不阻塞技术验收；D3-D5 建议（正锚 harness 补强/Track 断言/Run.Query 预算直驱断言）留后续波 |
| 过程教训（复发） | 勾选脚本引号失衡＋heredoc 换行串联→tasks.md 已勾 graph 未标半程态再演，勾选↔status 门再次拦红——**批量账目脚本必须单条命令内跑完且语法先自检**（python -c 编译通过再跑），下轮起脚本先 bash -n/python 语法检 |
| 就绪面 | 03.7/04.2/04.6(M回归)/04.7/05.3/06.2/08.1（七叶）；本波派发＝03.7 ∥ 04.7 ∥ 04.2（workspace/memory/tool-exec 三簇不相交）＋04.6 回归即清；06.2/05.3/08.1 均触 run.go 簇，下波合单/串行 |

### 批次 22（D-M6 波：06.2+03.7 勾选、04.7 假产物事件重派；进度 75→77/103）

| 项 | 内容 |
|---|---|
| 06.2 补差 | 安全快照接 GenerateContent 前（RequestSnapshotFace+fixRequestSnapshot 紧贴调用点；rl.SnapshotRequest 脱敏投影不改写请求本体）。负锚 40/0＋正锚 121/0；R30 裁定可勾选 |
| 03.7 补差 | 活跃保护生命周期（WithCleaner 注入/Open 持保护/span-hold/evaluate 字节比对复用重定影/父级链接拒/TOCTOU/启动遗留暂存件）。负锚 10/10（91 events 八子场景）＋正锚 61 events＋03.6 回归 71 events 不破；**空返重派**后完成（前次转发失败零产物，R6 验工作区重派） |
| **04.7 假产物事件（重大流程缺陷，R15+战报幻觉复合）** | 实施战报详述"负锚 392 行落盘/41 pass/资源门 10 skip"——**实证为幻影**：测试文件不存在（ls/grep/no tests to run 三重实证）、memory 侧 5 文件真实落盘但零消费者（D2 属实）。R30 评估代理抓住（全仓零命中），我此前复验**合并计数误读**（^A$\|^B$ 命令只跑 B 得 210，被战报 41+171 误导为双锚皆在）——教训：**复验必须按测试名逐锚分跑计数，合并计数会掩盖零匹配空转**（恰是测试锚规则要拦的形态，但只拦勾选后不拦复验）。处置：04.7 判不可勾选，重派补差（补负锚+消费者闭环）；memory 侧产物保留（真实落盘）作为重派的已就绪资产 |
| D4 修复 | RequestSnapshots 浅拷贝 vs Godoc"copy the caller owns"——收窄 Godoc 为结构层副本（ContentParts 共享如实声明）；快照固定语义由 fix 点保证 |
| R30 偏差评估（第六轮） | 分单裁定：06.2/03.7 可勾选、04.7 不可勾选（D1 负锚缺失+D2 零消费者）。D3 台账回填随本批次；D5 docs/api 生成面漂移随收尾账 |
| 就绪面 | 04.2/05.3/08.1（补差）＋04.7 重派；05.3/08.1 均触 run.go 簇需合单或串行 |

### 批次 23（D-M8 波：04.7 补差重派+05.3+08.1 合单；进度 77→80/103）

| 项 | 内容 |
|---|---|
| 04.7 补差（假产物重派） | 负锚 TestSearchRecallsFromRealStore 70/0/10skip 真实落盘（写后校验三连留证）；真实 FileSegmentStore 语料→观察表→StoreSearcher 召回逐字节等价；消费者口径裁决=负锚即具名消费者（grep 实证唯一），生产接线归属 04.9+09.5 不留无主孤岛 |
| 05.3 补差 | observePlugin（commit-observer）挂正式装配消费 After 产物；facts 账本单一真源（单写点 record 双读点全 r.mu）；缺口具名可查。负锚 40/0＋正锚 60/0；资源门合法 Skip 不结项留 09.5 |
| 08.1 补差 | /query 与诊断端点认证（单门先于路由、/healthz 无旁路、常数时间比对、未配认证不开口）；读源统一核验零改动（D-M1 成果）。负锚 60/0＋正锚 50/0 |
| R30 偏差评估（第七轮，一次高风险误拒后重试成） | 五点核查全达标（负锚非恒真/事实单一真源/认证门无旁路/真实 store 链/白名单合规）→有条件勾选→两手续 discharge（逐锚分跑 evidence＋git 白名单核：memory/ 8 行 diff 定性为前波 guard.go 遗留，本波零写入） |
| 检查器实拦（测试锚规则） | 05.3 verification 的**否定性引用**（"不以 TestCaptureAdmissionAndSeal 冒充"）被点名提取规则计为承诺锚→勾选后拦截"只跑一半"——处置：跑该测试 110/0 绿并在 evidence 对账点名（声明属 06.3 锚未被冒充）。**口径回写项**：测试锚提取对否定性引用的识别（后续可给检查器加"不以…冒充"句式豁免或要求 evidence 对账点名——现按对账点名执行） |
| 过程教训（第三次） | 批量账目脚本 heredoc 换行串联又致半程态（08.1 目录名错 08-exit/真名 08-host，前两叶已勾第三叶未勾）——下次**脚本内 assert 前先 ls 验路径**，或全部路径集中在脚本首部变量区 |
| 就绪面 | 04.2/04.8/04.9/06.3/07.3（五叶）；本波派发＝07.3（evolution）∥ 06.3（rl+learning）∥ 04.2（tool-exec+capabilities）；04.8/04.9（memory 簇）下波合单 |

### 批次 24（D-M9 波：06.3 补差＋04.2 新实施＋07.3 M 回归＋编排者 flaky 修复；进度 80→83/103）

| 项 | 内容 |
|---|---|
| 07.3 M 回归 | 上游 05.3 收口后 TestPostActivationEvidence 110/0 零适配即勾 |
| 06.3 补差 | CaptureSink 实现 rl.Recorder（三方法→recordLine→admitLine 有界准入），经 WithRecorder 正式链一条链贯通（run.go/loop 零改动）；拒收 ErrCaptureRefused 进 RecordingLoss、写失败进 CaptureLedger.WriteErrors 双账显式。负锚 40/0＋正锚 110 不破 |
| 04.2 新实施 | tool/exec 执行资源生命周期：Manager（Acquire/Adopt/Renew/Sweep/Release/Explain＋计数）/停止凭证/cancel≠停止/unknown≠dead/TTL/重挂/IsTmuxAvailable 真实 PATH 对拍/受理协议全链时序。锚 90/0（8 子测，fake exec 承载无 Skip） |
| **编排者 flaky 修复（R15/§七2 先例）** | TestFormalChainSealsCapture 间歇 FAIL（FAIL→ok 交替）——根因 `rl/sink.go:206 go s.run()` 异步采集队列，ledger 断言在 Seal（drain 完成点）前读账=时序竞态。**修同步缺陷非改断言**：断言组（ledger Admitted5/WriteErrors5/Written0/InFlight0＋losses==0）整体移到 Seal 后，格式串逐字保留仅位置平移（前后对照留档）；12/12 稳定复验 |
| R30 偏差评估（第八轮） | 有条件勾选→C1/C2 discharge：C1=04.2 任务行回写（state/resources.go 移交 05.x/05.4＋05.4 notes 登记根接线归属）；C2=evidence 按 S5 落原文+退出码+逐锚计数+12/12。S3=MonitorConfig 四值钉 contracts（30/60/90/120s 防漂移）；S4=fragmentation 记账按实测（learning #trajectory-capture 组 3→4 文件口径）；flaky 修复性质裁断=同步缺陷修复（断言满强度未削） |
| 就绪面 | 04.8/04.9（memory 簇合单，下一波）；其后 05.4/06.4 等 |

### 批次 25（D-M10 波：04.8+04.9 合单＋05.4＋06.4 三单并行；进度 83→87/103）

| 项 | 内容 |
|---|---|
| 04.8 记忆策展 | 三通道互斥（retain/evict/protect）淘汰唯一走 Lifecycle.Delete 墓碑通道/引用保护 ErrProtected/不改写原始事实。锚 60/0 |
| 04.9 检索生产接线 | run.go 组合根 supplyRetrieval/WithEmbedder/Run.Recall 真消费 04.7 资产（04.7 归属声明闭环）；评测保留零增益与失败。锚 40/0/10skip；真实资源腿挂 09.5（D3 明写） |
| 05.4 blocker 解释读面 | query/explain.go＋WithExecManager/Run.Explain 根接线消费 Manager.Explain 计数面（波24 回写项承接）；不建第二真源/授权过滤先于列与计数。锚 70/0＋正锚 271 不破 |
| 06.4 训练导出授权链 | CaptureExportSource 读正式链产物（seal SHA256 对账）；负锚三要件（授权先行/逐字节溯源双向/loss 不冒充）。正锚 160/0＋负锚 50/0 |
| R30 偏差评估（第九轮） | 2 可勾＋2 有条件→D1-D4 全 discharge：D1=05.4 实施方自勾 tasks.md（越权已申报）编排者/评估门**追认**（勾选内容属实），后续波次明令实施方禁写 openspec；D2=Run.record→StoreEvent 写入侧登记 09.5 挂账；D3=04.9 真实资源腿明写挂 09.5 结项条件；D4=编排者 -count=1 双锚抽验 PASS |
| run.go 并行双写实证 | 04.9 检索面+05.4 explain 面并行双写合体零遮蔽（R30 核）——最小面划界（检索供面 vs explain 读面）有效 |
| 就绪面 | 03.8（唯一余叶）＋09.x 验收单；09.5 资源门单承载全部真实资源挂账 |

### 概念收敛战役（W-S0..W-S3，2026-10-17 用户裁定开打；总纲=concept-slim-plan.md）

> 裁定三项：**破坏式收敛**（合并/收窄/删概念，已勾选面按需重验，显式覆盖"旧机制不下线"在 API 简化面的适用）／**全仓按痛点排波**／**验收=静态上限+盲测双轨**（S1-S6 标尺）。判据：实现开发者只关心"做什么事、与什么有关系"，零额外选择。
> 编号登记：13 收敛叶（00.21–00.25/02.21–02.22/04.21–04.23/05.21/06.21/09.21）已按 R 修订批先例入册 check_runtime_plan SLIM_IDS（65 实施＋24 骨架＋13 收敛=102 基面＋1 既有=116 图节点）。

| 波 | 内容 | 次序依据 |
|---|---|---|
| W-S0 | 00.21 概念预算检查器（check_concept_budget.py：S1-S4 机器门）＋CI 步骤 | 工装先行，标尺先于改造 |
| W-S1 | 05.21 query 归并（12 碎接口→≤4）∥ 04.21 Policy 零选择构造 ∥ 00.22 run.go 拆文件族 | 三痛点（实现者心智负担实测最高面） |
| W-S2 | 04.22 窄口裁决+范例 ∥ 02.21 ctx 显式化 ∥ 06.21 大参数面拆块 ∥ 04.23/00.25/02.22 各域端口审视 | 面收敛（破坏面级联到已勾叶的按需重验） |
| W-S3 | 00.23 红线清单 ∥ 00.24 范例全仓 → 09.21 盲测终验（不知情 agent 双盲测） | 全仓收口＋S5 终验 |
| 穿插规则 | 未实施叶零旧面返工：08.2/04.4 前置 04.21+04.22；06.5/06.6 前置 06.21；08.3-08.5 前置 00.22（图边已改线） | 收敛先行于消费 |
| W-S0 后纪律 | **新码零债**：W-S0 之后新增/改动结构不得新增 baseline 债（超限即红，入册须具名豁免理由＋收敛归属叶）；本波新产债 9 项已登记 00.21 notes 并归属 06.21 扩容+07.22 拆块 | 波26 R30 W4 裁定 |
| S2 类别豁免（波27） | **数据/记录 schema 属保真面非选择面**：序列化载体（AttemptRecord/FeedbackRecord/RewardRecord/FeedbackSubmission/ReplacementRecord/event.Event）加性保真字段不计 S2 债，走 baseline 具名豁免（注释注明数据面）；配置/参数面仍零债硬拦。机门拦下 06.8 五结构→编排者按元规则一裁决类别豁免而非拆块（拆块=序列化复杂化违数据原样透传） | 波27 编排者裁决 |

### 批次 26（波26：W-S0 检查器＋三余叶；进度 87→91/116）

| 项 | 内容 |
|---|---|
| 00.21 概念预算检查器 | S1-S4 机器门（接口≤2/结构≤5 字段/裸 ctx.Value=0/范例+红线清单）＋164 项 baseline 棘轮（warn 禁静默/超 baseline error/失效 stale）＋29 例自测＋CI 注册。**编排者级联修复**：SLIM_IDS 注册致 test fixture 4 例红→fixture 补节点/checkbox/动态断言（R8 归责编排者改动） |
| 03.8 统一外置 | 四格式同路同阈值/内容键去重/DecideExternalize 零新边/NewBoundedTool 注入端口。锚 90/0（8 子锚含正式链 G2.6）；生产者接线留 09.1（架构禁边，S3 注明） |
| 06.7 feedback HTTP 契约 | 内容寻址身份/derive-on-change 不改旧/双门认证/逐跳重定向复核/大小限制。锚 130/0（12 子测）；**W1 勾选口径**=真实端点门控未验证标注挂 09.5（spec 允许挂账+标注，缺凭据不计完成面） |
| 07.1 反思观察/水位 | 越界观察面拒/水位 fail-closed 三态+引导态/防重放/让位/重启自锁/digest 零 LLM。锚 80/0（7 子测，S1 口径勘误）；**W2/W3 文书闭合**=spec 水位场景改"不可建立"+新增引导场景、任务行 lineage 落点改 evolution 单源 |
| R30 偏差评估（第十轮） | 有条件勾选→W1-W5 全 discharge：W4=baseline 新产债 9 项归属（06.21 扩容 Feedback*＋新增 07.22 evolution 拆块叶，SLIM_IDS 两处同源登记）＋**W-S0 后新码零债纪律**入编排；W5=rl/ 104 条 comment_policy 存量如实登记收尾账（00.6 门 assert-0 与存量债矛盾，收尾处置：清零或棘轮登记，收尾前不宣称 workflow 全绿） |
| 就绪面 | 05.21/04.21/00.22（W-S1 三痛点）∥ 06.5/06.6（已改线待 C 面）｜07.22/02.21 等待中 |

### 批次 27（波27：W-S1 04.21＋07.22 拆块＋06.8 余叶；进度 91→94/117）

| 项 | 内容 |
|---|---|
| 04.21 plugin 收敛 | Policy 9f→0 导出字段+Open()+8 With*（Judge 语义逐字保留）；ApprovalRequest 11f→三块匿名嵌入 3/4/4（JSON 扁平兼容，外部审批工具链回写子测实证）；run.go 零级联。四锚 31/41/111/31 |
| 07.22 evolution 拆块 | 四结构 10→5/7→3/9→4/6→4＋reflection_config.go 54 行；96 断言零删级联；越界改 scripts 删 4 行 BASELINE（R30 裁断二接受：申报内机械表维护优于留 stale） |
| 06.8 模型替换生命周期 | ModelLease/VersionedModel/替换记录；版本归属通路闭合（Response.Model 自述→记录链）；mutation probe 非空转；四子测×10 |
| **S2 类别豁免裁决（元规则一实证）** | 机门拦下 06.8 五记录结构超棘轮→裁决"数据/记录 schema=序列化保真面非配置选择面"（S2 领域事实=实现者选择数；拆块=序列化复杂化违数据原样透传；同 event.Event 16f 先例）——baseline 具名豁免+双处口径登记；**FeedbackEndpointConfig 配置面剔出豁免块**（R30 W1：裁决当口不留后门）归 06.21 收敛 |
| R30 偏差评估（第十一轮） | 有条件勾选→W1 一行处置+W2 evidence 数字存档+S3 勘误全落；三裁断：S2 豁免成立（有条件）/07.22 越界可接受/匿名嵌入权衡成立；S1 键面锚建议留 04.22+ |
| 过程教训 | 加豁免前先查既有表项（重复行被自测重复检查抓住——00.21 的表完整性钉起效） |
| 就绪面 | 05.21（W-S1 query 归并，run.go 级联独占）∥ 06.5/06.6（06.21 前置）｜00.22 拆族最后、02.21/02.22/00.25/04.23/06.21 等待中 |

### 批次 28（波28：05.21+04.22+06.21 概念收敛三单；进度 94→97/117）

| 项 | 内容 |
|---|---|
| 05.21 query 归并 | 12 碎接口→4（Reading/ReadingSource/DetailSource/LeaseExplainer）；InventoryFaces 8f→删除（Generation 2f/SiteReading 4f/TaskReading 5f）；run.go 单供面 runReading（删四读面）；水位并入 Freeze 单 revision（裁断①可接受）。正锚"块锚+编译锚+行为锚"三重不弱于旧断言 |
| 04.22 五相位裁决 | Enricher/ViewSelector/ExternalizePolicy **删**（反事实=删除前零注册零调用）；TransformCall/FinalPresenter **留**（正锚被测对象/生产消费者）；Phase 6→4（PhaseCallTransform 名实对齐，裁断②）；11 范例+红线 31 条 S4 归零 |
| 06.21 配置面收敛 | FeedbackEndpointConfig 6f→0（零选择构造+2 With*）；五项数据面豁免具名（引入叶+保真理由） |
| R30 偏差评估（第十二轮） | 有条件勾选→三条件 discharge：W1=plugin 14 行幽灵/stale 机械清零；条件2=域 tasks.md 勘误回写；条件3=-count=1 双抽验 PASS 留证。三裁断：水位并入 Freeze/相位更名/段头计数校正全通过 |
| S1 留痕 | 反事实证据（零注册零调用）＋12→4 归并对照入本 evidence（09.21 盲测审计面） |
| 就绪面 | 02.21（ctx）∥ 04.23（memory 端口）∥ 00.25（event 文档分组）；其后 02.22/00.23/00.24→09.21 盲测→00.22 拆族收口 |

### 批次 29（波29：04.23+00.25+06.5；进度 97→100/117）

| 项 | 内容 |
|---|---|
| 04.23 memory 归并 | 12→7（删 5 零消费端口/kvPort 5→2/ScopedOriginalStore 去 bare 读嵌入授权收紧/LifecycleStore 能力改装配入参 fail-closed）；S3-1 补登 4 不可达错误名闭合"归并不是删减"字面；自勾越权经 R30 追认 |
| 00.25 event 文档分组 | Event 16f 四块 doc-only（comment-check 等值见证）＋ParsedMeta/Event 豁免具名 |
| 06.5 SFT 清洗契约 | CleanSFT 12 具名 gap/失败显式账/不改旧数据/资格矩阵不外推/因果分组防泄漏/v2 只读路由；锚 20/0；拆三文件（500 行硬门）经裁定可接受 |
| R30（第十三轮） | 有条件勾选→W1-① 落地（SplitDeclaration 配置面归位，SFTCleanRequest 6→5 免豁免）＋S3-1 补登＋D4 表头 53 勘误＋D5 路径勘误全落 |
| 就绪面 | 04.4/06.6/02.21/00.22/00.23（五叶）；本波派发＝02.21（ctx，run/agent 簇独占）∥ 04.4 ∥ 06.6；00.23/00.24/00.22 依序收口（00.22 拆族最后） |

### 批次 30（波30：02.21+04.4+06.6；进度 100→103/117）

| 项 | 内容 |
|---|---|
| 02.21 ctx 显式化 | Judge 二合一入签名（TriggerSource 入 CallPass）；裸 ctx.Value 3→2（双 accessor 落点具名）；ctx 缝契约表落 contracts.md。**S3 口径裁决**：标尺原文含 accessor 分支——派发单"豁免出表"过严已勘误（checker docstring+baseline 修正），02.21 实为达标 |
| 04.4 地址化投递 | state/send.go+tool/remote 双面；锚 80/0 七子测（变异回退自证非空转）；**C1=W1 数据竞争修复**（幂等预检查改持锁 noticeSeatOf）；S1 残余（未授权重复读冻结回执）留 09.1 |
| 06.6 SFT token 制品 | 机制面 26/0（token mask 分侧/18 具名拒绝/常量替身拒收）；真实 tokenizer 面 R24 无门合法 Skip 标注挂 09.5；C3=loss 语义级联具名登记（09.5 裁尾 span 须认领 Verify 契约反向破坏面） |
| R30（第十四轮） | 有条件勾选→C1/C2/C3/S4 全落；三裁断：S3 勘误成立/冻结优先可接受/loss 挂账成立（须具名级联） |
| 就绪面 | 02.22（agent 端口审视，含 agent 红线）∥ 00.23（非 agent 部分红线清单化）；其后 00.24 范例全仓→09.21 盲测→00.22 拆族→F 收尾 |

### 批次 31（波31：02.22+00.23+08.2；进度 103→106/117）

| 项 | 内容 |
|---|---|
| 02.22 agent 端口 | 7→5（Producer×2 删并登记函数；RequestFace/SnapshotFace 拒合并——失败语义分叉实证；Reconciler 保留+生产装配缺口挂账 08.4）；红线 16 面 |
| 00.23 红线清单化 | 42 口/143 条（10 包 doc-only，29 文件等值见证）；S4 红线维度 58→9，残值 9 口经裁断3 归 00.24 补差（含 09.21 盲测锚 B 消费面） |
| 08.2 wechat 会话路由 | 8 子测×10；TestGuard 守卫钉真实通道零构造；断言 15→13 映射表落 evidence（裁断1 等价性合理）；D2 口径修正=7/8 真实装配+审批组件级，入站胶水集成断言挂 08.4 |
| R30（第十五轮） | 有条件勾选→D1 锚串修正/D5 注释 58/裁断2 反事实补登/映射表对账表入 evidence 全落；三裁断：15→13 有条件接受/Reconciler 保留可接受/9 口补差归 00.24 |
| 就绪面 | 08.3/08.4/08.5（host 后段）+00.24（范例全仓+9 口补差）→09.21 盲测→00.22 拆族→09.1/09.3-09.6→F 收尾（约 3-4 波） |

### 批次 32（波32：00.24+00.22 概念收敛收口；进度 106→108/117）

| 项 | 内容 |
|---|---|
| 00.24 范例全仓 | 58 新例（15 包 69 口零孤儿）＋9 口红线补差＋S4 baseline 58 行出清（gate=0/stale=0）；范例可执行零 panic 实证；Cleaner 谓词反转自查修正 |
| 00.22 run.go 拆族 | 1740→311+7 文件（最大 311）；67 块逐字位移（块级恰好命中+R30 独立抽验逐字属实）；四回归锚×10 全绿；架构零新边；14 行索引注释=规则必需 |
| R30（第十六轮） | 有条件勾选→D1 锚串修正×2/四锚具名/措辞降格全落 evidence；备注5=S4 红线计数正则 MUST NOT 双计（既有取证面，登记不处置） |
| **概念收敛战役主体完成**：S1-S4 全达标（接口归并/配置零债/ctx 缝契约表/范例+红线 69 口出清）；实现者心智负担判据落地 |
| 就绪面 | 余 9 叶=08.3/08.4/08.5（host 后段）+09.1/09.3/09.4/09.5/09.6/09.21（验收域——09.21 盲测已具备条件）；09.5/09.6 触**法定停点①（资源门缺凭据）**预 |

### 批次 33（波33：08.3＋盲测 A/B 第一轮；进度 108→109/117）

| 项 | 内容 |
|---|---|
| 08.3 文件收发边界 | 类型识别/媒体分派/大结果唯一副本+活跃保护/可执行硬拒/逐件失败回执；锚 4 子测×10＋变异反证四组；正式链全通 |
| **09.21 盲测第一轮=未达标（负结果入库）** | A：TransformCall 面可写（红线清单有效），**After 侧两面写不动**（Presentation/event.EventID 类型底座不在契约面）；B：可编译可跑但**独立实现性中等**（SourcePosition 命名类型硬阻塞/ObjectRef 相等语义与 Order 序契约缺失/Blocked 无范例/授权错误分层靠读实现）。S5 判据"两轮均达标方可勾"未满足——09.21 保持 planned |
| **盲测抓出的接口自足性缺口（待补差叶 00.26）** | ①类型底座入契约面（ObjectRef/SourcePosition/SessionState 来源/Presentation）②装配图（Reading→Freeze→DetailSource→Projection）③负向范例（ErrDetailOutsideScope/ErrDetailAbsent/ErrCallIdentityChanged）④transform error 后果文档（error=拒调用）⑤Phase→窄口映射注释+PhaseRiskPolicy 双面裁决⑥空 RiskLevel 语义陷阱⑦Order 序/身份相等语义明文化 |
| 就绪面 | 08.4/08.5/09.1/09.3/09.4/09.5/09.6 依赖链＋00.26 新补差叶待入册；09.5/09.6=法定停点①（资源门）预告 |

### 终态分块回溯 review（09.7，用户判据 2026-10-18 登记）

> **判据（用户定）**：按孙任务、领域分块，结合 prototype 与项目文档回溯——①原型要求/规划了的（已实现）②重构前版本特性当前未实现（缺口）③新增演化。**过往契约和行为不作金标准对齐**——穿透行为看本质，深刻理解特性需求。理想工程双维：**简单**（概念统一、收敛）＋**清晰**（职责明确、抽象单一、丰富多态）。
> 对照物（只读）：`prototype/`（重构前原型）、`docs/wiki/`、外部批准计划、midterm-review-response.md、全部 evidence 台账。产出=分块 review 报告（三轴分类表＋缺口特性归因＋双维评估）。

### 批次 34（波34：08.4+00.26；进度 109→111/119）

| 项 | 内容 |
|---|---|
| 08.4 运行时可见性 | diagnostics.go 只读面（env/配置开关+七段读出+缺线材具名理由）；Reconciler 裁决=装配（02.22 挂账闭环，措辞收窄=接线行无变异锚如实挂账）；锚 6 子测+变异反证三处；DiagnosticReport 7f 数据面豁免 |
| 00.26 接口自足性补差 | 盲测七缺口 7/7 doc/范例级（含身份归一化重写=范例纠错）；W1 措辞回落（空 RiskLevel=按 RiskMedium 定级，留痕另裁——不发明契约）；W2=Blocked 范例挂账 09.21 |
| R30（第十七轮） | 有条件勾选→三条件全落；两项待裁定谳：Reconciler=装配（撤销属回退）/反思面收缩=可接受（生产面无 trigger_source 戳，不造测试后门） |
| 流程缺口登记 | comment-only 门在整场未提交树无基线（HEAD=战役前）——收尾账 |
| 就绪面 | 09.21 盲测第二轮（00.26 done 后解锁）∥ 09.3（门禁逐条）∥ 09.6（文档闭合）；**08.5/09.1/09.4/09.5=法定停点①（真实资源凭据）** |

### 批次 35（波35：09.21 盲测第二轮双达标；进度 111→112/119）

| 项 | 内容 |
|---|---|
| W2 闭合 | Detail.Blocked 范例补差（编排者直写：owner Obligations.Blocked→投影 Blocked 通道，输出逐字实证+runner 用后删） |
| 09.21 盲测第二轮 | **A2/B2 双达标**：A2=PostCommitObserver 最难面"没读框架源码写出可编译实现"（1 正 3 负全过）；B2=三层实现 8 测全过+变异反证（第一轮三缺口全消）。S5 判据满足，两轮均勾 |
| A2 抓出红线措辞矛盾 | Plugin 红线"一元素一相位面"与 risk 双面语义冲突——已修（"serves one phase and may carry the faces of that phase"） |
| 残余改进项登记（不阻塞） | DetailState 六 kind 构造范例/NoticePhase 取值集/After 签名指引/空 EventID 语义 |
| **到达法定停点①** | 余叶 08.5（微信凭据）/09.1（真实模型观测）/09.4（部署 soak）/09.5（真实资源总门）/09.6+09.3（依赖上述）/09.7（终态回溯 review，依赖 08.5/09.6）——全部直接或间接需要真实资源凭据 |

### 批次 36（波36：09.1 确定性档；进度 112→113/119）——**法定停点①到达**

| 项 | 内容 |
|---|---|
| 09.1 scenario runner | `run_runtime_scenarios.py` 建成（真实驱动+三轴机器对照+禁人工 PASS 汇总）；S01-S09 9/9 attest（54/58 键相等）；S10 如实 remaining（归 09.5）；runner 自测 19 tests |
| **4 处 golden drift 如实报**（非金标准穿透素材） | S03 model_message_roles（波18 已登记过时）＋S03 receipts[1].decision 归属＋S08 blocker/advancer（golden 非词表：storage-degraded/task-a vs 产品实词 executor/diagnosis/explicit-stop/notice-transport）＋S09 tokenizer_capability（unknown vs real/deterministic/absent）——待 09.7 穿透裁决 |
| W1 勾选口径 | deterministic 档达成；complete 档（全 10 严格）待 S10 真实资源挂 09.5，该面不计完成 |
| **🚫 法定停点①（资源门缺凭据）** | 停点清单见下（等用户裁定：给凭据 / 标注未验收收尾 / 裁 golden drift 取向） |

### 批次 37（波37：09.3+09.4；进度 113→115/119）

| 项 | 内容 |
|---|---|
| 09.3 完整门禁 | 16 门逐条原始退出码（14 PASS/2 FAIL 如实）：G09 comment_policy 494 超基线（存量债）、G10 docs/api 27 stale+13 分歧（生成面漂移）——两项入 F2 全绿前置；race 两 module 零 DATA RACE；产品包零包内测试如实报 |
| 09.4 部署 soak | 四件 3 PASS+1 部分：常驻 900 调用/崩溃恢复（SIGKILL+异进程读回 32/900 逐一相同+空根负锚）/权限资源泄漏/热更前滚+内容回退实证——**专用 Rollback 通道 BLOCKED**（Run 公共面无出口，不冒充）；硬件断电如实不宣称 |
| 待裁登记 | Rollback 公共出口（09.5 后续面或 Run.Publish 增口）/旧 CI soak job 引用已删测试/G05 空转门口径/单写者锁未实现（并行 soak 前置） |
| **🚫 法定停点①（资源门缺凭据）** | 余叶 08.5（微信）/09.5（真实资源总门：embedding/collector/tokenizer/微信/模型 API/S10）/09.6（文档闭合，依赖 08.5）/09.7（终态回溯 review，依赖 08.5/09.6）——全部阻塞于真实资源凭据 |

### 批次 38（波38：09.7 终态分块回溯 review；进度 115→116/119）——用户裁定"资源不强依赖先行"

| 项 | 内容 |
|---|---|
| 09.7 回溯 review | evidence/09.7-retrospective-review.md 落盘：三轴分类（10 域分块）＋穿透分析（Receipt/观察本质=受理≠执行、golden 4 drift 归因=改 golden 对齐产品、概念收敛=扩展边际成本投资、原型未实现清单=诚实边界）＋双维评估（简单 11 面量化收敛/清晰三项实证）——**理想工程判据结论：是** |
| 依赖解线 | 09.6/09.7 deps 解除资源锁（用户裁定：review 类 mock 评估先行）；08.5/09.5 真实面仍挂 09.5 资源门 |
| 就绪面 | 09.6（文档闭合核对，只读面）＋08.5/09.5（真资源门） |

### 批次 39（波39：09.7 分块细化 review；进度 116/119 不变）

| 项 | 内容 |
|---|---|
| 09.7 分块细化（用户要求按实现计划拆） | 五块独立 review 落盘 evidence/09.7-review/ 共 1738 行逐叶 119 叶全覆盖（每叶=任务行要求/落点 file:line/三轴/锚证据）；00-summary.md 汇总=三轴 84/24/11＋跨块裁决清单 T1-T10＋辩证五要点 |
| T1-T10 裁决清单（等用户裁定） | T1 根入口桩面（量级最大）/T2 golden 4 归因方向/T3 03.8 义务归属/T4 会话正文所有权/T5 06.6 勾选口径/T6 资源门/T7 Rollback 出口/T8 09.3 口径（编排者口径成立，evidence 勘误）/T9 接缝组/T10 数字勘误 |
| 块内实测锚 | 11+22+全量三包+race 688+soak 900 加压——五块各自复跑非汇总背书 |

### 裁决书（T1-T10，2026-10-18 用户授权"分析判断作出裁决"；判据 J1 简单/J2 清晰/J3 心智负担/J4 穿透本质/J5 诚实标注/J6 进度不卡资源）

| # | 裁决 | 落地 |
|---|---|---|
| T1 | 删桩收窄入口（Build/Runtime/errNotImplemented×3/HostBridge 死桩全删，Assemble=唯一公共入口）——死桩=伪概念，零引用无人坏 | 派单执行 |
| T2 | golden 4 处改对齐产品（S03 六消息形态/decision 1:N/S08 域词表+场景意图/S09 三态词表）——契约不作金标准 | 派单执行 |
| T3 | 03.8 义务写进宿主契约（wechat-bot host 承接），不放宽红线3 | 文档契约（收尾批） |
| T4 | 承认 run 持有正文＋改述快照契约（可恢复性本质已由 durable 链+09.4 实证满足） | 契约改述（收尾批） |
| T5 | 06.6 维持勾选+标注（W1 口径与 06.7/04.9 统一） | 已成立 |
| T6 | 08.5/09.5"未验证（缺凭据）"标注收尾，凭据后补跑 | 勾选口径 |
| T7 | 回滚=新代重发布旧内容（代际不可变更纯）；不加公共 Rollback 出口（零独立消费者） | 验收口径改述 |
| T8 | 09.3 维持勾选，evidence 措辞勘误（执行面达成；FAIL 入 F2 前置） | 账目勘误 |
| T9 | 接缝组：快照纵链具名收缩/宿主装配接线或标注/07.1-07.3 接 tick 或标注/08.2 补断言 | 收尾批 |
| T10 | 数字勘误（06.5 子测口径/包计数/09.21 证据入仓） | 账目勘误 |

### 批次 40（裁决落地 T1+T2/T4）——T1 范围确认＋T2 残余欠账

| 项 | 内容 |
|---|---|
| T1 落地 | build.go 整删（Deps/ModelFactory/Build）＋tagent.go 死桩删（Runtime/CloseSession/Shutdown/Query/ErrBridgeNotAssembled/Bridge）＋errNotImplemented 根包全清＋入口改述（Assemble=唯一装配入口）。**范围确认**：HostBridge 类型保留（有 canonical 范例消费者+S4 锚，非死桩）——T1 裁决范围=死桩符号（Bridge()/sentinel 已除）；真删宿主面不在裁决内 |
| T4 落地 | 快照契约改述 3 落点（tagent.go/run.go Run doc/run_fact.go SessionHistory doc）："可序列化现场=facts+配置，正文经 durable 链回放恢复"——正文所有权未迁移（裁决如是） |
| T2 落地 | golden 4 处全改对齐产品（S03 六消息形态+decision 1:N/S08 域词表+场景意图/S09 absent）——**runner 转绿实证：EXIT=0 drift=0，58/58 确定性键全等**（裁决正确性=机器对照） |
| T2 残余欠账 | "自由文本移出身份轴"在 golden-only 白名单不可实现（runner 键集固定）——已值对齐+description 具名；闭环需产品侧 blocker_kind 稳定 code（归 09.1/09.6 欠账）；3-call 场景仍会漂（同归） |
| T3/T7/T8/T10 | 文档契约改述+账目勘误（编排者写面，收尾批执行） |

### 批次 43（波43：04.24+04.25＋09.8；进度 118→121/122）——问题脉络残余落地＋三问裁决

| 项 | 内容 |
|---|---|
| 04.24 Renew 调用点 | tool.go:170 唯一生产调用（RunTool.Call 续租节律封顶 lease/2）＋Renew 语义收紧（过期具名拒不复活）；A1 10/0＋非空转反例（摘续租→立红） |
| 04.25 串场专项 | A3 4 session 并发真实组装 10/0（marker 集零串场）＋A4 AttachReplay 正式入口反事实对照 |
| 09.8 收尾账 | 锚2 docs/api 收敛（27D/33新/13M，--check 0）＋锚3 doc-refs 历史锚定（43 全锚定 exit 0）；锚1/4 受白名单约束→W1 拆 09.9 |
| **三问裁决（编排者批复）** | ①457 注释债=09.9 独立批分批清（tests/integration→prototype→产品，禁 baseline 掩盖）②audit-marker legacy 域名词豁免=**批准**（掩蔽标识符+领域词组，与 name-check"keep domain vocabulary"先例一致）③evidence=编排者代录（本表即） |
| tracked-hygiene 28 | 27 删页+build.go=index 事项（R26 波后 git add/提交自然消）——F2 前置重跑 gen_godoc（并行 in-flight 耦合已知） |
| 就绪面 | 09.9（注释债批）＋F 系列（停点②需明示） |

### 批次 44（波44：09.9 三单并发；进度 122→123/123）

| 项 | 内容 |
|---|---|
| 09.9-A 规则豁免 | audit-marker legacy 域名词豁免（30 条豁免闭表，裁决=接受 30 不按目录剪裁）＋正负样例；proc-refs 白名单外→编排者落地（procRefSkip+4 键，路线 A） |
| 09.9-B tests/integration | 244→0（29 文件仅注释，去注释词法取证 code_diff 0；两处工具误伤整行回填） |
| 09.9-C prototype+产品 | 213→0（48 文件仅注释，三段式头注释保全）；重叠扫描伪影→编排者修（seenFile 去重） |
| 编排者收尾 | proc-refs exit 0＋重叠去重＋两自有注释归槽——**comment_policy 全域 0 finding**；lint.sh 仅 tracked-hygiene 红（index 事项 R26 波后） |
| **123/123 全叶完成** | 余 F 系列（F1-F4——停点②：提交/归档需明示） |

### 明确未完成（不得勾选）

- 台账 **done 52/103**；31 叶已重开（23 补差＋8 M 回归）待按 M 波重新实施/重验；M5 剩余功能（03.8/04.2/04.4/04.8/04.9/05.4/06.5–06.8/07.1/08.2–08.5）与 09 验收（09.1/09.3–09.6）未实施。真实资源门（09.5 各子项）未授权保持 `remaining-required`。
- **五查第五查未过的不得勾选**：无正式公共入口调用路径＋负向反例的完成声明一律无效（design D8/原计划 §10 口径）。
- 外部计划文件**未改**；主 `openspec/specs/`、prototype、历史 wiki、用户文件**未改**。
