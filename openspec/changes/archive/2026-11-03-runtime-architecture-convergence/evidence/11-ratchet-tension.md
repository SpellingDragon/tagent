# 11 concept_ratchet 收紧设计张力（G-16，需裁定）

现象：golden/runtime/architecture.json 的 concept_ratchet.note 写着「tighten new_max to 0 once the baseline commit advances」，evidence/04-concept-net-ledger.md 也写「基线推进后为 0 / 收紧 new≤0」，但 new_max 至今为 1。

## 为什么不能直接设 0

domain_surface_report()（scripts/gates/check_concept_budget.py）把当前导出类型清单与 concept_bindings.baseline.commit（现为 dfdfa72e70d2cdd91ce8b1db408d5543481dc499，收敛前）对比：
- new_names = 当前 − 基线 → 现在 1 个（ConfigChange，已绑定 fact→config-hot-reload，合法）。
- removed_names = 基线 − 当前 → 现在 10 个（收敛删掉的公共面）。
- check_ratchet() 用 removed_min:10 / new_max:1 / exported_interfaces_max:77 三档冻结这次证明。

若把基线推进到收敛后的提交（如 170ef267 / 3b727199）：
- ConfigChange 进入基线 → new=0，new_max:0 可以成立；
- 但那 10 个被删的公共面也不在基线里 → removed=0 → removed_min:10 立刻判红。

也就是说「推进基线」与「保留 removed≥10 的证明」互斥。直接把 new_max 改成 0 而基线不动，则当前树 new=1 > 0，门立刻红 —— 会拒绝一个已经绑定、经归档裁定的合法事实类型，属于回归而非修复。

## 三个可选裁定

1. **保留证明（推荐）**：baseline 停在 dfdfa72e，new_max 保持 1，把 note 改成「new_max:1 是收敛期唯一已绑定新增（ConfigChange）的冻结额度；不再新增」。同时把 04 概念净账里「基线推进后为 0」改为「若推进基线须连同 retired 地板一并退役」。理由：棘轮的价值正是相对旧基线证明删了 10 个。
2. **退役地板**：推进 baseline 到收敛后提交，把 removed_min 改 0、new_max 改 0，并把「已删 10 面」移入 evidence/04 作为历史结论（不再由门强制）。代价：门不再拦截「把删掉的公共面加回来」。
3. **双基线**：给 concept_bindings 增加第二套「retired_faces」显式清单，让 removed 证明不依赖基线对比，再推进基线。改动最大，需要新 spec delta。

## 处置

本轮**不改** architecture.json（避免把门判红或销毁证明），登记为 BLOCKED 待用户裁定；裁定后需同步改 note、04 概念净账、并补一条 mutation selftest 证明新口径。
