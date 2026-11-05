# 资源有界：孙任务

## 0. 改造前基准（before）

- [ ] 0.9 基准函数与 before 数（1.4 前置；r9 F9）：在 memory/ledger 测试文件新增 `BenchmarkLocalKVGet` 与 `BenchmarkSegmentWindowRead`（仅测试代码，不改生产路径），并在 03 改造入树前采集 before 数 —— 验证：`go test ./memory/ledger -v -run '^$' -bench 'BenchmarkLocalKVGet|BenchmarkSegmentWindowRead' -benchmem -count=6` 输出含两个 Benchmark 的迭代行（`[no test files]` 或 0 迭代=无效重采）；before 数字入台账。

## 1. 实施

- [x] 1.1 KV 条目级驻留：shard 快照仍是持久单位，内存驻留改为**条目级 LRU**（key→value，字节预算常量集中定义；冷条目淘汰后 Get 按 shard 快照文件读回该键）——`TestLocalKVEntryResidencyBounded`（注入超大 shard，**以进程级内存断言（强制 GC 后 ReadMemStats），非 LRU 自身计数**——shard map 本体必须释放被淘汰值）+ `TestLocalKVColdEntryReadback`（淘汰后 Get/Scan 读回等价+重启等价）+ 负锚 `TestLocalKVColdEntryMiss` —— 验证：`go test ./memory/ledger -v -run 'TestLocalKVEntryResidencyBounded|TestLocalKVColdEntryReadback|TestLocalKVColdEntryMiss' -count=1` 用例数=3 PASS。
- [ ] 1.2 事件缓存冷读与惰性 Load：**内存保留既有 `{pid}:idx:{event_key}` 键的轻量映射（键驻留=既有索引的驻留形态，值=`{window}:{seq}` 极小；不另立第二份读路；body 仍惰性）**；GetEvent miss 由该映射定位 pid→窗口段读回；**kvPort 增按前缀的键值枚举（prefix-pair scan）**：localFileKV 的实现=内存 shard+冷读合并（淘汰条目按 shard 快照读回），memKV/rustvikingKV 同步实现（三实现入触碰集）；**Load 经两次前缀枚举（idx 前缀 + meta 前缀）只物化 idx/meta 小键值、不物化 evt body**（重启后 idx 映射由 Load 自身读 idx 键重建）；Events 枚举走窗口 meta 流式 ——`TestSegmentCacheBoundedAndEquivalent`（淘汰+读回等价）+ `TestSegmentGetEventColdMiss`（淘汰后按键读回）+ `TestSegmentLoadLazyPeak`（**启动峰值有界；以进程级内存断言，非缓存自身计数**）+ `TestSegmentCacheRespectsByteBudget` + **`TestColdReadAfterRestart`（重启后首次 GetEvent 冷读成功）** —— 验证：`go test ./memory/ledger -v -run 'TestSegmentCacheBoundedAndEquivalent|TestSegmentGetEventColdMiss|TestSegmentLoadLazyPeak|TestSegmentCacheRespectsByteBudget|TestColdReadAfterRestart' -count=1` 用例数=5 PASS。
- [ ] 1.2a 装配侧读面有界（F5；r9 F13/F14/F15）：assembly 的 ObservedEvents 合并事实、`r.replays` 注册表与 `r.facts` 已提交事实表**事实窗口内化**——事实窗口外按索引/前缀从持久段回读、事实窗口内显式预算；**未清交付事实（held/owed 活动集）不落事实窗口外；DeliveryConsumer 持有的义务恒驻事实窗口内**；触达集=`assembly/read.go`、`assembly/run.go`（三处枚举）、`assembly/host_tools.go`、`memory/store_query.go`；该面与 01 恢复共用、跨域集成测试归本任务（01 的 `TestSettlementFactsStayHotWhileUnsettled` 锁定义务驻留）——`TestAssemblyObservedReplayFactsBounded`（进程级内存/条目断言）+ `TestAssemblyColdWindowReadback`（事实窗口外回读等价）—— 验证：`go test ./assembly -v -run 'TestAssemblyObservedReplayFactsBounded|TestAssemblyColdWindowReadback' -count=1` 用例数=2 PASS。**单测只锁窗口预算；最终进程级断言由 1.5 的 300 轮长跑承担（日志+斜率）。**
- [x] 1.3 空闲地址回收与可观测（F13）：`TestIdleAddressReclaimed` + 负锚 `TestActiveAddressNotReclaimed` + `TestRecycleObservable`（回收具名计数递增＋日志含地址与原因；虚胖告警同观测面）—— 验证：`go test ./runtime/agent -v -run 'TestIdleAddressReclaimed|TestActiveAddressNotReclaimed|TestRecycleObservable' -count=1` 用例数=3 PASS。
- [x] 1.4 淘汰基准（命令修正：非空转证明）：**before 数由 0.9 在改造入树前采集**；after 同命令同机同参数（`go test ./memory/ledger -v -run '^$' -bench 'BenchmarkLocalKVGet|BenchmarkSegmentWindowRead' -benchmem -count=6`，**输出须含迭代行**，[no test files]=无效重采）；比较脚本 10% 阈值出 exit code（入库 scripts/）—— 验证：两组数字（各含迭代行）+比较脚本 exit 0 入台账。
- [x] 1.5a 真占用门实现：改 smoke_test.go 判据与 smoke_harness_test.go 采样器——判定数=强制 GC 后稳定窗 HeapInuse 斜率；虚胖分量=RSS−HeapInuse 单独记录（**当前 RSS 采样：linux 读 `/proc/self/statm`，darwin 走 `task_info`；任务注明平台实现与不可用时的具名 skip**），**观测上限=真占用门的 50%（64KiB/轮），超限记告警事实**；`TestSmokeGateTrueResidency`（与 smoke 同文件、build tag smoke）—— 验证：`cd examples/wechat-bot && go test -tags smoke -run 'TestSmokeGateTrueResidency' -count=1 -v ./...` PASS + 判据 diff 入台账。**`TestSmokeGateTrueResidency` 只锁判据/采样器；进程级内存断言由 1.5 的 300 轮长跑承担（长跑日志+斜率数字），不以单测冒充。**
- [ ] 1.5b 症状锚（慢）自有红证：W2 首日、**W1 合并增量（01+04+05）已入 main、02/03 未入树**时重跑 1.5 同命令取红证（该树含 **W1 合并增量（01+04+05）** 增重——斜率同时给出与 48d8eb93 评审数的差值，即 W1 合并增量估计；不再称“与 48d8eb93 同源基线”）—— 验证：红输出（斜率>128）+ 差值数字入台账。
- [ ] 1.5 症状锚⑤（慢）转绿（命令修正）：`cd examples/wechat-bot && go test -tags smoke -run '^TestLocalSmoke$' -count=1 -timeout 2h -v ./...` exit 0（非空转证明=日志 rounds=300+斜率数字）；preflight 超时调整由 1.5c 独立承接 —— 验证：命令 exit 0 + 数字入台账。
- [x] 1.5c preflight smoke 段超时放宽（F20 定版；r9 F12 收口）：**修改 `scripts/gates/preflight.sh` 的 smoke 段超时 30m→75m**（覆盖 30-60 分钟上界+裕量；只放宽时限、不改门语义）+ **CHANGELOG 记录**；属门参数调整，须在 F2 前向 lead 报备（报备措辞不变）—— 验证：`grep -n 'timeout 75m' scripts/gates/preflight.sh` 命中该行 + CHANGELOG 条目 + 报备记录入台账。
- [ ] 1.6 pprof 深挖：诊断长跑产出归因表（每源 KiB/轮），**含“W1 合并增量（01+04+05）”单列**（F29）；若修后仍超门，证据链上报法定停点 —— 验证：诊断脚本 exit 0 + 归因表含 W1 合并增量列。
- [x] 1.7 域门禁（8 包 ok＋concept ok errors:[] baseline_stale:0）：`go test ./memory/... ./runtime/agent... ./assembly -count=1` + 事件完整性相关既有测试不回归 + **concept budget 门自清**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空（新增字段未导出+方法访问；不新增导出类型）—— 验证：go test exit 0 + concept budget `errors: []`。
- [ ] 1.8 主 spec 条款回归与冷读契约：`TestSegmentLazyLoadPreservesEventCount`（启动计数恢复=窗口 meta 流式计数）+ `TestSegmentDedupViaIndex`（判重走索引）+ `TestSegmentColdReadFailureSurfaces`（读失败不冒充缺失）+ `TestSegmentLazyLoadPreservesConsumerContract`（消费者契约不变）—— 验证：`go test ./memory/ledger -v -run 'TestSegmentLazyLoadPreservesEventCount|TestSegmentDedupViaIndex|TestSegmentColdReadFailureSurfaces|TestSegmentLazyLoadPreservesConsumerContract' -count=1` 用例数=4 PASS。（主 spec `event-segment-store` 的 DeleteEvent/墓碑 eventCount 条款与 eventCount「从事实链重建/count_known=false」条款在 HEAD 均无实现（全仓无 DeleteEvent、count_known 0 命中），本域不实现不改条款、不设测试；归档导览（F4 ⑥）单列该债。）

## scenario ↔ 测试映射（F27）

| spec scenario | 测试 |
|---|---|
| 冷条目卸载后读回 | TestLocalKVColdEntryReadback / TestSegmentGetEventColdMiss |
| 超预算淘汰 | TestLocalKVEntryResidencyBounded / TestSegmentCacheRespectsByteBudget |
| 惰性 Load 峰值有界（启动峰值，进程级） | TestSegmentLoadLazyPeak |
| 重启后首次冷读 | TestColdReadAfterRestart |
| 长跑后窗口有界 | TestAssemblyObservedReplayFactsBounded + TestAssemblyColdWindowReadback |
| 未清交付事实驻留窗口内 | TestSettlementFactsStayHotWhileUnsettled（01 域） |
| 空闲回收与重建 | TestIdleAddressReclaimed |
| 在途/义务未清不回收 | TestActiveAddressNotReclaimed（含谓词分支） |
| 回收可观测 | TestRecycleObservable |
| 300 轮长跑（真占用） | 1.5 命令（exit 0+斜率） |
| 虚胖可观测 | TestSmokeGateTrueResidency + 台账虚胖列 |
| 主 spec 两条款（eventCount/判重；墓碑条款无实现=规格债） | TestSegmentLazyLoadPreservesEventCount / TestSegmentDedupViaIndex |
| 冷读失败显形 | TestSegmentColdReadFailureSurfaces |
| 消费者契约不变 | TestSegmentLazyLoadPreservesConsumerContract |

> **验证约定（r4 F14）**：本文件所有 `go test -run` 验证命令默认附加 `-v`；“用例数=N”=输出中 `--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。
