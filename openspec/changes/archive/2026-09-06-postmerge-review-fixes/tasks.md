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

- [x] 3.1 W2:Approval.Check 未命中时重扫 approvals 目录(**审批闭环实际靠此文件重扫**——外部落盘批准文件即经 Check 可见);Gate 暴露 `Approval()`。**Minor⑤宣称校准**:Decide=**预留接口**(供微信/CLI 通道回写,当前无生产调用方;原"Decide 接通消息/CLI 审批通道"言过其实,闭环实际靠文件重扫非 Decide)。**用户裁决**:审批送达=文件为主(external 落盘 approvals/<id>.json)+预留微信接口(调 Approval().Decide)
- [x] 3.2 W3:治理包裹扩展到所有 agent 的非 wrapper leaf 工具。**设计决策点**:子 agent 独立预算 vs 共享 entry 预算(影响 BudgetManager 语义)

## 4. Minor 批量(§8.4 全部 12 项)

- [x] 4.1 (9/12修,M1/M4/M7标注后续) declaration_stable_test 第二快照真构 engineBridge 形态;VectorInsert 改用 rustviking 默认 level 或实测验证 `-l 0`;judge 保守 reason 写入发布留痕;发布 pending 窄窗互斥锁;SetActive 单一写锁覆盖 write+缓存;DepMCP 区分业务/传输错误;judge 参数(窗口/阈值)配置化;persistBusEvent 补 trace 锚(或注释固化设计边界);turn span ctx 取消早退补 End;gate.go approval Request 错误处理;"参数/模型热切换"宣称收窄为存储就绪;canary 卡死重评机制

## 5. 门禁与收尾

- [x] 5.1 三道门禁全绿:build/vet + 全量 -short + 新子系统 `-race`(注意路径:治理/可靠性包在 `./agent/` 下)
- [x] 5.2 §8.6 启用禁令解除复核:W2/W3 修毕 → Governance 方可真实部署;E1/E2/W4 修毕 → Evolution 方可出实验环境
- [ ] 5.3 commit(conventional)+ archive 本变更 + 回写 execution-dag §8.3 各项 FIXED 标记 + LEDGER 记录

## 6. 二轮增补 — 修复轮复验新发现(2026-09-06 第三轮复验,§8.9;**本组关闭前 5.3 不得执行**)

- [x] 6.1 N1:wasActive 白名单数据源修复——ReleaseRecord 持久化到 bundle dir(重启恢复),或启动时 seed 磁盘 active.json 曾指向的 id / 显式豁免基线;回归测试:基线 rollback 可用 + 重启后历史 active 仍可回滚
- [x] 6.2 N2:子 agent gate 复用 entry 持久 Ledger(共享实例写同一 governance 分区),替代兜底内存账本;回归测试:子 agent 治理事件重启后可 recall

## 7. 二轮增补 — Minor 批量(§8.9 八项)

- [x] 7.1 预算 Dir="" 不落盘(修 tagent.go:542 filepath.Join 契约违反,优先);mem_spill.go:16 过时注释清除;E2 parentless draft 拒绝分支 rollbackTo 空值守卫
- [x] 7.2 W2 decided/expired 审批文件清理(rebuild 时);tasks 3.1 宣称校准(Decide 通道=预留,闭环=文件重扫);W3 工厂路径 agent 包裹缺口记录或补包;ActivationLog 持久化(或注释固化重启回退语义);测试补缺:buildAgent 级"子 agent exec 过闸" + W2 节流窗内不重扫

## 8. 三轮增补 — 第四轮复验遗留(2026-09-06,execution-dag §8.10;**本组关闭前 5.3 不得执行**)

- [x] 8.1 Major:治理审计事件补来源 agent 归属——DenialRecord.AgentName(json `agent,omitempty`)+ GateDeps.AgentName + GovernanceGate.agentName + writeGovernanceEvent metadata["agent"](omitempty,单 entry 不写噪声键)+ rebuildFromStore 回读 + tagent.go:565 接线传 `name`;回归:governance_wire_test.go 双 agent(tagent/worker)治理记录按 AgentName 区分来源(fail-before 探针:临时置 AgentName="" 则 sawEntry/sawSub 断言双双失败)
- [x] 8.2 补 governance_wire_test.go `TestBuildAgent_GovernanceWrapsAllAgents_SharedLedger`:走**真实 buildAgent** 构建 entry(tagent)+ 子 agent(worker),经构建出的 exec 工具链(OutputLimitTool→GovernanceTool→mock,Declaration.Name="exec")驱动 critical `rm -rf`,断言两 agent 均被治理闸拒(`[governance_denied]`)——此前仅 governance 包 gate_w3_test 手动模拟双 gate、wire 测试只断言 New 成功,无 buildAgent 级子 agent 过闸证明

## 9. 三轮增补 — Minor 批量(§8.10 八项)

- [x] 9.1 Ledger.Record 锁内快照 store/pid(消与 BindStore 并发写字段的 -race 竞争,governance `-race` 绿);n1_test 补 `TestRelease_SubmitReloadRefineRollback`(Submit 快道 draft1/draft2 正式 active→重载新 ReleaseManager loadHistory 恢复→refineRollback 到曾 active 的 draft1 成功);governance_wire_test 断言两 agent 记录落**同一** rc.govLedger(③ 行为证明同指针);release.go ④ `seedActiveBaseline`(NewReleaseManager 对当前 active 补 seed)+ NoteActive 幂等(wasActive 早返回)修 InitBaseline 崩溃窗口(active.json 已写而 releases.jsonl 未写→重启仍入白名单)
- [x] 9.2 删 gate.go `BindLedger` 死代码(语义与 ledger.BindStore 相反:N2 后无调用方,tagent.go 已改用 rc.govLedger.BindStore;删后 gate.go 不再 import memory);release.go Submit ⑥ `active==nil` 直接 reject 守卫(fail-before 探针:临时禁用守卫则孤儿 draft 快道 SetActive 滞留 active、stage=active);approval.go ⑦ 注入 `now func()`+`rescanInterval` 字段,approval_w2_test 假时钟确定性验节流(消 CI 重载 >2s 假失败)+ 补 `TestApproval_RescanWindowExpiryResumes` 窗过期恢复重扫正向用例;hybrid-semantic-recall/tasks.md 头部执行状态 + 7.3 过时 BLOCKED 措辞同步(5.1/5.3 已于 2026-09-06 真实 ZAI_API_KEY 实测完成、BLOCKED 解除)
