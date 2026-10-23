## 1. 源码

- [x] 1.1 contracts.go 增 StatePlan/Governance；state.go 增 SystemPrompt 字段与读取、Apply 改三段管道签名、快照含 Prompt；config.go ReloadConfig 改注入 resolve 与 Governance，ModelCompletion 改读 state 保存值 —— 验证：`GOPROXY=off go vet ./prototype`。
- [x] 1.2 迁移/更新集成测试：恒真管道、任一段拒绝零改动、段序与计划内容、prompt 经 Reload 生效且未 Reload 不变、快照恢复生效 prompt —— 验证：`GOPROXY=off go test ./prototype -count=10 -timeout=90s`。

## 2. 文档与复验

- [x] 2.1 README 增补一体双生管道（config→plan→govern→state）与生效 prompt 归属说明 —— 验证：读回 README 并执行其中测试命令。
- [x] 2.2 主代理复跑全量 `-count=10`、`-race -count=10`、vet、strict 校验 —— 验证：`GOPROXY=off go test -race ./prototype -count=10 -timeout=180s`；`openspec validate config-state-twinship --strict`。

## 验证记录

- 两子代理并行（源码+测试 / README），无写域交叉；README 按冻结契约先行，源码未偏离签名，主代理核对后无需返工。
- 主代理复跑：`go test ./prototype -count=10 -timeout=90s` ok；`go test -race ./prototype -count=10 -timeout=180s` ok（约 22s）；`go vet` 无输出；`gofmt -l` 空；`git diff --check` 空。
- 行为核验：TestAgentSystemPromptOwnedByState 证明请求期不触 registry.Skills（未 Reload 改 registry 不变、Reload 后随 state 更新）；TestAgentTwinTruthyPipeline/TwinGovernanceRejectKeepsState/TwinGovernanceSegmentOrderAndPlan 覆盖恒真、任一段拒绝零改动（含 prompt 与 Removes）、段序与计划内容；recovery 两端断言快照恢复 SystemPrompt。
- README 能力行"生效 system prompt：已实现"经源码与测试复核为据实描述。

