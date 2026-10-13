# 01 state-binding — 任务清单（三级孙任务）

> 本文件是二级子变更 `01-state-binding` 的任务清单，层级为三级「孙任务」。全部未勾选（`- [ ]`），本轮**不执行**。
> 编号沿用权威计划 §7 的 `01.1–01.6`，不修补旧编号、不继承旧完成率。每条编号**只出现一次**。
> 每条格式：`- [ ] <编号> <动词开头产出> + 关键约束 —— 前置 + 候选拟写文件（非本轮授权） + 设计闸(G0–G3) + 验证`。
> 验证口径：点名用例真实 pass 才算过；选择器零匹配/Skip/未命中不得因退出码 0 记通过（计划 §7）。并发/状态任务加 `-race`。
> 「前置」含设计/实现依赖，不表示当前授权执行；「候选拟写文件」是未来候选写域，不表示已存在或批准修改。

## 前置与边界

- 实现前置：00.3（G1 共享合同：身份/state 绑定/插件相位/任务提交/通知/关闭/Query 签名冻结）——本域消费，不自行定义。
- 接口设计可先消费00.3审定的常量；01.2/01.3/01.4的实施及验收须等待02.1内存载体和身份实现集成，不能只凭签名解除实现前置。
- 本域与 02 共享 `state`/`config` 写面 → **串行或同一代理**，需在 G3 登记读写冲突；本域不排在顶层 `specs/` 与 `ORCHESTRATION.md` 上。
- 共同设计闸 G0–G3 见 `design.md` §Open Questions；建立这些闸的规划任务不以自身已完成为前置（计划 §7 首句）。

## 任务

- [ ] 01.1 实现 config 严格解析、适用默认值与纯组织声明规范化指纹 —— 前置：00.3 —— 候选拟写文件（非本轮授权）：`config/config.go`（`Declaration`/严格解析）、`config/defaults.go`（默认值）、`config/clone.go`（访问器隔离）、`config/fingerprint.go`（`OrgFingerprint` 规范化）、`tests/integration/config/config_contract_test.go`（`TestConfigurationContract`）—— 设计闸：G0 = 源格式(yaml/json)/未知字段策略/默认值应用点/指纹归一化算法（DESIGN-GATE，未冻结则本 task design-blocked）；G1 = 指纹字段集须与「只含声明面」一致；G2 = 序列「等价内容重载不换代 / 不合法声明具名拒绝」；G3 = 白名单 + 无外部资源 —— 验证：`go test ./tests/integration/config -run '^TestConfigurationContract$' -count=10 -json`

- [ ] 01.2 实现 state 建立 agent/session、可信身份与投递/读取授权（不猜默认来源）—— 前置：02.1（实现前置）—— 候选拟写文件（非本轮授权）：`state/session.go`（`Session`/`Address`）、`state/authority.go`（可信主体与授权）、`tests/integration/state/session_authority_test.go`（`TestSessionAuthority`）—— 设计闸：G0 = 保护面/授权模型；G1 = `Address{AgentID, SessionID}` 与 02.1 事件身份一致、投递/读取授权字段（DESIGN-GATE：未冻结则 design-blocked）；G2 = 序列「显式寻址 / 未授权读取被拒 / session 不可用降级 / 源目标实例身份区分 / 退役实例未交付义务保留」；G3 = 白名单 + 读写冲突（与 01.3/01.4 同 `state`）—— 验证：`go test -race ./tests/integration/state -run '^TestSessionAuthority$' -count=10 -json`

- [ ] 01.3 实现 mailbox 接纳/认领/消费完成分别记账与同 session 执行占用（接纳不阻塞慢模型）—— 前置：01.2、02.1 —— 候选拟写文件（非本轮授权）：`state/mailbox.go`（`acceptSeq` 分配、`claimState` 转换、`consumeReceipt`）、`state/occupancy.go`（同 session 执行占用）、`tests/integration/state/mailbox_consumption_test.go`（`TestMailboxConsumption`）—— 设计闸：G1 = acceptSeq 单一串行化点 + claimState 可见性边界（DESIGN-GATE：与 02.5 批次冻结一致）；G2 = 序列「接纳→认领→完成三态可辨 / 慢模型下接纳与取消不被阻塞 / 崩溃后未完成认领可重试」；G3 = 白名单 + 与 02 的 `state` 写面串行 —— 验证：`go test -race ./tests/integration/state -run '^TestMailboxConsumption$' -count=10 -json`

- [ ] 01.4 实现组合根准备原生模型/工具/插件活绑定、state 持有并按代借出（候选失败释放）—— 前置：01.1、02.1 —— 候选拟写文件（非本轮授权）：`state/binding.go`（`AgentBinding`/`StatePlan`）、`state/lease.go`（`LeaseRef` 登记与逆序释放）、`prepare.go`（module根组合入口，构造原生模型、工具及插件后交state持有）、`tests/integration/state/state_owns_pipeline_test.go`（`TestStateOwnsPipeline`）—— 设计闸：G1 = `AgentBinding.Plugins` 相位枚举/回调类型须与 02.6 冻结一致；`Set` 连接 Close 归资源租约（DESIGN-GATE：未对齐则 design-blocked）；G2 = 序列「候选构造成功→借出同代 / 候选中途失败→逆序释放、共享面不动 / 在途请求持旧代跑完」；G3 = 白名单 + 与 01.3 同 `state` 写面串行 —— 验证：`go test -race ./tests/integration/state -run '^TestStateOwnsPipeline$' -count=10 -json`

- [ ] 01.5 实现组织发布、基线复核、同内容不换代与在途旧绑定/资源退役 —— 前置：01.4 —— 候选拟写文件（非本轮授权）：`state/publish.go`（单一原子发布入口、`Generation`）、`state/baseline.go`（候选基线复核、过期候选丢弃）、`state/retire.go`（在途旧绑定与退役）、`tests/integration/state/generation_publication_test.go`（`TestGenerationPublication`）—— 设计闸：G1 = `Generation` 归属与发布记录独立提交（不与任务终局共记录）；G2 = 序列「基线冲突丢弃过期候选 / 同内容重载不换代 / 发布不提前关闭在途旧资源 / 回滚产生新 generation」；G3 = 白名单 + 与 02 的 `state` 写面串行 —— 验证：`go test -race ./tests/integration/state -run '^TestGenerationPublication$' -count=10 -json`

- [ ] 01.6 实现 prompt/skill 直接热读、结构变更与回滚分路、错误原因链保留 —— 前置：01.5 —— 候选拟写文件（非本轮授权）：`state/filesource.go`（prompt/skill 请求边界固化版本）、`state/channels.go`（三通道矩阵 FP/SRC/FILE + 前置门分路）、`state/errors.go`（错误原因链）、`tests/integration/state/file_and_structural_change_test.go`（`TestFileAndStructuralChange`）—— 设计闸：G1 = 三通道归属枚举与 02.6/07.2 一致；FILE 面直接生效不等 register/评估/前置门；G2 = 序列「文件改动下一请求读新版本且不换代 / 结构改动走前置门 / 结构改动不借文件通道绕门 / 失败保留原因链」；G3 = 白名单 + 复用框架 `skill` 薄胶水（不复制 scanner/解析）—— 验证：`go test -race ./tests/integration/state -run '^TestFileAndStructuralChange$' -count=10 -json`

## 执行期回写（本轮为空）

（执行期新增问题按「场景 → 根源 → 影响面」追加；契约变化先回写 `design.md`，不静默塞进补丁。）
