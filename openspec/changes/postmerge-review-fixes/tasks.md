# Tasks: Postmerge Review Fixes

> 来源:[execution-dag.md §8.3/§8.4](../tagent-evolution-roadmap/execution-dag.md)(2026-09-06 深度 review + 第二轮复验确认**全部未修**)。
> 修复方向与 file:line 证据一律以 §8 为准,本清单仅为可勾选执行入口。启用禁令(§8.6)在本清单 E/W 项全部关闭前持续有效。
> 门禁:§4.3 三道门禁亲验;新增修复必须有回归测试(fail before / pass after)。

## 1. Major — 发布道铁律(E1/E2)

- [x] 1.1 E1:refine rollback 目标限定 ReleaseManager.History 中 Stage=active 的 bundle(或经 rm 走审批道);守卫测试:被拒 draft 不可作为 rollback 目标生效
- [x] 1.2 E2:approveGate 为 nil 且未显式 SkipApprovalGate 时默认 **reject**;测试:protected 提示词在无门注入且未跳过时不可激活

## 2. Major — 可靠性收敛(W1/W4)

- [x] 2.1 W1:MemSpill.Replay 重放前逐条 `GetEvent(sp.Key)` 预检(命中即计成功移除),或识别 "already exists" 为幂等成功;回归测试:假阴性失败(KV 已写但解析失败)场景重放收敛、spill 文件归零
- [x] 2.2 W4:记录 bundle 激活时间戳,`Collect` 以激活时刻为窗口起点(bundleID→activationTs 查表),不再固定回看 10m;CanaryHold>0 时窗口语义测试

## 3. Major — 治理闭环(W2/W3,含设计决策点)

- [x] 3.1 W2:Approval.Check 未命中时重扫 approvals 目录;Gate 暴露 `Approval()`;Decide 接通消息/CLI 审批通道。**设计决策点(与维护者确认)**:审批送达通道形态(digest 文件人工落盘 vs 微信交互审批)
- [x] 3.2 W3:治理包裹扩展到所有 agent 的非 wrapper leaf 工具。**设计决策点**:子 agent 独立预算 vs 共享 entry 预算(影响 BudgetManager 语义)

## 4. Minor 批量(§8.4 全部 12 项)

- [x] 4.1 (9/12修,M1/M4/M7标注后续) declaration_stable_test 第二快照真构 engineBridge 形态;VectorInsert 改用 rustviking 默认 level 或实测验证 `-l 0`;judge 保守 reason 写入发布留痕;发布 pending 窄窗互斥锁;SetActive 单一写锁覆盖 write+缓存;DepMCP 区分业务/传输错误;judge 参数(窗口/阈值)配置化;persistBusEvent 补 trace 锚(或注释固化设计边界);turn span ctx 取消早退补 End;gate.go approval Request 错误处理;"参数/模型热切换"宣称收窄为存储就绪;canary 卡死重评机制

## 5. 门禁与收尾

- [x] 5.1 三道门禁全绿:build/vet + 全量 -short + 新子系统 `-race`(注意路径:治理/可靠性包在 `./agent/` 下)
- [x] 5.2 §8.6 启用禁令解除复核:W2/W3 修毕 → Governance 方可真实部署;E1/E2/W4 修毕 → Evolution 方可出实验环境
- [ ] 5.3 commit(conventional)+ archive 本变更 + 回写 execution-dag §8.3 各项 FIXED 标记 + LEDGER 记录

## 6. 二轮增补 — 修复轮复验新发现(2026-09-06 第三轮复验,§8.9;**本组关闭前 5.3 不得执行**)

- [ ] 6.1 N1:wasActive 白名单数据源修复——ReleaseRecord 持久化到 bundle dir(重启恢复),或启动时 seed 磁盘 active.json 曾指向的 id / 显式豁免基线;回归测试:基线 rollback 可用 + 重启后历史 active 仍可回滚
- [ ] 6.2 N2:子 agent gate 复用 entry 持久 Ledger(共享实例写同一 governance 分区),替代兜底内存账本;回归测试:子 agent 治理事件重启后可 recall

## 7. 二轮增补 — Minor 批量(§8.9 八项)

- [ ] 7.1 预算 Dir="" 不落盘(修 tagent.go:542 filepath.Join 契约违反,优先);mem_spill.go:16 过时注释清除;E2 parentless draft 拒绝分支 rollbackTo 空值守卫
- [ ] 7.2 W2 decided/expired 审批文件清理(rebuild 时);tasks 3.1 宣称校准(Decide 通道=预留,闭环=文件重扫);W3 工厂路径 agent 包裹缺口记录或补包;ActivationLog 持久化(或注释固化重启回退语义);测试补缺:buildAgent 级"子 agent exec 过闸" + W2 节流窗内不重扫
