# 设计

## 事件

### compress.product

- `Request CompressRequest.ID`：产出它的判定身份（一条 request 至多一条 product）。
- `Key EventID`：产物在投影中的负键（内容寻址 `-hist-<hex>` / `-seg-<hex>`），与今天一致。
- `Kind string`：`segment_fold` / `history_summary`。
- `Covered []EventID`：被折叠覆盖的条目键，按原序。
- `Text string`：产物正文（history summary 的文本；segment fold 的摘要与关联 id）。
- `Producer string`：执行压缩的机制名。
- `At time.Time`。
- `ID EventID`：内容寻址（request + key），重复提交同一条。

### final_round.request

- `Range event.InvocationID`：判定的 range。
- `Reason string`：`rounds` / `budget`。
- `Rounds int`、`Elapsed time.Duration`：判定输入，供消费者复核。
- `ID EventID`：内容寻址（range + reason），一个 range 至多一条。
- `At time.Time`。

## 生产与消费

- **压缩**：`AgeFoldPlanner`（PostCommit 观察者）评估体积与龄段，只提交 `compress.request`；`CompressionConsumer`（PostCommit 观察者）收到 `compress.request` 后读材料、执行折叠、提交 `compress.product`。两者互不知道对方存在，只通过事实相接。
- **预算**：`FinalRoundPlanner`（PostCommit 观察者）按 `FinalRoundPolicy` 与已用轮次/时长提交 `final_round.request`；loop 在每次决策前读本 range 的 request，读到即本轮为收尾轮（四条红线不变）。

## 恢复

`seedReplay` 读到 `compress.product` 时把产物作为合成条目加入投影（键=product.Key，正文=product.Text），不再调用 `FoldByAge` 重折；读不到 product 的 `compress.request` 视为未执行的判定，保持投影为原始条目。

## 呈现

请求侧投影呈现读条目（含 product 合成条目），与今天一致；`renderProjection` 不变。

## 兼容

- `compress.request` 契约不变；重复判定仍同身份。
- 未接 consumer 的宿主：判定照旧提交，产物不生成，投影退回现算——行为等价于今天（由集成测试守住）。
- 收尾轮判定读不到 request 时（未接 planner），loop 退回本地快判以保留无插件的可用性（与今日一致），并把该快判提交为 `final_round.request`，保证事实不缺。
