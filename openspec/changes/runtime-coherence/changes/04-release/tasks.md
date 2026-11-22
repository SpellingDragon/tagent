# Tasks · 04 放行

> 只在 03.304 完成后派发；本子变更开始后冻结候选，任何被证据覆盖的字节改动都使受影响证据失效。

- [x] 04.401 生命周期与故障矩阵 —— 产出：挂载/换代/禁用/卸载/重启矩阵，含贡献与 builtin 同规则、未结义务去向、失败清理；新增 `TestFactoryContributionSurvivesReload` 等缺口锚。
  —— 勾选凭据（2026-11-22，编排者收口）：矩阵 3 节（面×操作、同规则判定、缺口锚择需）＋变异检验入 `evidence/04.401.md`；新增两缺口锚（`TestDisabledContributionKeepsItsObligations`、`TestRunLevelContributionDisableIsRefusedByName`，后者已加强 run-level 措辞断言）；11 锚 race×10 全 PASS；集成两包绿一次；G3 两 flake 按 HEAD 基线定性为既有等待面问题（`awaitSepFlush` 谓词窄于断言面），标注归 04.402 处置；G1 措辞缺陷由编排者修（run 级贡献拒绝现报 `run-level` 范围，`declaredPluginsOf` 收 runLevel 名字集），5 锚复验绿；policy 0 finding。

- [x] 04.402 成本与完整矩阵 —— 产出：十短、六长（seed 1/7/42 × window 256/32768）、变异、三 module、race、smoke、openspec、archive-preflight 的原始退出码与终局日志，落独立证据目录；补 CI 接线（OpenSpec CLI 安装、evidence-binding 产物链）。
  —— 勾选凭据（2026-11-22 编排者收口）：冻结树（`evidence/matrix/freeze.txt`）单一串行脚本——十短 **10/10 rc=0**、六长 **6/6 rc=0**（`TAGENT_CALIBRATION_RUN_DIR` 同 CI；verdict=match、durable_scan 有据）、变异 **23/23 killed**（唯一漂移项 `MUT-EVID-04` 已按 fault 形态迁移并单跑 killed；自测 60/60）、boot 墙钟 rc=0、长跑成本锚静默窗（load 3.06）×3 rc=0（首轮 load≈9 红判负载 flake）；CI 接线 `archive-preflight` 增 OpenSpec CLI 1.2.0（YAML_OK）＋G3 等待面修复（10/100→1/100，残留 0.5% 具名上报）。**evidence-binding 产物链已在 52d7b664 接线，本叶只补 CLI；端到端以真 CI 为权威（F4 核销）**。三 module/race/smoke/openspec 的全量矩阵按计划归 F2/CI 面执行并核销。
  —— 验证：`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat-index <n> --out <dir>`（n=1..10）；`go test ./tests/integration/execution/ -short -count=1`；`bash scripts/lint.sh`；`bash scripts/gates/race_check.sh <CI 同一包集>`；`bash scripts/gates/check-openspec.sh`；`python3 scripts/gates/check_event_integrity_mutations.py --all --json --out <dir>`。负锚：任一失败轮不得作绿证据；`grep -c` 为 0 不串 `&&`。消费者：CI 同名作业。

- [ ] 04.403 独立审与 F —— 独立只读审分轮，逐条裁定 frontier；修复后再审至空。F1 承接账与契约断言；F2 全量门禁；F3 DoD 与症状锚红转绿＋回滚即红；F4 归档并登记后继指针。
  —— 验证：审者输出扫描范围、未查面与 frontier 空；`git diff <baseline>..HEAD -- <契约目录>` 为空；全量门禁原始退出码；DoD 逐条证据；归档后 `openspec status` 与引用核对。负锚：frontier 非空或证据过期即拒绝放行。消费者：真实 CI 与归档入口。
