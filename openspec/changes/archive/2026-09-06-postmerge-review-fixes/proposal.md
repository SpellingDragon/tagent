# Post-Merge Review Fixes

> 本变更是 tagent-evolution-roadmap 马拉松落地后的**交接前深度 review 修复容器**(非新功能变更)。
> 来源:execution-dag.md §8 四域并行审计(脊柱不变量全 PASS,但揪出 6 Major + 12 Minor)
> + 三轮复验增补(§8.9 N1/N2 + Minor 8、§8.10 第四轮 §8.1 + Minor 8)。用户裁决以本变更为
> 统一修复容器(不另起变更);裁决与复验依据见 openspec/changes/LEDGER.md 与 execution-dag §8。
>
> 本变更**不引入任何能力规格(spec)delta**——纯缺陷修复/审计整改,行为收敛到既有 specs 已声明
> 的契约,故归档走 `--skip-specs`(无 specs/ 目录、无主 spec 增改)。逐项 file:line 证据与
> fail-before/pass-after 回归见 tasks.md 与 execution-dag §8.3-§8.11。

## Why

2026-09 马拉松交付了记忆引擎解耦(C6)、热配置自进化(T-EVO 发布道)、治理与常驻可靠性(T-G)三大子系统。交接前深度 review(四域并行 + 门禁亲验)裁定脊柱不变量全部保持,但新子系统存在若干**会在真实部署/重启/并发下显形**的缺陷:发布道铁律可被绕过(E1/E2)、可靠性收敛假阴性(W1)、治理审批闭环断路(W2)、子 agent 主风险面绕闸(W3)、后验证据窗错位(W4)。若不修,启用 Governance/Evolution 会带病上线。故设本容器集中修复并逐项加回归锁定,解除 §8.6 启用禁令。

## What Changes

- **发布道铁律(E1/E2)**:refine rollback 目标限定为发布历史中曾 `Stage=active` 的 bundle(堵被拒 draft 经 rollback 绕过发布道直接激活);慢道 `approveGate` 为 nil 且未显式 `SkipApprovalGate` 时默认 **reject**(堵审批门空转致 protected 提示词零审批激活)。
- **可靠性收敛(W1/W4)**:MemSpill.Replay 重放前 `GetEvent` 预检幂等(假阴性不再撞 already-exists 守卫致 spill 永久滞留);ActivationLog 记录 bundle 激活时刻,后验评估以激活时刻为窗口起点(修 CanaryHold=0 时固定回看窗全是旧数据致 judge 无判别力)。
- **治理闭环(W2/W3)**:Approval.Check 未命中时节流重扫 approvals 目录(运行中外部落盘批准可见,不再恒 Hold)+ Gate 暴露 `Approval()`;治理包裹从 `name==Entry` 扩展到**所有 agent** 的 leaf 工具(子 agent exec/save_file/mcp_call 主风险面不再绕闸)+ per-agent 独立 BudgetManager。
- **三轮复验 N1/N2**:发布历史持久化到 `releases.jsonl` + 启动 loadHistory 重放 + `seedActiveBaseline`/`NoteActive` 幂等(修 InitBaseline 崩溃窗口:基线跨重启仍在 wasActive 白名单);`DenialLedger.BindStore` 延迟绑定 + `rc.govLedger` 跨 agent 共享(子 agent 治理审计 durable,非兜底内存)。
- **第四轮复验 §8.1**:治理审计事件补来源 agent 归属(`DenialRecord.AgentName` + `GateDeps.AgentName` + 事件 `metadata["agent"]` + rebuild 回读 + 接线传 agent name),多子 agent 共享 Ledger 时治理事件可按来源区分。
- **Minor 批量(§8.4/§8.9/§8.10)**:预算 `Dir=""` 不落盘 CWD、parentless draft 回退用 prev active、审批过期文件清理、SetActive 磁盘+缓存单锁原子、Ledger.Record 锁内快照 store/pid(消 -race 竞争)、Submit `active==nil` 直接 reject 守卫、删 `BindLedger` 死代码、Approval 注入时钟使节流测试确定性、宣称校准(Decide=预留接口/热切换=存储就绪)等。

## Capabilities

### New Capabilities

<!-- 无。本变更为纯缺陷修复容器,不引入新能力规格。 -->

### Modified Capabilities

<!-- 无 spec delta。所有修复收敛到既有主 specs(governance/evolution/reliability/memory 相关能力)已声明的契约,不改变其 SHALL/MUST 语义,仅修正实现使其符合既有规格。故无 specs/ 目录、归档 --skip-specs。 -->

## Impact

- 代码:`evolution/`(发布道 E1/E2/W4/N1/④⑥)、`agent/governance/`(治理闸 W2/W3/N2/§8.1/①⑤⑦)、`agent/reliability/`(退化 W1)、`memory/`(MemSpill W1)、`tagent.go`(buildAgent 接线:治理包裹扩展、共享 Ledger、agent 归属、预算 Dir 守卫)。
- 测试:每项 Major/Minor 配 fail-before/pass-after 回归;新增 buildAgent 级治理过闸集成测试(governance_wire_test.go)、N1 生命周期回归(Submit→重载→rollback)、Approval 假时钟节流测试。
- 不变量:治理默认关闭(Enabled=false)时全放行、行为逐字节不变;发布道"agent 永无直接激活权"铁律;recall/工具 Declaration 恒定(prefix-cache)。
- 启用禁令(§8.6)解除:W2/W3/N2 修毕 → Governance 可真实部署(子 agent 主风险面审计 durable);E1/E2/W4/N1 修毕 → Evolution 可出实验环境(rollback 到基线跨重启可用)。
- 无新增外部依赖;无数据格式破坏性变更(governance 事件 metadata 增可选 `agent` 键、releases.jsonl 追加式,均向后兼容)。
