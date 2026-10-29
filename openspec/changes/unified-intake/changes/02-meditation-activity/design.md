# 技术要点与文件清单

- 触碰集：`features/evolution/meditation/{driver,judgment,reflection}.go`（reflection.go 仅 Tick 内判据①段：novelty 之后、rhythm 之前）、`assembly/host.go`（仅 NewDriver 构造段 401-407：注入 Occupancy+InstanceID）、4 个既有测试调用点、`tests/integration/evolution/meditation_activity_test.go`（红锚+场景）、`openspec/.../specs/meditation-idle-gating/spec.md`（delta）、`docs/wiki/evolution/evolution-architecture.md`（锚页随迁）。
- 数据源：既有 novelty 扫描（`ScanVerdict.Recent`）；决策在途=既有 `*state.Occupancy` 对象。
- 风险与回退：闸参数零值=现行为；让位/水位测试面既有全绿兜底。
