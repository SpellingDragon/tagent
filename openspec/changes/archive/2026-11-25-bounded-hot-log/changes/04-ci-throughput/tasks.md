# Tasks · 04 CI 吞吐

> 三锚口径（c10）：正锚＝点名测试＋可复跑命令；负锚＝具名错误路径；消费者锚＝生产入口调用证据；三锚随叶行冻结。

- [x] 04.401 缓存接线 —— 写面：`.github/workflows/ci.yml`（setup-go cache、module/build cache；键含 go.sum/lock）。缓存不参与判定；冷热两轮均须绿。 —— 验证：正锚＝workflow 静态断言（`python3 -c` 解析 YAML 断言 cache 键与键源）＋真 CI 冷/热两轮 run 读数（缓存命中行原文入档）；负锚＝缓存键剥离 go.sum 时静态断言红；消费者锚＝真 CI run 日志的 cache restore 行。
- [x] 04.402 race 分片 —— 写面：`scripts/gates/race_check.sh`（`--shard` 定义＋并集自检）、`scripts/gates/test_race_check.sh`（分片自测）、`.github/workflows/ci.yml`（race matrix＋`race-aggregate` fail-closed）。 —— 验证：正锚＝`bash scripts/gates/test_race_check.sh`＋`bash scripts/gates/race_check.sh --shard-union-check`（3 分片并集 diff 全包集为空）；负锚＝从 shard 定义删一个包→并集断言红（构造注入）；**assembly 选择器负锚**＝odd/even 缺失或不互补→并集断言红；**走查补负锚**＝race＋`[build failed]` 混合日志、普通 `--- FAIL` 无 race 证据均不得被 waiver 吞成 0；消费者锚＝真 CI 的 `race-aggregate` job（任一分片红则聚合红）。
- [x] 04.403 真 CI 读数与证据 —— 写面：`changes/04-ci-throughput/evidence/`（只留 md 小结；运行产物不入库）。 —— 验证：正锚＝前后两次 run 的 `gh run view --json jobs` 分解表＋墙钟对比（关键路径＝`race-aggregate.completedAt - run.startedAt`，< 原单 race job 60%、run 墙钟下降）；负锚＝分片聚合缺项（任一分片未接线）时证据判无效；消费者锚＝真 CI 聚合 job 与门面读数原文。
