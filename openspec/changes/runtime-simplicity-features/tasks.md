# 任务：runtime-simplicity-features（deferred，本轮不实施）

> **deferred**：全部为 `- [ ]`，勾选即开工。本轮（hot-update-and-purity-closure 纯核心收口）不派发，不计入任何一级变更的完成度。
> 来源：`hot-update-and-purity-closure/changes/09-runtime-simplicity/tasks.md` 的 0.1/0.2/2.2/2.4/2.5/4.2/4.3/5.4/6.2/6.3 与 F1–F4，按用户 2026-11-08 裁定剥离。
> 验证行按 09 原文保留（正锚点名 + 负锚具名 + 消费者锚），开工时不得追认缩水；命令指向不存在目录的先建目录再跑，零匹配 exit 0 不算通过。

## 0. 基线与规格落定

- [ ] 0.1 建立缺陷基线见证：在 `tests/integration/upgrade/baseline_witness_test.go` 新增 `TestRuntimeBaselineWitness`，经正式 Boot 驱动一次带宿主默认参数的调用，断言请求证据含生效参数、且重启后可读回；当前基线必须红，并记录红的原因行 —— 验证：`go test ./tests/integration/upgrade -run '^TestRuntimeBaselineWitness$' -count=1 -json`，红项须命中"参数缺失"或"证据不可读"，编译失败/超时/零匹配不算红
- [ ] 0.2 落定受影响主规格清单：在本变更 `design.md` 追加一节，逐条列出 `event-segment-store`、`decision-capture`、`offline-training-export`、`runtime-inspection`、`turn-tracing`、`trajectory-recording` 中被修改的 requirement 名，确认与 `openspec/specs/` 现有标题逐字符一致 —— 验证：`openspec validate runtime-simplicity-features --strict --no-interactive` 通过（原 09 原文用未登记名 `runtime-simplicity-upgrade`，不可复跑，见 design D2）；负锚：任一 requirement 名与主规格不一致时归档前 `openspec validate --strict` 报错（本轮只登记命令，不实际归档）

## 2. 请求证据（capability: request-evidence）

- [ ] 2.2 在 `runtime/agent/loop.go` 构造请求后、`fixCapture` 前合并生效参数，使快照 `Parameters` 等于实际交给 provider 的口径 —— 验证：`go test ./tests/integration/upgrade -run '^TestEffectiveRequestEvidence$' -count=1 -json`；负锚 `snapshot_after_mutation_unchanged`
  - 现状：代码已落（`loop.decideWith` 内 `req = withEffectiveConfig(req, handle.Binding.Model)`，单位消费者锚 `go test ./runtime/agent -run '^TestLoopRequestEvidenceCarriesEffectiveConfig$'` 曾 EXIT=0），缺的是本叶冻结验收面 `tests/integration/upgrade/`（目录当前 MISSING），故不勾。
- [ ] 2.4 在 `assembly/request.go` 把 `callArgumentsOf` 从内存 map 改为按引用读回并校验摘要；`RequestSnapshots` 保持单一来源 —— 验证：`go test ./assembly -run '^TestRequestEvidenceReadBack$' -count=1 -json`；负锚 `digest_mismatch_rejected`、`missing_evidence_returns_empty`
- [ ] 2.5 口径声明与文档对齐（复核）：在读取面标明"运行时请求快照"而非 wire body；确认 `capture.go` 中不成立的 "actual parameters" 注释未回潮 —— 验证：`go test ./features/rl ./api/model -count=1` 与 `bash scripts/gates/gen_godoc.sh --check`
  - 现状：09 的同名已勾条目附 I6 凭据（两条命令曾 EXIT=0，`gen_godoc` 报 77 packages），本叶只做复核，不重做（见 design D3）。

## 4. 观测（capability: runtime-observability / runtime-inspection）

- [ ] 4.2 补齐关键指标：请求终态、阶段耗时、token 用量（缺失记 unknown）、订阅丢弃 —— 验证：`go test ./tests/integration/inspection -run '^TestRuntimeMetrics$' -count=1 -json`；负锚 `unknown_usage_not_zero`
  - 现状：`features/telemetry/metrics.go` 当前 MISSING，且三处生产者分别在 `assembly/run.go`、`runtime/agent`、`api/model`，须与指标组件同单或显式串行。
- [ ] 4.3 查询面补齐采集损失与待处理义务数量/年龄及下一推进者 —— 验证：`go test ./tests/integration/inspection -run '^TestInspectionLossAndObligations$' -count=1 -json`；负锚 `no_loss_reported_as_complete`、`storage_error_not_empty_state`

## 5. 离线数据（capability: offline-training-export）

- [ ] 5.4 真实消费者读取：`training/tests/test_consumer.py` 用真实 tokenizer/chat template/整理器断言历史掩码为零、目标非空、模板不支持即失败 —— 验证：`uv run --project training pytest training/tests/test_consumer.py -q`；负锚 `test_unsupported_template_rejected`
  - 现状：`training/` 目录当前 MISSING，`training/tests/test_consumer.py` 与 `uv` 配置均为新立面；开工前先裁定该 Python 消费者是否存在于本仓库。

## 6. 收敛与门禁

- [ ] 6.2 删除只固定旧代码形状的门（`skeleton_assert.go` 中无行为价值的断言、`package-layout` 一文件一职责规则），保留行为/安全/恢复门；逐项回查引用 —— 验证：`bash scripts/lint.sh` 与 `go build ./...` 均通过；残留门：`grep -rn "package-layout" scripts/ .github/` 无残留
- [ ] 6.3 文档分区落地：入口标注历史 wiki 可能过时，当前架构以正式入口为准；`docs/api` 重新生成 —— 验证：`bash scripts/gates/gen_godoc.sh --check` 与 `bash scripts/gates/check-openspec.sh`

## 7. 收尾

- [ ] F1 契约核对：本变更新增/修改的 requirement 与实现逐条对应；旧的兼容性断言不作为门槛；被删结构无残留引用 —— 验证：`openspec validate runtime-simplicity-features --strict --no-interactive` + `grep` 残留清单
- [ ] F2 全量门禁：`go build ./...`、`go vet ./...`、`go test ./... -short -count=1`、`go test -race`（受影响包，10 次）、`uv run --project training pytest`、`bash scripts/lint.sh`，逐条记录退出码
- [ ] F3 DoD 核验：0.1 红锚红→绿两次证据、四个 capability 的 scenarios 均映射到具名测试、拒收统计与真实消费者证据入 notes；不声称权重训练或效果提升
- [ ] F4 归档（需明示授权）：按受影响主规格正常 `openspec archive`，归档前后分别跑 `scripts/gates/check-openspec.sh`

## 依赖与派发提示

- 顺序：0.1 → 2.2 → 2.4 →（4.2/4.3 与 5.4 可并行）→ 6.2/6.3 → F。
- 同文件簇：`assembly/request.go`（2.4）与 `assembly/host_diagnostics.go`（4.3）串行；`features/telemetry` 与 `api/model` 为 4.2 的跨包合单。
- 删除类叶（6.2）先列引用清单再动手，每条删除附"删掉谁会坏"的反事实说明入 notes。
