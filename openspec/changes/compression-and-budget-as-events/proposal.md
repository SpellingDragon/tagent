# 压缩与预算耗尽：判定成事实，执行归消费

## Why

压缩与收尾轮今天都是「机制内嵌」：`AgeFoldPlugin.ObserveCommit` 自己判定、自己折叠、只把 `compress.request` 当记录，产物仍是投影现算条目；收尾轮由 `closingRound` 在 loop 内纯内存判定，链上没有「预算耗尽」这件事。两者都挂在机制内部、不在三个设计挂点上，于是不可分别重放、不可独立替换，重启重折也让产物身份不稳定。

## What Changes

- **压缩三段式落地**（`compress_request.go` 已写明的契约）：producer 只判定并提交 `compress.request`；新增 `compress.product` 事实承载产物（键、覆盖区间、正文、产出者）；独立 consumer 读 request 执行折叠并提交 product；投影从 product 事实恢复，重启不再重折。
- **预算耗尽事件化**：新增 `final_round.request` 事实（range、原因、判定输入）；判定由插件在每次提交后产出；loop 在决策边界**读**该事实决定本轮是否收尾，`closingRound` 不再自算。

## Impact

判定=事件 x、执行=消费 g(x)、机制=插件 f(x)：两类能力可独立替换与重放。fold 产物身份落链后稳定，此前放宽的 smoke 断言恢复严格。
