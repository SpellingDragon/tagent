# agent 常驻实例与投递契约收敛 编排文档

## 一、结论

- **抵达 DoD 的唯一路径**：W0 防护面（三条红线先绿）→ 01 驱动层重构（每 agent 常驻实例、直驱退役、委派管线化）→ 02/03/04 并发 → F1–F4。
- **当前最大风险**：01 动驱动分流核心（`assembly/host.go` 的 `invokeConsumed`/`lockAddressContext` 双路径合一）会同时触碰 settle 恰好一次与会话占位两条红线语义——W0 防护用例是唯一保险，探针（非入口地址常驻化最小闭环）必须先行。

## 二、编排四问

### 2.1 次序（就绪孙任务表）

| 派发单 | 域/孙任务 | 前置 | 状态 | 触碰集 | 单号 |
|---|---|---|---|---|---|
| W0-A | 01/0.x 防护面 | 无 | ready | tests/integration/host、tests/integration/execution（新增防护用例） | W0-A |
| W1-A | 01/1.1–1.5 | W0-A | blocked | runtime/agent/consumer.go、assembly/host.go、tool/subagent | W1-A |
| W2-A | 02/2.1–2.5 | W1-A（账目消费面）；2.1/2.3 可先行 | blocked | assembly/host_tools.go、runtime/state/send.go、assembly/settle_turn.go | W2-A |
| W2-B | 03/3.1–3.4 | 无 | ready | features/evolution/meditation（golden 夹具）、tests/gates | W2-B |
| W2-C | 04/4.1–4.3 | 无 | ready | openspec 账目、assembly/run.go 注释、api/plugin/material.go 注释 | W2-C |

- **并发准入**：触碰集不相交（`go list -deps` 实测）；W1-A 与 W2-A 同 `assembly` 包相关面——2.1/2.3（`NewSendFace` 删除、元数据同构）与 W1-A 串行或合单（R28）；W2-B/W2-C 与 Go 面不相交可并行。
- **关键路径**（红标）：W0-A → W1-A → W2-A → F。
- **本轮做不了**：route 端口实现（注记预留）；业务 Thread 实体；业务停滞监督。

### 2.2 授权（派发白名单）

- W1-A：`runtime/agent/**`、`assembly/host.go`、`tool/subagent/**` 及各包测试。独占驱动面。
- W2-A：`assembly/host_tools.go`、`runtime/state/send.go`、`assembly/settle_turn.go`（插件路径范围门）。
- W2-B：`features/evolution/meditation/**`（golden 夹具）、`tests/gates/**`。
- W2-C：`openspec/changes/agent-instances-and-delivery/**`（账目）、`assembly/run.go`（仅注释）、`api/plugin/material.go`（仅注释）。
- 共享面单写：`assembly/` 包内跨域改动按 D4 串行；禁 git commit；过程件落 `/tmp/aid-<单号>/`。

### 2.3 凭据（完成度五查落点）

- 查1 勾选真实性：每叶 verify 命令复跑（`-run` 逐字符）。
- 查2 产物盘点：白名单文件 vs `git status`；spec scenarios↔测试映射。
- 查3 数字溯源：golden 轮数/水位读数逐条核对。
- 查4 遗漏：孙任务勾选 vs 域清单。
- 查5 生产消费者实测：01 域必须经 `assembly.Boot` 真实入口驱动非入口地址常驻化；三条红线测试为必跑硬门。
- 必亲验数字：I1–I4 红因行、golden 30+ 轮零重复、红线测试零回退。

### 2.4 释放（锁/等待表）

| 锁 | 占用者 | 释放条件 | 等待表（释放即消费） |
|---|---|---|---|
| 驱动面（host.go/consumer.go） | W1-A | 01 合并 | 补派 W2-A 的账目消费面接线 |
| assembly 包串行窗 | W1-A | 01 合并 | W2-A 的 host_tools 面 |

## 三、明细

- **派发粒度**：孙任务级（R28）；单均 2–5 叶。
- **预算**：W0 ≤0.5 天；W1 ≤3 天（长跑单，R32 标墙钟）；W2 ≤2 天（3 单并发）；W3 ≤1 天。
- **真实路径欠账**：01 的委派管线化需 subagent 真实链路验证；03 的 golden 用脚本模型控制墙钟。
- **训练类**：无。

## 四、台账

| 轮次 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|
| （待填） | | | | | | |
