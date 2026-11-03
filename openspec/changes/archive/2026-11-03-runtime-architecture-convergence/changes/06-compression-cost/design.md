# 压缩与成本技术决策

## D1 两个门而非两个阈值

容量门仅为 `Volume > Threshold`。执行门为“本轮确有新候选产品”。Volume 未超阈必须提前返回；超阈但无候选为 no-op，不写 request/product，不重写投影。被否：Material>Threshold，因为它会漏掉“大不可折骨架+小可折过程”。

## D2 公共读面不扩张

Material 从未发布，直接删除。Triggered 表示本轮确有实际折叠工作；Volume/Threshold/Products 足以诊断。不得为性能测量再增公共状态字段。

## D3 请求事实是执行前因

commit request 失败立即返回错误，不 Record、不 Execute。消费者不得从内存 pending 创造缺少链上 request 的 product。正常业务事实不回滚，但观察错误必须向 pipeline 返回。

## D4 测量与覆盖分工

benchmark 回答单位成本、分配与尾延迟；长测回答四会话、重启、故障与组合恢复。4096 若只用于稳态成本则不入长测；若证明独特恢复症状，再通过独立 spec delta 加入。

## D5 现行规范

以 request/product、PlanByAge/CompressionConsumer 为现行实现；legacy ContextCompressor/buildRetainedRefs 等要求由 04 域迁移或删除。实际模型请求预算仍由既有 AssembleBudgetPrefix 负责，不把投影阈值等同于完整请求预算。

## 风险与回退

- 空折保护改变历史事实数量 → 以“未真折叠不写”主 spec 为准，记录预期差异。
- 候选扫描优化误伤幂等 → Plan+Apply 与 FoldByAge 对同一输入必须等价。
- 既有工作树包含用户修改 → 逐 hunk 重定基，不整文件回退；无收益改动保留证据后由编排者决定是否撤回。
