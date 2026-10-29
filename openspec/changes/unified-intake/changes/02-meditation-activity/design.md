# 技术要点与文件清单

- 触碰集：`features/evolution/meditation/driver.go`（活跃闸判据）、`features/evolution/meditation/judgment.go`（判据合流）、`assembly/run.go`（注入既有 `*state.Occupancy`——与 D2 分段）、`tests/integration/evolution/meditation_activity_test.go`（红锚+场景）、`openspec/specs/meditation-idle-gating/spec.md`（经本 change MODIFIED delta）、`docs/wiki/evolution/evolution-architecture.md`（锚页随迁 evidence-window/verdict-states 不动，补水门语义）。
- 数据源：既有 novelty 扫描（`ScanVerdict.Recent`）；决策在途=既有 `*state.Occupancy` 对象。
- 风险与回退：闸参数零值=现行为；让位/水位测试面既有全绿兜底。
