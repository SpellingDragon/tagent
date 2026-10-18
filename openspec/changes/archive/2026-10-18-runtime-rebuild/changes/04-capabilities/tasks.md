# 04-capabilities — 任务清单（tasks.md）

> 二级子变更 `04-capabilities` 的三级孙任务。**全部未勾选**（`- [ ]`），**不继承**旧 change 的完成数。
> 任务状态仅 `planned` / `design-blocked` 两种；**本轮不声称 Code 实际 PASS**（无实现、无运行）。
> 编号与只读计划 §7「04-capabilities」行一一对应，每原编号仅出现一次。
>
> **验证命令基线**（仓库根执行，编号引用计划 §7 展开式）：
> - 确定性（并发族统一带 `-race`，重复 10 次并出 JSON）：
>   `go test -race ./tests/integration/capabilities -run '^<Test名>$' -count=10 -json`
> - 真实网络/付费资源：**独立门控**、单次：
>   `go test ./tests/integration/capabilities -run '^<Test名>$' -count=1 -json`（须启用门 + 授权资源齐备）
> - 判定口径：点名用例真实 `pass`；未匹配/零匹配/`Skip`/包级 `Fail` 均**不算通过**；不用 `grep/head/tail` 退出码替代原命令。
> - 全部产品实现单的**共同前置 G0–G3**（设计闸）不在此逐行重复；下方"前置"仅列**域内/跨域任务依赖**。
> **两阶段**：既有 `04.1–04.9` 属阶段二，统一以骨架评审门 `00.P0.3` 为额外前置（以 `task-graph.json` 为准），不逐行改写各自「前置」文案。

## 前置与边界

- **上游**：01-state-binding（生效绑定借出）、02-execution（原生 wrapper / 一次 Receipt / 后台结果 / 任务提交端口）、03-history（大结果外置）。本域**只消费**，不复制其装配。
- **写白名单（未来，本轮不写）**：工具/治理/检索相关产品包与 `tests/integration/capabilities/**`。**本轮只写本目录 md**。
- **禁止**：`LLMAgent` 第二循环、占位结构、反射 DI、新 registry 框架、未授权真实付费资源。
- 状态标签：`planned` = 依赖面已清楚，可在设计闸通过后按单实施；`design-blocked` = 关键语义/接口/资源待核，**不得因有空闲代理而强行派发**。

## P0 骨架任务（阶段一：数据/接口定义，零逻辑）

> 阶段一骨架：只落数据/接口定义与 CI 合规注释文档，**零业务逻辑**；stub 仅 `return <零值>, errors.New("phase 2: not implemented")`、无返回值方法仅 `// TODO(phase2)` 空体、**禁止 panic**；每包 `skeleton_assert.go` 放编译断言；导出符号英文 Godoc＋包注释；文件 ≤500 行。设计闸 G1=签名与 00.3 共享合同一致，G3=每包白名单（`tool/exec/**`、`tool/mcp/**`、`tool/remote/**`、`plugin/**`、`memory/search/**`）。

- [x] 04.P0.1 建立 tool/exec 骨架：执行声明/资源标识/停止凭证接口（只声明）—— 前置：00.3、00.P0.1、02.P0.2 —— 候选拟写文件（非本轮授权）：`tool/exec/decl.go`、`tool/exec/stop.go`、`tool/exec/skeleton_assert.go` —— 设计闸：G1=执行声明/资源标识与 00.3 合同一致；G3=每包白名单 —— 验证：`go build ./...`、`go vet ./...`、`gofmt -l tool/exec/`（空输出）；零逻辑由骨架评审门核

- [x] 04.P0.2 建立 tool/mcp 与 tool/remote 适配接口声明（复用原生，不重写协议）—— 前置：00.3、00.P0.1 —— 候选拟写文件（非本轮授权）：`tool/mcp/adapter.go`、`tool/remote/adapter.go`、`tool/mcp/skeleton_assert.go`、`tool/remote/skeleton_assert.go` —— 设计闸：G1=适配接口与 00.3 合同一致且复用原生类型；G3=每包白名单 —— 验证：`go build ./...`、`go vet ./...`、`gofmt -l tool/mcp/ tool/remote/`（空输出）；零逻辑由骨架评审门核

- [x] 04.P0.3 建立 plugin 骨架：相位接口、`Before`/`After`、治理声明类型（只声明）—— 前置：00.3、00.P0.1、02.P0.1 —— 候选拟写文件（非本轮授权）：`plugin/phases.go`、`plugin/governance.go`、`plugin/skeleton_assert.go` —— 设计闸：G1=相位枚举与 00.3 合同一致；G3=每包白名单 —— 验证：`go build ./...`、`go vet ./...`、`gofmt -l plugin/`（空输出）；零逻辑由骨架评审门核

- [x] 04.P0.4 建立 memory/search 骨架：检索端口、混合召回接口（只声明）—— 前置：00.3、00.P0.1、03.P0.1 —— 候选拟写文件（非本轮授权）：`memory/search/port.go`、`memory/search/fusion.go`、`memory/search/skeleton_assert.go` —— 设计闸：G1=检索端口与 00.3 合同一致；G3=每包白名单 —— 验证：`go build ./...`、`go vet ./...`、`gofmt -l memory/search/`（空输出）；零逻辑由骨架评审门核

## 任务

- [x] 04.1 正式注册原生 file/skill/FunctionTool 并使其进入 `Request.Tools` 真实执行；加载状态回 state、文件变化在下一请求边界一致读取 —— 状态：planned —— 前置：02.6（插件执行前恰一次 + 原生 wrapper）、G1 —— 候选文件：module 根装配 `prepare.go`（登记原生 file/skill/FunctionTool，复用框架 `FSRepository`/`CallableTool`，与 01.4 同根装配面串行；**不预建 `skill` 包**，原生 file/skill 复用框架类型）、`state/resources.go`（租约消费→移交 05.x/05.4，本叶由 tool/exec.Manager 承载）（工具加载状态回 state 的绑定消费点）、`tests/integration/capabilities/native_tools_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestNativeToolsAndSkillState$' -count=10 -json`【M 重开：缺统一受理缺失+技能正文不入下一请求——补差合同见 midterm-review-response.md §二】

- [x] 04.2 实现 exec/tmux 外部执行生命周期：启动、**停止凭证**、重挂、TTL/续行与资源租约；`unknown≠dead`，停止未确认不得伪称完成；**连续 `unknown` 只升级诊断或显式停止处理，MUST NOT 作为判死/退出依据，未止义务与资源租约保留且可解释**；**`IsTmuxAvailable` 真实探测（MUST NOT 恒真）与 monitor 节奏（interval/stable_duration）在无 tmux 降级路径可观测**（承载 `action-tool-config` 的活约束，见 `specs/action-tool-config/spec.md`：`monitor` 按代消费 + `IsTmuxAvailable` 真实探测不恒真 + `compress` 键退役） —— 状态：planned —— 前置：02.9（关闭/排空/真实停止）、01.5（组织发布与资源退役） —— 候选文件：`tool/exec/exec.go`（exec 启动/重挂/TTL/续行与资源租约）、`tool/exec/tmux.go`（`IsTmuxAvailable` 真实探测不恒真、monitor 节奏 interval/stable_duration、无 tmux 降级路径）、`tool/exec/stop.go`（停止凭证与停止证据）、`state/resources.go`（租约消费）、`tests/integration/capabilities/exec_lifecycle_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestExecutionResourceLifecycle$' -count=10 -json`

- [x] 04.3 实现 MCP 连接/发现/**固定网关**与刷新失败/关闭；协议原样复用（不重写）；声明不随注册表内容漂移 —— 状态：planned —— 前置：04.1 —— 候选文件：`tool/mcp/registry.go`、`tool/mcp/call.go`、`tool/mcp/discovery.go`、`tests/integration/capabilities/mcp_lifecycle_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestMCPProtocolLifecycle$' -count=10 -json`【M 回归重验：上游 04.1 重开，收口后重跑回归锚再勾选——见 midterm-review-response.md §四】

- [x] 04.4 实现 A2A 与内部 send 的**显式目标/调用归属/权限/重试**，并接入外置路径 —— 状态：planned —— 前置：02.9、03.8（通用工具/任务结果外置） —— 候选文件：`tool/remote/a2a.go`、`tool/remote/send.go`、`tool/remote/delivery.go`、`tests/integration/capabilities/addressed_delivery_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestAddressedDelivery$' -count=10 -json`

- [x] 04.5 实现 warn/strict、风险规则、**异步审批（Hold≠Denied）**、拒绝事实与原生参数一致（执行前策略恰一次） —— 状态：planned —— 前置：02.6 —— 候选文件：`plugin/policy.go`、`plugin/approval.go`、`tests/integration/capabilities/governance_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestGovernanceBeforeExecution$' -count=10 -json`【M 重开：缺正式策略裁决未接线（rig手工补）——补差合同见 midterm-review-response.md §二】

- [x] 04.6 实现预算 epoch、goal 目标状态（**旧语义核实后沿用，不偷改状态词**）、拒绝账本与恢复（经事件回放） —— 状态：**design-blocked**（goal 状态词/语义待现场核实） —— 前置：04.5、03.3（持久提交与幂等接纳） —— 候选文件：`plugin/budget.go`、`plugin/denial.go`、`plugin/goal.go`、`tests/integration/capabilities/governance_recovery_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestGovernanceRecovery$' -count=10 -json`【M 回归重验：上游 04.5/03.3 重开，收口后重跑回归锚再勾选——见 midterm-review-response.md §四】

- [x] 04.7 实现授权时间/类型/关系检索与混合融合；**先验证接口必要性**，再接真实检索源与 embedder —— 状态：**design-blocked**（接口必要性待验证） —— 前置：03.4/03.5（投影恢复/保留回收）、04.5 —— 候选文件：`memory/recall.go`、`memory/semantic.go`、`memory/fusion.go`、`tests/integration/capabilities/recall_consumer_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestRecallConsumer$' -count=10 -json`【M 重开：缺QueryEvents真实存储源未接线——补差合同见 midterm-review-response.md §二】

- [x] 04.8 实现价值评估、整理触发与固化物因果关联（**不改写原始事实**） —— 状态：planned —— 前置：04.7 —— 候选文件：`memory/curation.go`、`tests/integration/capabilities/memory_curation_test.go` —— 验证：`go test -race ./tests/integration/capabilities -run '^TestMemoryCuration$' -count=10 -json`

- [x] 04.9 用**已授权实际检索路径**形成召回质量与开销报告，**保留零增益与失败** —— 状态：**design-blocked**（依赖授权资源） —— 前置：04.7 及授权资源 —— 候选文件：`tests/integration/capabilities/recall_evaluation_test.go`、run 记录目录（仓库外） —— 验证（真实路径，独立单次门控）：`go test ./tests/integration/capabilities -run '^TestRecallEvaluation$' -count=1 -json`（须启用门 + 授权资源齐备；否则合法 Skip，**不以 Skip 结项**）

## 缺口修复

（执行期发现的异常/缺口写在此节，格式：场景 → 根源 → 影响面。契约变化先回写本域 design 与顶层计划，不静默塞进补丁。本轮无。）

## C 系列（概念收敛战役，见 ../../concept-slim-plan.md）
- [x] 04.21 Policy 零选择构造+ApprovalRequest 拆块 —— 验证：见 task-graph 04.21（静态锚+回归锚双轨）
- [x] 04.22 五空相位限期接或删+每窄口范例 —— 验证：见 task-graph 04.22（静态锚+回归锚双轨）
- [x] 04.23 memory 端口归并审视（≤7 接口） —— 验证：见 task-graph 04.23（静态锚+回归锚双轨）
- [x] 04.24 补长任务执行中 Renew 调用点（R2 残余）—— 验证：TestLongExecutionRenewsLease/TestRenewRefusesExpiredLease/消费者锚
- [x] 04.25 补并发跨 session 串场专项测试（R4 残余）—— 验证：TestConcurrentRunsStayScoped/TestCrossSessionContentIsRefused/消费者锚
