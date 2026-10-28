# 设计决策

D1 Wave2Data 断言随契约：受理回执最小化（AdmissionReceipt{CallID,TaskID,Accepted,Kind}→"{callID taskID accepted kind}"）是 delivery-review 变更的正式行为；断言改形状（前缀 {call-delegate-、含 " true "、保持 denied 拒收断言）。
D2 Wave2Recall 断言随语义：recall_recent 返回链上最近 N 条事实，运行时自产事件（thinking_plan/task_settled 等）入链为真实语义；恒 count=2 只在链上仅夹具的旧世界成立。改下界断言（count>=2）+ waitText 改扫前缀。
D3 mcp 升级：v0.0.18；先 build+适配核对（tool/mcp 是唯一消费面），删 U5 双签名后 race 全表复跑；若升级破坏 API 则回落并留档。
D4 tmux 拆分：按职责切 tmux_session.go（session 名单/存在性/tmux 命令执行）与 tmux_command.go（命令行/退出记录构造），主文件保留 Executor 接口实现；三文件均 <500 行；纯移动零行为变更。
D5 文档悬空：就地修指当前实现（policy.go Policy.Judge、approval.go 当前位置）；refine/asset_drift 已删能力行移除；agent-architecture 第三节表改当前树（只修悬空行不重写整节）。
D6 data-less 残留：examples/wechat-bot/skills/interview/ 下未跟踪空文件，mv 至 /tmp/tagent-datalss-backup/ 留证后出仓。
