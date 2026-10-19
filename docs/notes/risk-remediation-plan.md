# 风险处置计划（2026-10-18，大重构中途）

> 来源：外部风险评估 6 项；事实核实 6/6 实锤。处置原则：快赢先行、白盒测试核心三包先行、大面（1833 篇分区）只做方案不动本体、登记债保持登记并强化回正条件。

| # | 风险 | 处置 | 分片 | 判定 |
|---|---|---|---|---|
| R-A | 文档/规格与码面系统性错位（wiki/README"以 prototype 为准"表述与 rebuild 落地后现实错位；runtime-contracts.yml 过期注释；CI 空跑步骤） | 修正当期架构表述（现架构=runtime-rebuild 16 包，prototype=保留原型非现架构）＋CI 注释对齐＋空跑步骤处置 | 单A | 小 |
| R-B | 生产包 14 包零单元测试（黑盒集成为主，内部不变量/边界缺白盒） | 核心三包（state/task/agent）关键内部不变量白盒测试先行；其余 11 包登记续补 | 单C | 中 |
| R-C | 无根 README（对外首读缺失） | 写根 README：定位/架构速览/文档导航/pre-release 声明 | 单B | 小 |
| R-D | 文档/CI 漂移（含上述 A 项） | 并入单A | — | — |
| R-E | 依赖 replace 未回正（fork trpc-agent-go，等上游 PR #2637） | 保持登记＋回正条件显性化（go.mod 注释+检查脚本门） | 单D | 小 |
| R-F | 单人维护+pre-release（bus factor=1；无迁移承诺） | 接手指南（docs/OPERATION.md：构建/测试/门禁/发布/风险面）＋1833 篇文档归档分区方案（只做方案与导航，不动本体） | 单D | 中 |

**并发分片（触碰集不相交）**：单A=docs/wiki/README.md＋.github/workflows；单B=根 README.md（新建）；单C=state/task/agent 包 *_test.go（新增白盒测试）；单D=go.mod 注释＋docs/OPERATION.md＋docs/notes 分区方案节。

**验收通则**：逐锚独立 exit；白盒测试须断言内部不变量（非 API 复述）；文档修正须引用实测事实；全回归 `go test ./tests/integration/... -count=1` 零影响。

## 第二批（2026-10-18 外部评估第二轮，按严重度）

| # | 风险 | 处置 | 分片 | 判定 |
|---|---|---|---|---|
| R-G | 测试结构偏科（memory 5.7K/rl 7.4K/evolution 4.2K 零白盒） | 白盒按体量×不可见性补三包（压实/分区/召回断裂；导出/替换/账本；后验/作用域）＋包级覆盖率基线门 | 单A | 中 |
| R-H | 文档/规格熵（1855 篇/213K 行≈代码 4 倍） | markdown 只降不升棘轮门＋openspec archive 只读标记；移出主仓=裁定项 | 单B | 中 |
| R-I | 真实 LLM 验收缺位（CI 全 mock） | nightly/weekly 真实冒烟 workflow（记录模型版本）＋provider 契约独立 workflow（不阻每 PR） | 单C | 中 |
| R-J | 依赖 fork＋缺规模证据 | stream-close 4 commit 本地 patch 收口评估＋公开 benchmark（吞吐/恢复时延/内存基线） | 单D | 中 |
| R-K | 采用门槛高 | CONTRIBUTING＋issue 模板；CLI/HTTP 第二宿主=裁定项；module 路径=裁定项 | 单E | 小 |

**裁定项（列清单等用户）**：openspec archive 移出主仓 or 只读化；module 路径迁稳定命名空间（breaking）；CLI/HTTP 第二宿主示例立项。

## 第三批（五特性对齐，2026-10-19）

| 特性 | 期望 | 承载现状 | 对齐处置 |
|---|---|---|---|
| F1 plugin 作用+处理+数据转换 | 开发者定义相位面 | S04 四相位全覆盖（转换后执行/零副作用/提交后呈现） | 保持（"事件类型"语义=相位，演化已登记） |
| F2 自定义事件消费（冥想/knowledge/recall） | 外来自定义事件→消费逻辑（LLM/tool）→投递结果 | runtime 基础面=external_input 准入→Invocation 消费→投递（02.5/02.6 集成覆盖）；冥想/knowledge=宿主周期策展职责（不迁） | **语义归位**：runtime 供事件消费基础面（S11 显式覆盖外部输入驱动）；冥想/knowledge 登记为宿主职责（OPERATION 风险面） |
| F3 subagent=task+manager+回调通知 | 统一抽象/状态变更通知发送者 | S05 覆盖（通知定向不改投/身份字段）＋agent.Delegate | 保持 |
| F4 subagent 模型配置（prompt/skill/MCP） | 子 agent=普通 agent 独立装配 | 配置面齐（Model/Files/Tools/Plugins/TaskPolicy）、golden 缺场景 | **补 S11**（config 声明父+子，断言子模型/prompt 装配） |
| F5 配置描述前后插件 | AgentSpec.Plugins 驱动管线 | 链路在、golden 经替身注入未验配置面 | **补 S11**（插件按声明序执行） |

**S11 场景设计**：——一份 config.Declaration（父 agent 带工具插件管线+子 agent 声明 Model/Files/Tools/Plugins）→ 外部输入事件准入 → LLM 决策委派子 agent（task 受理收据）→ 子 agent 用自己装配的模型/prompt 应答 → 结算通知回调父会址 → 前后插件按声明序执行。观测键：子模型名/prompt 来源/委派 task 身份/通知定向/插件执行序。
