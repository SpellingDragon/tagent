# Design: 04-single-source-gates-docs

## 技术要点

- 常量单源：fuse.go bytesPerToken / request_assembly overhead 改为引用 window_bounds 登记常量（或登记处反向生成 Go 常量），机械连接而非注释"两表一致"；反扫词根补全（tokens 复数等）。
- gate 洞：GENERIC_AGENT_KEYS 与 decode.go agentKeys 收敛单一登记（gate 读 decode 导出或共享清单）；check_feature_boundary 解析器补列表式 `- id:` agent 分支；check_schema_allowlist 同修。
- preflight：恢复长稳定性种子与覆盖，见下"决策 D4.3-1"。
- 文档：本域 owning 页清偿已撤机制残留；一个事实一个家，交付/热更页归 01/05。

## 决策 D4.3-1：preflight 长跑分片的种子覆盖恢复

- **决策点**：`scripts/gates/preflight.sh` 的 event-long 段从 `for seed in 1 7 42`（六分片）被收窄到 `for seed in 1`（两分片），且新增 `--skip-event-long` 开关；提交前长跑覆盖是否维持收窄。
- **选择**：恢复 `for seed in 1 7 42`，与 `.github/workflows/ci.yml` 的 `event-integrity-long` 矩阵（`seed: [1, 7, 42]` × `window: [256, 32768]`）逐项对齐；`--skip-event-long` 保留，但跳过时与 `--skip-smoke` 同述"本次 preflight 不具备完整 MR 等价证据"。
- **理由**：收窄后本地链与 CI 判的不是同一组（种子, 窗口）组合，本地全绿不再是"CIMR 很可能绿"的等价证据（本机长跑是该脚本存在的全部理由）。跳过开关本身不是收窄——它显式声明放弃了什么；静默少跑两个种子才是。
- **替代方案及未采用原因**：① 保留单种子并只写裁决（不恢复）——把"本地等价证据"降级为文档承诺，验证面实际缩水；② 把长跑整体移出 preflight 交给 CI——提交前失去唯一的真实进程重启面，且与 07 的"CI 分片并发度收敛"重复。
- **代价**：完整 preflight 的长跑段从两分片回到六分片（本机 256 窗分片约 2800s/片）。这是既有验收面的原成本，不是新增成本。

## 文件清单

见 proposal 接口面。

## 风险与回退

- 风险：gate 解析器改动可能误伤既有合法配置；以正反样例自测（合成合法/非法各一）钉住。
- 回退：单域回滚；常量单源为重构无行为变化。