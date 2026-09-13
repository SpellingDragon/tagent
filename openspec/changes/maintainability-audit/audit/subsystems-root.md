# evolution / rl / 根包评分卡

base: `cf006e1` | 取证: evolution/rl 结构读 + gitrefine/exec 定向核对；根包 build_agent/tagent/org_hotreload/config/testing 全文级（多轮 review 积累）

## evolution（5 文件 1143 行 / 测试 652）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | A- | GitEvolution 装配单元/gitrefine 纯函数/judge/guardrail 分文件清晰；评估窗口快照一次性 |
| 耦合 | S | 经 StoreEvidenceSource 读 governance 事件（键常量统一 event 包）；不 import agent |
| 测试 | A- | gitrefine 纯函数覆盖充分（转义修正 N5 等教训注释）；judge 需真实 LLM 的部分靠契约层 |
| 文档一致 | A | 与 specs/self-evolution（劣化只出建议）一致 |
| 演进风险 | B+ | git 操作依赖工作目录状态（生产=独立部署仓的告诫已文档化）；**git exec 裸调无超时（F-8，同 F-6 类）** |

发现: F-8

## rl（4 文件 772 行 / 测试 809）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | A | TrajectoryRecorder（writeLoop/gcWg 生命周期分离）/SwappableModel/HTTPAPI 各一文件 |
| 耦合 | S | 独立顶级包；HTTP API 可选（未启零开销） |
| 测试 | S | 809 测试行超源码 |
| 文档一致 | A | 五端点契约表（rl/README）与实现互锚 |
| 演进风险 | A- | TurnTracker 跨 goroutine 语义（RL 任务串行/批次合并）已在 D5 标注待与 AReaL 侧确认 |

发现: 无

## 根包（11 文件 3514 行 / 测试 2355）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | B | build_agent.go（buildMode 类型化后仍 727 行装配分支+late-bind 散点）、tagent.go（New+懒检查编排+ring2）、org_hotreload.go（指纹白名单）；组合根天然集中，但 late-bind 接线（resident sink/redispatch/回滚钩子）缺一张类型化清单 |
| 耦合 | B+ | 根包汇聚全部子包（组合根本职）；wiring.go 已拆出 resolve/wire 族减负 |
| 测试 | A- | 2355 测试行；org_hotreload 三件套 + governance_wire + partitions + builtin_protection 覆盖装配关键面 |
| 文档一致 | A | buildMode 三谓词、热重载流程均已在 wiki §2.13/§六·A 文档化 |
| 演进风险 | B+ | 每新增一个「entry 专属接线点」需同时改冷启动与 executorOnly 壳两条路径的 ownership 判定——三谓词已收编大部分，但 late-bind 散点仍是新增接线的主要出错面（本次审计前刚发生 pruneTerminal 相邻面的生产 panic） |

发现: F-10（注释引用设计报告行号为系统性漂移温床，根包与 memory 均见）
