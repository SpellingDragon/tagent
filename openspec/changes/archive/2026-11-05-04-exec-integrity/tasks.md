# exec 回执完整性：孙任务

- [x] 1.1 锚⑥转绿（**真实 tmux** 锚，非 fake；W0 已红）：`TestSymptomAnchorExecDeadPaneIntegrity` —— 验证：`go test ./tool/exec -v -run 'TestSymptomAnchorExecDeadPaneIntegrity' -count=1` PASS（本机与 CI 均真实执行；skip=未交付硬门）。
- [ ] 1.2 修复点=held/复用路径：`RunTool.Call` 的 held 短路（tool.go:228 跳过 Probe）与 `Manager.Acquire` 的 resources 命中（exec.go:224-233 不 Probe 直接复用）——复用前 MUST Probe pane 状态；pane_dead ⇒ Release/清理旧会话（避免 errSessionExists）后以新命令 Start；`TestDeadPaneNotReusedOnHeldPath`（第二次调用不命中旧 held 缓冲、不回放旧 scrollback）+ `TestReusePathProbesPaneState` —— 验证：`go test ./tool/exec -v -run 'TestDeadPaneNotReusedOnHeldPath|TestReusePathProbesPaneState' -count=1` 用例数=2 PASS。
- [ ] 1.2a 重建分支真跑（防“探活后直接报错”假绿）：`TestRebuiltSessionReallyRuns`——死 pane 后重建会话真实执行新命令、touch 落盘、退出码真实（不是探活后报错）+ `TestRebuildRetryMetadataRecorded`（重建/重试事实进回执元数据，可观测）—— 验证：`go test ./tool/exec -v -run 'TestRebuiltSessionReallyRuns|TestRebuildRetryMetadataRecorded' -count=1` 用例数=2 PASS。
- [x] 1.3 世代令牌：会话世代绑定输出与退出码；`TestStaleScrollbackNotTrusted` + `TestTokenStrippedFromReceipt` —— 验证：`go test ./tool/exec -v -run 'TestStaleScrollbackNotTrusted|TestTokenStrippedFromReceipt' -count=1` 用例数=2 PASS。
- [x] 1.3a fake 快速回归（F20 分层）：fake-tmux 单测覆盖世代剥离/拒信逻辑（与真实锚不同面，秒级回归用）——`TestGenerationTokenFakeTmuxFast` —— 验证：`go test ./tool/exec -v -run 'TestGenerationTokenFakeTmuxFast' -count=1` PASS。
- [ ] 1.4 集成锚（**本地已验 PASS 行=1/SKIP=0；CI run URL 待合并后收集——lead 记账**） CI 非空转（PASS 行判据）：CI workflow 日志须含 `--- PASS: TestSymptomAnchorExecDeadPaneIntegrity`（skip 也产出 RUN 行，不作凭据）；touch 落盘读回一致 —— 验证：本地命令 PASS + CI run URL 且日志含 PASS 行入台账。
- [x] 1.5 消费者锚：`TestRunReceiptCarriesExecutionProof` —— 验证：`go test ./tool/exec -v -run 'TestRunReceiptCarriesExecutionProof' -count=1` PASS。
- [ ] 1.5a（**ci.yml:44-47 已核验无 diff；CI 日志 PASS 行待合并后入台账——lead 记账**） CI workflow tmux 核验（F18）：**核验 CI workflow 已安装 tmux（`.github/workflows/ci.yml:44-47` 既有 Install tmux 步骤；本域无 diff 则以核验凭据收口）**；CI 日志必须含 `--- PASS: TestSymptomAnchorExecDeadPaneIntegrity`（**无 tmux 的 runner=配置错误判红，不是 skip**）—— 验证：CI run URL + 日志 PASS 行入台账 + `sed -n '44,47p' .github/workflows/ci.yml` 核验输出（无 diff 时即凭据）。
- [x] 1.6 域门禁（**在仓库根执行，cd 正确；勿 cd 到子目录**）：`set -o pipefail; go test ./tool/exec -count=1 -v -run 'TestSymptomAnchorExecDeadPaneIntegrity' 2>&1 | tee /tmp/execcore.log; ec=${PIPESTATUS[0]}; test "$ec" = "0" && test "$(grep -c '^--- PASS: TestSymptomAnchorExecDeadPaneIntegrity' /tmp/execcore.log)" = "1" && test "$(grep -c '^--- SKIP: ' /tmp/execcore.log)" = "0"`（**go test 退出码取 `PIPESTATUS[0]`；PASS 行恰 1、任意 `--- SKIP: ` 行=0**）—— 验证：上述命令 exit 0。**concept budget 门自清**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空（新增字段未导出+方法访问；不新增导出类型）。
- [x] 1.6a 纯化（r22）：开关已删，唯一路径负锚替代 —— `TestGenerationCheckIsUnconditional`（任何 `TAGENT_EXPERIMENT_*` 值均无效：缺世代标记仍按显式陈旧世代拒收，无旁路） —— 验证：`go test ./tool/exec -v -run 'TestGenerationCheckIsUnconditional' -count=1` PASS。

## scenario ↔ 测试映射（F27）

| spec scenario | 测试 |
|---|---|
| 死 pane 复用旧缓冲 | 锚⑥ + TestDeadPaneNotReusedOnHeldPath |
| held 资源探活 | TestReusePathProbesPaneState |
| 重建后真实执行 | TestRebuiltSessionReallyRuns |
| 标记不污染输出 | TestTokenStrippedFromReceipt |
| 陈旧缓冲不采信 | TestStaleScrollbackNotTrusted |
| 唯一路径（无开关） | TestGenerationCheckIsUnconditional |

> **验证约定（r4 F14）**：本文件所有 `go test -run` 验证命令默认附加 `-v`；“用例数=N”=输出中 `--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。
