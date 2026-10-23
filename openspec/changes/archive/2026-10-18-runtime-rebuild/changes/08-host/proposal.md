# 08-host — SDK、CLI 与微信（二级子变更 proposal）

> 层级：隶属于一级变更 `runtime-rebuild`（上级目录 `../../`）。本域是**责任与验收单位**，不是代码包数量，也不是整域派发单位。
> 本轮只产规划文档（md）；产品代码、测试、脚本不在本轮写域。孙任务 `08.1–08.5` 全部未勾选，不得导入旧 change 编号。

## Why

新内核的公共入口（module 根 `package tagent` 最小装配入口）在 `02.5` 首次落地；宿主侧（公开 SDK/CLI、宿主状态与媒体接口、微信独立 module）尚未在其上收口。宿主必须在不重复建立运行时的前提下消费同一组合根，且必须区分**根 module** 与**微信独立 module** 两套验收面——历史事故正是"靠根 `./...` 认为 bot 通过"。

## What Changes

- **08.1 公开 SDK 与 CLI**：在 `02.5` 已使用的**根包装配入口**上完善公开 SDK/CLI，不重复建立运行时；集成测试不 import `prototype`。
- **08.2 微信独立 module 迁移**：`examples/wechat-bot`（module `wechat-bot`）迁移用户/session 映射、入站去重、显式 send 与审批回执；验收以独立 module 命令执行，不用根 `./...` 代替。
- **08.3 文件与媒体边界**：真实文件收发、类型识别、媒体与大结果副本、权限与失败回执。
- **08.4 诊断开关与宿主生命周期**：接 Query/反思/训练诊断实际开关，验证宿主启动/关闭/重启。
- **08.5 真实微信往返**：对已授权测试接收者验证真实文本/工具/文件与跨 session 往返（资源门，单次运行）。

## 边界与依赖（四要素）

### 依赖

- **实现依赖**：`02.5`（根包装配入口）、`02.9`（关闭/排空）、`05.2`（Query 授权过滤/游标）、`06.8`（模型替换）、`07.1`/`07.3`/`07.4`（反思/评估可见面）、`03.8`（大结果外置/副本）、`04.2`/`04.3`（exec/MCP）、`04.4`/`04.5`（A2A/send、治理审批）。
- **接口常数依赖**：公共入口归属 = **module 根 `package tagent`**（`github.com/SpellingDragon/tagent`），**非**嵌套 `tagent/tagent` 第二层门面；微信 module 名 = `wechat-bot`，`replace github.com/SpellingDragon/tagent => ../..`；`send`/`deliver` 内容上界 **8KiB**；`TAGENT_HTTP_PORT` 默认 **8089**。

### 被依赖方（消费者）

- `09.1` golden 场景（宿主生产入口）、`09.3` 完整门禁（含微信独立 module build/vet/short）、`09.5` 真实微信验收。

### 接口面

- 公开 SDK/CLI 入口（构造/启动/关闭/诊断查询）；宿主状态与媒体接口；`send` 工具（宿主侧显式送达）；`/task` 声明意图；投递回执矩阵。

### 禁止事项

- **不重复建立运行时**（复用 `02.5` 根包装配入口，不建第二套组合根/门面）。
- **测试不 import `prototype`**。
- **不得靠根 `./...` 认为 bot 通过**——微信独立 module 必须 `go -C examples/wechat-bot build/vet/test` 单独执行。
- **不直接恢复旧 API 以迁就 `main` 旧 imports**：`examples/wechat-bot/main.go` 现引用 `github.com/SpellingDragon/tagent`（根包）、`tagent/agent/governance`、`tagent/event`、`tagent/memory/engine`、`tagent/rl` 等已移除/待迁移路径，属**待迁移**而非待恢复。
- 公众可见文档尽量**静态**编写，不读取/不打印凭据；不在本域写 state/agent/task/memory 机制。

## Capabilities

### 本域 deltas（`specs/` 内）

- `production-wiring`：**ADDED** — 公开运行时入口（SDK/CLI）在根包装配入口上完善，不重复建立运行时；集成测试不 import the prototype。
- `example-local-file-delivery`：**ADDED** — 媒体与大结果副本、权限与失败回执（不改变既有 `ExtractFilePaths` 六规则语义）。
- `example-rl-visibility`：**ADDED** — 诊断可见性与宿主生命周期（Query/反思/训练诊断开关、启动/关闭/重启）。

### 继承（requirement 语义不变，**不复制归档 history 全套**）

- `example-file-memory-wiring`：localfile memory backend、LocalFileKV、recall 共享 store——**继承**。
- `inbound-intent-declaration`：`/task` 可选声明意图血统、fail-closed 三重校验、白名单语义隔离——**继承**。
- `wechat-host-send-tool`：`send` 工具仅显式声明挂载、参数仅 `{content}`、8KiB 上界、复用唯一发送通路、被动通道零变化——**继承**。
- `delivery-dispatch-receipts`：预期外静默终态必回执、回执走持久总线且血统内部、回执不递归——**继承**。

## Impact

- 未来实现范围：module 根公共入口、`examples/wechat-bot/**`（含 `main.go` 旧 imports 迁移）、宿主启动/关闭/诊断接线、`docs/wiki/examples/**`（由主代理决定创建）。
- 本轮写域仅 `openspec/changes/runtime-rebuild/changes/08-host/**md`；不动产品、不动旧 change、不提交/归档，不执行安装/付费微信。
- 08 域孙任务共 **5** 项（08.1–08.5），全部未勾选。
