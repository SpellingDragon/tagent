# Design · 04 CI 吞吐

## D0 机制归因（命题）

CI 耗时形状＝证明义务的镜像；优化只改变证明的**执行方式**。

- 关键路径＝race：红线「every first-party package + all integration suites」单 job 串行（ci.yml:170），`-race` 放大 3–5×；
- 资源大头＝event-integrity 家族：10 短分片各跑**完整 scope** 的独立轮次（确定性税，ci.yml:244 注释明示「every shard runs the complete scope」）；6 长分片＝3 seed×2 window；变异 23 个隔离副本编译；
- 墙钟膨胀＝无 actions/cache ＋ push/PR 双触发（24 job×2）＋并发排队（run 墙钟 22–26m vs 最慢 job 15m）；
- 不可删项：真实生命周期、`-race` 全覆盖、重复轮次、变异咬合、逐 boot 全日志 CRC（真相校验）。

## D1 缓存（04.401）

- `actions/setup-go` 开 `cache: true`（module+build）；如需更多共享，加 actions/cache 对 `~/go/pkg/mod` 与 `~/.cache/go-build`，键含 `go.sum`/锁文件。
- 约束：缓存**不参与判定**——门只看命令 exit；冷缓存与热缓存各取一轮证据均须绿；缓存键不含树内 .go 内容（Go build cache 内容寻址，安全）。
- 反事实：删缓存→每 job 冷编译 1–2 分钟；风险面（stale cache）由内容寻址与 go.sum 校验排除。

## D2 race 分片（04.402）

- **包集单点维护**：shard 划分定义在 `scripts/gates/race_check.sh`（`--shard <k>`），workflow 只传 shard 号；「所有 shard 并集 ≡ 原全包集」由脚本静态自检（缺包/重包即红），不新增第二份包清单。
- **分片**：core（assembly、runtime、memory、api、tool、features、internal…）／integration-a（execution、history、host）／integration-b（capabilities、evolution、learning）——按构建闭包与时长配比，落地时以实测重调。
- **聚合 fail-closed**：`race-aggregate` job `needs` 全部分片；任一分片非 success（含 skipped/cancelled）⇒ 聚合红；未接线分片数为零的断言入脚本。
- **语义保持**：race_check 的 exit-code 保真、分类器（waiver 仅对上游签名且无 plain 失败）、自测（test_race_check.sh）全部保留；分片只改变「谁在哪台 runner 跑」，不改变判定逻辑。

## D3 显式不做

- 重复轮次（10 短/6 长）不动：它们是「非确定性按缺陷处置」的执行面；
- 变异矩阵不删；按组拆分仅当被证明零语义变化才做（可选，非验收项）；
- 双触发去重不做：PR/push 门面语义变更，须单独裁，不在本域。

## D4 验收（真 CI 为权威面）

- 本地：并集自检、test_race_check.sh、workflow 静态断言（YAML 解析含 cache 与 shard matrix）。
- 真 CI：变更前后 job 分解与墙钟读数入 `evidence/`；关键路径目标＝race 墙钟 < 原单 job 的 60%；run 墙钟同步下降；冷/热两轮绿。
- 读数口径：同工作流、同提交内容（或等价）前后各一轮；job 名→墙钟映射用 `gh run view --json jobs` 原样入档。

## D5 停止条件

- 分片需要改变 race_check 的 exit-code 保真或分类器语义 → 停，回设计门；
- 缓存引入冷热行为差异（任一命令在两态下 exit 不同）→ 停；
- 任何优化需要删测试、缩包集、跳过或放宽阈值 → 拒绝，属越界。
