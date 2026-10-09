# Design: complete-meditation-loop

基线 `d9372b8`（main=dev 树）。

## D1 工具落点与接线（分层合法）
`tool/meditation/deliver_tool.go`：实现 `tool.Tool`，声明名 `deliver`，参数 `{target: string, content: string}`；执行经装配注入的 `DeliverFunc func(target string, msg model.Message) (string, error)`（根包 `build_agent` 绑定为 `DeliverToAgent(meditator, target, targetLoopSession, msg)`——from 由**闭包捕获**，非 LLM 参数）。tool/ 不 import agent/根包（闭包注入），方向断言测试自动覆盖。`targetLoopSession` 取装配期登记的 `meditation` 保留名（自述头仍带，路由仍按实例）。

## D2 授予条件（能力即授权）
`deliver` 工具仅当该 agent `meditation.enabled ∧ deliver_to 非空` 时挂进其工具面（装配期决定，不配则 LLM 根本看不到此工具）。工具内调用时**再**过投递缝四道门（白名单/盲投/未知/未运行）——装配授权是能力面，调用校验是安全面，两道都要。

## D3 错误模型=结果文本
四道拒绝与渲染失败一律作为 tool result 文本（`[delivery_denied] <具名原因>；你的白名单是 [...]` 风格，与 governance 拒绝同取向：模型读得到才能自纠），MUST NOT 抛 Go error 断回合。成功返回目标名与"已进入 mailbox（同批用户输入时让位）"的语义提示。

## D4 上界与可观测
content ≤ 8KiB（超限具名拒）；每次成功/被拒计数经工具面日志（run 级 debug 即可，不建第二账本）。被投递消息仍走 `InjectMessageWithSource("meditation")`——谱系、遥测自管、同批让位、目标 novelty 不 re-arm 全部自动继承，MUST NOT 新开注入路径。

## D5 改名清单
示例 agent 名 `curator→meditator`；session 保留名 `curation→meditation`；提示词 `curator.md/curator_agent.md → meditator.md/meditator_agent.md`（git mv）；main.go `curationAgentName/curationSession/curationStop` 相应改名；README 双语"策展/curation"叙述词降为"旁路冥想/冥想线"（中文语义描述，不造新英文术语）；wiki §2.14/runtime/platform-subsystems 同步；spec MODIFIED 仅措辞。撞名检查：`meditation` 作 session 名与谱系常量 `LineageMeditation="meditation"` 是**不同命名空间**（session 是循环身份、trigger_source 是事件属性），亲和而非冲突，注记说明。被否方案：保留 curator（概念税继续）；改名 `meditation-session` 之类长名（啰嗦）。

## D6 验收
单测：tool 参数校验/上界/拒绝转文本；装配：未配 deliver_to 的 agent 不见工具（能力面测试）；e2e：mock model 脚本化一次 `deliver` tool call → 目标真实消费卡片（替代手工 API 调用路径）；既有 TestDeliverToAgent*/TestExternalMeditation* 不回归；真实模型不重跑。spec 对账 F3。

## D7 风险
LLM 自主投递=能力扩大，防线是"白名单装配期+调用期双查 + 目标未运行天然拒绝"；最坏情形是白名单内目标收到噪声卡片——与手工投递同级，不新增面。
