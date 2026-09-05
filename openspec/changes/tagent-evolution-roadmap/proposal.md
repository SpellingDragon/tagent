# Tagent Evolution Roadmap

## Why

两篇独立评审(《Agent行业趋势与tagent项目评析》《deep-agent-tagent-analysis》)经本机事实核查后收敛出同一组差距:反馈归因闭环缺失、评估体系缺失、Harness 无自进化、多 Agent 组织停留在工具调用、语义检索为 stub、工具治理无权限/审批/审计链、发布工程粗糙(无 CI/tag、示例硬编码绝对路径)。与此同时,codes 工作区的开源生态提供了远超预期的低成本复用面:直接依赖 trpc-agent-go 内置完整的 evaluation/team/codeexecutor/container/vectorstore 模块(均可 import);同语言项目 crush 有成熟的 permission 系统(Go,allow-list + session 缓存 + 异步审批);rustviking(tagent file memory 的后端)自带 VectorStore/EmbeddingProvider 插件;nanobot 的 SKILL.md 技能生态与 tagent 的 skill.FSRepository 格式兼容。本变更是一个伞形路线图:把差距清单分解为五个有依赖顺序的阶段,每阶段派生独立子变更执行,并定义自驱执行所需的闭环治理机制(准出门禁、预留确认项核对、sub-agent 分工、失败处置)。

## What Changes

本变更自身不修改运行时代码,交付的是治理工件与阶段分解:

- 五阶段路线图(依赖排序,P0 先行):
  - **P0 工程可信度**:修示例硬编码绝对路径、接最小 CI(build/vet/test/race 门禁)、README 依赖声明(tmux/Go 版本/可选组件)、打第一个 version tag。
  - **P1 评估基座 + 语义检索**:复用 trpc-agent-go `evaluation/` 建 evals/ 一等公民目录(组件 Eval + 端到端任务集 + Bad Case 回归);语义检索二选一落地(桥接 trpc vectorstore+embedder,或打通 rustviking CLI 向量命令);压缩/冥想过程指标埋点。
  - **P2 反馈归因闭环**:EventKey 作为回执(Reef 模式),新增 feedback 入口(HTTPAPI `/feedback` + 可选 feedback 工具),评分写回 MemoryStore 并关联 record-id,反馈关联率纳入 TrajectoryRecorder,打通 AReaL reward 消费路径。
  - **P3 Harness 自进化(RHI)+ 陷阱注册表**:prompt 制品版本化(依托现有 prompt.Source 热载),轨迹 pairwise 比较生成候选版本、先评估后激活;失败教训作为特殊事件类型入库,冥想空闲期提炼陷阱卡片供 recall。
  - **P4 工具治理 + 组织模式**:借鉴 crush permission 实现工具执行 waterfall(权限检查→审批→审计→执行);exec 沙箱可选路径评估(trpc codeexecutor/container);Teamwork 式 critic/verifier 协作模式(评估 trpc team ModeCoordinator 直接复用 vs tagent 自定义 tool agent)。
- 每阶段派生独立 openspec 子变更执行(propose→apply→verify→archive),本变更的 tasks.md 是阶段级检查点清单。
- 自驱闭环治理机制写入 specs(roadmap-governance):阶段准出定义、预留确认项核对义务、验证门禁(build/vet/test -race/真实集成抽查/CodeReview)、sub-agent 分工惯例、失败处置与升级规则。
- 预留确认项(执行到对应阶段时重新核对,不阻塞路线图批准):见 design.md「预留确认项」与 tasks.md 各阶段 CONFIRM 任务。

## Capabilities

### New Capabilities

- `roadmap-governance`: 伞形变更的自驱执行治理——阶段依赖与准入/准出条件、预留确认项的核对时机与降级规则、每阶段验证门禁的最低标准、sub-agent 分工与失败处置、阶段完成后的归档与下一变更派生义务。

### Modified Capabilities

<!-- 无:各阶段子变更自行新增/修改对应能力规格(如 evaluation、semantic-retrieval、feedback-attribution、tool-governance 等),本伞形变更不直接改动既有能力 -->

## Impact

- 本变更:openspec/changes/tagent-evolution-roadmap/ 工件 + openspec/specs/roadmap-governance/;零代码改动。
- 后续阶段影响面(逐阶段细化,此处为量级预估):
  - P0:examples/wechat-bot/tagent.yaml、README、.github/workflows/(新增)、git tag。
  - P1:新增 evals/、memory/(SearchByEmbedding 落地)、tool/recall/、agent/compress/(指标)、rl/(指标);新增依赖 trpc-agent-go evaluation/knowledge 子模块或 rustviking CLI 命令面。
  - P2:rl/、memory/、event/types、examples/wechat-bot/main.go(HTTPAPI)。
  - P3:prompt/、agent/meditation、新增 harness 优化器模块。
  - P4:tool/action/、registry 执行路径、agent/(协作模式);可能引入 docker 依赖(可选)。
- 风险:P2/P3 依赖 P1 的评估信号,顺序不可颠倒;P4 沙箱路径涉及部署环境差异,需执行时确认。
