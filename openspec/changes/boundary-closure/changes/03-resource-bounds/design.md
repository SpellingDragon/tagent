# 资源有界：技术要点

## 文件清单

- memory/ledger/localfile.go：条目级驻留+LRU（localFileKV）；写路径=旧快照+脏合并+原子 rename；**按前缀的键值枚举（prefix-pair scan）=内存 shard+冷读合并**（被淘汰条目按 shard 快照读回）；**memKV/rustvikingKV 同步实现该前缀枚举（三实现入触碰集）**
- memory/ledger/segment_store.go：cache 有界 LRU + **内存保留既有 `{pid}:idx:{event_key}` 键的轻量映射（键驻留=既有索引的驻留形态，值=`{window}:{seq}` 极小；不另立第二份读路；body 惰性）** + 惰性 Load；GetEvent miss 由该映射定位 pid→窗口段读回；**Load 经两次前缀枚举（idx 前缀 + meta 前缀）只物化小键值**
- assembly/read.go、assembly/run.go（三处枚举）、assembly/host_tools.go、memory/store_query.go：Events 枚举调用点改流式（不物化全量）；**同一触达集承担 1.2a 的 Observed/Replay/r.facts 事实窗口内化（事实窗口外按索引/前缀回读、事实窗口内显式预算；未清交付事实不落事实窗口外）**；**该面与 01 恢复共读，跨域集成测试归 1.2a**
- runtime/agent（consumer 注册表所在）：空闲 TTL 回收
- examples/wechat-bot/smoke_test.go、smoke_harness_test.go：斜率门判据与采样器（真占用）
- **scripts/gates/preflight.sh**：smoke 段超时参数 30m→**75m**（覆盖 30-60 分钟上界+裕量；门参数调整，不改语义；任务 1.5c）
- tests/integration/execution/debug_pprof_test.go（或等价诊断入口）：长跑诊断
- golden/runtime/architecture.json：本域预期不新增包/边；如新增诊断包或包边变化，登记 packages+edges
- 各自 *_test.go：读回等价/淘汰/回收测试与基准

## 既有能力裁决（5.9）

- `event-segment-store`/`partition-local-storage-access`：本域只改**内存策略**（条目级驻留/淘汰），持久化语义与既有条款零冲突；淘汰后读回等价测试即是对既有 Scenario 的回归证明。
- `relation-store-lru-rocks`：LRU 先例（关系存储已做过），本域把同一纪律扩展到 KV 分片与事件缓存——模式一致，无条款冲突。

## 关键决策

- KV 驻留（r2 F7 废止 shard 页方案；淘汰单位=**条目级**）：shard 快照=持久单位；**内存驻留=条目级 LRU**（key→value，字节预算；热 shard 的驻留字节同样受预算约束；冷条目淘汰后按 shard 快照读回该键）；写路径 dirty 条目常驻直至 barrier。**writeShard=读旧快照+合并脏条目+原子 rename**（被淘汰条目不因内存缺席而从持久快照消失）；Scan=磁盘快照流式+内存脏合并。kvPort **增按前缀的键值枚举（prefix-pair scan）**：localFileKV 实现=内存 shard+冷读合并（被淘汰条目按 shard 快照读回）；memKV/rustvikingKV **同步实现**该枚举（三实现入触碰集）；不再以“已有前缀扫描”为由把其余实现排除在触碰集外。**Load 经两次前缀枚举（idx 前缀 + meta 前缀）只物化 idx/meta 小键值、不复制 evt body**。
- 事件缓存（r2 F8 对齐主 spec；r5 收窄；r6 F3 口径唯一化；F18 定版）：条目级 LRU+字节预算；**内存保留既有 `{pid}:idx:{event_key}` 键的轻量映射（键驻留=既有索引的驻留形态，值=`{window}:{seq}` 极小；本域不另立第二份读路；body 仍惰性）**（append 时索引已由 StoreEvent 写入）+ **惰性 Load**；**Load 用 kvPort 的两次前缀枚举（idx 前缀 + meta 前缀）只物化 idx/meta 小键值，不物化 evt body**（重启后 idx 映射由 Load 自身读 idx 键重建）；**GetEvent miss 由该映射定位 pid→窗口段读回**；惰性化 MUST 保住 `event-segment-store` 主 spec 的现有条款——①启动 eventCount 恢复=**窗口 meta 流式计数（HEAD 现状语义保持；不做墓碑计数——主 spec 的「从事实链重建/count_known=false」条款无实现，见③规格债）**；②StoreEvent 判重（走同一既有索引，不另立第二份读路）；③**规格债（r6 F8/F22）**：主 spec `event-segment-store` 的 DeleteEvent/墓碑 eventCount 条款与 eventCount「从事实链重建/count_known=false」条款在 HEAD 均无实现（全仓无 `DeleteEvent`、`count_known` 0 命中），本域不实现不改条款；归档导览（F4 ⑥）单列该债。Events 枚举改流式（调用点 assembly/read.go:94、assembly/run.go:1425/1461/1541、assembly/host_tools.go:902、memory/store_query.go:72 入触达集）。
- 冷读失败语义：GetEvent 冷读区分“不存在”与“读失败”（后者报错不冒充缺失——主 spec 条款）；补 `TestSegmentColdReadFailureSurfaces` + `TestSegmentLazyLoadPreservesConsumerContract` + **`TestColdReadAfterRestart`（重启后首次 GetEvent 冷读成功）** 与热路径回归。
- 空闲回收（r3 F23 跨包方向修正）：回收条件含**未清交付义务**，而义务状态在 assembly、回收在 runtime/agent（不可反向 import）——由 assembly 在装配层向地址注册表**注册“交付待清谓词”**（runtime/agent 暴露注册接口，assembly 提供实现；依赖方向=assembly→runtime/agent 合法）；无在途段+无未收通知+谓词为假超 TTL 才回收；**谓词读 owed/held 两类事实，含 held(reason=retrying) 的重试间隙即不回收**；Ensure 幂等重建；回收可观测＝具名计数递增＋日志含地址与原因，虚胖告警同观测面（F13；`TestRecycleObservable`）。
- 斜率归因（用户裁决口径；F14 给值）：过关线量**真占用**——固定轮数后强制 GC+稳定窗采样 HeapInuse 斜率（判定数）；虚胖（RSS−HeapInuse）单独记录、不卡关，**观测上限=真占用门的 50%（64KiB/轮），超限记告警事实**；瞬时峰值另记不判定。**改 smoke 判据与采样器是实现任务 1.5a**，不是“保持不改”。**`TestSmokeGateTrueResidency` 只锁判据/采样器；进程级内存断言由 1.5 的 300 轮长跑承担（长跑日志+斜率数字）**。
- **装配侧事实窗口内（F5，任务 1.2a；r9 F13/F14/F15 收口）**：ObservedEvents 的已提交事实合并、`r.replays` 注册表与 `r.facts` 已提交事实表 MUST 以事实窗口内为界；事实窗口外按索引/前缀从持久段回读（不复制全量 body），事实窗口内大小显式预算，由 `TestAssemblyObservedReplayFactsBounded`/`TestAssemblyColdWindowReadback` 锁定；**未清交付事实（held/owed 活动集）不落事实窗口外；DeliveryConsumer 持有的义务恒驻事实窗口内**——这是 01 恢复读面（结算事实与 owed 均读 `r.facts` 事实窗口内）包含全部未清交付事实的机制；该面与 01 共用、跨域集成测试归 1.2a；1.5 长跑给进程级断言。
- 基准（R13/R25；r9 F9 收口）：**before 数必须在改造入树前采集**（0.9 先落 `BenchmarkLocalKVGet`/`BenchmarkSegmentWindowRead` 并采数，1.4 引用之）；淘汰前后同树对照热路径（Get/读窗）延迟，比较脚本带 10% 阈值 exit code。
- 联合归因（F29；F8 更名）：1.6 归因表单列 **W1 合并增量（01+04+05）**；W2 首日、**W1 合并增量（01+04+05）已入 main、02/03 未入树**时 1.5b 取**症状锚（慢）**自有红证（该树含 W1 合并增量增重——斜率同时给出与 48d8eb93 评审数的差值，即 W1 合并增量估计）；若该增量使门不可达→法定停点上报（回退事件化或门留头寸，用户裁）。
- D6 声明：本域不另立第二份读路（**内存保留既有 `{pid}:idx:{event_key}` 键的轻量映射，值=`{window}:{seq}`**；GetEvent miss 由映射定位 pid→窗口段读回）；**preflight 对齐（F20 定版；r9 F12 收口）**：smoke 段超时参数 30m→**75m** 是 03 域显式任务（1.5c），产出=修改 `scripts/gates/preflight.sh` 该行＋CHANGELOG 记录；属门参数调整（不改语义只放宽时限），须在 F2 前向 lead 报备（报备措辞不变）。**concept budget 门自清（F11）**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空；本域新增字段一律未导出+方法访问（如需），不新增导出类型（若新增则先登记 concept_bindings）。

## 风险与回退

- 条目淘汰把成本移到读路径：以基准守门；劣化超标回退为“只对冷条目淘汰”。
- 回收误伤在途工作：回收条件必须包含“无在途段且无未收通知”双条件+宽限窗；负锚测试覆盖在途不回收。
- 斜率仍超门的处置：证据链（真占用 vs 虚胖分离）+ 建议 → 法定停点上报；域内其余成果独立成立。
