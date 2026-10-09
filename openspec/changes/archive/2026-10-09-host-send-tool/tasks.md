# Tasks: host-send-tool

- [x] 1.1 抽取共享发送函数：main.go 输出循环的发送分支（目标三级/typing/长文/附件/失败回执）抽为可复用实现，被动路径改为调用它——行为逐字节不变 —— 验证：`cd examples/wechat-bot && go build ./... && go test ./... -short -count=1`；既有输出路径相关测试零改动通过。
- [x] 1.2 实现 send_tool.go（Declaration send/content-only/8KiB/HostSendFunc 注入/[send_denied] 文本）+ 模块内单测（参数/上界/拒绝/成功/无目标）—— 验证：`go test ./... -run '^TestSendTool' -count=1`。
- [x] 1.3 装配：ToolRef 声明挂载（entry+meditator 默认示例）+ 扩展点接线（grep factory/WithTool 先例择惯例）+ "未声明不可见"断言 —— 验证：`go test ./... -run '^TestSendToolAssembly|^TestSendToolCapability' -count=1`。
- [x] 1.4 e2e：mock model 脚本化 send 调用 → mock bot 经同一发送函数收到内容；housekeeping 回合内 send 成功送达（对应昨案场景）—— 验证：`go test ./... -run '^TestSendToolE2E' -count=1`。
- [x] 1.5 提示词心智对齐：TOOLS/AGENTS/meditator_agent 补双通道语义句（被动不自动送达非用户回合的用户向内容；send=显式通道；未配则等用户回合补报）—— 验证：grep 关键句存在 + `GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh`=0。
- [x] F1 被动通道零变化断言：谱系白名单/投递缝/扣留回执 diff=0；被动路径与主动路径共用同一发送函数（代码审读+共用断言）。
- [x] F2 门禁：bot 模块全绿、根包 `./... -short`=0、lint(Go1.24)=0、check-openspec=0。
- [x] F3 DoD：spec 两 Requirement 全 Scenario→测试对账。
- [ ] F4 归档：strict → archive --yes。

> 编排者核销（2026-10-09 复跑）：bot `^TestSendTool` ok（15 PASS：工具面8+装配2+e2e4+守卫）、bot 全量 short ok、根 `./... -short`=0、lint(Go1.24)=0、check-openspec 116/0→117。F1 采代码波机械对账（被动块 34 语句 vs 共享通路 39，5 类规范化差异均语义等价：接收者字段/continue→return 末位/局部副本弃值/`_ =`/sendErr 记录；日志串、回执 kind、阈值逐字未变）+ 6 既有测试文件零改动。F3 两 Requirement 5 Scenario 全映射实存测试。主动侧继承统一回执经编排者裁决采纳（零静默丢失原则一致，send-tool 非白名单谱系无递归）。1.5 战报之 docs/api 尾账已由 GOTOOLCHAIN=go1.24.1 gen_godoc 清（wechat-bot.md）。
