# Tasks · 03 消费者收敛

> 03.301/03.302/03.303 在 02 完成后可并行；03.304 编排者单写，依赖前三叶验收。

- [x] 03.301 投影与压缩归位 —— 写面：`assembly/checkpoint.go`（`projectionStateSlice` 覆盖与 `checkpointCut`）、`assembly/compress_checkpoint.go`、`assembly/run.go`（`replayChainPastCut`/`seedAnchorSurvivors` 的业务特判）＋测试。投影切片只表投影覆盖；压缩判决/应答归压缩账户；协调者不按压缩事实修正投影；压缩正文副本按 03 design D2 判定后处理。
  —— 勾选凭据（2026-11-22 双单合交，编排者亲验）：压缩覆盖自述＝catalog→`compress_mount` 工厂→`restoreCoverageState`（run.go:1122）；协调者压缩特判零残留（`compressCut` grep 全 0）；Content 判定＝唯一可读来源保留（compress_consumer.go:391-403，产品身份含 request、条目不携字段）＋定位锚 `TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel`；三新锚亲验 PASS、全包 374s rc=0、retry 五锚绿。证据 `evidence/03.301.md` §八。
  —— 验证：`go test ./assembly -run '^(TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint|TestCompressionCheckpointTracksPendingWork|TestCompressionCheckpointPreservesOutOfOrderPendingRequest|TestCompressionCheckpointDoesNotRepeatSettledRequest|TestCheckpointCarriesOpenObligationAcrossFold|TestAnchorRebuildFoldsTheRangeNoCheckpointVouches)$' -count=1`；`go test ./assembly -run '^TestProjectionCoverageDoesNotStandForOtherConsumers$' -count=1`。负锚：`TestMissingCompressionSliceDoesNotReplayExecutedJudgments`。消费者：正式 Boot 的压缩消费与恢复路径。

- [x] 03.302 治理恢复归位 —— 写面：`assembly/governance_checkpoint.go`（`seatGovernance`/`Bind` 触发）、`features/evolution/governance/**`（`StateSeat`、`governanceTail`、审批绑定）＋测试。切片表有效态与尾序号；历史裁决按身份读链；`Bind` 由拥有者关系触发，读失败不铸新请求。
  —— 勾选凭据（2026-11-22 双面合交）：features 面（契约注释一处＋缺片正锚＋历史读失败负锚隔离树有牙，编排者亲验 3 锚）；装配面 W4b/W4b-c（`Bind` 触发＝host→seat 装配关系核对通过，治理七锚 7 PASS/0 SKIP 亲验含 `TestGovernanceApprovalBootReadsStateSlice`）。
  —— 验证：`go test ./assembly -run '^(TestGovernanceSliceWiredInProduction|TestGovernanceApprovalBootReadsStateSlice|TestGovernanceBootRefusesUnreadableStateCut|TestGovernanceBootReadsSettledAnswerByIdentity|TestGovernanceBootWorkTracksOpenItemsAndTail)$' -count=1`；`go test ./features/evolution/governance -count=1`。负锚：切片缺失/损坏具名拒绝；历史查询读失败不得答空。消费者：真实 `Boot` 的治理座位与审批闸。

- [x] 03.303 材料声明驱动一致分派 —— 写面：`api/plugin/material.go`、`api/plugin/phases.go`、`assembly/tools.go`（`policyRuling`）、`assembly/plugin_catalog.go`（Governor 接线）＋测试。声明归一（含兼容读法）后驱动同一分派；六端口与安全时机保留；`Governor` 的实际裁决与管线声明一致。
  —— 勾选凭据（2026-11-22 双面合交）：api 面（六趟核对＝已声明驱动、三锚新建绿、M1–M4 变异咬合，编排者亲验 rc=0）；Governor 乙案（W4b：`admissionRuling`/`policyRuling` 消费管线声明 Governor、`WithPolicy`＝无声明缺省，新锚 `TestGovernorDeclaredInThePipelineRulesTheCall` 含拒绝对照腿，race×3 绿，编排者亲验 rc=0；既有治理/reload 锚零变化）。
  —— 验证：`go test ./api/plugin -run '^(TestDeclaredMaterialDrivesItsPass|TestCompatMaterialKeepsPortBehavior|TestMaterialContradictingItsPassIsRefused)$' -count=1`；`go test ./assembly -run '^(TestGovernorDeclaredInThePipelineRulesTheCall|TestPluginDeclarationRefusesAnUnregisteredName)$' -count=1`。负锚：声明与端口矛盾的实例具名拒绝；未声明材料的面走兼容读法且失败行为不变。消费者：真实工具受理与裁决路径。

- [x] 03.304 删除被替代的双解释与专用分支 —— 写面：跨文件；由编排者按前三叶证据单写。逐项列：被删结构、替代者、覆盖该行为的 spec 场景与测试、反事实。范围限本变更已证明被替代者，不做顺手重构。
  —— 勾选凭据（2026-11-22 编排者单写）：`evidence/03.304.md` 台账八项（点读×4、Boot 丢弃、任务双解释、协调者特判、压缩特判、审批先改内存、Governor 死写、索引零值）各含替代者/反事实/覆盖锚；显式不删五项；`fold_test.go` 继承债按先例登豁免。收口验证：`package-layout` rc=0（810 文件）、`go build ./...` rc=0、`check_concept_budget` RESULT ok（见下「窄口修订」）、gofmt 0。
  —— 验证：`go build ./...`；`go vet ./...`；`go test ./assembly ./runtime/task ./runtime/task/approval ./features/evolution/governance -count=1`；`python3 scripts/gates/check_concept_budget.py`；删除项对应的原 spec 场景仍由新锚覆盖（逐条对应表入档）。负锚：每个删除项须有至少一个仍绿的替代锚；缺替代者即回退该删除。消费者：全量入口回归归 04。
