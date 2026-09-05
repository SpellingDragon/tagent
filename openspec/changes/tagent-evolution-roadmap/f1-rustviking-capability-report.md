# F1 rustviking 依赖能力报告（含强化清单与裁决）

> DAG 脊柱节点 F1 交付物。方法：源码核对（commands.rs / index_commands.rs / config.toml）+ live 实测（index info）。
> 指令1「强化这部分能力，应当先确认甚至优化依赖」的落地结论。

## 结论速览

1. rustviking 向量子系统**真实可用**（live probe：`index info` → `{count:0,dimension:768}`，IVF-PQ）。
2. tagent `RustVikingClient` 的 `VectorInsert/VectorSearch/Embed` 是**虚构契约**（命令名/向量格式/响应解析三重错）——这是 `SearchByEmbedding` 永远为 stub 的**根因**。T-A 必须重写。
3. rustviking 可作**向量索引后端**，但「闭环记忆引擎」的嵌入/hybrid/分区三块须由 **tagent 适配器**补足（rustviking 的高层 `find` 是文档导向，不契合 tagent 事件模型）。

## 实测证据

| 项 | 证据 |
|---|---|
| 真实 vector CLI | `index insert/search/delete/info`（commands.rs:232-259、index_commands.rs） |
| `index search` 返回 | `{query_dimension,k,count,results:[{id,score,level}]}`——**含 score**（index_commands.rs:33-60） |
| `index delete` | **存在且可用**（commands.rs:252-256、index_commands.rs:62）——印证报告「无 VectorDelete」是包装层局限 |
| live probe | `index info` → `{"success":true,"data":{"count":0,"dimension":768}}`；日志走 stderr、JSON 走 stdout（R3 契约成立） |
| 向量格式 | **逗号分隔 f32**（`value_delimiter=','`），非 JSON 数组 |
| `find` | 语义搜索自动 embedding，面向 `viking://` 文档，L0/L1/L2（commands.rs:131-141）——**文档导向，非事件导向** |
| 无 `embed` CLI | Commands 枚举无 embed（embedding 仅隐式经 write/find） |
| 无 `kv range` | KvOperation 仅 Get/Put/Del/Scan/Batch（commands.rs:196-230）——tagent-integration R2 的 P0 需求**从未落地** |
| config 默认 | `[vector]` ivf_pq dim768；`[vector_store]` memory（**易失**）；`[embedding]` mock dim1024 |
| 二进制状态 | v0.1.0，`cargo build --release` 0.60s no-op = 已与源码同步 |

## tagent 客户端虚构契约（三重错，T-A 重写）

| tagent 方法 | 现调用 | rustviking 真实 | 错处 |
|---|---|---|---|
| `VectorInsert` | `vector insert -i -v <JSON数组> -l` | `index insert -i -v <逗号分隔> -l` | 命令名 + 向量格式 |
| `VectorSearch` | `vector search -q <JSON数组> -k`，解析 `[]uint64` | `index search -q <逗号分隔> -k`，返回 `{results:[{id,score,level}]}` | 命令名 + 格式 + 响应解析 |
| `Embed` | `embed -t <JSON数组>` | **无此命令** | 命令不存在 |

## 缺口与强化清单

| # | 缺口 | 归属 | 裁决 |
|---|---|---|---|
| G-a | 无 hybrid RRF（`find` 纯向量、文档导向） | tagent 适配器 | RRF 在适配器（关键词 ∪ 向量） |
| G-b | 契约三重错 | tagent 适配器 | T-A 重写向量面为 `index` 契约 |
| G-c | `index` 无分区过滤（全局索引） | tagent 适配器 | EventKey 高位分区，Go 侧过滤（报告 D2 §4.3 跨分区泄漏防线） |
| G-d | 无 `embed` CLI | tagent | tagent 侧 zhipu HTTP 嵌入（复用 ZAI_API_KEY，维度可配） |
| G-e | 无 `kv range` | rustviking（可选） | 向量重建按分区前缀 scan 够用；跨分区需求出现再强化 |

## 自决记录（execution-dag.md §4.2）

- **DECIDED F1-①**（依据层6/实测）：rustviking 二进制 v0.1.0 已与源码同步，`index` 子系统 live 可用，**无需发版**即可作向量后端。
- **DECIDED F1-②**（依据层3 预登记默认「改动成本过高则降级 tagent 适配层」+ 层6）：hybrid RRF / 嵌入 / 分区过滤置于 **tagent 适配器层**；rustviking 用作**向量索引后端**（`index insert/search/delete`，IVF-PQ/HNSW）。理由：rustviking 的 `find`/VikingFS 是文档导向，重构为事件导向 hybrid 成本高；`index` 低层面已足够作向量后端。
- **DECIDED F1-③**（依据层6 先进性+可控性）：嵌入用 **tagent 侧 zhipu embedding-3 HTTP**（openai 兼容 `/v1/embeddings`，复用 ZAI_API_KEY，维度可配），非 rustviking mock/CLI。
- **BACKLOG（rustviking 优化，延后不阻塞 T-A）**：① 内置事件导向 hybrid 检索；② 暴露 `embed` CLI；③ 实现 `kv range`（R2）；④ `vector_store` 默认持久化后端改 rocksdb（现 memory 易失）。记入 rustviking roadmap。

## 对 F2 的输入

- 解耦缝须抽象三接口：**IndexBuilder**（事件→索引，引擎内部嵌入/存储）+ **Retriever**（查询→票据，引擎内部 keyword/vector/hybrid）+ 生命周期（Closer/Ready）。
- 两实现：**RustVikingEngine**（适配 `index` CLI + tagent 嵌入 + RRF + 分区过滤）、**InMemoryEngine**（MVP 兜底）。
- `VectorHit` 须含 `Score`（rustviking `index search` 返回 score，RRF 可用 rank，score 供诊断/阈值）。
