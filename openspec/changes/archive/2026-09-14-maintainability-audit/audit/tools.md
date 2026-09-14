# tool 族评分卡

base: `cf006e1` | 取证: action（tmux_monitor/resident_recovery/declarative 全文级）、mcp/registry 锁纪律、其余结构读

## tool/action（7 文件 3349 行 / 测试 4743）

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | B | action_tool.go 为最大件（执行+会话管理+resident 生命周期）；tmux_monitor 三态+自适应轮询+per-session callback 多机制同居 |
| 耦合 | A- | 依赖 agent/task 契约（TaskSpawner/SettleDetector）；向上经 SetResidentRecordSink late-bind |
| 测试 | S | 4743 测试行超源码 42%；resident_gate/resident_meta/tmux_monitor 有专项 |
| 文档一致 | A | 承诺表/三态化/n- 排除均与 wiki 九·A 同步 |
| 演进风险 | B+ | tmux 版本敏感（list-sessions 三态实测 tmux 3.6a）；monitor 回调链与 rebuiltResumeClosure 的新建 detector 归属较绕 |

发现: 无新

## tool/recall（4 文件 1221 行）+ tool/knowledge（3 文件 967 行）

recall: 统一入口「参数即路由」（recall.go 头注释），确定性形状不经 LLM——设计清晰。knowledge: websearch 带 API 出处注释。双双 A-（测试行 665/318 对源码偏薄，recall 尚可、knowledge B）。

## tool/mcp（3 文件 637 行）| tool/task + tool/govx + tool/memoryx + tool/plan + tool/spec + tool/file + tool 根 + workspace + modelutil + testutil（合并简报）

- mcp：Registry mu 锁 + 懒热同步（registry.go:41-52）+ API 注入面保护（L29 注释）——A-
- tool/task（293）/govx（216）/memoryx（171）：窄工具面，A-
- tool/plan（302）：PlanAgent 双模式（进度查询绕过 LLM 直读文件）——A-
- tool/spec（294，测试仅 71 行）：openspec 工具面——B（测试薄）
- tool/file（122，包装上游）/workspace（139，scratch 根+周期清理）/modelutil（93，ModelRef 统一装配，远端 agent 新提交）/tool 根（49）/testutil（93）：皆 S/A 级小件

发现: F-9（tool/spec 测试密度全包最低——🟡，见台账）
