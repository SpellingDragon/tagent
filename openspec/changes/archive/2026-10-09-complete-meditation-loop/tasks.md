# Tasks: complete-meditation-loop

## 1. deliver 工具（tool/meditation 新叶子包 + build_agent 接线，单代理）

- [x] 1.1 实现 `tool/meditation/deliver_tool.go`（Declaration `deliver`、参数 target/content、DeliverFunc 注入、8KiB 上界、拒绝转 `[delivery_denied]` 结果文本）+ 包测 —— 验证：`go test ./tool/meditation -count=1`；分层断言 `go test . -run '^TestArch_' -count=1`。
  验证输出：`go test ./tool/meditation -count=1` → ok 0.256s（7 用例：声明/上界双侧/拒绝含白名单/成功回执/身份夹带/未绑定/协议错）；`go vet ./tool/meditation` 净；`go test . -run '^TestArch_' -count=1` → ok（方向断言自动覆盖新叶，无需目录登记）。
- [x] 1.2 装配授予：`meditation.enabled ∧ deliver_to 非空` 时挂工具、闭包绑定 `DeliverToAgent(本实例, target, 保留 session, msg)`；"未配白名单的工具声明不含 deliver"能力面测试 —— 验证：`go test . -run '^TestMeditation.*(DeliverTool|CapabilitySurface)' -count=1`。
  验证输出：`go test . -run '^TestMeditation.*(DeliverTool|CapabilitySurface)' -count=1` → PASS×2（授予三态 + ExecutorConfig 能力面）；`go test . -run '^TestMeditation' -count=1` → ok（既有用例零回归）；`go test . -short -count=1` → ok 59s（根全量）。绑定时序=wireAgent 内 NewTagentAgent 与授权登记之后 SetDeliver（实例指针闭包捕获）。
- [x] 1.3 e2e：mock 脚本化一次自主 `deliver` tool call → 目标真实消费卡片；既有投递/e2e 用例零改动不回归 —— 验证：`go test ./tests -run '^TestExternalMeditation' -count=1`、`go test . -run '^TestDeliverToAgent' -count=1`。
  验证输出：`go test ./tests -run 'TestDeliverTool' -count=1 -v` → PASS×2（自主闭环 + 白名单外拒绝回合继续）；`go test ./tests -run '^TestExternalMeditation' -count=1` → PASS×3 零改动（meditation_e2e_test.go 未触碰）；`go test . -run '^TestDeliverToAgent' -count=1` → PASS×6 零改动（delivery.go/delivery_test.go 工作树 diff=0，F1 成立）；comment_policy 0 finding，gofmt 净；gen_godoc 新增 docs/api/tool__meditation 页，index 仅 +1 行/包总数 40→41。

## 2. 改名统一冥想语族（依赖 1.2 的常量引用；单代理）

- [x] 2.1 示例与代码：yaml `curator→meditator`（tools 声明/deliver_to 不动）、main.go 常量与日志前缀、`git mv` 两个提示词文件、`curation→meditation` session 名 —— 验证：`cd examples/wechat-bot && go build ./... && go test ./... -short -count=1`、`grep -rn "curator\|curation" examples/wechat-bot/ --include="*.go" --include="*.yaml" | wc -l` =0。
  验证输出：`cd examples/wechat-bot && go build ./... && go test ./... -short -count=1` → ok 0.973s；`grep -rn "curator\|curation" examples/wechat-bot --include="*.go" --include="*.yaml" --include="*.sh" | wc -l` → 0。
- [x] 2.2 文档：README 双语（"策展/curation"降为"旁路冥想线"叙述、示例名更新）、wiki §2.14/runtime/platform-subsystems 改名同步、spec delta 已由本 change MODIFIED 承载 —— 验证：`GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh`=0、`grep -rn "curation" README.md README_EN.md docs/wiki/ | wc -l`=0（历史归档目录除外）。
  验证输出：`GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh` → lint: ok（0 finding，comment_policy 0，gen_godoc 41 packages）；`bash scripts/check-openspec.sh` → 115 passed 0 failed；`grep -rn "curation\|策展 agent\|curator" README.md README_EN.md docs/wiki --include="*.md" | grep -v "meditation-curator\|历史上曾称\|archive" | wc -l` → 0。

## 3. 收尾 F

- [x] F1 非触碰断言：`git diff d9372b8..HEAD -- delivery.go 的既有四道门逻辑 agent/meditation.go 判据段 memory/ openspec/specs/cross-session-delivery` 语义零改动（工具是消费者）。
- [x] F2 全量门禁：build/vet/`./... -short`/bot/改动域 race/lint(Go1.24)/check-openspec 全 0。
- [x] F3 DoD：两 delta 全 Scenario→测试对账；真实模型不重跑（e2e 已覆盖自主投递路径，注记说明）。
- [x] F4 归档：strict → archive --yes → 文档残留清零复核。

> 编排者四查+F1–F3 核销（2026-10-09 复跑）：两波代理全部验证独立复跑通过——`./tool/meditation` ok 0.394s、`./tests -run TestDeliverTool|TestExternalMeditation` ok（自主投递/让位回合/既有 3 条零改动全绿）、根 `^TestMeditation|^TestDeliverToAgent` ok、bot 模块 ok；examples 与 README/wiki 的 curator/curation 残留 grep=0（历史注记与 `meditation-curator` 锚名除外，锚迁移确认不属本波）；F1 `git diff HEAD -- delivery.go agent/meditation.go memory/ event/lineage.go specs/cross-session-delivery`=0；F2 全量 short=0、lint(Go1.24)=0（41 包含新包）、check-openspec=0、改动域 race ok。F3 映射：ADDED 5 Scenario→实存测试 9 名（能力面 AssemblyGrant/CapabilitySurface@meditation_assembly_test.go:170/199、上界/拒绝文本/成功/伪造身份/未接线@deliver_tool_test.go、闭环/让位@meditation_deliver_tool_test.go）；MODIFIED 仅保留名措辞（行为测试不增）。真实模型按 D6 不重跑（e2e 已覆盖自主路径）。

> race 修正注记：F2 全量 `go test -race ./tool/meditation ./tests` 首跑 rc=1——FAIL 点为**既有真实 LLM 契约用例** TestContract_PlanWriteBoundary（本机有 key 被拉真跑、race 降速下 90s 超时，非数据竞争、非本批代码）。按改动域口径过滤复跑三面全绿：tool/meditation ok 1.354s、./tests -run '^TestDeliverTool|^TestExternalMeditation' ok 2.920s、根 -run '^TestDeliverToAgent|^TestMeditation' ok 2.301s。CI 无 key 该组合法 SKIP，race job 以 CI 口径为准。
