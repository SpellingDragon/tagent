# Design: host-send-tool

基线 `5e8398d`（dev）。

## D1 工具形态（deliver 裁决的宿主平移）
`examples/wechat-bot/send_tool.go`：实现框架 `tool.Tool`，声明名 `send`，参数 `{content: string(required)}`；执行经注入的 `HostSendFunc func(content string) (string, error)`（main.go 装配闭包捕获 bot 实例与目标解析，LLM 只给正文）。失败映射 `[send_denied] <原因>；目标规则：本会话盖章 > 最近活跃` 风格文本；成功 `[send_ok] 已送达（目标 <label>）`。err 仅限参数协议错（对齐 deliver/governance 先例：业务拒绝是 result 不是 error）。

## D2 发送通路复用（零第二通路）
HostSendFunc 内部即 main.go 输出循环的既有分支抽取：resolveDeliveryTarget 三级 → typing 清理 → >2000 长文 context-token 拆分（失败截断）→ SendTextToUser → DeliverFiles 附件 → 失败 emitReceipt(send-failed)。抽取为可复用函数（main.go 与工具共用），**行为逐字节同现状**——被动通道的发送路径与主动通道汇合于同一实现。

## D3 上界与防滥用
content ≤8KiB（超限 `[send_denied]` 具名拒）；不暴露 chat_id 参数（单主人；若未来多主人再议 target 白名单——注记，不在本变更）。频次不设框架闸：滥用面=给用户发多余消息，由提示词约束+用户可见即纠偏，与现有 task 通知同级。

## D4 授予与装配
yaml ToolRef（kind: tool, id: send）显式声明——不配不可见（能力即授权，同 deliver）。装配注入经既有扩展点：实施期 grep factory/WithTool 先例（kb/知识注入等），选最惯例的一条，战报申报。

## D5 提示词心智对齐
TOOLS.md（或等价）：一句"send=主动送达用户的唯一显式通道；你在非用户回合想说的话不会被自动送达"；meditator_agent.md：反思卡片的交付选择（deliver 给 agent / send 给用户）语义区分一句。

## D6 验收
单测（wechat-bot 模块内）：参数校验/上界/拒绝文本/成功回执/发送通路复用断言（mock bot）。e2e：mock model 脚本化一次 send tool call → mock bot 收到内容（断言经同一发送函数）。被动通道回归：既有 main.go 输出路径行为不变（测试或代码审读证明共用同一函数）。门禁：bot 模块 build/vet/test、根包零回归、lint(Go1.24)、check-openspec。
