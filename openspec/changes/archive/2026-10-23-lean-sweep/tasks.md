# Tasks：lean-sweep（删旧＋wechat 重写——审计驱动）

> 审计结论：148K 行中 ~16,700 行 A 类硬删（零消费者/过程产物）＋~10,000 行 B 类待裁。wechat-bot 非main 全删重写。诸事皆允。

## 波1（D1-D4 文件零相交并发：硬删）

- [x] D1 删 tool/remote＋memory/replica＋evolution(根+refine+consolidation)＋tool/spec＋internal/prototype＋关联测试/范例 —— 验证：golden-integrity 变更实际执行（S07 改写＋golden 同步＋生产零残留）
  - 已删（≈7472 行）：tool/remote(6 文件)、internal/prototype(11)、evolution/refine(4)、evolution/consolidation(1)、examples/contract/{tool_remote,evolution_refine,evolution_meditation/CompressPhase_example}、tests/integration/{capabilities/addressed_delivery,evolution/git_native_refine,evolution/post_activation_evidence}；architecture.json 34 包/34 边策略绿
  - 按 design D1「唯一装配点（wechat-bot host_runtime）计生产」保留并待 W1 后裁定：memory/replica（wechat-bot/file_replica.go:11 投递生产路径）、evolution 根（host_runtime.go:785 装配治理；evolution.Policy 是 runtime/state.Governance 唯一实现，run.go:197 无治理即 ErrGovernanceMissing，cli-host/main.go:153 依赖）
  - tool/spec 判 B 类保留：`examples/wechat-bot/tagent.yaml` plan agent 真声明 `{kind: tool, id: spec}`
  - 待收口：docs/api 因删包过期 6 篇（W3 重生成）；根 module 当前红因 D2 在飞 run.go: ExecLeaseExplainer，非本单
- [x] D2 删 Run.Explain＋memory/migrate＋memory/compress_filing＋CompressPhase 别名＋Run.Recall＋config.Load＋关联测试 —— 验证：同上＋`grep -rn "\.Explain(ctx\|ArchiveOriginals\|CompressPhase\b\|config\.Load\b" --include="*.go" . | grep -v _test | wc -l` 零
  - 战报：六面全删（CompressRequests 连带其唯一消费者 Judge/Tick 簇，见战报偏离）；`go build ./...` 绿；grep gate=0；`go test ./... -count=1` 38 ok/0 FAIL；golden deterministic 14/14 match；docs/api 重生成 check 绿；lint 剩余红仅 doc-budget +3（本 change 未跟踪规划四件套 +4 减 D1 删 internal/prototype/README.md -1，待批末对账）；无 git commit。
- [x] D3 api/plugin 旧面删除（Plugin/Phase/6 route word/NewPipeline）＋11 处生产消费迁 Instance/Port＋116 处测试/范例迁移/删除＋api_plugin 范例重写 —— 验证：`grep -rn "plugin\.Plugin\b\|plugin\.NewPipeline\|plugin\.Phase\b" --include="*.go" . | grep -v _test | wc -l` 零＋`go test ./api/plugin/... ./runtime/... -count=1` 绿
  - 战报：旧符号全仓 `.go` 零（含测试/范例）；`go test ./api/plugin/... ./runtime/... ./tests/integration/... -count=1` 全绿；golden deterministic 14/14 match（S11 10 观测相等）；`go build ./...` 绿；docs/api 重生成 check 绿（顺带删除 D1 已删包的 6 篇陈旧生成文档）。lint 剩余红为跨单在飞：read.go gofmt（D2）、meditation wiring_test 引用已删 `WithCompressPhase`（D2）、tracked-hygiene 需本波统一暂存删除、doc-budget +3 为本 change 未跟踪规划文档。
- [x] D4 git rm --cached comment_policy＋.gitignore＋删一次性迁移脚本（6 个 .py） —— 验证：`git ls-files comment_policy codetools | wc -l` 零＋lint 绿

## 波2（W1-W3 文件零相交并发：wechat 重写＋B 类裁定）

- [x] W1 wechat.go 重写（微信协议适配一个文件：SDK/文件收发/路由/去重/send/approval/notice）＋main.go 重写（纯 wiring＋挂点注册）＋删全部旧文件（host_runtime/inject_face/file_*/dedup/session_routing/send_tool/approval_channel/reincarnation_notice/system_alert/delivery_receipt/meditation_delivery＋全部测试） —— 验证：`cd examples/wechat-bot && go build ./... && go test ./... -count=1`＋`ls *.go | wc -l` ≤3
- [x] W2 B 类裁定执行：meditation 引擎裁剪（保留水位/反射/观察，删 NewMeditation/ConsumerPhase 孤面）；rl 裁剪（保留 Recorder 通路，删 export/SFT/import/reward 孤面） —— 验证：`go test ./evolution/meditation/... ./rl/... -count=1`＋golden match
  - 战报：meditation 保留 watermark/observation/reflection/reflection_config/reflection_scope/judgment/digest，删 wiring/strategy/vocabulary/prefold/plan/driver（含 4 测试与 3 集成测试）；rl 保留 produce/record/capture/sink/writer/line/episode/replacement/fact/feedback，删 export/export_source/manifest/sft×5/import/reward/reward_ledger/verifier/feedback_resolver/feedback_endpoint/http（15 生产面＋2 单测＋8 学习集成测试＋S10＋4 契约范例），裁 causal_records/model_replacement/fact_test/S09 的残留消费；`grep` 零消费者门（NewMeditation/ConsumerPhase/WithConsumerPhase/MeditationTick/ConsumeSFT/TrainingExporter/ImportFile/RewardLedger/NewFeedbackEndpoint/NewHeuristicVerifier/CaptureExportSource）=0；`go build ./...`、`go vet ./...`、`go test ./evolution/meditation/... ./rl/... ./tests/integration/... -count=1`、`go test ./... -short` 全绿；golden deterministic 14/14 match；architecture policy/implementation 双门 ok；lint 除既有 doc-budget +3（D2 已记的规划四件套，非本单）外全门绿（tracked-hygiene 经统一暂存 67 处删除修复）；docs/api 重生成 check 绿；无 git commit。
- [x] W3 文档债清理＋CI 收口＋golden 同步：
  a) 死概念清零：docs/wiki/CONTRIBUTING/README 中 plugin.Plugin/NewPipeline/Phase 系旧名/tool\/remote/memory\/replica\/internal\/prototype 引用全清（grep 零）
  b) 演进叙述改当前状态："以前/已改为/不再使用/归核/下沉/迁移到" 7 处重写为现在时陈述（文档不呈现演进过程）
  c) CI 约束：runtime-contracts.yml PRODUCT_PACKAGES 同步已删包；openspec strict 步骤核验当前 change 集；workflows 注释去 prototype 引用
  d) golden/architecture/S4 范例同步
  —— 验证：死概念 grep 零＋演进词 grep 零＋golden match＋lint ok＋CI yaml 有效
  - 战报：a) 死概念逐词 grep=0（docs 非 archive/.dev/api + CONTRIBUTING + README，1197 篇：plugin.Plugin/NewPipeline/Phase 五名/RelationStore/run_*.go 五名/CompressPhase/Run.Explain/ArchiveOriginals/config.Load/tool/remote/memory/replica/internal/prototype 全零）；删 `docs/wiki/agent/prototype-skeleton.md`（唯一主题＝已删 internal/prototype），wiki README 计数 25→24；b) 演进词 grep=0；c) runtime-contracts.yml `PRODUCT_PACKAGES` bench→internal（与 golden 首发段一致，selftest `test_product_scope_matches_golden_architecture` 15 测全绿）＋注释去 prototype/去演进叙述＋openspec strict 步注释改真（步名与 `--all --strict` 命令由 selftest 钉住不动）；ci.yml race 步路径 `./agent ./event ./plugin` → `./runtime/agent ./api/event ./api/plugin`（旧路径 `go list` 报错、原步骤必红）；d) golden/runtime/architecture.json 去 `prototype` 排除项与 reason 子句，`--check-policy` 34 包/34 边 ok=True；S4 已删机制范例目录 tool_remote/evolution_refine 清空、tool_spec 保留（B 类）；EXTENSIONS/OPERATION 已删面引用清零（余 `knowledge: a remote endpoint is not wired` 为代码诊断串，非面名）。验证：golden deterministic 14/14 match（`run_runtime_scenarios.py --scope deterministic` verdict=match，drift=0）；4 个 workflow YAML ruby 解析全 OK；`check_doc_budget` 批末对账 files 1196→1198、lines 94597→94416（RESULT: ok）。遗留红均出白名单：`bash scripts/lint.sh` 卡 comment_policy（examples/wechat-bot/wechat.go 37 findings，examples/** 非 W3 白名单）；CI concept-budget 4 pre-existing error（runtime/agent、runtime/task、tool/subagent 未改文件，HEAD 同红）；openspec 主规格 3 篇（call-subagent-config/fact-vocabulary/package-conventions）非 strict。archive/.dev 未动，无 git commit。

## F 收尾
- [x] F1 契约零改动（golden match 14/14）＋F2 全量门禁（lint ok/38包/deterministic match）＋F3 DoD
- [x] F4 archive（用户明示归档）
