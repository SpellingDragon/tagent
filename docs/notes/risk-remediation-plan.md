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
