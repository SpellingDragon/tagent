# 原型骨架：126 行的六件套与它的生产映射

`prototype/agent.go` 是 tagent 最初的可运行骨架，**刻意保持最小且自足**。它存在的价值不是历史陈列：生产实现至今仍遵循它定义的抽象，而"一个 agent 可以由这几件东西构成"这一命题是它证明的。本页承载它的构件清单、到生产原语的映射，以及三条被生产实现继承的不变量。

<a id="six-pieces"></a>
## 一、六件套

| 构件 | 职责 |
|---|---|
| `eventBus` | 有序事件队列，把生产者与循环解耦 |
| `inputs` | 事件流的**有界投影**（工作记忆） |
| `tools` | 可调函数，其输出被喂回 `eventBus` |
| `model` | 只是工具之一，在 `inputs` 非空时被调用 |
| `Run` | 持久事件循环：Pull → OnEvents → model → publish |
| `Compact` | 重置有界投影，**不触碰事件总线** |

<a id="production-mapping"></a>
## 二、到生产实现的映射

生产代码把这些构件映射到 trpc-agent-go 的原语上，语义保持不变：

| 原型 | 生产 |
|---|---|
| `eventBus` | `agent.EventBus` |
| `DefaultRun` | `TagentAgent.runEventLoop` |
| `OnEvents` | `ContextManager.BuildInvocation` + `onEvent` 回调 |
| `Compact` | `agent.Compactor` + `SmartCompressor` |
| `ModelCompletion` | 框架 `runner.Run` 配 `llmagent` |
| `tools["model"]` | 由 `runner.Run` 集成的框架 LLM 工具 |
| 其余 `tools[...]` | 注册的 trpc-agent-go 工具 |

`BaseTAgent` 因此只演示"事件驱动 agent 所需的最小状态"：串行化 `inputs` 访问的互斥锁、进出事件与内部事件的总线、工具注册表、有界 `inputs` 切片（投影）、一次模型补全函数，以及生命周期钩子 `Run`/`ModelCompletion`/`Compact`/`OnEvents`。生产 `TagentAgent` 保留同一批概念件，另加持久化、压缩与 A2A。

<a id="inherited-invariants"></a>
## 三、被继承的三条不变量

1. `inputs`（生产即 `SessionProjection`）是事件流的**投影**，不是第二份真相；
2. `Compact` 只重置投影，**绝不触碰** `MemoryStore` 与事件总线；
3. 工具输出与模型输出**一律经事件总线回流**，没有旁路通道。

这三条在原型里是可读性最强的形态，在生产里由各自的契约测试守护；改任何一条都要同时解释原型侧与生产侧为什么仍然同源。
