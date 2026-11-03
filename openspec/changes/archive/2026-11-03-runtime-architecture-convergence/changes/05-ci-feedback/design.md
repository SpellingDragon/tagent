# CI 反馈技术决策

## D1 证据先于调度

任何分片/merge_group 重排前，bundle checker 必须证明 short 每轮 test_ids 与合同等集、十个唯一 index/run_id/log、六组合完整，且 runner 与 mutation 来自同一受测基线。被否：十片都写 repeat_total=1；现有 checker 会正确拒绝，但也说明不能直接 matrix 化。

## D2  stale 工作取消域

仅 pull_request 对同 PR 旧 run cancel-in-progress；merge_group、main/dev 完整证明不被 PR push 取消。已完成/已取消历史 artifact 不删除。

## D3 唯一产品执行 owner

ci/test 持有 root build/vet/short；runtime-contracts 保留静态合同与结构检查。保留现有检查名以减少外部分支保护漂移。lint 内部重复在修改前实测并拆清，不以“同名 job 不同”为由双跑。

## D4 十分片而非十轮放大

runner 新增内部 `--repeat-index I`：只执行一轮但 repeat_total 仍读合同 10；与批量 --repeat 混用、越界或非 v2 必须具名拒绝。workflow matrix 恰覆盖 1..10，fail-fast=false，summary 仍 always()+bundle。加速比以实测为准。

## D5 merge queue 是外部硬门

目标是在 merge ref 上跑完整 10 short + 6 long + mutation 与全部 required checks。若仓库未启用 merge queue/required checks，保持现有 PR 全量，不得提前移除。PR 默认不发明缩水 long canary；fast 面由短测、race、mutation、symptom 等组成。

## D6 性能与覆盖分别记账

PR fast required wall 目标 ≤10m；merge proof wall 与 runner 总耗另记。完整绿样本前预算只作故障保护，不能宣称可靠上界。race 全第一方包+wechat 的既有规格缺口单独列为收口任务。

## 风险与回退

- 外部规则不可由 workflow 自证 → 将其设为实施停点，无证据不移门。
- 分片增加 setup 成本 → 实测后再定 5×2 或 10×1，不改变十进程证据总数。
- 去重误删 merge-ref 有意重验 → 只去同一 commit 的重复执行，merge_group 的 merge ref 重验保留。
