# O5 设计

## 范围与精度
只承诺model.Model SDK调用边界：最终messages、工具声明、GenerationConfig、响应序列及终态。provider可能做格式转换，capture_scope固定sdk_request；不以Request.Tools的json tag单独推断捕获能力，也不新增生产wire代理。真实模型HTTP测试只用合成数据核对公开声明转换。

## 格式（新增模式）
配置trajectory_capture.enabled=false；true要求trajectory_dump=true；max_record_bytes默认8MiB、max_pending_bytes默认64MiB、max_run_bytes默认512MiB、queue继续256，非法负数拒绝。构造期资源按RESTART处理。
保留原JSONL字段，新增：
- schema_version=2、run_id、call_id、capture_scope=sdk_request。
- owner：agent_name、partition_id、session_id、root_session_id、invocation_id、parent_invocation_id、task_id、attempt_token、struct_generation、config_digest；无法取得的可选值缺省空并标missing，不造持久turn/session系统。
- llm_call.request.tools：按S1声明快照；request_digest使用规范序列化摘要。
- response_fragments：收到顺序的深拷贝；既有response字段保留可确认终态。没有完整终态则response_incomplete，不能把最后delta拼成完整回答。
- binding_status=bound|unbound|ambiguous，missing_reasons数组。trace/span继续可空，不影响call_id。

## 身份与事实关联
每个runner尝试用独立scope，复用已有invocation/attempt标识；只在capture开启时分配。每scope最多4096项，满则unbound_capacity并计数，不淘汰活跃映射；真实runner生产者停止且事件回调排空后释放，不能以首个assistant或响应channel关闭替代。recorder用run随机前缀+单调计数生成call_id，在转交响应前向scope登记response.ID和tool_call_id关联；输入ctx缺invocation时记录standalone，不冒领父调用。子agent保留根分组标签但新建自己scope。
MemoryPlugin在写入前按scope和事件真实invocation/responseID查关联，匹配才附加call_id；StoreEvent成功后旁路发送fact-link（event_key/partition/call_id），不修改提交后事实、不等待轨迹写盘。丢fact-link有计数；无稳定response ID或冲突必须unbound，不能用最近调用补齐。旧有Attribution与EchoCredential责任不混合；接口放现有plugin/attribution.go的可选消费缝，实现归rl，禁止plugin反向依赖agent/root。
真实框架可能复制Response/ctx，O5.1必须核对ID在完整管线的保留；不成立时阻断精确关联，先回写本计划，不擅自换trace或改依赖。

## 生命周期与资源上限
深拷贝请求发生在inner调用前，响应深拷贝发生在转交前。64MiB总额同时覆盖采集在途请求副本、响应累积、队列与序列化缓冲；byte cap在分配/累计前检查，超限记录missing并释放累积内存，不先存无界完整流。共享recorder只共享writer/统计，身份来自调用scope；相同recorder嵌套用scope claim防重复记录，不吞真正子调用。
新增只读Stats及FlushAndWait(ctx)返回manifest：run_id、cutoff_seq、started/enqueued/written/dropped_full/dropped_closed/oversized/serialize_failed/write_failed/sync_failed/inflight/pending、文件摘要、synchronized、complete。writer处理flush确认后返回；inflight>0、drop/error或缺manifest时不能报complete。模型调用不等待flush，离线消费者显式等。关闭等已接收工作结束，失败可观察，不能只告警然后报完整。
目录0700/新文件0600；不记录key/header/带凭据URL。同时打开文件最多16个，LRU关闭句柄不删除文件；单capture累计写盘达到max_run_bytes后只保留预留的统计/封账空间，停止新的数据接纳并计dropped_disk_limit，业务继续且manifest为partial，不自动轮转规避预算。默认无事实全文副本；O6按授权导出快照。采集关闭时无scope、无新文件、原通路行为不变。

## 写面、测试与回滚
O5独占rl/trajectory_recorder.go及新capture帮助文件/测试；共享MemoryPlugin、Attribution、RunFlow、root/config修改由编排者按O1完成后的冻结版本集成。
测试覆盖channel/Iter两入口、零消费惰性、nil流、取消、流式全文/delta、共享writer多agent、两次重试、空/重复responseID、字节/队列满、写失败、封账与关闭。O3快照接线前fixture只算单元通过。
关闭新配置回到旧录制，既有JSONL不改写。新消费者接受legacy但标未知完整性。文档落点rl-architecture/事件metadata/插件归因及README配置参考。
