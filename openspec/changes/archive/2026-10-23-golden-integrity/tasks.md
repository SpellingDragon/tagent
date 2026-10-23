# Tasks：golden-integrity（两单并发）

- [x] G1 核心单：删 ReplayBudget 配置链＋窗口驱动投影注册＋compress_threshold/keep_recent_tasks 配置（缺省全默认）＋recordMapped 触发 FoldByAge＋装配余量推导＋三锚测试 —— 验证：go test ./... -count=1 绿＋TestAgeFoldFiresFromCommits（生产路径锚：提交即折）＋TestUnderThresholdUntouched＋TestRebuildRefoldsIdempotent
- [x] G2 replica 单：删 memory/replica 包＋compress_filing＋S07 场景改写（保留 owner isolation）＋golden architecture 同步 —— 验证：go test ./... -count=1 绿＋grep -rln "memory/replica" --include="*.go" 生产零＋golden match
- [x] F1-F3 收尾：37包绿＋wechat绿＋golden match（S13/S14 生产路径锚）＋architecture ok＋lint ok：全量门禁＋golden deterministic（S13/S14 attestation 生产路径）＋lint ok
