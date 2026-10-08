# decision-capture Specification

## Purpose
TBD - created by archiving change capture-decision-snapshots. Update Purpose after archive.
## Requirements
### Requirement: 可选SDK决策快照
启用trajectory_capture后 SHALL 记录v2 SDK请求、冻结工具声明、响应序列、终态及owner；旧trajectory_dump模式保持可用。MUST 标明capture_scope，不能冒称provider wire或隐藏提示完整。

#### Scenario: 请求对象随后被修改
- **WHEN** 调用后框架或另一路请求改变原messages/schema
- **THEN** 已采集快照不变化，模型调用仍用本次声明。

### Requirement: 精确可选归因
call_id SHALL 独立于trace生成并在调用scope内按invocation/response/tool-call精确关联；无证据必须unbound，冲突必须ambiguous。事实关联 MUST 发生在成功提交边界，不补写旧事实。

#### Scenario: 多agent与空响应身份
- **WHEN** 两个子调用并发且其中一个响应没有可用ID
- **THEN** 有证据者精确归属，无ID者unbound，不继承最近父调用。

### Requirement: 非阻塞有界录制
默认模型路径 SHALL 不等待训练磁盘；单条记录及全部采集在途副本/累积分片/队列/序列化缓冲总字节 SHALL 有上界，满队列、超限、关闭和写入错误分别计数。响应采集 MUST 不因累计无限分片突破上界；同时打开文件最多16个，单capture落盘上限默认512MiB，达限停止新采集并报告partial，不自动删除旧文件或轮转绕过限额。

#### Scenario: 慢磁盘与大响应
- **WHEN** writer停滞且模型持续输出
- **THEN** 数据可显式丢弃/截为不完整记录，内存有界、模型继续，统计可靠。

### Requirement: 封账完整性可证明
FlushAndWait SHALL 返回writer确认后的manifest；存在inflight/drop/error、未同步或无manifest MUST NOT 声称完整。统计读取不得依赖可能已满的数据队列。

#### Scenario: 关闭与flush失败
- **WHEN** capture结束或flush失败
- **THEN** 成功封账可按文件摘要复核，失败保留unknown/partial及原因，不能伪全量。

### Requirement: 采集不改变运行权责
采集 MUST NOT 强制OTel、改变TTL、自动训练、改变任务路由或记录凭据；关闭新模式不得创建归因scope/manifest。

#### Scenario: 关闭与隐私边界
- **WHEN** trajectory_capture关闭或仅运行合成验收
- **THEN** 原行为保持；启用输出按私有权限且无认证头/密钥。

