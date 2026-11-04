# 09 远端 CI run 证明状态（2026-11-04）

台账 r162 声称：CI 全绿（run 41772779593 @ cd590152 SUCCESS），六 long 分片 + test/race/mutations/short×10/validators/openspec 全绿，并据此标注 goal complete。

## 复核事实

- cd590152 是可达提交（2026-11-04「test: count the settle notice by presence in the long-run reconcile」），但 HEAD 3b727199 比它晚一个提交；台账没有引用 HEAD 上的 run。
- 本目录（evidence/）在复核前只有 04-concept-net-ledger.md、06-compression-legacy-authority.md 与 inherited-pr36-plan/；**没有** CI 日志、run 工件、job 结论、badge 或导出文件。
- 本次会话尝试查询 GitHub Actions API 返回 403（未认证/限流），无法从外部核验该 run 的 conclusion。
- ORCHESTRATION r151 自记「squash main 170ef267 后 runtime-contracts 红」，r154/r158 也记录了 CI 红与修复过程；即 CI 并非从一开始就绿。
- openspec/specs/orchestration/spec.md:303-310 要求「本地全套门禁绿后推送，并确认远端 CI 全绿后方可归档」；归档目录由 170ef267（00:53）建立，早于被引 run 的 commit（12:22）。

## 结论

该 run 的 SUCCESS 结论**无法从一份新克隆审计**，因此不能作为「证据链闭合」的凭据。本文件不改变历史台账的自述，只把它登记为「台账自述 / 待工件」。

## 要闭合它需要什么

1. 把 run 41772779593 的 job 结论（每个 job 的 conclusion、起止、artifact 名）导出为文本，落到 evidence/ 并在台账指向它；
2. 或补一条在 HEAD 3b727199 上的 run 工件；
3. 在 tasks.md 的 F2/F4 与 r162 之间建立可核对的引用链（见 08）。

在这些工件落盘之前，本项目对外不应宣称 goal complete。
