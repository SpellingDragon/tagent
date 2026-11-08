# 静态决策（restart-recovery-optimization）

## D1 症状直译锚（R33/R34）

- 锚 A（曲线）：本地与 CI 各一次完整 120 重启，`boot_ms` 不再随代数增长（当前 66ms→2885ms 为红）。
- 锚 B（CI）：全 6 分片在重冻预算内绿（当前 9011s 到第 28 代失败为红）。
- 测量锚先行：阶段 2 产出可判定证据；测试代码按用户显式裁定后置到阶段 6。
- 被否：以「15 分钟没 ready 判失败」为症状——那是判据不是症状；以「O(历史) 单独解释 15 分钟」为诊断——无实证，已由本地实测曲线否证。

## D1a 重启恢复形态（用户裁定，r17/r19/r20）

「重启」是一个事件，恢复就是它的消费逻辑两步：①装载未消费的事件；②装载（唯一一份快照）或重放内存状态。两步完成、且服务开放门（face.start）之前，才写完成点票据。票据是链上事实 recovery.ticket，快照是固定键下唯一一份可覆盖 blob；任何失败退回全量事件流。由此删掉「压缩时写票」（运行中写有并发）——完成点在服务开放前，但 settle pump 已 Wake，锁仍是真实必需。

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | vocabulary | restart-recovery-vocabulary | 改名与术语表；契约值（MetaKeyNoticeSource 等）显式 delta，不混改 | 无 |
| 02 | timing-evidence | recovery-timing-evidence | 只加观测，不改恢复语义 | 无 |
| 03 | compression-cursor | compression-recovery-ticket | 链上票据 + 外部只读 blob + 失败退回全量 | 02 的计时证据 |
| 04 | partition-cursor | startup-active-partition-bitmap | 位图 + 游标点读，等价回退 | 03 稳定后 |
| 05 | ci-orchestration | event-integrity-ci-budget | 分片/超时/liveness，不动数据准确性断言 | 02/03/04 |
| 06 | guards-and-tests | recovery-boundary-guards | F0 边界 + R3 登记 + 测试重写 | 01–05 |

## D3 波次（仅汇报分组，解锁 = 依赖入 main）

W0 阶段 0（回滚 + 建档）→ W1 阶段 1（01）→ W2 阶段 2（02）→ W3 阶段 3（03）→ W4 阶段 4（04）→ W5 阶段 5（05）→ W6 阶段 6（06）→ W7 收尾。每阶段末必须经「子代理拷问 → 主代理回答 → 子代理完善 → frontier 空」才进入下一阶段。

## D4 合并规则

- 共享面（FACT-VOCABULARY、golden/runtime/durable_truth.json、GLOSSARY、ci.yml、smoke_harness_test.go）由编排者单写；子代理写入白名单精确到本阶段文件。
- 同文件簇合单或串行；每阶段完成以复跑凭据为准，不采信汇报。
- 阶段间不「下一阶段再说」：悬置项若非真裁决点即流程债。

## D5 三级验收

- 孙任务：每单 `exit 0` + 可复跑命令（正锚/负锚/消费者锚随任务行冻结，R29）。
- 子域：该域 scenarios 全绿 + 独立拷问 frontier 空。
- 变更级 DoD：① 锚 A 红转绿 ② 锚 B 红转绿 ③ 数据准确性对账（1200/120、身份不重不漏、fault、drain、storage、folds）全绿 ④ F0 五规则 + 新增边界规则全绿 ⑤ 主 spec `--strict` 全绿 ⑥ 症状锚红转绿两次证据入 run notes。

## D6 dump 编码取舍（被否方案）

- **采用**：链上票据 JSON 确定性字节（`schema_version` + 内容指纹 + 长度 + 覆盖到的最后 fact key/revision 作为游标）；外部状态 blob 普通读。
- **被否**：0 拷贝内存映射。理由：① 恢复态是可变 Go map，mmap 只读页不兼容，需重写整个访问层并引入 `unsafe`（全仓 0 处先例）；② 瓶颈在 Scan 次数 × 并发抢盘，不在反序列化 CPU；③ 引入新机制面须单独立项。
- **边界澄清（用户裁定）**：durable dump 必须是链上事实；消费该事实的行为可以读外部文件——外部 blob 是唯一一份固定键、按票据 digest 校验的只读产物，不构成第二真相，仍为拓展。R3 登记该引用面。

## D7 不变式（03 核心）

1. 全量重放 ≡ dump + 其后的增量（逐位一致）。
2. 票据缺失/截断/校验不过/版本不认识 → 退回全量事件流，绝不猜。
3. blob 与票据按 rename 序落地；本后端 `Sync` 为 no-op、无 fsync，失败窗口一律退回全量重放（r19/r20 诚实化）。
4. 票据字节确定（map 排序后序列化），身份内容寻址稳定。
