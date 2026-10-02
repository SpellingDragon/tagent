# Tasks

## 1. 先证缺陷（fail-before）

- [x] 1.1 归因修正后的用例即 fail-before：在当前实现上本地确定性复现 `--- FAIL ... (20.03s) Condition never satisfied`（与 CI 签名一致）。**实现方式改为 `m.noDelegation()`**——"等告警轮跑完"的记账静默经实测不可靠（通知轮异步抬起，`Obligations` 在它开始前就是 0），关掉委派才是结构性消除干扰源
- [x] 1.2 `newBefore` 移到 relaunch **之前**采样；`noDelegation` 使 SUB-C-G2 只能由重入产生（`startReentryChain` 未改：它服务 6 条用例，改动面按最小化收口）

## 2. 产品修复

- [x] 2.1 `AgentToolWrapper.armDeclaredCall(ctx)`：arms 目标声明代，返回 (ctx, release, err)；未接线时原样透传
- [x] 2.2 `Call` 改走 helper（行为等价，错误文案不变）
- [x] 2.3 `subagentRelaunchClosure` 与 `subagentResumeClosure` 在 producers 内 arms，声明代不可用时在模型调用之前具名拒绝并释放属主租约

## 3. 验证与 CI 固化

- [x] 3.1 红(20.03s) → 绿(0.42s)，反证被等待事件从不依赖任何 tick/TTL；根包 plain 58.3s / race 74.6s、`./agent/` plain 44.5s / race 83.7s、`./tests/` 全绿；lint ok（含 doc-refs/gen_godoc --check/proc-refs，注释门禁四度逮住本变更自写的 sloppy：`不再` audit-marker、doc-not-name-prefixed、free-standing、gofmt 规范化导致的 wrapped bullet 触发 doc-not-brief）
- [ ] 3.2 **未达成，另案**：`GOMAXPROCS=1` 下 `TestRollbackOfHotAddNumericWithInFlightTurn` 仍烧满 20s（回滚 × 在途门控 × 通知轮，属另一条链路，修复前即如此，CI 的 2 核今日不复现）。不混改本变更，单独立项
- [ ] 3.3 lint + gen_godoc --check + openspec strict 全绿；推送并确认 CI 四 job 绿
