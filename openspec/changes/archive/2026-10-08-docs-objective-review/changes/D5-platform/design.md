# Design: D5 平台子系统域评审

## 阅读顺序与技术要点

1. `platform-subsystems.md`（221 行总纲）：治理闸 / 自进化(git 原生) / 常驻可靠性 / 配置热更(候选事务/执行代) / 统一可观测 / 记忆引擎 / MCP 闭环——默认关闭项全部 opt-in。
2. `agent-behavior-matrix.md`（281 行）：子系统启用后 agent 实际反应矩阵（逐条溯源代码）——检验"默认关≠没设计"。
3. `org-hot-reload.md`（272 行）：组织级热更——换代、应用记录与无锁读面。
4. `resource-ownership.md`：租约、代际与封路（共享 store 最后引用清理）。
5. `cognitive-asset-guard.md`：认知资产防线（漂移审计 D1 默认开 / 写审批 D2 / 走私引导 D4 / 权限域分离 D3 终态）。
6. `reincarnation-notice.md`（54 行）：换装后首轮自我告知。
7. `evolution/evolution-architecture.md`（113 行）：refine 通道、git 原语安全闸、后验评估窗口与双回滚触发——自进化子系统的展开篇（总纲在 platform-subsystems）。

## 重点问题（过度设计审计主战场）

- 五子系统各自的消费者画像：除 wechat-bot 示例外，谁真的开过治理闸/自进化？文档是否给出适用场景的硬判据。
- org 热更（世代治理/指纹/子集规范化）与 config 热更（候选事务）两套机制的职责切分是否清晰、是否重叠。
- 自进化（evolution 篇）：git 原生 refine + 后验评估 + 双回滚——「劣化只出建议」的信号建议式与「框架永不动手」四原则是否真能护住生产；生产=独立部署仓的硬前提是否被配置面强制。
- 资源租约治理为"共享存储的最后引用清理"服务——真实共享场景频率 vs 机制复杂度。
- 认知资产防线 D1 默认开：默认开启项的零配置承诺与 `working_dir` 例外是否自洽。
- 判准 R/T 专项：子系统开关注入的行为差异，对轨迹一致性（判准 T）是灾难还是可控（RL 训练时这些开关应全关？文档是否交代）。

## 代码抽查断言候选（≥2 个）

- org 候选事务热更执行路径（agent/org/）；
- resources.DefaultResources.Acquire 租约清理（agent/resources/）；
- 漂移审计默认开、零必填配置（组合根 wiring）；
- governance/evolution/reliability 三者的 enabled 默认值核实；
- GitEvolution 的安全闸/双回滚触发（evolution/ 包）。

## 风险与回退

六篇均为中短篇，无预算风险；注意 behavior-matrix 是"声明溯源"，抽查时抽它的溯源链接是否真指向代码。
