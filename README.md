# tagent

事件驱动的 agent 运行时：事件进入总线，由任务、模型与工具协作产出结果。Go 单仓（module `github.com/SpellingDragon/tagent`，稳定命名空间）；微信宿主是独立 module 的宿主示例，不反向依赖宿主代码。

## 项目定位

术语先查 [`docs/GLOSSARY.md`](docs/GLOSSARY.md)：一页把机制名（冥想／转世／结算／血统／义务／策展／退位）与事实九族对成通用词，本文不重复定义。

- **事件是一等输入**：用户消息、任务结算、系统告警等都以事件进入，消费壳按事件类型路由。
- **状态、历史、义务各有其主**：`state` 持生效绑定与领域现场，`memory` 持原文与投影写权威，`task` 持转移与通知义务，互不代管。
- **组合根只装配**：根包 `tagent` 是把组织声明变成活动运行时的唯一装配点（`Assemble`），机制实现位于各领域包，根包不留第二份运行现场。
- **运行时而非脚手架**：持久提交、跨重启恢复、投递回执、治理闸与观测是运行时组件。

## 架构速览

重构按 10 个交付领域（`00-foundation` … `09-acceptance`，见[重构台账](openspec/changes/archive/2026-10-18-runtime-rebuild/design.md)）推进，代码面收敛为六层：

| 层 | 目录 | 职责 |
|---|---|---|
| 词汇 | `api/event/` `api/model/` `api/config/` `api/plugin/` `api/view/` | 事件契约与元数据、模型抽象、声明式配置装载（严格解码）、插件阶段、呈现词汇 |
| 核心 | `runtime/task/` `runtime/state/` `runtime/agent/` `memory/` | 任务生命周期、绑定与现场、事件循环、原文/投影与召回 |
| 供面 | `tool/`（`exec/` `file/` `knowledge/` `mcp/` `recall/` `spec/` `subagent/`）、`rl/`、`evolution/`（`governance/` `meditation/`）、`telemetry/`、`memory/query/` | 工具与外部执行、训练数据侧、自进化、观测实现、只读查询 |
| 组合根 | 根包（11 文件：`tagent.go` `run*.go` `prepare.go` `read.go` `host_face.go` `skeleton_assert.go`） | 装配、公共入口、诊断注入点；`opts.go` 的装配选项并入 `run.go` |
| 宿主壳 | `cli/` | 命令面（`invoke`/`query`/`start`）与宿主转换面（`HostBridge`/回执/审批）；位于组合根之上（`cli`→根），运行时不反向依赖 |
| 宿主 | `examples/wechat-bot/`（独立 module） | 微信宿主：收发、去重、投递回执；经 `replace` 指向本仓 |

`examples/contract/` 为各领域公共 API 的编译期契约示例。

## 快速开始

需要 Go 1.24.1 及以上（见 `go.mod`）。

```bash
go build ./...                          # 构建全部包（根 module）
go test ./tests/integration/...         # 集成/契约测试
bash scripts/lint.sh                    # 仓库门禁：格式、构建、vet、注释棘轮、文档引用、生成物同步
```

`docs/api/` 是生成物：改完源码注释后运行 `bash scripts/gen_godoc.sh` 重新生成，不要手改。

## 文档导航

| 入口 | 内容 |
|---|---|
| [`docs/GLOSSARY.md`](docs/GLOSSARY.md) | **学习入口**：术语一行一对照（一句话定义＋领域类比＋在哪看） |
| [`docs/api/index.md`](docs/api/index.md) | **API 权威**：由源码注释生成，`gen_godoc --check` 保证与码面同步 |
| [`docs/wiki/README.md`](docs/wiki/README.md) | 历史设计记录（historical-verbatim）：机制背景，非当期码面事实 |
| [`docs/notes/risk-remediation-plan.md`](docs/notes/risk-remediation-plan.md) | 当期风险处置计划（分片、验收通则） |
| [`openspec/changes/archive/`](openspec/changes/archive/) | 实施台账：变更的 proposal/design/tasks 与波次记录 |

## 版本说明

- 当前版本 **v0.3.0**，变更记录与升级注意事项见 `CHANGELOG.md`。
- 配置装载是**严格 YAML/JSON**：未知键拒绝启动而非静默忽略（`api/config` 的 `unknown field` 判定）；升级前先核对配置键。

## 依赖注记

`trpc-agent-go` 目前指向 fork：

```
replace trpc.group/trpc-go/trpc-agent-go => github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1
```

回正条件：上游 PR [#2637](https://github.com/trpc-group/trpc-agent-go/pull/2637) 合并后移除该 `replace`（`go.mod` 内有 `TODO(upstream)` 标记）。

## 许可证

Apache-2.0，见 [`LICENSE`](LICENSE)。
