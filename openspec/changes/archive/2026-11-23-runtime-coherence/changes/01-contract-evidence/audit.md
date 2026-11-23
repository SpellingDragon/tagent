# audit.md · 01.101 取证台账（Wave 1 收口）

候选 `52d7b664`（2026-11-22）。执行方：W1-A/B/C/E 子代理＋编排者亲验；证据原件见 [anchors.md](evidence/anchors.md)、[store-probes.md](evidence/store-probes.md)、[state-probes.md](evidence/state-probes.md)、[ci-triage.md](evidence/ci-triage.md)。

## 判定总表

| # | 疑点 | 终态 | 关键证据 | 承接叶 |
|---|---|---|---|---|
| 1 | 贡献经正式 `Boot` 不生效 | **已复现** | S1 锚 rc=1×2（builder 0 次、turn 正常完成）；`cfg.plugins` 在 host.go 零引用（grep exit 1） | 02.203 |
| 2 | 审批回答写失败后切面丢未决项 | **已复现** | S2 锚 rc=1×2（`the cut states [] under []`）；定性＝切面丢项，非误放行（不放行负锚绿） | 02.206 |
| 3 | 已知完整键走 `Scan` 无点读 | **已复现** | Scan(完整键)＝全表枚举（N=1000→2048 slots、N=5000→16384）；Get 恒 1–3 slots；`TierFacts.Get` 已在 | 02.201 |
| 4 | 派生索引零偏移静默改答案 | **已复现** | `order.line` word 置 0 后 `GetParent` 答 `""`，全序走查答 `probe-b1`（rc=1 确定性）；word 无校验和、0 在合法域不触发 rebuild（设计差距） | 02.202 |
| 5 | 任务在线/恢复双解释 | **已驳回（行为层）** | 同序列两路径 status/revision/round/receipts 全一致（×10 绿）。边界：仅内存 Flush 消费（无日志收据）时 obligations 0 vs 2——恢复按日志确认口径，at-least-once 重投由幂等吸收，记为边界行为非缺陷。**结构性双份解释仍在**（lifecycle/restore 两套代码），按 P2 收敛但不按缺陷修 | 02.204（结构收敛） |
| 6 | 旧切面缺新消费者切片→空态启动 | **已复现** | fold 8/24、缺 16 条覆盖区事实、空态启动（×5 稳定红）；机理＝`bootIndexWalk` 以 projection 切片定界＋`RestoreState` 只还声明切片 | 01.102 契约＋03.301 |
| 7 | CI retry 三项红 | **产品缺陷（f13509f9 引入）** | 7b0cde16 绿→f13509f9 红；`-count=5` 全轮红；其 "repeat folds again" 语义与既有 D8.3 裁定（重复不得重述已折叠材料）及三项契约测试相反 | 02.205（修回契约语义） |
| 8 | CI Scenario03 / event-short shard6 红 | **环境或负载（CI 特有）** | 本地 `count=1/5` 绿；shard6 本地含 `-race` 绿（rc=0, verdict=match） | 04.402（CI 稳定性） |
| 9 | `TestNativeConversationRoundTrip` | **本地间歇 3/10** | settle 观测入列时序竞态（call-1 材料整体缺席 view #2）；产品或夹具待裁 | 04.401 |
| 10 | reload 不重建插件集 | **静态证实** | `Reload` 全程无 plugins 触碰（契约注释同证）；贡献经 reload 的存活语义未定义 | 01.104 契约 |
| 11 | Stateful 贡献无公开入口门 | **静态证实（W1-A 障碍）** | `HostOption` 无状态切片贡献面；`RegisterStateSlice` 仅 boot 后 | 02.204 |

## 计时项（W1-F，收口后独占串行重取）

`TestBootWallClockRejectsUnattributedGrowth`、`TestSymptomAnchorPerGenerationCostFlatOverALongRun`：结果见 [timing-triage.md](evidence/timing-triage.md)（若该文件缺失＝尚未执行，不得视为已分诊）。

## 症状锚终态

- **S1 已复现且第一天红**：`TestSymptomAnchorHostBootMountsContributedPlugin`（assembly）。
- **S2 已复现且第一天红**：`TestSymptomAnchorFailedRulingKeepsTheHold`（runtime/task/approval）。
- 必答一问：本变更完成后，"宿主用公开入口声明插件与消费者即真实生效"这句话的场景不会再发生——证据即 S1 红转绿（02.203/02.204 交付时），回滚即红已由当前红态证得。

## 保留/删除清单（供 03.304 复核）

- 保留：`TierFacts.Get`（既有能力，成为契约点读载体）；`CommitOrder` 可选能力；六端口与兼容读法；三族收据。
- 待删（须替代证据）：`governanceTail`/`readReceiptLocked` 的 `Scan(完整键)` 形态；`Boot` 自建 options 丢弃 `cfg.plugins` 的路径；`foldLocked` 与 `applyCommitLocked` 的重复解释（收敛为一份后）；`replayChainPastCut` 的压缩特判（03.301 后）。
- 不删：`eachHeld` 前缀扫描本身（列表语义合法）；`anchorEntry.Content`（03.301 判定唯一来源前不动）。
