# rustviking-config delta（03-history）

> 复用登记（非规范，不改本条语义）：`type: file` 档位接线真实 rustviking 客户端，不复活旧 `memory/kv` 子包结构；真实 CLI 用例独立 `-count=1` 另列资源门；原文与**实际投影**分界保留。
> 本 delta 只修改「Production MemoryStore wiring uses real RustVikingClient」；本能力其余 Requirement（MemoryConfig supports RustViking binary path）由主 spec `openspec/specs/rustviking-config/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: Production MemoryStore wiring uses real RustVikingClient

When `MemoryConfig.Type` is `"file"`, `resolveMemoryStore` SHALL wire a **真实 rustviking 客户端**（真实 CLI）与**真实关系/快照持久化后端**（WAL + snapshot），MUST NOT 使用 mock 或占位实现。

**具体包路径、类型名与构造签名不在此冻结**（DESIGN-GATE(G1)：按真实消费者最小签名收敛）。实现布局 MUST NOT 以**复活旧 `memory/kv` 子包结构、旧类名**（如 `MockRustVikingClient`、`simpleInMemRelationStore`）或**镜像 provider/tool 协议**为基线（计划 §1、§3「不预建 memory 的十余个算法、转发、类型子包」「原生框架类型直接使用，不为阻止一个 import 建同形接口或影子结构」）。

`type: file` 档位的**实际提交屏障** SHALL 由真实 rustviking 客户端与分片原子快照后端兑现，MUST NOT 由 mock/占位实现充当（见 `event-segment-store`「LocalFileKV Sync 为原子快照屏障」）。真实 CLI 的集成用例 SHALL 独立 `-count=1`、列入资源门，MUST NOT 以替身结项。原文与**实际投影**的分界保留：原文读取与原文存储字节一致，模型投影按记录重建，两者不互相冒充。

#### Scenario: 生产接线不用 mock

- **WHEN** 审查 `type: file` 的存储装配
- **THEN** SHALL 为真实 rustviking 客户端与真实关系/快照后端，MUST NOT 使用 mock / 占位实现

#### Scenario: 不复活旧子包与镜像结构

- **WHEN** 审查实现布局
- **THEN** MUST NOT 以复活旧 `memory/kv` 子包、旧类名或镜像 provider/tool 协议为基线；包路径/类型名/构造签名按真实消费者收敛（DESIGN-GATE）

#### Scenario: 真实客户端实际持久化

- **WHEN** `type: file` with `path: /data/events` and `rustviking_binary` set
- **THEN** events SHALL 经真实 rustviking CLI 持久化，可由独立新进程读回

#### Scenario: 真实 CLI 用例独立计门

- **WHEN** 运行真实 rustviking 集成用例
- **THEN** SHALL 以 `-count=1` 独立运行并列入资源门，MUST NOT 与契约用例合并计通过
