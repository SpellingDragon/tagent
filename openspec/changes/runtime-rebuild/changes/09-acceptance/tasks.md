# 09-acceptance — 任务清单（三级孙任务）

> 本文件是二级子变更 `09-acceptance` 的任务清单（孙任务级）。编号 `09.1–09.6` 全局唯一，**全部未勾选**（`- [ ]`）。不得导入旧 change 编号或旧完成率。
> 资产口径：检查脚本（`scripts/check_runtime_*.py`）、自测（`scripts/test_check_runtime_*.py`）、golden（`golden/runtime/{architecture.json,scenarios.json}`）、CI（`.github/workflows/runtime-contracts.yml`）、manifest（`openspec/changes/runtime-rebuild/task-graph.json`，89 task＝65 实施＋24 骨架、均带 dep/schema，本轮 00.P0.1/00.P0.2 已完成）**名称固定，由主代理 tooling 单创建**，本域只引用。
> 判定：点名测试不存在/零匹配/子项 Skip 不算通过；命令记录**原始退出码**，不用 `grep/head/tail` 退出码代替原命令。门控：无门=合法 Skip、有门缺凭据=Fatal、必验项 Skip 不 complete。
> **两阶段**：既有 `09.1–09.6` 属阶段二，统一以骨架评审门 `00.P0.3` 为额外前置（以 `task-graph.json` 为准），不逐行改写各自「前置」文案。

## 前置与边界

- 实现前置：`00.1`、`00.6`；`01`–`08` 相关确定性实施项；`03.3`/`03.5`；宿主确定性验收；部署环境（09.4）；授权资源（09.5）。
- 写白名单（未来实施）：主代理 tooling 单的检查资产与 golden/CI/manifest；`tests/integration/acceptance/**`。**本轮不写产品/测试/脚本/CI/golden**。
- 禁止：伪 restore 副作用；合成 SFT consumer/mock 冒充真实；靠 `tagent: *` 通配通过结构覆盖；提高空注释 baseline；改产品/主 spec/旧 change；提交归档。

## P0 骨架任务（阶段一：数据/接口定义，零逻辑）

> 阶段一骨架：只落数据/接口定义与 CI 合规注释文档，**零业务逻辑**；stub 仅 `return <零值>, errors.New("phase 2: not implemented")`、无返回值方法仅 `// TODO(phase2)` 空体、**禁止 panic**；每包 `skeleton_assert.go` 放编译断言；导出符号英文 Godoc＋包注释；文件 ≤500 行。设计闸 G1=签名与 00.3 共享合同一致，G3=每包白名单（`tests/`）。

- [x] 09.P0.1 建立场景 runner 类型骨架：`tests/` 下 observations/断言框架接口（仅类型，无 runner 逻辑）—— 前置：00.3、00.P0.1 —— 候选拟写文件（非本轮授权）：`tests/integration/acceptance/runner_types.go`、`tests/integration/acceptance/skeleton_assert.go` —— 设计闸：G1=观测/断言接口与 00.3 合同一致；G3=每包白名单 —— 验证：`go build ./...`、`go vet ./...`、`gofmt -l tests/`（空输出）；零逻辑由骨架评审门核

## 任务

- [x] 09.1 建立正式装配场景 runner 与 golden 对照：所有正式入口输出真实观测，断言**值/消息/身份关系**，不用人工 PASS 汇总 —— 前置：`01`–`08` 相关确定性实施项完成；真实资源欠账另列 `09.5` —— 复用目标：golden `golden/runtime/{architecture.json,scenarios.json}`、`scripts/check_runtime_evidence.py`（严格行为） —— 复用注意点：随机 ID/时间可规范化但保留一对一关系/顺序/因果；观测器 MUST NOT 写成期望值复制器；缺 case/错值/未知断言/子测试 Skip/包级 Fail 都失败；**场景 runner 具体名称未定（待实现）**——MUST NOT 以占位/未命名 runner 冒充已实现并报绿，未命名前相关子项标**待实现**；**验收集分层**：默认 `--scope complete`（全 10 场景严格，缺 S10 即失败），确定性 runner 用 `--scope deterministic` 只验合同登记的确定性集（S01–S09）并输出 `scope` 与 `remaining_required`（S10 列为**显式欠账**，不报全通过）；**确定性集不要求真实 SFT 批次，也不得用合成/替身 SFT 凑 S10**——S10（含真实 `06.6`）由 `09.5` 在授权资源下承担 —— 验证：`python3 scripts/check_runtime_evidence.py --evidence <RUN-file.json> --test-log <PATH> [--scope deterministic]`（严格行为，`--evidence`/`--test-log` **成对**提供，只给一半即失败；raw 输出为值/消息/身份关系差异报告）。

- [x] 09.2 离线迁移与回退：只读旧数据导入新路径、缺失外置文件/不可恢复任务具名、中断重跑与回退 —— 前置：`00.1`、`03.3`/`03.5` —— 复用目标：`state`/`memory` 持久面（读旧写新） —— 复用注意点：源摘要（字节）MUST 不变、不让旧程序读新格式；回退只读旧数据走新路径、**不伪 restore 副作用**—— 验证：`go test ./tests/integration/acceptance -run '^TestOfflineMigration$' -count=10 -json`。【M 回归重验：上游 03.3 重开，收口后重跑回归锚再勾选——见 midterm-review-response.md §四】

- [x] 09.3 完整门禁：两 module 完整 build/vet/test/lint、第一方全包 race 与检查器负控 —— 前置：`00.6`、`09.1` 及宿主确定性验收 —— 复用目标：`scripts/lint.sh`、`scripts/race_check.sh`、`scripts/check-openspec.sh`、`scripts/test_race_check.sh`、检查器自测 —— 复用注意点：零失败不能由过滤输出得出；棘轮只降不升、**不提高空注释 baseline**；检查器负控必须有意失败（红）才算有效；**`scripts/test_race_check.sh` 只是 race 检查器自测、不等价于产品 race**——产品 race 入口须按下方两 module 命令分别执行并**保存原始退出码**（`-short`、不隐式打开真实资源启用门）；**第一方新包经 `./...` 递归覆盖、MUST NOT 遗漏**；根 `./...` 若连带编译 `prototype/`（历史骨架）属**额外只读编译，本轮不执行、作为未来全量 command 允许** —— 验证（逐条独立记录原始退出码）：
  ```sh
  go mod verify
  go build ./...
  go vet ./...
  go test ./... -short -count=1 -json
  GOMAXPROCS=1 go test . -short -count=1 -json
  go -C examples/wechat-bot build ./...
  go -C examples/wechat-bot vet ./...
  go -C examples/wechat-bot test ./... -short -count=1 -json
  bash scripts/lint.sh
  bash scripts/gen_godoc.sh --check
  bash scripts/test_race_check.sh        # race 检查器自测（保持，不等价于产品 race）
  go test -race ./... -short -count=1 -json                    # 产品 race：根 module 全第一方子包递归
  go -C examples/wechat-bot test -race ./... -short -count=1 -json   # 产品 race：微信 module 全第一方子包递归
  python3 scripts/test_verify_restart_prefix.py
  python3 -m unittest discover -s scripts -p 'test_check_runtime_*.py'
  bash scripts/check-openspec.sh
  openspec validate --all --strict --no-interactive
  ```

- [x] 09.4 常驻 soak、**进程崩溃恢复**、热更回滚、权限与资源泄漏（部署环境实跑）—— 前置：`09.1`/`09.2` 及部署环境到位 —— 复用目标：宿主生产入口、持久现场；**旧 CI soak job**（`.github/workflows/ci.yml` 的 `soak`：`go test ./tests/ -tags soak -run TestSoak_Continuity -count=1 -v -timeout 45m -args -rounds=30 -events-per-round=30`，ubuntu-latest / go 1.24）**仅作待核参数参考，不作新验收锚** —— 复用注意点：正式新路径验收用例为**新计划用例名 `TestSoakContinuity`**（与旧 CI 的 `TestSoak_Continuity` 不同，**不得以旧名替代新验收**；如需 soak 构建标签隔离则统一加 `-tags soak`，`task-graph.json`/`design.md`/`tasks.md` 三处一致）；旧 CI 的 30 轮 × 30 事件与 45m **仅作待核参数参考**；**承诺进程崩溃恢复**（独立进程中止后可从持久现场恢复），**硬件断电不宣称**——不把掉电作为验收前提、不声称掉电实证；**不新增固定 72h 窗口**、不新增 on-policy 训练硬件诉求；未到位标 `BLOCKED`，不以 mock/优雅 Close/同进程 New 顶替 —— 验证（部署环境实跑，macOS/Linux 实际进程崩溃恢复/热更回滚/权限与资源泄漏必验）：`go test -race ./tests/integration/host -run '^TestSoakContinuity$' -count=1 -json`；**缺资源保持未完成，不强加硬件**。

- [x] 09.5 各资源子场景的必需验收（按启用门逐项；互不等待无关服务）—— 前置：相应实现 + 资源 + 授权（逐项具名）—— 复用目标：各域既有资源门用例 —— 复用注意点：无门=合法 Skip、有门缺凭据=Fatal、**正式必验项不得以 Skip 结项**；只报告凭据存在性不打印秘密值；**SFT 仅数据消费（真实 tokenizer/chat template/批次/labels/动作 mask），不加 on-policy 训练硬件需求**；**不把合成 SFT consumer/mock 当真实**—— 验证（逐子场景单次运行 `-count=1`）：
  - 存储档（memory/localfile/file-rustviking）：`go test ./tests/integration/history -run '^TestStorageCapabilities$' -count=1 -json`（rustviking 另列资源门）。
  - 真实模型（评估窗）：`go test ./tests/integration/evolution -run '^TestStructuralEvaluation$' -count=1 -json`。
  - MCP：`go test ./tests/integration/capabilities -run '^TestMCPProtocolLifecycle$' -count=1 -json`。
  - A2A：`go test ./tests/integration/capabilities -run '^TestAddressedDelivery$' -count=1 -json`。
  - embedding/检索：`go test ./tests/integration/capabilities -run '^TestRecallEvaluation$' -count=1 -json`。
  - collector/OTel（**真实 collector 收到非空 span/metric**，归属 **`05.3` 实现 + `09.5` 验收**）：`go test ./tests/integration/inspection -run '^TestRealCollectorDelivery$' -count=1 -json`（启用门=导出端存在；授权；预算；非空接收证据；缺门合法 Skip、有门缺凭据 Fatal、必验项 Skip 不结项）。**不以 fixture 封账冒充 collector 收到**：`TestCaptureAdmissionAndSeal` 是 `06.3` 的 RL 队列封账，与本子项无关。**关闭 OTel 仍 Query 可用**属确定性 `05.3` 用例：`go test -race ./tests/integration/inspection -run '^TestObservationDoesNotOwnState$' -count=10 -json`。
  - SFT 消费：`go test ./tests/integration/learning -run '^TestSFTConsumer$' -count=1 -json`。
  - 微信（真实往返）：`go -C examples/wechat-bot test ./... -run '^TestRealWechat$' -count=1 -json`（**明确发送授权**）。

- [x] 09.6 闭合：主规格 delta、文档/API 与消费者逐项闭合，提交未决事项与最终证据清单 —— 前置：`09.1`–`09.5`；**存在未裁项则如实保持未完成** —— 复用目标：`scripts/check_runtime_architecture.py`、`check_runtime_structure.py`、`check_runtime_plan.py`、`check-openspec.sh` —— 复用注意点：设计待裁不隐式消失；结构覆盖 strict 全 coverage **不得靠 `tagent: *` 通配通过**；历史 wiki 按固定 SHA 校验、新文档映射新代码；归档**另行授权**，按标准流程同步主 spec；闭合**逐条对照 `expectations.md`（O/KR 预期）与 `trpc-reuse.md`（原生复用依据）**，未裁项如实保留—— 验证：
  ```sh
  python3 scripts/check_runtime_plan.py --change-dir openspec/changes/runtime-rebuild --json
  python3 scripts/check_runtime_evidence.py --check-contract --json
  python3 scripts/check_runtime_structure.py
  python3 scripts/check_runtime_architecture.py
  bash scripts/check-openspec.sh
  openspec validate --all --strict --no-interactive
  ```

## 新增问题修复

（执行期发现的异常/缺口写在此节，格式：场景 → 根源 → 影响面。契约变化先回写 `design.md`，不静默塞进补丁。）

## C 系列（概念收敛战役，见 ../../concept-slim-plan.md）
- [x] 09.21 盲测终验（不知情 agent 双盲测） —— 验证：见 task-graph 09.21（静态锚+回归锚双轨）

- [x] 09.7 终态分块回溯 review（孙任务×领域×原型对照；辩证穿透不以契约为金标准；简单+清晰双维评估）—— 验证：见 task-graph 09.7
- [x] 09.8 收尾账三件清理（comment_policy/docs/api/doc-refs 口径）—— 验证：lint.sh/gen_godoc --check/doc-refs exit 0
- [x] 09.9 注释债清理批（457 条分批+legacy 豁免+proc-refs 豁免）—— 验证：comment_policy 全域 exit 0/lint.sh 全绿
