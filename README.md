# tagent

**记忆驱动的长期运行 Agent 框架** —— 基于 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)，用事件驱动引擎替代同步 ReAct 循环：事件永久入库、上下文按需压缩、历史随时召回，让 Agent 可以**连续运行数天而不失忆、不失控**。

[English](README_EN.md) | 中文

---

## ✨ 特性一览

| 特性 | 一句话说明 |
|------|-----------|
| 🔄 **持久事件循环** | `StartLoop` 后常驻运行；消息、工具结果、定时事件统一经 EventBus 驱动 turn |
| 🧠 **记忆三原语** | store（事件不可变入库）/ compress（总结+自然遗忘）/ recall（票据或语义召回） |
| 🗂 **卡片序列** | 压缩后的历史浓缩为索引卡片行——模型始终"看得见做过什么"，每张卡自带召回票据 |
| ⚡ **异步任务层** | 长命令/服务经 tmux 后台运行：快命令内联返回，慢任务 ACK + `task_settled` 通知回写 |
| 🔁 **任务重入** | `resume_task` 对存活服务续输入（REPL 式）、对完成的子 Agent 续指令（自动还原上下文） |
| 🤖 **子 Agent 编排** | 本地 `AgentToolWrapper` / 远程 A2A 协议统一封装；事件跨 Agent 按 key 精确传递 |
| 🧘 **冥想心跳** | 空闲期自动回顾与沉淀，产出 ★ 高亮卡片进入长期记忆 |
| 🎓 **RL 集成** | HTTPAPI + SwappableModel + TrajectoryRecorder，与 AReaL 对接采集训练轨迹 |

## 🎬 一个长期运行的日常

```mermaid
sequenceDiagram
    participant U as 用户
    participant T as tagent
    participant X as tmux 任务层
    participant M as MemoryStore

    U->>T: "部署服务并盯着"
    T->>X: spawn(deploy.sh)
    Note over X: dense 窗口密集探测（~10s）
    X-->>T: 未结算 → ACK「后台运行 task-42」
    T-->>U: 已开始部署，完成后通知你
    Note over T: 期间正常处理其他消息
    X->>T: task_settled(task-42, 部署成功)
    T-->>U: 🔔 部署完成（通知回写，非阻塞）
    Note over T,M: 上下文超预算 → 压缩：旧事件归档，<br/>历史浓缩为卡片行 [evt_1a2b] 部署成功…
    U->>T: （次日）"昨天部署时的报错细节是什么？"
    T->>M: memory_recall(items=[{key: 1a2b}])
    M-->>T: 精确回补原文（零幻觉）
    T-->>U: 完整细节
```

## 🚀 快速开始

**1. 声明式配置（YAML）**

```yaml
entry: tagent
prompt_dir: resources/prompts
model: glm-4-flash
providers:
  openai:
    api_endpoint: "https://open.bigmodel.cn/api/paas/v4"
    api_key_env: "ZAI_API_KEY"

agents:
  tagent:
    system_prompt:
      files: [AGENTS.md, SOUL.md, TOOLS.md]
    memory:
      type: localfile
      path: /data/tagent/events
    tools:
      - agent: recall            # 子 Agent 工具：复杂记忆检索
        description_file: recall_tool_desc.md
        event_params: [event_keys]
      - kind: tool
        id: memory_recall        # 纯函数工具：票据/关键词召回
      - kind: tool
        id: exec                 # tmux 命令执行（异步任务层）
        description_file: action_tool_desc.md

  recall:
    system_prompt:
      files: [recall_agent.md]
    memory:
      type: memory
    max_tool_iterations: 10
```

**2. 三行进入持久循环（Go）**

```go
ta, _ := tagent.New(cfg, tagent.WithModel(model))
defer ta.Close()

outputCh, _ := ta.StartLoop("userID", "sessionID")
ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "帮我执行一个命令"})

for evt := range outputCh {
    if evt.IsFinalResponse() {
        println("Final:", evt.Message.Content)
    }
}
```

**3. 跑通完整示例**

```bash
cd examples/wechat-bot && go run .    # 微信机器人：持久循环+全部机制实战
```

其他运行模式：A2A 服务端（`agent.NewA2AServer`）、RL rollout worker（`agent.NewHTTPAPI` 对接 AReaL）——见 [examples/](examples/) 与 [docs/wiki/](docs/wiki/)。

## 🧠 心智模型

### 三层数据表示

| 层 | 位置 | 职责 | 生命周期 |
|-----|------|------|----------|
| **EventBus AgentEvent** | Agent 内存 | 事件触发队列 | Publish → Pull 后丢弃 |
| **SessionProjection EventReference[]** | Agent 内存 | 投影（有界工作内存） | 可被 Compactor 清理 |
| **MemoryStore FullEvent** | 内存/文件/DB | 永久存储（不可变） | 永久 |

```mermaid
graph TB
    EB["EventBus: AgentEvent"]
    SP["SessionProjection: EventReference[]"]
    MS["MemoryStore: FullEvent"]
    LLM["[]model.Message<br/>发给 LLM 的上下文"]
    TOOL["Tool"]

    EB -->|驱动 turn: Pull → RunFlow| SP
    EB -->|插件管线: 事件入库| MS
    MS -.同步追加轻量引用.-> SP
    SP -->|assembleRequest 原生渲染| LLM
    MS -->|memory_recall / recall 工具| TOOL
```

**关键约束**：投影只存轻量引用（key+type+summary）；MemoryStore 是唯一完整事件链；压缩只改 LLM 视图与投影，永不动存储。

### 记忆三原语与固化级联

```mermaid
graph LR
    A["事件原文<br/>(第0层,唯一全文接触点)"] -->|L3 归档,同段不重摘| B["段摘要<br/>(挂因果链+来源keys)"]
    B -->|工程化提取,零 LLM| C["卡片行<br/>[evt_key] 任务骨架"]
    C -->|超限,LLM 整理| D["浓缩卡片<br/>(保骨架+key引用)"]
```

- **成本恒定**：每层摘要只基于上一层的产物，压缩开销只与新增内容有关，与历史总量无关——跑一年和跑一天一样快
- **卡片序列**：压缩后的历史保持为可读的卡片行（`[Compacted N] + 卡片行 + recent keys`），冥想沉淀带 ★ 高亮
- **原文可忘，摘要长存**：摘要永不过期；卡片里的 `[hex]` key 就是召回票据——随时用 `memory_recall` 取回原文

### 记忆数据模型（LSM）

存储按 **LSM 树**组织：事件从三条管线（EventBus 注入 / 框架 LLM 事件 / 压缩固化物）汇入唯一写入路径，顺序追加进按写入时间分段的存储；层级表示写入新近度与压实代数，封口/压实写入真实时间边界供查询剪枝；遗忘由压实、TTL、容量三层各自负责。

```mermaid
graph LR
    P["事件管线<br/>注入/LLM事件/固化物"] --> W["StoreEvent<br/>碰撞守卫+seq恢复"]
    W --> S["分段存储<br/>evt/idx/meta/tomb"]
    S --> L["L0活跃→L1封口→L2→L3<br/>压实写真实边界"]
    L --> R["召回：票据/语义/卡片"]
    F["遗忘：压实·TTL·容量"] -.墓碑.-> L
```

- **召回与压缩同向**：压缩丢旧留新，召回新先于旧——`timestamp_desc` 下截断只牺牲最旧，永不丢最新记忆
- **两条时间轴**：`Timestamp`（事件时刻）是唯一语义时间轴；EventKey 内嵌时间（写入时刻）仅用于段放置与同毫秒决胜
- **事件不可变**：EventKey 是事件身份，重复写入被拒绝；重启后 seq 从已有最大值恢复，不覆写旧事件
- **遗忘可配置**：TTL 按事件类型衰减（固化物豁免），经 `memory.lifecycle` 声明；负全局 TTL = 总开关关闭遗忘

完整数据流、隐式连接与硬契约见 [wiki/memory §16](docs/wiki/memory/memory-architecture.md)。

## ⚙️ 六大机制速览

| 机制 | 亮点 | 详解 |
|------|------|------|
| 持久事件循环 | Pull 批处理；async 结果排队不打断进行中 turn | [wiki/agent](docs/wiki/agent/event-flow.md) |
| 上下文压缩 | 双层设计：发给 LLM 的视图分级压缩 + 工作内存滚动成卡片；多维触发（token 阈值或完整任务段超龄）防占位符渲染饿死压缩；进行中段工具调用历史折叠为工具链行（有界化，无零信息占位符）；被丢弃的执行过程经 `memory_turn` 因果链召回；永不修改已存储的事件 | [wiki/memory](docs/wiki/memory/memory-architecture.md) |
| 事件驱动记忆 | 每个事件有全局唯一 key（时间有序）；Agent 间存储隔离，跨 Agent 读需显式授权 | [wiki/memory](docs/wiki/memory/memory-architecture.md) |
| 子 Agent 调用 | `event_params: [event_keys]` 按 key 传事件（数据隔离）；A2A 远程透明 | [wiki/tool](docs/wiki/tool/tool-architecture.md) |
| 异步任务层 | 快命令秒回、慢任务后台通知；实时任务看板；`resume_task` 随时续跑；退出不留孤儿进程 | [wiki/tool](docs/wiki/tool/tool-architecture.md) |
| 冥想心跳 | 空闲期自动回顾近期工作，总结沉淀为 ★ 卡片进入长期记忆 | [wiki/agent](docs/wiki/agent/agent-architecture.md) |

## 🏗 架构

```mermaid
graph TB
    ROOT["tagent.New() 组合根"]
    TA["TagentAgent"]
    EB["EventBus"]
    CM["ContextManager"]
    SC["SmartCompressor"]
    CP["Compactor"]
    MM["MeditationManager"]
    MP["MemoryPlugin"]
    MS["MemoryStore"]
    RS["RelationStore"]
    ATW["AgentToolWrapper"]

    ROOT --> TA
    TA --> EB
    EB -->|Pull| TA
    TA -->|BuildInvocation + RunFlow| CM
    CM --> SC
    CM --> CP
    CM -->|runner.Run| LLMAGENT["框架 LLMAgent/Runner"]
    LLMAGENT -->|OnEvent| MP
    MP --> MS
    MS --> RS
    ATW -->|调用| TA
    TA --> MM
```

| 模块 | 职责 |
|------|------|
| `agent/` | 事件驱动引擎：EventBus、runEventLoop、ContextManager（粘合层）、冥想、子 Agent 封装 |
| `agent/task/` | 任务生命周期：TaskManager、完成探测、任务看板、重入 |
| `agent/compress/` | 压缩域：上下文压缩、卡片序列、投影、token 计量 |
| `memory/` | 结构化事件存储：InMemoryStore、FileSegmentStore、RelationStore、生命周期 |
| `plugin/` | 框架插件：MemoryPlugin（持久化+因果链）、SummaryPlugin（元数据标注） |
| `tool/` | 工具：ActionTool（tmux）、recall/knowledge 子工具、任务工具族、文件工具 |
| `event/` | 事件类型系统与元数据契约（`FormatEventKey`/`ParseEventMeta`） |
| `rl/` | RL 集成：TrajectoryRecorder、SwappableModel、HTTPAPI |
| `tagent.go` + `config.go` | 组合根与声明式配置 |

依赖全部单向无循环：`root → agent → plugin → memory`，`tool/* → memory`。

## 📐 设计哲学

四条承诺，贯穿所有机制：

1. **事件不可变**：发生过的事永久入库、永不修改——压缩、遗忘都只作用于"视图"，不作用于事实
2. **上下文有界**：发给 LLM 的工作内存永远有预算上限，超限自动压缩——不靠无限窗口，靠分层记忆
3. **召回精确**：压缩掉的内容都留有票据（事件 key），按票取回原文，零幻觉
4. **异步不失联**：长任务先应答、完成后通知；通知自带完整上下文，压缩或乱序都不会产生"断线"的任务

更完整的设计论证（不变量、时间线渲染规则、元数据契约）见 [docs/wiki/](docs/wiki/) 与 [openspec/specs/](openspec/specs/)。

## 🔧 配置参考

### 全局选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `entry` | `tagent` | 入口 Agent 名称 |
| `prompt_dir` | `resources/prompts` | 全局 prompt 目录 |
| `model` | （必填） | 默认模型名称 |
| `provider` | `openai` | 默认 provider |
| `providers` | `{}` | provider 连接信息 |
| `log_level` | `info` | 日志级别 |
| `request_timeout_seconds` | `3600` | 请求超时 |
| `trajectory_dump` | `false` | 启用轨迹记录 |
| `trajectory_dir` | `data/trajectories` | 轨迹文件目录 |

### Agent 级选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `model` / `provider` | （继承全局） | LLM 模型与 provider |
| `system_prompt.files` | `[]` | 加载的 prompt 文件 |
| `memory.type` | `memory` | `memory`/`file`/`localfile` |
| `memory.path` | `""` | 存储路径/标识 |
| `memory.read_namespaces` | `[]` | 可读取的其他 agent 分区 |
| `max_tool_iterations` | 入口 50 / 子 10 | 最大 ReAct 迭代次数 |
| `max_tokens` | 入口 8000 / 子 4096 | 上下文 token 预算 |
| `compress_threshold` | `0.8` | 压缩触发比例——**整理（compaction）的唯一触发条件**（容量超阈才整理）；task_settled 通知全文内联，整理间上下文前缀稳定以利缓存复用 |
| `keep_recent_tasks` | `2` | **整理后**保留的最近任务数（L0 保留区与全文窗口派生的状态参数，不参与触发） |
| `task_terminal_ttl` | `"2m"` | 终态任务回收前保留期（也是终态任务的 resume_task 重入窗口） |
| `resume_context_rounds` | `3` | 子 Agent 重入还原的前序轮次数 |
| `temperature` | 入口 0.7 / 子 0.3 | LLM 温度 |
| `meditation.enabled` | `false` | 启用冥想（`interval`/`min_gap`/`prompt_file`） |

### compress 块（压缩家族）

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `skeleton_segmentation` | `true` | 骨架分段压缩（agent_output 切段 + 段龄定级 + 多段归档）；`false` 回退旧 user 切段 |
| `summary_model` / `summary_provider` | （继承 agent） | 压缩摘要专用模型（可用廉价模型） |
| `card_max_chars` | `6000` | 卡片序列长度上限；超限旧卡 LLM 整理或沉底 |
| `compact_keys_listed` | `32` | 滚动摘要列出的 recent keys 上限 |
| `recent_full_count` | `keep_recent_tasks × 4` | 全文解析窗口大小（未配置时派生，显式配置优先）；**在整理轮锚定、整理间冻结**——锚点后的既有引用保持摘要渲染，新追加事件全文（活跃前沿），前缀字节稳定 |
| `max_notice_chars` | `800` | 压缩通知文案上限 |
| `archive_cache_cap` | `256` | 进程内归档缓存条数（固化物本体永在 MemoryStore） |

### 工具引用（ToolRef）

| 字段 | 说明 |
|------|------|
| `kind` | `agent`（默认）或 `tool` |
| `agent` / `id` | 子 Agent 名称 / 工具 ID |
| `description_file` | 工具描述 prompt 文件 |
| `event_params` | 事件参数，如 `[event_keys]` |
| `extra_params` | 附加路由参数声明（如 plan 的 `action` enum + `name`）；调用时随 `request` 打包为 JSON 消息体透传子 Agent，未声明则消息体保持纯文本 |
| `async` | 子 Agent 是否走异步任务层（默认 true） |
| `remote.url` | 远程 A2A Agent URL |
| `properties` | 工具专属配置（exec: `workspace`/`run_as_user`/`run_as_group`） |

> agent 运行参数（`max_tool_iterations`/`max_tokens`/`temperature`）**只在被引用 agent 自身的 `agents.<name>` 定义处配置**——ToolRef 只声明引用关系。

## 📚 深入阅读

| 主题 | 文档 |
|------|------|
| 记忆架构 / 策展 / recall 协议 | [docs/wiki/memory/memory-architecture.md](docs/wiki/memory/memory-architecture.md) |
| 工具架构 / 任务重入 / 会话回收 | [docs/wiki/tool/tool-architecture.md](docs/wiki/tool/tool-architecture.md) |
| Agent 架构 / 事件流 | [docs/wiki/agent/](docs/wiki/agent/) |
| 事件系统 / 插件 / Prompt | [docs/wiki/](docs/wiki/) |
| 设计规格（OpenSpec） | [openspec/specs/](openspec/specs/) |
| 完整示例（WeChat Bot + RL） | [examples/wechat-bot/](examples/wechat-bot/) |

## 开发

```bash
go build ./...                        # 构建（Go 1.21+）
go test ./...                         # 测试
bash scripts/race_check.sh            # race 门禁
cd examples/wechat-bot && go run .    # 运行示例
```

## License

Apache License 2.0
