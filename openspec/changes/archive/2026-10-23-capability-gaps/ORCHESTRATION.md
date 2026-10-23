# ORCHESTRATION：capability-gaps

## 结论层
抵达 DoD 的唯一路径：W0 五单并发交付六能力实现（协议冻结保证零相交）→ 单6 冒烟四负锚翻正＝生产消费者证据 → preflight＋CI 双绿。最大风险：单5（host 装配）依赖四单签名——冻结协议已消解，但 risk_rules/telemetry 的 yaml 键解析若与单2 语义偏移将返工单6。

## 分组层·编排四问

**次序**：就绪队列＝单1/2/3/4/5 全就绪（协议冻结）。关键路径＝单5→单6（host 接线落地前冒烟无法转正）。本轮做不了：单6（等 W0 全落）。

**授权**：白名单（零相交铁律）——
- 单1：tool/exec/**
- 单2：evolution/governance/**
- 单3：rl/**＋tool/subagent/**（builder 模型包装点）＋runtime/agent/loop.go（如需）
- 单4：tool/knowledge/**＋request.go
- 单5：host.go＋tools.go＋run.go＋api/config（yaml 键：telemetry/risk_rules）＋docs 生成
- 单6：examples/wechat-bot smoke 文件
共享面：根包装配三文件（host/tools/run）全归单5——禁他单触碰（R21 热点）。

**凭据**：各单三锚见 tasks.md 验证面（具名测试，R29 已冻结）；编排者复验点＝单6 的四翻正断言原文（负锚→正锚逐字符对账）＋preflight 尾行。必须亲自复验：`go test -tags smoke ./... -run TestLocalSmoke -count=1`（examples/wechat-bot 内）与 `bash scripts/preflight.sh` exit 0。

**释放**：无设备/端口锁；单6 的前置＝W0 五单全部回报＋编排者 grep 验锚；熔断线＝任一单两轮返工仍红→停摆对账协议是否偏移。

## 明细层

| 单 | 触碰集 | 状态 | 前置 |
|---|---|---|---|
| 1 exec | tool/exec/** | ready | 无 |
| 2 approval | evolution/governance/** | ready | 无 |
| 3 trajectory | rl/**＋tool/subagent/**＋loop.go | ready | 无 |
| 4 skill body | tool/knowledge/**＋request.go | ready | 无 |
| 5 host wiring | host/tools/run＋api/config | ready | 协议（已冻结） |
| 6 smoke flip | examples/wechat-bot/** | blocked | 1-5 落地 |

预算：单6 前总工时无硬预算；W0 各单一次会话内完成。

## 台账层
- 单4（skill body）：done。注入口签名：`knowledge.LoadSink = func(name, body string)`、`knowledge.Options.Sink`、`tagent.NewSkillLoadSink(agent event.AgentID) knowledge.LoadSink`；request 装配 `loadedSkillBodies` 两源（files 的 `ToolResources.SkillLoads`＋skill_dirs 的 per-agent ledger），结果文本 `loaded: <name>` 不变。验证：`TestSkillLoadBodyEntersNextRequest`＋`TestUnknownSkillStillRefused` 绿，`go test ./tool/knowledge/...` 绿。**单5 已接线**（`host.go:1487` 的 `knowledgeTools`：`Sink: NewSkillLoadSink(event.AgentID(id))`），生产链路 knowledge `skillLoadTool.Call` → `skillLoadLedger.loaded` → request `loadedSkillBodies` → 下一轮 RoleSystem 正文段；smoke 技能正锚归单6 翻正。`docs/api` 已随生成器同步本单两个页面（根包、tool/knowledge）。
