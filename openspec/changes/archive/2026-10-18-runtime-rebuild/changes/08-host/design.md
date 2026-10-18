# 08-host — SDK、CLI 与微信（design）

> 本设计只写「怎么做」；不改一级 design、主 spec、prototype/wiki 与既有源码。产品实现为**未来授权**。

## Context

- 现现场（实测本仓）：module 根 `github.com/SpellingDragon/tagent`（`go.mod`，go 1.24.1），根目录**无** `.go`。**清理前历史**：公共 `package tagent` 曾落在 `tagent/` 子目录（`tagent/assembly.go`、`tagent/runtime.go`），且 import 已删除包（`tagent/agent`、`tagent/event`、`tagent/memory`…），不可编译；该目录（连同 `model/`、`state/`、`tests/`）已随 **2026-10-13 用户授权清理批次整目录删除**（见 `baseline.md` §2.1.1）。清理前历史 HEAD `56c58eb`，**当前新基线 HEAD `a9381ce`**。
- 微信独立 module：`examples/wechat-bot/`，module 名 `wechat-bot`，`replace github.com/SpellingDragon/tagent => ../..`；含 `main.go`(944)、`file_delivery.go`、`send_tool.go`、`delivery_receipt.go`、`dedup.go`、`file_intake.go`、`reincarnation_notice.go`、`system_alert.go` 及各自测试。
- `examples/wechat-bot/main.go` import `github.com/SpellingDragon/tagent`（根包）与 `tagent/agent/governance`、`tagent/event`、`tagent/memory/engine`、`tagent/rl` 等——**均属待迁移**路径（上述被 import 的包已随清理删除），当前不可编译。
- `examples/wechat-bot/` 自带 `AGENTS.md`（运行时代理须知：能力发现优先、实证优先「改前 read、改后 read 回读确认」、**路径陷阱**——主树绝对路径与 wechat-bot 副本不是同一份文件、高并发编排纪律）。宿主迁移（08.2/08.3/08.5）SHALL **先读该 `AGENTS.md` 与 `main.go`/`config`/`file_delivery.go` 实际源码**再动手；MUST NOT 凭记忆补全符号/接口名，MUST NOT 靠根 module `./...` 绿灯推断 bot 通过（根 `./...` 不含独立 module）。
- 上级计划 §3：公共入口放 module 根 `package tagent`，**不再**同时建根门面与 `tagent/` 同名第二层门面。

## Goals / Non-Goals

**Goals**

- 在根包装配入口（`02.5` 建立）之上完善公开 SDK/CLI，不重建运行时。
- 微信独立 module 在新内核上迁移并单独验收（用户/session 映射、入站去重、send/审批回执）。
- 文件/媒体边界：类型识别、媒体发送、大结果副本、权限与失败回执。
- 诊断可见性：Query/反思/训练诊断开关就绪；宿主启停重启可验证。

**Non-Goals**

- 不建第二套组合根/门面；不实现 state/agent/task/memory 机制；不恢复旧 API 迁就 `main`。
- 不执行真实微信付费收发（属 08.5/09.5 资源门，且需明确发送授权）；不安装依赖、不改全局配置、不提交/归档。

## Decisions

**D08-1 公共入口归 module 根，08 只在其上完善。**
- 公共 `package tagent` = `github.com/SpellingDragon/tagent`（module 根），是唯一公共门面；MUST NOT 同时存在 `tagent/tagent` 第二层同名门面。`02.5` 已用"根包最小公共装配入口"驱动 Agent；`08.1` 只在该入口上加公开 SDK/CLI，**不重复建立运行时**（不另建 org/state/task 组合）。
- 集成测试（`tests/integration/host`）SHALL 经根包公开面驱动，**MUST NOT import `prototype`**（`prototype/` 是历史骨架，非生产入口）。
- 被否方案：在 `tagent/` 保留嵌套门面并再建根门面——与"不再同时建立根门面和同名第二层门面"冲突。

**D08-2 两套独立验收面，不互相顶替。**
- 根 module 与微信独立 module 分别 build/vet/test：根用 `go build/vet/test ./...`；微信用 `go -C examples/wechat-bot build/vet/test ./...`。**MUST NOT** 以根 `./...` 绿灯推断 bot 通过（根 `./...` 不覆盖独立 module）。
- 测试源进入触碰集：共享 `tests/integration/host` 包的编译读包含该包全部测试文件。

**D08-3 `main` 旧 imports 待迁，不直接恢复 API。**
- 迁移方向（未来实施，本域只登记）：`github.com/SpellingDragon/tagent` → 新公共入口；`tagent/agent/governance`、`tagent/event`、`tagent/memory/engine`、`tagent/rl` → 新内核对应域（`config/state/agent/task/memory/workspace/plugin/model/telemetry/rl/evolution`）的实际落点，**按新设计**接入，不因 `main` 引用过就复活旧符号/旧包路径。
- 迁移以"宿主消费同一组合根"为验收：宿主不得绕过公共入口直连内部机制。
- 被否方案：为让 `main.go` 编译而恢复被删包/旧签名——计划 §1 明确"新架构不是恢复已删除代码"。

**D08-4 文件/媒体边界复用既有规则 + 复用外置/副本。**
- `ExtractFilePaths` 六条识别规则（本地路径形态、排除 URL、须带扩展名、须存在且普通文件、拒可执行、去重保序）与 `FileSender` 窄接口语义**继承不变**；媒体按扩展名选择 `SendImageFromPath`/`SendVoiceFromPath`/`SendVideoFromPath`/`SendFileFromPath`；单文件失败不阻断文本与其他文件。
- **大结果副本** SHALL 走 `03.8` 的外置路径与 `workspace` 唯一副本生命周期（先持久原文、副本可清理、原文有效时可重物化），不另造第二套副本机制。
- 失败/拒绝 SHALL 以具名回执返回（`[send_denied] <具名原因>` 等），回合继续。
- 被否方案：宿主自建大结果落盘/清理——与"workspace 唯一副本"重复。

**D08-5 诊断可见性接实际开关；宿主生命周期可验证。**
- 宿主 SHALL 通过实际开关（配置/env）暴露 Query 只读总图、反思/自演进状态、训练（RL）诊断的可见面；关闭相关子系统（如 OTel）MUST NOT 使 Query 不可用。
- 宿主启动/关闭/重启：HTTP 服务由宿主 `http.Server` 持有，SIGTERM 走 `Shutdown`；重试循环受主 ctx 取消，MUST NOT 在 goroutine 内无控制死循环（与 `ops-deployment-integrity` 一致）。
- 被否方案：为诊断另开无鉴权旁路——与 Query 按权限过滤、`/healthz` 鉴权不豁免冲突。

**D08-6 公众可见文档静态化，凭据不落盘。**
- 面向公众的 Read 类文档/示例尽量静态编写；凭证类探针只报告**存在性**，绝不打印/env 值进文档、日志或测试快照。

## 文件白名单（未来实施候选，本轮不写）

- 未来：module 根公共入口文件、`examples/wechat-bot/**`（含 `main.go` 导入迁移）、`tests/integration/host/**`、`docs/wiki/examples/**`（创建时机由主代理定）。
- 本轮仅 `openspec/changes/runtime-rebuild/changes/08-host/**md`。

## 上游复用矩阵

| 本域产出 | 复用包/符号 | 不采用 | 注意点 |
|---|---|---|---|
| 宿主发送通路 | `github.com/SpellingDragon/wechat-robot-go/wechat`（`*wechat.Bot`，满足 `FileSender`） | 新开第二条到 IM 的路径 | `send`/被动投递共用同一发送实现（single path） |
| 公开模型/工具声明 | `trpc.group/trpc-go/trpc-agent-go` 原生 Model/Tool/ToolSet | 镜像协议/影子结构 | 根包公开面直接暴露原生类型 |
| HTTP 诊断/提交 | 标准库 `net/http`（`http.Server`） | 无控制永久 goroutine | 主 ctx 取消 + SIGTERM `Shutdown` |

## Risks / Trade-offs

- [`main.go` 迁移面大] 944 行、多处旧 import → 迁移按"消费同一组合根"逐点替换，禁止批量恢复旧包；bot 单独门禁兜底。
- [两套 module 混淆] 根 `./...` 不覆盖 bot → 门禁显式列出 `go -C examples/wechat-bot ...`，`09.3` 核对。
- [真实微信不可回滚] 08.5 单次、需明确发送授权 → 未授权即 `BLOCKED`，不得以 mock 冒充。

## Open Questions（待裁，不先设假默认）

- 公开 SDK 的 API 形态（导出符号集合）在设计闸 G1/G3 以真实生产调用链冻结——本文件不擅定签名。
- 08.5 的真实测试接收者与发送授权是否到位——未核验前 08.5 `design-blocked`。
