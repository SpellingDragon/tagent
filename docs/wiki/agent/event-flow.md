# 事件流与上下文压缩机制

> 本文档用 Mermaid 图说明 tagent 事件驱动的完整流转：从外部消息进入、经 Runner 处理、MemoryPlugin 持久化、SessionProjection 维护、到 BeforeModel 回调链（ContextCompressor 统一压缩）最终调用 LLM 的全过程。

## 一、整体事件流

```mermaid
graph TD
    User["用户 / 外部输入"] -->|InjectMessage| Bus[EventBus<br/>persistentBus]
    Tool["工具回调"] -->|Publish| Bus
    SubAgent["子 Agent 结果"] -->|Publish| Bus
    Task["后台任务 settle<br/>(TaskManager)"] -->|task_settled Publish| Bus

    Bus -->|Pull 批量拉取| EL[runEventLoop]
    EL -->|① BuildInvocation| Msg[model.Message 合并]
    EL -->|② RunFlow| CM[ContextManager]
    CM -->|runner.Run| Runner[框架 Runner]

    Runner -->|emit event channel| OutCh[outputCh]
    OutCh -->|wechat-bot / HTTPAPI| Consumer[外部消费者]

    OutCh -.->|isFinalResponse| Bus
```

> **task_settled 回收 turn**：长命令 / 子 agent 经**任务层**异步执行，后台结算时 `TaskManager` 发一条自包含的 `task_settled` 事件（复用 `external_input` 类型，`source=task`）到 EventBus，像外部输入一样触发一个回收 turn——循环空闲则唤醒、进行中则排队（不打断当前 turn）。详见 `agent-architecture.md` §2.10 任务层。

## 二、Runner 内部流转

```mermaid
graph TD
    A[runner.Run] --> B[创建 / 复用 session]
    B --> C[追加用户消息到 session<br/>ResponseEvent RoleUser]
    C --> D[Plugin.OnEvent 链]
    D --> D1[SummaryPlugin<br/>注入 Tag + summary]
    D1 --> D2[MemoryPlugin<br/>持久化 + 写 StateDelta]
    D2 --> E[ContentRequestProcessor<br/>从 session.Events 构建 messages]
    E --> F[BeforeModel 回调链]
    F --> G[model.GenerateContent]
    G --> H[FunctionCallResponseProcessor<br/>工具执行 / ReAct 控制]
    H --> I[追加 response event]
    I --> J[EmitEvent 到 channel]
```

## 三、BeforeModel 回调链

当前实现将上下文重建收敛为**一个统一的 BeforeModel 回调**（外加一个诊断日志回调），按 4 步执行：

```mermaid
graph TD
    M[messages from ContentRequestProcessor] --> S1
    S1["Step 1: 持久化新 bus 事件<br/>EventBus.TryPull → persistBusEvent → projection"]
    S1 --> S2["Step 2: 解析投影<br/>ContextCompressor.Compress(refs)<br/>→ 压缩历史消息(带 [evt_KEY|type] 前缀)"]
    S2 --> S3["Step 3: 提取当前轮消息<br/>extractCurrentTurnMessages(filterUser)<br/>丢弃重复/回显的未前缀 user"]
    S3 --> S4["Step 4: 重建 = [system] + 历史 + 当前轮"]
    S4 --> DIAG["诊断回调: BeforeLLM 日志"]
    DIAG --> OUT[最终 Request.Messages → LLM]
```

**重建顺序即消息顺序**：`[system] + 压缩历史 + 当前轮`。因此**驱动请求必须先进入 projection**（顶层由框架 emit + SessionHook 转发；子 Agent 由 `Run()` 的 `persistBusEvent` 显式写入），才能作为「历史」的首条紧跟 system；否则它会停留为框架未前缀的 invocation seed，被 `extractCurrentTurnMessages` 当作「当前轮」拼到末尾，随 ReAct 累积被挤到最后。

**filterUser 语义**：当压缩历史中已含 user（即请求已入 projection）时，`extractCurrentTurnMessages` 丢弃框架重复插入的未前缀 user（session echo / invocation seed），避免重复；ReAct 内部消息（带 tool_calls 的 assistant + tool 结果）始终保留。

## 四、MemoryPlugin 持久化（含 nil-Response 过滤）

```mermaid
graph TD
    E[event.Event] --> N{evt == nil ?}
    N -- yes --> R1[return nil]
    N -- no --> P{Response 为空<br/>或 Choices 为空 ?}
    P -- yes --> R2["return evt（同步类事件，跳过）<br/>不生成 EventKey / StateDelta<br/>不写入 MemoryStore<br/>不进入 projection"]
    P -- no --> K["生成 Snowflake EventKey"]
    K --> I[inferEventInfo<br/>按 Role 推断 EventType + summary]
    I --> F["构建 FullEvent<br/>Content = msg.Content<br/>EventSummary = summary"]
    F --> S[StoreEvent 到 MemoryStore]
    S --> SD[写入 StateDelta:<br/>event_key / partition_id / event_type / event_summary]
    SD --> R3[return evt]
```

> 过滤逻辑是为避免框架在 ReAct 过程中发出的同步类事件（start / wait / barrier，无 payload）被误存为 `external_input` 空占位符，污染 projection。

## 五、SessionProjection 生命周期

```mermaid
graph TD
    E[框架 event.Event<br/>含 StateDelta] --> B[BuildEventReference]
    B --> OK{有 event_key ?}
    OK -- no --> SKIP[不追加]
    OK -- yes --> APP["projection.Append(ref)<br/>EventKey / PartitionID / EventType / EventSummary / Role"]
    APP --> PROJ[SessionProjection<br/>EventReference 数组]

    PROJ --> READ["ContextCompressor.Compress<br/>读取 refs"]
    PROJ --> KEYS["Callback 0: InjectEventKeys<br/>读取 refs 注入前缀"]

    READ --> COMPRESS["ContextCompressor<br/>解析 + 压缩 + 生成 retainedRefs"]
    COMPRESS --> REP["projection.Replace(retainedRefs)<br/>旧 refs 替换为新摘要 + 保留近期"]
```

## 六、ContextCompressor 统一压缩

```mermaid
graph TD
    IN["Compress(ctx, refs, currentMessages)"] --> T{"usedTokens ≤ threshold ?"}
    T -- yes --> P1["pass-through<br/>返回 currentMessages 不变<br/>返回 refs 不变"]

    T -- no --> RES["resolveRef 解析历史 refs<br/>MemoryStore 优先取完整 Content<br/>打上 [evt_KEY|type] 前缀"]
    RES --> MERGE["合并：historical + currentBody"]
    MERGE --> SC["SmartCompressor.Compress<br/>价值驱动 L0-L3 压缩<br/>保留 KeepRecentTasks 个近期段"]
    SC --> BR["buildRetainedRefs<br/>扫压缩后消息的 [evt_KEY] 前缀<br/>→ retainedKeys"]
    BR --> OUT["返回：Messages + RetainedRefs + Notices"]
```

> 关键点：历史消息（`resolveRef` 解析得到）和当前消息（`InjectEventKeys` 注入过前缀）都带 `[evt_KEY|type]`，`buildRetainedRefs` 据此判断哪些投影 ref 被压缩、哪些被保留，最后调用 `projection.Replace(retainedRefs)` 一次原子替换。

## 七、一次完整请求的端到端时序

```mermaid
sequenceDiagram
    participant U as 用户
    participant Bus as EventBus
    participant EL as runEventLoop
    participant CM as ContextManager
    participant R as Runner
    participant MP as MemoryPlugin
    participant Proj as SessionProjection
    participant CC as ContextCompressor
    participant LLM as LLM

    U->>Bus: InjectMessage(user)
    Bus->>EL: Pull 拉取事件
    EL->>CM: BuildInvocation + RunFlow(msg)
    CM->>R: runner.Run
    R->>MP: Plugin.OnEvent(user event)
    MP-->>Proj: projection.Append(ref)
    R->>R: ContentRequestProcessor 构建 messages
    R->>Proj: InjectEventKeys 注入前缀
    R->>CC: ContextCompressor.Compress
    CC-->>Proj: projection.Replace(retainedRefs)
    R->>LLM: GenerateContent
    LLM-->>R: response / tool_calls
    R->>MP: Plugin.OnEvent(assistant/tool events)
    MP-->>Proj: projection.Append(refs)
    R-->>CM: emit event channel
    CM-->>Bus: final response echo (agent_output)
    CM-->>U: outputCh → consumer
```

## 八、压缩前后投影变化示例

| 阶段 | SessionProjection 内容 |
|------|----------------------|
| 压缩前 | `[ref1(user), ref2(asst), ref3(tool), ref4(user), ref5(asst), ref6(tool), ref7(user), ref8(asst)]` |
| KeepRecent=2，L3 压缩旧段 | `[summaryRef(context_compress), ref5(asst), ref6(tool), ref7(user), ref8(asst)]` |
| 注入前缀后 LLM 看到 | `[evt_summary|context_compress]...`、`[evt_5|agent_output]`、`[evt_6|action_command]`、`[evt_7|external_input]`、`[evt_8|agent_output]` |

> 旧事件被压缩为一个 summary ref（含所有被压缩 event key 清单），近期事件保留，LLM 可通过 `recall(event_keys=[KEY])` 按需检索完整内容。
