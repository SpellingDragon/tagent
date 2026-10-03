# Proposal: 被钉跳的验收必须锚定被钉跳本身

## Why

`TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget`（`cross_generation_test.go:1677`）在 `GOMAXPROCS=1` 下间歇失败（实测 3/25，**修复前的 `40dd3ad` 同样 3/25 ⇒ 与本会话两次变更无关，是既有缺陷**），失败形态是 0.06s 的断言红而非超时：

```
Error: "[evt_…|action_command] \"served:SUB-C-PROMPT-G2\"" does not contain "\"served:SUB-C-PROMPT\""
Messages: 被钉跳的回执必须是 G1 之 C 的回答（派生前继承发起调用租约）
```

机制与刚归档的回滚用例同源：`CheckOrgReload()` 发布新代时**必然抬起一个通知回合**（`execution-generations.md` §四），它会经 `a→b→c` 在**新代面**上产出一条 B 的答案；而用例用 `answers[hopsBefore]` 取"发布后第一条新答案"来代表被钉跳。两条答案的到达顺序由调度决定——P=1 下取到的常是告警轮那条，于是**产品语义没被违反（被钉跳确实回了 G1）却报红**；反过来，若告警轮那条先达标，`NotContains(G2)` 的守卫也就形同虚设。

## What Changes

- 把断言从"发布后第一条新答案"改为**锚定被钉跳自身**：以发布前被 park 的那一轮为唯一来源识别其答案（按调用身份/回合关联，而非按数组下标），并在锚定前**要求被钉跳的答案确已出现**。
- 只改测试编排与谓词；产品实现与 20s 预算不动。

## Impact

- Affected specs: `config-hot-reload`（MODIFIED：执行代绑定完整性 +1 scenario「被钉跳的验收锚定被钉跳本身」）
- Affected code: `cross_generation_test.go`（该用例及其 `delegModel` 观测辅助）
- 顺带解锁一项 CI 固化：三枚 P=1  offenders 清完后可评估把 `GOMAXPROCS=1 go test . -short` 纳入门禁
