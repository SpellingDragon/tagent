## 1. 核心哲学与三层模型统一

- [x] 1.1 在 README.md "设计哲学"章节添加核心设计哲学和三个不变量：① inputs 是投影（有界，读写同一份数据）② Compact 修改投影不修改事件流 ③ 工具结果回写 bus 不直接操作 inputs。标注原型隐含的三个设计要点：所有输出回写 bus、model 作为工具、批量处理
- [x] 1.2 在 README_EN.md 同步 1.1 的英文版
- [x] 1.3 在 agent-architecture.md §1 模块定位章节添加原型哲学说明：tagent 的核心设计源于 prototype/agent.go，扩展保留了三个不变量，OnEvents/Compact/Run 可替换的框架骨架
- [x] 1.4 在 agent-architecture.md §4 修正层2 描述：从"Session Events（工作内存，完整未压缩）"改为"Session Events（投影，有界工作内存）"，标注当前实现偏差
- [x] 1.5 在 agent-architecture.md §4 层间数据流图修正：去掉"AgentLoop 自己维护的 session copy"表述，改为"onEvent 追加 EventReference 到 Session（投影）"
- [x] 1.6 在 memory-architecture.md §4.3 确认 EventReference 描述与设计目标一致，添加"当前实现偏差"标注
- [x] 1.7 在 event-architecture.md 三层模型描述中同步层2 为"投影，有界工作内存"
- [x] 1.8 在 README.md "关键约束"章节修正：删除"AgentLoop 直接维护 Session.Events"和"sessionSvc.AppendEvent 仍被调用"的描述，改为"onEvent 追加 EventReference 到 Session，Preprocessor 从同一份 Session.Events 读取"

## 2. 视图转换原则与 Compact 机制

- [x] 2.1 在 agent-architecture.md §12.2 修正"视图转换原则"：明确区分三者的可变性——messages（SmartCompressor 可修改）、Session.Events（Compact 可清理）、MemoryStore（永不可变）
- [x] 2.2 在 README.md "两阶段上下文压缩"章节添加 Compact 职责说明：SmartCompressor 压缩 LLM 视图，Compact 清理 Session 投影，两者协作顺序（SmartCompress 先，Compact 后）
- [x] 2.3 在 README_EN.md 同步 2.2 的英文版修正
- [x] 2.4 在 agent-architecture.md §9 SmartCompressor 章节后新增 §9.5 Compact 机制设计：触发条件、清理策略、与 SmartCompressor 的协作顺序、与 onEvent 持久化的时序关系（onEvent 实时持久化 → Compact 安全清理投影）、对比表
- [x] 2.5 在 agent-architecture.md §12.4 "onEvent 回调的写入原子性"修正：删除"AgentLoop 直接追加到 al.session.Events"的描述，改为"onEvent 五步协同追加 EventReference 到 Session"

## 3. onEvent 五步协同与事件生命周期

- [x] 3.1 在 agent-architecture.md 新增"一条事件的完整生命周期"章节：展示 onEvent 五步协同（事件提取→记忆写入→因果链→StateDelta→投影追加）、Preprocessor 构建 messages（从 EventReference 按需拉取）、Model + handleResponse（输出回写 bus）
- [x] 3.2 在 agent-architecture.md §7.3.1 修正 AgentLoop.Run 伪代码：区分设计目标和当前实现。设计目标：Step 1 处理 external_input（onEvent 五步+Session 追加），Step 2 Preprocessor，Step 3 Model，Step 4 handleResponse（含 dispatch）。当前实现标注为偏差
- [x] 3.3 在 agent-architecture.md §7.3.2 序列图修正：去掉重复行，改为五步协同 + Preprocessor 按需拉取
- [x] 3.4 在 README.md "持久事件循环"流程图修正：将"onEvent (MemoryStore + SessionService) then append to al.session.Events"改为"onEvent 五步协同（持久化+因果链+StateDelta+追加 EventReference）"
- [x] 3.5 在 README_EN.md 同步 3.4 的英文版修正
- [x] 3.6 在 README.md "事件驱动记忆"章节补充 onEvent 五步协同说明
- [x] 3.7 在 agent-architecture.md §7.3.3 InjectMessage 描述确认（已正确，确认无需修改）
- [x] 3.8 在 agent-architecture.md 补充 Preprocessor 从 EventReference 按需拉取的说明：最近引用从 MemoryStore 拉取完整 Content，旧引用直接用 EventSummary
- [x] 3.9 在 agent-architecture.md 补充"所有输出回写 bus"说明：handleResponse 的 tool_use 和 final 都回写 bus/outputCh，与工具结果回写 bus 一致
- [x] 3.10 在 agent-architecture.md 补充"批量处理"说明：Pull 批量取出所有待处理事件，一轮处理多个事件

## 4. Compact 与 MaxToolIterations 的主次关系

- [x] 4.1 在 agent-architecture.md §5.2 TagentConfig 修正 MaxToolIterations：标注 Compact 为主 MaxToolIterations 为辅，子 agent 默认 10，复用框架 Invocation.MaxToolIterations 字段，当前 200 为偏差
- [x] 4.2 在 agent-architecture.md 补充两种模式的控制策略：StartLoop 模式 Compact 为唯一控制阀门；Run() 模式 Compact + MaxToolIterations 协同
- [x] 4.3 在 agent-architecture.md §7.2.1 确认统一 dispatch 路径描述（已正确，确认无需修改）
- [x] 4.4 在 agent-architecture.md §7.2.1 子 agent 停止条件确认：只检查 tool_calls，不检查 Content（已正确，确认无需修改）
- [x] 4.5 在 agent-architecture.md §7.2.1 添加子 agent Session 有界性说明：子 agent 的 Session.Events 也有界，Compact 同样适用
- [x] 4.6 在 README.md "子 Agent 调用"章节确认 activeBus 切换描述正确（已正确，确认无需修改）

## 5. model 与 tool 的映射关系 + tagent 与框架结合边界

- [x] 5.1 在 agent-architecture.md 补充 model 与 tool 的映射说明：原型中 `tools["model"] = ModelCompletion`（model 是工具的一种），生产中 model 独立因框架接口差异（GenerateContent 返回 streaming channel vs Call 返回同步结果），本质一致：AgentLoop 是唯一调用者
- [x] 5.2 在 README.md 确认或补充 model 与 tool 关系说明
- [x] 5.3 在 agent-architecture.md 新增"tagent 与 trpc-agent-go 结合边界"章节，包含：
  - 该做什么（复用）：Agent/Model/Tool/Plugin/Session/Event/Invocation 接口原语 + Flow.Run(ReAct 循环) + ContentRequestProcessor(从 session 构建 messages) + FunctionCallResponseProcessor(工具执行+迭代控制) + BeforeModel(压缩 hook)
  - 不该做（重复造轮子）：AgentLoop.Run(~500行, 框架 Flow.Run 已有) / Preprocessor.Process(~250行, 框架 ContentRequestProcessor 已有) / dispatchToolUse(~100行, 框架 FunctionCallResponseProcessor 已有) / handleResponse(~80行, 框架 processStreamingResponses 已有)
  - tagent 独有（框架没有）：EventBus+StartLoop / MemoryStore(FullEvent+因果链) / InjectMessage / Compact / MeditationManager / TrajectoryRecorder
  - 深层结合方向：tagent = StartLoop + EventBus + LLMAgent.Run(Flow.Run) + MemoryStore + Compact + MeditationManager
  - 深层结合收益：可减少 ~1000 行代码，获得框架 tracing/telemetry/jsonrepair 能力
  - 深层结合挑战：tmux 异步模型变化（Flow.Run 结束后回到 StartLoop, tmux 结果作为下一条消息进入）、Compact 需在 BeforeModel 中实现
  - 关键原则：tagent 独有的只在持久循环和异步注入层面，ReAct 循环本身应复用框架 Flow
- [x] 5.4 在 README.md 补充 tagent 与框架关系的简要说明
- [x] 5.5 在 agent-architecture.md 标注 Session 存 EventReference 是有意识偏离框架 Session.Events 设计的决策，理由是 tagent 有 MemoryStore 作为完整事件存储

## 6. 数据流图与场景演练统一

- [x] 6.1 在 agent-architecture.md §4 层间数据流图修正：所有"al.session.Events"改为"Session.Events（投影）"，去掉"AgentLoop 自己维护的 session copy"
- [x] 6.2 在 agent-architecture.md §7.1 序列图修正：去掉"AgentLoop 维护自己持有的 session copy"Note，改为"onEvent 五步协同追加 EventReference 到 Session（投影）"
- [x] 6.3 在 README.md 场景演练表格修正：第 2 行改为"onEvent（五步协同：事件提取+记忆写入+因果链+StateDelta+追加 EventReference）"
- [x] 6.4 在 README_EN.md 同步 6.3 的英文版修正
- [x] 6.5 在 README.md 场景演练表格第 4 行修正：从"从 al.session.Events 构建 messages"改为"从 Session.Events（投影）构建 messages，按需从 MemoryStore 拉取完整内容"
- [x] 6.6 在 README_EN.md 同步 6.5 的英文版修正

## 7. 关键设计决策章节修正

- [x] 7.1 在 agent-architecture.md §12.1 确认 Preprocessor vs BeforeModel 对比表正确（已正确，确认无需修改）
- [x] 7.2 在 agent-architecture.md §12.2 修正视图转换原则（见 Task 2.1）
- [x] 7.3 在 agent-architecture.md §12.3 确认 EventBus 有序性描述正确（已正确，确认无需修改）
- [x] 7.4 在 agent-architecture.md §12.4 修正 onEvent 写入原子性（见 Task 2.5）
- [x] 7.5 在 agent-architecture.md §12.5 确认 Memory 数据隔离与 EventKey 设计正确（已正确，确认无需修改）

## 8. 插件文档同步

- [x] 8.1 在 plugin-architecture.md 确认 MemoryPlugin.OnEvent 描述：五步协同（事件提取→记忆写入→因果链→StateDelta→投影追加），如有需要修正
- [x] 8.2 在 plugin-architecture.md 确认 SummaryPlugin 描述正确（已正确，确认无需修改）

## 9. 已知技术债标注

- [x] 9.1 在 agent-architecture.md §4 层2 添加"当前实现偏差"标注：Session.Events 实际存储 []event.Event，设计目标为 EventReference[]
- [x] 9.2 在 agent-architecture.md §5.2 TagentConfig 添加偏差标注：当前默认 MaxToolIterations=200 且无 Compact，设计目标为 Compact 为主 MaxToolIterations=10 为辅
- [x] 9.3 在 agent-architecture.md §12.4 添加"当前实现偏差"标注：AgentLoop 因 SessionService clone 语义维护独立 session copy，设计目标为消除 copy
- [x] 9.4 在 agent-architecture.md 添加 AgentLoop.Run Step 顺序偏差标注：当前 dispatch 在 Step 1，设计目标 dispatch 在 handleResponse
- [x] 9.5 在 README.md 添加"已知实现偏差"小节或在相关章节内联标注

## 10. 验证与归档

- [x] 10.1 用 prototype 三个不变量逐条校验所有文档：① inputs 是投影（有界，读写同一份数据）— 搜索所有 Session.Events 描述是否含"投影""有界"；② Compact 修改投影不修改事件流 — 搜索所有 Compact 描述是否区分 SmartCompressor；③ 工具结果回写 bus 不直接操作 inputs — 搜索所有工具回写描述是否走 bus
- [x] 10.2 校验原型三个设计要点是否在文档中体现：所有输出回写 bus、model 作为工具、批量处理
- [x] 10.3 校验 onEvent 五步协同是否在所有相关文档中一致描述
- [x] 10.4 校验 tagent 与 trpc-agent-go 结合边界是否在文档中明确：该做什么（7 项复用）、不该做什么（5 项不用）、关键原则（用框架积木搭建不同执行引擎）
- [x] 10.5 全文档交叉检查：搜索"完整未压缩"、"session copy"、"al.session.Events"、"MaxToolIterations.*200"等关键词，确保无遗漏
- [x] 10.6 全文档交叉检查：搜索"onEvent.*Session"、"append.*Session"、"写入 Session"等模式，确保描述统一
- [x] 10.7 验证所有 mermaid 图中 Session.Events 的描述一致
- [x] 10.8 验证所有伪代码中事件处理步骤的编号和顺序一致，且区分"设计目标"和"当前实现偏差"
- [x] 10.9 验证 design.md 的扩展映射表（prototype 组件 → 正确扩展 → 偏差）在所有文档中一致
- [x] 10.10 归档变更到 openspec/changes/archive/
