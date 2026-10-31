# ORCHESTRATION: attention-lifecycle

## ① 结论

抵达 DoD 的唯一路径：W1 冻结 attention 事实族并让两条症状锚红着入库 → W2 以
不相交触碰集并发迁入审批与投影清除 → W3 编排者收尾消费者锚与 F1–F4。
当前最大风险：启动门（PR #33 归档）未过前任何文件级规划都会随消费循环落地而
漂移，故派发单在门过时才冻结文件清单。

## ② 编排四问

- **次序**：01 → {02 ∥ 03} → W3。02/03 并发准入 = 触碰集不相交（design D6）。
- **授权**：每单白名单精确到文件，只含本单所需；`api/event` W1 后冻结；账目
  （一级 tasks.md checkbox + 本台账）单点归属编排者。零容忍门（comment 棘轮、
  doc budget、命名门）的 finding 由各单在自己白名单内自清（命令随令下发），
  共享面与全局面的 finding 归编排者；编排者终跑门是唯一终态判据。
- **凭据**：完成以复跑命令 exit 0 为准（正锚/负锚/消费者锚随任务行冻结），
  汇报不算数；W2 收尾派独立只读审者对照原任务行做偏差评估。
- **释放**：到法定停点必须停并列清单等裁定——资源门缺凭据 / 提交归档需明示 /
  重大取向裁定 / blocked / DoD。等待表：
  | 等待项 | 等谁 | 解除条件 |
  |---|---|---|
  | 启动门 | PR #33（event-driven-consumption + compression + oom 线）落地 next 并归档 | `openspec list` 不含 event-driven-consumption |
  | CI 长时门（PR #33 当前轮） | GitHub Actions | `gh pr checks 33` 全绿后 squash merge |

## ③ 明细

### 派发单-孙任务矩阵

| 派发单 | 孙任务（详见各域 tasks.md） | 白名单要点 |
|---|---|---|
| W1-01 | 01 域 T1–T6 | `api/event/**`、`runtime/task/attention*`、两条红锚测试文件 |
| W2-02 | 02 域 T1–T6 | `runtime/task/approval/**`、`features/evolution/governance/**`、`runtime/agent/fuse.go`、`runtime/task/hold.go`、`tests/integration/**/attention_approval_*` |
| W2-03 | 03 域 T1–T6 | `memory/query/**`、`assembly/host*.go`、`features/evolution/meditation/deliver.go`、`examples/wechat-bot/wechat_approval*.go`、`tests/integration/**/attention_projection_*` |
| W3-编 | F1–F4 + 消费者锚全量 | 只读核验 + 台账；改动仅限归档与导览文档 |

### 合单判据

同文件簇（如 wechat 人侧多文件）必须合单；02 与 03 对 `tests/integration` 的
分侧以文件名前缀（attention_approval_ / attention_projection_）硬隔离，越界
文件出现在对方单中即重开。

### 预算

W1 一轮；W2 每域一轮 + 一轮返工余量；W3 一轮。全部孙任务的验证命令必须在
本机 ≤10 分钟内可复跑（长时门只出现在 F2 的 CI 面）。

## ④ 台账

| 轮 | 凭据 | 备注 |
|---|---|---|
| 2026-11-01 立案 | 用户四项裁定（收编面=审批+notice 全量/summary 投影接入；中档 3 域；全量门；两红锚）+ 三路径现状调查（approval_store.go / inventory.go / deliver.go 实证） | 计划工件建于分支 attention-lifecycle（基线 origin/next 21ba86a3） |
| 2026-11-01 红锚落库 | `go test ./tests/integration/host -run '^TestAttentionClears' -count=2` 两锚皆红：锚 1 于 attention_lifecycle_test.go:85（Lookup 仍报 answered approval 为 approved），锚 2 于 :164（未消费通知 Close 后以 phase "accepted" 挂着）；comment_policy 对新文件零 finding（余 17 条为 next 既有 consumer_loop_convergence_test.go 的基线状态，不属本变更） | 症状锚已按 R33 第一天红着；一级 tasks W1 红锚项勾选 |
| 待办 | 启动门核实命令与凭据；W1 派发单冻结 | 门过前不冻结文件清单 |
| 2026-11-01 02:22 门状态 | `gh pr checks 33`（run 41224322781）：绿=openspec/race/mutations/symptoms/format-and-contract/validators/long×3(32768)；红=test（TestSettleWakeUnified 20s 窗 flaky，本地 -count=10 全绿）与 event-integrity-short（45m 上限被取消，日志“operation was canceled”，非测试失败，系 6 长分片同 runner 挤占）；pending=long×3(256 分片，新预算 120m 跑至约 90 分钟) | 处置：等 256 分片释放 runner 后 `gh run rerun 41224322781 --failed`；此间不 push（push 会取消在跑分片） |
