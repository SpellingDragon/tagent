# Design: resident-remaining-hardening

## Context

继承 `2026-09-17-resident-readiness-plan` 归档的全部基线：事件级耐久屏障、durable inbox-v1（两轮 cold-eyes 审查闭环）、RuntimeResources 租约、热更身份绑定、HTTP 控制面硬化。本 change 只做增量，不改已闭环语义。

## Goals / Non-Goals

**Goals**:
- 压缩回收率从 ~3% 提升至 >30%（远端实测验收）
- 结算风暴源头限流（事件数 N→1）
- 发布前置证据链（守卫/基准/E2E/fuzz）与文档完备
- Major 5 CheckRedirect 落地（消除文档化降级）

**Non-Goals**:
- 不重做 WP0–WP4 已闭环项
- 7.7/7.8/7.9 授权类任务保持 BLOCKED（显式标注，不伪造证据）
- 不引入 ANN/BM25/数据库替换（6.6 既有约束）

## Decisions

### D1：批量退役汇总走 record-only 落链 + 单条汇总事件（M-1 教训的结构延续）

第一轮审查已证明：per-task 事实（registry 重建归并依赖 settled 事件）不能省。因此汇总方案为：
- task.TaskManager 增加 `OnBatchRetire([]RetiredReceipt)` 可选回调；RetireOrphans/reconcileZombies 循环收集 (task, sig) 列表，循环结束统一回调一次（回调为 nil 时保持逐个 onSettle 的旧行为）。
- agent 层注册 OnBatchRetire：per-task 的 settle 记录仍逐条**record-only 落链**（task_record_sink 同款机制，供 RebuildTaskRegistry 归并），bus 只发布**一条**汇总 external_input（N 行票据摘要，`---` 分隔，复用 newTaskSettledEvent 的行格式）。
- Rejected alternative：直接合并事实事件（破坏 registry 归并）；纯投影折叠不改源头（事件量不变，溯源/轨迹侧仍被风暴污染）。

### D2：settled 类 external_input 的票据化折叠

- 压缩折叠判定新增一类：Content 以 `[task settled]` 前缀（或 Metadata 子型标记）的 external_input ref，无论段龄一律可折叠。
- 折叠形态：多条连续 settled ref 合并为一张汇总卡片，每条一行 `✗/✓ [evt_key] 摘要行`（与现卡片行同构），原文依赖事实链 recall——不产生新票据文件（settled 正文已在链上）。
- 前置：D1 落地后 settled 事件数已被限流，折叠兜底存量风暴。

### D3：CheckRedirect 逐跳校验

- LLM client 构造处（openai.WithHTTPClient 或等价注入）安装 `CheckRedirect`：每一跳目标 host 必须 ∈ endpoint allowlist；allowlist 空（未启用重定向）时任何跳转直接拒绝。
- 配置面复用 rl.SetEndpointPolicy 的 allowlist——rl 包新增导出 helper `EndpointRedirectPolicy(allowlist)` 返回 http.Client 可用的 CheckRedirect func，由宿主装配，避免 rl 直接依赖 provider SDK。

### D4：授权类任务显式 BLOCKED 协议

7.7/7.8/7.9 与 5.7 在任务清单中标注 `[BLOCKED: needs-authorization]`；汇总报告（7.10）必须把它们列为「未验」，不得计入通过项。

## Risks / Trade-offs

- D1 改 task 包回调协议 → 用可选回调保持向后兼容；既有 per-task 测试不动。
- D2 改折叠判定 → settled 消息不再全文驻留，模型只能看到票据行；语义由 recall 兜底——折叠卡片必须带「可 recall」提示文案。
- D3 若 provider SDK 不暴露 http.Client 注入 → 降级为在 swappable 层包装 transport；先侦察再实施。

## Open Questions

- 估值器（chars/token 2.5–2.6）是否立项：待 D2 上线后由远端 24h 基线数据决定（本 change 内只出测量结论）。
