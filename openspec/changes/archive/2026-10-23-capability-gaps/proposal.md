# capability-gaps：六项核心能力补全——执行/组装/审批/轨迹/技能/可观测

## 裁定（用户）
冒烟与审计登记的六缺口全部是核心能力，立档推进，测试先行拦截。

## 六缺口→能力（目标-任务承载矩阵）

| # | 缺口 | 核心能力 | 生产消费者 | 验收形态 |
|---|---|---|---|---|
| G1 | exec 无 Executor/装配 | 命令执行（tmux 异步＋监控三态） | action agent 的 exec 工具 | smoke_exec 负锚→正锚（echo 真跑） |
| G2 | skill_load 不回正文 | 技能正文进下一轮请求 | 模型决策（load 后请求含正文） | smoke_skill GAP→正锚 |
| G3 | call_subagent 不在宿主 | 运行时组装子 agent（显式源） | 宿主文档声明＋模型按需造 agent | smoke_data B3-2 负锚→正锚 |
| G4 | 审批闭环缺登记＋无风险面 | critical 自动 Hold→微信审批→批准执行 | approvalGateway 全自动闭环 | smoke 审批去 smokeGovernor 补接线 |
| G5 | worker 调用不入 trajectory | 训练轨迹完整（520=520） | ChainExporter/计费/审计 | B3 对账口径翻正 |
| G6 | OTel 未接线/无 metrics | 可观测（trace 开关＋指标端点） | 运维监控 | /metrics 200＋计数>0 |

## 波次（文件簇零相交；host.go 为热点——四接线的 host 部分合一单）

```
W0（五单并发，协议冻结后）：
  单1 exec实现      tool/exec/**（tmux Executor＋monitor 三态）       ——不碰 host
  单2 审批闭环      evolution/governance/policy.go（Request 登记＋默认风险面）
  单3 轨迹完整      rl/capture_wiring＋runtime/agent/loop.go（worker 接 Capture）
  单4 技能正文      tool/knowledge＋request.go（FixateSkillLoad 式注入）
  单5 host装配      host.go 四接线（按 W0 冻结签名：exec/风险面/call_subagent/metrics）
W1（依赖落地后）：
  单6 冒烟转正      examples/wechat-bot smoke 四负锚翻正＋审批去补接线
```

## 协议（冻结——W0 并发前提）
- exec：`tagentexec.Executor`（tmux 实现）＋`WithExecManager(mgr)`——签名单1定、单5按此接线
- 风险面：`governance` 声明规则（工具名前缀→风险级）＋`Policy.riskCritical` Hold 时调 `approvals.Request` 登记后回 Hold 回执
- 轨迹：worker 的子 agent 模型经同一 Capture 面（CaptureAdapter 包装 worker binding 的模型）
- 技能：skill_load 结果后下一轮请求注入正文（ToolResources 路径）
- call_subagent：hostToolIDs 收录＋toolsFor 装配（显式源语义已有——只缺宿主声明面）
- metrics：/metrics（Prometheus 文本）＋`WithTelemetry(SDKObserver)` 配置开关（yaml telemetry 段）

## DoD
六能力各有生产消费者证据（smoke 六断言全绿）；37 包＋preflight＋CI 全绿。
