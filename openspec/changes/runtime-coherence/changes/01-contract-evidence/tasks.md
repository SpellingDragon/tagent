# Tasks · 01 契约与取证

> 本子变更不改产品实现。唯一允许的新增代码是两个症状锚测试文件；其余写面是本子变更文档与隔离区探针。
> 三锚口径（c10）：正锚＝点名测试＋可复跑命令；负锚＝具名错误路径；消费者锚＝生产入口调用证据；三锚随叶行冻结。

- [x] 01.101 归档核验、审计复现与保留/删除清单 —— 产出：`audit.md`（逐项“已复现/已驳回＋反证/设计差距”）；新增 `assembly/symptom_anchor_coherence_test.go`（S1，红）与 `runtime/task/approval/symptom_anchor_ruling_test.go`（S2 探针，红或具名驳回）；隔离副本探针只落仓库外。 —— 验证：`go test ./assembly -run '^TestSymptomAnchorHostBootMountsContributedPlugin$' -count=1`（预期红；脚本须落原始退出码）；`go test ./runtime/task/approval -run '^TestSymptomAnchorFailedRulingKeepsTheHold$' -count=1`（红或附反证）；`git -C openspec/changes/archive/2026-11-22-runtime-recovery-ideal rev-parse HEAD` 与归档文件清单核验。负锚：已驳回项必须附可复跑反证命令。消费者：正式 `Boot` 与真实 `ApprovalManager` 入口，不用 helper 替代。
  —— 勾选凭据（2026-11-22 Wave 1 收口）：S1/S2 均已复现（编排者亲验 rc=1×2，复跑同红）；11 项判定入 `audit.md`；归档对 HEAD 零 diff、12 项齐全；证据四件在 `evidence/`；计时项 W1-F 独立执行、未完成不阻塞本勾选（其归 04.402 面）。

- [x] 01.102 冻结状态派生与切面覆盖契约 —— 产出：`contracts/state-derivation.md`，写明每消费者 apply 输入/身份/幂等/提交结果/失败语义、切面覆盖自述、缺片/空/损坏/尾部语义、恢复零副作用、时钟与观测在 apply 之外、恢复前置关系的声明方式。 —— 验证：`grep -c '^## ' contracts/state-derivation.md`（≥ 6）且 `grep -n '缺片' contracts/state-derivation.md` 命中；对照 `go test ./assembly -run '^(TestNewConsumerRegistersSliceOnly|TestNoPerConsumerDispatchPath|TestRecoveryEntriesPreserveStateAndObligations)$' -count=1` 记录当前可观察行为。负锚：契约须写明“缺片不得按空状态启动”的具名拒绝/回读路径。消费者：`Run.RegisterStateSlice` 与 boot 恢复入口。
  —— 勾选凭据（2026-11-22）：契约 69 行 7 节、repeat≠filing 落定、五态表与审批三态入约、11 锚落点（7 存 4 新）；唯一待裁点（Stateful 入口门）由编排者裁甲 `WithStateSlice` 并写入契约§六。

- [x] 01.103 冻结存储读取契约 —— 产出：`contracts/store-reads.md`，写明点读与扫描语义、`不存在` 与 `读失败` 的区分、已知键不扫族、派生索引失效答案等价、`CommitOrder` 可选性与同能力包装等价、成本归引擎。 —— 验证：`go test ./memory/ledger -run '^TestAtomicStoreContract$' -count=1`；`go test ./memory/ledger -run '^(TestJournalStatesItsCommitOrderNewestFirst|TestOrderLineIndexFollowsTheTableItParallels)$' -count=1`；01.101 的点读探针原始输出入档。负锚：读失败必须具名上返，不得答不存在。消费者：`runtime/state` 回执读取与 `governance` 尾读的真实调用面。
  —— 勾选凭据（2026-11-22）：契约 53 行；裁定＝`FactLog` 四面（Put/Get/Scan/Sync，三处实现破口点名：memoryFactLog/stagedLog/lineageFactLog）＋索引方案乙（读侧身份校验，谓词＝走查命名进入键）；两叶共 6 锚落点。

- [x] 01.104 冻结插件装配与材料分派契约 —— 产出：`contracts/plugin-assembly.md`，写明贡献来源、`Assemble`/`Boot`/reload 统一接线、工厂组合顺序与恰好一次、失败清理、作用域与换代归属；材料声明→分派映射，保留六端口与兼容读法。 —— 验证：`python3 scripts/gates/check_plugin_mounting.py --root .`（exit 0）；`go test ./assembly -run '^(TestHostPluginsMountThroughTheCatalog|TestDeclaredPluginChoiceSurvivesRestart|TestPluginDeclarationRefusesAnUnregisteredName)$' -count=1`；入口矩阵表覆盖 `Assemble`、`Boot`、reload 各一行。负锚：未注册名与跨级名具名拒绝。消费者：正式 `Boot` 装配回调与 reload 路径。
  —— 勾选凭据（2026-11-22）：契约 51 行；裁定＝工厂序列＋追加＋声明序执行（02.203）＋reload 裁乙（零行为变化，ADR-0007 一致）；三锚落点（S1 承接＋失败清理负锚＋reload 存活锚）。
