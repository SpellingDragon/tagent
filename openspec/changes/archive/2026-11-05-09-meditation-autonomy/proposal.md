# 冥想自主面（meditation-autonomy）

父变更：boundary-closure（W2；2026-11-05 用户报告立项；前置=01 入 main）。

## Why

核查实证（HEAD 48d8eb93）：默认 tagent.yaml 的冥想三件套（独立会话/send 跨会话/事件驱动自跑）**已完整接线**（7 组测试实测 PASS）；但用户预期的"自主执行冥想任务进行整理"**未接**——meditator 工具面只有 send；file/exec/recall 面 tagent.yaml:261-262 明写"待接线"；"自主整理产物"没有落盘与登记路径。另三处漂移：meditation-agent-partition:44-46 旧两锚条款（LastTurnEnd 全仓无写入者）、README 用"tick/定时"描述事件驱动机制、[meditation] vs [reflection] 日志头。

裁决线（用户，2026-11-05）：r11 真实事件投影＋自持状态机（ADR 0003）；r12 门控内化为状态机内部规则、对外只留不变量；r14/r14b 自察为缺省、反思会话=`tagent-session:tagent:reflection`（ADR 0005）；r15 golden＋lint＋CI 三钉；r16② refine 不在本变更范围；r16③ **不动 api/config**，插件作为正常 agent 活动执行并落产物。

## What Changes

1. **工具面扩展**：meditator 接线 file（读整理材料）＋exec（受限执行整理任务）＋recall（检索历史）——按 tagent.yaml 待接线清单；**refine 不在本轮**。
2. **状态机实现**：冥想插件是自持状态机——激活＝既有 `external_input` 携带因果元数据（激活规则/积累窗/来源），状态持久经 `AnchorStore` 扩展；**不新立事实族**（GLOSSARY"冥想不登记事实族"不变）；激活事件是真实事件，不是虚投影。
3. **主体/会话统一 r14b 唯一版**：主语＝**配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）**；写隔离（反思事实落该会话链，用户会话零污染）＋读完整（同 agent 全分区真实内容可读）＋教训触达（recall 拉＋send 推）。
4. **挂载为行为等价迁移**（r16③）：`meditation` 配置节保留既有形态；插件作为正常 agent 活动执行并落产物；**挂载泛化与 schema 迁移不在本变更范围**。
5. **规格收敛（四个 delta）**：`meditation-autonomy`（ADDED：整理面＋状态机不变量）、`meditation-idle-gating`（REMOVED：独立门控能力退役四旧条款；附一条退役记录防空 spec）、`meditation-agent-partition`（MODIFIED：自察主体/反思会话；门控与锚点改由状态机持有）、`self-improvement-meditation`（MODIFIED：叙事对齐自察宿主）。机制细节下沉 design；"工具使用进 reflection 事件元数据"作为机制细节从 spec 移除（design 保留一句）。
6. **三处漂移修正**：旧两锚条款随状态机实现更新；README 去掉"tick/定时"表述；`[reflection]` 日志头统一。
7. **三钉与失败观察**：golden 登记状态机/反思会话接线；lint 拦特性专属运行时分支与新增特性专属装配路径；两门接入 preflight。冥想回合失败走既有失败观察（不新增通道）。

## 范围边界

- 工具面：本轮只接 file/exec/recall，refine 不在本变更范围。
- 通用判定结论事实族：不在本变更范围，本轮不立族、不预写条款。
- 挂载泛化：通用 `plugins:` 挂载位与 schema 迁移不在本变更范围；本轮保留既有 `meditation` 节。
- 工具使用元数据：机制字段与断言实现留 design/实现文档，不写进 spec。
- 状态机持久字段：AnchorStore 扩展的字段形态是机制细节，不进 spec。

## 边界与依赖

- **依赖**：01 入 main（同触 assembly 工具注册面，避免并发窗口）。
- **被依赖方**：无。
- **接口面**：不动 `api/**`（含 `api/config`——r16③）；`meditation` 配置节保留既有形态；冥想各主规格经四个 delta 同步。
- **禁止事项**：不动冥想三件套既有语义（独立会话/send 四道门/空闲门节奏）；不引入 ticker；不动 tool/exec 内核（04 域）；不新立事实族。
