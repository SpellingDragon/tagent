# O4 叶任务（均未执行）

- [x] 4.1 编排者扩充现有离线基准，记录真实库存规模和before输出，建立扫描参照对拍 —— 验证：`go test ./tests/offline_bench -run '^$' -bench 'Benchmark(PartitionScan|SegmentQuery|SnapshotCommit)$' -benchmem -benchtime=1x -count=5`；报告含基线HEAD与fixture规模。
- [x] 4.2 实现保守的分区定向Scan/Range及模糊/跨桶回退，消除全库展平分配 —— 验证：`go test ./memory/kv -run '^TestLocalFileKV_PartitionScanEquivalence$' -count=1`。
- [x] 4.3 收敛查询header解码和去重容器，保持时间/分页/高层优先/partial错误 —— 验证：`go test ./memory -run '^TestSegmentQuery_HeaderParity$' -count=1`。
- [x] 4.4 实现有界快照编码复用候选及错误dirty保持，与标准JSON输出对拍 —— 验证：`go test ./memory/kv -run '^TestLocalFileKV_SnapshotEncodingEquivalence$' -count=1`；O4.7不达门必须撤候选。
- [x] 4.5 加关系append失败红测并修正内存边发布顺序，不新增事实与关系联合事务 —— 验证：`go test ./memory -run '^TestRelationStore_FailedAppend$' -count=1`；该项解锁O1.6。
- [x] 4.6 执行无Close重启/屏障失败/墓碑租约/TTL与全部查询回归 —— 验证：`go test -short -race ./memory/... -count=1`；跨进程新增TestStorageBarrier_FreshProcess必须PASS。
- [x] 4.7 重跑O4.1矩阵并逐cell比较，保留原始负结果，按D17判定编码缓存保留或撤回 —— 验证：`go test ./tests/offline_bench -run '^$' -bench 'Benchmark(PartitionScan|SegmentQuery|SnapshotCommit)$' -benchmem -benchtime=1x -count=5`；结论报告含全部cell与阈值判定。
- [x] 4.8 交编排者同步查询路径/快照成本与耐久边界文档，明确未减少磁盘写放大 —— 验证：`bash scripts/lint.sh && bash scripts/check-openspec.sh`。

> 编排者核销（W1）：证据 /tmp/tagent-w1v/m3.log（memory 1.314s / kv 1.029s）。4.5 经 R8 裁决扩展至 RemoveRelations 同型缺陷（journal 先行、失败保此前边），回归并入 TestRelationStore_FailedAppend，REL=0。4.1/4.6/4.7（bench、race、跨进程 barrier）留 W4 独占槽。

> 编排者核销：4.6=go test -short -race ./memory/... 全绿（race 门 /tmp/tagent-w4/race.log 0 RACE 0 WAIVED；跨进程 TestStorageBarrier_FreshProcess PASS）。4.1/4.7=partition_bench 正式矩阵执行中。

> 编排者核销（W4）：判决表 /tmp/tagent-w4/bench_full.log——PartitionScan +76~90%、SegmentQuery 100k 2.11s→0.12s(+94%)；SnapshotCommit -17.8% 超阈 → **编码缓存候选已撤回**（codec 设施摘除、flushLocked 回直接 marshal），撤回后复测 3.49ms vs before 3.99ms 非退化、memory -race 定向复跑绿。写盘字节不变（等价对拍随候选一并移除，屏障语义由既有用例守护）。4.8=D。真实库存：常规 1k/10k/100k×1KiB、大正文 512×64KiB，16 分区，预热+10 轮取中位。
