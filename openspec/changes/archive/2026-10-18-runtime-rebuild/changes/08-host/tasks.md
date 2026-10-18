# 08-host — 任务清单（三级孙任务）

> 本文件是二级子变更 `08-host` 的任务清单（孙任务级）。编号 `08.1–08.5` 全局唯一，**全部未勾选**（`- [ ]`）。不得导入旧 change 编号或旧完成率。
> 命令口径：根 module 用例 `go test ./tests/integration/<域> -run '^<完整测试名>$' -count=10 -json`（并发/状态任务加 `-race`）；**微信独立 module 用例必须用 `go -C examples/wechat-bot test ./...`**，MUST NOT 以根 `./...` 代替。真实资源用例独立 `-count=1`。
> 判定：点名测试不存在、零匹配、子项 Skip 不算通过。测试名为未来将落盘的正式用例名。
> **两阶段**：既有 `08.1–08.5` 属阶段二，统一以骨架评审门 `00.P0.3` 为额外前置（以 `task-graph.json` 为准），不逐行改写各自「前置」文案。

## 前置与边界

- 实现前置：`02.5`（根包装配入口）、`02.9`、`05.2`、`06.8`、`07.1`/`07.3`/`07.4`、`03.8`、`04.2`/`04.3`、`04.4`/`04.5`。
- 写白名单（未来实施）：module 根公共入口文件、`examples/wechat-bot/**`、`tests/integration/host/**`。**本轮不写产品/测试/脚本**。
- 禁止：重复建立运行时；测试 import `prototype`；靠根 `./...` 判定 bot 通过；为迁就 `main` 旧 imports 恢复旧 API；打印凭据；执行付费微信收发（未授权时）。

## P0 骨架任务（阶段一：数据/接口定义，零逻辑）

> 阶段一骨架：只落数据/接口定义与 CI 合规注释文档，**零业务逻辑**；stub 仅 `return <零值>, errors.New("phase 2: not implemented")`、无返回值方法仅 `// TODO(phase2)` 空体、**禁止 panic**；每包 `skeleton_assert.go` 放编译断言；导出符号英文 Godoc＋包注释；文件 ≤500 行。设计闸 G1=签名与 00.3 共享合同一致，G3=每包白名单（module 根 `package tagent`）。

- [x] 08.P0.1 建立 module 根 package tagent 公共 API 骨架：`Runtime`/`Build` 入口签名与公共 API 面（只签名）—— 前置：00.3、00.P0.1、02.P0.4、03.P0.2、05.P0.1、06.P0.1、07.P0.1 —— 候选拟写文件（非本轮授权）：`tagent.go`、`build.go`、`skeleton_assert.go`（module 根 `package tagent`）—— 设计闸：G1=Runtime/Build 入口签名与 00.3 合同一致；G3=每包白名单 —— 验证：`go build ./...`、`go vet ./...`、`gofmt -l .`（空输出，module 根）；零逻辑由骨架评审门核

- [x] 08.P0.2 建立根 module 内微信宿主桥接口：只建根侧桥类型（wechat module 修复属阶段二 08.2）—— 前置：00.3、00.P0.1、08.P0.1 —— 候选拟写文件（非本轮授权）：`host_bridge.go`（module 根 `package tagent`）、`skeleton_assert.go` —— 设计闸：G1=桥类型与 00.3 合同一致；G3=根 module 内白名单（不动 `examples/wechat-bot/**`）—— 验证：`go build ./...`、`go vet ./...`、`gofmt -l .`（空输出，根侧）；零逻辑由骨架评审门核

## 任务

- [x] 08.1 在 02.5 根包装配入口上完善公开 SDK 与 CLI（构造/启动/关闭/诊断查询），不重复建立运行时 —— 前置：`02.9`、`05.2` —— 复用目标：module 根 `package tagent`（唯一公共门面）、`net/http` —— 复用注意点：入口归属 module 根（`github.com/SpellingDragon/tagent`），非嵌套 `tagent/tagent`；集成测试经根包公开面驱动、**不 import `prototype`**；不另建 org/state/task 组合—— 真实测试：`go test ./tests/integration/host -run '^TestPublicRuntimeEntry$' -count=10 -json`。【M 重开：缺/query无请求主体认证——补差合同见 midterm-review-response.md §二】

- [x] 08.2 微信独立 module 迁移：用户/session 映射、入站去重、显式 `send` 与审批回执 —— 前置：`08.1`、`04.4`/`04.5` —— 复用目标：`wechat-robot-go/wechat`（唯一发送通路）、`delivery-receipts`/`send` 工具（继承） —— 复用注意点：`main.go` 旧 imports（`tagent/agent/governance`/`tagent/event`/`tagent/memory/engine`/`tagent/rl`）按新设计迁移，**不恢复旧符号**；被动通道语义零变化；回执不递归—— 真实测试：`go -C examples/wechat-bot test ./... -run '^TestSessionRouting$' -count=10 -json`（**独立 module**）。

- [x] 08.3 真实文件收发边界：类型识别、媒体发送、大结果副本、权限与失败回执 —— 前置：`08.2`、`03.8` —— 复用目标：`ExtractFilePaths` 六规则 + `FileSender` 窄接口（继承）、`workspace` 唯一副本（03.8） —— 复用注意点：大结果副本走 03.8 外置/workspace 生命周期，不另造副本机制；拒可执行硬规则不可配置；单文件失败不阻断文本与其他文件—— 真实测试：`go -C examples/wechat-bot test ./... -run '^TestFileRoundTrip$' -count=10 -json`。

- [x] 08.4 接 Query/反思/训练诊断实际开关，验证宿主启动/关闭/重启 —— 前置：`06.8`、`07.1`/`07.3`/`07.4`、`08.3` —— 复用目标：Query 只读总图（05.2）、`http.Server` —— 复用注意点：诊断经实际开关暴露、关闭 OTel 不影响 Query；HTTP 服务由宿主持有、SIGTERM 走 `Shutdown`、重试循环受主 ctx 取消（无无控制 goroutine）—— 真实测试：`go test -race ./tests/integration/host -run '^TestRuntimeVisibility$' -count=10 -json`。

- [x] 08.5 对已授权测试接收者验证真实文本/工具/文件与跨 session 往返，逐次核对送达结果 —— 前置：`08.4` 及**明确发送授权**；单次运行 —— 复用目标：真实微信发送通路 —— 复用注意点：资源门（启用门+授权+接收者到位）；未授权即 `BLOCKED`，**不以 mock/替身冒充真实送达**；只报告凭据存在性，不打印秘密值—— 真实测试：`go -C examples/wechat-bot test ./... -run '^TestRealWechat$' -count=1 -json`（资源门，单次）。

## 新增问题修复

（执行期发现的异常/缺口写在此节，格式：场景 → 根源 → 影响面。契约变化先回写 `design.md`，不静默塞进补丁。）
