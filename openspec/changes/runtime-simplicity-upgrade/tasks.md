# 任务：运行时精简升级

分域仅作账目与授权单位；派发原子是单条叶任务。四项能力共 24 叶，另设收尾 F。

与多级目录结构的偏离及其理由：不建 `changes/<NN-domain>/` 嵌套子变更。本次只有四个相互耦合的能力，嵌套目录会让 specs 归档需要二次提升，收益低于成本；分域在本文件内以小节表达，验收仍按 capability 归口。

验证约定：每条叶任务的验证写成完整可复跑命令，`-run '^TestName$'` 与测试名逐字符一致。正锚为具名测试；负锚为同文件中点名的负向子测试；消费者锚为经正式入口（`assembly.Boot`/`assembly.Assemble`、CLI、公开导出命令）的调用路径。基线测试名以待改代码的实际测试文件为准，落盘时逐一对齐，不得凭记忆填写。

## 0. 基线与红锚

- [ ] 0.1 建立缺陷基线见证：在 `tests/integration/upgrade/baseline_witness_test.go` 新增 `TestRuntimeBaselineWitness`，经正式 Boot 驱动一次带宿主默认参数的调用，断言请求证据含生效参数、且重启后可读回；当前基线必须红，并记录红的原因行 —— 验证：`go test ./tests/integration/upgrade -run '^TestRuntimeBaselineWitness$' -count=1 -json`，红项须命中"参数缺失"或"证据不可读"，编译失败/超时/零匹配不算红
- [ ] 0.2 落定受影响主规格清单：在 `design.md` 追加一节，逐条列出 `event-segment-store`、`decision-capture`、`offline-training-export`、`runtime-inspection`、`turn-tracing`、`trajectory-recording` 中被修改的 requirement 名，确认与 `openspec/specs/` 现有标题逐字符一致 —— 验证：`openspec validate runtime-simplicity-upgrade --strict --no-interactive` 通过；负锚：任一 requirement 名与主规格不一致时 `openspec archive --dry-run` 报错（本轮只登记命令，不实际归档）

## 1. 追加事实日志（capability: append-fact-log）

- [ ] 1.1 在 `memory/ledger/journal.go` 新增帧编解码：长度 + CRC32C + 载荷，单 writer `O_APPEND` 追加；写入成功才更新内存索引 —— 验证：`go test ./memory/ledger -run '^TestJournalFraming$' -count=1 -json`；负锚子测试 `rejects_corrupt_middle_frame`、`truncates_torn_tail`、`failed_write_not_visible`
- [ ] 1.2 在 `memory/ledger/journal_kv.go` 实现 `kvPort`，替换 localfile 的整分片快照写入；`Sync` 成为唯一持久屏障，`kvCommit` 不再内部 flush —— 验证：`go test ./memory/ledger -run '^TestJournalKVCommit$' -count=1 -json`；负锚 `sync_failure_reports_error`、`no_snapshot_rewrite`
- [ ] 1.3 在 `memory/ledger/config.go` 增加格式版本与目录判定：旧 `shard_*.json` 目录具名拒绝，不迁移不清理 —— 验证：`go test ./memory/ledger -run '^TestJournalRejectsLegacyLayout$' -count=1 -json`；负锚 `legacy_dir_untouched_after_refusal`
- [ ] 1.4 在 `runtime/state/durable.go` 去掉 Put 之后的重复 `Sync`，改为单次屏障，并使 `CommitUncertain` 在真实后端可达 —— 验证：`go test ./runtime/state -run '^TestDurableCommitBarrier$' -count=1 -json`；负锚 `barrier_failure_returns_uncertain`；消费者：`assembly.Assemble` 提交路径
- [ ] 1.5 删除 `localfile.go` 的 `writeShard`/`readShardSnapshot`/`kvStaged`/`kvUnstage` 与 `segment_store.go` 的 `idx:`/`meta:` 冗余写入，回查全部引用 —— 验证：`go build ./...` 与 `go test ./memory/... ./runtime/state/... -count=1`；`grep -rn "kvUnstage\|shardSnapshotTempName" --include=*.go .` 须零命中
- [ ] 1.6 重启与并发回归：`runtime/state` 与 `memory/ledger` 相关用例各 10 次独立复跑并开竞态 —— 验证：`go test -race ./memory/ledger ./runtime/state -count=10 -run '^(TestJournal|TestDurableCommitBarrier)'`

## 2. 请求证据（capability: request-evidence）

- [ ] 2.1 在 `assembly/host_model.go` 为 `hostParameterizedModel` 增加 `EffectiveGenerationConfig()`，实现"声明才填、请求已声明则不覆盖" —— 验证：`go test ./assembly -run '^TestEffectiveGenerationConfig$' -count=1 -json`；负锚 `undeclared_stays_absent`、`request_value_not_overridden`
- [ ] 2.2 在 `runtime/agent/loop.go` 构造请求后、`fixCapture` 前合并生效参数，使快照 `Parameters` 等于实际交给 provider 的口径 —— 验证：`go test ./tests/integration/upgrade -run '^TestEffectiveRequestEvidence$' -count=1 -json`；负锚 `snapshot_after_mutation_unchanged`
- [ ] 2.3 在 `features/rl/line.go` 新增封闭记录 kind（请求证据），沿 `CaptureSink` 有界异步落盘；拒绝与写失败计入损失账目 —— 验证：`go test ./features/rl -run '^TestRecordKinds$' -count=1 -json`；负锚 `unknown_kind_rejected`、`full_queue_counted`
- [ ] 2.4 在 `assembly/request.go` 把 `callArgumentsOf` 从内存 map 改为按引用读回并校验摘要；`RequestSnapshots` 保持单一来源 —— 验证：`go test ./assembly -run '^TestRequestEvidenceReadBack$' -count=1 -json`；负锚 `digest_mismatch_rejected`、`missing_evidence_returns_empty`
- [ ] 2.5 口径声明与文档对齐：在读取面标明"运行时请求快照"而非 wire body；修正 `capture.go` 中不成立的 "actual parameters" 注释 —— 验证：`go test ./features/rl ./api/model -count=1` 与 `bash scripts/gates/gen_godoc.sh --check`

## 3. 流式增量（capability: streaming-increments）

- [ ] 3.1 主路径开启 `Stream: true`，确认既有归约仍产出同一终态 —— 验证：`go test ./tests/integration/upgrade -run '^TestStreamingTerminalParity$' -count=1 -json`；负锚 `partial_only_never_completed`
- [ ] 3.2 在 `runtime/agent/stream.go` 实现 tee 与有界非阻塞 fan-out，慢订阅者丢弃并计数，不阻塞模型 —— 验证：`go test ./runtime/agent -run '^TestIncrementFanout$' -count=1 -json`；负锚 `slow_subscriber_does_not_block`、`cancel_subscription_keeps_call`
- [ ] 3.3 流式超时改为空闲判定，消除 `http.Client.Timeout` 对长流的整体截断；显式处理 openai-go 默认重试 —— 验证：`go test ./tests/integration/upgrade -run '^TestLongStreamCompletes$' -count=1 -json`；负锚 `idle_timeout_fires`、`retry_disabled_or_recorded`
- [ ] 3.4 CLI 实时输出：`examples/cli-host/main.go` 经公开面订阅增量并在完成前写出首段 —— 验证：`go test ./examples/cli-host -run '^TestCLIStreamsIncrementally$' -count=1 -json`；负锚 `final_body_printed_once`；消费者：真实 CLI 子进程

## 4. 观测（capability: runtime-observability）

- [ ] 4.1 观测组件改为装配注入（logger/tracer/meter），移除进程级默认赋值与 `tagent.event.key` 高基数标签 —— 验证：`go test ./features/telemetry -run '^TestObserverInjection$' -count=1 -json`；负锚 `no_global_mutation`、`no_high_cardinality_label`
- [ ] 4.2 补齐关键指标：请求终态、阶段耗时、token 用量（缺失记 unknown）、订阅丢弃 —— 验证：`go test ./tests/integration/inspection -run '^TestRuntimeMetrics$' -count=1 -json`；负锚 `unknown_usage_not_zero`
- [ ] 4.3 查询面补齐采集损失与待处理义务数量/年龄及下一推进者 —— 验证：`go test ./tests/integration/inspection -run '^TestInspectionLossAndObligations$' -count=1 -json`；负锚 `no_loss_reported_as_complete`、`storage_error_not_empty_state`

## 5. 离线数据（capability: offline-training-export）

- [ ] 5.1 `features/rl/export_chain.go` 改为读取落盘请求证据；缺证据/摘要不符/终态未知具名拒收并出清单 —— 验证：`go test ./tests/integration/learning -run '^TestExportReadsDurableEvidence$' -count=1 -json`；负锚 `missing_evidence_rejected`、`redacted_not_exact`
- [ ] 5.2 SFT 导出补齐 prompt/completion/tools 与仅有工具调用的目标 —— 验证：`go test ./tests/integration/learning -run '^TestSFTDatasetShape$' -count=1 -json`；负锚 `tool_only_target_preserved`、`unsupported_media_rejected`
- [ ] 5.3 DPO 导出要求同前缀 + 同工具集合 + 偏好来源与版本 —— 验证：`go test ./tests/integration/learning -run '^TestDPODatasetShape$' -count=1 -json`；负锚 `different_prefix_not_paired`、`missing_preference_rejected`
- [ ] 5.4 真实消费者读取：`training/tests/test_consumer.py` 用真实 tokenizer/chat template/整理器断言历史掩码为零、目标非空、模板不支持即失败 —— 验证：`uv run --project training pytest training/tests/test_consumer.py -q`；负锚 `test_unsupported_template_rejected`

## 6. 收敛与门禁

- [ ] 6.1 `features/rl` 内与事实链并行的 `TrajectoryRecorder` 默认关闭，配置项标注为调试用途；回查 `assembly/run.go`、`host.go` 引用 —— 验证：`go test ./features/rl ./assembly -run '^(TestRecorderDefaultOff|TestBoot)' -count=1`；`grep -rn "trajectory_dump" --include=*.go .` 结果逐条核对
- [ ] 6.2 删除只固定旧代码形状的门（`skeleton_assert.go` 中无行为价值的断言、`package-layout` 一文件一职责规则），保留行为/安全/恢复门；逐项回查引用 —— 验证：`bash scripts/lint.sh` 与 `go build ./...` 均通过；`grep -rn "package-layout" scripts/ .github/` 无残留
- [ ] 6.3 文档分区落地：入口标注历史 wiki 可能过时，当前架构以正式入口为准；`docs/api` 重新生成 —— 验证：`bash scripts/gates/gen_godoc.sh --check` 与 `bash scripts/gates/check-openspec.sh`

## 7. 收尾

- [ ] F1 契约核对：本变更新增/修改的 requirement 与实现逐条对应；旧的兼容性断言不作为门槛；被删结构无残留引用 —— 验证：`openspec validate runtime-simplicity-upgrade --strict --no-interactive` + `grep` 残留清单
- [ ] F2 全量门禁：`go build ./...`、`go vet ./...`、`go test ./... -short -count=1`、`go test -race`（受影响包，10 次）、`uv run --project training pytest`、`bash scripts/lint.sh`，逐条记录退出码
- [ ] F3 DoD 核验：0.1 红锚红→绿两次证据、六个 capability 的 scenarios 均映射到具名测试、拒收统计与真实消费者证据入 notes；不声称权重训练或效果提升
- [ ] F4 归档（需明示授权）：按受影响的六个主规格正常 `openspec archive`，归档前后分别跑 `scripts/gates/check-openspec.sh`

## 依赖与派发提示

- 顺序：0.1 → 1.1 → 1.2 → 1.4 →（2.1/2.2 与 1.3/1.5 可并行）→ 2.3/2.4 → 3.x → 4.x → 5.x → 6.x → F。
- 3.x 必须晚于 2.x：流式会放大未修复的快照缺口。
- 5.x 必须晚于 2.3/2.4：导出来源是落盘证据。
- 同文件簇（`features/rl`、`assembly`）不得并行派发；`memory/ledger` 与 `runtime/state` 属同一构建闭包，串行。
- 6.1/6.2 删除类任务需先列引用清单再动手，且每条删除附"删掉谁会坏"的反事实说明入 notes。
