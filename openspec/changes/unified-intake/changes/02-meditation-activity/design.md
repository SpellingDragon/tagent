# 技术要点与文件清单

- 触碰集：`features/evolution/meditation/driver.go`（活跃闸判据）、`features/evolution/meditation/judgment.go`（判据合流）、`assembly/run.go`（装配注入既有 `*state.Occupancy`）、`tests/integration/evolution/meditation_activity_test.go`（红锚+场景）、`openspec/specs/meditation-idle-gating/spec.md`（经本 change MODIFIED delta）、`docs/wiki/evolution/evolution-architecture.md`（锚页随迁 evidence-window/verdict-states 不动，补水门语义）。
- 数据源：事实链 `trigger_source`（novelty 同源）；席位占用=（01 域注入的只读窄面）。
- 风险与回退：闸参数零值=现行为；让位/水位测试面既有全绿兜底。
