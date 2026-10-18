# 07-evolution — 反思与自演进（二级子变更 proposal）

> 层级：隶属于一级变更 `runtime-rebuild`（上级目录 `../../`）。本域是**责任与验收单位**，不是代码包数量，也不是整域派发单位。
> 本轮只产规划文档（md）；产品代码、测试、脚本、检查资产均不在本轮写域。所有孙任务未完成（`- [ ]`），不得导入旧 change 编号或勾选。

## Why

运行时重建采用新内核（state 持生效绑定、agent 借同代绑定、事件管线插件归 state），历史 `evolution/` 与冥想链路的旧实现已随残件清理失效。反思与自演进能力必须在新内核上重建，且必须避开历史教训：自持永动、把"nil 恒真"当真实评估、登记动作卷走用户工作区、缺证据仍报通过、以及新造一套 bundle 版本库替代 git 原生真源。

## What Changes

- **07.1 反思主动输入**：授权观察面（缺省自察/显式跨域）+ 水位新鲜度判据 + 自状态摘要，反思作为**显式输入生产者**注入本 agent 循环 session；只读事实链，零 LLM 渲染 digest。
- **07.2 git 原生 refine**：`register` / `status` / `rollback` 三操作；`register` 仅对**显式 pathspec** 内受控文件 `git add` + 结构化 commit（`[self-improve]`），**不卷用户 index/工作区其他改动**；文件直接热读生效，git 仅版本层与后验建议。
- **07.3 激活时刻证据窗**：窗口锚 = 改进版本**首次被真实请求消费**的**激活时刻（激活证据）**（**非 commit/register 时刻**），窗口内前后版本请求按**真实消费版本**归因（GitSHA 仅关联），指标/LLM-judge 评估输出**四态**（健康/劣化/**样本不足**/未到期）——样本不足（含缺激活证据）MUST NOT 判健康。
- **07.4 结构评估门**：结构候选的 Eval/Benchmark/ApplyGate 消费**同一声明与同代绑定**；缺证据不报通过；门错误链因果保留（`errors.Is/As` 可达）；不依赖后验反思调度完成。

## 边界与依赖（四要素）

### 依赖

- **实现依赖**（需前置任务真实落地）：
  - 07.1 ← `04.7`（授权时间/类型/关系检索，观察面读缝）、`05.4`（运行时阻塞解释）。
  - 07.2 ← `03.3`（持久提交与幂等接纳）、`01.6`（文件直接热读与结构变更分路）。
  - 07.3 ← `07.2`、`05.3`（提交后观测接点）。
  - 07.4 ← `01.5`（组织发布/换代/在途旧绑定）、`02.4`（模型单一终局协议）；**不依赖**后验反思调度完成。
- **接口常数依赖**（签名级、冻结后即可与实现并行；选错代价大，需在设计闸复核）：
  - 观察面解析：`observed_namespaces ⊆ read_namespaces ∪ {自身分区}`；缺省 = `[自身分区]`；越界具名拒绝启动。
  - 自管谱系判定单源 = event 包 `SelfManagedLineage`（消费方 MUST NOT 复刻清单）。
  - 锚点持久化 `AnchorStore` **两锚结构**（`lastTurnEnd` / `lastMeditation`）；历史三锚文件多余键忽略。
  - 受控路径 `evolution.protected_paths`（默认 `resources/prompts/**`、`skills/**`）；`deliver`/`send` 内容上界 **8KiB**。
  - 评估窗口锚 = improvement 事件的**激活证据时刻**（改进版本首次被真实请求消费；**非 commit/register 时刻**），无独立时刻表组件；improvement 事件 GitSHA 仅作版本关联，不作窗口起算。

### 被依赖方（本域产物的消费者）

- `08.4` 宿主接 Query/反思/训练诊断开关（消费 07.1/07.3/07.4 的可见面）。
- `09.1` golden 场景（反思 digest、评估四态、refine 台账）；`09.5` 真实模型评估窗（资源门）。
- 上游：`04.7` 检索、`05.3` 观测、`03.x` 事实链——本域只消费只读事实，不写它们的状态。

### 接口面

- refine 工具三操作（`register`/`status`/`rollback`）的输入（显式 `paths` + `note`）、返回（sha/窗口提示/越界清单）与失败语义。
- 后验评估输出（evaluation 事件 Content：窗口 sha、verdict、证据摘要、建议文案）与四态枚举。
- 结构评估门（Eval/Benchmark/ApplyGate）对同一声明/绑定的消费契约与错误链。

### 禁止事项

- **不新造 bundle 版本库**（不复活 BundleStore/ReleaseManager/发布道/快慢道/diff-propose）；git 为唯一版本层。
- **不擅自把"nil 恒真"当真实评估**——缺证据/未接读缝必须 fail-closed 并具名，不回落备用判据、不以静默零值冒充"无劣化信号"。
- 不自动执行 git 回滚（执行权在人/agent）；不新增持久化（让位欠账为进程内会话语义）。
- 不恢复退役训练桥；不把合成/mock 消费者当真实消费者。
- 不在本域写 `state`/`config`/`agent`/`task`/`memory`/`05-inspection` 的机制实现（只消费其公开面）。

## Capabilities

### 本域 deltas（`specs/` 内，仅列确有变化的条款）

- `meditation-agent-partition`：**ADDED** — 反思主动观察面的授权与水位新鲜度（fail-closed）。
- `git-native-refine`：**MODIFIED** — `refine register` 以显式 pathspec 为界、不卷用户 index（完整源 Requirement + scenarios 保全，附新增 scenario）。
- `evolution-evaluation`：**ADDED + MODIFIED** — ADDED：评估结论四态（样本不足不得判健康）、结构评估门同源声明与缺证据不报通过（错误因果保留）；**MODIFIED**：「后验评估锚点(improvement 事件)」与「改进版本章(feedback join 保持)」——窗口锚由登记 commit 时刻改为**激活证据时刻**、窗口内前后版本按**真实消费版本**归因、GitSHA 降为关联（完整主源条文 + scenarios 保全）。

### 继承（requirement 语义不变，只在实现层重建；不复制归档 history 全套）

- `self-improvement-meditation`：产物纪律/登记闭环、负反馈回顾、digest 让位欠账——**继承**。
- `meditation-delivery-tool`：`deliver` 仅授予有回流授权的冥想 agent、过投递缝四道门、内容有界——**继承**。
- `meditation-idle-gating`：双闸门（novelty/节奏）、不依赖输出侧血统、混合批让位、水位执行语义——**继承**。
- `meditation-self-state-digest`：确定性 digest、概况为主/任务层可选、有界渲染、优雅降级——**继承**。
- `evals-harness`：票据可召回率、工具选择评估与 Bad Case 资产——**继承**（本域重建其 harness 挂点，不改条款）。
- `cognitive-asset-guard`：漂移审计不变量、审批脚手架与拆除账本——**继承**（终态=权限域分离，属独立运维变更）。

## Impact

- 未来实现范围：`evolution/`（含冥想调度、refine、评估）、`plugin/` 反思/观测接点、`docs/` 相关新架构文档（由主代理决定创建时机）。
- 本轮写域仅 `openspec/changes/runtime-rebuild/changes/07-evolution/**md`；不动主 spec、不动旧 change、不提交/归档。
- 07 域孙任务共 **4** 项（07.1–07.4），全部未勾选。
