# 资源有界（resource-bounds）

父变更：boundary-closure（W2；依赖 W1 全入 main）。

## Why

模型上下文有界不等于进程资源有界（一级 design D1 E5-E7）：TestLocalSmoke 300 轮两次实测堆增长 175/159 KiB/轮，超 ≤128 KiB/轮门；本地 KV 分片全内存常驻（localfile.go shards map，open 全量加载、无淘汰）；事件存储读缓存无界（segment_store.go cache map）；常驻地址与消费记录的回收边界未验证。OOM 报告 S1/S2 已在 HEAD 闭合，剩余缺口集中在存储层与回收面。

## What Changes

1. KV 本地存储条目级驻留：内存驻留改为条目级 LRU（字节预算），冷条目淘汰后按 shard 快照读回；写路径保持 WAL+barrier 与原子 rename 语义不变。
2. 事件读缓存淘汰与惰性 Load：cache 改有界（字节预算 LRU），**内存保留既有 `{pid}:idx:{event_key}` 键的轻量映射（键驻留=既有索引的驻留形态，值=`{window}:{seq}` 极小；不另立第二份读路；body 仍惰性）**；GetEvent miss 由该映射定位 pid→窗口段读回；**kvPort 增按前缀的键值枚举（prefix-pair scan）**：localFileKV 的实现=内存 shard+冷读合并（淘汰条目按 shard 快照读回），memKV/rustvikingKV 同步实现（三实现入触碰集）；**Load 经两次前缀枚举（idx 前缀 + meta 前缀）只物化 idx/meta 小键值、不物化 evt body**（重启后 idx 映射由 Load 自身读 idx 键重建；body 按需回读）；`TestSegmentLoadLazyPeak` 以进程级断言量启动峰值。主 spec `event-segment-store` 的 DeleteEvent/墓碑 eventCount 条款与"从事实链重建/count_known=false"条款在 HEAD 均无实现（全仓无 DeleteEvent、count_known 0 命中），本域不实现不改条款；归档导览（F4 ⑥）单列该债。
3. 空闲地址回收：常驻 consumer/地址表增加空闲 TTL 回收（回收后新输入可重建），回收与虚胖告警具名计数＋日志可观测（F13）。
4. 冒烟斜率达标：TestLocalSmoke ≤128 KiB/轮（300 轮长跑，R32 后台单）。
5. 深挖档：pprof 诊断长跑分离真占用/虚胖分量；**门语义（用户已裁决 2026-11-05）：过关线量『真占用分量』（GC 后稳定常驻），虚胖（已归还未收回）单独记录不卡关，观测上限=真占用门的 50%（64KiB/轮），超限记告警事实**；空闲地址回收的长期负载验证；缓存淘汰基准（淘汰不劣化热路径，同树对照 R25）。
6. **装配侧读面有界（F5；任务 1.2a；r9 F13/F14/F15 收口）**：assembly 的观察面（ObservedEvents 合并的已提交事实）、回放注册表 `r.replays` 与已提交事实表 `r.facts` 事实窗口内化——事实窗口外按索引/前缀从持久段回读，事实窗口内有显式预算；**未清交付事实（held/owed 活动集）不落事实窗口外；DeliveryConsumer 持有的义务恒驻事实窗口内**，该面与 01 恢复共用、跨域集成测试归 1.2a；进程级断言由 1.5 的 300 轮长跑承担。

## 边界与依赖

- **依赖**：W1 全入 main（03 触碰 smoke 面与 02 有集成序）。
- **被依赖方**：07 training-sample-loop 需要长跑稳定环境；评审下一轮"长期资源稳定性已验证"结论以本域 run 为凭。
- **接口面**：不改 api/**；存储文件格式不变（条目级驻留是内存策略，不是磁盘格式变更）；LRU/缓存常量集中定义。
- **禁止事项**：不动 assembly/ 的**其余部分**（1.2a 列明的 read.go/run.go 三处/host_tools.go 与 memory/store_query.go 除外）；不动 tool/exec、memory/replay；不引入外部缓存依赖（纯标准库实现）；不降低事件完整性门。
