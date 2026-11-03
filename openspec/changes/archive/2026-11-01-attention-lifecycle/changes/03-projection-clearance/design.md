# Design: 03-projection-clearance

## 技术要点

- 投影：`query.Inventory` 的 graph 装配新增 attention 维度（question/answer/
  clearance 三集合 → 相位），`ObjectKind` 词汇表与 `DetailState` 加一相
  （AttentionPhase），进 skeleton_assert 与 FACT-VOCABULARY。装配是**读时**的
  （Inventory 快照/Follow 派发时推导），MUST NOT 挂进 per-commit 观察管线——
  防 256 窗口长测实证过的 per-commit 全量重导 O(n²) 形态。
- 工作结束清除：挂在 `Run.Close` 的排空成功路径（与既有 delivery/owed 排空同
  钩子族），批量落 clearance=work-end；失败路径直接返回不清。
- notice expiry：读面在 notice 维装配处补 `expires_ms`（常量缺省 + 配置覆盖），
  不动 `task.Notice` 结构；过期只影响 ObjectAttention 在场，不动 ObjectNotice
  四相读数（两条读数语义分立，沿用 DetailRetired 的先例）。
- summary kind：`deliver_summary` 落链后追加一条 kind=summary 的在场记录
  （同一提交事务面）；"下一轮读到即清"挂在既有请求组装的边界读处（不动组装
  逻辑本身，只在读到后提交清除）。
- 人侧：`approvalGateway.Pending` 展示与 CLI 读 ObjectAttention；
  `Resolve` 写 answer 走 02 域 manager（本域只换读面与入口接线）。

## 文件清单（以启动门时 next 为准，派发单冻结）

- `memory/query/inventory.go`、`snapshot.go`、`skeleton_assert.go`
- `assembly/host.go`（Close 排空钩子）或启动门后对应的排空文件
- `features/evolution/meditation/deliver.go`（投递后在场记录）
- `examples/wechat-bot/wechat_approval.go`（Pending 读面与 answer 接线）
- `tests/integration/host/attention_projection_test.go`（新，正/负锚 + 两症状锚绿态）
- `tests/integration/inspection/attention_inventory_test.go`（新，投影单测）

## 风险与回退

- summary"读到即清"依赖请求组装边界读：若启动门后边界读形态变化，回退为
  "下一轮请求发出即清"（更宽的清除点，语义仍写死）。
- work-end 清除批量落链在 Close 关键路径上：提交失败按"不清"处理并告警，
  绝不半清（要么全部 clearance 落链，要么一条不落）。
- 人侧接线回退：Pending 读面切回 manager 本地读（02 域保留的兼容面），行为与
  基线零 diff。
