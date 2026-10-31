# Design: 01-attention-primitive

## 技术要点

- 类型与键：`api/event/types.go` 三枚 `TypeAttention*`；`api/event/meta.go` 五枚
  `MetaKeyAttention*`（kind/target/lineage/expires_ms/clearance），`skeleton_assert.go`
  与 `docs/FACT-VOCABULARY.md` 同步登记（F1 的契约面在这里一次做齐）。
- 提交面：`runtime/task/attention.go` 提供 `Commit(ctx, AttentionFact)`，内部走
  `Run` 既有事实提交；question 身份 = 目标 + lineage 的稳定散列（同身份幂等靠
  既有"同键拒二落"语义，不新造去重表）。
- 读回面：`Present(ctx, filter)` 在场集；过期在读侧按注入时钟判定（测试注入
  clock，沿用 approval 的 `WithApprovalClock` 先例）；clearance 只影响在场读数。
- 清除规则：引擎是纯函数（question/answer/clearance 三集合 → 在场集），无
  goroutine、无定时器——expiry 不需要后台扫描，读时判定。

## 文件清单（以启动门时 next 为准，派发单冻结）

- `api/event/types.go`、`api/event/meta.go`、`api/event/skeleton_assert.go`
- `runtime/task/attention.go`（新）
- `runtime/task/attention_test.go`（新，正/负锚）
- `tests/integration/host/attention_lifecycle_test.go`（新，两条症状红锚）
- `docs/FACT-VOCABULARY.md`（登记行）

## 风险与回退

- 幂等依赖既有同键拒二落：若启动门后的提交面语义变化，改用显式身份键前缀
  （`attention:` + 散列），仍不新造存储。
- 读侧判过期要求 `expires_ms` 落链为绝对毫秒（与 approval 的 ExpiresMs 同口径），
  避免时钟注入歧义。
- 回退 = 删除新增类型与包，事实链无残留影响（只增事实未删任何既有面）。
