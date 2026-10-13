## Context

本变更从清理后的现场重新规划，不沿用旧 `rebuild-formal-runtime` 的完成状态。**清理前历史** HEAD `56c58eb`（其残留的 `model/`/`state/`/`tagent/`/`tests/` 与旧 change 残件已于 2026-10-13 用户授权批次整目录删除）；**当前新基线** HEAD `a9381ce`（`chore(plan): land runtime-rebuild planning baseline and contract checkers; remove stale wave remnants`）。清理前盘点与保护面**保留于 `baseline.md`（历史记录，附处置更新，不逐行改写）**。本轮用户授权为 OpenSpec 规划产物及检查器；产品代码、产品测试、依赖、主规格不修改。

## Goals / Non-Goals

目标：清晰可演进的运行时、state 所有的执行及插件绑定、统一只读运行总图、原生工具任务受理、准确原文与投影、数据采集和 SFT 实际消费。内部按依赖集成，最终一次交付全部适用能力。

非目标：复活旧包结构；第二工具循环或第二会话真源；泛化 DI 容器、插件 DAG、永久去重总库；PPO/GRPO训练引擎接入；未经授权的数据库更换、外部调用、提交或归档。

## Decisions

### D1 责任先于包和接口

| 责任 | 唯一写入/生命周期归属 | 生产消费者 |
|---|---|---|
| 组织声明 | config解析，组合根编译 | state发布、结构评估 |
| 生效执行绑定 | state按代持有模型、工具、文件来源、插件管线 | Agent请求借用、Query、策略快照 |
| session/mailbox/消费位置 | state | Agent、恢复、Query |
| task/通知义务 | task决定转移，经提交端口保存 | 投递、调用归约、Query |
| 原文/引用/投影决策 | memory | 模型视图、recall、文件、导出 |
| 临时副本 | workspace | 工具/宿主；不改变原文寿命 |
| 观察/训练工件 | 观察插件、capture/export writer | 诊断、清洗、SFT；不作恢复真源 |

公共 `package tagent` 位于 module 根，不建立第二个 `tagent/` 同名门面。根只组合和提供公共API，领域代码按真实替换与生命周期边界注入。核心类型及其行为可同包/同文件，不为包数、构造器名或行数制造抽象。

备选未采纳：把现存接口视为必须保留的设计；为禁止一个import另造镜像类型；把所有领域装入万能Store；每文件单独包。

### D2 二级领域与能力责任

| 域 | 边界与主要能力 | 外部依赖/下游 |
|---|---|---|
| 00-foundation | 基线、覆盖、设计闸、检查器；roadmap-governance | 所有域共用，主代理维护 |
| 01-state-binding | 配置声明、session权威、绑定发布；config-state-twinship/workflow-config-compilation/config-hot-reload | event协议；Agent/Query/评估消费 |
| 02-execution | 身份、任务、模型、事件循环；unified-task-semantics/async-task-execution/persistent-event-loop | state绑定；提交端口与03联合审定 |
| 03-history | 原文、持久提交、投影、回收、副本；event-segment-store/recall-protocol/event-sourced-projection/tool-output-interception | task提交消费者、Agent视图、工具/导出 |
| 04-capabilities | 原生工具、exec/MCP/A2A、治理、检索 | 02执行、03原文、01租约；宿主/反思消费 |
| 05-inspection | 运行总图、权限/分页/订阅、观测；runtime-inspection及既有tracing能力 | state/task事实；不向Query开放写权 |
| 06-learning | capture/export、SFT清洗/消费、反馈/模型替换 | 02身份及终局、03原文、01绑定 |
| 07-evolution | 反思输入、Git留痕、后验与结构评估 | 原文/Query/模型；文件先直接生效 |
| 08-host | 根公共API/CLI、微信独立module、文件/诊断 | 02–07实际装配；真实发送另授权 |
| 09-acceptance | 场景观测、迁移回退、全量门禁、正式档/规范闭合 | 全部被验收任务；归档另授权 |

主规格120条能力的责任映射见 `coverage.md`，这是能力盘点，不是601个Requirement均已语义认证的声明。新增 `runtime-inspection` 用于跨对象统一查询，不替代任务看板或观测导出。每个能力只有一个delta写者；跨域通过任务前置协作，不复制同名规范。目标—KR（架构成功判据）见 [expectations.md](expectations.md)；锁定框架的原生复用依据集中于 [trpc-reuse.md](trpc-reuse.md)，由 00.3 接入 G1 审定。

### D3 生命周期合同与设计闸

行为合同汇总在 `contracts.md`，不复制所有原生Go类型。G0–G3各有明确输出：

| 闸 | 输出 | 当前状态 |
|---|---|---|
| G0 | 清理后文件/保护清单、能力与资源边界 | 文档盘点已生成，开工时复核；未跑产品基线 |
| G1 | 共享字段/签名、生产消费者、所有权/取消/失败结果、原生接口探针方案 | 设计稿及待核项已列；签名未编译取证 |
| G2 | 正常/失败/并发/重启的纵向场景与观测合同 | 10场景合同已编排，产品观测未产生 |
| G3 | 每单精确写集/读闭包、实际前置、命令及资源预算 | 本轮仅规划/检查器单；无产品派发 |

00.3中的联合设计先于02.1/03.3的提交实现，01.4/02.6的插件绑定与阶段、05.2的快照接口、06.1的数据格式也先在此审定。不能以实现任务已完成作为修复该任务所需设计的前置。

### D4 模型和插件组件复用

锁定core为 `github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`，provider为独立module `v1.10.0`。直接使用Model/IterModel/Message/Response/CallableTool/ToolSet及原生CallID，不实现LLMAgent的第二工具循环。

- 模型终局只一份归约：channel/iterator、usage尾包、空/partial/错误、取消和ProducerDone分别处理；RL只观察不复制归约器。
- Before策略只在副作用前一次，最终变换参数经核心授权后同时执行和记录。原生ToolCall参数按原生类型解码。
- state持插件生效链；Agent借同代模型/tools/plugins/files，不持私有在线配置真源。
- 请求丰富、视图选择、风险规则、外置策略可委派插件；可信身份、最终授权、canonical提交与任务义务属于核心。
- After只形成呈现；canonical提交后只读观察可以驱动OTel、RL和反思输入。观察失败不回滚事实、呈现drop不删除事实。
- 原生model/tool callbacks与plugin.Manager可独立接入；框架nil“不替换”不等于tagent drop。
- file/skill/MCP/A2A/TokenCounter/Embedder/适用Evaluator优先复用，验收是实际消费者调用，不是只有import或类型别名。

备选未采纳：为挂callbacks引入第二Session持久化；用observer成功冒充canonical提交；仅测试factory而不连接正式请求。

### D5 提交与结果的分层

受理Receipt、存储提交结果和目标投递接纳是三个合同，不能合并为一个状态枚举。

任务转移与outbox意图共同提交。持久成功后再可见和派发；确定未提交与结果不确定区分，不对不确定副作用自动重跑。幂等key标识一次mutation，任务同一执行轮次可有多次状态变化；通知身份包含任务、轮次、类别和序号。

重复提交只证明事实已存在，不证明通知已交付；重放未确认通知仍需向原目标实例尝试幂等接纳。配置发布有自己的generation记录，不与所有任务拼成跨store大事务。

原文blob可以按内容去重，引用metadata区分owner/format/保留策略。原文读取逐字节一致；投影恢复按原呈现决策一致。副本允许多个或安全复用，只要求引用明确、不覆盖及可管理的生命周期，不要求全局唯一文件。

备选未采纳：在NoticePort后补持久化；将后端batch方法当已证明的原子提交；临时副本作原文唯一载体；未知恢复项直接变成功。

### D6 Query与训练数据

Query在已有授权内读取，不主动GrantRead；统计先过滤。Snapshot/List/Detail基于有界不可变revision，订阅窗口过期/溢出具名报告gap/reset，删除和同状态变化也进入增量。Query索引只是投影，不是第二状态真源；报告源水位与滞后，不夸跨库实时原子快照。

episode≠session，decision/attempt与实际请求边界同步产生。SDK capture分层保存原文、模型视图、SDK与provider请求，敏感头/端点凭据不落盘。上游支持、此次返回、采集保留、本地派生分别取证；unknown不是unsupported。capture发送与Close采用同一准入排空协议，封账与缺口真实。

用户选定数据闭环＋SFT消费：真实tokenizer/chat template/collator、labels/mask、split依赖和反馈/模型替换要有证据；不接PPO/GRPO引擎、不凭文本日志宣称采样RL资格。

### D7 并发、写域与合并规则

实际派发只依据 `ORCHESTRATION.md` 和 `task-graph.json` 的新编号。域只作为责任与验收单位。

同树并发需写集W彼此不相交且各自W不与对方构建/验证读集R相交；只读公共依赖可以共享。测试包内所有测试源也属于验证读集。冲突合单/串行/隔离，不用“追加兼容”或文件名不同代替检查。

组合根、共享合同、机器任务图、CI/golden和覆盖表默认主集成者单写。代理不安装依赖、不改Git index、不在共享产品树做破坏性变异。每单结束核产物、测试实际执行、数字来源和遗漏，设计未定或资源未授权不勾选。

### D8 三级验收与证据语义

- 叶任务：实际产物＋生产消费者＋点名测试；零匹配/仅Skip/仅编译不足。
- 子域：全部适用场景和跨域接线。当前四件套生成不代表65实施叶任务（“阶段二”）已执行。
- 顶层：两个module、所有一方子包、场景golden、迁移/soak及真实资源。

检查资产的三种输出互不替代：`plan/policy/contractValid` 是规划格式；`implementation` 是实际代码覆盖与依赖；`evidence` 是真实观测及测试结果一致性。后两者本轮不执行。错误输出不可被管道过滤掩盖。

### D9 两阶段交付与骨架先行

交付分两阶段，**阶段一骨架先行、过评审门后才进阶段二**：

- **阶段一 = 24 个 P0 骨架任务**（编号 `NN.P0.M`）：只落**数据/接口定义与 CI 合规注释文档**（类型、常量、方法签名、Godoc／包注释），**零业务逻辑**。
- **骨架约定（冻结于此，实施时逐包核）**：
  - 有返回值方法体统一 `return <零值>, errors.New("phase 2: not implemented")`；无返回值方法仅 `// TODO(phase2)` 空体；**禁止 panic**（不以 panic 占位）。
  - 每包 `skeleton_assert.go` 放编译断言 `var _ Iface = (*T)(nil)`（接口实现关系在骨架期即编译取证）。
  - 导出符号英文 Godoc **完整句**＋包注释；文件 ≤500 行；类型与其阶段二逻辑**同文件内聚**，不建空壳拆分。
- **阶段门 = `00.P0.3`**：全部域 P0 done 后执行**认真评审**（CodeReview 子代理＋主代理亲自复核＋用户确认），产出评审记录后解锁阶段二。01–09 全部 `NN.M`（阶段二）MUST 以 `00.P0.3` 为额外前置——此为 `task-graph.json` 硬边，并由 `check_runtime_plan.py` 拒收（缺边报错文案含「阶段门」）。**未过门派发任何 `NN.M` 属编排违规**。
- **P0 只建有真实消费者的包，不预建新包**（与 O1-4 一致）；每个 P0 单的白名单 = **本包文件组 + 该包 `skeleton_assert.go`**。

**被否方案**：
- *仅纸面签名不落码*——探针无力（无法编译取证）、评审无实物（只审文字不审可编译符号）；
- *骨架夹带逻辑*——评审失焦（难以界定“零逻辑”）、返工面大（逻辑在骨架期即被冻结，后续修订成本高）；
- *整域单个骨架单*——粒度过粗，不利于并行推进与按包评审。

## Risks / Trade-offs

- 残件引用不存在包 → 保留只读清单，逐文件后续处置，不默认恢复；实现检查当前可能诚实失败。
- 主规格含旧接线/重复标题 → 逐行为对照与delta替换；不把旧名整体继承，不在本轮改主spec。
- 共享提交、插件、查询签名仍待探针 → 相关任务design-blocked；在闭合前不扩大产品实现。
- 真实服务/制品未授权 → 保留resource-gated，不打印秘密值、不以Skip结项。
- 原CI和bot大文件不符合新树 → 检查器报告红项，由后续00.6/08/09实施，不降低阈值掩盖。

## Migration Plan

本轮不迁移代码或数据。后续先清单化残件处理；旧配置/存档显式读旧写新，源摘要不变，缺失外置文件和不确定任务具名。旧二进制和新格式路径隔离；公开根API迁移由宿主域验证。

归档需独立授权，二级delta按单一能力责任提升并标准同步，保留父子对应；不假设顶层archive递归处理嵌套。

## Open Questions

G1尚需实现前签名探针确认：state执行绑定借用/释放、提交mutation与恢复结果、插件阶段映射、Query快照/订阅令牌、capture新格式与旧v2读取路由。每项在领域design列明消费者及候选，不以空结构或旧类名提前冻结。

重大Runner承载、存储真源/DB/WAL更换、跨进程动态插件或采样训练引擎不在当前默认方案；若成为必要前提，阻塞相应路径并在其余独立工作和证据齐备后交用户裁定。当前批准的能力并不授权这些方向的预实施。
