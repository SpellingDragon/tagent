## 1. P0 无损校验接线(先行,独立可回滚)

- [x] 1.1 `scripts/hooks/pre-commit` 增收敛批触发判定:暂存区含 `_test.go` 变更且暂存区存在映射表(`*.map` 约定名)时,以 HEAD 为 base-ref 调 `check_test_merge.sh HEAD <涉及目录> --map <表>`;不满足条件静默放行。验证:伪造映射表跑 `bash scripts/hooks/pre-commit` 必执行校验;普通单文件测试暂存跑之必不触发
- [x] 1.2 提交 P0(仅 `scripts/hooks/` 一个文件,独立 revert 面);验证:`git show --stat HEAD` 只含 hook

## 2. P1 门禁上线(规则按语义分四个独立单元,各有单测)

- [x] 2.1 事实收集:`comment_policy` 主遍历扩展,收集每个 `_test.go` 的 (目录, `//go:build` 表达式集, `契约:` 锚点, `func Test` 计数)。验证:新增单测断言 testdata 样本的四元组事实正确(含无 build 行→空集、多 `契约:` 行取首个)
- [x] 2.2 分组与违规判定:包级聚合 pass,键=(目录, build-tag 集, 锚点);参与者=Test>0;镜像=同目录存在去 `_test` 后缀同名 `.go`,变体后缀仅 `_real` 宽容;组内参与者≥2 时每个无镜像文件出一条 finding。验证:单测覆盖六工况——全镜像组零 finding、tag 异组分键、Test=0 出局、真碎片计 finding、`_real` 宽容、前缀相似不宽容(`action_test` 对 `action_tool.go` 判不中)
- [x] 2.3 finding 文案:`responsibility-fragmentation` 消息并列三出口(并入镜像文件/改名对齐镜像/文档侧锚点收敛),附同键镜像落点与锚点。验证:单测断言文案含三出口关键词与落点路径
- [x] 2.4 权威计数与基线落定:全仓运行规则,记录权威文件数与逐文件清单(预期≈25;对勘 shell 预演 26 的差异须可解释,如 `_real` 宽容),经 `bash scripts/lint.sh --update-baseline` 写槽,与规则实现同批提交。验证:`bash scripts/lint.sh` 全绿且 `-v` 输出清单已存档到本 change 目录(`baseline-files.txt`,冻结为 P2 对账锚)
- [x] 2.5 负路径验证:临时新增一个同锚点无镜像测试文件 → `lint.sh` 必红(REGRESSION `responsibility-fragmentation +1`)→ 删除恢复绿。验证:两次运行的退出码与输出留痕(change 目录 `negative-path.log`)

## 3. P2 分域收敛(防跑偏协议见下;每任务一个独立提交)

**批规程(每个 3.x 收敛任务必须同构执行,顺序固定,任一步失败即停,不得跳步):**
①前置:该任务涉及文件的工作树干净、`baseline-files.txt` 中该文件仍在册;
②单一动作:只做该任务声明的出口动作(①并入/②改名/③锚点收敛),不顺手改其他;
③无损校验:`check_test_merge.sh <本任务起点提交> <目录> [--map 表]` 通过;
④测试:`go test <涉及包> -count=1` 与 `go test -race <涉及包>` 全绿,若动 wiki 则 `gen_godoc.sh --check` 绿;
⑤对账提交:`lint.sh --update-baseline` 降幅必须恰等于该任务销号数(N),不符即停并回查;独立提交,提交信息含 `[colocation]` 与销号文件名。

- [x] 3.1 冻结对账锚:以 2.4 存档清单为准,把 3.2-3.20 各任务与在册文件一一绑定;发现清单与下述枚举有出入(多出/缺失文件)时,先回填 design 判例表再开工。验证:本 tasks 内每个收敛任务都能在清单中找到全部对象文件
- [x] 3.2 evolution 试点(规程走通):`switch_combo_test.go` 工况→①并入 `judge_test.go` 子测试;N=1
- [x] 3.3 agent 试点(发起判例,小步验证 agent 包流程):`meditation_audit_test.go` →①并入 `telemetry_audit_test.go`;N=1
- [x] 3.4 tool/action 改名批:`action_test.go` →②改名 `action_tool_test.go`(映射表登记旧→新,同步脚本/文档引用);`tui_integration_test.go` 属 tag 异组、预期不在册,核对后记入对账说明;N=1
- [x] 3.5 rl 批之一:`auth_test.go` 工况→①并入 `http_api_test.go`;N=1
- [x] 3.6 rl 批之二:`http_api_closeout_test.go` 工况→①并入 `http_api_test.go`;N=1
- [ ] 3.7 memory 批之一:`compaction_safety_test.go` →①并入 `compaction_test.go`;N=1
- [ ] 3.8 memory 批之二:`error_tracking_engine_test.go` →①并入 `error_tracking_test.go`;N=1
- [ ] 3.9 memory 批之三:`mem_spill_notify_test.go` →①并入 `mem_spill_test.go`;N=1
- [ ] 3.10 memory 批之四(B 型,含文档侧):`segment_store_barrier_test.go`+`segment_store_recovery_test.go` →①并入 `segment_store_test.go` 子测试,同批③ `#tombstone` 锚上收族锚(wiki 小节调整或工况并入族锚);`gen_godoc.sh --check` 同批绿;N=2
- [ ] 3.11 memory 批之五:`embedder/contract_test.go` →①并入 `traced_test.go`;N=1
- [ ] 3.12 memory 批之六:`engine/engine_contract_test.go` →①并入 `engine_inmemory_test.go`;N=1
- [ ] 3.13 memory 批之七:`kv/partition_snapshot_test.go` →①并入 `local_file_kv_test.go`;N=1
- [ ] 3.14 agent 批之一:`session_refusal_test.go` →①并入 `exec_lease_test.go`;N=1
- [ ] 3.15 agent 批之二:`reliability/quarantine_barrier_test.go` →①并入 `inbox_test.go`;N=1
- [ ] 3.16 agent 批之三(②+①组合):`compress/session_projection_test.go` →②改名对齐 `projection.go`,随后 `fold_run_exemption_test.go` →①并入改名后文件;N=2
- [ ] 3.17 agent 批之四(裁决型):`agent/batch_retire_split_test.go`+`lineage_signal_test.go`——先核对 `agent/` 生产面裁定①落点或③锚点收敛,裁决记录写入本任务提交信息;N=2
- [ ] 3.18 agent 批之五(热区,B 型):前置 `git status` 确认 `agent/task/` 干净;`task/batch_retire_split_test.go`+`retire_lineage_test.go` →①并入 `task_manager_test.go` 子测试 + ③ `#finalize-lineage` 族锚上收;N=2
- [ ] 3.19 tests 批之一(C 型):`async_result_test.go`+`async_task_e2e_test.go` →①合并为单一文件,锚点取其一;N=2
- [ ] 3.20 tests 批之二(C 型,含文档侧):`compression_test.go`/`integration_test.go` →③重挂 e2e 级锚(落点语义写入提交信息;若评审判两文件确属同工况则辅①);N=2(实测 25 清单与 3.2-3.20 销号数逐一对账吻合)

## 4. P3 归零切硬与终验

- [ ] 4.1 归零核对:`baseline-files.txt` 全部在册文件已销号,规则全仓输出 0 finding;从 `baseline.json` 移除槽位。验证:`go run ./scripts/comment_policy -v . examples/wechat-bot` 无该规则输出
- [ ] 4.2 硬门生效验证:临时造一个同锚点无镜像测试文件 → `lint.sh` 直接红(不再有基线预算);删除恢复绿;两运行留痕
- [ ] 4.3 归档与 specs 核对:`openspec archive`,delta 并入主 specs 后复验「测试文件族与职责同位」机检条款与 10 个场景完整、`openspec validate --specs --strict` 全绿
- [ ] 4.4 终验:`bash scripts/lint.sh` + `go build ./...` + `go test ./... -short -count=1` + `examples/wechat-bot` 三连 + `./scripts/race_check.sh`(全包集)全绿;推送后 CI `test`/`race`/`validators`/`openspec` 四 job 绿
