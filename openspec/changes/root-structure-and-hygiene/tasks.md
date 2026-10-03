# Tasks

## 1. P1 追踪卫生

- [x] 1.1 （实测：10 个残骸目录全为 0 字节、仅含 lock/journal、无 .md/.json 数据后删除；`.pre-isolation-backup/`、`.tagent-workspace/` 按约保留）`git rm` hottest-sub1/ hottest-sub2/（4 个零字节文件）；删孤儿 `go.work.sum`；清工作树遗物（own-*、hottest-sub3、hottest-drop-*；`.pre-isolation-backup/`、`.tagent-workspace/` 属运行/备份数据，默认保留）
- [x] 1.2 （实现为 `codetools tracked-hygiene` 子命令，纯函数 + 3 条单测；接入 `scripts/lint.sh` 与本地同命令；负路径双红留痕 `hygiene-probe.log`）新增 `scripts/check_tracked_hygiene`（空文件/运行期后缀/顶层白名单三查）并接入 validators job；负路径探针（空文件与 `x.lock` 各造一枚）双红留痕后撤除
- [x] 1.3 （`go test ./... -short` 31 包全绿零 FAIL、`GOMAXPROCS=1` 根包 59.6s ok、lint ok 含新门）全量 short + lint 绿；提交

## 2. P2 退役陈旧 AReaL 训练桥 + dotted-refs 门（依 D6/D7 与用户裁决）

- [x] 2.1 （桥三件 git rm；convert_trajectories.py git mv 至 scripts/；README 知识并入 wiki rl-architecture 新增 #offline-converter 与退役记录；白名单重生成 21 目录不含 train）删除 `train/rl/tagent_adapter.py`、`examples/wechat-bot/train_tagent.py`、`examples/wechat-bot/train_rl_config.yaml`；`git mv train/rl/convert_trajectories.py scripts/convert_trajectories.py`；`train/rl/README.md` 中仅转换器相关内容并入 wiki rl 页，其余随桥退役；顶层白名单去 `train`
- [x] 2.2 （run.sh 摘除 areal 命令族/变量/状态块共 59 处，bash -n 过；切除留下的孤儿 shift;; 与无标题横幅两处手术伤已修；`rl` 运行时模式与 AREAL_API_KEY 认证行保留；root/bot README 同步）`run.sh` 移除 areal 命令族与 AREAL_* 变量（`rl` 运行时模式保留）；root/bot README 与 wiki `rl-architecture` 同步：退役事实、保留面（`rl/` Go 包、转换器、RL 运行时配置）、重接条件
- [x] 2.3 （codetools dotted-refs：位置精确提取 + 纯函数判定 + 2 组单测；接入 lint.sh；探针红/复原绿留痕 dotted-refs-probe.log；期间 doc-refs 连锁抓到退役记录的悬空引用并已改写）新增 `codetools dotted-refs`（位置精确：`python -m` 后续 token 与 yaml `workflow:` 值；仓库内解析或显式外部允许表）+ 单测 + 接入 lint.sh；负路径探针（造一条指向已删布局的 workflow 引用必红）留痕
- [x] 2.4 （`go test ./... -short` 31 包零 FAIL；bot 模块三连绿 final rc=0——首跑曾见一次无法复现的 exit status 1，如实记档交 CI 裁决；lint ok 含两道新门）全量 short + lint（含两新门）绿；提交

## 3. P3 结构一步到位（D10/D11）

### C1 配置模型外迁 config 包

- [~] 3.2 config.go → config/ 包（代码已完成，工作树未提交；背景见 design D12）
  - [x] config/ 五文件成形（config/modelref/clone/lifecycle/config_test）+ 根包 config_alias.go（20 类型别名 + 2 常量 + 2 函数包装）+ IsRemoteRef 导出化 + 4 个注册表系测试并回 registry_test.go + build/vet/config 测试/根包全量 short 57.9s/gen_godoc/hygiene 白名单 全过

  - [ ] 3.2a 清 org_hotreload 最后一条 lint finding
    - 对象：`org_hotreload.go` 约 440 行、`orgSubset` 类型 doc 上方的孤儿行，原文恰为：`// Clone returns a private deep copy of the configuration: a published generation`
    - 做法：删除该行；把"私有深拷贝、发布代各自持有快照"这层意思并入 `config/clone.go` 的 `Clone` doc（现 doc 已含 round-trip/ConfigPath/omitempty 三点，补上第一句语义即可）
    - 完成：`go run ./scripts/comment_policy -v . examples/wechat-bot` 输出中 `org_hotreload.go` 零 finding
    - 边界：不许动 orgSubset 自身 doc 与类型体；不许为清 finding 调 `--update-baseline`

  - [ ] 3.2b README 布局表登记 config/ 层
    - 对象：README.md 的模块表（`| \`agent/\` |` 行起，约 232 行）
    - 做法：在 `agent/` 行**之前**插入一行 `| \`config/\` | 配置模型层：编排声明的类型实体（Config/AgentConfig/ToolRef 族）与 LoadConfig/严格校验/生命周期投影；根包以别名再导出保持 tagent.* API 不变 |`——位置在 agent 之前是分层顺序（root → config → agent）
    - 完成：肉眼核对表行对齐；`bash scripts/lint.sh` 的 doc-refs 段仍绿
    - 边界：只加一行，不重排现有行

  - [ ] 3.2c TestArch_LayeredDependencyDirection 预跑（防 C1 提交即红）
    - 对象：`guardrails_test.go:90` 的既有分层断言测试
    - 做法：`go test . -run '^TestArch_LayeredDependencyDirection$' -count=1 -v` 直接跑；若因新增 `config` 包而红，**只允许**把 config 加进该测试枚举的包集合（这正是 C2/3.5 的正式扩集的前置信号，先做最小修正让 C1 可提交），并把红的原因记进 3.5 的执行注记
    - 完成：该测试 PASS
    - 边界：不许删除断言、不许放宽既有方向规则；若红因与 config 无关 ⇒ 停，按熔断上报

  - [ ] 3.2d TestLiveSession 并发红定性
    - 对象：`cross_generation_test.go` 的 `TestLiveSessionStaysWatchedAcrossToolGeneration`（曾于全量 `./...` 并发跑红一次 32.19s；单包复跑绿 12.8s）
    - 做法：连跑三次 `go test ./... -short -count=1`，记录该测试每次结果
    - 完成（二选一，各自闭环）：①三次全绿 ⇒ 在本任务后追加注记"不可复现，三连绿放行"并勾掉；②任一次复现 ⇒ **不在本变更修**，将复现命令与输出追加到 design D12，立新档处理（同族先例：fix-nested-hop-alert-attribution）
    - 边界：禁止用调大 waitFor 预算的方式"处理"；禁止跳过该测试

- [ ] 3.3 C1 提交（前置：3.2a–3.2d 全勾）
  - 做法：`gofmt -l .` 为空 → `go build ./...` → `go test ./config/ . -count=1 -short` → `go test . -count=1 -race -short` → `GOMAXPROCS=1 go test . -short -count=1` → `bash scripts/gen_godoc.sh && bash scripts/gen_godoc.sh --check` → `bash scripts/lint.sh` → `openspec validate root-structure-and-hygiene --strict`，全绿后 `git add` 全部 C1 路径（config/、config_alias.go、wiring.go、build_agent.go、org_hotreload.go、partition_collision.go、registry_test.go、scripts/codetools/hygiene.go、docs/api/）提交
  - 完成：提交落库，`git status --short` 为空；tasks 3.2 主项改 [x]
  - 边界：提交信息写明"行为零变更：模型外迁 + 别名层"；不许把 C2 的任何改动混进本提交

### C2 纯查询随迁 + 分层立法修订

- [ ] 3.4 partition_collision 四查询迁 config 包
  - 对象：`partition_collision.go` 中四个以 `*Config`/`*AgentConfig` 为参的纯函数：`agentMemoryFingerprint`、`changedMemoryAgents`、`reachableAgents`、`remoteDeclarationOnly`
  - 做法：`git mv` 不可用（同文件拆分）——剪切四函数（连同各自 doc）入新文件 `config/queries.go`（文件头加 `契约: docs/wiki/platform/platform-subsystems.md#config-surface`），导出为首字母大写（`AgentMemoryFingerprint` 等）；根包调用点（`org_hotreload.go`、`org_candidate_overlay.go`、`build_agent.go` 中 grep 四个旧名可得全部位置）改为 `config.` 前缀；`partition_collision.go` 只剩注册表三方法与 `var _ = agent.TagentAgent{}`——若该哑变量失去存在理由一并清理
  - 完成：`go build ./...` + `go vet ./...` + `go test ./config/ . -count=1 -short` 全绿；`grep -rn "agentMemoryFingerprint\|changedMemoryAgents\|reachableAgents\|remoteDeclarationOnly" *.go` 零命中（根包无残留旧名）
  - 边界：函数体逻辑零改动（纯移动+改名）；`runtimeConfig` 的三个注册表方法不动（它们属 C3 的接口化，不属本任务）

- [ ] 3.5 分层断言扩集 + 两法条措辞落位
  - 对象：`guardrails_test.go` 的 `TestArch_LayeredDependencyDirection`；`openspec/changes/root-structure-and-hygiene/specs/architecture-guardrails/spec.md`（MODIFIED 两条已写好，无需再改）；`docs/wiki/agent/agent-architecture.md` 的 `#package-layout` 表
  - 做法：断言测试按其既有模式加两条——①config 及其子包 MUST NOT import 根包；②`agent/`（不含子包）MUST NOT import config，`agent/org`（C3 产物，尚不存在时先留 TODO 注释）例外。wiki package-layout 表补 `config/` 行与根包"发布权所在"注记
  - 完成：`go test . -run TestArch_LayeredDependencyDirection -count=1 -v` PASS 且新断言真实生效（临时在 config/config.go 加 `"github.com/SpellingDragon/tagent"` import 应使测试红——验完撤掉，输出留注记）
  - 边界：方向规则只增不减；负路径验证的临时 import 必须撤净（`git diff config/` 为空）

- [ ] 3.6 C2 提交（前置：3.4、3.5）
  - 做法：与 3.3 同一面验证墙（含 `GOMAXPROCS=1` 与 `-race`），提交范围：`config/queries.go`、`partition_collision.go`、根包调用点、`guardrails_test.go`、wiki、docs/api
  - 完成：提交落库、工作树净
  - 边界：不混入 C3 改动

### C3 世代机制外迁 agent/org

- [ ] 3.7 三件迁移 + 注入契约落地
  - 对象与逐件处置：
    1. `owner_retirement.go` 整文件 → `agent/org/retirement.go`：`retirementLedger`→`Ledger`、`newRetirementLedger`→`NewLedger`、`retireDecision`→`RetireDecision`（其余方法名不变）；零根包依赖，预期只需改包名与文件头索引（`契约:` 指向 wiki 世代页）
    2. `org_candidate_txn.go` 整文件 → `agent/org/candidate_txn.go`：`candidateTxn`→`Txn`；其 `rc *runtimeConfig` 字段改为注入结构 `deps{ UnregisterStoreOwner func(name string) }`；`recordDiscardOrder`/`lastDiscardOrder` 随迁，根侧若有测试读它则经新导出名
    3. `org_candidate_overlay.go` 整文件 → `agent/org/overlay.go`：`buildCandidateOwners`→`BuildOwners`；`rc` 的三个用法逐一替换——`rc.resident`→注入的 resident 句柄（其类型即根包现有封装类型，经参数传入）、`rc.residentMemFP`→随族自管字段、`buildAgent(...)` 调用→注入的 `ShellBuilder func(name string, acfg config.AgentConfig, cfg config.Config, cache map[string]*agent.TagentAgent) (*agent.TagentAgent, error)`（根包闭包内固定 buildModeResident）
  - 做法：`mkdir agent/org` 后逐件 `git mv` + 改造；根包（`org_hotreload.go`、`tagent.go`、`build_agent.go`）调用点改为 `org.Xxx`；`org_hotreload.go` **整文件留根**（发布权物理位置，D9/D10 裁决）
  - 完成：`go build ./...`、`go vet ./...`、`go test ./agent/... . -count=1 -short` 全绿；`grep -rn "runtimeConfig" agent/org/` 零命中（机制不见装配态——这正是 spec 新 scenario 的机器判据）
  - 边界：`owner_retirement_test.go` 等**测试文件不动**（D3：装配级测试留根）；若改造中发现第四个根包依赖点 ⇒ 停，先回 design 补 D8' 再继续；`agent/org` 不得 import 根包（编译器与 3.5 断言双保险）

- [ ] 3.8 C3 提交（前置：3.7）
  - 做法：验证墙同 3.3，另加 `go test ./agent/org/ -count=1 -race`；`bash scripts/gen_godoc.sh` 重生成（新包进 docs/api）；提交
  - 完成：提交落库、工作树净、`docs/api` 含 agent/org 页
  - 边界：不混入 P4

## 4. P4 巨型测试按域拆分

- [ ] 4.1 三文件按域拆分（前置：C3 完成后做，避免与生产迁移互相踩）
  - 对象与目标（源文件 → 目标文件名，全部留根包，每文件 ≤600 行）：
    - `org_candidate_test.go`(2007 行) → `org_candidate_txn_test.go`（候选事务/回滚族）+ `org_candidate_overlay_test.go`（换壳/采纳族）+ `org_candidate_store_test.go`（存储共享/碰撞族）
    - `cross_generation_test.go`(1969 行) → `cross_generation_publish_test.go`（发布/排队输入族）+ `cross_generation_reentry_test.go`（重入族，含 walReentry 常量块）+ `cross_generation_delegation_test.go`（委派/嵌套跳族）
    - `org_hotreload_test.go`(1715 行) → `org_hotreload_core_test.go`（发布/回滚主链）+ `org_hotreload_rollback_test.go`（回滚族）+ `org_hotreload_gates_test.go`（闸门/守卫族）
  - 做法：按**整测试函数**为单位移动（函数体零改动）；共享 helper（waitFor/countServed/chainYAML 等）留在原文件或移入被最多目标引用的那份；每个新文件头加 `契约:` 索引（沿用源文件的锚）；`delegation_test.go` 的 delegModel/park 族 helper 不动
  - 完成：三个源文件删除或缩至纯 helper；`go test . -count=1 -short` 全绿且测试总数与拆分前一致（`go test . -list '.*' -short | wc -l` 前后相等）
  - 边界：禁止借拆分改任何断言/预算/谓词——那是行为变更，需另立 fail-before；行数是目标不是硬门，某域不足 600 行不强行再拆

- [ ] 4.2 无损证明 + 同位门核对（前置：4.1）
  - 做法：对三个源文件分别跑 `bash scripts/check_test_merge.sh <拆分前基线ref> <源文件路径>`（基线 ref 用 C3 的提交号）；`bash scripts/lint.sh` 确认 responsibility-fragmentation 零 finding
  - 完成：三份无损证明输出留痕到 change 目录 `p4-merge-proof.log`
  - 边界：证明失败 ⇒ 回 4.1 修，不许调门

- [ ] 4.3 P4 提交（前置：4.2）
  - 做法：验证墙同 3.3 另加 `GOMAXPROCS=1 go test . -short -count=1`；提交
  - 完成：提交落库、工作树净

## 5. 收口

- [ ] 5.1 文档面同步（前置：P4）
  - 对象：README 模块表（若 C2/C3 后有出入）、`docs/wiki/agent/agent-architecture.md#package-layout` 五组职责表（补 config 与 agent/org 两行、根包行改为"装配+发布权"）、`docs/wiki/README.md` 索引
  - 完成：`bash scripts/lint.sh`（doc-refs/gen_godoc --check）绿；`grep -rn "train/" README.md docs/wiki` 零活引用
  - 边界：只补布局事实，不重写文档

- [ ] 5.2 终验与归档（前置：5.1）
  - 做法：`git push` 后盯 CI 至四 job 绿（test/race/validators/openspec）；`openspec archive root-structure-and-hygiene -y`；`openspec validate --specs --strict`
  - 完成：归档目录出现 `2026-10-XX-root-structure-and-hygiene`，spec 合并计数 +2 ADDED +2 MODIFIED
  - 边界：CI 任一 job 红 ⇒ 停在归档前，先诊断（禁止带着红归档）
