# 冥想自主面：状态机与主体（技术要点）

## 关键决策

### D1 主体与会话（r14b 唯一版）

冥想的主语是**配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）**。三条性质同时成立：**写隔离**——卡片、工具调用与 digest 全落反思会话的链，用户会话零污染；**读完整**——同一 agent ⇒ 自己分区全部事实以真实内容可读（有界投影＋recall 深查，不是跨 agent 的摘要授权）；**教训触达**——卡片落同一分区，业务回合经 recall 可取（拉）＋send 注入目标活跃会话（推，同 agent 跨会话）。不同会话＝不同消费循环，天然并行；席位抑制管"宿主回合在飞不激活"，两时刻让路照旧。独立 agent 降格为**显式跨域策展选项**（读别人＝摘要＋回查），`meditator` 保留为可选策展声明。

### D2 触发是自持状态机（r11；ADR 0003）

- **状态积累**：外部事件修改状态（novelty 累积、静默窗、欠账计数）；状态达到激活即投影为一个**真实事件**——既有 `external_input` 携带因果元数据（激活规则/积累窗/来源），冥想回合在专属反思会话消费该事件，digest 只是载荷。定时只是激活检查方式之一；时间窗、累积阈值、外部请求都是一等激活源。
- **状态持久**：经 `AnchorStore` 扩展保存与审计（迁移记录），不新立事实族——GLOSSARY「冥想不登记事实族」不变。
- **不变量**：① 用户让路（宿主回合在飞不激活）；② 锚点重启安全；③ 触发即真实事件可审计（带因果）；④ 自管产出不计入状态积累（激活即复位；防自持）；⑤ 影响他人只经 send。机制条款（novelty 判定、双锚、rearm、fail-closed、抑制判据）留实现文档，不进 spec。

### D3 工具面：file / exec / recall（refine 为已登记债）

- meditator 的工具面接 file 读、exec（受既有安全门与 04 域世代校验）、recall；全部经标准工具面与既有白名单，零新机制。
- **refine 不在本轮**：`self-improvement-meditation` 的产物登记/refine 条款保持主 spec 原样（已登记规格债）；本轮产物经既有 file/exec 落盘、既有事实生产可审计，不新增 refine 接线。
- **工具使用元数据**：字段与断言实现留本 design/实现文档，不写进 spec（r12 式：机制细节出 spec）。
- 触达集收窄（F25）：只改 `examples/wechat-bot/tagent.yaml` 的 meditator 声明＋meditator 侧声明校验；工具白名单文件不改。

### D4 挂载：行为等价迁移（r16③）

按用户裁决"不动 api/config、插件作为正常 agent 活动执行并落产物"：

- `meditation` 配置节**保留既有形态**；冥想插件作为正常 agent 活动执行，产物经既有路径落盘。
- **行为等价**是验收面：迁移前后冥想回合的观察面、发送门、节奏与产物路径不变。
- **欠账（显式）**：通用 `plugins:` 挂载位与 schema 迁移指向未来"agent 产物统一管理"变更；ADR 0004 的目标形态不在本轮实施。
- lint 以**基线**处理既有 `meditation` 节与既有专属解析路径（登记为债）；**新增**特性专属节、`resolve<Feature>` 解析路径或运行时专属分支即红——既不为存量伪造绿灯，也不放行新债。

### D5 三挂点映射（无冥想专用运行时机制）

| 能力 | 挂点 | 挂载形式 |
|---|---|---|
| 整理动作（file 读/exec 受限执行/recall 回查） | **标准工具面** | tagent.yaml 的 tools 声明挂到 meditator——复用既有工具与安全门，零新机制 |
| 整理产物落盘 | **事实/文件既有路径** | 产物经既有 file/exec 工具落盘、既有事实生产入链；refine 登记=已登记债 |
| 负反馈进反思素材 | **插件 f(x)（管线）** | digest 渲染扩展（请求装配期确定性变换），读既有 feedback 事实，零 LLM |
| 触发（状态激活→真实事件） | **插件自有状态机＋事件 x** | 状态由外部事件迁移（持久可审计）；激活迁移本身成为真实事件投影给反思会话消费——多源激活（时间窗/阈值/外部请求），配置声明 |
| 目标/完成判据素材（后续） | **消费 g(x)（读链）** | 观察面读链扩展——验收结论/目标清单作为可回查事实被冥想消费；判定结论事实族（L5 桥）属未来变更 |

约束：meditator 与任何 agent 同构——工具白名单、安全门、审批面全部复用；装配面不得出现冥想专属**运行时**分支（配置节存量除外，见 D4）。

### D6 漂移修正

- meditation-agent-partition 旧两锚条款（LastTurnEnd 无写入者）随状态机实现更新（delta 内 RENAMED+MODIFIED 为"门控与锚点由状态机持有"，消除与 idle-gating 退役的矛盾）；本 agent"循环 session"旧约改为**本 agent 的专属反思会话**。
- README 去掉"tick/定时"表述，改事件驱动＋状态激活。
- `[reflection]` 日志头统一（不再出现 `[meditation]` 头描述反思回合）。

### D7 三钉（r15）

1. **golden**：`golden/runtime/architecture.json` 登记冥想插件包（状态机、digest 渲染、反思会话接线）、插件注册表与反思会话边；`check_runtime_architecture.py --implementation` exit 0。
2. **lint**：①运行时专属分支扫描（冥想/策展等特性分支出现在 loop/consumer/tool dispatch 即红）；②声明面新增特性专属节（基线 `meditation` 之外）即红；③新增 `resolve<Feature>` 专属解析路径即红。门带正反样例自测。
3. **CI**：上述门接入 `scripts/gates/preflight.sh`，逐提交跑。

### D8 失败观察

冥想回合失败（模型失败、工具失败、发送遇阻）走既有失败观察（`noteFailureObservation` 形态），不新增通道、不写冥想专属失败文案。

## 文件清单

- `examples/wechat-bot/tagent.yaml`：meditator 工具面（file/exec/recall，删除"待接线"注记）；既有 `meditation` 节保留。
- `features/evolution/meditation/`：状态机类型与激活事件（`driver.go`/`reflection.go`）、`AnchorStore` 扩展、declaration 校验、digest 渲染、meditator 岗位说明。
- `assembly/host_document.go` / `host_tools.go`：**默认不动**（F25 收窄；既有 `meditation` 节已注册，工具白名单文件不改）。若既有声明路径必须改动，因与 08 同文件，上报编排者合单或串行；不得新增冥想专属运行时分支。
- `prompts/meditation.md`：工具面边界句更新（素材=摘要＋可回查工具）。
- `docs/README` 与日志头：措辞修正；**wiki 归 08 归口，09 默认不动**（若必须改=与 08 合单）。
- `openspec/changes/09-meditation-autonomy/specs/`：四个 delta（autonomy ADDED、idle-gating REMOVED 四旧条款＋一条退役记录 ADDED（防空 spec）、agent-partition MODIFIED、self-improvement MODIFIED）。

## 风险与回退

- 自主 exec 的安全边界：复用既有审批/安全门；负锚覆盖越权调用。
- 自察上下文预算：靠折叠与卡片有界性；状态机持久字段受 AnchorStore 既有预算约束。
- 状态机与既有水位/在途计时的双源风险：以单次迁移（状态持久化）落地，旧水位只读退役；迁移记录可审计。
- 回退：工具面与配置是逐项接线，可逐工具/逐声明回退；状态机实现可整体 revert（不新立事实族，无链上迁移债）。
