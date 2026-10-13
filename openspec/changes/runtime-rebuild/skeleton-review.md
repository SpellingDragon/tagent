# 00.P0.3 骨架评审门记录（2026-10-13）

> 门定义（expectations §8）：CodeReview 子代理独立评审 ＋ 主编排者亲自复核 ＋ **用户确认**——三道齐备后方可勾选 00.P0.3 并解锁阶段二（65 项 NN.M）。
> 当前状态：第一、二道已完成；**第三道（用户确认）待决**。

## 第一道：独立 CodeReview（已完成）

**总评：有条件通过。无 P1 阻塞项。**

七项核查结论：零逻辑 ✓（35 stub 统一哨兵、2 纯比较谓词、零 panic）；签名-合同一致 ✓（event 常量集 §1.1 逐值一致、task 三层、state 三对象、model 真别名+五终局、plugin 六相、memory/query/telemetry/rl 均对齐）；Godoc/锚点 ✓（17 文件 30+ 锚点全部真实存在）；结构纪律 ✓（≤500 行、无空壳）；依赖方向 ✓（policy+语义双验、无影子类型/镜像协议）；完整性 ✓（23 单承诺文件全部落盘）。

## 第二道：主编排者复核与 P2 处置（已完成）

| 项 | 裁决与处置 | 验证 |
|---|---|---|
| P2-1 telemetry 锚点语义不同源 | **改锚**至 `rl-architecture.md#trajectory-capture`（与 recorder.go 同源） | comment_policy 0 findings |
| P2-2 memory.RecallResult 无消费者 | **接线**：`OriginalStore.Get` 改返回 `RecallResult`（具名分类获得真实消费者） | build/vet/policy 全 0（含 skeleton_assert 同步） |
| P2-3 D9 断言口径两包矛盾 | **修订 D9**：两种形态均认可（占位实现断言 ∥ 方法形状断言），不为断言预造类型 | design.md D9 已更新 |
| P2-4 plugin 接口两处合同缺口 | **留痕**：contracts §7 补登（参数变换 hook 归属待裁；胖接口形状待 01.4/02.6 与原生对齐冻结） | contracts §7 已补两条 |

P3 项（哨兵两种形式、断言位置、workspace Default* 常量）随记录归档不单独设门。

## 阶段二签名变更风险预告（评审门归档，作 00.3/G1 定向核对表）

1. **context 普遍缺失**（约 28 方法无 ctx，与有 ctx 的包两套风格并存）——阶段二统一时横改约 10 包
2. mailbox/通知无载荷引用（AcceptNotice 只传身份，消费侧还原输入时必加字段）
3. **plugin.Plugin 胖接口**最可能整接口重造（与原生 plugin.Manager 对齐待 01.4/02.6）
4. Deps 注入面不完整（无 Store/Manager/memory/telemetry/plugin 管线注入位）
5. AdmissionReceipt 无 fresh/dedup 区分
6. Borrow 无释放/句柄面（已登记 contracts §7）
7. NoticeKind/DetailState/Advancer 词表未枚举

## 风险修复轮（2026-10-13，用户裁定"值得都修"后执行）

七项风险全部修复，全套验证复绿（build/vet/gofmt/comment_policy 16包/architecture implementation/plan/strict 全 0）：

| # | 风险 | 修复 |
|---|---|---|
| 1 | ctx 缺失两套风格 | 全部无 ctx 方法补 `ctx context.Context` 首参（state/task/memory/workspace/config/search/tool 适配/tests/Runner），断言同步 |
| 2 | plugin 胖接口 | 重构为六窄接口（Enricher/RiskAssessor/ViewSelector/ExternalizePolicy/PostCommitObserver/FinalPresenter）+ `Plugin` 降为注册单元（Name+Phase）；**Enricher 即 §2 参数变换承载**（§7 缺口①闭合） |
| 3 | 通知无载荷引用 | `Notice.Event event.EventID`（指向已提交事实；身份仍由 TaskID+Round+Kind+Seq） |
| 4 | Deps 注入面不完整 | +Memory(memory.OriginalStore)/Telemetry(telemetry.Observer)/Plugins([]plugin.Plugin)；Models 工厂不预建（签名 02.4 冻结） |
| 5 | Receipt 无 fresh/dedup | `AdmissionKind`（Fresh/Dedup/Rejected）入 AdmissionReceipt |
| 6 | Borrow 无释放面 | `Borrow` 返回 `*BindingHandle`（持绑定+Release(ctx)，释放以实际使用结束为准） |
| 7 | 词表未枚举 | `NoticeKind`（accepted/progress/settled/failed/cancelled）+ `DetailState`（active/blocked/retired）常量落盘 |

遗留观察（不阻塞）：RiskAssessor.AssessRisk 按裁决签名无 error 返回（阶段二复核）；ViewSelector 返回 []string（具体类型 03.4 冻结）；Borrow 释放面已闭合（§7 该条可销）。

## 第三道：用户确认

**待用户裁决**：是否接受本评审结果、勾选 00.P0.3、解锁阶段二 65 项实施。

- 确认后：G1 口径随之达成（全部域 P0 done + 本门通过）——G1 通过=骨架编译断言绿+评审记录，不宣称产品行为通过。
- 风险预告 1-7 不阻塞解锁（均为阶段二实施期对照表），但 #3（plugin 接口）建议在 02.6 开工前优先核。
