# Design

## D1 外迁落点按依赖方向定，不按"感觉像谁"定

| 件 | 对外依赖 | 落点 | 方向核验 |
|---|---|---|---|
| `resources.go` | 仅 memory | `agent/resources` | root→agent→memory ✓；agent 不引用它（现居根包）⇒ 无环 |
| `asset_drift.go` | 仅 evolution | `evolution` | root→evolution ✓；已 grep 证 evolution 零反向引用其符号 |
| `consolidation_hint.go` | 仅 event | `memory` | root→memory、memory→event（纯叶子）✓；调用面仅 `build_agent.go` 4 处（构造与传参），import 即可 |

落点若与语义直觉冲突（如 resources 语义上像"治理"），以依赖方向与既有 spec 归属为准（`resource-ownership`、`cognitive-asset-guard` 均非根包专属）。

## D2 B 组的裁决修订：从"整文件留根"到"簿记随机制、发布动作留根"

D9 当时的否决理由（12 处依赖根包 Config）随 C1 消失。D10 原文即预留了本方向："org_hotreload 的机制部分（代数、指纹、可达集）可迁 agent/org，只留发布动作在根"。本变更是执行该既定方向，不是新裁决。`orgCoordinator` 及其 swap/publish/record*/告警留根——「唯一编排发布权」法条的物理位置不动。

`Org*` 五类型迁走后根包保留**类型别名**（`config_alias.go` 同手法）：`tagent.OrgStatus` 等 API 源码级不变，根包非测试调用点 8 处无需改写。

## D3 C 组的判据只有编译，静态扫描已证不可靠

上次静态扫描把 `acquire`/`fingerprint`/`loop`/`stopped` 等字段名/局部名误判为未导出顶层依赖，得出 4 可搬/21 不可搬的错误分法。本轮判定协议：对每个候选测试文件构造 `tests/` 版本（`package tagent_test` + `import tagent` + 符号加限定），`go vet ./tests/` 编译通过即判可搬；失败即回退并在判定表记录首个未解析符号。判定表入档，作为将来"要不要扩 testing.go 导出面"的裁决依据。

## D4 不做的事（边界）

- 不合并同位门要求分域的测试文件（合规形状不是乱）；
- 不为搬测试而扩公共 API——`testing.go` 导出面是否扩是逐条过 AGENTS.md 导出必要性判据的裁决项，默认不动；
- 不动组合根四件套（tagent/build_agent/wiring/builtin+registry）与 `partition_collision.go`（runtimeConfig 注册表）、`config_alias.go`（API 面）。
