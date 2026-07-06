# tagent

A Go-based agent framework embracing event-driven, memory-centric design, built on top of [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go).

[中文文档](README.md)

## Overview

tagent is not a from-scratch agent framework. Built on top of [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go), it replaces the framework's synchronous React Loop with an **event-driven execution engine** (EventBus + AgentLoop + Preprocessor), and **injects** differentiated capabilities — context compression, event persistence, causal memory — through the framework's extension points (OnEvent plugins).

The result is a persistent, event-driven agent that can run as:
- A **persistent event loop** (continuously receives events, processes in batches, waits for next)
- An **A2A server** (exposed to remote agents via A2A protocol)
- An **RL rollout worker** (integrated with AReaL for PPO online training)
- A **trajectory recorder** (records every LLM call to JSONL via `TrajectoryRecorder`, convertible to AReaL SFT/RL datasets for offline training)

## Design Philosophy

tagent's core design philosophy originates from [prototype/agent.go](prototype/agent.go) (126-line abstract implementation). The prototype defines an extensible framework skeleton using replaceable function fields (`OnEvents`, `Compact`, `Run`), with the core philosophy: **inputs is a projection of the event flow, the event bus carries the event flow, when inputs is full it triggers Compact and memory persistence**.

### Three Invariants

| Invariant | Meaning | Prototype |
|-----------|---------|-----------|
| **① inputs is projection** | Bounded working memory, read and write the same data | OnEvents append, ModelCompletion read, Compact clear — all operate on same inputs |
| **② Compact modifies projection** | Does not modify event flow (bus) or permanent storage | `inputs = inputs[:0]` — clear projection, events already flowed through bus |
| **③ Tool results write back to bus** | Does not directly manipulate inputs | goroutine executes tool → result goes to bus → next OnEvents appends |

### Implicit Design Points

- **All outputs write back to bus**: OnEvents return value is also written to eventBus, same path as tool results
- **model as tool**: Prototype registers `tools["model"] = ModelCompletion`, model and other tools share the same call path (in production, model is independent as `model.Model.GenerateContent` due to framework interface differences)
- **Batch processing**: After Pull gets the first event, non-blocking drain all remaining events into a batch

### Relationship with trpc-agent-go

tagent reuses the framework's interface primitives (Agent, Model, Tool, Plugin, Session, Event, Invocation), extending at the persistent loop and async injection layer. tagent's unique components are only EventBus + StartLoop + MemoryStore + Compact + MeditationManager + TrajectoryRecorder; the ReAct loop itself should reuse the framework's Flow.

> Detailed design rationale: [agent-architecture.md §1](docs/wiki/agent/agent-architecture.md)

## Core Concepts

### Memory as the Brain

tagent's central design philosophy is **memory-driven, event-decoupled execution**:

Three layers of data representation, each with a distinct purpose:

| Layer | Location | Responsibility | Lifetime |
|-------|----------|---------------|----------|
| **EventBus AgentEvent** | Agent memory | Event flow (source of truth), distinguishes external_input / tool_use | Publish → Pull, then discarded |
| **Session.Events EventReference[]** | session.Session | Projection (bounded working memory), sole history source for Preprocessor | During session lifetime, Compactable |
| **MemoryStore FullEvent** | File/DB | Permanent storage (immutable), recall tool queries | Permanent |

> **Known implementation deviation**: Current production code stores `[]event.Event` (full events with `*model.Response`) in Session.Events, not EventReference. Design goal is EventReference[] (lightweight reference: key + type + summary), with full data in MemoryStore.

```mermaid
graph TB
    subgraph "Layer 1: EventBus (event flow, temporary)"
        EB["AgentEvent<br/>(external_input/tool_use)"]
    end

    subgraph "Layer 2: Session.Events (projection, bounded)"
        SESS["EventReference[]<br/>(key + type + summary)"]
    end

    subgraph "Layer 3: MemoryStore (permanent, immutable)"
        MS["FullEvent<br/>(causal chain + full content)"]
    end

    EB -->|"onEvent 5-step pipeline"| SESS
    EB -->|"onEvent 5-step pipeline"| MS
    SESS -->|"Preprocessor fetches on demand"| LLM["[]model.Message<br/>(LLM Context)"]
    MS -->|"recall tool query"| TOOL["Tool"]

    style EB fill:#e3f2fd,stroke:#1565c0
    style SESS fill:#f3e5f5,stroke:#7b1fa2
    style MS fill:#e1f5ff,stroke:#0277bd,stroke-width:3px
    style LLM fill:#fff3e0,stroke:#ef6c00
```

**Key constraints**:
- **Session.Events is a projection** — Bounded working memory, not full event storage. onEvent appends EventReference (lightweight reference), Preprocessor reads from the same Session.Events. Read and write the same data, no copy maintained
- **onEvent 5-step pipeline** — On each event: ①ExtractEventType (infer type) → ②MemoryStore.StoreEvent (persist FullEvent) → ③RelationStore.SetParent (causal chain) → ④StateDelta fill (event_key/event_type) → ⑤Session.Events.append (EventReference)
- **Compact cleans projection** — When Session.Events exceeds limit, clean old references replacing with summary reference, MemoryStore untouched (already persisted by onEvent)
- **Preprocessor fetches on demand** — When building messages from EventReference, recent references fetch full Content from MemoryStore, old references use EventSummary directly
- **MemoryStore is the sole complete event chain** — Agent and Tool access on-demand via EventKey

### Event Classification

Every interaction — including internal operations like compression and tool calls — is an event:

| Category | Event Type | Trigger | Stored in Session? |
|----------|-----------|---------|-------------------|
| **External** | `external_input` | User message, API call, TmuxMonitor injection | Yes (as EventReference) |
| **External** | `agent_output` | Agent's final response (no tool_calls) | Yes |
| **Action** | `action_command` | Tool/command execution result | Yes |
| **Thinking** | `thinking_plan` | Agent planning (assistant with tool_calls) | Yes |
| **Thinking** | `thinking_recall` | Memory recall via RecallAgent | Yes |
| **Thinking** | `thinking_knowledge` | Knowledge retrieval via KnowledgeAgent | Yes |
| **Internal** | `context_compress` | SmartCompressor drops old segments | No (view-only) |

> `context_compress` is a view transformation — it modifies the LLM message list but does not create a Session event.

### Traditional vs tagent

| Dimension | Traditional Agent | tagent |
|-----------|------------------|--------|
| Context passing | Layer-by-layer via function parameters | Shared via MemoryStore + EventKey |
| Agent's view | Knows the complete execution flow | Knows only current task + event keys |
| Tool's view | Passive executor, depends on Agent for context | Autonomously accesses MemoryStore via `event_key` |
| Memory role | Optional component | Core hub (single source of truth) |
| Event granularity | Coarse (request/response) | Fine-grained (every action/thinking) |
| Context overflow | Hard limit or simple truncation | Two-stage compression (task boundary + LLM summary) |

> Details: [event-architecture.md](docs/wiki/event/event-architecture.md), [memory-architecture.md](docs/wiki/memory/memory-architecture.md)

## Architecture

### Module Overview

```mermaid
graph TB
    subgraph "tagent (root)"
        NEW["tagent.New()<br/>Composition Root"]
    end

    subgraph "tagent/agent"
        TA["TagentAgent<br/>(Composition Root)"]
        EB["EventBus<br/>(event queue)"]
        AL["AgentLoop<br/>(Pull-Process-Model-Dispatch)"]
        PP["Preprocessor<br/>(filter+build+compress)"]
        SC["SmartCompressor<br/>(2-stage compression)"]
    end

    subgraph "tagent/plugin"
        MP["MemoryPlugin<br/>(persistence + causal chain)"]
        SP["SummaryPlugin<br/>(event tag injection)"]
    end

    subgraph "tagent/memory"
        MS["MemoryStore<br/>(InMemory / FileSegmentStore)"]
        RS["RelationStore<br/>(causal chain)"]
    end

    subgraph "tagent/tool"
        KA["KnowledgeAgent"]
        RA["RecallAgent"]
        CT["ActionTool + TmuxMonitor"]
    end

    subgraph "tagent/event"
        ET["Event Types + Summary"]
    end

    subgraph "tagent/prompt"
        PL["Prompt Loader"]
    end

    subgraph "trpc-agent-go (framework)"
        RUN["Runner"]
        LLMA["LLMAgent<br/>(React Loop Flow)"]
        SESS["Session"]
    end

    subgraph "External"
        LLM["model.Model"]
        A2A["A2A Remote Agents"]
    end

    NEW --> TA
    NEW --> KA
    NEW --> RA
    NEW --> CT

    TA --> RUN
    RUN --> LLMA
    RUN --> SESS
    RUN --> MP
    RUN --> SP

    LLMA --> CI
    CI --> SC
    LLMA --> LLM

    MP --> MS
    MP --> ET
    SP --> ET
    MS --> RS

    KA --> MS
    RA --> MS
    CT -->|InjectMessage| TA

    TA -.->|A2A Server| A2A
```

### Core Modules

| Module | Responsibility | Wiki |
|--------|---------------|------|
| `agent/` | Event-driven engine: `EventBus` (per-agent event queue), `AgentLoop` (Pull-Process-Model-Dispatch pure engine), `Preprocessor` (event filtering+message building+compression), `SmartCompressor` (2-stage compression), `AgentToolWrapper` (sub-agent wrapper), `MeditationManager` (meditation heartbeat) | [agent-architecture.md](docs/wiki/agent/agent-architecture.md) |
| `memory/` | Structured event storage: `InMemoryStore`, `FileSegmentStore` (L0-L3 layered), `RelationStore` (causal chain), `Compactor`, `Tombstone`, `Lifecycle` (TTL) | [memory-architecture.md](docs/wiki/memory/memory-architecture.md) |
| `plugin/` | Framework plugins: `MemoryPlugin` (event persistence + causal chain), `SummaryPlugin` (event tag injection) | [plugin-architecture.md](docs/wiki/plugin/plugin-architecture.md) |
| `tool/` | Callable tools: `KnowledgeAgent` (RAG), `RecallAgent` (memory recall), `ActionTool` (shell/tmux execution + TmuxMonitor) | [tool-architecture.md](docs/wiki/tool/tool-architecture.md) |
| `event/` | Event type system: type inference (`ExtractEventType`), summary generation (`GenerateEventSummary`), strict no-truncation policy | [event-architecture.md](docs/wiki/event/event-architecture.md) |
| `prompt/` | Prompt template loader: single file, directory, composite, bootstrap-style loading | [prompt-architecture.md](docs/wiki/prompt/prompt-architecture.md) |
| `config.go` | Declarative config: YAML/JSON serializable `Config` → `AgentConfig` → `MemoryConfig` / `ToolRef` | — |
| `tagent.go` | Composition root: `tagent.New(cfg, opts...)` wires Config + runtime options into a complete agent | — |

### Module Dependencies

```mermaid
graph TD
    ROOT["tagent (root)<br/>config.go + tagent.go + builtin.go"]
    AGENT["agent/"]
    PLUGIN["plugin/"]
    MEMORY["memory/"]
    CMD["tool/action/"]
    RECALL["tool/recall/"]
    KNOW["tool/knowledge/"]
    EVENT["event/<br/>(zero external deps)"]
    PROMPT["prompt/"]

    ROOT --> AGENT
    AGENT --> PLUGIN
    PLUGIN --> MEMORY
    ROOT --> CMD
    ROOT --> RECALL
    ROOT --> KNOW
    CMD --> MEMORY
    RECALL --> MEMORY
    KNOW --> MEMORY
    ROOT --> EVENT
    ROOT --> PROMPT
```

All dependencies are one-way, no cycles.

## Core Mechanisms

### 1. Persistent Event Loop

tagent's core runtime model. The agent acts as a persistent, OS-like process: continuously receiving events (user input, TmuxMonitor callbacks), processing them in batches, and waiting for the next batch.

```mermaid
graph LR
    START["StartLoop<br/>(userID, sessionID)"] --> PULL["EventBus.Pull<br/>batch all pending events"]
    PULL --> ONEVT["onEvent 5-step pipeline<br/>(persist + causal + StateDelta + append EventReference)"]
    ONEVT --> PROC["Preprocessor.Process<br/>read Session.Events (projection) + compress"]
    PROC --> MODEL{"shouldCallModel?"}
    MODEL -->|"Yes"| CALL["model.GenerateContent"]
    MODEL -->|"No"| DISPATCH["dispatch tool_use<br/>(async goroutine)"]
    CALL --> RESP["handleResponse"]
    RESP -->|"tool_calls"| ONEVT2["emitEvent (onEvent + session append)<br/>+ bus.Publish(tool_use) + dispatch (async)"]
    RESP -->|"final"| EMIT["emit → outputCh"]
    ONEVT2 --> PULL
    EMIT --> PULL
    DISPATCH --> PULL

    style CALL fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
```

Key design: onEvent executes the 5-step pipeline (event extraction → memory write → causal chain → StateDelta → projection append) on each event, then Preprocessor reads from the same Session.Events. Read and write the same data, no copy maintained.

> Details: [agent-architecture.md §7](docs/wiki/agent/agent-architecture.md)

### 2. Context Compression and Projection Cleanup

tagent has two independent context management operations:

**SmartCompressor (compress LLM view)**: Activates when token usage exceeds threshold (`MaxTokens * CompressThreshold`):
- **Stage 1 — Task boundary drop**: Split messages into `TaskSegment`s. Drop old segments, keep recent N (`KeepRecentTasks`, default 2)
- **Stage 2 — LLM summary** (optional): Generate batched LLM summaries of dropped segments
- **Target**: `[]model.Message` (LLM view), does not modify Session.Events or MemoryStore

**Compact (clean Session projection)**: Triggered when SmartCompressor still exceeds limit after compression:
- **Strategy**: Split Session.Events by task boundaries, keep recent N complete tasks' EventReference, replace old references with summary reference (includes compressed EventKey list)
- **Target**: Session.Events (projection), does not modify MemoryStore (already persisted by onEvent)
- **Temporal relationship**: onEvent persists FullEvent to MemoryStore in real-time on each event; Compact cleans projection when it's full — different timing, Compact doesn't lose data

| Operation | Target | Trigger | Effect |
|-----------|--------|---------|--------|
| SmartCompressor | messages (LLM view) | token exceeds threshold | Compress message list LLM sees |
| Compact | Session.Events (projection) | Still over limit after SmartCompress | Clean old references, replace with summary |

**Three-tier mutability**: messages (SmartCompressor can modify), Session.Events (Compact can clean), MemoryStore (never mutable).

> Details: [agent-architecture.md §9](docs/wiki/agent/agent-architecture.md)

### 3. Event-Driven Memory (onEvent 5-Step Pipeline + Causal Chain)

On each event arrival, the `onEvent` callback executes a 5-step pipeline:

1. **Event extraction**: `ExtractEventType` infers type from Message.Role (`external_input`, `agent_output`, `action_command`, etc.)
2. **Memory write**: Persist FullEvent to MemoryStore (permanent storage, immutable)
3. **Causal chain**: `RelationStore.SetParent` — each event points to its predecessor
4. **StateDelta fill**: `event_key`, `event_type` written to `evt.StateDelta`
5. **Projection append**: Session.Events appends EventReference (lightweight reference: key + type + summary)

The LLM sees event keys in message prefixes (`[evt_123456|agent_output]`), enabling it to pass relevant keys to sub-agents for context retrieval.

> Details: [memory-architecture.md](docs/wiki/memory/memory-architecture.md), [plugin-architecture.md](docs/wiki/plugin/plugin-architecture.md)

### 4. Sub-Agent Invocation (AgentToolWrapper + A2A)

`AgentToolWrapper` wraps any `agent.Agent` interface (local `TagentAgent` or remote `A2AAgent`) as a callable tool. The invocation flow:

```mermaid
graph TD
    CALL["AgentToolWrapper.Call<br/>(ctx, jsonArgs)"] --> PARSE["1. Parse event_keys<br/>from LLM args"]
    PARSE --> GET["2. parentStore.GetEvent(key)<br/>→ FullEvents"]
    GET --> SER["3. Serialize → RuntimeState<br/>['external_context']"]
    SER --> RUN["4. agent.Run(ctx, invocation)"]
    RUN --> LOCAL["Local: TagentAgent.Run<br/>→ EventBus + AgentLoop (in-process)"]
    RUN --> REMOTE["Remote: A2AAgent.Run<br/>→ A2A HTTP → remote TagentAgent"]

    style RUN fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
```

**Remote path**: `WithTransferStateKey("external_context")` automatically maps RuntimeState to A2A message metadata, transparent across process boundaries.

**Configuration layering**:

| Layer | Responsibility | Config |
|-------|---------------|--------|
| tagent YAML | Agent definition (model, prompt, tools, event_params) | Declarative YAML |
| `ToolRef.Remote` | Connection info ("where is this agent?") | `remote.url` field |
| trpc Go options | Communication details (A2A protocol, TransferStateKey) | Auto-generated by `tagent.go` |

> Details: [agent-architecture.md §13-14](docs/wiki/agent/agent-architecture.md), [tool-architecture.md §4](docs/wiki/tool/tool-architecture.md)

## Key Scenarios

### Scenario 1: Persistent Event Loop (Long-Running)

```mermaid
sequenceDiagram
    participant U as User
    participant TM as TmuxMonitor
    participant TA as TagentAgent
    participant EB as EventBus
    participant AL as AgentLoop
    participant PP as Preprocessor
    participant OC as outputCh

    TA->>AL: StartLoop(userID, sessionID)

    par Concurrent writes to EventBus
        U->>TA: InjectMessage(msg)
        TA->>EB: Publish(external_input)
    and
        TM->>TA: InjectMessage(system_msg)
        TA->>EB: Publish(external_input)
    end

    AL->>AL: Pull (batch all)
    AL->>AL: onEvent 5-step pipeline (persist + causal + StateDelta + append EventReference)
    AL->>PP: Process(batch, session)
    PP-->>AL: messages + shouldCallModel
    AL->>AL: model.GenerateContent
    AL->>AL: onEvent 5-step pipeline (persist response + append EventReference)
    AL->>OC: emit → outputCh
    AL->>AL: back to Pull
```

### Scenario 2: Sub-Agent with A2A Remote Communication

```mermaid
sequenceDiagram
    participant LLM as Top-Level LLM
    participant AW as AgentToolWrapper
    participant PS as Parent MemoryStore
    participant A2A as A2AAgent (client)
    participant SRV as A2A Server
    participant RTA as Remote TagentAgent
    participant RMS as Remote MemoryStore

    LLM->>AW: Call(jsonArgs with event_keys)
    AW->>PS: GetEvent(event_key)
    PS-->>AW: FullEvents
    AW->>AW: Serialize → RuntimeState["external_context"]
    AW->>A2A: agent.Run(invocation)

    Note over A2A: WithTransferStateKey("external_context")<br/>RuntimeState → A2A metadata

    A2A->>SRV: HTTP (with metadata)
    SRV->>SRV: metadata → RuntimeState
    SRV->>RTA: TagentAgent.Run(invocation)
    RTA->>RTA: Deserialize external context
    RTA->>RTA: injectExternalContext → EventBus.Publish

    loop Remote ReAct Loop
        RTA->>RMS: Persist events
    end

    RTA-->>SRV: final response
    SRV-->>A2A: HTTP response
    A2A-->>AW: response
    AW-->>LLM: tool result
```

## Scenario Walkthrough

> A concrete example showing how tagent's modules collaborate in a real task.

### Task: "Review recent Git commits, summarize the changes, and search for related design documents"

User sends this request in **Persistent Event Loop mode**. The agent needs multiple ReAct iterations, invokes two sub-agents, and triggers context compression.

### Step-by-Step Module Collaboration

| Step | Module | Action | Event Type | MemoryStore Operation |
|------|--------|--------|-----------|----------------------|
| 1 | **User** → `TagentAgent` | `InjectMessage("Review recent Git commits...")` | — | — |
| 2 | **AgentLoop** | `EventBus.Pull` → `onEvent` 5-step pipeline (event extraction → memory write → causal chain → StateDelta → append EventReference) | — | — |
| 3 | **onEvent** → **MemoryPlugin** | 5-step pipeline: infer type, generate EventKey, persist FullEvent, build causal chain, fill StateDelta, append EventReference to Session.Events (projection) | `external_input` | Store FullEvent (immutable) |
| 4 | **Preprocessor** | Build messages from `Session.Events` (projection), fetch full Content from MemoryStore on demand, inject `[evt_KEY\|external_input]` prefix, check token budget | — | — |
| 5 | **AgentLoop** → LLM | LLM decides to call `action` tool | `thinking_plan` | `onEvent` 5-step pipeline persists assistant message with tool_calls |
| 6 | **ActionTool** | goroutine executes `git log --oneline -10`, result published as external_input back to bus | `action_command` | Next round onEvent 5-step pipeline persists tool result, EventKey → causal parent = step 5 |
| 7 | **AgentLoop** → LLM | LLM sees git log, decides to call `recall` sub-agent | `thinking_plan` | `onEvent` persists new assistant message with tool_calls |
| 8 | **AgentToolWrapper** | Parse `event_keys` from LLM args → `parentStore.GetEvent(key)` → serialize context → `RuntimeState["external_context"]` | — | Read FullEvents from MemoryStore (no write) |
| 9 | **RecallAgent** (sub-agent) | `agent.Run()` → independent EventBus + AgentLoop → returns summary | `thinking_recall` | Sub-agent persists its own events; parent sees only tool result |
| 10 | **Preprocessor** | Token budget exceeded → `SmartCompressor.Compress()` (compress messages view) → still over limit then `Compact` (clean Session.Events projection, replace old references with summary) | `context_compress` *(view-only)* | **No MemoryStore change** (Compact doesn't touch permanent storage) |
| 11 | **AgentLoop** → LLM | LLM sees: `[compress_event]` + `[summary]` + `[recent: recall result]`. Decides to call `knowledge` sub-agent | `thinking_plan` | `onEvent` persists with `[evt_KEY\|thinking_plan]` prefix |
| 12 | **KnowledgeAgent** (sub-agent) | `AgentToolWrapper` → `agent.Run()` → independent AgentLoop → returns docs | `thinking_knowledge` | Sub-agent persists its own events |
| 13 | **AgentLoop** → LLM | LLM synthesizes compressed history + recall + knowledge → generates final response (no tool_calls) | `agent_output` | `onEvent` 5-step pipeline persists final response, EventKey → causal parent = step 12 |
| 14 | **AgentLoop** | emit → outputCh → back to `EventBus.Pull` | — | — |

### Event Chain (Causal)

> `context_compress` (step 10) is a **view transformation** — it modifies the LLM message list but does NOT create a MemoryStore event. It is therefore excluded from the causal chain.

```mermaid
graph LR
    E1["evt_1<br/>external_input<br/>'Review Git commits...'"] --> E2["evt_2<br/>thinking_plan<br/>call command"]
    E2 --> E3["evt_3<br/>action_command<br/>git log result"]
    E3 --> E4["evt_4<br/>thinking_plan<br/>call recall"]
    E4 --> E5["evt_5<br/>thinking_recall<br/>recall summary"]
    E5 --> E6["evt_6<br/>thinking_plan<br/>call knowledge"]
    E6 --> E7["evt_7<br/>thinking_knowledge<br/>doc search result"]
    E7 --> E8["evt_8<br/>agent_output<br/>final summary"]
```

> Step 10's `context_compress` occurs between evt_5 and evt_6 in wall-clock time, but since it's view-only, the causal chain skips directly from evt_5 → evt_6.

### What Each Module Sees

| Module | What it sees | What it doesn't see |
|--------|-------------|-------------------|
| **LLM (top-level)** | Event summaries with `[evt_KEY\|type]` prefixes; compressed view after step 10 | FullEvent content of old segments (dropped by SmartCompressor) |
| **ActionTool** | Command string from LLM; executes independently | Why this command was chosen; what other tools were called |
| **RecallAgent** | External context (event summaries from parent); its own React loop | Parent agent's full Session; other sub-agent results |
| **KnowledgeAgent** | External context + search query; its own React loop | Parent's compression history; ActionTool results |
| **MemoryStore** | **Everything**: all FullEvents, causal chain, timestamps | — (single source of truth) |
| **Session** | Lightweight EventReference[] (key + type + summary) | FullEvent content (lives in MemoryStore) |

### Key Observations

1. **Tool autonomy**: RecallAgent and KnowledgeAgent each run their own internal React loop. The top-level agent only sees their final result — not their internal iterations.

2. **Compression is safe**: Step 10 drops steps 3-6 from the LLM's view, but MemoryStore still holds all FullEvents. If the LLM needs detail, it can call `recall` with the compressed EventKeys listed in the `[context_compress]` message.

3. **Causal chain integrity**: Each event's `EventKey` encodes its causal parent. Even after compression, the MemoryStore chain `evt_1 → evt_2 → ... → evt_8` is intact and traceable.

4. **Async events**: If TmuxMonitor injects a message during step 7, it goes to EventBus. AgentLoop won't process it until the next `Pull` batch.

## Quick Start

### 1. Define Configuration (YAML)

```yaml
# config.yaml
entry: tagent
prompt_dir: resources/prompts
model: glm-4-flash

agents:
  tagent:
    system_prompt:
      files: [AGENTS.md, SOUL.md, USER.md, TOOLS.md]
    memory:
      type: file
      path: /data/tagent/events
    tools:
      # Local sub-agent (in-process)
      - agent: knowledge
        description_file: knowledge_tool_desc.md
        event_params: [event_keys]
      # Remote sub-agent (A2A communication)
      - agent: remote-recall
        description_file: recall_tool_desc.md
        event_params: [event_keys]
        remote:
          url: "http://recall-service:8088"
      # Plain tool
      - kind: tool
        id: command
        description_file: command_tool_desc.md

  knowledge:
    model: glm-4-flash
    prompt:
      files: [knowledge_agent.md]
    memory:
      type: memory
    max_tool_iterations: 5
```

### 2. Persistent Event Loop Mode

```go
// Start persistent loop
outputCh, err := ta.StartLoop("userID", "sessionID")
if err != nil { panic(err) }

// Inject messages from any goroutine (user input, external callbacks)
ta.InjectMessage(model.Message{
    Role:    model.RoleUser,
    Content: "Help me execute a command",
})

// Consume events
for evt := range outputCh {
    if evt.IsFinalResponse() {
        println("Final:", evt.Message.Content)
    }
    // Handle tool calls, intermediate events, etc.
}

// Graceful shutdown
ta.StopLoop()
```

### 3. A2A Server Mode

```go
// Expose TagentAgent as an A2A server for remote agents to call
srv, err := agent.NewA2AServer(ta, "0.0.0.0:8088")
if err != nil { panic(err) }
go srv.Start("0.0.0.0:8088")
```

`tagent.New()` accepts a declarative `Config` (serializable from YAML/JSON) plus runtime `Option` functions for non-serializable dependencies (model instances, MCP tool sets, etc.).

## Configuration Reference

### Agent-Level Options

| Option | Default | Description |
|--------|---------|-------------|
| `model` | (required) | LLM model name |
| `system_prompt.files` | `[]` | Prompt files to load (supports bootstrap ordering) |
| `memory.type` | `memory` | `memory` (in-memory) or `file` (persistent) |
| `memory.path` | `""` | File path (required when `type: file`) |
| `max_tool_iterations` | `200` | Max ReAct loop iterations |
| `max_tokens` | `8000` | Token budget for context compression |
| `compress_threshold` | `0.8` | Compression trigger ratio (`max_tokens * threshold`) |
| `temperature` | `0.7` | LLM temperature |

### Tool Reference Options

| Field | Description |
|-------|-------------|
| `agent` | Sub-agent name (for `kind: agent` tools) |
| `kind` | `agent` (default) or `tool` |
| `id` | Tool ID (for `kind: tool`) |
| `description_file` | Tool description prompt file |
| `event_params` | Parameters that accept event keys (e.g., `[event_keys]`) |
| `remote.url` | Remote agent URL (enables A2A communication) |

## Project Structure

```
tagent/
├── agent/          # Core: EventBus, AgentLoop, Preprocessor, SmartCompressor, AgentToolWrapper, MeditationManager
├── builtin.go      # init(): register built-in tool factories
├── config.go       # Declarative config: Config, AgentConfig, ToolRef, PromptConfig
├── docs/wiki/      # Architecture documents (per-module)
├── event/          # Event types: ExtractEventType, GenerateEventSummary
├── examples/       # Examples (wechat-bot)
├── memory/         # Storage: InMemoryStore, FileSegmentStore, RelationStore, Compactor
├── openspec/       # Design specs and change records
├── plugin/         # Plugins: MemoryPlugin, SummaryPlugin
├── prompt/         # Prompt loader: file, directory, bootstrap
├── resources/      # Prompt files, resources
├── tagent.go       # Composition root: tagent.New(cfg, opts...)
├── testutil/       # Test utilities
├── tool/           # Tools: command, recall, knowledge
└── go.mod
```

## Development

```bash
# Build
go build ./...

# Test
go test ./...

# Run example
cd examples/wechat-bot && go run .
```

### Prerequisites

- Go 1.21+

## License

Apache License 2.0
