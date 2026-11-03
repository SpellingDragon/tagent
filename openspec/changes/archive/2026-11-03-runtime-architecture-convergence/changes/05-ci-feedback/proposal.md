# CI 反馈与 merge 前完整证据

父变更：runtime-architecture-convergence。W0-A05 的 Q1–Q7 已由编排者采纳，并增加外部仓库门禁作为硬启动条件。

## Why

近期 CI 壁钟中位数约 90 分钟，long 占绝大多数 runner 成本；stale PR 工作持续运行，short 十轮串行，同一源码产品门重复执行。当前 evidence checker 还允许所有分片一致缩成同一子集，mutation 证据也未纳入同基线校验。直接分片或移门会先放大这些漏洞。

## What Changes

- 先让 short scope 等集、repeat 身份、long 组合、runner+mutation 同基线成为公共 bundle 硬门。
- PR-only stale run 自动取消；merge_group 使用独立并发域，历史 artifacts 保留。
- short 保持十轮十独立进程，使用显式 repeat-index 分片，不使用 count 放大。
- 指定 ci/test 为 root build/vet/short 的唯一 owner；其他 workflow 保留合同/结构职责和稳定检查名。
- 完整六组合目标位置为 merge_group 必过，但只有实际仓库规则启用并验证后才可离开 PR；不得用 main push/nightly 代替。
- race 既有覆盖偏差单独收口，不在提速单里删规；4096 永久 shard 由 06 决定。
- 分别度量 PR fast wall、merge proof wall、runner/stale 总耗；PR fast 目标 ≤10 分钟，merge proof 目标待完整绿样本，禁止编造 p95。

## 边界与依赖

- **依赖**：分片依赖证据等集；移长测依赖 merge queue/required checks 的外部启用证据；09 以外的窗口变更依赖 06。
- **被依赖方**：一级 DoD 使用 merge 前完整 bundle；06 使用 CI 运行证据而非语义替代。
- **接口面**：现有 workflow YAML、run_runtime_scenarios.py CLI、check_runtime_evidence.py --bundle。
- **禁止事项**：不减少既有输入/重启/seed/window；不把 PR head 证据冒充 merge ref；不新增第二解析器；不静默忽略 race 缺口。
