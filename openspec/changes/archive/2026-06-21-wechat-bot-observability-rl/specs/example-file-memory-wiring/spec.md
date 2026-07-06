## ADDED Requirements

### Requirement: FileSegmentStore 作为 memory backend（使用 LocalFileKV）
wechat-bot 示例的 tagent agent SHALL 使用 `type: localfile` memory store（FileSegmentStore + LocalFileKV），通过本地 JSON 文件持久化事件数据，重启后数据不丢失。不依赖 rustviking CLI。

#### Scenario: 首次启动创建存储
- **WHEN** 用户首次启动 wechat-bot（`.wechat-config/data/` 目录不存在）
- **THEN** 系统自动创建数据目录，初始化 `kv.json`（LocalFileKV 持久化文件）和 `relations.journal`（关系存储 WAL）

#### Scenario: 重启后数据持久化
- **WHEN** 用户重启 wechat-bot
- **THEN** 之前会话的事件数据从 `kv.json` 恢复到内存，recall agent 可查询历史事件

### Requirement: LocalFileKV 实现
系统 SHALL 提供 `LocalFileKV` 实现 KVStore 接口，使用 JSON 文件作为持久化后端，无外部二进制依赖。

#### Scenario: KV 写入后持久化
- **WHEN** 调用 `KVPut(key, value)` 写入数据
- **THEN** 数据立即写入内存 map 并同步 flush 到 `kv.json` 文件

#### Scenario: KVScan 前缀扫描
- **WHEN** 调用 `KVScan(prefix, limit)` 
- **THEN** 从内存 map 中过滤匹配前缀的 key，按 limit 限制返回数量

#### Scenario: 启动时恢复数据
- **WHEN** 创建新的 LocalFileKV 实例且 `kv.json` 文件已存在
- **THEN** 从文件加载全部 KV 数据到内存 map

### Requirement: recall agent 共享 file store
recall agent SHALL 使用与 tagent agent 相同的 `type: localfile` 和 `path` 配置，确保通过同一个 FileSegmentStore 实例读取 tagent 写入的事件数据。

#### Scenario: recall 读取 tagent 的事件
- **WHEN** tagent agent 处理消息后产生事件并持久化
- **THEN** recall agent 通过 `read_namespaces: [tagent]` 能查询到这些事件

### Requirement: 旧 FileBackend 残留清理
wechat-bot 示例 SHALL 删除 `.wechat-config/agent-events/` 目录中的旧 FileBackend 单事件 JSON 文件。

#### Scenario: 清理旧文件
- **WHEN** 执行此 change 后
- **THEN** `.wechat-config/agent-events/` 目录不再存在，新数据写入 `.wechat-config/data/` 下的 `kv.json`
